import { expect, test, type APIRequestContext } from "@playwright/test";
import { WebSocket } from "ws";

const HTTP_ORIGIN = "http://127.0.0.1:4178";
const WS_URL = "ws://127.0.0.1:4179";
const PROTOCOL = "tutorhub-sync-v1";

test.describe("P5-COLLAB-01 WebSocket credential and abuse gates", () => {
  test("one-time grant bị khóa theo origin/document và không replay", async ({
    request,
  }) => {
    const documentId = uniqueDocument("binding");
    const grant = await issueGrant(request, documentId, "editor-binding");

    expect(
      await rejectedUpgrade(
        grant,
        documentId,
        "session-wrong-origin",
        "https://attacker.invalid",
      ),
    ).toBe(403);
    const accepted = await openSocket(
      grant,
      documentId,
      "session-binding",
      HTTP_ORIGIN,
    );
    accepted.close();
    await waitForClosed(accepted);
    expect(
      await rejectedUpgrade(grant, documentId, "session-replay", HTTP_ORIGIN),
    ).toBe(401);

    const crossGrant = await issueGrant(
      request,
      documentId,
      "editor-cross-doc",
    );
    expect(
      await rejectedUpgrade(
        crossGrant,
        `${documentId}-other`,
        "session-cross-doc",
        HTTP_ORIGIN,
      ),
    ).toBe(401);
    expect(
      await rejectedUpgrade(
        crossGrant,
        documentId,
        "session-consumed-mismatch",
        HTTP_ORIGIN,
      ),
    ).toBe(401);
  });

  test("actor connection quota, oversized frame và message rate fail closed", async ({
    request,
  }) => {
    const quotaDocument = uniqueDocument("quota");
    const sockets: WebSocket[] = [];
    for (let index = 0; index < 4; index += 1) {
      const grant = await issueGrant(request, quotaDocument, "editor-quota");
      sockets.push(
        await openSocket(
          grant,
          quotaDocument,
          `session-quota-${index}`,
          HTTP_ORIGIN,
        ),
      );
    }
    const fifthGrant = await issueGrant(request, quotaDocument, "editor-quota");
    expect(
      await rejectedUpgrade(
        fifthGrant,
        quotaDocument,
        "session-quota-fifth",
        HTTP_ORIGIN,
      ),
    ).toBe(401);
    for (const socket of sockets) socket.terminate();

    const oversizeDocument = uniqueDocument("oversize");
    const oversizeGrant = await issueGrant(
      request,
      oversizeDocument,
      "editor-oversize",
    );
    const oversized = await openSocket(
      oversizeGrant,
      oversizeDocument,
      "session-oversize",
      HTTP_ORIGIN,
    );
    const oversizedClose = closeCode(oversized);
    oversized.send("x".repeat(600 * 1024));
    expect(await oversizedClose).not.toBe(1000);

    const rateDocument = uniqueDocument("rate");
    const rateGrant = await issueGrant(request, rateDocument, "editor-rate");
    const noisy = await openSocket(
      rateGrant,
      rateDocument,
      "session-rate",
      HTTP_ORIGIN,
    );
    const noisyClose = closeCode(noisy);
    for (
      let index = 0;
      index < 140 && noisy.readyState === WebSocket.OPEN;
      index += 1
    ) {
      noisy.send('{"type":"ping"}');
    }
    expect(await noisyClose).not.toBe(1000);
  });
});

async function issueGrant(
  request: APIRequestContext,
  documentId: string,
  persona: string,
): Promise<string> {
  const response = await request.post("http://127.0.0.1:4179/gate/grants", {
    headers: { origin: HTTP_ORIGIN },
    data: {
      tenantId: "tenant-gate",
      documentId,
      actorId: `tenant-gate:${persona}`,
      requestedCapability: "edit",
    },
  });
  expect(response.status()).toBe(201);
  const body = (await response.json()) as { grant?: string };
  if (!body.grant) throw new Error("grant_missing");
  return body.grant;
}

function openSocket(
  grant: string,
  documentId: string,
  sessionId: string,
  origin: string,
): Promise<WebSocket> {
  return new Promise((resolve, reject) => {
    const socket = createSocket(grant, documentId, sessionId, origin);
    socket.once("open", () => resolve(socket));
    socket.once("unexpected-response", (_request, response) => {
      response.resume();
      reject(new Error(`unexpected_status_${response.statusCode ?? 0}`));
    });
    socket.once("error", reject);
  });
}

function rejectedUpgrade(
  grant: string,
  documentId: string,
  sessionId: string,
  origin: string,
): Promise<number> {
  return new Promise((resolve, reject) => {
    const socket = createSocket(grant, documentId, sessionId, origin);
    socket.once("open", () => {
      socket.terminate();
      reject(new Error("upgrade_unexpectedly_accepted"));
    });
    socket.once("unexpected-response", (_request, response) => {
      const status = response.statusCode ?? 0;
      response.resume();
      resolve(status);
    });
    socket.once("error", () => undefined);
  });
}

function createSocket(
  grant: string,
  documentId: string,
  sessionId: string,
  origin: string,
): WebSocket {
  const url = new URL(`/connect/${documentId}`, WS_URL);
  url.searchParams.set("sessionId", sessionId);
  url.searchParams.set("storeId", `${sessionId}-store`);
  return new WebSocket(url, [PROTOCOL, `tutorhub-grant.${grant}`], { origin });
}

function closeCode(socket: WebSocket): Promise<number> {
  return new Promise((resolve) => {
    socket.once("close", (code) => resolve(code));
  });
}

async function waitForClosed(socket: WebSocket): Promise<void> {
  if (socket.readyState === WebSocket.CLOSED) return;
  await new Promise<void>((resolve) => socket.once("close", () => resolve()));
}

function uniqueDocument(prefix: string): string {
  return `document-${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
}
