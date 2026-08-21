import type { RuntimeMode } from "./contracts.js";

export type RuntimeLifecycle = "draining" | "running" | "starting" | "stopped";

export type RuntimeReadinessReason =
  "dependency_unavailable" | "draining" | "ready" | "runtime_off";

export interface RuntimeReadinessSnapshot {
  authorityMode: RuntimeMode;
  epoch: number;
  lifecycle: RuntimeLifecycle;
  ready: boolean;
  reason: RuntimeReadinessReason;
}

export interface RuntimeDependencyProbeResult {
  authorityGuard: boolean;
  controlPlane: boolean;
  mode: RuntimeMode;
  persistence: boolean;
}

export interface RuntimeAuthorityProbeResult<T> {
  mode: RuntimeMode;
  value: T;
}

export type CoordinatedProbeOutcome<T> =
  | {
      applied: false;
      reason: "failed" | "stale";
      snapshot: RuntimeReadinessSnapshot;
    }
  | {
      applied: true;
      snapshot: RuntimeReadinessSnapshot;
      value: T;
    };

export type RuntimeReadinessErrorCode = "runtime_not_ready" | "write_disabled";

export class RuntimeReadinessError extends Error {
  constructor(readonly code: RuntimeReadinessErrorCode) {
    super(code);
    this.name = "RuntimeReadinessError";
  }
}

/**
 * Coordinates all readiness-affecting probes through one serialized queue.
 *
 * Every request captures the current epoch. A fail-closed transition, drain,
 * stop, or explicit invalidation advances the epoch, so an older in-flight or
 * queued success cannot make the runtime ready again. Recovery therefore
 * requires a probe requested after the last failure.
 */
export class RuntimeReadinessCoordinator {
  private authorityHealthy = false;
  private authorityMode: RuntimeMode = "off";
  private checkpointHealthy = true;
  private dependenciesHealthy = false;
  private epoch = 0;
  private lifecycle: RuntimeLifecycle = "starting";
  private operation: Promise<void> = Promise.resolve();

  constructor(private readonly assertAuthorityHeld: () => void) {}

  activate(): RuntimeReadinessSnapshot {
    this.advanceEpoch();
    this.lifecycle = "running";
    this.authorityHealthy = false;
    this.dependenciesHealthy = false;
    return this.snapshot();
  }

  beginDrain(): RuntimeReadinessSnapshot {
    this.advanceEpoch();
    this.lifecycle = "draining";
    this.authorityHealthy = false;
    this.dependenciesHealthy = false;
    return this.snapshot();
  }

  markStopped(): RuntimeReadinessSnapshot {
    this.advanceEpoch();
    this.lifecycle = "stopped";
    this.authorityHealthy = false;
    this.dependenciesHealthy = false;
    return this.snapshot();
  }

  failClosed(): RuntimeReadinessSnapshot {
    this.advanceEpoch();
    this.authorityHealthy = false;
    this.dependenciesHealthy = false;
    return this.snapshot();
  }

  markCheckpointFailed(): RuntimeReadinessSnapshot {
    this.advanceEpoch();
    this.checkpointHealthy = false;
    this.authorityHealthy = false;
    this.dependenciesHealthy = false;
    return this.snapshot();
  }

  markCheckpointRecovered(): RuntimeReadinessSnapshot {
    this.checkpointHealthy = true;
    return this.snapshot();
  }

  mode(): RuntimeMode {
    return this.authorityMode;
  }

  readiness(): RuntimeReadinessSnapshot {
    const current = this.snapshot();
    if (!current.ready) return current;
    try {
      this.assertAuthorityHeld();
    } catch {
      return this.failClosed();
    }
    return this.snapshot();
  }

  assertAdmissionReady(options: { write?: boolean } = {}): RuntimeMode {
    const before = this.snapshot();
    if (!before.ready) {
      throw new RuntimeReadinessError("runtime_not_ready");
    }
    try {
      this.assertAuthorityHeld();
    } catch (error) {
      this.failClosed();
      throw error;
    }
    const after = this.snapshot();
    if (!after.ready) {
      throw new RuntimeReadinessError("runtime_not_ready");
    }
    if (options.write === true && after.authorityMode !== "enabled") {
      throw new RuntimeReadinessError("write_disabled");
    }
    return after.authorityMode;
  }

  refreshDependencies(
    probe: () => Promise<RuntimeDependencyProbeResult>,
  ): Promise<CoordinatedProbeOutcome<RuntimeDependencyProbeResult>> {
    return this.enqueueProbe(probe, (value) => {
      const healthy =
        value.authorityGuard && value.controlPlane && value.persistence;
      this.dependenciesHealthy = healthy;
      this.authorityHealthy = value.authorityGuard && value.controlPlane;
      if (value.controlPlane) this.authorityMode = value.mode;
      if (!healthy || value.mode === "off") this.advanceEpoch();
    });
  }

  refreshAuthority<T>(
    probe: () => Promise<RuntimeAuthorityProbeResult<T>>,
  ): Promise<CoordinatedProbeOutcome<RuntimeAuthorityProbeResult<T>>> {
    return this.enqueueProbe(probe, (value) => {
      this.authorityHealthy = true;
      this.authorityMode = value.mode;
      if (value.mode === "off") this.advanceEpoch();
    });
  }

  private enqueueProbe<T>(
    probe: () => Promise<T>,
    apply: (value: T) => void,
  ): Promise<CoordinatedProbeOutcome<T>> {
    const requestedEpoch = this.epoch;
    let resolveOutcome!: (outcome: CoordinatedProbeOutcome<T>) => void;
    const outcome = new Promise<CoordinatedProbeOutcome<T>>(
      (resolve) => (resolveOutcome = resolve),
    );
    const execute = async (): Promise<void> => {
      if (!this.canApply(requestedEpoch)) {
        resolveOutcome({
          applied: false,
          reason: "stale",
          snapshot: this.snapshot(),
        });
        return;
      }
      try {
        const value = await probe();
        if (!this.canApply(requestedEpoch)) {
          resolveOutcome({
            applied: false,
            reason: "stale",
            snapshot: this.snapshot(),
          });
          return;
        }
        apply(value);
        resolveOutcome({ applied: true, snapshot: this.snapshot(), value });
      } catch {
        if (this.canApply(requestedEpoch)) {
          this.advanceEpoch();
          this.authorityHealthy = false;
          this.dependenciesHealthy = false;
          resolveOutcome({
            applied: false,
            reason: "failed",
            snapshot: this.snapshot(),
          });
          return;
        }
        resolveOutcome({
          applied: false,
          reason: "stale",
          snapshot: this.snapshot(),
        });
      }
    };
    const pending = this.operation.then(execute, execute);
    this.operation = pending.catch(() => undefined);
    return outcome;
  }

  private canApply(epoch: number): boolean {
    return this.lifecycle === "running" && epoch === this.epoch;
  }

  private advanceEpoch(): void {
    this.epoch += 1;
  }

  private snapshot(): RuntimeReadinessSnapshot {
    if (this.lifecycle === "draining") {
      return {
        authorityMode: this.authorityMode,
        epoch: this.epoch,
        lifecycle: this.lifecycle,
        ready: false,
        reason: "draining",
      };
    }
    if (
      this.lifecycle !== "running" ||
      !this.authorityHealthy ||
      !this.checkpointHealthy ||
      !this.dependenciesHealthy
    ) {
      return {
        authorityMode: this.authorityMode,
        epoch: this.epoch,
        lifecycle: this.lifecycle,
        ready: false,
        reason: "dependency_unavailable",
      };
    }
    if (this.authorityMode === "off") {
      return {
        authorityMode: this.authorityMode,
        epoch: this.epoch,
        lifecycle: this.lifecycle,
        ready: false,
        reason: "runtime_off",
      };
    }
    return {
      authorityMode: this.authorityMode,
      epoch: this.epoch,
      lifecycle: this.lifecycle,
      ready: true,
      reason: "ready",
    };
  }
}
