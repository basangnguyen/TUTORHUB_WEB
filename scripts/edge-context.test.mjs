import assert from "node:assert/strict";
import test from "node:test";

import {
  canonicalClientPrefix,
  canonicalEdgeContext,
  onRequest,
  signEdgeContext,
} from "../functions/api/[[path]].ts";

test("Cloudflare signer matches the Go verifier golden vector", async () => {
  const canonical = canonicalEdgeContext(
    "v1",
    "1784548800",
    "post",
    "/api/v1/path?x=1",
    "203.0.113.0/24",
  );

  assert.equal(
    canonical,
    "v1\n1784548800\nPOST\n/api/v1/path?x=1\n203.0.113.0/24",
  );
  assert.equal(
    await signEdgeContext(
      new TextEncoder().encode("0123456789abcdef0123456789abcdef"),
      canonical,
    ),
    "M5qd8zMEjsOUEU3WfQAV-oJlUrgfeL9UoFpayvxodJo",
  );
});

test("Cloudflare prefix reduction keeps only the approved privacy prefix", () => {
  assert.equal(canonicalClientPrefix("203.0.113.87"), "203.0.113.0/24");
  assert.equal(
    canonicalClientPrefix("2001:db8:1234:56ff:abcd:ef01:2345:6789"),
    "2001:db8:1234:5600::/56",
  );
  assert.equal(canonicalClientPrefix("203.0.113.999"), null);
});

test("edge proxy errors are complete Problem Details responses", async () => {
  const response = await onRequest({
    request: new Request("https://tutorhub.example/api/ready"),
    env: {
      CORE_API_ORIGIN: "",
      EDGE_CONTEXT_SECRET: "",
    },
  });

  assert.equal(response.status, 503);
  assert.equal(
    response.headers.get("content-type"),
    "application/problem+json; charset=utf-8",
  );
  assert.deepEqual(await response.json(), {
    type: "urn:tutorhub:problem:http-503",
    title: "CORE_API_ORIGIN is not configured",
    status: 503,
  });
});
