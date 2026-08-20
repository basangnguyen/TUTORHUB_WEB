import { pathToFileURL } from "node:url";
import { validateGateF3Environment } from "./require-p5-collab-01-gate-f3-confirm.mjs";
import { NeonCheckpointStore } from "../services/whiteboard-runtime/dist/checkpointStore.js";

const EXACT_OUTAGE_CONFIRMATION =
  "I_UNDERSTAND_P5_F3_B2_KEY_WILL_REMAIN_UNAVAILABLE_FOR_600_SECONDS";
const B2_AUTHORIZE_URL =
  "https://api.backblazeb2.com/b2api/v4/b2_authorize_account";
const DOCUMENT_NAME = "wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1";
const SCOPE = {
  actorId: "gate-f3-teacher",
  capability: "edit",
  documentId: "gate-f3-document",
  generation: 1,
  providerDocumentName: DOCUMENT_NAME,
  sessionId: "gate-f3-session",
  tenantId: "gate-f3-tenant",
  writerFence: 1,
};
let boundedFailureStage = "startup";

export function validateB2OutageEnvironment(env = process.env) {
  const validated = validateGateF3Environment(env);
  if (env.P5_F3_OUTAGE_TARGET !== "b2") {
    throw new Error("gate_f3_b2_outage_target_invalid");
  }
  if (env.P5_F3_OUTAGE_SECONDS !== "600") {
    throw new Error("gate_f3_b2_outage_duration_invalid");
  }
  if (env.P5_F3_OUTAGE_CONFIRM !== EXACT_OUTAGE_CONFIRMATION) {
    throw new Error("gate_f3_b2_outage_confirmation_missing");
  }
  if (env.P5_F3_PROVIDER_DOCUMENT_NAME !== DOCUMENT_NAME) {
    throw new Error("gate_f3_b2_document_name_mismatch");
  }
  return validated;
}

export async function runGateF3B2Outage(env = process.env) {
  boundedFailureStage = "validate";
  const validated = validateB2OutageEnvironment(env);
  const checkpoints = new NeonCheckpointStore(
    validated.runtimeDatabase.toString(),
  );

  try {
    boundedFailureStage = "runtime_liveness_preflight";
    await waitForStatus(`${validated.runtimeUrl}/livez`, 200, 120_000);
    writeStage("runtime_liveness_preflight");
    boundedFailureStage = "runtime_readiness_preflight";
    await waitForStatus(`${validated.runtimeUrl}/readyz`, 200, 120_000);
    writeStage("runtime_readiness_preflight");
    boundedFailureStage = "b2_fail_closed_preflight";
    if (!(await b2CredentialIsRevoked(env))) {
      throw new Error("gate_f3_b2_failed_open");
    }
    writeStage("b2_fail_closed_preflight");
    boundedFailureStage = "neon_checkpoint_preflight";
    if (!(await checkpointExists(checkpoints))) {
      throw new Error("gate_f3_b2_neon_checkpoint_missing");
    }
    writeStage("neon_checkpoint_preflight");

    for (let elapsed = 60; elapsed <= 600; elapsed += 60) {
      boundedFailureStage = `hold_${elapsed}`;
      await delay(60_000);
      await assertHttpStatus(`${validated.runtimeUrl}/livez`, 200);
      await assertHttpStatus(`${validated.runtimeUrl}/readyz`, 200);
      if (!(await b2CredentialIsRevoked(env))) {
        throw new Error("gate_f3_b2_recovered_before_window_closed");
      }
      if (!(await checkpointExists(checkpoints))) {
        throw new Error("gate_f3_b2_neon_checkpoint_lost");
      }
      console.log(
        JSON.stringify({
          b2_fail_closed: true,
          elapsed_seconds: elapsed,
          liveness_preserved: true,
          neon_checkpoint_preserved: true,
          target: "b2",
        }),
      );
    }
  } finally {
    await checkpoints.close().catch(() => undefined);
  }

  boundedFailureStage = "complete";
  console.log(
    JSON.stringify({
      b2_fail_closed: true,
      duration_seconds: 600,
      liveness_preserved: true,
      neon_checkpoint_preserved: true,
      outcome: "pass",
      target: "b2",
    }),
  );
}

async function checkpointExists(checkpoints) {
  try {
    return (await checkpoints.load(SCOPE)) !== null;
  } catch {
    return false;
  }
}

export async function b2CredentialIsRevoked(
  env = process.env,
  fetchImpl = fetch,
) {
  const encodedCredential = Buffer.from(
    `${env.B2_KEY_ID}:${env.B2_APPLICATION_KEY}`,
  ).toString("base64");
  const response = await fetchImpl(B2_AUTHORIZE_URL, {
    headers: { authorization: `Basic ${encodedCredential}` },
    signal: AbortSignal.timeout(15_000),
  });
  if (response.status === 401) return true;
  if (response.status === 200) return false;
  throw new Error("gate_f3_b2_authorize_unexpected_status");
}

async function assertHttpStatus(url, expected) {
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
    if (response.status !== expected) throw new Error("unexpected_status");
  } catch {
    throw new Error("gate_f3_b2_http_probe_failed");
  }
}

async function waitForStatus(url, expected, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
      if (response.status === expected) return;
    } catch {
      // Render Free can briefly refuse connections while waking.
    }
    await delay(500);
  }
  throw new Error("gate_f3_b2_readiness_preflight_timeout");
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function writeStage(stage) {
  console.log(JSON.stringify({ outcome: "pass", stage }));
}

const entrypoint = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === entrypoint) {
  runGateF3B2Outage().catch(() => {
    console.error(
      JSON.stringify({
        outcome: "fail",
        reason: "bounded_b2_outage_gate_failure",
        stage: boundedFailureStage,
      }),
    );
    process.exitCode = 1;
  });
}
