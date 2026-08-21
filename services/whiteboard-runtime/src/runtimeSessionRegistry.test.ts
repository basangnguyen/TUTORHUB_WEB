import { describe, expect, it } from "vitest";
import type { CollaborationScope } from "./contracts.js";
import {
  RuntimeConnectionPolicy,
  type ConnectionPolicyLimits,
} from "./runtimePolicy.js";
import {
  RuntimeSessionRegistry,
  RuntimeSessionRegistryError,
  type RuntimeSessionAdmission,
  type RuntimeSessionFailureStage,
} from "./runtimeSessionRegistry.js";

const connectionLimits: ConnectionPolicyLimits = {
  maxConnections: 8,
  maxConnectionsPerActor: 2,
  maxConnectionsPerDocument: 3,
  maxConnectionsPerTenant: 4,
  maxReconnectAttempts: 8,
  reconnectWindowMs: 1_000,
};

describe("RuntimeSessionRegistry", () => {
  it("reserves one provider scope for compatible sessions and exposes fixed counters", () => {
    const registry = registryWithPolicy();
    const first = registry.reserve("socket-a:provider-a", scope());
    const second = registry.reserve(
      "socket-b:provider-a",
      scope({ actorId: "actor-b", sessionId: "session-b" }),
    );

    expect(registry.metrics()).toEqual({
      activeDocumentScopes: 1,
      activeSessions: 0,
      pendingSessions: 2,
      reservationAcceptedTotal: 2,
      reservationDeniedTotal: 0,
      reservationReleasedTotal: 0,
      rollbackAuthenticationTotal: 0,
      rollbackLoadTotal: 0,
      rollbackSetupTotal: 0,
    });

    first.commit();
    first.commit();
    expect(registry.metrics()).toMatchObject({
      activeDocumentScopes: 1,
      activeSessions: 1,
      pendingSessions: 1,
    });
    expect(registry.scopes()).toHaveLength(2);

    first.release();
    second.release();
    expect(registry.metrics()).toMatchObject({
      activeDocumentScopes: 0,
      activeSessions: 0,
      pendingSessions: 0,
      reservationReleasedTotal: 2,
    });
  });

  it.each([
    {
      label: "a different canonical scope under one provider name",
      candidate: scope({ documentId: "document-b" }),
    },
    {
      label: "a new generation under one provider name",
      candidate: scope({ generation: 2 }),
    },
    {
      label: "a stale writer fence under one provider name",
      candidate: scope({ writerFence: 2 }),
    },
    {
      label: "a second provider name for one canonical scope",
      candidate: scope({ providerDocumentName: "provider-b" }),
    },
  ])("rejects $label without changing held capacity", ({ candidate }) => {
    const policy = new RuntimeConnectionPolicy(connectionLimits, () => 100);
    const registry = new RuntimeSessionRegistry(policy);
    const first = registry.reserve("socket-a:provider-a", scope());

    expect(() =>
      registry.reserve("socket-b:provider-b", candidate),
    ).toThrowError(
      expect.objectContaining<Partial<RuntimeSessionRegistryError>>({
        code: "document_scope_conflict",
      }),
    );
    expect(policy.activeConnections()).toBe(1);
    expect(registry.metrics()).toMatchObject({
      activeDocumentScopes: 1,
      pendingSessions: 1,
      reservationAcceptedTotal: 1,
      reservationDeniedTotal: 1,
    });
    first.release();
  });

  it("atomically chooses only one authority when async auth callbacks race", async () => {
    const registry = registryWithPolicy();
    const results = await Promise.allSettled([
      Promise.resolve().then(() =>
        registry.reserve("socket-a:provider-a", scope()),
      ),
      Promise.resolve().then(() =>
        registry.reserve(
          "socket-b:provider-a",
          scope({ generation: 2, writerFence: 2 }),
        ),
      ),
    ]);

    expect(results.filter(({ status }) => status === "fulfilled")).toHaveLength(
      1,
    );
    expect(results.filter(({ status }) => status === "rejected")).toHaveLength(
      1,
    );
    expect(registry.metrics()).toMatchObject({
      activeDocumentScopes: 1,
      pendingSessions: 1,
      reservationAcceptedTotal: 1,
      reservationDeniedTotal: 1,
    });
    registry.release("socket-a:provider-a");
    registry.release("socket-b:provider-a");
  });

  it("does not install a scope when admission fails and permits a clean retry", () => {
    let attempts = 0;
    let releases = 0;
    const admission: RuntimeSessionAdmission = {
      acquire: () => {
        attempts += 1;
        if (attempts === 1) throw new Error("bounded-admission-failure");
        return () => {
          releases += 1;
        };
      },
    };
    const registry = new RuntimeSessionRegistry(admission);

    expect(() => registry.reserve("socket-a:provider-a", scope())).toThrowError(
      "bounded-admission-failure",
    );
    expect(registry.metrics()).toMatchObject({
      activeDocumentScopes: 0,
      pendingSessions: 0,
      reservationAcceptedTotal: 0,
      reservationDeniedTotal: 1,
    });

    const retry = registry.reserve("socket-a:provider-a", scope());
    retry.release();
    retry.release();
    expect(releases).toBe(1);
  });

  it.each([
    "authentication",
    "setup",
    "load",
  ] satisfies RuntimeSessionFailureStage[])(
    "rolls back %s failures and frees quotas/scopes exactly once",
    (stage) => {
      const policy = new RuntimeConnectionPolicy(
        { ...connectionLimits, maxConnections: 1 },
        () => 100,
      );
      const registry = new RuntimeSessionRegistry(policy);
      const failed = registry.reserve("socket-a:provider-a", scope());

      expect(failed.rollback(stage).released).toBe(true);
      expect(failed.rollback(stage).released).toBe(false);
      expect(policy.activeConnections()).toBe(0);
      expect(registry.metrics()).toMatchObject({
        activeDocumentScopes: 0,
        pendingSessions: 0,
        [`rollback${titleCase(stage)}Total`]: 1,
      });

      expect(() =>
        registry.reserve(
          "socket-b:provider-a",
          scope({ actorId: "actor-b", sessionId: "session-b" }),
        ),
      ).not.toThrow();
    },
  );

  it("releases disconnects by key idempotently and retains the capability", () => {
    const registry = registryWithPolicy();
    const reservation = registry.reserve("socket-a:provider-a", scope());
    reservation.commit();

    expect(registry.release("socket-a:provider-a")).toEqual({
      capability: "edit",
      phase: "active",
      released: true,
    });
    expect(registry.release("socket-a:provider-a")).toEqual({
      released: false,
    });
    expect(registry.metrics()).toMatchObject({
      activeDocumentScopes: 0,
      activeSessions: 0,
      reservationReleasedTotal: 1,
    });
  });

  it("prevents a stale transaction handle from releasing a reused session key", () => {
    const registry = registryWithPolicy();
    const stale = registry.reserve("socket-a:provider-a", scope());
    stale.rollback("setup");

    const current = registry.reserve("socket-a:provider-a", scope());
    expect(stale.release()).toEqual({ released: false });
    expect(registry.sessionScope("socket-a:provider-a")).toEqual(scope());
    expect(registry.metrics()).toMatchObject({
      activeDocumentScopes: 1,
      pendingSessions: 1,
    });
    current.release();
  });

  it("rejects duplicate and malformed reservations with content-free errors", () => {
    const registry = registryWithPolicy();
    const privateMarker = "private-provider-marker";
    registry.reserve("socket-a:provider-a", scope());

    for (const operation of [
      () => registry.reserve("socket-a:provider-a", scope()),
      () =>
        registry.reserve(
          "socket-b:provider-b",
          scope({ providerDocumentName: privateMarker, writerFence: 2 }),
        ),
      () => registry.reserve("", scope()),
    ]) {
      let failure: unknown;
      try {
        operation();
      } catch (error) {
        failure = error;
      }
      expect(failure).toBeInstanceOf(RuntimeSessionRegistryError);
      expect(JSON.stringify(failure)).not.toContain(privateMarker);
    }
  });
});

function registryWithPolicy(): RuntimeSessionRegistry {
  return new RuntimeSessionRegistry(
    new RuntimeConnectionPolicy(connectionLimits, () => 100),
  );
}

function titleCase(value: RuntimeSessionFailureStage): string {
  return `${value.slice(0, 1).toUpperCase()}${value.slice(1)}`;
}

function scope(
  overrides: Partial<CollaborationScope> = {},
): CollaborationScope {
  return {
    actorId: "actor-a",
    authorityLease: "authority-lease-a",
    capability: "edit",
    documentId: "document-a",
    generation: 1,
    origin: "https://app.example.test",
    providerDocumentName: "provider-a",
    sessionId: "session-a",
    tenantId: "tenant-a",
    writerFence: 1,
    ...overrides,
  };
}
