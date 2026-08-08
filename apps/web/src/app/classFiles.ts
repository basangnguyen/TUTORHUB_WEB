import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import {
  APIRequestError,
  completeFileMultipartUpload,
  createFileMultipartUpload,
  createFileUploadIntent,
  finalizeFileUpload,
  issueFileDownloadCapability,
  issueFileMultipartPartCapability,
  issueFileUploadCapability,
  listClassFiles,
  rotateCSRFToken,
  type ContentFile,
} from "@tutorhub/api-client";
import { useRef, useState } from "react";

const classFilePageSize = 20;
const multipartThresholdBytes = 10_000_000;
const multipartPartBytes = 8_000_000;

function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL ?? "/api";
}

export const classFileQueryKeys = {
  all: ["class-files"] as const,
  tenant: (tenantID: string) => ["class-files", tenantID] as const,
  list: (tenantID: string, classID: string) =>
    ["class-files", tenantID, classID] as const,
};

function shouldRetryFileQuery(failureCount: number, error: Error) {
  return (
    failureCount < 1 &&
    !(
      error instanceof APIRequestError &&
      error.status >= 400 &&
      error.status < 500
    )
  );
}

export function useClassFiles(
  tenantID: string | undefined,
  classID: string | undefined,
) {
  return useInfiniteQuery({
    queryKey: classFileQueryKeys.list(
      tenantID ?? "inactive",
      classID ?? "invalid",
    ),
    queryFn: ({ pageParam, signal }) =>
      listClassFiles(
        tenantID ?? "",
        classID ?? "",
        { cursor: pageParam, limit: classFilePageSize },
        { baseUrl: getApiBaseUrl(), signal },
      ),
    enabled: Boolean(tenantID && classID),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    initialPageParam: undefined as string | undefined,
    retry: shouldRetryFileQuery,
    staleTime: 20_000,
  });
}

export type ClassFileTransferPhase =
  "idle" | "checksum" | "reserving" | "uploading" | "finalizing" | "complete";

export interface ClassFileTransferProgress {
  phase: ClassFileTransferPhase;
  percent: number;
}

interface ClassFileUploadInput {
  file: File;
  clientRequestID: string;
}

export async function sha256Hex(file: File): Promise<string> {
  if (typeof Worker !== "undefined") {
    return new Promise((resolve, reject) => {
      const worker = new Worker(
        new URL("./fileChecksum.worker.ts", import.meta.url),
        {
          type: "module",
        },
      );
      worker.addEventListener("message", (event: MessageEvent<unknown>) => {
        worker.terminate();
        const result = event.data as { checksum?: string; error?: string };
        if (result.checksum) {
          resolve(result.checksum);
        } else {
          reject(new Error(result.error ?? "file_checksum_failed"));
        }
      });
      worker.addEventListener("error", () => {
        worker.terminate();
        reject(new Error("file_checksum_failed"));
      });
      worker.postMessage(file);
    });
  }
  const contents = await file.arrayBuffer();
  const digest = await crypto.subtle.digest("SHA-256", contents);
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export function putFileWithProgress(
  capability: {
    url: string;
    required_headers: Record<string, string>;
  },
  file: File,
  onProgress: (percent: number) => void,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("PUT", capability.url, true);
    for (const [name, value] of Object.entries(capability.required_headers)) {
      request.setRequestHeader(name, value);
    }
    request.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable && event.total > 0) {
        onProgress(
          Math.min(100, Math.round((event.loaded / event.total) * 100)),
        );
      }
    });
    request.addEventListener("error", () =>
      reject(new Error("file_upload_failed")),
    );
    request.addEventListener("abort", () =>
      reject(new Error("file_upload_aborted")),
    );
    request.addEventListener("load", () => {
      if (request.status < 200 || request.status >= 300) {
        reject(new Error("file_upload_failed"));
        return;
      }
      const versionID = request.getResponseHeader("x-amz-version-id")?.trim();
      if (!versionID) {
        reject(new Error("file_upload_version_missing"));
        return;
      }
      onProgress(100);
      resolve(versionID);
    });
    request.send(file);
  });
}

function putMultipartPartWithProgress(
  capability: {
    url: string;
    required_headers: Record<string, string>;
  },
  part: Blob,
  onProgress: (loadedBytes: number) => void,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("PUT", capability.url, true);
    for (const [name, value] of Object.entries(capability.required_headers)) {
      request.setRequestHeader(name, value);
    }
    request.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable) {
        onProgress(event.loaded);
      }
    });
    request.addEventListener("error", () =>
      reject(new Error("file_upload_failed")),
    );
    request.addEventListener("abort", () =>
      reject(new Error("file_upload_aborted")),
    );
    request.addEventListener("load", () => {
      if (request.status < 200 || request.status >= 300) {
        reject(new Error("file_upload_failed"));
        return;
      }
      const etag = request.getResponseHeader("etag")?.trim();
      if (!etag) {
        reject(new Error("file_upload_etag_missing"));
        return;
      }
      onProgress(part.size);
      resolve(etag);
    });
    request.send(part);
  });
}

interface MultipartResumeState {
  clientRequestID: string;
  checksum: string;
  fileID: string;
  fileName: string;
  fileSize: number;
  fileVersion: number;
  multipartID: string;
  completedParts: Map<number, string>;
  storageVersionID?: string;
}

interface SingleFinalizeResumeState {
  clientRequestID: string;
  checksum: string;
  fileName: string;
  fileSize: number;
  storageVersionID: string;
}

export function useClassFileUpload(
  tenantID: string | undefined,
  classID: string,
) {
  const queryClient = useQueryClient();
  const multipartResume = useRef<MultipartResumeState | null>(null);
  const singleFinalizeResume = useRef<SingleFinalizeResumeState | null>(null);
  const [progress, setProgress] = useState<ClassFileTransferProgress>({
    phase: "idle",
    percent: 0,
  });
  const mutation = useMutation({
    mutationFn: async ({ file, clientRequestID }: ClassFileUploadInput) => {
      if (!tenantID) {
        throw new Error("file_scope_missing");
      }
      const resumable = multipartResume.current;
      const canResume =
        resumable?.clientRequestID === clientRequestID &&
        resumable.fileName === file.name &&
        resumable.fileSize === file.size;
      const singleResumable = singleFinalizeResume.current;
      const canResumeSingleFinalize =
        singleResumable?.clientRequestID === clientRequestID &&
        singleResumable.fileName === file.name &&
        singleResumable.fileSize === file.size;
      setProgress({ phase: "checksum", percent: 0 });
      const checksum = canResume
        ? resumable.checksum
        : canResumeSingleFinalize
          ? singleResumable.checksum
          : await sha256Hex(file);
      setProgress({ phase: "reserving", percent: 0 });
      let csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const intent = await createFileUploadIntent(
        tenantID,
        {
          class_id: classID,
          display_name: file.name,
          declared_media_type: file.type || "application/octet-stream",
          expected_size_bytes: file.size,
          checksum_sha256: checksum,
          client_request_id: clientRequestID,
        },
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
      let storageVersionID: string;
      if (file.size < multipartThresholdBytes) {
        multipartResume.current = null;
        if (canResumeSingleFinalize) {
          storageVersionID = singleResumable.storageVersionID;
        } else {
          csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
          const capability = await issueFileUploadCapability(
            tenantID,
            intent.id,
            { expected_version: intent.version },
            csrf.csrf_token,
            { baseUrl: getApiBaseUrl() },
          );
          setProgress({ phase: "uploading", percent: 0 });
          storageVersionID = await putFileWithProgress(
            capability,
            file,
            (percent) => setProgress({ phase: "uploading", percent }),
          );
          singleFinalizeResume.current = {
            clientRequestID,
            checksum,
            fileName: file.name,
            fileSize: file.size,
            storageVersionID,
          };
        }
      } else {
        singleFinalizeResume.current = null;
        let resume = canResume ? resumable : null;
        if (!resume) {
          csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
          const multipart = await createFileMultipartUpload(
            tenantID,
            intent.id,
            { expected_version: intent.version },
            csrf.csrf_token,
            { baseUrl: getApiBaseUrl() },
          );
          resume = {
            clientRequestID,
            checksum,
            fileID: intent.id,
            fileName: file.name,
            fileSize: file.size,
            fileVersion: intent.version,
            multipartID: multipart.id,
            completedParts: new Map(),
          };
          multipartResume.current = resume;
        }
        let completedBytes = 0;
        for (const partNumber of resume.completedParts.keys()) {
          const start = (partNumber - 1) * multipartPartBytes;
          completedBytes += Math.min(multipartPartBytes, file.size - start);
        }
        const partCount = Math.ceil(file.size / multipartPartBytes);
        for (let partNumber = 1; partNumber <= partCount; partNumber += 1) {
          if (resume.completedParts.has(partNumber)) {
            continue;
          }
          const start = (partNumber - 1) * multipartPartBytes;
          const part = file.slice(
            start,
            Math.min(start + multipartPartBytes, file.size),
          );
          csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
          const capability = await issueFileMultipartPartCapability(
            tenantID,
            resume.fileID,
            resume.multipartID,
            partNumber,
            {
              expected_version: resume.fileVersion,
              content_length_bytes: part.size,
            },
            csrf.csrf_token,
            { baseUrl: getApiBaseUrl() },
          );
          const beforePart = completedBytes;
          const etag = await putMultipartPartWithProgress(
            capability,
            part,
            (loadedBytes) => {
              const percent = Math.round(
                ((beforePart + loadedBytes) / file.size) * 100,
              );
              setProgress({
                phase: "uploading",
                percent: Math.min(100, percent),
              });
            },
          );
          resume.completedParts.set(partNumber, etag);
          completedBytes += part.size;
        }
        if (resume.storageVersionID) {
          storageVersionID = resume.storageVersionID;
        } else {
          csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
          const completed = await completeFileMultipartUpload(
            tenantID,
            resume.fileID,
            resume.multipartID,
            {
              expected_version: resume.fileVersion,
              parts: [...resume.completedParts.entries()]
                .sort(([left], [right]) => left - right)
                .map(([part_number, etag]) => ({ part_number, etag })),
            },
            csrf.csrf_token,
            { baseUrl: getApiBaseUrl() },
          );
          storageVersionID = completed.storage_version_id;
          resume.storageVersionID = storageVersionID;
        }
      }
      setProgress({ phase: "finalizing", percent: 100 });
      csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      return finalizeFileUpload(
        tenantID,
        intent.id,
        {
          expected_version: intent.version,
          storage_version_id: storageVersionID,
        },
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
    },
    onSuccess: async () => {
      multipartResume.current = null;
      singleFinalizeResume.current = null;
      setProgress({ phase: "complete", percent: 100 });
      if (tenantID) {
        await queryClient.invalidateQueries({
          queryKey: classFileQueryKeys.list(tenantID, classID),
        });
      }
    },
    retry: false,
  });

  return {
    ...mutation,
    progress,
    resetTransfer: () => {
      mutation.reset();
      multipartResume.current = null;
      singleFinalizeResume.current = null;
      setProgress({ phase: "idle", percent: 0 });
    },
  };
}

export function useClassFileDownload(tenantID: string | undefined) {
  return useMutation({
    mutationFn: async (file: ContentFile) => {
      if (!tenantID || !file.viewer_access.can_download) {
        throw new Error("file_download_forbidden");
      }
      const csrf = await rotateCSRFToken({ baseUrl: getApiBaseUrl() });
      const capability = await issueFileDownloadCapability(
        tenantID,
        file.id,
        csrf.csrf_token,
        { baseUrl: getApiBaseUrl() },
      );
      const anchor = document.createElement("a");
      anchor.href = capability.url;
      anchor.download = file.display_name;
      anchor.rel = "noreferrer noopener";
      anchor.referrerPolicy = "no-referrer";
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      return file.id;
    },
    retry: false,
  });
}
