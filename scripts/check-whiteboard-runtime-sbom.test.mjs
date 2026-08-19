import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { checkWhiteboardRuntimeSbom } from "./check-whiteboard-runtime-sbom.mjs";

const requiredComponents = [
  { group: "@aws-sdk", name: "client-s3" },
  { group: "@hocuspocus", name: "server" },
  { name: "pg" },
  { name: "yjs" },
].map((component) => ({ ...component, type: "library", version: "test" }));

test("accepts a secret-safe CycloneDX runtime SBOM", async () => {
  await withSbom(
    { bomFormat: "CycloneDX", components: requiredComponents },
    async (path) => {
      const result = await checkWhiteboardRuntimeSbom(path);
      assert.equal(result.components, 4);
    },
  );
});

test("rejects an SBOM with secret markers", async () => {
  await withSbom(
    {
      bomFormat: "CycloneDX",
      components: requiredComponents,
      metadata: { property: "COLLAB_CONTROL_PLANE_TOKEN=not-allowed" },
    },
    async (path) => {
      await assert.rejects(checkWhiteboardRuntimeSbom(path));
    },
  );
});

test("rejects an SBOM missing an exact runtime dependency", async () => {
  await withSbom(
    { bomFormat: "CycloneDX", components: requiredComponents.slice(1) },
    async (path) => {
      await assert.rejects(checkWhiteboardRuntimeSbom(path));
    },
  );
});

async function withSbom(sbom, assertion) {
  const directory = await mkdtemp(join(tmpdir(), "tutorhub-sbom-test-"));
  const path = join(directory, "runtime.cdx.json");
  try {
    await writeFile(path, JSON.stringify(sbom), "utf8");
    await assertion(path);
  } finally {
    await rm(directory, { force: true, recursive: true });
  }
}
