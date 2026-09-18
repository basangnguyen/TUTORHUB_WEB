import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

import { createP520ProviderFixture } from "../../scripts/p520-provider-fixture.mjs";
import { P520_RAMP_EXIT_CONTRACT } from "../../scripts/p520-ramp-exit-contract.mjs";
import { runP519ProviderSoak } from "./p519-provider-soak.mjs";

const ROOT = resolve(new URL("../..", import.meta.url).pathname.slice(1));
const MANIFEST_FILE = "tmp/p5-collab-20/tenants.json";
const BINDING_FILE = "tmp/p5-collab-20/run-binding.json";
const DEPLOY_STATE_FILE = "tmp/p5-collab-20/render-deploy.json";
const OUTPUT_FILE = "tmp/p5-collab-20/provider-report.json";
const MAX_INPUT_BYTES = 64 * 1024;
const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_COLLAB_20_R3_DISPOSABLE_ONLY";

function readBoundedJson(relativePath) {
  const raw = readFileSync(resolve(ROOT, relativePath));
  if (raw.byteLength === 0 || raw.byteLength > MAX_INPUT_BYTES) {
    throw new Error("p520_soak_private_input_size_invalid");
  }
  try {
    return JSON.parse(raw.toString("utf8"));
  } catch {
    throw new Error("p520_soak_private_input_json_invalid");
  }
}

export async function runP520ProviderSoak(environment = process.env) {
  const fixture = createP520ProviderFixture(readBoundedJson(MANIFEST_FILE));
  return runP519ProviderSoak(environment, {
    bindingFile: BINDING_FILE,
    deployStateFile: DEPLOY_STATE_FILE,
    outputFile: OUTPUT_FILE,
    outputFileFromArgv: false,
    providerFixture: fixture,
    artifactSampleCount:
      P520_RAMP_EXIT_CONTRACT.providerEvidence.artifactSampleCount,
  });
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) {
  const authorized =
    process.argv.length === 4 &&
    process.argv[2] === "--confirm" &&
    process.argv[3] === EXACT_CONFIRMATION;
  const operation = authorized
    ? runP520ProviderSoak()
    : Promise.reject(new Error("p520_soak_exact_confirmation_required"));
  operation
    .then((result) => process.stdout.write(`${JSON.stringify(result)}\n`))
    .catch((error) => {
      const reason =
        error instanceof Error && /^p519_[a-z0-9_]+$/u.test(error.message)
          ? error.message.replace(/^p519_/u, "p520_")
          : "p520_provider_soak_bounded_failure";
      process.stderr.write(`${JSON.stringify({ outcome: "fail", reason })}\n`);
      process.exitCode = 1;
    });
}
