import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { checkP519RenderCandidateText } from "./check-p519-render-candidate.mjs";

const blueprintPath = new URL(
  "../infrastructure/render/p5-collab-19-private-alpha.render.yaml",
  import.meta.url,
);

test("accepts the isolated P519 free-tier candidate", async () => {
  const text = await readFile(blueprintPath, "utf8");
  assert.deepEqual(checkP519RenderCandidateText(text), {
    secretCount: 11,
    serviceCount: 2,
  });
});

test("rejects a plaintext dashboard secret", async () => {
  const text = await readFile(blueprintPath, "utf8");
  assert.throws(
    () =>
      checkP519RenderCandidateText(
        text.replace(
          "      - key: COLLAB_METRICS_TOKEN\n        sync: false",
          "      - key: COLLAB_METRICS_TOKEN\n        value: unsafe",
        ),
      ),
    /dashboard-supplied/u,
  );
});

test("rejects paid-tier-only shutdown configuration", async () => {
  const text = await readFile(blueprintPath, "utf8");
  assert.throws(
    () =>
      checkP519RenderCandidateText(
        text.replace(
          "    healthCheckPath: /livez",
          "    healthCheckPath: /livez\n    maxShutdownDelaySeconds: 30",
        ),
      ),
    /maxShutdownDelaySeconds/u,
  );
});
