import assert from "node:assert/strict";
import test from "node:test";

import { P519_RENDER_SERVICES } from "./p519-render-sync.mjs";
import {
  createP520RenderAdapter,
  validateP520RenderEnvironment,
} from "./p520-render-adapter.mjs";

const CANDIDATE = "a".repeat(40);
const FINGERPRINT = "b".repeat(64);
const TOKEN = "token-that-is-long-enough";
const TENANTS = [
  "11111111-1111-4111-8111-111111111111",
  "22222222-2222-4222-8222-222222222222",
];

function jsonResponse(value, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function fakeProvider() {
  const calls = [];
  let mode = "off";
  const services = {
    [P519_RENDER_SERVICES.control.name]: {
      id: "srv-control",
      name: P519_RENDER_SERVICES.control.name,
      type: "web_service",
      branch: "main",
      autoDeploy: "no",
      region: "singapore",
      serviceDetails: { plan: "free" },
    },
    [P519_RENDER_SERVICES.runtime.name]: {
      id: "srv-runtime",
      name: P519_RENDER_SERVICES.runtime.name,
      type: "web_service",
      branch: "main",
      autoDeploy: "no",
      region: "singapore",
      serviceDetails: { plan: "free" },
    },
  };
  async function fetchImpl(url, init = {}) {
    const parsed = new URL(url);
    calls.push([init.method ?? "GET", parsed.pathname, parsed.search]);
    if (parsed.hostname === "api.render.com") {
      if (parsed.pathname === "/v1/services") {
        const service = services[parsed.searchParams.get("name")];
        return jsonResponse(service ? [{ service }] : []);
      }
      if (parsed.pathname.includes("/env-vars/")) return jsonResponse({});
      if (parsed.pathname.endsWith("/deploys")) {
        return jsonResponse({
          id: parsed.pathname.includes("srv-control")
            ? "dep-control"
            : "dep-runtime",
        });
      }
      if (parsed.pathname.includes("/deploys/dep-")) {
        return jsonResponse({ status: "live", commit: { id: CANDIDATE } });
      }
    }
    if (
      parsed.hostname === new URL(P519_RENDER_SERVICES.control.url).hostname
    ) {
      if (parsed.pathname === "/p519/v1/state") {
        mode = JSON.parse(init.body).mode;
        return jsonResponse({ authority_available: true, mode });
      }
      if (parsed.pathname === "/p519/v1/status") {
        return jsonResponse({
          authority_available: true,
          mode,
          tenant_count: 2,
        });
      }
    }
    if (parsed.pathname === "/readyz") {
      return new Response("", { status: mode === "off" ? 503 : 200 });
    }
    if (parsed.pathname === "/metrics") {
      return new Response(
        [
          "collab_documents_current 0",
          'collab_connections_current{capability="edit"} 0',
        ].join("\n"),
        { status: 200 },
      );
    }
    throw new Error(`unexpected request ${url}`);
  }
  return { calls, fetchImpl };
}

function environment() {
  return {
    control: {
      P5_COLLAB_20_INITIAL_MODE: "off",
      P5_COLLAB_20_TENANT_IDS: TENANTS.join(","),
    },
    runtime: { COLLAB_BUILD_ID: CANDIDATE },
  };
}

function adapter(provider = fakeProvider()) {
  return {
    adapter: createP520RenderAdapter({
      adminToken: TOKEN,
      apiKey: TOKEN,
      delay: async () => {},
      expectedTargetFingerprintSha256: FINGERPRINT,
      fetchImpl: provider.fetchImpl,
      metricsToken: TOKEN,
      timeoutMs: 5_000,
    }),
    provider,
  };
}

test("Render adapter scopes sync and deploy to the exact two disposable services", async () => {
  const fixture = adapter();
  assert.deepEqual(
    await fixture.adapter.assertExactTarget({
      candidateSha: CANDIDATE,
      services: P519_RENDER_SERVICES,
      targetFingerprintSha256: FINGERPRINT,
    }),
    { serviceCount: 2 },
  );
  await fixture.adapter.syncEnvironment(environment());
  assert.deepEqual(await fixture.adapter.deployCandidate(CANDIDATE), {
    controlDeployId: "dep-control",
    runtimeDeployId: "dep-runtime",
  });
  await fixture.adapter.setMode("read_only");
  assert.deepEqual(await fixture.adapter.verifyMode("read_only"), {
    authorityAvailable: true,
    documents: 0,
    editConnections: 0,
    mode: "read_only",
    runtimeReady: true,
  });
  const mutations = fixture.provider.calls.filter(
    ([method]) => method !== "GET",
  );
  assert.equal(mutations.length, 6);
});

test("Render adapter rejects non-allowlisted environment keys before mutation", async () => {
  const fixture = adapter();
  const invalid = environment();
  invalid.control.SECRET = "must-not-sync";
  await assert.rejects(
    fixture.adapter.syncEnvironment(invalid),
    /control_environment_invalid/u,
  );
  assert.equal(fixture.provider.calls.length, 0);
});

test("Render adapter requires initial off and exact-two canonical tenants", () => {
  const initialEnabled = environment();
  initialEnabled.control.P5_COLLAB_20_INITIAL_MODE = "enabled";
  assert.throws(
    () => validateP520RenderEnvironment(initialEnabled),
    /environment_value_invalid/u,
  );
  const oneTenant = environment();
  oneTenant.control.P5_COLLAB_20_TENANT_IDS = TENANTS[0];
  assert.throws(
    () => validateP520RenderEnvironment(oneTenant),
    /environment_value_invalid/u,
  );
});

test("Render adapter rejects target fingerprint and service drift", async () => {
  const fixture = adapter();
  await assert.rejects(
    fixture.adapter.assertExactTarget({
      candidateSha: CANDIDATE,
      services: P519_RENDER_SERVICES,
      targetFingerprintSha256: "c".repeat(64),
    }),
    /target_invalid/u,
  );
  await assert.rejects(
    fixture.adapter.assertExactTarget({
      candidateSha: CANDIDATE,
      services: {
        control: P519_RENDER_SERVICES.control,
        runtime: {
          ...P519_RENDER_SERVICES.runtime,
          url: "https://wrong.invalid",
        },
      },
      targetFingerprintSha256: FINGERPRINT,
    }),
    /target_invalid/u,
  );
  assert.equal(fixture.provider.calls.length, 0);
});

test("Render adapter keeps credentials out of its public surface and failures", async () => {
  const fixture = adapter();
  assert.equal(JSON.stringify(fixture.adapter).includes(TOKEN), false);
  await assert.rejects(fixture.adapter.setMode("invalid"), (error) => {
    assert.equal(error.message.includes(TOKEN), false);
    return /mode_invalid/u.test(error.message);
  });
});
