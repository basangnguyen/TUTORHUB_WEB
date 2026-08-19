export const EXCALIDRAW_AUTHORIZATION_LIMITS = {
  defaultGrantTtlMs: 30_000,
  maxGrantTtlMs: 60_000,
  maxHttpBodyBytes: 8 * 1024,
  maxFrameBytes: 1024 * 1024,
  maxUpdateBytes: 768 * 1024,
  maxAwarenessBytes: 16 * 1024,
  maxAwarenessDepth: 8,
  maxAwarenessStates: 64,
  maxMessagesPerSecond: 120,
  maxConnectionsPerActor: 4,
  maxConnectionsPerDocument: 50,
  maxConnectionsPerTenant: 100,
  maxReconnectAttemptsPerWindow: 8,
  reconnectWindowMs: 10_000,
  maxGrantIssuesPerWindow: 8,
  grantIssueWindowMs: 10_000,
  socketRevocationBudgetMs: 1_000,
} as const;

export type CollaborationCapability = "view" | "edit" | "present";

export interface CollaborationGrantRequest {
  actorId: string;
  documentId: string;
  expectedGeneration?: number;
  requestedCapability: CollaborationCapability;
  sessionId: string;
  tenantId: string;
}

export interface CollaborationGrantResponse {
  capability: CollaborationCapability;
  expiresInSeconds: number;
  generation: number;
  grant: string;
  providerDocumentName: string;
}

export type CollaborationAuthorizationErrorCode =
  | "actor_revoked"
  | "authorization_authority_unavailable"
  | "capability_escalation_denied"
  | "document_closed"
  | "document_connection_quota"
  | "document_scope_denied"
  | "grant_binding_mismatch"
  | "grant_expired"
  | "grant_invalid_or_replayed"
  | "grant_issue_rate_limited"
  | "grant_ttl_invalid"
  | "identifier_invalid"
  | "membership_denied"
  | "origin_denied"
  | "rate_authority_unavailable"
  | "reader_mutation_denied"
  | "reconnect_storm_denied"
  | "scene_budget_denied"
  | "session_binding_denied"
  | "stale_generation"
  | "tenant_connection_quota"
  | "tenant_scope_denied"
  | "update_rate_limited"
  | "update_too_large"
  | "actor_connection_quota";

export class CollaborationAuthorizationError extends Error {
  constructor(readonly code: CollaborationAuthorizationErrorCode) {
    super(code);
    this.name = "CollaborationAuthorizationError";
  }
}

export function capabilityIncludes(
  resolved: CollaborationCapability,
  requested: CollaborationCapability,
): boolean {
  const rank: Record<CollaborationCapability, number> = {
    view: 0,
    edit: 1,
    present: 2,
  };
  return rank[resolved] >= rank[requested];
}
