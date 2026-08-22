import { createHash, randomBytes } from "node:crypto";
import {
  DeleteObjectCommand,
  GetObjectCommand,
  HeadObjectCommand,
  PutObjectCommand,
  S3Client,
} from "@aws-sdk/client-s3";
import { MAX_ARTIFACT_BYTES } from "./artifactEnvelope.js";

interface ObjectClient {
  send(command: unknown): Promise<unknown>;
}

export interface ArtifactObjectBinding {
  objectKey: string;
  objectVersionId: string;
}

export interface ArtifactObjectExpectation extends ArtifactObjectBinding {
  contentSha256: string;
  sizeBytes: number;
  verificationKeyId: string;
}

export class ArtifactObjectStoreError extends Error {
  constructor(
    readonly code:
      | "artifact_object_binding_invalid"
      | "artifact_object_corrupt"
      | "artifact_object_too_large"
      | "artifact_object_unavailable",
  ) {
    super(code);
    this.name = "ArtifactObjectStoreError";
  }
}

export class B2ArtifactObjectStore {
  private readonly client: ObjectClient;

  constructor(
    private readonly bucket: string,
    config: {
      applicationKey: string;
      endpoint: string;
      keyId: string;
      region: string;
    },
    client?: ObjectClient,
  ) {
    this.client =
      client ??
      new S3Client({
        credentials: {
          accessKeyId: config.keyId,
          secretAccessKey: config.applicationKey,
        },
        endpoint: config.endpoint,
        forcePathStyle: true,
        region: config.region,
      });
  }

  async putVerified(
    bytes: Uint8Array,
    contentSha256: string,
    verificationKeyId: string,
  ): Promise<ArtifactObjectBinding> {
    validateExpectationValues(
      bytes.byteLength,
      contentSha256,
      verificationKeyId,
    );
    if (sha256(bytes) !== contentSha256) {
      throw new ArtifactObjectStoreError("artifact_object_corrupt");
    }
    const token = randomBytes(24).toString("hex");
    const objectKey = `wb/${token.slice(0, 2)}/${token}`;
    try {
      const result = (await this.client.send(
        new PutObjectCommand({
          Body: bytes,
          Bucket: this.bucket,
          ChecksumSHA256: Buffer.from(contentSha256, "hex").toString("base64"),
          ContentLength: bytes.byteLength,
          ContentType: "application/vnd.tutorhub.whiteboard-snapshot+json",
          Key: objectKey,
          Metadata: {
            "content-sha256": contentSha256,
            "format-version": "2",
            "verification-key-id": verificationKeyId,
          },
        }),
      )) as { VersionId?: string };
      const objectVersionId = result.VersionId?.trim() ?? "";
      if (objectVersionId === "") {
        throw new ArtifactObjectStoreError("artifact_object_binding_invalid");
      }
      const expectation: ArtifactObjectExpectation = {
        contentSha256,
        objectKey,
        objectVersionId,
        sizeBytes: bytes.byteLength,
        verificationKeyId,
      };
      await this.verifyHead(expectation);
      const loaded = await this.getVerified(expectation);
      if (!bytesEqual(bytes, loaded)) {
        throw new ArtifactObjectStoreError("artifact_object_corrupt");
      }
      return { objectKey, objectVersionId };
    } catch (error) {
      if (error instanceof ArtifactObjectStoreError) throw error;
      throw new ArtifactObjectStoreError("artifact_object_unavailable");
    }
  }

  async getVerified(
    expectation: ArtifactObjectExpectation,
  ): Promise<Uint8Array> {
    validateObjectBinding(expectation);
    validateExpectationValues(
      expectation.sizeBytes,
      expectation.contentSha256,
      expectation.verificationKeyId,
    );
    try {
      await this.verifyHead(expectation);
      const result = (await this.client.send(
        new GetObjectCommand({
          Bucket: this.bucket,
          Key: expectation.objectKey,
          VersionId: expectation.objectVersionId,
        }),
      )) as {
        Body?: { transformToByteArray?: () => Promise<Uint8Array> };
        ContentLength?: number;
        Metadata?: Record<string, string>;
        VersionId?: string;
      };
      const bytes = await result.Body?.transformToByteArray?.();
      if (
        !bytes ||
        bytes.byteLength !== expectation.sizeBytes ||
        result.VersionId !== expectation.objectVersionId ||
        result.Metadata?.["content-sha256"] !== expectation.contentSha256 ||
        result.Metadata?.["format-version"] !== "2" ||
        result.Metadata?.["verification-key-id"] !==
          expectation.verificationKeyId
      ) {
        throw new ArtifactObjectStoreError("artifact_object_corrupt");
      }
      if (sha256(bytes) !== expectation.contentSha256) {
        throw new ArtifactObjectStoreError("artifact_object_corrupt");
      }
      return bytes;
    } catch (error) {
      if (error instanceof ArtifactObjectStoreError) throw error;
      throw new ArtifactObjectStoreError("artifact_object_unavailable");
    }
  }

  async deleteVersion(binding: ArtifactObjectBinding): Promise<void> {
    validateObjectBinding(binding);
    try {
      await this.client.send(
        new DeleteObjectCommand({
          Bucket: this.bucket,
          Key: binding.objectKey,
          VersionId: binding.objectVersionId,
        }),
      );
    } catch {
      throw new ArtifactObjectStoreError("artifact_object_unavailable");
    }
  }

  private async verifyHead(
    expectation: ArtifactObjectExpectation,
  ): Promise<void> {
    const result = (await this.client.send(
      new HeadObjectCommand({
        Bucket: this.bucket,
        Key: expectation.objectKey,
        VersionId: expectation.objectVersionId,
      }),
    )) as {
      ContentLength?: number;
      Metadata?: Record<string, string>;
      VersionId?: string;
    };
    if (
      result.ContentLength !== expectation.sizeBytes ||
      result.VersionId !== expectation.objectVersionId ||
      result.Metadata?.["content-sha256"] !== expectation.contentSha256 ||
      result.Metadata?.["format-version"] !== "2" ||
      result.Metadata?.["verification-key-id"] !== expectation.verificationKeyId
    ) {
      throw new ArtifactObjectStoreError("artifact_object_corrupt");
    }
  }
}

function validateObjectBinding(binding: ArtifactObjectBinding): void {
  if (
    !/^wb\/[a-f0-9]{2}\/[a-f0-9]{48}$/.test(binding.objectKey) ||
    binding.objectVersionId.length === 0 ||
    binding.objectVersionId.length > 255 ||
    containsControlCharacter(binding.objectVersionId)
  ) {
    throw new ArtifactObjectStoreError("artifact_object_binding_invalid");
  }
}

function containsControlCharacter(value: string): boolean {
  return Array.from(value).some((character) => {
    const codePoint = character.codePointAt(0) ?? 0;
    return codePoint <= 0x1f || codePoint === 0x7f;
  });
}

function validateExpectationValues(
  sizeBytes: number,
  contentSha256: string,
  verificationKeyId: string,
): void {
  if (!Number.isSafeInteger(sizeBytes) || sizeBytes < 1) {
    throw new ArtifactObjectStoreError("artifact_object_binding_invalid");
  }
  if (sizeBytes > MAX_ARTIFACT_BYTES) {
    throw new ArtifactObjectStoreError("artifact_object_too_large");
  }
  if (
    !/^[a-f0-9]{64}$/.test(contentSha256) ||
    !/^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/.test(verificationKeyId)
  ) {
    throw new ArtifactObjectStoreError("artifact_object_binding_invalid");
  }
}

function bytesEqual(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false;
  for (let index = 0; index < left.byteLength; index += 1) {
    if (left[index] !== right[index]) return false;
  }
  return true;
}

function sha256(value: Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}
