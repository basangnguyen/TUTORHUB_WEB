import { createHash, timingSafeEqual } from "node:crypto";
import {
  GetObjectCommand,
  HeadBucketCommand,
  PutObjectCommand,
  S3Client,
} from "@aws-sdk/client-s3";
import type { PortableSnapshotStore, SnapshotArtifact } from "./contracts.js";

const MAX_PORTABLE_BYTES = 8 * 1024 * 1024;

interface ObjectClient {
  send(command: unknown): Promise<unknown>;
}

export class SnapshotStoreError extends Error {
  constructor(
    readonly code:
      "snapshot_corrupt" | "snapshot_too_large" | "snapshot_unavailable",
  ) {
    super(code);
    this.name = "SnapshotStoreError";
  }
}

export class B2PortableSnapshotStore implements PortableSnapshotStore {
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

  async probe(): Promise<void> {
    try {
      await this.client.send(new HeadBucketCommand({ Bucket: this.bucket }));
    } catch {
      throw new SnapshotStoreError("snapshot_unavailable");
    }
  }

  async put(bytes: Uint8Array): Promise<SnapshotArtifact> {
    validatePortableEnvelope(bytes);
    const checksum = sha256(bytes);
    const objectKey = `portable/v1/${checksum.slice(0, 2)}/${checksum}.json`;
    try {
      const existing = await this.getObject(objectKey);
      if (existing) {
        if (!checksumMatches(existing, checksum)) {
          throw new SnapshotStoreError("snapshot_corrupt");
        }
        return { bytes: existing, checksum, objectKey };
      }

      await this.client.send(
        new PutObjectCommand({
          Body: bytes,
          Bucket: this.bucket,
          ChecksumSHA256: Buffer.from(checksum, "hex").toString("base64"),
          ContentLength: bytes.byteLength,
          ContentType: "application/json",
          Key: objectKey,
          Metadata: {
            checksum,
            format: "tutorhub-excalidraw-portable-v1",
          },
        }),
      );
      const loadedBytes = await this.getObject(objectKey);
      if (!loadedBytes || !checksumMatches(loadedBytes, checksum)) {
        throw new SnapshotStoreError("snapshot_corrupt");
      }
      return { bytes: loadedBytes, checksum, objectKey };
    } catch (error) {
      if (error instanceof SnapshotStoreError) throw error;
      throw new SnapshotStoreError("snapshot_unavailable");
    }
  }

  private async getObject(objectKey: string): Promise<Uint8Array | null> {
    try {
      const loaded = (await this.client.send(
        new GetObjectCommand({ Bucket: this.bucket, Key: objectKey }),
      )) as { Body?: { transformToByteArray?: () => Promise<Uint8Array> } };
      return (await loaded.Body?.transformToByteArray?.()) ?? null;
    } catch (error) {
      if (isObjectNotFound(error)) return null;
      throw error;
    }
  }
}

function isObjectNotFound(error: unknown): boolean {
  if (typeof error !== "object" || error === null) return false;
  const metadata = (error as { $metadata?: { httpStatusCode?: number } })
    .$metadata;
  return metadata?.httpStatusCode === 404;
}

function validatePortableEnvelope(bytes: Uint8Array): void {
  if (bytes.byteLength === 0 || bytes.byteLength > MAX_PORTABLE_BYTES) {
    throw new SnapshotStoreError("snapshot_too_large");
  }
  try {
    const value = JSON.parse(
      new TextDecoder("utf-8", { fatal: true }).decode(bytes),
    ) as unknown;
    if (
      typeof value !== "object" ||
      value === null ||
      Array.isArray(value) ||
      (value as Record<string, unknown>).format !==
        "tutorhub.excalidraw.portable-scene" ||
      (value as Record<string, unknown>).formatVersion !== 1
    ) {
      throw new Error("snapshot_corrupt");
    }
  } catch {
    throw new SnapshotStoreError("snapshot_corrupt");
  }
}

function sha256(value: Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

function checksumMatches(value: Uint8Array, expected: string): boolean {
  if (!/^[a-f0-9]{64}$/.test(expected)) return false;
  return timingSafeEqual(
    Buffer.from(sha256(value), "hex"),
    Buffer.from(expected, "hex"),
  );
}
