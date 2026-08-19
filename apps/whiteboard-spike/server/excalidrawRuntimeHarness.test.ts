// @vitest-environment node

import { describe, expect, it } from "vitest";
import {
  BoundedRuntimeTelemetry,
  EXCALIDRAW_RUNTIME_TARGET,
  ExcalidrawRuntimeNode,
  RuntimeCredentialRing,
  SharedRuntimeCoordinator,
  assessRuntimeCost,
  assessRuntimeQuota,
  type RuntimeCredentialKind,
  type RuntimeDependency,
  type RuntimeDocumentScope,
} from "./excalidrawRuntimeHarness";

const scope: RuntimeDocumentScope = {
  documentId: "board-gate-f",
  generation: 1,
  tenantId: "tenant-gate-f",
};

describe("Excalidraw Gate F runtime and operations", () => {
  it("pins the exact candidate and shares one-time grants, opaque names and one writer lease across two nodes", () => {
    const harness = createHarness();
    harness.nodeA.start();
    harness.nodeB.start();

    expect(EXCALIDRAW_RUNTIME_TARGET).toMatchObject({
      collaborationProvider: "hocuspocus@4.6.0",
      engine: "@excalidraw/excalidraw@0.18.1",
      node: "24.15.0",
      persistence: "neon-yjs-binary-checkpoint",
      region: "singapore",
      replicas: 2,
      yjs: "13.6.27",
    });

    const grantA = harness.coordinator.issueGrant(
      request("teacher-a", "teacher-session-a"),
    );
    expect(grantA.providerDocumentName).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(grantA.providerDocumentName).not.toContain(scope.tenantId);
    expect(grantA.providerDocumentName).not.toContain(scope.documentId);
    const connectionA = harness.nodeA.openConnection(grantA);
    expect(() => harness.nodeB.openConnection(grantA)).toThrowError(
      "grant_invalid_or_replayed",
    );

    const connectionB = harness.nodeB.openConnection(
      harness.coordinator.issueGrant(request("teacher-b", "teacher-session-b")),
    );
    expect(connectionA.nodeId).toBe("runtime-a");
    expect(connectionB.nodeId).toBe("runtime-b");
    expect(harness.coordinator.documentStatus(scope)).toMatchObject({
      fenceEpoch: 1,
      writerNodeId: "runtime-a",
    });
  });

  it("fences a crashed writer, transfers ownership, flushes the watermark and drains cleanly", () => {
    const harness = createHarness();
    harness.nodeA.start();
    harness.nodeB.start();
    const connectionA = harness.nodeA.openConnection(
      harness.coordinator.issueGrant(request("teacher-a", "teacher-session-a")),
    );
    const connectionB = harness.nodeB.openConnection(
      harness.coordinator.issueGrant(request("teacher-b", "teacher-session-b")),
    );

    expect(harness.nodeA.mutate(connectionA.id)).toBe(1);
    expect(harness.nodeA.flush(scope)).toBe(1);
    expect(() => harness.nodeB.takeWriterLease(scope)).toThrowError(
      "writer_lease_active",
    );
    harness.nodeA.crash();
    expect(harness.nodeB.mutate(connectionB.id)).toBe(2);
    expect(harness.nodeB.takeWriterLease(scope)).toBe(2);
    expect(() => harness.nodeA.flush(scope)).toThrowError("stale_writer_fence");
    expect(harness.nodeB.flush(scope)).toBe(2);

    harness.nodeB.drain();
    expect(harness.nodeB.currentState()).toBe("stopped");
    expect(harness.coordinator.activeConnections()).toBe(0);
    expect(harness.coordinator.documentStatus(scope)).toMatchObject({
      persistedWatermark: 2,
      watermark: 2,
      writerNodeId: undefined,
    });
    expect(() =>
      harness.nodeB.openConnection(
        harness.coordinator.issueGrant(
          request("teacher-c", "teacher-session-c"),
        ),
      ),
    ).toThrowError("node_not_ready");
  });

  it("changes the opaque provider room on generation swap and denies the stale generation", () => {
    const harness = createHarness();
    harness.nodeA.start();
    const oldGrant = harness.coordinator.issueGrant(
      request("teacher-a", "teacher-session-a"),
    );
    const oldConnection = harness.nodeA.openConnection(oldGrant);
    const nextScope = harness.coordinator.swapGeneration(scope);
    const nextGrant = harness.coordinator.issueGrant({
      ...request("teacher-b", "teacher-session-b"),
      generation: nextScope.generation,
    });

    expect(nextScope.generation).toBe(2);
    expect(nextGrant.providerDocumentName).not.toBe(
      oldGrant.providerDocumentName,
    );
    expect(harness.coordinator.connection(oldConnection.id)?.readOnly).toBe(
      true,
    );
    expect(() =>
      harness.coordinator.issueGrant(request("teacher-c", "teacher-session-c")),
    ).toThrowError("stale_generation");
  });

  it("closes connections and releases ownership when drain cannot flush", () => {
    const harness = createHarness();
    harness.nodeA.start();
    harness.nodeA.openConnection(
      harness.coordinator.issueGrant(request("teacher-a", "teacher-session-a")),
    );
    harness.coordinator.setDependency("persistence", false);

    expect(() => harness.nodeA.drain()).toThrowError("dependency_unavailable");
    expect(harness.nodeA.currentState()).toBe("degraded");
    expect(harness.coordinator.activeConnections()).toBe(0);
    expect(
      harness.coordinator.documentStatus(scope).writerNodeId,
    ).toBeUndefined();
    expect(harness.telemetry.snapshot().metrics).toContainEqual({
      labels: { outcome: "failed" },
      name: "collab_drain",
      value: 1,
    });
  });

  it.each<RuntimeDependency>(["control_plane", "coordination", "persistence"])(
    "fails closed through a sustained %s outage and requires a fresh grant after recovery",
    (dependency) => {
      const harness = createHarness();
      harness.nodeA.start();
      const connection = harness.nodeA.openConnection(
        harness.coordinator.issueGrant(
          request("teacher-a", "teacher-session-a"),
        ),
      );
      harness.coordinator.setDependency(dependency, false);
      harness.nodeA.reconcileDependencies();
      harness.advance(10 * 60_000);

      expect(harness.nodeA.isReady()).toBe(false);
      expect(harness.coordinator.connection(connection.id)?.readOnly).toBe(
        true,
      );
      expect(() => harness.nodeA.mutate(connection.id)).toThrowError(
        "write_disabled",
      );
      expect(() =>
        harness.coordinator.issueGrant(
          request("teacher-b", "teacher-session-b"),
        ),
      ).toThrowError("dependency_unavailable");

      harness.coordinator.setDependency(dependency, true);
      harness.nodeA.recover();
      expect(harness.coordinator.connection(connection.id)).toBeUndefined();
      const recovered = harness.nodeA.openConnection(
        harness.coordinator.issueGrant(
          request("teacher-b", "teacher-session-b"),
        ),
      );
      expect(recovered.readOnly).toBe(false);
    },
  );

  it("keeps live edits available during snapshot-storage outage but exposes the bounded dependency signal", () => {
    const harness = createHarness();
    harness.nodeA.start();
    harness.coordinator.setDependency("snapshot", false);
    const connection = harness.nodeA.openConnection(
      harness.coordinator.issueGrant(request("teacher-a", "teacher-session-a")),
    );

    expect(harness.nodeA.mutate(connection.id)).toBe(1);
    expect(harness.nodeA.isReady()).toBe(true);
    expect(harness.telemetry.snapshot().metrics).toContainEqual({
      labels: { dependency: "snapshot", outcome: "down" },
      name: "collab_dependency_state",
      value: 0,
    });
  });

  it("enforces read-only and force-off kill switches on the server", () => {
    const harness = createHarness();
    harness.nodeA.start();
    const editConnection = harness.nodeA.openConnection(
      harness.coordinator.issueGrant(request("teacher-a", "teacher-session-a")),
    );

    harness.coordinator.setMode("read_only");
    expect(harness.coordinator.connection(editConnection.id)?.readOnly).toBe(
      true,
    );
    expect(() => harness.nodeA.mutate(editConnection.id)).toThrowError(
      "write_disabled",
    );
    expect(() =>
      harness.coordinator.issueGrant(request("teacher-b", "teacher-session-b")),
    ).toThrowError("write_disabled");
    const viewer = harness.nodeA.openConnection(
      harness.coordinator.issueGrant({
        ...request("student-a", "student-session-a"),
        capability: "view",
      }),
    );
    expect(viewer.readOnly).toBe(true);

    harness.coordinator.setMode("export_only");
    harness.nodeA.reconcileDependencies();
    expect(harness.nodeA.isReady()).toBe(false);
    expect(() =>
      harness.coordinator.issueGrant({
        ...request("student-b", "student-session-b"),
        capability: "view",
      }),
    ).toThrowError("new_connections_disabled");
    harness.coordinator.setMode("off");
    expect(harness.coordinator.currentMode()).toBe("off");
    expect(harness.coordinator.activeConnections()).toBe(0);
  });

  it("applies document and runtime quotas globally rather than per replica", () => {
    const harness = createHarness({
      maxConnections: 2,
      maxDocumentConnections: 1,
    });
    harness.nodeA.start();
    harness.nodeB.start();
    harness.nodeA.openConnection(
      harness.coordinator.issueGrant(request("teacher-a", "teacher-session-a")),
    );
    expect(() =>
      harness.nodeB.openConnection(
        harness.coordinator.issueGrant(
          request("teacher-b", "teacher-session-b"),
        ),
      ),
    ).toThrowError("document_quota_exceeded");

    harness.nodeB.openConnection(
      harness.coordinator.issueGrant({
        ...request("teacher-b", "teacher-session-b"),
        documentId: "board-gate-f-2",
      }),
    );
    expect(() =>
      harness.nodeB.openConnection(
        harness.coordinator.issueGrant({
          ...request("teacher-c", "teacher-session-c"),
          documentId: "board-gate-f-3",
        }),
      ),
    ).toThrowError("document_quota_exceeded");

    const quotaScopes = harness.telemetry
      .snapshot()
      .metrics.filter((point) => point.name === "collab_quota_reject")
      .map((point) => point.labels.scope)
      .sort();
    expect(quotaScopes).toEqual(["document", "runtime"]);
  });

  it("rotates every credential with bounded overlap, then rejects old material without logging it", () => {
    const harness = createHarness();
    const kinds: RuntimeCredentialKind[] = [
      "coordination",
      "grant_signing",
      "persistence",
      "snapshot_binding",
    ];

    const snapshotRetentionMs = 30 * 24 * 60 * 60_000;
    for (const kind of kinds) {
      const oldKeyId = `${kind}-v1`;
      const oldSecret = `${kind}-old-secret-`.padEnd(40, "x");
      const newKeyId = `${kind}-v2`;
      const newSecret = `${kind}-new-secret-`.padEnd(40, "y");
      expect(harness.credentials.authenticate(kind, oldKeyId, oldSecret)).toBe(
        true,
      );
      harness.credentials.rotate(
        kind,
        { keyId: newKeyId, secret: newSecret },
        kind === "snapshot_binding" ? snapshotRetentionMs : 60_000,
      );
      expect(harness.credentials.authenticate(kind, oldKeyId, oldSecret)).toBe(
        true,
      );
      expect(harness.credentials.authenticate(kind, newKeyId, newSecret)).toBe(
        true,
      );
    }

    harness.advance(60_001);
    harness.credentials.retireExpired();
    for (const kind of kinds.filter((kind) => kind !== "snapshot_binding")) {
      expect(
        harness.credentials.authenticate(
          kind,
          `${kind}-v1`,
          `${kind}-old-secret-`.padEnd(40, "x"),
        ),
      ).toBe(false);
    }
    expect(
      harness.credentials.authenticate(
        "snapshot_binding",
        "snapshot_binding-v1",
        "snapshot_binding-old-secret-".padEnd(40, "x"),
      ),
    ).toBe(true);
    harness.advance(snapshotRetentionMs);
    harness.credentials.retireExpired();
    expect(
      harness.credentials.authenticate(
        "snapshot_binding",
        "snapshot_binding-v1",
        "snapshot_binding-old-secret-".padEnd(40, "x"),
      ),
    ).toBe(false);
    const evidence = JSON.stringify(harness.telemetry.snapshot());
    expect(evidence).not.toContain("old-secret");
    expect(evidence).not.toContain("new-secret");
    expect(evidence).not.toContain(scope.tenantId);
    expect(evidence).not.toContain(scope.documentId);
    expect(() =>
      harness.telemetry.recordMetric("collab_runtime_state", 1, {
        tenant_id: scope.tenantId,
      }),
    ).toThrowError("metric_label_denied");
    for (let index = 0; index < 300; index += 1) {
      harness.telemetry.recordLog({
        event: "connection",
        outcome: "ok",
        reason: "none",
      });
    }
    expect(harness.telemetry.snapshot().logs).toHaveLength(256);
  });

  it("bounds outstanding grants and rejects malformed capability before allocating state", () => {
    const harness = createHarness({ maxOutstandingGrants: 1 });
    harness.nodeA.start();
    harness.coordinator.issueGrant(
      request("teacher-a", "teacher-session-a"),
      1_000,
    );
    expect(() =>
      harness.coordinator.issueGrant(request("teacher-b", "teacher-session-b")),
    ).toThrowError("document_quota_exceeded");
    harness.advance(1_001);
    expect(() =>
      harness.coordinator.issueGrant(request("teacher-b", "teacher-session-b")),
    ).not.toThrow();
    expect(() =>
      harness.coordinator.issueGrant({
        ...request("teacher-c", "teacher-session-c"),
        capability: "owner",
      } as never),
    ).toThrowError("grant_scope_invalid");
  });

  it("requires dashboard quotes and emits deterministic 70/90/100 cost and 70/85/100 quota outcomes", () => {
    const telemetry = new BoundedRuntimeTelemetry();
    expect(
      assessRuntimeCost({ b2MonthlyUsd: 5, neonMonthlyUsd: 20 }, telemetry, {
        freezeLimitUsd: 180,
        hardLimitUsd: 200,
        notifyLimitUsd: 140,
      }),
    ).toMatchObject({ outcome: "quote_pending" });
    expect(
      assessRuntimeCost(
        {
          b2MonthlyUsd: 5,
          coordinationMonthlyUsd: 35,
          neonMonthlyUsd: 20,
          renderMonthlyUsdPerInstance: 50,
        },
        telemetry,
        {
          freezeLimitUsd: 180,
          hardLimitUsd: 200,
          notifyLimitUsd: 140,
        },
      ),
    ).toMatchObject({ outcome: "notify", quotedMonthlyUsd: 160 });
    expect(
      assessRuntimeCost(
        {
          b2MonthlyUsd: 15,
          coordinationMonthlyUsd: 45,
          neonMonthlyUsd: 25,
          renderMonthlyUsdPerInstance: 50,
        },
        telemetry,
        {
          freezeLimitUsd: 180,
          hardLimitUsd: 200,
          notifyLimitUsd: 140,
        },
      ),
    ).toMatchObject({ outcome: "freeze", quotedMonthlyUsd: 185 });
    expect(
      assessRuntimeCost(
        {
          b2MonthlyUsd: 25,
          coordinationMonthlyUsd: 60,
          neonMonthlyUsd: 40,
          renderMonthlyUsdPerInstance: 50,
        },
        telemetry,
        {
          freezeLimitUsd: 180,
          hardLimitUsd: 200,
          notifyLimitUsd: 140,
        },
      ),
    ).toMatchObject({ outcome: "hard_limit", quotedMonthlyUsd: 225 });
    expect(assessRuntimeQuota(69, 100, "runtime", telemetry).outcome).toBe(
      "ok",
    );
    expect(assessRuntimeQuota(70, 100, "runtime", telemetry).outcome).toBe(
      "notify",
    );
    expect(assessRuntimeQuota(85, 100, "document", telemetry).outcome).toBe(
      "throttle",
    );
    expect(assessRuntimeQuota(100, 100, "document", telemetry).outcome).toBe(
      "deny",
    );
    expect(() =>
      assessRuntimeCost({ b2MonthlyUsd: -1, neonMonthlyUsd: 20 }, telemetry, {
        freezeLimitUsd: 180,
        hardLimitUsd: 200,
        notifyLimitUsd: 140,
      }),
    ).toThrowError("metric_label_denied");
  });
});

function request(actorId: string, sessionId: string) {
  return {
    ...scope,
    actorId,
    capability: "edit" as const,
    sessionId,
  };
}

function createHarness({
  maxConnections = 100,
  maxDocumentConnections = 50,
  maxOutstandingGrants = 10_000,
}: {
  maxConnections?: number;
  maxDocumentConnections?: number;
  maxOutstandingGrants?: number;
} = {}) {
  let clock = Date.parse("2026-08-19T00:00:00.000Z");
  const now = () => clock;
  const telemetry = new BoundedRuntimeTelemetry();
  const credentials = new RuntimeCredentialRing(
    {
      coordination: credential("coordination"),
      grant_signing: credential("grant_signing"),
      persistence: credential("persistence"),
      snapshot_binding: credential("snapshot_binding"),
    },
    now,
    telemetry,
  );
  const coordinator = new SharedRuntimeCoordinator(
    new Uint8Array(32).fill(83),
    credentials,
    telemetry,
    now,
    maxConnections,
    maxDocumentConnections,
    maxOutstandingGrants,
  );
  return {
    advance: (milliseconds: number) => {
      clock += milliseconds;
    },
    coordinator,
    credentials,
    nodeA: new ExcalidrawRuntimeNode("runtime-a", coordinator, telemetry),
    nodeB: new ExcalidrawRuntimeNode("runtime-b", coordinator, telemetry),
    telemetry,
  };
}

function credential(kind: RuntimeCredentialKind) {
  return {
    keyId: `${kind}-v1`,
    secret: `${kind}-old-secret-`.padEnd(40, "x"),
  };
}
