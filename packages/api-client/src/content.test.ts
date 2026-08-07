import { describe, expect, it, vi } from "vitest";
import {
  createFileUploadIntent,
  finalizeFileUpload,
  getFileMetadata,
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
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("content file API", () => {
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
