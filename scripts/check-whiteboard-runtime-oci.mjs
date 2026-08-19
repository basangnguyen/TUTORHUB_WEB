import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const repositoryRoot = fileURLToPath(new URL("../", import.meta.url));
const dockerfilePath = new URL(
  "../services/whiteboard-runtime/Dockerfile",
  import.meta.url,
);
const packagePath = new URL(
  "../services/whiteboard-runtime/package.json",
  import.meta.url,
);
const mainPath = new URL(
  "../services/whiteboard-runtime/src/main.ts",
  import.meta.url,
);

export async function checkWhiteboardRuntimeOci() {
  const [dockerfile, packageText, main] = await Promise.all([
    readFile(dockerfilePath, "utf8"),
    readFile(packagePath, "utf8"),
    readFile(mainPath, "utf8"),
  ]);
  const manifest = JSON.parse(packageText);
  const expectedBase =
    "node:24.15.0-alpine3.23@sha256:d1b3b4da11eefd5941e7f0b9cf17783fc99d9c6fc34884a665f40a06dbdfc94f";

  assert.match(
    dockerfile,
    new RegExp(`ARG NODE_IMAGE=${escapeRegExp(expectedBase)}`),
  );
  assert.equal((dockerfile.match(/FROM \$\{NODE_IMAGE\}/g) ?? []).length, 2);
  assert.doesNotMatch(dockerfile, /(?:^|[:@])latest(?:\s|$)/im);
  assert.match(dockerfile, /pnpm install --frozen-lockfile/);
  assert.match(dockerfile, /pnpm@11\.7\.0/);
  assert.match(
    dockerfile,
    /--config\.inject-workspace-packages=true deploy --prod \/runtime/,
  );
  assert.doesNotMatch(dockerfile, /deploy --prod --legacy/);
  assert.match(dockerfile, /USER node/);
  assert.match(dockerfile, /HEALTHCHECK[\s\S]*\/readyz/);
  assert.match(dockerfile, /CMD \["node", "dist\/main\.js"\]/);
  assert.match(dockerfile, /libcrypto3>=3\.5\.7-r0/);
  assert.match(dockerfile, /libssl3>=3\.5\.7-r0/);
  for (const buildOnlyTool of ["npm", "npx", "corepack", "pnpm", "pnpx"]) {
    assert.match(
      dockerfile,
      new RegExp(`/usr/local/bin/${buildOnlyTool}(?:\\s|$)`),
    );
  }

  assert.equal(manifest.engines.node, "24.15.0");
  assert.deepEqual(manifest.files, ["dist"]);
  assert.deepEqual(manifest.dependencies, {
    "@aws-sdk/client-s3": "3.1113.0",
    "@hocuspocus/server": "4.6.0",
    pg: "8.23.0",
    yjs: "13.6.27",
  });
  for (const forbidden of ["@excalidraw/excalidraw", "react", "tldraw"]) {
    assert.equal(manifest.dependencies[forbidden], undefined);
  }
  assert.match(main, /process\.versions\.node !== RUNTIME_VERSIONS\.node/);
  assert.doesNotMatch(
    main,
    /DATABASE_POOL_URL|DATABASE_MIGRATION_URL|console\.(?:error|log)/,
  );

  return {
    baseImage: expectedBase,
    repositoryRoot,
    runtimeDependencies: Object.keys(manifest.dependencies).length,
  };
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  checkWhiteboardRuntimeOci()
    .then((result) => {
      process.stdout.write(
        `Whiteboard runtime OCI static gate passed (${result.runtimeDependencies} runtime dependencies).\n`,
      );
    })
    .catch(() => {
      process.stderr.write("Whiteboard runtime OCI static gate failed.\n");
      process.exitCode = 1;
    });
}
