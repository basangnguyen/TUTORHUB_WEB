import type {
  CollaborationCapability,
  CollaborationScope,
  ControlPlane,
  RuntimeAuthorityState,
  RuntimeMode,
} from "./contracts.js";

const IDENTIFIER_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/;
const AUTHORITY_LEASE_PATTERN = /^[A-Za-z0-9_-]{32,128}$/;
const PROVIDER_DOCUMENT_PATTERN = /^wb_[A-Za-z0-9_-]{22,125}$/;
const MAX_RESPONSE_BYTES = 8 * 1024;

export class ControlPlaneError extends Error {
  constructor(
    readonly code: "control_plane_denied" | "control_plane_unavailable",
  ) {
    super(code);
    this.name = "ControlPlaneError";
  }
}

export class HttpControlPlane implements ControlPlane {
  constructor(
    private readonly baseUrl: string,
    private readonly serviceToken: string,
    private readonly timeoutMs: number,
    private readonly fetcher: typeof fetch = fetch,
  ) {}

  async exchangeGrant(input: {
    documentName: string;
    grant: string;
    origin: string;
  }): Promise<CollaborationScope> {
    const payload = await this.request(
      "/internal/v1/collaboration/grants/exchange",
      {
        body: JSON.stringify({
          grant: input.grant,
          origin: input.origin,
          provider_document_name: input.documentName,
        }),
        method: "POST",
      },
    );
    return parseScope(payload, input.documentName, input.origin);
  }

  async probe(): Promise<RuntimeAuthorityState> {
    const payload = await this.request(
      "/internal/v1/collaboration/runtime-state",
      {
        method: "GET",
      },
    );
    if (!isRecord(payload) || !isMode(payload.mode)) {
      throw new ControlPlaneError("control_plane_unavailable");
    }
    return { mode: payload.mode };
  }

  async validateScopes(scopes: CollaborationScope[]): Promise<Set<string>> {
    if (scopes.length === 0) return new Set<string>();
    if (scopes.length > 100) {
      throw new ControlPlaneError("control_plane_denied");
    }
    const requested = new Set(scopes.map((scope) => scope.authorityLease));
    if (requested.size !== scopes.length) {
      throw new ControlPlaneError("control_plane_denied");
    }
    const payload = await this.request(
      "/internal/v1/collaboration/grants/validate",
      {
        body: JSON.stringify({
          scopes: scopes.map((scope) => ({
            actor_id: scope.actorId,
            authority_lease: scope.authorityLease,
            capability: scope.capability,
            document_id: scope.documentId,
            generation: scope.generation,
            origin: scope.origin,
            provider_document_name: scope.providerDocumentName,
            session_id: scope.sessionId,
            tenant_id: scope.tenantId,
            writer_fence: scope.writerFence,
          })),
        }),
        method: "POST",
      },
    );
    if (!isRecord(payload) || !Array.isArray(payload.valid_authority_leases)) {
      throw new ControlPlaneError("control_plane_unavailable");
    }
    const valid = new Set<string>();
    for (const value of payload.valid_authority_leases) {
      if (
        typeof value !== "string" ||
        !AUTHORITY_LEASE_PATTERN.test(value) ||
        !requested.has(value) ||
        valid.has(value)
      ) {
        throw new ControlPlaneError("control_plane_unavailable");
      }
      valid.add(value);
    }
    return valid;
  }

  private async request(path: string, init: RequestInit): Promise<unknown> {
    try {
      const response = await this.fetcher(`${this.baseUrl}${path}`, {
        ...init,
        headers: {
          accept: "application/json",
          authorization: `Bearer ${this.serviceToken}`,
          "content-type": "application/json",
        },
        signal: AbortSignal.timeout(this.timeoutMs),
      });
      if (
        response.status === 401 ||
        response.status === 403 ||
        response.status === 409
      ) {
        throw new ControlPlaneError("control_plane_denied");
      }
      if (!response.ok) {
        throw new ControlPlaneError("control_plane_unavailable");
      }
      const declaredLength = Number(
        response.headers.get("content-length") ?? "0",
      );
      if (declaredLength > MAX_RESPONSE_BYTES) {
        throw new ControlPlaneError("control_plane_unavailable");
      }
      const body = await response.text();
      if (Buffer.byteLength(body) > MAX_RESPONSE_BYTES) {
        throw new ControlPlaneError("control_plane_unavailable");
      }
      return JSON.parse(body) as unknown;
    } catch (error) {
      if (error instanceof ControlPlaneError) throw error;
      throw new ControlPlaneError("control_plane_unavailable");
    }
  }
}

function parseScope(
  payload: unknown,
  expectedDocumentName: string,
  origin: string,
): CollaborationScope {
  if (!isRecord(payload)) throw new ControlPlaneError("control_plane_denied");
  const authorityLease = stringField(payload, "authority_lease");
  const actorId = stringField(payload, "actor_id");
  const capability = payload.capability;
  const documentId = stringField(payload, "document_id");
  const generation = integerField(payload, "generation", 1);
  const providerDocumentName = stringField(payload, "provider_document_name");
  const sessionId = stringField(payload, "session_id");
  const tenantId = stringField(payload, "tenant_id");
  const writerFence = integerField(payload, "writer_fence", 1);

  if (
    !isCapability(capability) ||
    !AUTHORITY_LEASE_PATTERN.test(authorityLease) ||
    !IDENTIFIER_PATTERN.test(actorId) ||
    !IDENTIFIER_PATTERN.test(documentId) ||
    !IDENTIFIER_PATTERN.test(sessionId) ||
    !IDENTIFIER_PATTERN.test(tenantId) ||
    !PROVIDER_DOCUMENT_PATTERN.test(providerDocumentName) ||
    providerDocumentName !== expectedDocumentName
  ) {
    throw new ControlPlaneError("control_plane_denied");
  }

  return {
    authorityLease,
    actorId,
    capability,
    documentId,
    generation,
    origin,
    providerDocumentName,
    sessionId,
    tenantId,
    writerFence,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(value: Record<string, unknown>, key: string): string {
  const field = value[key];
  if (typeof field !== "string")
    throw new ControlPlaneError("control_plane_denied");
  return field;
}

function integerField(
  value: Record<string, unknown>,
  key: string,
  minimum: number,
): number {
  const field = value[key];
  if (!Number.isSafeInteger(field) || (field as number) < minimum) {
    throw new ControlPlaneError("control_plane_denied");
  }
  return field as number;
}

function isCapability(value: unknown): value is CollaborationCapability {
  return value === "edit" || value === "present" || value === "view";
}

function isMode(value: unknown): value is RuntimeMode {
  return value === "enabled" || value === "off" || value === "read_only";
}
