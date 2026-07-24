import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { checkCalendarDependencies } from "./check-dependencies.mjs";

async function createPackage(packageJson, source = 'export const clean = "ok";') {
  const directory = await mkdtemp(join(tmpdir(), "tutorhub-calendar-guard-"));
  await mkdir(join(directory, "src"));
  await writeFile(join(directory, "package.json"), JSON.stringify(packageJson), "utf8");
  await writeFile(join(directory, "src", "main.ts"), source, "utf8");
  return directory;
}

test("accepts exact Standard v7 and Temporal pins", async () => {
  const directory = await createPackage({
    dependencies: {
      "@fullcalendar/react": "7.0.1",
      "temporal-polyfill": "1.0.1",
    },
  });
  try {
    assert.deepEqual((await checkCalendarDependencies(directory)).issues, []);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("rejects Premium/resource packages and remote telemetry", async () => {
  const directory = await createPackage(
    {
      dependencies: {
        "@fullcalendar/react": "7.0.1",
        "temporal-polyfill": "1.0.1",
        "@fullcalendar/resource-timeline": "7.0.1",
      },
    },
    'import "https://example.invalid/telemetry.js";',
  );
  try {
    const issues = (await checkCalendarDependencies(directory)).issues.join("\n");
    assert.match(issues, /Premium\/resource dependency/);
    assert.match(issues, /remote asset reference/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
