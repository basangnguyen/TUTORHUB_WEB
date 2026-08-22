import { describe, expect, it } from "vitest";
import {
  RuntimeConnectionPolicy,
  RuntimeIngressPolicy,
  RuntimePolicyError,
  RuntimeTenantOperationPolicy,
  validateAwarenessEnvelope,
  type AwarenessPolicyLimits,
  type ConnectionPolicyLimits,
  type IngressPolicyLimits,
} from "./runtimePolicy.js";

const connectionLimits: ConnectionPolicyLimits = {
  maxConnections: 8,
  maxConnectionsPerActor: 2,
  maxConnectionsPerDocument: 3,
  maxConnectionsPerTenant: 4,
  maxReconnectAttempts: 8,
  reconnectWindowMs: 1_000,
};

describe("RuntimeConnectionPolicy", () => {
  it.each([
    {
      code: "actor_connection_quota",
      limits: { ...connectionLimits, maxConnectionsPerActor: 1 },
      scopes: [scope(), scope({ documentId: "document-b" })],
    },
    {
      code: "document_connection_quota",
      limits: { ...connectionLimits, maxConnectionsPerDocument: 1 },
      scopes: [scope(), scope({ actorId: "actor-b" })],
    },
    {
      code: "tenant_connection_quota",
      limits: { ...connectionLimits, maxConnectionsPerTenant: 1 },
      scopes: [
        scope(),
        scope({ actorId: "actor-b", documentId: "document-b" }),
      ],
    },
    {
      code: "global_connection_quota",
      limits: { ...connectionLimits, maxConnections: 1 },
      scopes: [
        scope(),
        scope({
          actorId: "actor-b",
          documentId: "document-b",
          tenantId: "tenant-b",
        }),
      ],
    },
  ] as const)("fails closed with $code", ({ code, limits, scopes }) => {
    const policy = new RuntimeConnectionPolicy(limits, () => 100);
    const release = policy.acquire(scopes[0]);
    expect(() => policy.acquire(scopes[1])).toThrowError(
      expect.objectContaining<Partial<RuntimePolicyError>>({ code }),
    );
    expect(policy.activeConnections()).toBe(1);
    release();
    release();
    expect(policy.activeConnections()).toBe(0);
  });

  it("isolates tenant quotas and releases capacity idempotently", () => {
    const policy = new RuntimeConnectionPolicy(
      { ...connectionLimits, maxConnectionsPerTenant: 1 },
      () => 100,
    );
    const releaseA = policy.acquire(scope());
    const releaseB = policy.acquire(
      scope({
        actorId: "actor-b",
        documentId: "document-b",
        tenantId: "tenant-b",
      }),
    );
    expect(policy.activeConnections()).toBe(2);
    expect(() =>
      policy.acquire(scope({ actorId: "actor-c", documentId: "document-c" })),
    ).toThrowError("tenant_connection_quota");

    releaseA();
    releaseA();
    expect(() =>
      policy.acquire(scope({ actorId: "actor-c", documentId: "document-c" })),
    ).not.toThrow();
    releaseB();
  });

  it("uses a sliding reconnect window per tenant and actor", () => {
    let now = 100;
    const policy = new RuntimeConnectionPolicy(
      {
        ...connectionLimits,
        maxReconnectAttempts: 3,
        reconnectWindowMs: 1_000,
      },
      () => now,
    );
    const candidate = scope();
    policy.acquire(candidate)();
    now = 500;
    policy.acquire(candidate)();
    now = 1_099;
    policy.acquire(candidate)();
    expect(() => policy.acquire(candidate)).toThrowError(
      "reconnect_storm_denied",
    );

    now = 1_100;
    expect(() => policy.acquire(candidate)).not.toThrow();
  });

  it("fails closed for an invalid clock or scope without echoing identifiers", () => {
    const privateActor = "private-actor-marker";
    const policy = new RuntimeConnectionPolicy(connectionLimits, () => -1);
    let failure: unknown;
    try {
      policy.acquire(scope({ actorId: privateActor }));
    } catch (error) {
      failure = error;
    }
    expect(failure).toEqual(
      expect.objectContaining<Partial<RuntimePolicyError>>({
        code: "policy_clock_invalid",
      }),
    );
    expect(JSON.stringify(failure)).not.toContain(privateActor);
  });
});

describe("RuntimeTenantOperationPolicy", () => {
  it("clamps tenant scope limits and isolates a noisy tenant", () => {
    let now = 100;
    const policy = new RuntimeTenantOperationPolicy(100, () => now);
    const tenantA = scope({ maxOperationsPerMinute: 2 });
    const tenantB = scope({
      maxOperationsPerMinute: 10,
      tenantId: "tenant-b",
    });

    policy.consume(tenantA);
    policy.consume(tenantA);
    expect(() => policy.consume(tenantA)).toThrowError(
      "tenant_operation_quota",
    );
    expect(() => policy.consume(tenantB, 10)).not.toThrow();

    now = 60_101;
    expect(() => policy.consume(tenantA)).not.toThrow();
  });

  it("rejects invalid limits without exposing tenant identifiers", () => {
    const privateTenant = "private-tenant-marker";
    let failure: unknown;
    try {
      new RuntimeTenantOperationPolicy(10).consume({
        maxOperationsPerMinute: 0,
        tenantId: privateTenant,
      });
    } catch (error) {
      failure = error;
    }
    expect(failure).toEqual(
      expect.objectContaining<Partial<RuntimePolicyError>>({
        code: "policy_input_invalid",
      }),
    );
    expect(JSON.stringify(failure)).not.toContain(privateTenant);
  });
});

const ingressLimits: IngressPolicyLimits = {
  maxBytesPerWindow: 100,
  maxMessagesPerWindow: 3,
  windowMs: 1_000,
};

describe("RuntimeIngressPolicy", () => {
  it("applies independent sliding message budgets per socket", () => {
    let now = 100;
    const policy = new RuntimeIngressPolicy(ingressLimits, () => now);
    expect(policy.consume("socket-a", 10)).toEqual({
      bytes: 90,
      messages: 2,
    });
    now = 500;
    policy.consume("socket-a", 10);
    now = 1_099;
    policy.consume("socket-a", 10);
    expect(() => policy.consume("socket-a", 1)).toThrowError(
      "ingress_message_budget_exceeded",
    );
    expect(() => policy.consume("socket-b", 1)).not.toThrow();

    now = 1_100;
    expect(() => policy.consume("socket-a", 1)).not.toThrow();
  });

  it("applies a byte budget and release drops pending socket history", () => {
    const policy = new RuntimeIngressPolicy(ingressLimits, () => 100);
    policy.consume("socket-a", 60);
    expect(() => policy.consume("socket-a", 41)).toThrowError(
      "ingress_byte_budget_exceeded",
    );
    policy.release("socket-a");
    expect(() => policy.consume("socket-a", 100)).not.toThrow();
  });

  it("rejects zero/invalid byte lengths and a clock rollback", () => {
    let now = 100;
    const policy = new RuntimeIngressPolicy(ingressLimits, () => now);
    expect(() => policy.consume("socket-a", 0)).toThrowError(
      "policy_input_invalid",
    );
    policy.consume("socket-a", 1);
    now = 99;
    expect(() => policy.consume("socket-a", 1)).toThrowError(
      "policy_clock_invalid",
    );
  });
});

const awarenessLimits: AwarenessPolicyLimits = {
  maxBytes: 256,
  maxDepth: 3,
  maxStates: 2,
};

describe("validateAwarenessEnvelope", () => {
  it("accepts bounded JSON-shaped states and returns content-free metadata", () => {
    expect(
      validateAwarenessEnvelope(
        {
          byteLength: 80,
          states: [
            { cursor: { x: 10, y: 20 }, mode: "select" },
            { cursor: null, mode: "idle" },
          ],
        },
        awarenessLimits,
      ),
    ).toEqual({ byteLength: 80, maxDepth: 2, stateCount: 2 });
  });

  it.each([
    {
      code: "awareness_bytes_exceeded",
      envelope: { byteLength: 257, states: [] },
    },
    {
      code: "awareness_state_count_exceeded",
      envelope: { byteLength: 80, states: [{}, {}, {}] },
    },
    {
      code: "awareness_depth_exceeded",
      envelope: {
        byteLength: 80,
        states: [{ one: { two: { three: { four: true } } } }],
      },
    },
  ] as const)("fails closed with $code", ({ code, envelope }) => {
    expect(() =>
      validateAwarenessEnvelope(envelope, awarenessLimits),
    ).toThrowError(
      expect.objectContaining<Partial<RuntimePolicyError>>({ code }),
    );
  });

  it("rejects cyclic/accessor state without exposing awareness content", () => {
    const privateMarker = "private-awareness-marker";
    const cyclic: { marker: string; self?: unknown } = {
      marker: privateMarker,
    };
    cyclic.self = cyclic;
    let failure: unknown;
    try {
      validateAwarenessEnvelope(
        { byteLength: 100, states: [cyclic] },
        awarenessLimits,
      );
    } catch (error) {
      failure = error;
    }
    expect(failure).toEqual(
      expect.objectContaining<Partial<RuntimePolicyError>>({
        code: "awareness_structure_invalid",
      }),
    );
    expect(JSON.stringify(failure)).not.toContain(privateMarker);

    const accessorState: Record<string, unknown> = {};
    Object.defineProperty(accessorState, "unsafe", {
      enumerable: true,
      get: () => privateMarker,
    });
    expect(() =>
      validateAwarenessEnvelope(
        { byteLength: 100, states: [accessorState] },
        awarenessLimits,
      ),
    ).toThrowError("awareness_structure_invalid");
  });

  it("rejects symbol-keyed awareness state as non-JSON structure", () => {
    expect(() =>
      validateAwarenessEnvelope(
        {
          byteLength: 40,
          states: [{ [Symbol("hidden")]: "not-json" }],
        },
        awarenessLimits,
      ),
    ).toThrowError("awareness_structure_invalid");
  });
});

function scope(
  overrides: Partial<{
    actorId: string;
    documentId: string;
    generation: number;
    maxConnectionsPerTenant: number;
    maxOperationsPerMinute: number;
    tenantId: string;
  }> = {},
) {
  return {
    actorId: "actor-a",
    documentId: "document-a",
    generation: 1,
    maxConnectionsPerTenant: 4,
    maxOperationsPerMinute: 100,
    tenantId: "tenant-a",
    ...overrides,
  };
}
