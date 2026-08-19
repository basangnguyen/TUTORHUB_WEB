import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

export async function checkWhiteboardRuntimeSbom(path) {
  const text = await readFile(path, "utf8");
  assert.ok(text.length > 100, "sbom_empty");
  assert.doesNotMatch(
    text,
    /postgres(?:ql)?:\/\/|B2_APPLICATION_KEY|COLLAB_CONTROL_PLANE_TOKEN|COLLAB_METRICS_TOKEN/i,
    "sbom_contains_secret_material",
  );
  const sbom = JSON.parse(text);
  assert.equal(sbom.bomFormat, "CycloneDX");
  assert.ok(Array.isArray(sbom.components), "sbom_components_missing");
  const names = new Set(sbom.components.map((component) => component?.name));
  for (const dependency of [
    "@aws-sdk/client-s3",
    "@hocuspocus/server",
    "pg",
    "yjs",
  ]) {
    assert.ok(names.has(dependency), `sbom_dependency_missing:${dependency}`);
  }
  return { components: sbom.components.length };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const path = process.argv[2];
  if (!path) {
    process.stderr.write("Whiteboard runtime SBOM path is required.\n");
    process.exitCode = 1;
  } else {
    checkWhiteboardRuntimeSbom(path)
      .then(({ components }) => {
        process.stdout.write(
          `Whiteboard runtime SBOM gate passed (${components} components).\n`,
        );
      })
      .catch(() => {
        process.stderr.write("Whiteboard runtime SBOM gate failed.\n");
        process.exitCode = 1;
      });
  }
}
