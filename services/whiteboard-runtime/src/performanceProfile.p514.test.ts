import { performance } from "node:perf_hooks";
import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import {
  CanonicalExcalidrawAuthority,
  type CanonicalElementV1,
} from "@tutorhub/collaboration-client";
import {
  MAX_DURABLE_DOCUMENT_BYTES,
  type CollaborationScope,
} from "./contracts.js";
import { compactCheckpointState } from "./checkpointCompaction.js";
import {
  RuntimeConnectionPolicy,
  RuntimeIngressPolicy,
  RuntimeTenantOperationPolicy,
} from "./runtimePolicy.js";
import { RuntimeSessionRegistry } from "./runtimeSessionRegistry.js";

const CLIENTS = 50;
const SHAPES = 2_000;

describe("P5-COLLAB-14 production runtime performance invariants", () => {
  it("compacts the 2,000-shape cap, isolates backpressure, and cleans 50 sessions", () => {
    const source = new Y.Doc();
    const recovered = new Y.Doc();
    const authority = new CanonicalExcalidrawAuthority(
      source,
      canonicalScope(),
      "teacher-p514",
    );
    let recoveryAuthority: CanonicalExcalidrawAuthority | undefined;

    try {
      authority.initialize({
        elements: Array.from({ length: SHAPES }, (_, index) =>
          rectangle(index),
        ),
        files: {},
        page: {
          backgroundColor: "#f8fafc",
          id: "page-p514",
          name: "P5-COLLAB-14",
        },
        schemaVersion: 1,
      });
      const encoded = authority.encodeProviderState();
      expect(encoded.byteLength).toBeLessThanOrEqual(
        MAX_DURABLE_DOCUMENT_BYTES,
      );

      const compactionLatencies: number[] = [];
      let compacted = compactCheckpointState(
        encoded,
        MAX_DURABLE_DOCUMENT_BYTES,
      );
      for (let iteration = 0; iteration < 10; iteration += 1) {
        const startedAt = performance.now();
        compacted = compactCheckpointState(
          compacted.state,
          MAX_DURABLE_DOCUMENT_BYTES,
        );
        compactionLatencies.push(performance.now() - startedAt);
      }
      Y.applyUpdate(recovered, compacted.state);
      recoveryAuthority = new CanonicalExcalidrawAuthority(
        recovered,
        canonicalScope(),
        "recovery-p514",
      );
      expect(recoveryAuthority.getSemanticHash()).toBe(
        authority.getSemanticHash(),
      );
      expect(recoveryAuthority.getScene().elements).toHaveLength(SHAPES);

      const connectionPolicy = new RuntimeConnectionPolicy(
        {
          maxConnections: CLIENTS,
          maxConnectionsPerActor: 2,
          maxConnectionsPerDocument: CLIENTS,
          maxConnectionsPerTenant: CLIENTS,
          maxReconnectAttempts: 100,
          reconnectWindowMs: 60_000,
        },
        () => 100,
      );
      const registry = new RuntimeSessionRegistry(connectionPolicy);
      const reservations = Array.from({ length: CLIENTS }, (_, index) => {
        const reservation = registry.reserve(
          `socket-p514-${index}`,
          runtimeScope(index),
        );
        reservation.commit();
        return reservation;
      });
      expect(registry.metrics()).toMatchObject({
        activeDocumentScopes: 1,
        activeSessions: CLIENTS,
        pendingSessions: 0,
      });
      expect(connectionPolicy.activeConnections()).toBe(CLIENTS);

      const operationPolicy = new RuntimeTenantOperationPolicy(100, () => 100);
      operationPolicy.consume(
        { maxOperationsPerMinute: 10, tenantId: "tenant-noisy" },
        10,
      );
      expect(() =>
        operationPolicy.consume(
          { maxOperationsPerMinute: 10, tenantId: "tenant-noisy" },
          1,
        ),
      ).toThrowError("tenant_operation_quota");
      expect(() =>
        operationPolicy.consume(
          { maxOperationsPerMinute: 100, tenantId: "tenant-quiet" },
          100,
        ),
      ).not.toThrow();

      const ingressPolicy = new RuntimeIngressPolicy(
        {
          maxBytesPerWindow: 1_024,
          maxMessagesPerWindow: 2,
          windowMs: 1_000,
        },
        () => 100,
      );
      ingressPolicy.consume("socket-noisy", 512);
      ingressPolicy.consume("socket-noisy", 512);
      expect(() => ingressPolicy.consume("socket-noisy", 1)).toThrowError(
        "ingress_message_budget_exceeded",
      );
      expect(() => ingressPolicy.consume("socket-quiet", 1)).not.toThrow();
      ingressPolicy.release("socket-noisy");
      ingressPolicy.release("socket-quiet");

      for (const reservation of reservations) reservation.release();
      expect(connectionPolicy.activeConnections()).toBe(0);
      expect(registry.metrics()).toMatchObject({
        activeDocumentScopes: 0,
        activeSessions: 0,
        pendingSessions: 0,
        reservationReleasedTotal: CLIENTS,
      });

      const result = {
        clients: CLIENTS,
        compactedBytes: compacted.state.byteLength,
        compactionP95Ms: rounded(percentile95(compactionLatencies)),
        encodedStateBytes: encoded.byteLength,
        noisyTenantDenied: true,
        quietTenantAccepted: true,
        sessionsAfterCleanup: registry.metrics().activeSessions,
        shapes: SHAPES,
      };
      process.stdout.write(`P5_COLLAB_14_RUNTIME ${JSON.stringify(result)}\n`);
      expect(result.compactionP95Ms).toBeLessThanOrEqual(5_000);
      expect(result.sessionsAfterCleanup).toBe(0);
    } finally {
      recoveryAuthority?.destroy();
      authority.destroy();
      recovered.destroy();
      source.destroy();
    }
  }, 30_000);
});

function canonicalScope() {
  return {
    documentId: "document-p514",
    generation: 14,
    tenantId: "tenant-p514",
  };
}

function runtimeScope(index: number): CollaborationScope {
  return {
    actorId: `actor-p514-${index}`,
    authorityLease: "authority-lease-p514",
    capability: "edit",
    documentId: "document-p514",
    generation: 14,
    maxConnectionsPerTenant: CLIENTS,
    maxOperationsPerMinute: 6_000,
    maxStorageBytesPerTenant: MAX_DURABLE_DOCUMENT_BYTES,
    origin: "https://app.example.test",
    providerDocumentName: "provider-document-p514",
    sessionId: `session-p514-${index}`,
    tenantId: "tenant-p514",
    writerFence: 14,
  };
}

function rectangle(index: number): CanonicalElementV1 {
  return {
    height: 60,
    id: `shape-p514-${index.toString().padStart(4, "0")}`,
    type: "rectangle",
    width: 100,
    x: (index % 40) * 120,
    y: Math.floor(index / 40) * 80,
  };
}

function percentile95(values: number[]): number {
  const sorted = [...values].sort((left, right) => left - right);
  return sorted[Math.max(0, Math.ceil(sorted.length * 0.95) - 1)] ?? 0;
}

function rounded(value: number): number {
  return Math.round(value * 10) / 10;
}
