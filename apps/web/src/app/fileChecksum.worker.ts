/// <reference lib="webworker" />

self.addEventListener("message", async (event: MessageEvent<File>) => {
  try {
    const contents = await event.data.arrayBuffer();
    const digest = await crypto.subtle.digest("SHA-256", contents);
    const checksum = Array.from(new Uint8Array(digest), (byte) =>
      byte.toString(16).padStart(2, "0"),
    ).join("");
    self.postMessage({ checksum });
  } catch {
    self.postMessage({ error: "file_checksum_failed" });
  }
});

export {};
