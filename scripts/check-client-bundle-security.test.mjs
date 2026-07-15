import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { scanClientBundle } from "./check-client-bundle-security.mjs";

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
