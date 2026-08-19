import { performance } from "node:perf_hooks";
import {
  CollaborationControlPlane,
  createAuthorizedTestClient,
  startAuthorizedExcalidrawServer,
  waitForAuthorizedClient,
  waitUntil,
  type AuthorizedTestClient,
  type MembershipFixture,
} from "./excalidrawAuthorizationHarness";
import {
  CanonicalExcalidrawAuthority,
  excalidrawSceneToCanonical,
  type CanonicalExcalidrawSceneV1,
} from "../src/excalidraw/canonicalAuthority";

const LOAD_ORIGIN = "http://127.0.0.1:4180";
const LOAD_TENANT = "tenant-load";
const LOAD_DOCUMENT = "board-load";
const LOAD_WRITER_ACTOR = "load-actor-000";

export interface ExcalidrawLoadProfile {
  clients: 2 | 10 | 50;
  elements: 500 | 2_000;
  name: "2x500" | "10x500" | "50x2000";
}

export interface ExcalidrawLoadBudget {
  cleanupMs: number;
  convergenceP95Ms: number;
  cpuMs: number;
  heapDeltaBytes: number;
  joinP95Ms: number;
  receivedBytes: number;
}

export interface ExcalidrawLoadResult {
  activeConnectionsAtPeak: number;
  cleanupActiveConnections: number;
  cleanupMs: number;
  clients: number;
  convergenceP95Ms: number;
  cpuMs: number;
  elements: number;
  encodedStateBytes: number;
  heapDeltaBytes: number;
  joinP95Ms: number;
  profile: ExcalidrawLoadProfile["name"];
  receivedBytes: number;
  semanticHash: string;
}

export const EXCALIDRAW_LOAD_PROFILES: readonly ExcalidrawLoadProfile[] = [
  { clients: 2, elements: 500, name: "2x500" },
  { clients: 10, elements: 500, name: "10x500" },
  { clients: 50, elements: 2_000, name: "50x2000" },
];

export const EXCALIDRAW_LOAD_BUDGETS: Readonly<
  Record<ExcalidrawLoadProfile["name"], ExcalidrawLoadBudget>
> = {
  "2x500": {
    cleanupMs: 2_000,
    convergenceP95Ms: 1_500,
    cpuMs: 3_000,
    heapDeltaBytes: 128 * 1024 * 1024,
    joinP95Ms: 3_000,
    receivedBytes: 4 * 1024 * 1024,
  },
  "10x500": {
    cleanupMs: 3_000,
    convergenceP95Ms: 2_500,
    cpuMs: 8_000,
    heapDeltaBytes: 320 * 1024 * 1024,
    joinP95Ms: 7_500,
    receivedBytes: 16 * 1024 * 1024,
  },
  "50x2000": {
    cleanupMs: 5_000,
    convergenceP95Ms: 5_000,
    cpuMs: 75_000,
    heapDeltaBytes: 1024 * 1024 * 1024,
    joinP95Ms: 20_000,
    receivedBytes: 128 * 1024 * 1024,
  },
};

export async function runExcalidrawLoadProfile(
  profile: ExcalidrawLoadProfile,
): Promise<ExcalidrawLoadResult> {
  const memberships = createMemberships(profile.clients);
  const controlPlane = new CollaborationControlPlane(LOAD_ORIGIN, memberships);
  const server = await startAuthorizedExcalidrawServer({
    allowedOrigin: LOAD_ORIGIN,
    controlPlane,
  });
  const clients: AuthorizedTestClient[] = [];
  const authorities: CanonicalExcalidrawAuthority[] = [];
  const heapBefore = process.memoryUsage().heapUsed;
  const cpuBefore = process.cpuUsage();
  let cleanupStartedAt = 0;

  try {
    const writerMembership = memberships[0];
    if (!writerMembership) {
      throw new Error("gate_e_profile_membership_missing");
    }
    const writerStartedAt = performance.now();
    const writer = createClient(server, writerMembership);
    clients.push(writer);
    await waitForAuthorizedClient(writer, 10_000);
    const joinLatencies = [performance.now() - writerStartedAt];
    const writerAuthority = createAuthority(writer, writerMembership.actorId);
    authorities.push(writerAuthority);
    await bootstrapExactExcalidrawScene(writerAuthority, profile.elements);

    const readerStarts = new Map<AuthorizedTestClient, number>();
    for (const membership of memberships.slice(1)) {
      const client = createClient(server, membership);
      clients.push(client);
      readerStarts.set(client, performance.now());
    }
    await Promise.all(
      clients.slice(1).map(async (client) => {
        await waitForAuthorizedClient(client, 20_000);
        joinLatencies.push(
          performance.now() - (readerStarts.get(client) ?? performance.now()),
        );
      }),
    );

    for (let index = 1; index < clients.length; index += 1) {
      const client = clients[index];
      const membership = memberships[index];
      if (!client || !membership) {
        throw new Error("gate_e_profile_client_membership_mismatch");
      }
      const authority = createAuthority(client, membership.actorId);
      authorities.push(authority);
    }
    const initialHash = writerAuthority.getSemanticHash();
    await waitUntil(
      () =>
        authorities.every(
          (authority) =>
            authority.getScene().elements.length === profile.elements &&
            authority.getSemanticHash() === initialHash,
        ),
      `gate_e_${profile.name}_initial_convergence_timeout`,
      20_000,
    );
    await waitUntil(
      () => server.evidence.activeConnections === profile.clients,
      `gate_e_${profile.name}_connection_peak_timeout`,
      5_000,
    );

    const baseline = writerAuthority.getScene();
    const updatedElement = updateFirstElement(baseline);
    writerAuthority.putElement(updatedElement);
    const expectedHash = writerAuthority.getSemanticHash();
    const convergenceStartedAt = performance.now();
    const convergenceLatencies = await Promise.all(
      clients.map(async (client) => {
        await waitUntil(
          () => clientHasElementVersion(client, updatedElement),
          `gate_e_${profile.name}_mutation_convergence_timeout`,
          10_000,
        );
        return performance.now() - convergenceStartedAt;
      }),
    );
    if (
      !authorities.every(
        (authority) => authority.getSemanticHash() === expectedHash,
      )
    ) {
      throw new Error(`gate_e_${profile.name}_semantic_divergence`);
    }

    const cpu = process.cpuUsage(cpuBefore);
    const heapDeltaBytes = Math.max(
      0,
      process.memoryUsage().heapUsed - heapBefore,
    );
    const encodedStateBytes = writerAuthority.encodeProviderState().byteLength;
    const receivedBytes = clients.reduce(
      (total, client) => total + client.traffic.receivedBytes,
      0,
    );
    const activeConnectionsAtPeak = server.evidence.activeConnections;

    authorities.splice(0).forEach((authority) => authority.destroy());
    cleanupStartedAt = performance.now();
    clients.splice(0).forEach((client) => client.destroy());
    await waitUntil(
      () => server.evidence.activeConnections === 0,
      `gate_e_${profile.name}_cleanup_timeout`,
      EXCALIDRAW_LOAD_BUDGETS[profile.name].cleanupMs,
    );
    const cleanupMs = performance.now() - cleanupStartedAt;

    return {
      activeConnectionsAtPeak,
      cleanupActiveConnections: server.evidence.activeConnections,
      cleanupMs: rounded(cleanupMs),
      clients: profile.clients,
      convergenceP95Ms: rounded(percentile95(convergenceLatencies)),
      cpuMs: rounded((cpu.user + cpu.system) / 1_000),
      elements: profile.elements,
      encodedStateBytes,
      heapDeltaBytes,
      joinP95Ms: rounded(percentile95(joinLatencies)),
      profile: profile.name,
      receivedBytes,
      semanticHash: expectedHash,
    };
  } finally {
    authorities.splice(0).forEach((authority) => authority.destroy());
    clients.splice(0).forEach((client) => client.destroy());
    if (cleanupStartedAt === 0 && server.evidence.activeConnections > 0) {
      await waitUntil(
        () => server.evidence.activeConnections === 0,
        `gate_e_${profile.name}_finally_cleanup_timeout`,
        5_000,
      ).catch(() => undefined);
    }
    await server.destroy();
  }
}

function createMemberships(count: number): MembershipFixture[] {
  return Array.from({ length: count }, (_, index) => ({
    actorId: `load-actor-${index.toString().padStart(3, "0")}`,
    capability: "edit",
    sessionId: `load-session-${index.toString().padStart(3, "0")}`,
    tenantId: LOAD_TENANT,
  }));
}

function createClient(
  server: Awaited<ReturnType<typeof startAuthorizedExcalidrawServer>>,
  membership: MembershipFixture,
): AuthorizedTestClient {
  const issued = server.controlPlane.issueGrant(
    {
      actorId: membership.actorId,
      documentId: LOAD_DOCUMENT,
      requestedCapability: "edit",
      sessionId: membership.sessionId,
      tenantId: membership.tenantId,
    },
    LOAD_ORIGIN,
  );
  return createAuthorizedTestClient({
    grant: issued.grant,
    origin: LOAD_ORIGIN,
    providerDocumentName: issued.providerDocumentName,
    providerUrl: server.providerUrl,
  });
}

function createAuthority(
  client: AuthorizedTestClient,
  actorId: string,
): CanonicalExcalidrawAuthority {
  return new CanonicalExcalidrawAuthority(
    client.document,
    { documentId: LOAD_DOCUMENT, generation: 1, tenantId: LOAD_TENANT },
    actorId,
  );
}

async function bootstrapExactExcalidrawScene(
  authority: CanonicalExcalidrawAuthority,
  elementCount: 500 | 2_000,
): Promise<void> {
  const initialCount = Math.min(500, elementCount);
  authority.initialize(createScene(initialCount));
  for (let count = initialCount + 100; count <= elementCount; count += 100) {
    authority.replaceScene(createScene(count));
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  await new Promise((resolve) => setTimeout(resolve, 80));
}

function createScene(elementCount: number): CanonicalExcalidrawSceneV1 {
  return excalidrawSceneToCanonical({
    appState: { viewBackgroundColor: "#f8fafc" },
    elements: Array.from({ length: elementCount }, (_, index) =>
      createRectangle(index),
    ),
    files: {},
    page: { id: "page-1", name: "Gate E load profile" },
  });
}

function createRectangle(index: number) {
  return {
    angle: 0,
    backgroundColor: "#a5d8ff",
    boundElements: null,
    fillStyle: "solid",
    frameId: null,
    groupIds: [],
    height: 60,
    id: `load-element-${index.toString().padStart(4, "0")}`,
    index: null,
    isDeleted: false,
    link: null,
    locked: false,
    opacity: 100,
    roughness: 1,
    roundness: null,
    seed: index + 1,
    strokeColor: "#1c3f60",
    strokeStyle: "solid",
    strokeWidth: 2,
    type: "rectangle",
    updated: 1_786_000_000_000 + index,
    version: 1,
    versionNonce: index + 2,
    width: 100,
    x: (index % 40) * 120,
    y: Math.floor(index / 40) * 80,
  };
}

function updateFirstElement(scene: CanonicalExcalidrawSceneV1) {
  const first = scene.elements[0];
  if (!first) {
    throw new Error("gate_e_profile_scene_empty");
  }
  return {
    ...first,
    updated: 1_786_100_000_000,
    version: typeof first.version === "number" ? first.version + 1 : 2,
    x: first.x + 1,
  };
}

function clientHasElementVersion(
  client: AuthorizedTestClient,
  expected: ReturnType<typeof updateFirstElement>,
): boolean {
  const value = client.document
    .getMap<string>("tutorhub.excalidraw.elements.v1")
    .get(`${LOAD_WRITER_ACTOR.length}:${LOAD_WRITER_ACTOR}${expected.id}`);
  if (typeof value !== "string") return false;
  try {
    const envelope = JSON.parse(value) as {
      value?: { version?: unknown; x?: unknown };
    };
    return (
      envelope.value?.version === expected.version &&
      envelope.value.x === expected.x
    );
  } catch {
    return false;
  }
}

function percentile95(values: number[]): number {
  const sorted = [...values].sort((left, right) => left - right);
  const index = Math.max(0, Math.ceil(sorted.length * 0.95) - 1);
  return sorted[index] ?? 0;
}

function rounded(value: number): number {
  return Math.round(value * 10) / 10;
}
