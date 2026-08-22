import { describe, expect, it } from "vitest";
import { ControlPlaneError, HttpControlPlane } from "./controlPlane.js";

const DOCUMENT_NAME = "wb_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const AUTHORITY_LEASE = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const ORIGIN = "https://app.example.test";

describe("HTTP collaboration control plane", () => {
  it("exchanges a one-time grant and revalidates the exact authority lease", async () => {
    const requests: Array<{ body: string; url: string }> = [];
    const fetcher: typeof fetch = async (input, init) => {
      const url = String(input);
      requests.push({ body: String(init?.body ?? ""), url });
      if (url.endsWith("/grants/exchange")) {
        return jsonResponse({
          actor_id: "11111111-1111-4111-8111-111111111111",
          authority_lease: AUTHORITY_LEASE,
          capability: "edit",
          document_id: "22222222-2222-4222-8222-222222222222",
          generation: 3,
          max_connections_per_tenant: 50,
          max_operations_per_minute: 6_000,
          max_storage_bytes_per_tenant: 1_073_741_824,
          provider_document_name: DOCUMENT_NAME,
          session_id: "33333333-3333-4333-8333-333333333333",
          tenant_id: "44444444-4444-4444-8444-444444444444",
          writer_fence: 5,
        });
      }
      return jsonResponse({ valid_authority_leases: [AUTHORITY_LEASE] });
    };
    const control = new HttpControlPlane(
      "https://control.example.test",
      "service-token-that-is-long-enough",
      1_000,
      fetcher,
    );

    const scope = await control.exchangeGrant({
      documentName: DOCUMENT_NAME,
      grant: "one-time-grant-that-is-long-enough",
      origin: ORIGIN,
    });
    await expect(control.validateScopes([scope])).resolves.toEqual(
      new Set([AUTHORITY_LEASE]),
    );

    expect(scope.origin).toBe(ORIGIN);
    expect(scope.authorityLease).toBe(AUTHORITY_LEASE);
    const validationBody = JSON.parse(requests[1]?.body ?? "null") as {
      scopes?: Array<Record<string, unknown>>;
    };
    expect(validationBody.scopes?.[0]).toMatchObject({
      actor_id: scope.actorId,
      authority_lease: AUTHORITY_LEASE,
      origin: ORIGIN,
      provider_document_name: DOCUMENT_NAME,
      writer_fence: 5,
    });
  });

  it("rejects a validation response containing an unrequested lease", async () => {
    const fetcher: typeof fetch = async () =>
      jsonResponse({
        valid_authority_leases: ["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"],
      });
    const control = new HttpControlPlane(
      "https://control.example.test",
      "service-token-that-is-long-enough",
      1_000,
      fetcher,
    );
    const scope = {
      actorId: "11111111-1111-4111-8111-111111111111",
      authorityLease: AUTHORITY_LEASE,
      capability: "view" as const,
      documentId: "22222222-2222-4222-8222-222222222222",
      generation: 3,
      maxConnectionsPerTenant: 50,
      maxOperationsPerMinute: 6_000,
      maxStorageBytesPerTenant: 1_073_741_824,
      origin: ORIGIN,
      providerDocumentName: DOCUMENT_NAME,
      sessionId: "33333333-3333-4333-8333-333333333333",
      tenantId: "44444444-4444-4444-8444-444444444444",
      writerFence: 5,
    };

    await expect(control.validateScopes([scope])).rejects.toEqual(
      new ControlPlaneError("control_plane_unavailable"),
    );
  });
});

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    headers: { "content-type": "application/json" },
    status: 200,
  });
}
