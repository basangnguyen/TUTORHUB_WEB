import { pathToFileURL } from "node:url";
import { B2ArtifactObjectStore } from "./artifactObjectStore.js";
import { ArtifactQueueError, PostgresArtifactQueue } from "./artifactQueue.js";
import {
  ArtifactWorkerError,
  WhiteboardArtifactWorker,
} from "./artifactWorker.js";
import { NeonCheckpointStore } from "./checkpointStore.js";
import { loadRuntimeConfig, RuntimeConfigurationError } from "./config.js";
import { HttpControlPlane } from "./controlPlane.js";
import { RUNTIME_VERSIONS } from "./contracts.js";
import {
  PostgresProviderAuthorityGuard,
  ProviderAuthorityGuardError,
} from "./providerAuthorityGuard.js";
import { createCollaborationRuntime } from "./runtimeServer.js";
import { B2PortableSnapshotStore } from "./snapshotStore.js";
import { JsonSafeLogger } from "./telemetry.js";

export async function run(): Promise<void> {
  if (process.versions.node !== RUNTIME_VERSIONS.node) {
    throw new Error("runtime_node_version_mismatch");
  }
  const config = loadRuntimeConfig();
  const checkpoints = new NeonCheckpointStore(config.databaseUrl);
  const authorityGuard = new PostgresProviderAuthorityGuard(config.databaseUrl);
  const controlPlane = new HttpControlPlane(
    config.controlPlaneUrl,
    config.controlPlaneToken,
    config.probeTimeoutMs,
  );
  const snapshots = new B2PortableSnapshotStore(config.b2.bucket, config.b2);
  const logger = new JsonSafeLogger();
  const artifactQueue = config.artifactWorker.enabled
    ? new PostgresArtifactQueue(config.databaseUrl)
    : undefined;
  const artifactWorker = artifactQueue
    ? new WhiteboardArtifactWorker(
        artifactQueue,
        new B2ArtifactObjectStore(config.b2.bucket, config.b2),
        {
          id: config.artifactWorker.currentBindingKeyId,
          secret: config.artifactWorker.currentBindingKey,
        },
        config.artifactWorker.previousBindingKeyId
          ? {
              id: config.artifactWorker.previousBindingKeyId,
              secret: config.artifactWorker.previousBindingKey,
            }
          : undefined,
        config.artifactWorker.leaseSeconds,
        config.artifactWorker.pollIntervalMs,
        {
          event(event) {
            process.stdout.write(
              `${JSON.stringify({ ...event, timestamp: new Date().toISOString() })}\n`,
            );
          },
        },
      )
    : undefined;
  const runtime = createCollaborationRuntime(config, {
    authorityGuard,
    checkpoints,
    controlPlane,
    logger,
    snapshots,
  });
  await artifactWorker?.start();
  try {
    await runtime.start();
  } catch (error) {
    await artifactWorker?.stop().catch(() => undefined);
    throw error;
  }

  let shuttingDown = false;
  const shutdown = async (): Promise<void> => {
    if (shuttingDown) return;
    shuttingDown = true;
    try {
      await artifactWorker?.stop();
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
  if (error instanceof ProviderAuthorityGuardError) return error.code;
  if (error instanceof ArtifactQueueError) return error.code;
  if (error instanceof ArtifactWorkerError) return error.code;
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
