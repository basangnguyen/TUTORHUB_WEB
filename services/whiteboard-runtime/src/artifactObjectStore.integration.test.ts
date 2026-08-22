import { createHash, randomBytes } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  ArtifactObjectStoreError,
  B2ArtifactObjectStore,
  type ArtifactObjectBinding,
} from "./artifactObjectStore.js";

const confirmation = "I_UNDERSTAND_P5_COLLAB_07_B2_DISPOSABLE_ONLY";
const enabled = process.env.P5_COLLAB_07_B2_CONFIRM === confirmation;
const integrationDescribe = enabled ? describe : describe.skip;

integrationDescribe("B2ArtifactObjectStore disposable", () => {
  let store: B2ArtifactObjectStore;
  let binding: ArtifactObjectBinding | undefined;

  beforeAll(() => {
    store = new B2ArtifactObjectStore(process.env.B2_BUCKET ?? "", {
      applicationKey: process.env.B2_APPLICATION_KEY ?? "",
      endpoint: process.env.B2_ENDPOINT ?? "",
      keyId: process.env.B2_KEY_ID ?? "",
      region: process.env.B2_REGION ?? "",
    });
  });

  afterAll(async () => {
    if (binding) await store.deleteVersion(binding);
  });

  it("binds immutable version metadata and deletes the exact disposable version", async () => {
    const bytes = randomBytes(1536);
    const contentSha256 = createHash("sha256").update(bytes).digest("hex");
    const verificationKeyId = "p5-collab-07-disposable-v1";
    binding = await store.putVerified(bytes, contentSha256, verificationKeyId);

    const loaded = await store.getVerified({
      ...binding,
      contentSha256,
      sizeBytes: bytes.byteLength,
      verificationKeyId,
    });
    expect(Buffer.from(loaded)).toEqual(bytes);

    await expect(
      store.getVerified({
        ...binding,
        contentSha256: "0".repeat(64),
        sizeBytes: bytes.byteLength,
        verificationKeyId,
      }),
    ).rejects.toEqual(
      expect.objectContaining<Partial<ArtifactObjectStoreError>>({
        code: "artifact_object_corrupt",
      }),
    );

    await store.deleteVersion(binding);
    const deleted = binding;
    binding = undefined;
    await expect(
      store.getVerified({
        ...deleted,
        contentSha256,
        sizeBytes: bytes.byteLength,
        verificationKeyId,
      }),
    ).rejects.toEqual(
      expect.objectContaining<Partial<ArtifactObjectStoreError>>({
        code: "artifact_object_unavailable",
      }),
    );
  }, 30_000);
});
