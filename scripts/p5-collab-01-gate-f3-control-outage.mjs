import { validateGateF3Environment } from "./require-p5-collab-01-gate-f3-confirm.mjs";

const EXACT_OUTAGE_CONFIRMATION =
  "I_UNDERSTAND_P5_F3_CONTROL_WILL_BE_UNAVAILABLE_FOR_600_SECONDS";

async function run() {
  const env = process.env;
  const validated = validateGateF3Environment(env);
  if (env.P5_F3_OUTAGE_TARGET !== "control") {
    throw new Error("gate_f3_outage_target_invalid");
  }
  if (env.P5_F3_OUTAGE_SECONDS !== "600") {
    throw new Error("gate_f3_outage_duration_invalid");
  }
  if (env.P5_F3_OUTAGE_CONFIRM !== EXACT_OUTAGE_CONFIRMATION) {
    throw new Error("gate_f3_outage_confirmation_missing");
  }

  let outageEnabled = false;
  try {
    await setControlMode(validated.controlUrl, "unavailable");
    outageEnabled = true;
    await waitForStatus(`${validated.runtimeUrl}/readyz`, 503, 20_000);
    for (let elapsed = 60; elapsed <= 600; elapsed += 60) {
      await delay(60_000);
      const response = await fetch(`${validated.runtimeUrl}/readyz`, {
        signal: AbortSignal.timeout(5_000),
      });
      if (response.status !== 503) {
        throw new Error("gate_f3_runtime_failed_open_during_control_outage");
      }
      console.log(
        JSON.stringify({
          elapsed_seconds: elapsed,
          fail_closed: true,
          target: "control",
        }),
      );
    }
  } finally {
    if (outageEnabled) {
      await setControlMode(validated.controlUrl, "enabled");
      await waitForStatus(`${validated.runtimeUrl}/readyz`, 200, 20_000);
    }
  }
  console.log(
    JSON.stringify({
      duration_seconds: 600,
      fail_closed: true,
      outcome: "pass",
      recovered: true,
      target: "control",
    }),
  );
}

async function setControlMode(baseUrl, mode) {
  const response = await fetch(`${baseUrl}/gate-f3/v1/state`, {
    body: JSON.stringify({ mode }),
    headers: {
      authorization: `Bearer ${process.env.P5_F3_CONTROL_ADMIN_TOKEN}`,
      "content-type": "application/json",
    },
    method: "PUT",
    signal: AbortSignal.timeout(5_000),
  });
  if (!response.ok) throw new Error("gate_f3_control_mode_update_failed");
}

async function waitForStatus(url, expected, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(5_000) });
      if (response.status === expected) return;
    } catch {
      // A sleeping disposable service can briefly refuse connections.
    }
    await delay(500);
  }
  throw new Error("gate_f3_readiness_transition_timeout");
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

run().catch(() => {
  console.error(
    JSON.stringify({ outcome: "fail", reason: "bounded_outage_gate_failure" }),
  );
  process.exitCode = 1;
});
