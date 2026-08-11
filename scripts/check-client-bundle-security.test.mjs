import assert from "node:assert/strict";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  scanClientBundle,
  scanMediaBundleIsolation,
} from "./check-client-bundle-security.mjs";

test("accepts a client bundle without server credentials", async () => {
  const directory = await mkdtemp(join(tmpdir(), "tutorhub-bundle-clean-"));
  try {
    await writeFile(
      join(directory, "app.js"),
      'console.log("TutorHub");',
      "utf8",
    );
    assert.deepEqual((await scanClientBundle(directory)).issues, []);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("rejects server-only identifiers in a client bundle", async () => {
  const directory = await mkdtemp(join(tmpdir(), "tutorhub-bundle-secret-"));
  try {
    await writeFile(
      join(directory, "app.js"),
      'const name = "LIVEKIT_API_SECRET";',
      "utf8",
    );
    assert.match(
      (await scanClientBundle(directory)).issues.join("\n"),
      /server-only environment variable/,
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("keeps LiveKit out of the app entry and canonical prejoin chunks", async () => {
  const directory = await createMediaBundleFixture({
    entry: 'const deps=["assets/livekit-media.js"]; import"./session.js";',
    prejoin: 'import"./session.js";',
    room: 'import"./livekit-media.js";',
  });
  try {
    assert.deepEqual(await scanMediaBundleIsolation(directory), []);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("rejects a static LiveKit import from the app entry", async () => {
  const directory = await createMediaBundleFixture({
    entry: 'import"./livekit-media.js";',
    prejoin: 'import"./session.js";',
    room: 'import"./livekit-media.js";',
  });
  try {
    assert.match(
      (await scanMediaBundleIsolation(directory)).join("\n"),
      /statically imports the LiveKit media bundle/,
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

async function createMediaBundleFixture({ entry, prejoin, room }) {
  const directory = await mkdtemp(join(tmpdir(), "tutorhub-media-bundle-"));
  const assets = join(directory, "assets");
  await mkdir(assets);
  await Promise.all([
    writeFile(
      join(directory, "index.html"),
      '<script type="module" src="/assets/index-app.js"></script>',
      "utf8",
    ),
    writeFile(join(assets, "index-app.js"), entry, "utf8"),
    writeFile(join(assets, "MediaSpacePages-prejoin.js"), prejoin, "utf8"),
    writeFile(join(assets, "MediaSpaceRoomPage-room.js"), room, "utf8"),
    writeFile(join(assets, "livekit-media.js"), "export {};", "utf8"),
  ]);
  return directory;
}
