import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { parseEnvFile } from "./run-p507-disposable.mjs";
import {
  P519_RENDER_SERVICES,
  createP519RenderConfiguration,
} from "./p519-render-sync.mjs";

const ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const API_BASE = "https://api.render.com/v1";

async function request(apiKey, path) {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      accept: "application/json",
      authorization: `Bearer ${apiKey}`,
    },
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok) throw new Error(`p519_render_inspect_${response.status}`);
  return response.json();
}

async function exactService(apiKey, name) {
  const query = new URLSearchParams({ limit: "100", name });
  const entries = await request(apiKey, `/services?${query.toString()}`);
  const matches = entries
    .map((entry) => entry.service ?? entry)
    .filter((service) => service.name === name);
  if (matches.length !== 1) throw new Error("p519_render_service_cardinality");
  return matches[0];
}

function safeDeploy(deploy) {
  return {
    commit: deploy.commit?.id ?? deploy.commitId ?? null,
    finishedAt: deploy.finishedAt ?? null,
    id: deploy.id,
    status: deploy.status,
    trigger: deploy.trigger ?? null,
  };
}

const values = parseEnvFile(
  readFileSync(
    resolve(ROOT, process.argv[2] ?? ".env.p5-collab-19-disposable.local"),
    "utf8",
  ),
);
const configuration = createP519RenderConfiguration(values);
const result = {};
for (const [key, expected] of Object.entries(P519_RENDER_SERVICES)) {
  const service = await exactService(configuration.apiKey, expected.name);
  const deployEntries = await request(
    configuration.apiKey,
    `/services/${encodeURIComponent(service.id)}/deploys?limit=3`,
  );
  result[key] = {
    autoDeploy: service.autoDeploy,
    branch: service.branch,
    id: service.id,
    name: service.name,
    recentDeploys: deployEntries.map((entry) =>
      safeDeploy(entry.deploy ?? entry),
    ),
    region: service.region ?? service.serviceDetails?.region ?? null,
    type: service.type,
  };
}
process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
