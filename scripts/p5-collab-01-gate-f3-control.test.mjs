import assert from "node:assert/strict";
import { test } from "node:test";
import {
  createGateF3ControlServer,
  loadGateF3ControlConfig,
} from "./p5-collab-01-gate-f3-control.mjs";

const CURRENT_TOKEN = "c".repeat(40);
const NEXT_TOKEN = "n".repeat(40);
const ADMIN_TOKEN = "a".repeat(40);
const ORIGIN = "https://gate-f3-client.example";
const DOCUMENT = "wb/aaaaaaaaaaaaaaaaaaaaaaaa/bbbbbbbbbbbbbbbbbbbbbbbb/g1";

test("issues one-time grants, supports token overlap and fails closed", async (t) => {
  const control = createGateF3ControlServer(
    loadGateF3ControlConfig({
      P5_F3_ALLOWED_ORIGIN: ORIGIN,
      P5_F3_CONTROL_ADMIN_TOKEN: ADMIN_TOKEN,
      P5_F3_CONTROL_TOKEN_CURRENT: CURRENT_TOKEN,
      P5_F3_CONTROL_TOKEN_NEXT: NEXT_TOKEN,
      P5_F3_DISPOSABLE_CONFIRM: "I_UNDERSTAND_P5_F3_DISPOSABLE_ONLY",
      P5_F3_PROVIDER_DOCUMENT_NAME: DOCUMENT,
      PORT: "0",
    }),
  );
  await control.start();
  t.after(() => control.close());
  const baseUrl = `http://127.0.0.1:${control.address().port}`;

  assert.equal(await status(baseUrl, "/livez"), 200);
  assert.equal(
    await status(baseUrl, "/internal/v1/collaboration/runtime-state"),
    401,
  );
  assert.equal(
    await status(baseUrl, "/internal/v1/collaboration/runtime-state", {
      headers: bearer(NEXT_TOKEN),
    }),
    200,
  );

  const issued = await requestJson(baseUrl, "/gate-f3/v1/grants", {
    body: JSON.stringify({ capability: "edit" }),
    headers: { ...bearer(ADMIN_TOKEN), "content-type": "application/json" },
    method: "POST",
  });
  assert.equal(issued.response.status, 201);
  assert.equal(issued.body.provider_document_name, DOCUMENT);
  assert.equal(typeof issued.body.grant, "string");

  const exchange = () =>
    requestJson(baseUrl, "/internal/v1/collaboration/grants/exchange", {
      body: JSON.stringify({
        grant: issued.body.grant,
        origin: ORIGIN,
        provider_document_name: DOCUMENT,
      }),
      headers: {
        ...bearer(CURRENT_TOKEN),
        "content-type": "application/json",
      },
      method: "POST",
    });
  assert.equal((await exchange()).response.status, 200);
  assert.equal((await exchange()).response.status, 409);

  assert.equal(
    (
      await requestJson(baseUrl, "/gate-f3/v1/state", {
        body: JSON.stringify({ mode: "unavailable" }),
        headers: {
          ...bearer(ADMIN_TOKEN),
          "content-type": "application/json",
        },
        method: "PUT",
      })
    ).response.status,
    200,
  );
  assert.equal(await status(baseUrl, "/readyz"), 503);
  assert.equal(
    await status(baseUrl, "/internal/v1/collaboration/runtime-state", {
      headers: bearer(CURRENT_TOKEN),
    }),
    503,
  );
});

test("rejects missing disposable confirmation and reused admin token", () => {
  assert.throws(
    () =>
      loadGateF3ControlConfig({
        P5_F3_ALLOWED_ORIGIN: ORIGIN,
        P5_F3_CONTROL_ADMIN_TOKEN: ADMIN_TOKEN,
        P5_F3_CONTROL_TOKEN_CURRENT: CURRENT_TOKEN,
        P5_F3_PROVIDER_DOCUMENT_NAME: DOCUMENT,
      }),
    /gate_f3_disposable_confirmation_required/,
  );
  assert.throws(
    () =>
      loadGateF3ControlConfig({
        P5_F3_ALLOWED_ORIGIN: ORIGIN,
        P5_F3_CONTROL_ADMIN_TOKEN: CURRENT_TOKEN,
        P5_F3_CONTROL_TOKEN_CURRENT: CURRENT_TOKEN,
        P5_F3_DISPOSABLE_CONFIRM: "I_UNDERSTAND_P5_F3_DISPOSABLE_ONLY",
        P5_F3_PROVIDER_DOCUMENT_NAME: DOCUMENT,
      }),
    /gate_f3_control_admin_token_must_be_distinct/,
  );
});

function bearer(token) {
  return { authorization: `Bearer ${token}` };
}

async function status(baseUrl, path, init) {
  return fetch(`${baseUrl}${path}`, init).then((response) => response.status);
}

async function requestJson(baseUrl, path, init) {
  const response = await fetch(`${baseUrl}${path}`, init);
  return { body: await response.json(), response };
}
