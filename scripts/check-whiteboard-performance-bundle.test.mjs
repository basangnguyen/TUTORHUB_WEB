import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { scanWhiteboardPerformanceBundle } from "./check-whiteboard-performance-bundle.mjs";

test("accepts an isolated lazy whiteboard closure", async () => {
  const directory = await fixture({ entryImportsWhiteboard: false });
  try {
    const result = await scanWhiteboardPerformanceBundle(directory);
    assert.deepEqual(result.issues, []);
    assert.equal(result.whiteboardClosureChunks, 2);
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
});

test("rejects a whiteboard engine pulled into the initial entry", async () => {
  const directory = await fixture({ entryImportsWhiteboard: true });
  try {
    const result = await scanWhiteboardPerformanceBundle(directory);
    assert.match(
      result.issues.join("\n"),
      /initial entry statically imports the whiteboard engine/,
    );
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
});

async function fixture({ entryImportsWhiteboard }) {
  const directory = await mkdtemp(join(tmpdir(), "p514-bundle-"));
  const assets = join(directory, "assets");
  await mkdir(assets);
  await writeFile(
    join(directory, "index.html"),
    '<script type="module" src="/assets/index-fixture.js"></script>',
  );
  await writeFile(
    join(assets, "index-fixture.js"),
    entryImportsWhiteboard
      ? 'import "./LazyWhiteboardEngine-fixture.js";'
      : 'import "./shared-fixture.js";',
  );
  await writeFile(
    join(assets, "LazyWhiteboardEngine-fixture.js"),
    'import "./whiteboard-vendor-fixture.js";',
  );
  await writeFile(join(assets, "shared-fixture.js"), "export const shared=1;");
  await writeFile(
    join(assets, "whiteboard-vendor-fixture.js"),
    "export const whiteboard=1;",
  );
  return directory;
}
