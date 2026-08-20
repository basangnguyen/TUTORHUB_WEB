import { pathToFileURL } from "node:url";
import { NeonCheckpointStore } from "./checkpointStore.js";
import { loadRuntimeConfig, RuntimeConfigurationError } from "./config.js";
import { HttpControlPlane } from "./controlPlane.js";
import { RUNTIME_VERSIONS } from "./contracts.js";
import { createCollaborationRuntime } from "./runtimeServer.js";
import { B2PortableSnapshotStore } from "./snapshotStore.js";
import { JsonSafeLogger } from "./telemetry.js";

export async function run(): Promise<void> {
  if (process.versions.node !== RUNTIME_VERSIONS.node) {
    throw new Error("runtime_node_version_mismatch");
  }
  const config = loadRuntimeConfig();
  const checkpoints = new NeonCheckpointStore(config.databaseUrl);
  const controlPlane = new HttpControlPlane(
    config.controlPlaneUrl,
    config.controlPlaneToken,
    config.probeTimeoutMs,
  );
  const snapshots = new B2PortableSnapshotStore(config.b2.bucket, config.b2);
  const logger = new JsonSafeLogger();
  const runtime = createCollaborationRuntime(config, {
    checkpoints,
    controlPlane,
    logger,
    snapshots,
  });
  await runtime.start();

  let shuttingDown = false;
  const shutdown = async (): Promise<void> => {
    if (shuttingDown) return;
    shuttingDown = true;
    try {
      await runtime.drain();
      process.exitCode = 0;
    } catch {
      process.exitCode = 1;
    }
  };
  process.once("SIGINT", () => void shutdown());
  process.once("SIGTERM", () => void shutdown());
}

export function startupFailureCode(error: unknown): string {
  if (error instanceof RuntimeConfigurationError) return error.code;
  if (
    error instanceof Error &&
    error.message === "runtime_node_version_mismatch"
  ) {
    return "runtime_node_version_mismatch";
  }
  return "runtime_start_unknown";
}

const entrypoint = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === entrypoint) {
  run().catch((error: unknown) => {
    process.stderr.write(
      `${JSON.stringify({
        event_code: "runtime_start_failed",
        outcome: "failed",
        reason_code: startupFailureCode(error),
      })}\n`,
    );
    process.exitCode = 1;
  });
}
