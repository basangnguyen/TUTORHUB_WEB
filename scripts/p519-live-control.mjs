import { randomBytes, timingSafeEqual } from "node:crypto";
import { createServer } from "node:http";
import { pathToFileURL } from "node:url";

const IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._:-]{2,127}$/u;
const UUID =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/iu;
const DOCUMENT = /^wb_[A-Za-z0-9_-]{22,125}$/u;
const CAPABILITIES = new Set(["edit", "present", "view"]);
const MODES = new Set(["enabled", "off", "read_only"]);
const MAX_BODY_BYTES = 16 * 1024;

const DEFAULT_DOCUMENTS = [
  "wb_p519_private_alpha_document_01",
  "wb_p519_private_alpha_document_02",
];

function safeEqual(actual, expected) {
  const left = Buffer.from(String(actual ?? ""));
  const right = Buffer.from(String(expected ?? ""));
  return left.length === right.length && timingSafeEqual(left, right);
}

function bearer(request) {
  const value = request.headers.authorization ?? "";
  return value.startsWith("Bearer ") ? value.slice(7) : "";
}

function json(response, status, payload) {
  const body = JSON.stringify(payload);
  response.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(body),
    "cache-control": "no-store",
  });
  response.end(body);
}

async function readJson(request) {
  const chunks = [];
  let length = 0;
  for await (const chunk of request) {
    length += chunk.length;
    if (length > MAX_BODY_BYTES) throw new Error("body_too_large");
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}");
}

function requiredIdentifier(value, name) {
  if (typeof value !== "string" || !IDENTIFIER.test(value)) {
    throw new Error(`invalid_${name}`);
  }
  return value;
}

function requiredUuid(value, name) {
  if (typeof value !== "string" || !UUID.test(value)) {
    throw new Error(`invalid_${name}`);
  }
  return value;
}

function requiredDocument(value, documents) {
  if (
    typeof value !== "string" ||
    !DOCUMENT.test(value) ||
    !documents.has(value)
  ) {
    throw new Error("invalid_provider_document_name");
  }
  return value;
}

function exactScope(left, right) {
  return (
    left.actor_id === right.actor_id &&
    left.authority_lease === right.authority_lease &&
    left.capability === right.capability &&
    left.document_id === right.document_id &&
    left.generation === right.generation &&
    left.max_connections_per_tenant === right.max_connections_per_tenant &&
    left.max_operations_per_minute === right.max_operations_per_minute &&
    left.max_storage_bytes_per_tenant === right.max_storage_bytes_per_tenant &&
    left.origin === right.origin &&
    left.provider_document_name === right.provider_document_name &&
    left.session_id === right.session_id &&
    left.tenant_id === right.tenant_id &&
    left.writer_fence === right.writer_fence
  );
}

function createGrant(body, documents, allowedOrigin) {
  const providerDocumentName = requiredDocument(
    body.provider_document_name,
    documents,
  );
  const capability = body.capability ?? "edit";
  if (!CAPABILITIES.has(capability)) throw new Error("invalid_capability");
  return {
    actor_id: requiredIdentifier(body.actor_id, "actor_id"),
    capability,
    document_id: requiredUuid(body.document_id, "document_id"),
    generation: 1,
    max_connections_per_tenant: 10,
    max_operations_per_minute: 12_000,
    max_storage_bytes_per_tenant: 1_073_741_824,
    origin: allowedOrigin,
    provider_document_name: providerDocumentName,
    session_id: requiredIdentifier(body.session_id, "session_id"),
    tenant_id: requiredUuid(body.tenant_id, "tenant_id"),
    writer_fence: 1,
  };
}

export function createP519Control(options) {
  const serviceToken = String(options.serviceToken ?? "");
  const adminToken = String(options.adminToken ?? "");
  const allowedOrigin = String(options.allowedOrigin ?? "");
  const documents = new Set(options.documents ?? DEFAULT_DOCUMENTS);
  if (serviceToken.length < 20 || adminToken.length < 20) {
    throw new Error("control_tokens_must_be_at_least_20_characters");
  }
  if (!/^https?:\/\//u.test(allowedOrigin)) {
    throw new Error("invalid_allowed_origin");
  }
  if (
    documents.size !== 2 ||
    [...documents].some((value) => !DOCUMENT.test(value))
  ) {
    throw new Error("exactly_two_valid_documents_required");
  }

  const grants = new Map();
  const leases = new Map();
  let mode = "enabled";
  let authorityAvailable = true;

  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url ?? "/", "http://127.0.0.1");
      if (request.method === "GET" && url.pathname === "/livez") {
        return json(response, 200, { status: "ok" });
      }

      const isAdmin = url.pathname.startsWith("/p519/v1/");
      const expectedToken = isAdmin ? adminToken : serviceToken;
      if (!safeEqual(bearer(request), expectedToken)) {
        return json(response, 401, { code: "unauthorized" });
      }

      if (
        request.method === "GET" &&
        url.pathname === "/internal/v1/collaboration/runtime-state"
      ) {
        if (!authorityAvailable)
          return json(response, 503, { code: "unavailable" });
        return json(response, 200, { mode });
      }

      if (
        request.method === "POST" &&
        url.pathname === "/internal/v1/collaboration/grants/exchange"
      ) {
        if (!authorityAvailable)
          return json(response, 503, { code: "unavailable" });
        if (mode === "off") return json(response, 409, { code: "runtime_off" });
        const body = await readJson(request);
        const grant = grants.get(body.grant);
        if (
          !grant ||
          grant.used ||
          grant.expiresAt < Date.now() ||
          body.origin !== grant.scope.origin ||
          body.provider_document_name !== grant.scope.provider_document_name
        ) {
          return json(response, 403, { code: "grant_denied" });
        }
        grant.used = true;
        const authorityLease = randomBytes(32).toString("base64url");
        const scope = {
          ...grant.scope,
          authority_lease: authorityLease,
          capability: mode === "read_only" ? "view" : grant.scope.capability,
        };
        leases.set(authorityLease, {
          expiresAt: Date.now() + 3 * 60 * 60 * 1000,
          revoked: false,
          scope,
        });
        return json(response, 200, scope);
      }

      if (
        request.method === "POST" &&
        url.pathname === "/internal/v1/collaboration/grants/validate"
      ) {
        if (!authorityAvailable)
          return json(response, 503, { code: "unavailable" });
        const body = await readJson(request);
        const scopes = Array.isArray(body.scopes) ? body.scopes : [];
        if (scopes.length > 100)
          return json(response, 409, { code: "too_many_scopes" });
        const valid = [];
        for (const scope of scopes) {
          const lease = leases.get(scope?.authority_lease);
          if (
            lease &&
            !lease.revoked &&
            lease.expiresAt >= Date.now() &&
            exactScope(lease.scope, scope)
          ) {
            valid.push(scope.authority_lease);
          }
        }
        return json(response, 200, { valid_authority_leases: valid });
      }

      if (request.method === "GET" && url.pathname === "/p519/v1/status") {
        return json(response, 200, {
          authority_available: authorityAvailable,
          active_leases: [...leases.values()].filter((item) => !item.revoked)
            .length,
          documents: documents.size,
          mode,
          pending_grants: [...grants.values()].filter((item) => !item.used)
            .length,
        });
      }

      if (request.method === "PUT" && url.pathname === "/p519/v1/state") {
        const body = await readJson(request);
        if (body.mode !== undefined && !MODES.has(body.mode)) {
          return json(response, 400, { code: "invalid_mode" });
        }
        if (body.mode !== undefined) mode = body.mode;
        if (body.authority_available !== undefined) {
          if (typeof body.authority_available !== "boolean") {
            return json(response, 400, { code: "invalid_authority_available" });
          }
          authorityAvailable = body.authority_available;
        }
        return json(response, 200, {
          authority_available: authorityAvailable,
          mode,
        });
      }

      if (request.method === "POST" && url.pathname === "/p519/v1/grants") {
        if (mode === "off") return json(response, 409, { code: "runtime_off" });
        if (!authorityAvailable) {
          return json(response, 503, { code: "authority_unavailable" });
        }
        const body = await readJson(request);
        const scope = createGrant(body, documents, allowedOrigin);
        const token = randomBytes(32).toString("base64url");
        grants.set(token, {
          expiresAt: Date.now() + 15 * 60 * 1000,
          scope,
          used: false,
        });
        return json(response, 201, { grant: token });
      }

      if (
        request.method === "POST" &&
        url.pathname === "/p519/v1/leases/revoke"
      ) {
        const body = await readJson(request);
        const lease = leases.get(body.authority_lease);
        if (lease) lease.revoked = true;
        return json(response, 200, { revoked: Boolean(lease) });
      }

      return json(response, 404, { code: "not_found" });
    } catch (error) {
      const code = error instanceof Error ? error.message : "invalid_request";
      return json(response, code === "body_too_large" ? 413 : 400, { code });
    }
  });

  return { server, documents: [...documents] };
}

export function optionsFromEnvironment(environment = process.env) {
  const confirmation =
    environment.P5_COLLAB_19_DISPOSABLE_CONFIRM ??
    environment.P519_DISPOSABLE_CONFIRM;
  if (confirmation !== "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY") {
    throw new Error("P519 disposable confirmation is required");
  }
  return {
    adminToken:
      environment.P5_COLLAB_19_CONTROL_ADMIN_TOKEN ??
      environment.P5_F3_CONTROL_ADMIN_TOKEN,
    allowedOrigin:
      environment.P5_COLLAB_19_ALLOWED_ORIGIN ??
      environment.P519_ALLOWED_ORIGIN ??
      environment.P5_F3_ALLOWED_ORIGIN,
    documents:
      (environment.P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES ??
      environment.P519_PROVIDER_DOCUMENT_NAMES)
        ? (
            environment.P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES ??
            environment.P519_PROVIDER_DOCUMENT_NAMES
          )
            .split(",")
            .map((value) => value.trim())
        : DEFAULT_DOCUMENTS,
    serviceToken:
      environment.P5_COLLAB_19_CONTROL_TOKEN_CURRENT ??
      environment.P519_CONTROL_TOKEN_CURRENT ??
      environment.P5_F3_CONTROL_TOKEN_CURRENT,
  };
}

export function startFromEnvironment(environment = process.env) {
  const { server } = createP519Control(optionsFromEnvironment(environment));
  const port = Number.parseInt(environment.PORT ?? "4179", 10);
  server.listen(port, "0.0.0.0", () => {
    process.stdout.write("p519_control_ready\n");
  });
  return server;
}

const invokedPath = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === invokedPath) startFromEnvironment();

export { DEFAULT_DOCUMENTS };
