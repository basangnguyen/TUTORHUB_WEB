import { randomBytes } from "node:crypto";
import {
  CanonicalExcalidrawAuthority,
  importPortableScene,
  type CanonicalAuthorityScope,
} from "@tutorhub/collaboration-client";
import * as Y from "yjs";
import {
  ArtifactEnvelopeError,
  createArtifactEnvelope,
  verifyArtifactEnvelope,
  type ArtifactBindingKey,
} from "./artifactEnvelope.js";
import {
  ArtifactObjectStoreError,
  type ArtifactObjectBinding,
  type ArtifactObjectExpectation,
} from "./artifactObjectStore.js";
import {
  ArtifactQueueError,
  type ArtifactJob,
  type ArtifactQueuePort,
} from "./artifactQueue.js";

export interface ArtifactObjectStorePort {
  deleteVersion(binding: ArtifactObjectBinding): Promise<void>;
  getVerified(expectation: ArtifactObjectExpectation): Promise<Uint8Array>;
  putVerified(
    bytes: Uint8Array,
    contentSha256: string,
    verificationKeyId: string,
  ): Promise<ArtifactObjectBinding>;
}

export interface ArtifactWorkerLogger {
  event(event: {
    event_code: string;
    outcome: "failed" | "succeeded";
    reason_code?: string;
  }): void;
}

export class ArtifactWorkerError extends Error {
  constructor(readonly code: string) {
    super(code);
    this.name = "ArtifactWorkerError";
  }
}

export class WhiteboardArtifactWorker {
  private timer: ReturnType<typeof setTimeout> | undefined;
  private stopping = false;
  private running: Promise<void> | undefined;
  private readonly verificationKeys: ReadonlyMap<string, string>;
  private readonly workerId: string;

  constructor(
    private readonly queue: ArtifactQueuePort & {
      close?: () => Promise<void>;
      probe?: () => Promise<void>;
    },
    private readonly objects: ArtifactObjectStorePort,
    private readonly bindingKey: ArtifactBindingKey,
    previousBindingKey: ArtifactBindingKey | undefined,
    private readonly leaseSeconds: number,
    private readonly pollIntervalMs: number,
    private readonly logger: ArtifactWorkerLogger,
  ) {
    this.workerId = `artifact-${randomBytes(12).toString("hex")}`;
    this.verificationKeys = new Map(
      [bindingKey, previousBindingKey]
        .filter((key): key is ArtifactBindingKey => key !== undefined)
        .map((key) => [key.id, key.secret]),
    );
  }

  async start(): Promise<void> {
    if (this.stopping || this.running) return;
    await this.queue.probe?.();
    this.schedule(0);
  }

  async stop(): Promise<void> {
    this.stopping = true;
    if (this.timer) clearTimeout(this.timer);
    await this.running;
    await this.queue.close?.();
  }

  async runOnce(): Promise<boolean> {
    const job = await this.queue.claim(this.workerId, this.leaseSeconds);
    if (!job) return false;
    try {
      if (job.kind === "snapshot" || job.kind === "export") {
        await this.createSnapshot(job);
      } else if (job.kind === "restore") {
        await this.restore(job);
      } else {
        throw new ArtifactWorkerError("artifact_import_unbound");
      }
      this.logger.event({
        event_code: "artifact_command",
        outcome: "succeeded",
      });
    } catch (error) {
      const classified = classifyWorkerError(error);
      try {
        await this.queue.fail(job, classified.code, classified.disposition);
      } catch {
        // A lost lease owns the final state; never log the raw database error.
      }
      this.logger.event({
        event_code: "artifact_command",
        outcome: "failed",
        reason_code: classified.code,
      });
    }
    return true;
  }

  private schedule(delay: number): void {
    if (this.stopping) return;
    this.timer = setTimeout(() => {
      this.running = this.runOnce()
        .then((worked) => this.schedule(worked ? 0 : this.pollIntervalMs))
        .catch(() => {
          this.logger.event({
            event_code: "artifact_worker_poll",
            outcome: "failed",
            reason_code: "artifact_queue_unavailable",
          });
          this.schedule(this.pollIntervalMs);
        })
        .finally(() => {
          this.running = undefined;
        });
    }, delay);
    this.timer.unref?.();
  }

  private async createSnapshot(job: ArtifactJob): Promise<void> {
    if (job.currentGeneration !== job.generation) {
      throw new ArtifactWorkerError("artifact_generation_stale");
    }
    const scope = sourceScope(job);
    const state = await this.queue.loadCheckpoint(job);
    const envelope = createArtifactEnvelope(scope, state, this.bindingKey);
    const binding = await this.objects.putVerified(
      envelope.bytes,
      envelope.contentSha256,
      this.bindingKey.id,
    );
    try {
      await this.queue.publishSnapshot(job, {
        causalWatermarkSha256: envelope.causalWatermarkSha256,
        contentSha256: envelope.contentSha256,
        objectKey: binding.objectKey,
        objectVersionId: binding.objectVersionId,
        sizeBytes: envelope.bytes.byteLength,
        verificationKeyId: this.bindingKey.id,
      });
    } catch (error) {
      try {
        await this.objects.deleteVersion(binding);
      } catch {
        // Exact orphan cleanup is retried by provider lifecycle tooling.
      }
      throw error;
    }
  }

  private async restore(job: ArtifactJob): Promise<void> {
    if (
      job.currentGeneration !== job.generation ||
      job.targetGeneration !== job.generation + 1 ||
      !job.targetProviderDocumentName ||
      !job.source
    ) {
      throw new ArtifactWorkerError("artifact_restore_binding_invalid");
    }
    const bytes = await this.objects.getVerified(job.source);
    const verified = verifyArtifactEnvelope(
      bytes,
      {
        documentId: job.documentId,
        generation: job.source.generation,
        tenantId: job.tenantId,
      },
      this.verificationKeys,
    );
    const document = new Y.Doc();
    try {
      const authority = new CanonicalExcalidrawAuthority(
        document,
        {
          documentId: job.documentId,
          generation: job.targetGeneration,
          tenantId: job.tenantId,
        },
        "artifact-restore",
      );
      authority.initialize(verified.scene);
      await this.queue.completeRestore(job, authority.encodeProviderState());
    } finally {
      document.destroy();
    }
  }
}

export function validatePortableImport(bytes: Uint8Array): void {
  importPortableScene(bytes);
}

function sourceScope(job: ArtifactJob): CanonicalAuthorityScope {
  return {
    documentId: job.documentId,
    generation: job.generation,
    tenantId: job.tenantId,
  };
}

function classifyWorkerError(error: unknown): {
  code: string;
  disposition: "failed" | "quarantined" | "retryable";
} {
  if (error instanceof ArtifactEnvelopeError) {
    return { code: error.code, disposition: "quarantined" };
  }
  if (error instanceof ArtifactObjectStoreError) {
    return {
      code: error.code,
      disposition:
        error.code === "artifact_object_unavailable"
          ? "retryable"
          : "quarantined",
    };
  }
  if (error instanceof ArtifactQueueError) {
    return {
      code: error.code,
      disposition:
        error.code === "artifact_checkpoint_corrupt"
          ? "quarantined"
          : error.code === "artifact_queue_fence_lost"
            ? "failed"
            : "retryable",
    };
  }
  if (error instanceof ArtifactWorkerError) {
    return { code: error.code, disposition: "failed" };
  }
  return { code: "artifact_worker_failed", disposition: "failed" };
}
