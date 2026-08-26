import assert from "node:assert/strict";
import test from "node:test";

import {
  DEFAULT_DOCUMENTS,
  createP519Control,
  optionsFromEnvironment,
} from "./p519-live-control.mjs";
import { P519_PROVIDER_FIXTURE } from "./p519-provider-fixture.mjs";

const SERVICE = "service-token-that-is-long-enough";
const ADMIN = "admin-token-that-is-long-enough";
const ORIGIN = "https://p5-f3-client.invalid";

test("accepts the canonical P5-COLLAB-19 environment names", () => {
  const options = optionsFromEnvironment({
    P5_COLLAB_19_ALLOWED_ORIGIN: ORIGIN,
    P5_COLLAB_19_CONTROL_ADMIN_TOKEN: ADMIN,
    P5_COLLAB_19_CONTROL_TOKEN_CURRENT: SERVICE,
    P5_COLLAB_19_DISPOSABLE_CONFIRM:
      "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
    P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES: DEFAULT_DOCUMENTS.join(","),
  });
  assert.deepEqual(options, {
    adminToken: ADMIN,
    allowedOrigin: ORIGIN,
    documents: DEFAULT_DOCUMENTS,
    serviceToken: SERVICE,
  });
});

async function fixture(t) {
  const control = createP519Control({
    adminToken: ADMIN,
    allowedOrigin: ORIGIN,
    documents: DEFAULT_DOCUMENTS,
    serviceToken: SERVICE,
  });
  await new Promise((resolve) =>
    control.server.listen(0, "127.0.0.1", resolve),
  );
  t.after(() => new Promise((resolve) => control.server.close(resolve)));
  const address = control.server.address();
  return `http://127.0.0.1:${address.port}`;
}

async function request(base, path, token, body, method = "POST") {
  const response = await fetch(base + path, {
    body: body === undefined ? undefined : JSON.stringify(body),
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
    },
    method,
  });
  return { response, payload: await response.json() };
}

test("issues one-time grant and validates the exact authority lease", async (t) => {
  const base = await fixture(t);
  const issued = await request(base, "/p519/v1/grants", ADMIN, {
    actor_id: "actor-01",
    capability: "edit",
    document_id: P519_PROVIDER_FIXTURE.documents[0].documentId,
    provider_document_name: DEFAULT_DOCUMENTS[0],
    session_id: "session-01",
    tenant_id: P519_PROVIDER_FIXTURE.tenantId,
  });
  assert.equal(issued.response.status, 201);

  const exchanged = await request(
    base,
    "/internal/v1/collaboration/grants/exchange",
    SERVICE,
    {
      grant: issued.payload.grant,
      origin: ORIGIN,
      provider_document_name: DEFAULT_DOCUMENTS[0],
    },
  );
  assert.equal(exchanged.response.status, 200);
  assert.match(exchanged.payload.authority_lease, /^[A-Za-z0-9_-]{32,128}$/u);

  const replay = await request(
    base,
    "/internal/v1/collaboration/grants/exchange",
    SERVICE,
    {
      grant: issued.payload.grant,
      origin: ORIGIN,
      provider_document_name: DEFAULT_DOCUMENTS[0],
    },
  );
  assert.equal(replay.response.status, 403);

  const validated = await request(
    base,
    "/internal/v1/collaboration/grants/validate",
    SERVICE,
    { scopes: [exchanged.payload] },
  );
  assert.deepEqual(validated.payload.valid_authority_leases, [
    exchanged.payload.authority_lease,
  ]);
});

test("outage and force-off fail closed without exposing tokens", async (t) => {
  const base = await fixture(t);
  const outage = await request(
    base,
    "/p519/v1/state",
    ADMIN,
    { authority_available: false },
    "PUT",
  );
  assert.equal(outage.response.status, 200);
  const state = await request(
    base,
    "/internal/v1/collaboration/runtime-state",
    SERVICE,
    undefined,
    "GET",
  );
  assert.equal(state.response.status, 503);

  const authorityGrant = await request(base, "/p519/v1/grants", ADMIN, {
    actor_id: "actor-02",
    document_id: P519_PROVIDER_FIXTURE.documents[0].documentId,
    provider_document_name: DEFAULT_DOCUMENTS[0],
    session_id: "session-02",
    tenant_id: P519_PROVIDER_FIXTURE.tenantId,
  });
  assert.equal(authorityGrant.response.status, 503);

  await request(
    base,
    "/p519/v1/state",
    ADMIN,
    { authority_available: true, mode: "off" },
    "PUT",
  );
  const denied = await request(base, "/p519/v1/grants", ADMIN, {
    actor_id: "actor-02",
    document_id: P519_PROVIDER_FIXTURE.documents[0].documentId,
    provider_document_name: DEFAULT_DOCUMENTS[0],
    session_id: "session-02",
    tenant_id: P519_PROVIDER_FIXTURE.tenantId,
  });
  assert.equal(denied.response.status, 409);
  const status = await request(
    base,
    "/p519/v1/status",
    ADMIN,
    undefined,
    "GET",
  );
  assert.equal(status.payload.mode, "off");
  assert.equal(JSON.stringify(status.payload).includes(SERVICE), false);
  assert.equal(JSON.stringify(status.payload).includes(ADMIN), false);
});
