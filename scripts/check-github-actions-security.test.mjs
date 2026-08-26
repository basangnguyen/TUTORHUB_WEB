import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { auditWorkflowSource } from "./check-github-actions-security.mjs";

const checkoutSha = "9f698171ed81b15d1823a05fc7211befd50c8ae0";

function workflow(
  action = `actions/checkout@${checkoutSha}`,
  checkoutOptions = "      persist-credentials: false",
) {
  return `name: Verify
on: [push]
permissions:
  contents: read
concurrency:
  group: verify
  cancel-in-progress: true
jobs:
  verify:
    runs-on: ubuntu-24.04
    timeout-minutes: 10
    steps:
      - uses: ${action}
        with:
${checkoutOptions}
`;
}

test("accepts a least-privilege workflow with an immutable action", () => {
  assert.deepEqual(auditWorkflowSource(workflow()), []);
});

test("rejects mutable action tags", () => {
  assert.match(
    auditWorkflowSource(workflow("actions/checkout@v6")).join("\n"),
    /full commit SHA/,
  );
});

test("rejects pull_request_target", () => {
  assert.match(
    auditWorkflowSource(`${workflow()}\npull_request_target:\n`).join("\n"),
    /pull_request_target/,
  );
});

test("rejects missing root permissions", () => {
  assert.match(
    auditWorkflowSource(
      workflow().replace("permissions:\n  contents: read\n", ""),
    ).join("\n"),
    /permissions/,
  );
});

test("rejects checkout credential persistence", () => {
  assert.match(
    auditWorkflowSource(workflow(undefined, "      fetch-depth: 0")).join("\n"),
    /persist-credentials/,
  );
});

test("rejects write permissions outside security event upload", () => {
  assert.match(
    auditWorkflowSource(
      workflow().replace(
        "  contents: read",
        "  contents: read\n  packages: write",
      ),
    ).join("\n"),
    /packages/,
  );
});

test("Core API runtime requires the patched Alpine OpenSSL release", async () => {
  const dockerfile = await readFile(
    new URL(
      "../infrastructure/huggingface/core-api/Dockerfile",
      import.meta.url,
    ),
    "utf8",
  );
  const runtimeStage = dockerfile.slice(
    dockerfile.lastIndexOf("FROM alpine:3.23"),
  );

  assert.match(runtimeStage, /apk add --no-cache --upgrade/);
  assert.match(runtimeStage, /libcrypto3>=3\.5\.8-r0/);
  assert.match(runtimeStage, /libssl3>=3\.5\.8-r0/);
  assert.ok(
    runtimeStage.indexOf("apk add --no-cache --upgrade") <
      runtimeStage.indexOf("USER tutorhub"),
    "OpenSSL packages must be upgraded before the runtime drops root privileges",
  );
});
