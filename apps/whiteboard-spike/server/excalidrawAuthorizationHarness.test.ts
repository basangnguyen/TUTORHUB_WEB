// @vitest-environment node

import { afterEach, describe, expect, it } from "vitest";
import {
  CollaborationControlPlane,
  ConnectionQuota,
  createAuthorizedTestClient,
  startAuthorizedExcalidrawServer,
  waitForAuthorizedClient,
  waitUntil,
  type AuthorizedExcalidrawServer,
  type AuthorizedTestClient,
} from "./excalidrawAuthorizationHarness";
import {
  EXCALIDRAW_AUTHORIZATION_LIMITS,
  type CollaborationCapability,
  type CollaborationGrantRequest,
} from "../src/excalidraw/authorizationContract";
import {
  CanonicalExcalidrawAuthority,
  excalidrawSceneToCanonical,
} from "../src/excalidraw/canonicalAuthority";

const origin = "http://127.0.0.1:4180";
const resources: Array<AuthorizedExcalidrawServer | AuthorizedTestClient> = [];
const authorities: CanonicalExcalidrawAuthority[] = [];

afterEach(async () => {
  for (const authority of authorities.splice(0).reverse()) {
    authority.destroy();
  }
  for (const resource of resources.splice(0).reverse()) {
    await resource.destroy();
  }
});

describe("Excalidraw Gate C control-plane grants", () => {
  it("issues opaque <=60 second grants, binds scope/session/origin, and denies replay", () => {
    let now = 1_786_000_000_000;
    const control = new CollaborationControlPlane(origin, undefined, () => now);
    const request = grantRequest("teacher-a", "teacher-session", "edit");
    const issued = control.issueGrant(request, origin, 60_000);

    expect(issued.expiresInSeconds).toBe(60);
    expect(issued.grant).not.toContain("tenant-a");
    expect(issued.grant).not.toContain("teacher-a");
    expect(issued.providerDocumentName).not.toContain("tenant-a");
    expect(issued.providerDocumentName).not.toContain("board-1");

    const exchanged = control.exchangeGrant(
      issued.grant,
      origin,
      issued.providerDocumentName,
    );
    expect(exchanged).toMatchObject({
      actorId: "teacher-a",
      capability: "edit",
      documentId: "board-1",
      generation: 1,
      sessionId: "teacher-session",
      tenantId: "tenant-a",
    });
    expect(() =>
      control.exchangeGrant(issued.grant, origin, issued.providerDocumentName),
    ).toThrowError("grant_invalid_or_replayed");

    const expiring = control.issueGrant(request, origin, 1);
    now += 2;
    expect(() =>
      control.exchangeGrant(
        expiring.grant,
        origin,
        expiring.providerDocumentName,
      ),
    ).toThrowError("grant_expired");
  });

  it("denies forged capability, cross-tenant membership, wrong session, stale generation, and authority outage", () => {
    const control = new CollaborationControlPlane(origin);
    expect(() =>
      control.issueGrant(
        grantRequest("viewer-c", "viewer-session", "edit"),
        origin,
      ),
    ).toThrowError("capability_escalation_denied");
    expect(() =>
      control.issueGrant(
        {
          ...grantRequest("teacher-a", "teacher-session", "edit"),
          tenantId: "tenant-b",
        },
        origin,
      ),
    ).toThrowError("membership_denied");
    expect(() =>
      control.issueGrant(
        grantRequest("teacher-a", "wrong-session", "edit"),
        origin,
      ),
    ).toThrowError("session_binding_denied");
    expect(() =>
      control.issueGrant(
        {
          ...grantRequest("teacher-a", "teacher-session", "edit"),
          expectedGeneration: 2,
        },
        origin,
      ),
    ).toThrowError("stale_generation");
    expect(() =>
      control.issueGrant(
        grantRequest("teacher-a", "teacher-session", "edit"),
        "https://attacker.invalid",
      ),
    ).toThrowError("origin_denied");

    control.setAvailable(false);
    expect(() =>
      control.issueGrant(
        grantRequest("teacher-a", "teacher-session", "edit"),
        origin,
      ),
    ).toThrowError("authorization_authority_unavailable");
  });
});

describe("Excalidraw Gate C Hocuspocus authorization", () => {
  it("replicates editor mutations to a viewer while rejecting viewer protocol writes", async () => {
    const server = await startAuthorizedExcalidrawServer({
      allowedOrigin: origin,
    });
    resources.push(server);
    const editorGrant = server.controlPlane.issueGrant(
      grantRequest("teacher-a", "teacher-session", "edit"),
      origin,
    );
    const viewerGrant = server.controlPlane.issueGrant(
      grantRequest("viewer-c", "viewer-session", "view"),
      origin,
    );
    const editor = createClient(server, editorGrant);
    const viewer = createClient(server, viewerGrant);
    await Promise.all([
      waitForAuthorizedClient(editor),
      waitForAuthorizedClient(viewer),
    ]);
    expect(viewer.provider.authorizedScope).toBe("readonly");

    const editorAuthority = createAuthority(editor, "teacher-a");
    authorities.push(editorAuthority);
    editorAuthority.initialize(initialScene());
    await waitUntil(
      () =>
        viewer.document
          .getMap("tutorhub.excalidraw.metadata.v1")
          .get("schemaVersion") === 1,
      "viewer did not receive canonical bootstrap",
    );
    const viewerAuthority = createAuthority(viewer, "viewer-c");
    authorities.push(viewerAuthority);
    expect(viewerAuthority.getScene().elements).toHaveLength(1);

    editorAuthority.putElement(rectangle("teacher-shape", 320));
    await waitUntil(
      () =>
        viewerAuthority
          .getScene()
          .elements.some((element) => element.id === "teacher-shape"),
      "viewer did not receive editor update",
    );

    viewerAuthority.putElement(rectangle("forged-viewer-shape", 520));
    await waitUntil(
      () => (server.evidence.rejectionCounts.reader_mutation_denied ?? 0) > 0,
      "viewer mutation was not rejected",
    );
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(
      editorAuthority
        .getScene()
        .elements.some((element) => element.id === "forged-viewer-shape"),
    ).toBe(false);
  }, 20_000);

  it("denies wrong Origin/document, fake grants, replay, and stale generation", async () => {
    const server = await startAuthorizedExcalidrawServer({
      allowedOrigin: origin,
    });
    resources.push(server);

    const validGrant = server.controlPlane.issueGrant(
      grantRequest("teacher-a", "teacher-session", "edit"),
      origin,
    );
    const first = createClient(server, validGrant);
    await waitForAuthorizedClient(first);

    const replay = createClient(server, validGrant);
    await waitUntil(
      () => replay.authenticationFailures.length > 0,
      "replayed grant was not denied",
    );

    const wrongDocumentGrant = server.controlPlane.issueGrant(
      grantRequest("student-b", "student-session", "edit"),
      origin,
    );
    const wrongDocument = createAuthorizedTestClient({
      grant: wrongDocumentGrant.grant,
      origin,
      providerDocumentName: `${wrongDocumentGrant.providerDocumentName}-forged`,
      providerUrl: server.providerUrl,
    });
    resources.push(wrongDocument);
    await waitUntil(
      () => wrongDocument.authenticationFailures.length > 0,
      "forged provider document was not denied",
    );

    const wrongOriginGrant = server.controlPlane.issueGrant(
      grantRequest("student-b", "student-session", "edit"),
      origin,
    );
    const wrongOrigin = createAuthorizedTestClient({
      grant: wrongOriginGrant.grant,
      origin: "https://attacker.invalid",
      providerDocumentName: wrongOriginGrant.providerDocumentName,
      providerUrl: server.providerUrl,
    });
    resources.push(wrongOrigin);
    await waitUntil(
      () => wrongOrigin.authenticationFailures.length > 0,
      "wrong origin was not denied",
    );

    const fake = createAuthorizedTestClient({
      grant: "opaque-but-forged-grant",
      origin,
      providerDocumentName: validGrant.providerDocumentName,
      providerUrl: server.providerUrl,
    });
    resources.push(fake);
    await waitUntil(
      () => fake.authenticationFailures.length > 0,
      "fake grant was not denied",
    );

    const stale = server.controlPlane.issueGrant(
      grantRequest("student-b", "student-session", "edit"),
      origin,
    );
    server.controlPlane.transitionDocument({
      action: "restore",
      documentId: "board-1",
      tenantId: "tenant-a",
    });
    const staleClient = createClient(server, stale);
    await waitUntil(
      () => staleClient.authenticationFailures.length > 0,
      "stale generation grant was not denied",
    );
  }, 20_000);

  it("increments generation on revoke/close/restore and closes stale sockets within budget", async () => {
    const server = await startAuthorizedExcalidrawServer({
      allowedOrigin: origin,
    });
    resources.push(server);
    const teacher = createClient(
      server,
      server.controlPlane.issueGrant(
        grantRequest("teacher-a", "teacher-session", "edit"),
        origin,
      ),
    );
    const student = createClient(
      server,
      server.controlPlane.issueGrant(
        grantRequest("student-b", "student-session", "edit"),
        origin,
      ),
    );
    await Promise.all([
      waitForAuthorizedClient(teacher),
      waitForAuthorizedClient(student),
    ]);

    const startedAt = Date.now();
    const revoked = server.controlPlane.transitionDocument({
      action: "revoke",
      actorId: "teacher-a",
      documentId: "board-1",
      tenantId: "tenant-a",
    });
    expect(revoked.nextGeneration).toBe(2);
    await waitUntil(
      () => server.evidence.activeConnections === 0,
      "revoked generation sockets did not close",
      EXCALIDRAW_AUTHORIZATION_LIMITS.socketRevocationBudgetMs,
    );
    expect(Date.now() - startedAt).toBeLessThanOrEqual(
      EXCALIDRAW_AUTHORIZATION_LIMITS.socketRevocationBudgetMs,
    );
    expect(() =>
      server.controlPlane.issueGrant(
        {
          ...grantRequest("teacher-a", "teacher-session", "edit"),
          expectedGeneration: 2,
        },
        origin,
      ),
    ).toThrowError("actor_revoked");

    const studentGenerationTwo = server.controlPlane.issueGrant(
      {
        ...grantRequest("student-b", "student-session", "edit"),
        expectedGeneration: 2,
      },
      origin,
    );
    expect(studentGenerationTwo.generation).toBe(2);

    const closed = server.controlPlane.transitionDocument({
      action: "close",
      documentId: "board-1",
      tenantId: "tenant-a",
    });
    expect(closed.nextGeneration).toBe(3);
    expect(() =>
      server.controlPlane.issueGrant(
        grantRequest("student-b", "student-session", "view"),
        origin,
      ),
    ).toThrowError("document_closed");

    const restored = server.controlPlane.transitionDocument({
      action: "restore",
      documentId: "board-1",
      tenantId: "tenant-a",
    });
    expect(restored.nextGeneration).toBe(4);
    expect(
      server.controlPlane.issueGrant(
        {
          ...grantRequest("student-b", "student-session", "edit"),
          expectedGeneration: 4,
        },
        origin,
      ).generation,
    ).toBe(4);
  }, 20_000);

  it("rejects oversized and structurally corrupt canonical updates before replication", async () => {
    const server = await startAuthorizedExcalidrawServer({
      allowedOrigin: origin,
    });
    resources.push(server);
    const trusted = createClient(
      server,
      server.controlPlane.issueGrant(
        grantRequest("teacher-a", "teacher-session", "edit"),
        origin,
      ),
    );
    const attacker = createClient(
      server,
      server.controlPlane.issueGrant(
        grantRequest("student-b", "student-session", "edit"),
        origin,
      ),
    );
    await Promise.all([
      waitForAuthorizedClient(trusted),
      waitForAuthorizedClient(attacker),
    ]);
    const trustedAuthority = createAuthority(trusted, "teacher-a");
    authorities.push(trustedAuthority);
    trustedAuthority.initialize(initialScene());
    await waitUntil(
      () =>
        attacker.document
          .getMap("tutorhub.excalidraw.metadata.v1")
          .get("schemaVersion") === 1,
      "attacker fixture did not receive bootstrap",
    );

    attacker.document.getMap<string>("tutorhub.excalidraw.page.v1").set(
      "state",
      JSON.stringify({
        id: "page-1",
        name: nestedValue(30),
        backgroundColor: "#fff",
      }),
    );
    await waitUntil(
      () => (server.evidence.rejectionCounts.scene_budget_denied ?? 0) > 0,
      "deep/corrupt canonical update was not rejected",
    );
    expect(trustedAuthority.getScene().page.name).toBe("Bai 1");

    const oversizeGrant = server.controlPlane.issueGrant(
      grantRequest("student-b", "student-session", "edit"),
      origin,
    );
    const oversize = createClient(server, oversizeGrant);
    await waitForAuthorizedClient(oversize);
    oversize.document
      .getMap("malicious")
      .set(
        "payload",
        "x".repeat(EXCALIDRAW_AUTHORIZATION_LIMITS.maxUpdateBytes + 64 * 1024),
      );
    await waitUntil(
      () => (server.evidence.rejectionCounts.update_too_large ?? 0) > 0,
      "oversized update was not rejected",
    );
    expect(trusted.document.getMap("malicious").has("payload")).toBe(false);
  }, 25_000);
});

describe("Excalidraw Gate C connection and reconnect quotas", () => {
  it("enforces actor/document/tenant ceilings, reconnect storms, and fails closed", () => {
    const actorQuota = new ConnectionQuota();
    const actorReleases = Array.from(
      { length: EXCALIDRAW_AUTHORIZATION_LIMITS.maxConnectionsPerActor },
      () => actorQuota.acquire(context("same-actor", "doc-a"), 1),
    );
    expect(() =>
      actorQuota.acquire(context("same-actor", "doc-b"), 1),
    ).toThrowError("actor_connection_quota");
    actorReleases.forEach((release) => release());

    const documentQuota = new ConnectionQuota();
    const documentReleases = Array.from(
      { length: EXCALIDRAW_AUTHORIZATION_LIMITS.maxConnectionsPerDocument },
      (_, index) =>
        documentQuota.acquire(context(`actor-${index}`, "shared-doc"), 1),
    );
    expect(() =>
      documentQuota.acquire(context("overflow-actor", "shared-doc"), 1),
    ).toThrowError("document_connection_quota");
    documentReleases.forEach((release) => release());

    const tenantQuota = new ConnectionQuota();
    const tenantReleases = Array.from(
      { length: EXCALIDRAW_AUTHORIZATION_LIMITS.maxConnectionsPerTenant },
      (_, index) =>
        tenantQuota.acquire(
          context(`tenant-actor-${index}`, `doc-${index}`),
          1,
        ),
    );
    expect(() =>
      tenantQuota.acquire(context("tenant-overflow", "doc-overflow"), 1),
    ).toThrowError("tenant_connection_quota");
    tenantReleases.forEach((release) => release());

    const reconnectQuota = new ConnectionQuota();
    for (
      let index = 0;
      index < EXCALIDRAW_AUTHORIZATION_LIMITS.maxReconnectAttemptsPerWindow;
      index += 1
    ) {
      reconnectQuota.acquire(context("reconnecting", "doc-a"), 1)();
    }
    expect(() =>
      reconnectQuota.acquire(context("reconnecting", "doc-a"), 1),
    ).toThrowError("reconnect_storm_denied");

    const unavailable = new ConnectionQuota();
    unavailable.setAvailable(false);
    expect(() =>
      unavailable.acquire(context("actor-a", "doc-a"), 1),
    ).toThrowError("rate_authority_unavailable");
  });
});

function grantRequest(
  actorId: string,
  sessionId: string,
  requestedCapability: CollaborationCapability,
): CollaborationGrantRequest {
  return {
    actorId,
    documentId: "board-1",
    requestedCapability,
    sessionId,
    tenantId: "tenant-a",
  };
}

function createClient(
  server: AuthorizedExcalidrawServer,
  grant: { grant: string; providerDocumentName: string },
): AuthorizedTestClient {
  const client = createAuthorizedTestClient({
    grant: grant.grant,
    origin,
    providerDocumentName: grant.providerDocumentName,
    providerUrl: server.providerUrl,
  });
  resources.push(client);
  return client;
}

function createAuthority(
  client: AuthorizedTestClient,
  actorId: string,
): CanonicalExcalidrawAuthority {
  return new CanonicalExcalidrawAuthority(
    client.document,
    { documentId: "board-1", generation: 1, tenantId: "tenant-a" },
    actorId,
  );
}

function initialScene() {
  return excalidrawSceneToCanonical({
    appState: { viewBackgroundColor: "#f8fafc" },
    elements: [rectangle("initial-shape", 100)],
    files: {},
    page: { id: "page-1", name: "Bai 1" },
  });
}

function rectangle(id: string, x: number) {
  return {
    angle: 0,
    backgroundColor: "#a5d8ff",
    boundElements: null,
    fillStyle: "solid",
    frameId: null,
    groupIds: [],
    height: 100,
    id,
    index: null,
    isDeleted: false,
    link: null,
    locked: false,
    opacity: 100,
    roughness: 1,
    roundness: null,
    seed: x + 1,
    strokeColor: "#1c3f60",
    strokeStyle: "solid",
    strokeWidth: 2,
    type: "rectangle",
    updated: 1_786_000_000_000,
    version: 1,
    versionNonce: x + 2,
    width: 160,
    x,
    y: 120,
  };
}

function nestedValue(depth: number): unknown {
  let value: unknown = "deep";
  for (let index = 0; index < depth; index += 1) {
    value = { nested: value };
  }
  return value;
}

function context(actorId: string, documentId: string) {
  return {
    actorId,
    capability: "edit" as const,
    documentId,
    generation: 1,
    providerDocumentName: "opaque-provider-document",
    sessionId: `${actorId}-session`,
    tenantId: "tenant-a",
  };
}
