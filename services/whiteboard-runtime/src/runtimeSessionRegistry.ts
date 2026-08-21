import type {
  CollaborationCapability,
  CollaborationScope,
} from "./contracts.js";

export type RuntimeSessionRegistryErrorCode =
  | "document_scope_conflict"
  | "duplicate_session_reservation"
  | "invalid_session_reservation"
  | "reservation_not_found";

/**
 * Carries a bounded code only. Provider document names, tenant IDs, document
 * IDs, actors, and authority leases must never be appended to this error.
 */
export class RuntimeSessionRegistryError extends Error {
  constructor(readonly code: RuntimeSessionRegistryErrorCode) {
    super(code);
    this.name = "RuntimeSessionRegistryError";
  }
}

export type RuntimeSessionFailureStage = "authentication" | "load" | "setup";
export type RuntimeSessionPhase = "active" | "pending";

export interface RuntimeSessionAdmission {
  acquire(scope: CollaborationScope): () => void;
}

export interface RuntimeSessionRegistryMetrics {
  activeDocumentScopes: number;
  activeSessions: number;
  pendingSessions: number;
  reservationAcceptedTotal: number;
  reservationDeniedTotal: number;
  reservationReleasedTotal: number;
  rollbackAuthenticationTotal: number;
  rollbackLoadTotal: number;
  rollbackSetupTotal: number;
}

export interface RuntimeSessionRelease {
  capability?: CollaborationCapability;
  phase?: RuntimeSessionPhase;
  released: boolean;
}

export interface RuntimeSessionReservation {
  readonly scope: Readonly<CollaborationScope>;
  commit(): void;
  release(): RuntimeSessionRelease;
  rollback(stage: RuntimeSessionFailureStage): RuntimeSessionRelease;
}

interface CanonicalDocumentScope {
  documentId: string;
  generation: number;
  tenantId: string;
  writerFence: number;
}

interface ScopeReservation {
  canonicalKey: string;
  references: number;
  scope: CanonicalDocumentScope;
}

interface CanonicalOwner {
  providerDocumentName: string;
  references: number;
  writerFence: number;
}

interface SessionRecord {
  admissionRelease: () => void;
  phase: RuntimeSessionPhase;
  scope: Readonly<CollaborationScope>;
  token: symbol;
}

/**
 * Process-local transaction boundary for runtime sessions and provider
 * document scope ownership.
 *
 * `reserve` is deliberately synchronous. It checks both directions of the
 * provider/canonical document mapping, acquires admission capacity, and
 * installs the session in one event-loop turn. Callers may then perform async
 * setup/load work and either `commit` on `connected`, or `rollback` from the
 * failing lifecycle hook. Disconnect release is idempotent.
 */
export class RuntimeSessionRegistry {
  private readonly canonicalOwners = new Map<string, CanonicalOwner>();
  private readonly providerScopes = new Map<string, ScopeReservation>();
  private readonly sessions = new Map<string, SessionRecord>();
  private readonly totals = {
    accepted: 0,
    denied: 0,
    released: 0,
    rollbackAuthentication: 0,
    rollbackLoad: 0,
    rollbackSetup: 0,
  };

  constructor(private readonly admission: RuntimeSessionAdmission) {}

  reserve(
    sessionKey: string,
    candidate: CollaborationScope,
  ): RuntimeSessionReservation {
    try {
      validateSessionKey(sessionKey);
      const scope = immutableScope(candidate);
      if (this.sessions.has(sessionKey)) {
        throw new RuntimeSessionRegistryError("duplicate_session_reservation");
      }

      const canonicalKey = canonicalDocumentKey(scope);
      this.assertProviderReservation(scope, canonicalKey);
      this.assertCanonicalOwner(scope, canonicalKey);

      const admissionRelease = this.admission.acquire(scope);
      let admissionHeld = true;
      let scopeHeld = false;
      let sessionHeld = false;
      try {
        const token = Symbol("runtime-session-reservation");
        this.incrementScopeReservations(scope, canonicalKey);
        scopeHeld = true;
        this.sessions.set(sessionKey, {
          admissionRelease,
          phase: "pending",
          scope,
          token,
        });
        sessionHeld = true;
        admissionHeld = false;
        this.totals.accepted += 1;
        return this.handle(sessionKey, token, scope);
      } catch (error) {
        if (sessionHeld) this.sessions.delete(sessionKey);
        if (scopeHeld) this.decrementScopeReservations(scope);
        throw error;
      } finally {
        if (admissionHeld) admissionRelease();
      }
    } catch (error) {
      this.totals.denied += 1;
      throw error;
    }
  }

  /** Marks a fully connected session active. Repeating the commit is safe. */
  commit(sessionKey: string): void {
    validateSessionKey(sessionKey);
    const record = this.sessions.get(sessionKey);
    if (!record) {
      throw new RuntimeSessionRegistryError("reservation_not_found");
    }
    record.phase = "active";
  }

  /**
   * Disconnect fallback for lifecycle code that has only the session key.
   * Repeating the release is safe and does not change counters twice.
   */
  release(sessionKey: string): RuntimeSessionRelease {
    validateSessionKey(sessionKey);
    const record = this.sessions.get(sessionKey);
    if (!record) return { released: false };
    return this.close(sessionKey, record.token, "release");
  }

  /** Rolls back a failed auth/setup/load transaction by session key. */
  rollback(
    sessionKey: string,
    stage: RuntimeSessionFailureStage,
  ): RuntimeSessionRelease {
    validateSessionKey(sessionKey);
    validateFailureStage(stage);
    const record = this.sessions.get(sessionKey);
    if (!record) return { released: false };
    return this.close(sessionKey, record.token, stage);
  }

  sessionScope(sessionKey: string): Readonly<CollaborationScope> | undefined {
    validateSessionKey(sessionKey);
    return this.sessions.get(sessionKey)?.scope;
  }

  /** Includes pending scopes so authority revalidation fails closed mid-auth. */
  scopes(): ReadonlyArray<Readonly<CollaborationScope>> {
    return [...this.sessions.values()].map((record) => record.scope);
  }

  metrics(): RuntimeSessionRegistryMetrics {
    let activeSessions = 0;
    for (const record of this.sessions.values()) {
      if (record.phase === "active") activeSessions += 1;
    }
    return {
      activeDocumentScopes: this.providerScopes.size,
      activeSessions,
      pendingSessions: this.sessions.size - activeSessions,
      reservationAcceptedTotal: this.totals.accepted,
      reservationDeniedTotal: this.totals.denied,
      reservationReleasedTotal: this.totals.released,
      rollbackAuthenticationTotal: this.totals.rollbackAuthentication,
      rollbackLoadTotal: this.totals.rollbackLoad,
      rollbackSetupTotal: this.totals.rollbackSetup,
    };
  }

  private assertProviderReservation(
    scope: Readonly<CollaborationScope>,
    canonicalKey: string,
  ): void {
    const current = this.providerScopes.get(scope.providerDocumentName);
    if (
      current &&
      (current.canonicalKey !== canonicalKey ||
        !sameCanonicalScope(current.scope, scope))
    ) {
      throw new RuntimeSessionRegistryError("document_scope_conflict");
    }
  }

  private assertCanonicalOwner(
    scope: Readonly<CollaborationScope>,
    canonicalKey: string,
  ): void {
    const current = this.canonicalOwners.get(canonicalKey);
    if (
      current &&
      (current.providerDocumentName !== scope.providerDocumentName ||
        current.writerFence !== scope.writerFence)
    ) {
      throw new RuntimeSessionRegistryError("document_scope_conflict");
    }
  }

  private incrementScopeReservations(
    scope: Readonly<CollaborationScope>,
    canonicalKey: string,
  ): void {
    const provider = this.providerScopes.get(scope.providerDocumentName);
    if (provider) provider.references += 1;
    else {
      this.providerScopes.set(scope.providerDocumentName, {
        canonicalKey,
        references: 1,
        scope: canonicalScope(scope),
      });
    }

    const owner = this.canonicalOwners.get(canonicalKey);
    if (owner) owner.references += 1;
    else {
      this.canonicalOwners.set(canonicalKey, {
        providerDocumentName: scope.providerDocumentName,
        references: 1,
        writerFence: scope.writerFence,
      });
    }
  }

  private decrementScopeReservations(
    scope: Readonly<CollaborationScope>,
  ): void {
    const canonicalKey = canonicalDocumentKey(scope);
    const provider = this.providerScopes.get(scope.providerDocumentName);
    if (provider) {
      provider.references -= 1;
      if (provider.references <= 0) {
        this.providerScopes.delete(scope.providerDocumentName);
      }
    }

    const owner = this.canonicalOwners.get(canonicalKey);
    if (owner) {
      owner.references -= 1;
      if (owner.references <= 0) this.canonicalOwners.delete(canonicalKey);
    }
  }

  private handle(
    sessionKey: string,
    token: symbol,
    scope: Readonly<CollaborationScope>,
  ): RuntimeSessionReservation {
    return Object.freeze({
      scope,
      commit: () => this.commitExact(sessionKey, token),
      release: () => this.close(sessionKey, token, "release"),
      rollback: (stage: RuntimeSessionFailureStage) => {
        validateFailureStage(stage);
        return this.close(sessionKey, token, stage);
      },
    });
  }

  private commitExact(sessionKey: string, token: symbol): void {
    const record = this.sessions.get(sessionKey);
    if (!record || record.token !== token) {
      throw new RuntimeSessionRegistryError("reservation_not_found");
    }
    record.phase = "active";
  }

  private close(
    sessionKey: string,
    token: symbol,
    outcome: RuntimeSessionFailureStage | "release",
  ): RuntimeSessionRelease {
    const record = this.sessions.get(sessionKey);
    if (!record || record.token !== token) return { released: false };

    this.sessions.delete(sessionKey);
    this.decrementScopeReservations(record.scope);
    record.admissionRelease();

    if (outcome === "release") this.totals.released += 1;
    else if (outcome === "authentication") {
      this.totals.rollbackAuthentication += 1;
    } else if (outcome === "load") this.totals.rollbackLoad += 1;
    else this.totals.rollbackSetup += 1;

    return {
      capability: record.scope.capability,
      phase: record.phase,
      released: true,
    };
  }
}

function immutableScope(
  candidate: CollaborationScope,
): Readonly<CollaborationScope> {
  if (
    !candidate ||
    !validKeyPart(candidate.actorId, 256) ||
    !validKeyPart(candidate.authorityLease, 2_048) ||
    !validCapability(candidate.capability) ||
    !validKeyPart(candidate.documentId, 256) ||
    !positiveSafeInteger(candidate.generation) ||
    !validKeyPart(candidate.origin, 2_048) ||
    !validKeyPart(candidate.providerDocumentName, 512) ||
    !validKeyPart(candidate.sessionId, 256) ||
    !validKeyPart(candidate.tenantId, 256) ||
    !positiveSafeInteger(candidate.writerFence)
  ) {
    throw new RuntimeSessionRegistryError("invalid_session_reservation");
  }
  return Object.freeze({ ...candidate });
}

function canonicalScope(
  scope: Readonly<CollaborationScope>,
): CanonicalDocumentScope {
  return Object.freeze({
    documentId: scope.documentId,
    generation: scope.generation,
    tenantId: scope.tenantId,
    writerFence: scope.writerFence,
  });
}

function canonicalDocumentKey(scope: Readonly<CollaborationScope>): string {
  return JSON.stringify([scope.tenantId, scope.documentId, scope.generation]);
}

function sameCanonicalScope(
  current: CanonicalDocumentScope,
  candidate: Readonly<CollaborationScope>,
): boolean {
  return (
    current.tenantId === candidate.tenantId &&
    current.documentId === candidate.documentId &&
    current.generation === candidate.generation &&
    current.writerFence === candidate.writerFence
  );
}

function validateSessionKey(sessionKey: string): void {
  if (!validKeyPart(sessionKey, 1_024)) {
    throw new RuntimeSessionRegistryError("invalid_session_reservation");
  }
}

function validateFailureStage(stage: RuntimeSessionFailureStage): void {
  if (stage !== "authentication" && stage !== "load" && stage !== "setup") {
    throw new RuntimeSessionRegistryError("invalid_session_reservation");
  }
}

function validKeyPart(value: unknown, maxLength: number): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= maxLength &&
    !value.includes("\u0000")
  );
}

function positiveSafeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}

function validCapability(value: unknown): value is CollaborationCapability {
  return value === "edit" || value === "present" || value === "view";
}
