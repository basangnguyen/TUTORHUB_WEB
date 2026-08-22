import {
  DeleteObjectCommand,
  GetObjectCommand,
  HeadObjectCommand,
  PutObjectCommand,
} from "@aws-sdk/client-s3";
import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import {
  ArtifactObjectStoreError,
  B2ArtifactObjectStore,
} from "./artifactObjectStore.js";

class FakeVersionedObjectClient {
  bytes = new Uint8Array();
  deletedVersion = "";
  metadata: Record<string, string> = {};
  objectKey = "";
  putChecksumSha256: string | undefined;
  putIfNoneMatch: string | undefined;
  readonly versionId = "4_z-disposable-version";

  async send(command: unknown): Promise<unknown> {
    if (command instanceof PutObjectCommand) {
      this.objectKey = command.input.Key ?? "";
      this.bytes = new Uint8Array(command.input.Body as Uint8Array);
      this.metadata = command.input.Metadata ?? {};
      this.putChecksumSha256 = command.input.ChecksumSHA256;
      this.putIfNoneMatch = command.input.IfNoneMatch;
      return { VersionId: this.versionId };
    }
    if (command instanceof HeadObjectCommand) return this.objectResult();
    if (command instanceof GetObjectCommand) {
      return {
        ...this.objectResult(),
        Body: { transformToByteArray: async () => this.bytes },
      };
    }
    if (command instanceof DeleteObjectCommand) {
      this.deletedVersion = command.input.VersionId ?? "";
      return {};
    }
    throw new Error("unsupported_command");
  }

  private objectResult(): Record<string, unknown> {
    return {
      ContentLength: this.bytes.byteLength,
      Metadata: this.metadata,
      VersionId: this.versionId,
    };
  }
}

describe("B2ArtifactObjectStore", () => {
  it("requires one immutable version and verifies metadata plus bytes", async () => {
    const client = new FakeVersionedObjectClient();
    const store = createStore(client);
    const bytes = new TextEncoder().encode('{"artifact":"bounded"}');
    const contentSha256 = createHash("sha256").update(bytes).digest("hex");

    const binding = await store.putVerified(
      bytes,
      contentSha256,
      "snapshot-binding-v1",
    );
    const loaded = await store.getVerified({
      ...binding,
      contentSha256,
      sizeBytes: bytes.byteLength,
      verificationKeyId: "snapshot-binding-v1",
    });
    await store.deleteVersion(binding);

    expect(binding.objectKey).toMatch(/^wb\/[a-f0-9]{2}\/[a-f0-9]{48}$/);
    expect(binding.objectVersionId).toBe(client.versionId);
    expect(loaded).toEqual(bytes);
    expect(client.deletedVersion).toBe(client.versionId);
    expect(client.putChecksumSha256).toBe(
      Buffer.from(contentSha256, "hex").toString("base64"),
    );
    expect(client.putIfNoneMatch).toBeUndefined();
  });

  it("quarantines a mismatched exact object version", async () => {
    const client = new FakeVersionedObjectClient();
    const store = createStore(client);
    const bytes = new TextEncoder().encode("artifact");
    const sha = createHash("sha256").update(bytes).digest("hex");
    const binding = await store.putVerified(bytes, sha, "snapshot-binding-v1");
    client.bytes = new TextEncoder().encode("corrupt!");

    await expect(
      store.getVerified({
        ...binding,
        contentSha256: sha,
        sizeBytes: bytes.byteLength,
        verificationKeyId: "snapshot-binding-v1",
      }),
    ).rejects.toEqual(
      expect.objectContaining<Partial<ArtifactObjectStoreError>>({
        code: "artifact_object_corrupt",
      }),
    );
  });
});

function createStore(client: FakeVersionedObjectClient): B2ArtifactObjectStore {
  return new B2ArtifactObjectStore(
    "private-bucket",
    {
      applicationKey: "not-a-real-application-key",
      endpoint: "https://storage.example",
      keyId: "not-a-real-key-id",
      region: "ap-southeast-1",
    },
    client,
  );
}
