import type { CollaborationScope } from "./contracts.js";

export type RuntimePolicyErrorCode =
  | "actor_connection_quota"
  | "awareness_bytes_exceeded"
  | "awareness_depth_exceeded"
  | "awareness_identity_invalid"
  | "awareness_state_count_exceeded"
  | "awareness_structure_invalid"
  | "document_connection_quota"
  | "global_connection_quota"
  | "ingress_byte_budget_exceeded"
  | "ingress_message_budget_exceeded"
  | "policy_clock_invalid"
  | "policy_configuration_invalid"
  | "policy_input_invalid"
  | "reconnect_storm_denied"
  | "tenant_connection_quota";

/**
 * Deliberately carries only a bounded code. Callers must not attach scope IDs,
 * frame bytes, or decoded awareness content when logging a denial.
 */
export class RuntimePolicyError extends Error {
  constructor(readonly code: RuntimePolicyErrorCode) {
    super(code);
    this.name = "RuntimePolicyError";
  }
}

export interface ConnectionPolicyLimits {
  maxConnections: number;
  maxConnectionsPerActor: number;
  maxConnectionsPerDocument: number;
  maxConnectionsPerTenant: number;
  maxReconnectAttempts: number;
  reconnectWindowMs: number;
}

type ConnectionScope = Pick<
  CollaborationScope,
  "actorId" | "documentId" | "generation" | "tenantId"
>;

interface RollingHistory {
  lastObservedAt: number;
  timestamps: number[];
}

/**
 * Process-local admission policy for the accepted single-instance profile.
 * A future multi-instance topology must replace this with a shared atomic
 * admission authority rather than summing independent replica-local limits.
 */
export class RuntimeConnectionPolicy {
  private active = 0;
  private readonly actors = new Map<string, number>();
  private readonly documents = new Map<string, number>();
  private readonly reconnects = new Map<string, RollingHistory>();
  private readonly tenants = new Map<string, number>();

  constructor(
    private readonly limits: ConnectionPolicyLimits,
    private readonly now: () => number = Date.now,
  ) {
    validatePositiveLimits(Object.values(limits));
  }

  acquire(scope: ConnectionScope): () => void {
    validateScope(scope);
    const now = readClock(this.now);
    const actorKey = scopedKey(scope.tenantId, scope.actorId);
    const documentKey = scopedKey(
      scope.tenantId,
      scope.documentId,
      String(scope.generation),
    );

    this.consumeReconnectAttempt(actorKey, now);

    const actorCount = this.actors.get(actorKey) ?? 0;
    const documentCount = this.documents.get(documentKey) ?? 0;
    const tenantCount = this.tenants.get(scope.tenantId) ?? 0;
    if (actorCount >= this.limits.maxConnectionsPerActor) {
      throw new RuntimePolicyError("actor_connection_quota");
    }
    if (documentCount >= this.limits.maxConnectionsPerDocument) {
      throw new RuntimePolicyError("document_connection_quota");
    }
    if (tenantCount >= this.limits.maxConnectionsPerTenant) {
      throw new RuntimePolicyError("tenant_connection_quota");
    }
    if (this.active >= this.limits.maxConnections) {
      throw new RuntimePolicyError("global_connection_quota");
    }

    this.active += 1;
    increment(this.actors, actorKey);
    increment(this.documents, documentKey);
    increment(this.tenants, scope.tenantId);

    let released = false;
    return () => {
      if (released) return;
      released = true;
      this.active = Math.max(0, this.active - 1);
      decrement(this.actors, actorKey);
      decrement(this.documents, documentKey);
      decrement(this.tenants, scope.tenantId);
    };
  }

  activeConnections(): number {
    return this.active;
  }

  private consumeReconnectAttempt(actorKey: string, now: number): void {
    this.pruneReconnects(now);
    const cutoff = now - this.limits.reconnectWindowMs;
    const current = this.reconnects.get(actorKey);
    if (current && now < current.lastObservedAt) {
      throw new RuntimePolicyError("policy_clock_invalid");
    }
    const timestamps = current?.timestamps ?? [];
    while ((timestamps[0] ?? Number.POSITIVE_INFINITY) <= cutoff) {
      timestamps.shift();
    }
    if (timestamps.length >= this.limits.maxReconnectAttempts) {
      throw new RuntimePolicyError("reconnect_storm_denied");
    }
    timestamps.push(now);
    this.reconnects.set(actorKey, { lastObservedAt: now, timestamps });
  }

  private pruneReconnects(now: number): void {
    const cutoff = now - this.limits.reconnectWindowMs;
    for (const [key, history] of this.reconnects) {
      if (now < history.lastObservedAt) {
        throw new RuntimePolicyError("policy_clock_invalid");
      }
      while ((history.timestamps[0] ?? Number.POSITIVE_INFINITY) <= cutoff) {
        history.timestamps.shift();
      }
      if (history.timestamps.length === 0) this.reconnects.delete(key);
    }
  }
}

export interface IngressPolicyLimits {
  maxBytesPerWindow: number;
  maxMessagesPerWindow: number;
  windowMs: number;
}

interface IngressEvent {
  bytes: number;
  observedAt: number;
}

interface SocketIngressState {
  bytes: number;
  events: IngressEvent[];
  lastObservedAt: number;
}

export interface IngressBudgetRemaining {
  bytes: number;
  messages: number;
}

/**
 * Applies independent rolling message and byte budgets per authenticated
 * socket. Exceeding either budget is a backpressure signal that callers should
 * map to a bounded protocol denial/close, never to an unbounded queue.
 */
export class RuntimeIngressPolicy {
  private readonly sockets = new Map<string, SocketIngressState>();

  constructor(
    private readonly limits: IngressPolicyLimits,
    private readonly now: () => number = Date.now,
  ) {
    validatePositiveLimits(Object.values(limits));
  }

  consume(socketId: string, byteLength: number): IngressBudgetRemaining {
    if (!validKeyPart(socketId) || !positiveSafeInteger(byteLength)) {
      throw new RuntimePolicyError("policy_input_invalid");
    }
    const now = readClock(this.now);
    const state = this.sockets.get(socketId) ?? {
      bytes: 0,
      events: [],
      lastObservedAt: now,
    };
    if (now < state.lastObservedAt) {
      throw new RuntimePolicyError("policy_clock_invalid");
    }
    state.lastObservedAt = now;
    pruneIngress(state, now - this.limits.windowMs);

    if (state.events.length >= this.limits.maxMessagesPerWindow) {
      throw new RuntimePolicyError("ingress_message_budget_exceeded");
    }
    if (state.bytes + byteLength > this.limits.maxBytesPerWindow) {
      throw new RuntimePolicyError("ingress_byte_budget_exceeded");
    }

    state.events.push({ bytes: byteLength, observedAt: now });
    state.bytes += byteLength;
    this.sockets.set(socketId, state);
    return {
      bytes: this.limits.maxBytesPerWindow - state.bytes,
      messages: this.limits.maxMessagesPerWindow - state.events.length,
    };
  }

  release(socketId: string): void {
    if (!validKeyPart(socketId)) {
      throw new RuntimePolicyError("policy_input_invalid");
    }
    this.sockets.delete(socketId);
  }
}

export interface AwarenessPolicyLimits {
  maxBytes: number;
  maxDepth: number;
  maxStates: number;
}

export interface AwarenessEnvelope {
  byteLength: number;
  states: readonly unknown[];
}

/** Safe metadata for bounded metrics; it never contains decoded state. */
export interface AwarenessSummary {
  byteLength: number;
  maxDepth: number;
  stateCount: number;
}

/**
 * Validates already-decoded awareness state against the exact raw message byte
 * length supplied by the transport decoder. Only JSON-shaped values are
 * accepted. The function returns counts only and never serializes content.
 */
export function validateAwarenessEnvelope(
  envelope: AwarenessEnvelope,
  limits: AwarenessPolicyLimits,
): AwarenessSummary {
  validatePositiveLimits([limits.maxBytes, limits.maxDepth, limits.maxStates]);
  if (
    !nonNegativeSafeInteger(envelope.byteLength) ||
    !Array.isArray(envelope.states)
  ) {
    throw new RuntimePolicyError("awareness_structure_invalid");
  }
  if (envelope.byteLength > limits.maxBytes) {
    throw new RuntimePolicyError("awareness_bytes_exceeded");
  }
  if (envelope.states.length > limits.maxStates) {
    throw new RuntimePolicyError("awareness_state_count_exceeded");
  }

  let observedDepth = 0;
  let observedNodes = 0;
  const seen = new WeakSet<object>();
  const pending = envelope.states.map((value) => ({ depth: 0, value }));
  while (pending.length > 0) {
    const current = pending.pop();
    if (!current) break;
    observedNodes += 1;
    if (observedNodes > Math.max(1, limits.maxBytes)) {
      throw new RuntimePolicyError("awareness_structure_invalid");
    }
    observedDepth = Math.max(observedDepth, current.depth);
    if (current.depth > limits.maxDepth) {
      throw new RuntimePolicyError("awareness_depth_exceeded");
    }
    if (isJsonPrimitive(current.value)) continue;
    if (typeof current.value !== "object" || current.value === null) {
      throw new RuntimePolicyError("awareness_structure_invalid");
    }
    if (seen.has(current.value)) {
      throw new RuntimePolicyError("awareness_structure_invalid");
    }
    seen.add(current.value);
    if (Array.isArray(current.value)) {
      for (const value of current.value) {
        pending.push({ depth: current.depth + 1, value });
      }
      continue;
    }
    const prototype = Object.getPrototypeOf(current.value);
    if (prototype !== Object.prototype && prototype !== null) {
      throw new RuntimePolicyError("awareness_structure_invalid");
    }
    if (Object.getOwnPropertySymbols(current.value).length > 0) {
      throw new RuntimePolicyError("awareness_structure_invalid");
    }
    for (const descriptor of Object.values(
      Object.getOwnPropertyDescriptors(current.value),
    )) {
      if (!("value" in descriptor)) {
        throw new RuntimePolicyError("awareness_structure_invalid");
      }
      pending.push({ depth: current.depth + 1, value: descriptor.value });
    }
  }

  return {
    byteLength: envelope.byteLength,
    maxDepth: observedDepth,
    stateCount: envelope.states.length,
  };
}

function pruneIngress(state: SocketIngressState, cutoff: number): void {
  while ((state.events[0]?.observedAt ?? Number.POSITIVE_INFINITY) <= cutoff) {
    const expired = state.events.shift();
    if (expired) state.bytes = Math.max(0, state.bytes - expired.bytes);
  }
}

function validatePositiveLimits(values: readonly number[]): void {
  if (!values.every(positiveSafeInteger)) {
    throw new RuntimePolicyError("policy_configuration_invalid");
  }
}

function validateScope(scope: ConnectionScope): void {
  if (
    !validKeyPart(scope.actorId) ||
    !validKeyPart(scope.documentId) ||
    !validKeyPart(scope.tenantId) ||
    !positiveSafeInteger(scope.generation)
  ) {
    throw new RuntimePolicyError("policy_input_invalid");
  }
}

function readClock(now: () => number): number {
  const value = now();
  if (!nonNegativeSafeInteger(value)) {
    throw new RuntimePolicyError("policy_clock_invalid");
  }
  return value;
}

function validKeyPart(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= 256 &&
    !value.includes("\u0000")
  );
}

function positiveSafeInteger(value: number): boolean {
  return Number.isSafeInteger(value) && value > 0;
}

function nonNegativeSafeInteger(value: number): boolean {
  return Number.isSafeInteger(value) && value >= 0;
}

function scopedKey(...parts: readonly string[]): string {
  return parts.join("\u0000");
}

function increment(values: Map<string, number>, key: string): void {
  values.set(key, (values.get(key) ?? 0) + 1);
}

function decrement(values: Map<string, number>, key: string): void {
  const next = (values.get(key) ?? 1) - 1;
  if (next <= 0) values.delete(key);
  else values.set(key, next);
}

function isJsonPrimitive(
  value: unknown,
): value is boolean | null | number | string {
  return (
    value === null ||
    typeof value === "boolean" ||
    (typeof value === "number" && Number.isFinite(value)) ||
    typeof value === "string"
  );
}
