import {
  createHash,
  createHmac,
  randomBytes,
  randomUUID,
  timingSafeEqual,
} from "node:crypto";

export const EXCALIDRAW_RUNTIME_TARGET = {
  collaborationProvider: "hocuspocus@4.6.0",
  coordination: "redis-cloud-paid-multi-az",
  coordinationPurpose: "fanout-and-revocation-only",
  drainTimeoutMs: 60_000,
  engine: "@excalidraw/excalidraw@0.18.1",
  grantTtlMs: 30_000,
  maxPersistenceDebounceMs: 10_000,
  node: "24.15.0",
  persistence: "neon-yjs-binary-checkpoint",
  persistenceDebounceMs: 2_000,
  providerExit: "b2-private-immutable-portable-scene",
  region: "singapore",
  renderInstanceType: "standard",
  replicas: 2,
  rpoMs: 10_000,
  rtoMs: 5 * 60_000,
  yjs: "13.6.27",
} as const;

export type RuntimeCapability = "edit" | "present" | "view";
export type RuntimeCredentialKind =
  "coordination" | "grant_signing" | "persistence" | "snapshot_binding";
export type RuntimeDependency =
  "control_plane" | "coordination" | "persistence" | "snapshot";
export type RuntimeMode = "enabled" | "export_only" | "off" | "read_only";
export type RuntimeNodeState =
  "degraded" | "draining" | "ready" | "starting" | "stopped";

export type RuntimeOperationsErrorCode =
  | "credential_invalid"
  | "dependency_unavailable"
  | "document_quota_exceeded"
  | "grant_expired"
  | "grant_invalid_or_replayed"
  | "grant_scope_invalid"
  | "metric_cardinality_exceeded"
  | "metric_label_denied"
  | "new_connections_disabled"
  | "node_not_ready"
  | "runtime_force_off"
  | "stale_generation"
  | "stale_writer_fence"
  | "write_capability_denied"
  | "write_disabled"
  | "writer_lease_active";

export class RuntimeOperationsError extends Error {
  constructor(readonly code: RuntimeOperationsErrorCode) {
    super(code);
    this.name = "RuntimeOperationsError";
  }
}

export interface RuntimeDocumentScope {
  documentId: string;
  generation: number;
  tenantId: string;
}

export interface RuntimeGrantRequest extends RuntimeDocumentScope {
  actorId: string;
  capability: RuntimeCapability;
  sessionId: string;
}

export interface RuntimeGrant {
  expiresAt: number;
  grant: string;
  providerDocumentName: string;
}

export interface RuntimeConnection {
  capability: RuntimeCapability;
  id: string;
  nodeId: string;
  providerDocumentName: string;
  readOnly: boolean;
  scope: RuntimeDocumentScope;
}

export interface RuntimeDocumentStatus {
  fenceEpoch: number;
  generation: number;
  persistedWatermark: number;
  providerDocumentName: string;
  watermark: number;
  writerNodeId?: string;
}

type MetricName =
  | "collab_connections"
  | "collab_cost_budget"
  | "collab_dependency_state"
  | "collab_drain"
  | "collab_persistence"
  | "collab_quota_budget"
  | "collab_quota_reject"
  | "collab_runtime_state"
  | "collab_secret_rotation";

const METRIC_LABEL_POLICY: Record<
  MetricName,
  Record<string, readonly string[]>
> = {
  collab_connections: {
    capability: ["edit", "present", "view"],
    outcome: ["accepted", "closed", "rejected"],
  },
  collab_cost_budget: {
    outcome: ["freeze", "hard_limit", "notify", "ok", "quote_pending"],
  },
  collab_dependency_state: {
    dependency: ["control_plane", "coordination", "persistence", "snapshot"],
    outcome: ["down", "up"],
  },
  collab_drain: {
    outcome: ["complete", "failed", "started"],
  },
  collab_persistence: {
    outcome: ["fenced", "flushed", "unavailable"],
  },
  collab_quota_budget: {
    outcome: ["deny", "notify", "ok", "throttle"],
    scope: ["document", "runtime"],
  },
  collab_quota_reject: {
    scope: ["document", "runtime"],
  },
  collab_runtime_state: {
    state: ["degraded", "draining", "ready", "starting", "stopped"],
  },
  collab_secret_rotation: {
    credential: [
      "coordination",
      "grant_signing",
      "persistence",
      "snapshot_binding",
    ],
    outcome: ["accepted", "rejected", "rotated"],
  },
};

export interface RuntimeMetricPoint {
  labels: Readonly<Record<string, string>>;
  name: MetricName;
  value: number;
}

export interface RuntimeLogEvent {
  event:
    | "connection"
    | "dependency"
    | "drain"
    | "kill_switch"
    | "persistence"
    | "rotation";
  outcome: "denied" | "failed" | "ok" | "started";
  reason:
    "dependency_unavailable" | "disabled" | "none" | "quota" | "stale_fence";
}

export class BoundedRuntimeTelemetry {
  private readonly logs: RuntimeLogEvent[] = [];
  private readonly metrics = new Map<string, RuntimeMetricPoint>();

  constructor(
    private readonly maxSeries = 64,
    private readonly maxLogs = 256,
  ) {
    if (
      !Number.isSafeInteger(maxSeries) ||
      maxSeries < 1 ||
      !Number.isSafeInteger(maxLogs) ||
      maxLogs < 1
    ) {
      throw new RuntimeOperationsError("metric_cardinality_exceeded");
    }
  }

  recordMetric(
    name: MetricName,
    value: number,
    labels: Readonly<Record<string, string>>,
  ): void {
    if (!Number.isFinite(value)) {
      throw new RuntimeOperationsError("metric_label_denied");
    }
    const policy = METRIC_LABEL_POLICY[name];
    const expectedKeys = Object.keys(policy).sort();
    const actualKeys = Object.keys(labels).sort();
    if (
      expectedKeys.length !== actualKeys.length ||
      expectedKeys.some((key, index) => key !== actualKeys[index])
    ) {
      throw new RuntimeOperationsError("metric_label_denied");
    }
    for (const [key, allowedValues] of Object.entries(policy)) {
      const labelValue = labels[key];
      if (labelValue === undefined || !allowedValues.includes(labelValue)) {
        throw new RuntimeOperationsError("metric_label_denied");
      }
    }
    const seriesKey = `${name}:${actualKeys.map((key) => `${key}=${labels[key]}`).join(",")}`;
    if (!this.metrics.has(seriesKey) && this.metrics.size >= this.maxSeries) {
      throw new RuntimeOperationsError("metric_cardinality_exceeded");
    }
    this.metrics.set(seriesKey, { labels: { ...labels }, name, value });
  }

  recordLog(event: RuntimeLogEvent): void {
    if (this.logs.length === this.maxLogs) this.logs.shift();
    this.logs.push({ ...event });
  }

  snapshot(): {
    logs: RuntimeLogEvent[];
    metrics: RuntimeMetricPoint[];
  } {
    return {
      logs: this.logs.map((event) => ({ ...event })),
      metrics: [...this.metrics.values()].map((point) => ({
        ...point,
        labels: { ...point.labels },
      })),
    };
  }
}

interface StoredCredential {
  digest: Buffer;
  keyId: string;
}

interface CredentialSlot {
  current: StoredCredential;
  previous?: StoredCredential & { validUntil: number };
}

export class RuntimeCredentialRing {
  private readonly slots = new Map<RuntimeCredentialKind, CredentialSlot>();

  constructor(
    initial: Record<RuntimeCredentialKind, { keyId: string; secret: string }>,
    private readonly now: () => number = Date.now,
    private readonly telemetry?: BoundedRuntimeTelemetry,
  ) {
    for (const kind of credentialKinds) {
      const credential = initial[kind];
      this.slots.set(kind, { current: storeCredential(credential) });
    }
  }

  authenticate(
    kind: RuntimeCredentialKind,
    keyId: string,
    secret: string,
  ): boolean {
    const slot = this.requiredSlot(kind);
    const accepted =
      matchesCredential(slot.current, keyId, secret) ||
      (slot.previous !== undefined &&
        slot.previous.validUntil >= this.now() &&
        matchesCredential(slot.previous, keyId, secret));
    this.telemetry?.recordMetric("collab_secret_rotation", 1, {
      credential: kind,
      outcome: accepted ? "accepted" : "rejected",
    });
    return accepted;
  }

  currentKeyId(kind: RuntimeCredentialKind): string {
    return this.requiredSlot(kind).current.keyId;
  }

  rotate(
    kind: RuntimeCredentialKind,
    replacement: { keyId: string; secret: string },
    overlapMs: number,
  ): void {
    const maxOverlapMs =
      kind === "snapshot_binding" ? 366 * 24 * 60 * 60_000 : 15 * 60_000;
    if (
      !Number.isSafeInteger(overlapMs) ||
      overlapMs < 0 ||
      overlapMs > maxOverlapMs
    ) {
      throw new RuntimeOperationsError("credential_invalid");
    }
    const slot = this.requiredSlot(kind);
    if (slot.current.keyId === replacement.keyId) {
      throw new RuntimeOperationsError("credential_invalid");
    }
    this.slots.set(kind, {
      current: storeCredential(replacement),
      previous: { ...slot.current, validUntil: this.now() + overlapMs },
    });
    this.telemetry?.recordMetric("collab_secret_rotation", 1, {
      credential: kind,
      outcome: "rotated",
    });
    this.telemetry?.recordLog({
      event: "rotation",
      outcome: "ok",
      reason: "none",
    });
  }

  retireExpired(): void {
    for (const [kind, slot] of this.slots) {
      if (
        slot.previous !== undefined &&
        slot.previous.validUntil < this.now()
      ) {
        this.slots.set(kind, { current: slot.current });
      }
    }
  }

  private requiredSlot(kind: RuntimeCredentialKind): CredentialSlot {
    const slot = this.slots.get(kind);
    if (slot === undefined) {
      throw new RuntimeOperationsError("credential_invalid");
    }
    return slot;
  }
}

interface GrantClaims extends RuntimeGrantRequest {
  expiresAt: number;
  providerDocumentName: string;
}

interface MutableRuntimeConnection extends RuntimeConnection {
  readOnly: boolean;
}

interface MutableRuntimeDocument {
  fenceEpoch: number;
  generation: number;
  persistedWatermark: number;
  providerDocumentName: string;
  watermark: number;
  writerLeaseExpiresAt?: number;
  writerNodeId?: string;
}

export class SharedRuntimeCoordinator {
  private readonly connections = new Map<string, MutableRuntimeConnection>();
  private readonly dependencies = new Map<RuntimeDependency, boolean>([
    ["control_plane", true],
    ["coordination", true],
    ["persistence", true],
    ["snapshot", true],
  ]);
  private readonly documents = new Map<string, MutableRuntimeDocument>();
  private readonly grants = new Map<string, GrantClaims>();
  private mode: RuntimeMode = "enabled";

  constructor(
    private readonly opaqueDocumentKey: Uint8Array,
    readonly credentials: RuntimeCredentialRing,
    readonly telemetry: BoundedRuntimeTelemetry,
    private readonly now: () => number = Date.now,
    private readonly maxConnections = 100,
    private readonly maxDocumentConnections = 50,
    private readonly maxOutstandingGrants = 10_000,
    private readonly writerLeaseMs = 15_000,
  ) {
    if (
      opaqueDocumentKey.byteLength < 32 ||
      !Number.isSafeInteger(maxConnections) ||
      maxConnections < 1 ||
      !Number.isSafeInteger(maxDocumentConnections) ||
      maxDocumentConnections < 1 ||
      !Number.isSafeInteger(maxOutstandingGrants) ||
      maxOutstandingGrants < 1 ||
      !Number.isSafeInteger(writerLeaseMs) ||
      writerLeaseMs < 1
    ) {
      throw new RuntimeOperationsError("credential_invalid");
    }
  }

  dependencyAvailable(dependency: RuntimeDependency): boolean {
    return this.dependencies.get(dependency) === true;
  }

  criticalDependenciesAvailable(): boolean {
    return (
      this.dependencyAvailable("control_plane") &&
      this.dependencyAvailable("coordination") &&
      this.dependencyAvailable("persistence")
    );
  }

  currentMode(): RuntimeMode {
    return this.mode;
  }

  setDependency(dependency: RuntimeDependency, available: boolean): void {
    this.dependencies.set(dependency, available);
    this.telemetry.recordMetric("collab_dependency_state", available ? 1 : 0, {
      dependency,
      outcome: available ? "up" : "down",
    });
    this.telemetry.recordLog({
      event: "dependency",
      outcome: available ? "ok" : "failed",
      reason: available ? "none" : "dependency_unavailable",
    });
  }

  setMode(mode: RuntimeMode): void {
    this.mode = mode;
    if (mode === "read_only") {
      for (const connection of this.connections.values()) {
        connection.readOnly = true;
      }
    } else if (mode === "export_only" || mode === "off") {
      this.closeAllConnections();
    }
    this.telemetry.recordLog({
      event: "kill_switch",
      outcome: "ok",
      reason: mode === "enabled" ? "none" : "disabled",
    });
  }

  issueGrant(
    request: RuntimeGrantRequest,
    ttlMs: number = EXCALIDRAW_RUNTIME_TARGET.grantTtlMs,
  ): RuntimeGrant {
    validateGrantRequest(request);
    if (!this.criticalDependenciesAvailable()) {
      throw new RuntimeOperationsError("dependency_unavailable");
    }
    if (this.mode === "export_only" || this.mode === "off") {
      throw new RuntimeOperationsError("new_connections_disabled");
    }
    if (this.mode === "read_only" && request.capability !== "view") {
      throw new RuntimeOperationsError("write_disabled");
    }
    if (!Number.isSafeInteger(ttlMs) || ttlMs <= 0 || ttlMs > 60_000) {
      throw new RuntimeOperationsError("grant_scope_invalid");
    }
    this.purgeExpiredGrants();
    if (this.grants.size >= this.maxOutstandingGrants) {
      this.telemetry.recordMetric("collab_quota_reject", 1, {
        scope: "runtime",
      });
      throw new RuntimeOperationsError("document_quota_exceeded");
    }
    const document = this.document(request);
    if (document.generation !== request.generation) {
      throw new RuntimeOperationsError("stale_generation");
    }
    const grant = randomBytes(32).toString("base64url");
    const expiresAt = this.now() + ttlMs;
    const claims: GrantClaims = {
      ...request,
      expiresAt,
      providerDocumentName: document.providerDocumentName,
    };
    this.grants.set(hashGrant(grant), claims);
    return {
      expiresAt,
      grant,
      providerDocumentName: document.providerDocumentName,
    };
  }

  consumeGrant(nodeId: string, issued: RuntimeGrant): RuntimeConnection {
    assertIdentifier(nodeId);
    validateIssuedGrant(issued);
    if (!this.criticalDependenciesAvailable()) {
      throw new RuntimeOperationsError("dependency_unavailable");
    }
    if (this.mode === "export_only" || this.mode === "off") {
      throw new RuntimeOperationsError("new_connections_disabled");
    }
    const digest = hashGrant(issued.grant);
    const claims = this.grants.get(digest);
    this.grants.delete(digest);
    if (claims === undefined) {
      throw new RuntimeOperationsError("grant_invalid_or_replayed");
    }
    if (this.mode === "read_only" && claims.capability !== "view") {
      throw new RuntimeOperationsError("write_disabled");
    }
    if (claims.expiresAt < this.now()) {
      throw new RuntimeOperationsError("grant_expired");
    }
    const document = this.document(claims);
    if (
      claims.expiresAt !== issued.expiresAt ||
      claims.providerDocumentName !== issued.providerDocumentName ||
      document.providerDocumentName !== issued.providerDocumentName
    ) {
      throw new RuntimeOperationsError("grant_scope_invalid");
    }
    if (document.generation !== claims.generation) {
      throw new RuntimeOperationsError("stale_generation");
    }
    if (this.connections.size >= this.maxConnections) {
      this.telemetry.recordMetric("collab_quota_reject", 1, {
        scope: "runtime",
      });
      throw new RuntimeOperationsError("document_quota_exceeded");
    }
    const documentConnectionCount = [...this.connections.values()].filter(
      (connection) => scopeKey(connection.scope) === scopeKey(claims),
    ).length;
    if (documentConnectionCount >= this.maxDocumentConnections) {
      this.telemetry.recordMetric("collab_quota_reject", 1, {
        scope: "document",
      });
      throw new RuntimeOperationsError("document_quota_exceeded");
    }
    const connection: MutableRuntimeConnection = {
      capability: claims.capability,
      id: randomUUID(),
      nodeId,
      providerDocumentName: claims.providerDocumentName,
      readOnly: claims.capability === "view",
      scope: scopeOf(claims),
    };
    this.connections.set(connection.id, connection);
    this.telemetry.recordMetric("collab_connections", 1, {
      capability: connection.capability,
      outcome: "accepted",
    });
    this.telemetry.recordLog({
      event: "connection",
      outcome: "ok",
      reason: "none",
    });
    return cloneConnection(connection);
  }

  ensureWriterLease(
    nodeId: string,
    scope: RuntimeDocumentScope,
  ): number | undefined {
    this.assertPersistenceAvailable();
    const document = this.document(scope);
    if (document.generation !== scope.generation) {
      throw new RuntimeOperationsError("stale_generation");
    }
    if (
      document.writerNodeId === undefined ||
      (document.writerNodeId !== nodeId &&
        (document.writerLeaseExpiresAt ?? 0) <= this.now())
    ) {
      document.writerNodeId = nodeId;
      document.fenceEpoch += 1;
    }
    if (document.writerNodeId !== nodeId) return undefined;
    document.writerLeaseExpiresAt = this.now() + this.writerLeaseMs;
    return document.fenceEpoch;
  }

  forceWriterTakeover(nodeId: string, scope: RuntimeDocumentScope): number {
    this.assertPersistenceAvailable();
    const document = this.document(scope);
    if (document.generation !== scope.generation) {
      throw new RuntimeOperationsError("stale_generation");
    }
    if (
      document.writerNodeId !== undefined &&
      document.writerNodeId !== nodeId &&
      (document.writerLeaseExpiresAt ?? 0) > this.now()
    ) {
      throw new RuntimeOperationsError("writer_lease_active");
    }
    if (document.writerNodeId !== nodeId) {
      document.writerNodeId = nodeId;
      document.fenceEpoch += 1;
    }
    document.writerLeaseExpiresAt = this.now() + this.writerLeaseMs;
    return document.fenceEpoch;
  }

  mutate(connectionId: string): number {
    const connection = this.connections.get(connectionId);
    if (connection === undefined) {
      throw new RuntimeOperationsError("grant_scope_invalid");
    }
    if (!this.criticalDependenciesAvailable()) {
      throw new RuntimeOperationsError("dependency_unavailable");
    }
    if (this.mode !== "enabled" || connection.readOnly) {
      throw new RuntimeOperationsError("write_disabled");
    }
    if (
      connection.capability !== "edit" &&
      connection.capability !== "present"
    ) {
      throw new RuntimeOperationsError("write_capability_denied");
    }
    const document = this.document(connection.scope);
    if (document.generation !== connection.scope.generation) {
      throw new RuntimeOperationsError("stale_generation");
    }
    document.watermark += 1;
    return document.watermark;
  }

  flush(
    nodeId: string,
    scope: RuntimeDocumentScope,
    fenceEpoch: number,
  ): number {
    this.assertPersistenceAvailable();
    const document = this.document(scope);
    if (
      document.generation !== scope.generation ||
      document.writerNodeId !== nodeId ||
      document.fenceEpoch !== fenceEpoch
    ) {
      this.telemetry.recordMetric("collab_persistence", 1, {
        outcome: "fenced",
      });
      this.telemetry.recordLog({
        event: "persistence",
        outcome: "denied",
        reason: "stale_fence",
      });
      throw new RuntimeOperationsError("stale_writer_fence");
    }
    document.persistedWatermark = document.watermark;
    document.writerLeaseExpiresAt = this.now() + this.writerLeaseMs;
    this.telemetry.recordMetric("collab_persistence", 1, {
      outcome: "flushed",
    });
    this.telemetry.recordLog({
      event: "persistence",
      outcome: "ok",
      reason: "none",
    });
    return document.persistedWatermark;
  }

  makeNodeReadOnly(nodeId: string): void {
    for (const connection of this.connections.values()) {
      if (connection.nodeId === nodeId) connection.readOnly = true;
    }
  }

  releaseNodeConnections(nodeId: string): void {
    for (const [connectionId, connection] of this.connections) {
      if (connection.nodeId !== nodeId) continue;
      this.connections.delete(connectionId);
      this.telemetry.recordMetric("collab_connections", 0, {
        capability: connection.capability,
        outcome: "closed",
      });
    }
  }

  releaseWriterLeases(nodeId: string): void {
    for (const document of this.documents.values()) {
      if (document.writerNodeId !== nodeId) continue;
      document.writerNodeId = undefined;
      document.writerLeaseExpiresAt = undefined;
    }
  }

  activeConnections(nodeId?: string): number {
    return nodeId === undefined
      ? this.connections.size
      : [...this.connections.values()].filter(
          (connection) => connection.nodeId === nodeId,
        ).length;
  }

  connection(connectionId: string): RuntimeConnection | undefined {
    const connection = this.connections.get(connectionId);
    return connection === undefined ? undefined : cloneConnection(connection);
  }

  documentStatus(scope: RuntimeDocumentScope): RuntimeDocumentStatus {
    const document = this.document(scope);
    return { ...document };
  }

  swapGeneration(scope: RuntimeDocumentScope): RuntimeDocumentScope {
    const document = this.document(scope);
    if (document.generation !== scope.generation) {
      throw new RuntimeOperationsError("stale_generation");
    }
    document.generation += 1;
    document.fenceEpoch += 1;
    document.persistedWatermark = 0;
    document.watermark = 0;
    document.providerDocumentName = this.providerDocumentName({
      ...scope,
      generation: document.generation,
    });
    document.writerLeaseExpiresAt = undefined;
    document.writerNodeId = undefined;
    for (const connection of this.connections.values()) {
      if (scopeKey(connection.scope) === scopeKey(scope)) {
        connection.readOnly = true;
      }
    }
    return { ...scope, generation: document.generation };
  }

  private assertPersistenceAvailable(): void {
    if (!this.dependencyAvailable("persistence")) {
      this.telemetry.recordMetric("collab_persistence", 1, {
        outcome: "unavailable",
      });
      throw new RuntimeOperationsError("dependency_unavailable");
    }
  }

  private document(scope: RuntimeDocumentScope): MutableRuntimeDocument {
    const key = documentIdentity(scope);
    const existing = this.documents.get(key);
    if (existing !== undefined) return existing;
    const document: MutableRuntimeDocument = {
      fenceEpoch: 0,
      generation: scope.generation,
      persistedWatermark: 0,
      providerDocumentName: this.providerDocumentName(scope),
      watermark: 0,
    };
    this.documents.set(key, document);
    return document;
  }

  private closeAllConnections(): void {
    for (const connection of this.connections.values()) {
      this.telemetry.recordMetric("collab_connections", 0, {
        capability: connection.capability,
        outcome: "closed",
      });
    }
    this.connections.clear();
  }

  private providerDocumentName(scope: RuntimeDocumentScope): string {
    return createHmac("sha256", this.opaqueDocumentKey)
      .update(scopeKey(scope))
      .digest("base64url");
  }

  private purgeExpiredGrants(): void {
    for (const [digest, claims] of this.grants) {
      if (claims.expiresAt < this.now()) this.grants.delete(digest);
    }
  }
}

export class ExcalidrawRuntimeNode {
  private readonly connections = new Set<string>();
  private readonly leases = new Map<string, number>();
  private state: RuntimeNodeState = "starting";

  constructor(
    readonly nodeId: string,
    private readonly coordinator: SharedRuntimeCoordinator,
    private readonly telemetry: BoundedRuntimeTelemetry,
  ) {
    assertIdentifier(nodeId);
    this.recordState();
  }

  start(): void {
    if (!this.coordinator.criticalDependenciesAvailable()) {
      this.state = "degraded";
      this.recordState();
      throw new RuntimeOperationsError("dependency_unavailable");
    }
    this.state = "ready";
    this.recordState();
  }

  isReady(): boolean {
    return (
      this.state === "ready" &&
      (this.coordinator.currentMode() === "enabled" ||
        this.coordinator.currentMode() === "read_only") &&
      this.coordinator.criticalDependenciesAvailable()
    );
  }

  currentState(): RuntimeNodeState {
    return this.state;
  }

  openConnection(grant: RuntimeGrant): RuntimeConnection {
    if (!this.isReady()) {
      throw new RuntimeOperationsError("node_not_ready");
    }
    const connection = this.coordinator.consumeGrant(this.nodeId, grant);
    this.connections.add(connection.id);
    const lease = this.coordinator.ensureWriterLease(
      this.nodeId,
      connection.scope,
    );
    if (lease !== undefined) {
      this.leases.set(scopeKey(connection.scope), lease);
    }
    return connection;
  }

  mutate(connectionId: string): number {
    if (!this.isReady() || !this.connections.has(connectionId)) {
      throw new RuntimeOperationsError("write_disabled");
    }
    return this.coordinator.mutate(connectionId);
  }

  reconcileDependencies(): void {
    if (
      this.coordinator.currentMode() === "export_only" ||
      this.coordinator.currentMode() === "off" ||
      !this.coordinator.criticalDependenciesAvailable()
    ) {
      this.state = "degraded";
      this.coordinator.makeNodeReadOnly(this.nodeId);
      this.recordState();
    }
  }

  recover(): void {
    if (
      this.coordinator.currentMode() !== "enabled" ||
      !this.coordinator.criticalDependenciesAvailable()
    ) {
      throw new RuntimeOperationsError("dependency_unavailable");
    }
    this.coordinator.releaseNodeConnections(this.nodeId);
    this.connections.clear();
    this.state = "ready";
    this.recordState();
  }

  takeWriterLease(scope: RuntimeDocumentScope): number {
    const fence = this.coordinator.forceWriterTakeover(this.nodeId, scope);
    this.leases.set(scopeKey(scope), fence);
    return fence;
  }

  flush(scope: RuntimeDocumentScope): number {
    const fence = this.leases.get(scopeKey(scope));
    if (fence === undefined) {
      throw new RuntimeOperationsError("stale_writer_fence");
    }
    return this.coordinator.flush(this.nodeId, scope, fence);
  }

  drain(): void {
    if (this.state === "stopped") return;
    this.state = "draining";
    this.recordState();
    this.telemetry.recordMetric("collab_drain", 1, { outcome: "started" });
    this.telemetry.recordLog({
      event: "drain",
      outcome: "started",
      reason: "none",
    });
    try {
      for (const [key, fence] of this.leases) {
        const scope = parseScopeKey(key);
        this.coordinator.flush(this.nodeId, scope, fence);
      }
      this.coordinator.releaseNodeConnections(this.nodeId);
      this.coordinator.releaseWriterLeases(this.nodeId);
      this.connections.clear();
      this.state = "stopped";
      this.recordState();
      this.telemetry.recordMetric("collab_drain", 1, {
        outcome: "complete",
      });
      this.telemetry.recordLog({
        event: "drain",
        outcome: "ok",
        reason: "none",
      });
    } catch (error) {
      this.coordinator.makeNodeReadOnly(this.nodeId);
      this.coordinator.releaseNodeConnections(this.nodeId);
      this.coordinator.releaseWriterLeases(this.nodeId);
      this.connections.clear();
      this.state = "degraded";
      this.recordState();
      this.telemetry.recordMetric("collab_drain", 1, { outcome: "failed" });
      this.telemetry.recordLog({
        event: "drain",
        outcome: "failed",
        reason: "dependency_unavailable",
      });
      throw error;
    }
  }

  crash(): void {
    this.coordinator.releaseNodeConnections(this.nodeId);
    this.coordinator.releaseWriterLeases(this.nodeId);
    this.connections.clear();
    this.state = "stopped";
    this.recordState();
  }

  private recordState(): void {
    this.telemetry.recordMetric("collab_runtime_state", 1, {
      state: this.state,
    });
  }
}

export interface RuntimeCostQuote {
  b2MonthlyUsd: number;
  coordinationMonthlyUsd?: number;
  neonMonthlyUsd: number;
  renderMonthlyUsdPerInstance?: number;
}

export interface RuntimeCostAssessment {
  freezeLimitUsd: number;
  hardLimitUsd: number;
  notifyLimitUsd: number;
  outcome: "freeze" | "hard_limit" | "notify" | "ok" | "quote_pending";
  quotedMonthlyUsd?: number;
}

export function assessRuntimeCost(
  quote: RuntimeCostQuote,
  telemetry: BoundedRuntimeTelemetry,
  {
    freezeLimitUsd,
    hardLimitUsd,
    notifyLimitUsd,
  }: {
    freezeLimitUsd: number;
    hardLimitUsd: number;
    notifyLimitUsd: number;
  },
): RuntimeCostAssessment {
  const quoteValues = [
    quote.b2MonthlyUsd,
    quote.neonMonthlyUsd,
    quote.coordinationMonthlyUsd,
    quote.renderMonthlyUsdPerInstance,
  ].filter((value): value is number => value !== undefined);
  if (
    quoteValues.some((value) => !Number.isFinite(value) || value < 0) ||
    !Number.isFinite(hardLimitUsd) ||
    !Number.isFinite(notifyLimitUsd) ||
    !Number.isFinite(freezeLimitUsd) ||
    notifyLimitUsd <= 0 ||
    freezeLimitUsd <= notifyLimitUsd ||
    hardLimitUsd <= freezeLimitUsd
  ) {
    throw new RuntimeOperationsError("metric_label_denied");
  }
  let outcome: RuntimeCostAssessment["outcome"];
  let quotedMonthlyUsd: number | undefined;
  if (
    quote.coordinationMonthlyUsd === undefined ||
    quote.renderMonthlyUsdPerInstance === undefined
  ) {
    outcome = "quote_pending";
  } else {
    quotedMonthlyUsd =
      quote.renderMonthlyUsdPerInstance * EXCALIDRAW_RUNTIME_TARGET.replicas +
      quote.coordinationMonthlyUsd +
      quote.neonMonthlyUsd +
      quote.b2MonthlyUsd;
    outcome =
      quotedMonthlyUsd >= hardLimitUsd
        ? "hard_limit"
        : quotedMonthlyUsd >= freezeLimitUsd
          ? "freeze"
          : quotedMonthlyUsd >= notifyLimitUsd
            ? "notify"
            : "ok";
  }
  telemetry.recordMetric("collab_cost_budget", quotedMonthlyUsd ?? 0, {
    outcome,
  });
  return {
    freezeLimitUsd,
    hardLimitUsd,
    notifyLimitUsd,
    outcome,
    quotedMonthlyUsd,
  };
}

export interface RuntimeQuotaAssessment {
  limit: number;
  outcome: "deny" | "notify" | "ok" | "throttle";
  scope: "document" | "runtime";
  used: number;
}

export function assessRuntimeQuota(
  used: number,
  limit: number,
  scope: RuntimeQuotaAssessment["scope"],
  telemetry: BoundedRuntimeTelemetry,
): RuntimeQuotaAssessment {
  if (
    !Number.isSafeInteger(used) ||
    used < 0 ||
    !Number.isSafeInteger(limit) ||
    limit < 1
  ) {
    throw new RuntimeOperationsError("metric_label_denied");
  }
  const ratio = used / limit;
  const outcome =
    ratio >= 1
      ? "deny"
      : ratio >= 0.85
        ? "throttle"
        : ratio >= 0.7
          ? "notify"
          : "ok";
  telemetry.recordMetric("collab_quota_budget", ratio, { outcome, scope });
  return { limit, outcome, scope, used };
}

const credentialKinds: RuntimeCredentialKind[] = [
  "coordination",
  "grant_signing",
  "persistence",
  "snapshot_binding",
];

function storeCredential(credential: {
  keyId: string;
  secret: string;
}): StoredCredential {
  if (
    !/^[A-Za-z0-9._:-]{3,64}$/.test(credential.keyId) ||
    Buffer.byteLength(credential.secret, "utf8") < 32
  ) {
    throw new RuntimeOperationsError("credential_invalid");
  }
  return {
    digest: createHash("sha256").update(credential.secret).digest(),
    keyId: credential.keyId,
  };
}

function matchesCredential(
  stored: StoredCredential,
  keyId: string,
  secret: string,
): boolean {
  if (stored.keyId !== keyId) return false;
  const candidate = createHash("sha256").update(secret).digest();
  return timingSafeEqual(stored.digest, candidate);
}

function validateGrantRequest(request: RuntimeGrantRequest): void {
  assertIdentifier(request.actorId);
  assertIdentifier(request.documentId);
  assertIdentifier(request.sessionId);
  assertIdentifier(request.tenantId);
  if (!Number.isSafeInteger(request.generation) || request.generation < 1) {
    throw new RuntimeOperationsError("grant_scope_invalid");
  }
  if (
    request.capability !== "edit" &&
    request.capability !== "present" &&
    request.capability !== "view"
  ) {
    throw new RuntimeOperationsError("grant_scope_invalid");
  }
}

function validateIssuedGrant(issued: RuntimeGrant): void {
  if (
    typeof issued.grant !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(issued.grant) ||
    typeof issued.providerDocumentName !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(issued.providerDocumentName) ||
    !Number.isSafeInteger(issued.expiresAt)
  ) {
    throw new RuntimeOperationsError("grant_scope_invalid");
  }
}

function assertIdentifier(value: string): void {
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/.test(value)) {
    throw new RuntimeOperationsError("grant_scope_invalid");
  }
}

function hashGrant(grant: string): string {
  return createHash("sha256").update(grant).digest("base64url");
}

function documentIdentity(scope: RuntimeDocumentScope): string {
  return `${scope.tenantId.length}:${scope.tenantId}:${scope.documentId.length}:${scope.documentId}`;
}

function scopeKey(scope: RuntimeDocumentScope): string {
  return `${Buffer.from(scope.tenantId).toString("base64url")}.${Buffer.from(
    scope.documentId,
  ).toString("base64url")}.${scope.generation}`;
}

function parseScopeKey(key: string): RuntimeDocumentScope {
  const [tenant, document, generation] = key.split(".");
  if (
    tenant === undefined ||
    document === undefined ||
    generation === undefined
  ) {
    throw new RuntimeOperationsError("grant_scope_invalid");
  }
  const parsedGeneration = Number(generation);
  if (!Number.isSafeInteger(parsedGeneration) || parsedGeneration < 1) {
    throw new RuntimeOperationsError("grant_scope_invalid");
  }
  return {
    documentId: Buffer.from(document, "base64url").toString("utf8"),
    generation: parsedGeneration,
    tenantId: Buffer.from(tenant, "base64url").toString("utf8"),
  };
}

function scopeOf(scope: RuntimeDocumentScope): RuntimeDocumentScope {
  return {
    documentId: scope.documentId,
    generation: scope.generation,
    tenantId: scope.tenantId,
  };
}

function cloneConnection(connection: RuntimeConnection): RuntimeConnection {
  return { ...connection, scope: scopeOf(connection.scope) };
}
