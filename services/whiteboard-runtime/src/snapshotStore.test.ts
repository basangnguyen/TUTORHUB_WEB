import {
  GetObjectCommand,
  HeadBucketCommand,
  PutObjectCommand,
} from "@aws-sdk/client-s3";
import { describe, expect, it } from "vitest";
import {
  B2PortableSnapshotStore,
  SnapshotStoreError,
} from "./snapshotStore.js";

class FakeObjectClient {
  private stored = new Uint8Array();

  async send(command: unknown): Promise<unknown> {
    if (command instanceof HeadBucketCommand) return {};
    if (command instanceof PutObjectCommand) {
      this.stored = new Uint8Array(command.input.Body as Uint8Array);
      return {};
    }
    if (command instanceof GetObjectCommand) {
      return {
        Body: {
          transformToByteArray: async () => this.stored,
        },
      };
    }
    throw new Error("unsupported_command");
  }
}

describe("B2PortableSnapshotStore", () => {
  it("writes an immutable content-addressed object and verifies the read-back", async () => {
    const store = createStore(new FakeObjectClient());
    const bytes = new TextEncoder().encode(
      JSON.stringify({
        format: "tutorhub.excalidraw.portable-scene",
        formatVersion: 1,
      }),
    );

    await store.probe();
    const artifact = await store.put(bytes);

    expect(artifact.objectKey).toMatch(
      /^portable\/v1\/[a-f0-9]{2}\/[a-f0-9]{64}\.json$/,
    );
    expect(artifact.bytes).toEqual(bytes);
  });

  it("rejects non-portable or empty content before contacting B2", async () => {
    const store = createStore(new FakeObjectClient());

    await expect(
      store.put(new TextEncoder().encode('{"format":"wrong"}')),
    ).rejects.toEqual(
      expect.objectContaining<Partial<SnapshotStoreError>>({
        code: "snapshot_corrupt",
      }),
    );
    await expect(store.put(new Uint8Array())).rejects.toEqual(
      expect.objectContaining<Partial<SnapshotStoreError>>({
        code: "snapshot_too_large",
      }),
    );
  });
});

function createStore(client: FakeObjectClient): B2PortableSnapshotStore {
  return new B2PortableSnapshotStore(
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
