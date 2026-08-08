import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  completeMultipart: vi.fn(),
  createIntent: vi.fn(),
  createMultipart: vi.fn(),
  finalize: vi.fn(),
  issuePart: vi.fn(),
  issueSingle: vi.fn(),
  rotateCSRF: vi.fn(),
}));

vi.mock("@tutorhub/api-client", async () => {
  const actual = await vi.importActual<typeof import("@tutorhub/api-client")>(
    "@tutorhub/api-client",
  );
  return {
    ...actual,
    completeFileMultipartUpload: api.completeMultipart,
    createFileUploadIntent: api.createIntent,
    createFileMultipartUpload: api.createMultipart,
    finalizeFileUpload: api.finalize,
    issueFileMultipartPartCapability: api.issuePart,
    issueFileUploadCapability: api.issueSingle,
    rotateCSRFToken: api.rotateCSRF,
  };
});

import { useClassFileUpload } from "./classFiles";

const tenantID = "b43d5964-b602-4dd5-befe-cbbfe6fc9f0b";
const classID = "7e9e9b62-b439-447c-8b7c-15e7345f8648";
const fileID = "ebdb21f8-209f-4afb-9ee1-298a01782943";
const multipartID = "b931a214-a653-4132-a5ce-44c70b690b0f";

class ChecksumWorker {
  private message?: (event: MessageEvent<unknown>) => void;
  addEventListener(
    name: string,
    listener: (event: MessageEvent<unknown>) => void,
  ) {
    if (name === "message") this.message = listener;
  }
  postMessage() {
    this.message?.({ data: { checksum: "00".repeat(32) } } as MessageEvent);
  }
  terminate() {}
}

describe("useClassFileUpload multipart resume", () => {
  afterEach(() => vi.unstubAllGlobals());

  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("Worker", ChecksumWorker);
    api.rotateCSRF.mockResolvedValue({ csrf_token: "csrf" });
    api.createIntent.mockResolvedValue({
      id: fileID,
      class_id: classID,
      creator_user_id: "fc06b119-e27b-4fa0-93b1-391d70dd81c0",
      display_name: "large.bin",
      declared_media_type: "application/octet-stream",
      expected_size_bytes: 10_000_001,
      expected_checksum_sha256: "00".repeat(32),
      status: "pending",
      version: 1,
      upload_expires_at: "2030-08-08T01:15:00Z",
      created_at: "2030-08-08T01:00:00Z",
      updated_at: "2030-08-08T01:00:00Z",
      viewer_access: { can_download: false, can_retry_upload: true },
    });
    api.createMultipart.mockResolvedValue({
      id: multipartID,
      file_id: fileID,
      status: "active",
      expires_at: "2030-08-08T01:15:00Z",
    });
    api.issuePart.mockImplementation(
      (
        _tenantID: string,
        _fileID: string,
        _multipartID: string,
        part: number,
      ) =>
        Promise.resolve({
          method: "PUT",
          url: `https://storage.example.test/part-${part}`,
          expires_at: "2030-08-08T01:05:00Z",
          part_number: part,
          content_length_bytes: part === 1 ? 8_000_000 : 2_000_001,
          required_headers: {},
        }),
    );
    api.completeMultipart.mockResolvedValue({
      upload: {
        id: multipartID,
        file_id: fileID,
        status: "completed",
        expires_at: "2030-08-08T01:15:00Z",
      },
      storage_version_id: "version-1",
      etag: "complete-etag",
    });
    api.finalize.mockResolvedValue({ status: "uploaded" });
  });

  it("keeps completed parts and resumes from the failed part", async () => {
    let partTwoAttempts = 0;
    class MultipartRequest {
      status = 200;
      private url = "";
      private listeners: Record<string, (() => void) | undefined> = {};
      upload = { addEventListener: vi.fn() };
      open(_method: string, url: string) {
        this.url = url;
      }
      setRequestHeader() {}
      addEventListener(name: string, listener: () => void) {
        this.listeners[name] = listener;
      }
      getResponseHeader(name: string) {
        return name === "etag" ? `etag-${this.url.at(-1)}` : null;
      }
      send() {
        if (this.url.endsWith("part-2")) {
          partTwoAttempts += 1;
          if (partTwoAttempts === 1) {
            this.listeners.error?.();
            return;
          }
        }
        this.listeners.load?.();
      }
    }
    vi.stubGlobal("XMLHttpRequest", MultipartRequest);
    const queryClient = new QueryClient({
      defaultOptions: {
        mutations: { retry: false },
        queries: { retry: false },
      },
    });
    const wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useClassFileUpload(tenantID, classID), {
      wrapper,
    });
    const input = {
      file: new File([new Uint8Array(10_000_001)], "large.bin"),
      clientRequestID: "c5b01234-9257-4f03-b356-1e71f124c4b0",
    };

    await expect(
      act(async () => result.current.mutateAsync(input)),
    ).rejects.toThrow("file_upload_failed");
    await act(async () => result.current.mutateAsync(input));
    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(api.createMultipart).toHaveBeenCalledOnce();
    expect(api.issuePart.mock.calls.map((call) => call[3])).toEqual([1, 2, 2]);
    expect(api.completeMultipart).toHaveBeenCalledOnce();
    expect(api.finalize).toHaveBeenCalledOnce();
    expect(api.issueSingle).not.toHaveBeenCalled();
  });
});
