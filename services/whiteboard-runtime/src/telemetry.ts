import type {
  CollaborationCapability,
  RuntimeDependencyCode,
  RuntimeEventCode,
  SafeLogger,
} from "./contracts.js";

type ConnectionOutcome = "accepted" | "closed" | "rejected";
type CheckpointOutcome = "failed" | "loaded" | "stored";

export class RuntimeTelemetry {
  private readonly checkpointTotals = new Map<CheckpointOutcome, number>();
  private readonly connectionCurrent = new Map<
    CollaborationCapability,
    number
  >();
  private readonly connectionTotals = new Map<ConnectionOutcome, number>();
  private readonly dependencies = new Map<RuntimeDependencyCode, 0 | 1>();
  private dirtyDocuments = 0;
  private documents = 0;
  private draining = 0;

  constructor(private readonly buildId: string) {
    for (const capability of ["edit", "present", "view"] as const) {
      this.connectionCurrent.set(capability, 0);
    }
    for (const outcome of ["accepted", "closed", "rejected"] as const) {
      this.connectionTotals.set(outcome, 0);
    }
    for (const outcome of ["failed", "loaded", "stored"] as const) {
      this.checkpointTotals.set(outcome, 0);
    }
    for (const dependency of [
      "control_plane",
      "persistence",
      "snapshot",
    ] as const) {
      this.dependencies.set(dependency, 0);
    }
  }

  checkpoint(outcome: CheckpointOutcome): void {
    this.checkpointTotals.set(
      outcome,
      (this.checkpointTotals.get(outcome) ?? 0) + 1,
    );
  }

  connection(
    capability: CollaborationCapability,
    outcome: ConnectionOutcome,
  ): void {
    this.connectionTotals.set(
      outcome,
      (this.connectionTotals.get(outcome) ?? 0) + 1,
    );
    const current = this.connectionCurrent.get(capability) ?? 0;
    if (outcome === "accepted")
      this.connectionCurrent.set(capability, current + 1);
    if (outcome === "closed")
      this.connectionCurrent.set(capability, Math.max(0, current - 1));
  }

  dependency(code: RuntimeDependencyCode, up: boolean): void {
    this.dependencies.set(code, up ? 1 : 0);
  }

  setDirtyDocuments(value: number): void {
    this.dirtyDocuments = Math.max(0, value);
  }

  setDocuments(value: number): void {
    this.documents = Math.max(0, value);
  }

  setDraining(value: boolean): void {
    this.draining = value ? 1 : 0;
  }

  render(): string {
    const lines = [
      "# HELP collab_runtime_build_info Immutable runtime build information.",
      "# TYPE collab_runtime_build_info gauge",
      `collab_runtime_build_info{build_id="${escapeLabel(this.buildId)}"} 1`,
      "# HELP collab_connections_current Current authenticated connections.",
      "# TYPE collab_connections_current gauge",
    ];
    for (const [capability, value] of this.connectionCurrent) {
      lines.push(
        `collab_connections_current{capability="${capability}"} ${value}`,
      );
    }
    lines.push(
      "# HELP collab_connection_total Bounded connection outcomes.",
      "# TYPE collab_connection_total counter",
    );
    for (const [outcome, value] of this.connectionTotals) {
      lines.push(`collab_connection_total{outcome="${outcome}"} ${value}`);
    }
    lines.push(
      "# HELP collab_checkpoint_total Bounded checkpoint outcomes.",
      "# TYPE collab_checkpoint_total counter",
    );
    for (const [outcome, value] of this.checkpointTotals) {
      lines.push(`collab_checkpoint_total{outcome="${outcome}"} ${value}`);
    }
    lines.push(
      "# HELP collab_dependency_up Dependency readiness by fixed code.",
      "# TYPE collab_dependency_up gauge",
    );
    for (const [dependency, value] of this.dependencies) {
      lines.push(`collab_dependency_up{dependency="${dependency}"} ${value}`);
    }
    lines.push(
      "# TYPE collab_documents_current gauge",
      `collab_documents_current ${this.documents}`,
      "# TYPE collab_dirty_documents gauge",
      `collab_dirty_documents ${this.dirtyDocuments}`,
      "# TYPE collab_drain_active gauge",
      `collab_drain_active ${this.draining}`,
      "",
    );
    return lines.join("\n");
  }
}

export class JsonSafeLogger implements SafeLogger {
  event(
    code: RuntimeEventCode,
    outcome: "denied" | "failed" | "ok" | "started",
    durationMs?: number,
  ): void {
    const payload: Record<string, string> = {
      event_code: code,
      outcome,
      timestamp: new Date().toISOString(),
    };
    if (durationMs !== undefined)
      payload.duration_bucket = durationBucket(durationMs);
    process.stdout.write(`${JSON.stringify(payload)}\n`);
  }
}

function durationBucket(durationMs: number): string {
  if (durationMs < 100) return "lt_100ms";
  if (durationMs < 1_000) return "lt_1s";
  if (durationMs < 5_000) return "lt_5s";
  if (durationMs < 30_000) return "lt_30s";
  return "gte_30s";
}

function escapeLabel(value: string): string {
  return value
    .replaceAll("\\", "\\\\")
    .replaceAll('"', '\\"')
    .replaceAll("\n", "\\n");
}
