import { randomBytes, timingSafeEqual } from "node:crypto";
import { createServer } from "node:http";
import { pathToFileURL } from "node:url";

const EXACT_CONFIRMATION = "I_UNDERSTAND_P5_F3_DISPOSABLE_ONLY";
const DOCUMENT_PATTERN = /^wb\/[a-f0-9]{24}\/[a-f0-9]{24}\/g[1-9][0-9]*$/;
const MAX_BODY_BYTES = 8 * 1024;
const MAX_GRANTS = 64;
const GRANT_TTL_MS = 5 * 60 * 1000;

export function loadGateF3ControlConfig(env = process.env) {
  if (env.P5_F3_DISPOSABLE_CONFIRM !== EXACT_CONFIRMATION) {
    throw new Error("gate_f3_disposable_confirmation_required");
  }
  const allowedOrigin = secureOrigin(env.P5_F3_ALLOWED_ORIGIN);
  const documentName = env.P5_F3_PROVIDER_DOCUMENT_NAME?.trim() ?? "";
  if (!DOCUMENT_PATTERN.test(documentName)) {
    throw new Error("gate_f3_provider_document_name_invalid");
  }
  const serviceTokens = [requiredSecret(env, "P5_F3_CONTROL_TOKEN_CURRENT")];
  const nextToken = env.P5_F3_CONTROL_TOKEN_NEXT?.trim();
  if (nextToken) {
    if (nextToken.length < 32 || serviceTokens.includes(nextToken)) {
      throw new Error("gate_f3_control_token_next_invalid");
    }
    serviceTokens.push(nextToken);
  }
  const adminToken = requiredSecret(env, "P5_F3_CONTROL_ADMIN_TOKEN");
  if (serviceTokens.includes(adminToken)) {
    throw new Error("gate_f3_control_admin_token_must_be_distinct");
  }
  const port = integer(env.PORT, 3000, 0, 65_535);
  return {
    adminToken,
    allowedOrigin,
    documentName,
    port,
    serviceTokens,
  };
}

export function createGateF3ControlServer(config) {
  const grants = new Map();
  let mode = "enabled";
  let consumedCount = 0;

  const server = createServer(async (request, response) => {
    try {
      const path = new URL(request.url ?? "/", "http://gate.invalid").pathname;
      if (request.method === "GET" && path === "/livez") {
        writeJson(response, 200, { status: "ok" });
        return;
      }
      if (request.method === "GET" && path === "/readyz") {
        writeJson(response, mode === "unavailable" ? 503 : 200, {
          status: mode === "unavailable" ? "unavailable" : "ready",
        });
        return;
      }
      if (path.startsWith("/gate-f3/")) {
        if (!authorized(request.headers.authorization, [config.adminToken])) {
          writeJson(response, 401, { status: "unauthorized" });
          return;
        }
        if (request.method === "GET" && path === "/gate-f3/v1/status") {
          pruneGrants(grants);
          writeJson(response, 200, {
            consumed_grants: consumedCount,
            mode,
            outstanding_grants: grants.size,
            status: "ok",
          });
          return;
        }
        if (request.method === "PUT" && path === "/gate-f3/v1/state") {
          const body = await readJson(request);
          const nextMode = body.mode;
          if (
            nextMode !== "enabled" &&
            nextMode !== "off" &&
            nextMode !== "read_only" &&
            nextMode !== "unavailable"
          ) {
            writeJson(response, 400, { status: "invalid_mode" });
            return;
          }
          mode = nextMode;
          writeJson(response, 200, { mode, status: "updated" });
          return;
        }
        if (request.method === "POST" && path === "/gate-f3/v1/grants") {
          pruneGrants(grants);
          if (grants.size >= MAX_GRANTS) {
            writeJson(response, 429, { status: "grant_quota_exceeded" });
            return;
          }
          const body = await readJson(request);
          const capability = body.capability;
          if (
            capability !== "edit" &&
            capability !== "present" &&
            capability !== "view"
          ) {
            writeJson(response, 400, { status: "invalid_capability" });
            return;
          }
          const grant = randomBytes(32).toString("base64url");
          grants.set(grant, {
            capability,
            expiresAt: Date.now() + GRANT_TTL_MS,
          });
          writeJson(response, 201, {
            expires_in_seconds: GRANT_TTL_MS / 1000,
            grant,
            provider_document_name: config.documentName,
            status: "issued",
          });
          return;
        }
        writeJson(response, 404, { status: "not_found" });
        return;
      }

      if (!authorized(request.headers.authorization, config.serviceTokens)) {
        writeJson(response, 401, { status: "unauthorized" });
        return;
      }
      if (mode === "unavailable") {
        writeJson(response, 503, { status: "provider_unavailable" });
        return;
      }
      if (
        request.method === "GET" &&
        path === "/internal/v1/collaboration/runtime-state"
      ) {
        writeJson(response, 200, { mode });
        return;
      }
      if (
        request.method === "POST" &&
        path === "/internal/v1/collaboration/grants/exchange"
      ) {
        const body = await readJson(request);
        const grant = typeof body.grant === "string" ? body.grant : "";
        const issued = grants.get(grant);
        if (
          !issued ||
          issued.expiresAt <= Date.now() ||
          body.origin !== config.allowedOrigin ||
          body.provider_document_name !== config.documentName
        ) {
          grants.delete(grant);
          writeJson(response, 409, { status: "grant_denied" });
          return;
        }
        grants.delete(grant);
        consumedCount += 1;
        writeJson(response, 200, {
          actor_id: "gate-f3-teacher",
          capability: issued.capability,
          document_id: "gate-f3-document",
          generation: 1,
          provider_document_name: config.documentName,
          session_id: "gate-f3-session",
          tenant_id: "gate-f3-tenant",
          writer_fence: 1,
        });
        return;
      }
      writeJson(response, 404, { status: "not_found" });
    } catch (error) {
      const status = error instanceof BodyError ? error.status : 500;
      writeJson(response, status, {
        status: status === 500 ? "internal_error" : "invalid_request",
      });
    }
  });

  return {
    address() {
      const address = server.address();
      if (!address || typeof address === "string") {
        throw new Error("gate_f3_control_not_started");
      }
      return { address: address.address, port: address.port };
    },
    async close() {
      await new Promise((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      );
    },
    async start() {
      await new Promise((resolve, reject) => {
        server.once("error", reject);
        server.listen(config.port, "0.0.0.0", () => {
          server.off("error", reject);
          resolve();
        });
      });
    },
  };
}

async function run() {
  const control = createGateF3ControlServer(loadGateF3ControlConfig());
  await control.start();
  process.stdout.write(
    '{"event_code":"gate_f3_control_started","outcome":"ok"}\n',
  );
  let closing = false;
  const close = async () => {
    if (closing) return;
    closing = true;
    await control.close();
    process.stdout.write(
      '{"event_code":"gate_f3_control_drained","outcome":"ok"}\n',
    );
  };
  process.once("SIGINT", () => void close());
  process.once("SIGTERM", () => void close());
}

class BodyError extends Error {
  constructor(status) {
    super("invalid_body");
    this.status = status;
  }
}

async function readJson(request) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > MAX_BODY_BYTES) throw new BodyError(413);
    chunks.push(chunk);
  }
  try {
    const value = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      throw new Error("invalid_body");
    }
    return value;
  } catch {
    throw new BodyError(400);
  }
}

function authorized(header, expectedTokens) {
  if (!header?.startsWith("Bearer ")) return false;
  const received = Buffer.from(header.slice("Bearer ".length));
  return expectedTokens.some((token) => {
    const expected = Buffer.from(token);
    return (
      received.byteLength === expected.byteLength &&
      timingSafeEqual(received, expected)
    );
  });
}

function pruneGrants(grants) {
  const now = Date.now();
  for (const [grant, value] of grants) {
    if (value.expiresAt <= now) grants.delete(grant);
  }
}

function writeJson(response, status, payload) {
  response.writeHead(status, {
    "cache-control": "no-store",
    "content-type": "application/json; charset=utf-8",
    "x-content-type-options": "nosniff",
  });
  response.end(JSON.stringify(payload));
}

function requiredSecret(env, name) {
  const value = env[name]?.trim() ?? "";
  if (value.length < 32) throw new Error(`${name.toLowerCase()}_invalid`);
  return value;
}

function secureOrigin(value) {
  try {
    const parsed = new URL(value?.trim() ?? "");
    if (
      parsed.protocol !== "https:" ||
      parsed.username ||
      parsed.password ||
      parsed.pathname !== "/" ||
      parsed.search ||
      parsed.hash
    ) {
      throw new Error("invalid_origin");
    }
    return parsed.origin;
  } catch {
    throw new Error("gate_f3_allowed_origin_invalid");
  }
}

function integer(raw, fallback, minimum, maximum) {
  const value = raw?.trim() ? Number(raw) : fallback;
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error("gate_f3_port_invalid");
  }
  return value;
}

const entrypoint = process.argv[1] ? pathToFileURL(process.argv[1]).href : "";
if (import.meta.url === entrypoint) {
  run().catch(() => {
    process.stderr.write(
      '{"event_code":"gate_f3_control_start_failed","outcome":"failed"}\n',
    );
    process.exitCode = 1;
  });
}
