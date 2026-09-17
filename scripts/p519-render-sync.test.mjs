import assert from "node:assert/strict";
import test from "node:test";

import {
  P519_RENDER_SERVICES,
  createP519RenderConfiguration,
  validateP519RenderService,
} from "./p519-render-sync.mjs";

function values(overrides = {}) {
  return new Map(
    Object.entries({
      B2_APPLICATION_KEY: "application-key-that-is-long-enough",
      B2_BUCKET: "p519-disposable-bucket",
      B2_ENDPOINT: "https://s3.us-west-000.backblazeb2.com",
      B2_KEY_ID: "0123456789abcdef",
      B2_REGION: "us-west-000",
      COLLAB_CONTROL_PLANE_TOKEN: "control-token-that-is-long-enough",
      COLLAB_METRICS_TOKEN: "metrics-token-that-is-long-enough",
      DATABASE_COLLABORATION_URL:
        "postgresql://tutorhub_collab_worker:password@example.test/app?sslmode=require",
      DATABASE_MIGRATION_URL:
        "postgresql://neondb_owner:password@example.test/app?sslmode=require",
      DATABASE_POLL_MAINTENANCE_URL:
        "postgresql://tutorhub_poll_maintenance:password@example.test/app?sslmode=require",
      DATABASE_POOL_URL:
        "postgresql://tutorhub_runtime:password@example-pooler.test/app?sslmode=require",
      P5_COLLAB_19_ALLOWED_ORIGIN: "https://p519-private-alpha.invalid",
      P5_COLLAB_19_CONTROL_ADMIN_TOKEN: "admin-token-that-is-long-enough",
      P5_COLLAB_19_CONTROL_TOKEN_CURRENT: "control-token-that-is-long-enough",
      P5_COLLAB_19_CONTROL_URL: P519_RENDER_SERVICES.control.url,
      P5_COLLAB_19_DISPOSABLE_CONFIRM:
        "I_UNDERSTAND_P5_COLLAB_19_DISPOSABLE_ONLY",
      P5_COLLAB_19_PROVIDER_DOCUMENT_NAMES:
        "wb_p519_private_alpha_document_01,wb_p519_private_alpha_document_02",
      P5_COLLAB_19_RENDER_API_KEY: "render-api-key-that-is-long-enough",
      P5_COLLAB_19_RUNTIME_URL: P519_RENDER_SERVICES.runtime.url,
      ...overrides,
    }),
  );
}

test("Render configuration binds only the exact disposable services", () => {
  const configuration = createP519RenderConfiguration(values(), {
    commitSha: "a".repeat(40),
    now: new Date("2026-09-05T01:02:03.004Z"),
  });
  assert.equal(configuration.commitSha, "a".repeat(40));
  assert.equal(
    configuration.serviceEnvironment.runtime.COLLAB_CONTROL_PLANE_TOKEN,
    configuration.serviceEnvironment.control.P5_COLLAB_19_CONTROL_TOKEN_CURRENT,
  );
  assert.equal(
    configuration.serviceEnvironment.runtime.DATABASE_COLLABORATION_URL,
    "postgresql://tutorhub_collab_worker:password@example.test/app?sslmode=require",
  );
  assert.match(configuration.runId, /^p519-private-alpha-/u);
});

test("Render configuration rejects mismatched control credentials", () => {
  assert.throws(
    () =>
      createP519RenderConfiguration(
        values({
          COLLAB_CONTROL_PLANE_TOKEN: "different-token-that-is-long-enough",
        }),
        { commitSha: "a".repeat(40) },
      ),
    /p519_control_tokens_must_match/u,
  );
});

test("Render service validation enforces free isolated main service", () => {
  const service = {
    autoDeploy: "no",
    branch: "main",
    id: "srv-0123456789abcdef",
    name: P519_RENDER_SERVICES.control.name,
    region: "singapore",
    serviceDetails: { plan: "free" },
    type: "web_service",
  };
  assert.equal(
    validateP519RenderService(service, P519_RENDER_SERVICES.control.name),
    service,
  );
  assert.throws(
    () =>
      validateP519RenderService(
        { ...service, autoDeploy: "yes" },
        P519_RENDER_SERVICES.control.name,
      ),
    /p519_render_service_contract_mismatch/u,
  );
});
