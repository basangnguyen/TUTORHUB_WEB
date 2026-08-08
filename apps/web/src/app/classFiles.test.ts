import { describe, expect, it, vi } from "vitest";
import {
  classFileQueryKeys,
  putFileWithProgress,
  sha256Hex,
} from "./classFiles";

describe("class file transfer helpers", () => {
  it("binds query caches to both tenant and class", () => {
    expect(classFileQueryKeys.list("tenant-a", "class-a")).toEqual([
      "class-files",
      "tenant-a",
      "class-a",
    ]);
    expect(classFileQueryKeys.list("tenant-a", "class-b")).not.toEqual(
      classFileQueryKeys.list("tenant-a", "class-a"),
    );
  });

  it("calculates the exact lowercase SHA-256 digest", async () => {
    const file = new File(["abc"], "lesson.txt", { type: "text/plain" });
    await expect(sha256Hex(file)).resolves.toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
  });

  it("fails closed when the provider version selector is missing", async () => {
    const original = globalThis.XMLHttpRequest;
    class MissingVersionRequest {
      status = 200;
      upload = { addEventListener: vi.fn() };
      open = vi.fn();
      setRequestHeader = vi.fn();
      send = vi.fn(() => this.listeners.load?.());
      getResponseHeader = vi.fn(() => null);
      private listeners: Record<string, (() => void) | undefined> = {};
      addEventListener(name: string, listener: () => void) {
        this.listeners[name] = listener;
      }
    }
    vi.stubGlobal("XMLHttpRequest", MissingVersionRequest);

    await expect(
      putFileWithProgress(
        { url: "https://storage.example.test/private", required_headers: {} },
        new File(["safe"], "safe.txt"),
        vi.fn(),
      ),
    ).rejects.toThrow("file_upload_version_missing");

    vi.stubGlobal("XMLHttpRequest", original);
  });
});
