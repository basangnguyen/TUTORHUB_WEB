import { describe, expect, it, vi } from "vitest";
import {
  abortFileMultipartUpload,
  completeFileMultipartUpload,
  createFileMultipartUpload,
  createFileUploadIntent,
  finalizeFileUpload,
  getFileMetadata,
  issueFileDownloadCapability,
  issueFileMultipartPartCapability,
  issueFileUploadCapability,
  listClassFiles,
} from "./index";
import type {
  ContentFile,
  CreateFileUploadIntentRequest,
  FinalizeFileUploadRequest,
} from "./index";

const tenantID = "7f44c093-1cb2-46ae-8285-779b78728524";
const classID = "0ce0994b-1d0c-4125-9ad0-dfba33f70322";
const fileID = "527a9874-df73-4bf7-97f0-10f0e6bd9a8d";
const actorID = "5391c8b2-1224-4105-a44e-452eb69d9884";
const multipartID = "f46d1d0e-d724-4ca5-8f87-950c9040f027";

const pendingFile: ContentFile = {
  id: fileID,
  class_id: classID,
  creator_user_id: actorID,
  display_name: "lesson.pdf",
  declared_media_type: "application/pdf",
  expected_size_bytes: 42,
  expected_checksum_sha256: "00".repeat(32),
  status: "pending",
  version: 1,
  upload_expires_at: "2030-08-07T10:15:00Z",
  created_at: "2030-08-07T10:00:00Z",
  updated_at: "2030-08-07T10:00:00Z",
  viewer_access: { can_download: false, can_retry_upload: true },
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("content file API", () => {
  it("binds class-file pagination to the expected tenant and class", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(
        jsonResponse({ items: [pendingFile], next_cursor: "next-files" }),
      );

    const page = await listClassFiles(
      tenantID,
      classID,
      { cursor: "current-files", limit: 12 },
      { baseUrl: "https://web.example.test/api", fetch: fetchMock },
    );

    expect(page.next_cursor).toBe("next-files");
    const request = fetchMock.mock.calls[0]?.[0] as Request;
    expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(tenantID);
    expect(new URL(request.url).pathname).toBe(
      `/api/v1/classes/${classID}/files`,
    );
    expect(new URL(request.url).searchParams.get("cursor")).toBe(
      "current-files",
    );
    expect(new URL(request.url).searchParams.get("limit")).toBe("12");
  });

  it("binds intent, metadata and finalize to the expected tenant with exact bodies", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementation(() => Promise.resolve(jsonResponse(pendingFile)));
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };
    const createInput = {
      class_id: classID,
      display_name: "lesson.pdf",
      declared_media_type: "application/pdf",
      expected_size_bytes: 42,
      checksum_sha256: "00".repeat(32),
      client_request_id: "668cd4e4-2c2a-48f0-96c0-d528d373fbbb",
    } satisfies CreateFileUploadIntentRequest;
    const finalizeInput = {
      expected_version: 1,
      storage_version_id: "version-1",
    } satisfies FinalizeFileUploadRequest;

    await createFileUploadIntent(tenantID, createInput, "create-csrf", options);
    await getFileMetadata(tenantID, fileID, options);
    await finalizeFileUpload(
      tenantID,
      fileID,
      finalizeInput,
      "finalize-csrf",
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests).toHaveLength(3);
    for (const request of requests) {
      expect(request.credentials).toBe("include");
      expect(request.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
        tenantID,
      );
    }
    expect(requests.map((request) => request.method)).toEqual([
      "POST",
      "GET",
      "POST",
    ]);
    expect(requests[0]?.headers.get("X-CSRF-Token")).toBe("create-csrf");
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBeNull();
    expect(requests[2]?.headers.get("X-CSRF-Token")).toBe("finalize-csrf");
    expect(JSON.parse(await requests[0]!.clone().text())).toEqual(createInput);
    expect(JSON.parse(await requests[2]!.clone().text())).toEqual(
      finalizeInput,
    );
    expect(new URL(requests[1]!.url).pathname).toBe(`/api/v1/files/${fileID}`);
    expect(new URL(requests[2]!.url).pathname).toBe(
      `/api/v1/files/${fileID}/finalize`,
    );
  });

  it("issues upload and download capabilities with CSRF and tenant binding", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() =>
        Promise.resolve(
          jsonResponse({
            method: "PUT",
            url: "https://storage.example/upload?signature=secret",
            expires_at: "2030-08-07T10:05:00Z",
            content_length_bytes: 42,
            required_headers: { "Content-Type": "application/pdf" },
          }),
        ),
      )
      .mockImplementationOnce(() =>
        Promise.resolve(
          jsonResponse({
            method: "GET",
            url: "https://storage.example/download?signature=secret",
            expires_at: "2030-08-07T10:02:00Z",
          }),
        ),
      );
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await issueFileUploadCapability(
      tenantID,
      fileID,
      { expected_version: 1 },
      "upload-csrf",
      options,
    );
    await issueFileDownloadCapability(
      tenantID,
      fileID,
      "download-csrf",
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => new URL(request.url).pathname)).toEqual([
      `/api/v1/files/${fileID}/upload-capability`,
      `/api/v1/files/${fileID}/download-capability`,
    ]);
    expect(requests[0]?.headers.get("X-CSRF-Token")).toBe("upload-csrf");
    expect(requests[1]?.headers.get("X-CSRF-Token")).toBe("download-csrf");
    expect(requests[0]?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    expect(requests[1]?.headers.get("X-TutorHub-Expected-Tenant-ID")).toBe(
      tenantID,
    );
    expect(JSON.parse(await requests[0]!.clone().text())).toEqual({
      expected_version: 1,
    });
    expect(await requests[1]!.clone().text()).toBe("");
  });

  it("binds multipart initiate, part, complete and abort to tenant ownership", async () => {
    const upload = {
      id: multipartID,
      file_id: fileID,
      status: "active",
      expires_at: "2030-08-07T10:15:00Z",
    } as const;
    const fetchMock = vi
      .fn()
      .mockImplementationOnce(() => Promise.resolve(jsonResponse(upload, 201)))
      .mockImplementationOnce(() =>
        Promise.resolve(
          jsonResponse({
            method: "PUT",
            url: "https://storage.example/part?signature=secret",
            expires_at: "2030-08-07T10:05:00Z",
            part_number: 1,
            content_length_bytes: 42,
            required_headers: {},
          }),
        ),
      )
      .mockImplementationOnce(() =>
        Promise.resolve(
          jsonResponse({
            upload: { ...upload, status: "completed" },
            storage_version_id: "version-2",
            etag: "multipart-etag",
          }),
        ),
      )
      .mockImplementationOnce(() =>
        Promise.resolve(jsonResponse({ ...upload, status: "aborted" })),
      );
    const options = {
      baseUrl: "https://web.example.test/api",
      fetch: fetchMock,
    };

    await createFileMultipartUpload(
      tenantID,
      fileID,
      { expected_version: 1 },
      "multipart-csrf",
      options,
    );
    await issueFileMultipartPartCapability(
      tenantID,
      fileID,
      multipartID,
      1,
      { expected_version: 1, content_length_bytes: 42 },
      "part-csrf",
      options,
    );
    await completeFileMultipartUpload(
      tenantID,
      fileID,
      multipartID,
      { expected_version: 1, parts: [{ part_number: 1, etag: "part-etag" }] },
      "complete-csrf",
      options,
    );
    await abortFileMultipartUpload(
      tenantID,
      fileID,
      multipartID,
      { expected_version: 1 },
      "abort-csrf",
      options,
    );

    const requests = fetchMock.mock.calls.map((call) => call[0] as Request);
    expect(requests.map((request) => new URL(request.url).pathname)).toEqual([
      `/api/v1/files/${fileID}/multipart-uploads`,
      `/api/v1/files/${fileID}/multipart-uploads/${multipartID}/parts/1/capability`,
      `/api/v1/files/${fileID}/multipart-uploads/${multipartID}/complete`,
      `/api/v1/files/${fileID}/multipart-uploads/${multipartID}/abort`,
    ]);
    expect(
      requests.every(
        (request) =>
          request.headers.get("X-TutorHub-Expected-Tenant-ID") === tenantID,
      ),
    ).toBe(true);
  });

  it("rejects an empty expected tenant before sending", async () => {
    const fetchMock = vi.fn();
    await expect(
      getFileMetadata(" ", fileID, {
        baseUrl: "https://web.example.test/api",
        fetch: fetchMock,
      }),
    ).rejects.toThrow(TypeError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("never models object keys or provider proof in the public file projection", () => {
    expect(pendingFile).not.toHaveProperty("object_key");
    expect(pendingFile).not.toHaveProperty("storage_etag");
    expect(pendingFile).not.toHaveProperty("storage_version_id");
    expect(pendingFile).not.toHaveProperty("tenant_id");
  });
});
