import type { Hocuspocus, WebSocketLike } from "@hocuspocus/server";
import { describe, expect, it } from "vitest";
import {
  installRawWebSocketIngressGate,
  type RawIngressDenialReason,
  type RawWebSocketIngressLimits,
} from "./rawWebSocketIngressGate.js";

const limits: RawWebSocketIngressLimits = {
  maxBytesPerWindow: 1_024,
  maxFrameBytes: 512,
  maxMessagesPerWindow: 8,
  maxPendingUnauthenticatedSockets: 2,
  maxQueuedBytesPerSocket: 768,
  maxQueuedMessagesPerSocket: 2,
  windowMs: 1_000,
};

describe("raw WebSocket ingress gate", () => {
  it("rejects oversized frames before Hocuspocus can enqueue them", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, limits);
    const socket = harness.connect();

    socket.connection.handleMessage(syncFrame(513));

    expect(socket.delegatedFrames).toHaveLength(0);
    expect(socket.websocket.closeEvents).toEqual([
      { code: 1009, reason: "raw_frame_too_large" },
    ]);
    expect(gate.snapshot()).toEqual({
      activeSockets: 0,
      pendingUnauthenticatedSockets: 0,
      queuedBytes: 0,
      queuedMessages: 0,
    });
    expect(harness.upstreamCloseCount).toBe(1);

    socket.connection.handleClose({ code: 1000, reason: "transport_closed" });
    expect(harness.upstreamCloseCount).toBe(1);
  });

  it("cleans up exactly once even when the rejection observer throws", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, {
      ...limits,
      onReject: () => {
        throw new Error("observer_failed");
      },
    });
    const socket = harness.connect();

    expect(() => socket.connection.handleMessage(syncFrame(513))).not.toThrow();
    expect(socket.websocket.closeEvents).toEqual([
      { code: 1009, reason: "raw_frame_too_large" },
    ]);
    expect(harness.upstreamCloseCount).toBe(1);
    expect(gate.snapshot()).toEqual({
      activeSockets: 0,
      pendingUnauthenticatedSockets: 0,
      queuedBytes: 0,
      queuedMessages: 0,
    });

    socket.connection.handleClose({ code: 1000, reason: "late_close" });
    socket.connection.handleClose({ code: 1000, reason: "duplicate_close" });
    expect(harness.upstreamCloseCount).toBe(1);
  });

  it("bounds burst queue depth until exact frames finish processing", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, limits);
    const socket = harness.connect();
    const first = syncFrame(200);
    const second = syncFrame(200);
    const denied = syncFrame(200);

    socket.connection.handleMessage(first);
    socket.connection.handleMessage(second);
    expect(gate.snapshot().queuedMessages).toBe(2);
    expect(harness.maxDelegatedWithoutCompletion).toBe(2);

    socket.connection.handleMessage(denied);
    expect(socket.delegatedFrames).toEqual([first, second]);
    expect(socket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_queue_messages_exceeded",
    );
    expect(harness.maxDelegatedWithoutCompletion).toBe(2);
  });

  it("releases queue capacity only after the matching frame completes", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, limits);
    const socket = harness.connect();
    const first = syncFrame(250);
    const second = syncFrame(250);
    const third = syncFrame(250);

    socket.connection.handleMessage(first);
    socket.connection.handleMessage(second);
    gate.completeProcessing(socket.connection.socketId, first);
    socket.connection.handleMessage(third);

    expect(socket.delegatedFrames).toEqual([first, second, third]);
    expect(socket.websocket.closeEvents).toHaveLength(0);
    expect(gate.snapshot()).toMatchObject({
      queuedBytes: 500,
      queuedMessages: 2,
    });
  });

  it("counts auth traffic for rate but never as document queue backlog", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, limits);
    const socket = harness.connect();

    socket.connection.handleMessage(authFrame());

    expect(socket.delegatedFrames).toHaveLength(1);
    expect(gate.snapshot()).toMatchObject({
      pendingUnauthenticatedSockets: 1,
      queuedBytes: 0,
      queuedMessages: 0,
    });
    gate.markAuthenticated(socket.connection.socketId);
    gate.markAuthenticated(socket.connection.socketId);
    expect(gate.snapshot().pendingUnauthenticatedSockets).toBe(0);
  });

  it("caps pending unauthenticated sockets before creating Hocuspocus state", () => {
    const harness = createHarness();
    const rejected: RawIngressDenialReason[] = [];
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, {
      ...limits,
      maxPendingUnauthenticatedSockets: 1,
      onReject: (reason) => rejected.push(reason),
    });
    const first = harness.connect();
    const second = harness.connect();

    expect(harness.createdConnections).toBe(1);
    expect(second.websocket.closeEvents).toEqual([
      { code: 1008, reason: "raw_pending_auth_quota_exceeded" },
    ]);
    expect(rejected).toEqual(["raw_pending_auth_quota_exceeded"]);

    first.connection.handleClose();
    const third = harness.connect();
    expect(harness.createdConnections).toBe(2);
    expect(third.websocket.closeEvents).toHaveLength(0);
    expect(gate.snapshot().pendingUnauthenticatedSockets).toBe(1);
  });

  it("closes a raw message-rate burst at the configured bound", () => {
    let now = 50;
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, {
      ...limits,
      maxMessagesPerWindow: 3,
      maxQueuedMessagesPerSocket: 8,
      now: () => now,
    });
    const socket = harness.connect();
    const frames = [syncFrame(20), syncFrame(20), syncFrame(20)];
    for (const frame of frames) {
      socket.connection.handleMessage(frame);
      gate.completeProcessing(socket.connection.socketId, frame);
    }

    socket.connection.handleMessage(syncFrame(20));
    expect(socket.delegatedFrames).toHaveLength(3);
    expect(socket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_message_rate_exceeded",
    );

    now = 1_050;
    expect(gate.snapshot().activeSockets).toBe(0);
  });

  it("closes a byte-rate burst even when every individual frame is valid", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, {
      ...limits,
      maxBytesPerWindow: 500,
      maxFrameBytes: 400,
      maxQueuedMessagesPerSocket: 8,
    });
    const socket = harness.connect();
    const first = syncFrame(300);
    socket.connection.handleMessage(first);
    gate.completeProcessing(socket.connection.socketId, first);
    socket.connection.handleMessage(syncFrame(300));

    expect(socket.delegatedFrames).toHaveLength(1);
    expect(socket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_byte_rate_exceeded",
    );
  });

  it("measures awareness from the original update bytes", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, {
      ...limits,
      awareness: { maxBytes: 24, maxDepth: 4, maxStates: 1 },
    });
    const socket = harness.connect();
    const frame = awarenessFrame([
      { clientId: 7, clock: 1, state: "x".repeat(32) },
    ]);

    socket.connection.handleMessage(frame);

    expect(socket.delegatedFrames).toHaveLength(0);
    expect(socket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_awareness_bytes_exceeded",
    );
    expect(gate.snapshot().queuedMessages).toBe(0);
  });

  it("blocks multi-state and duplicate-client awareness bypasses", () => {
    const multi = createHarness();
    installRawWebSocketIngressGate(multi.hocuspocus, {
      ...limits,
      awareness: { maxBytes: 256, maxDepth: 4, maxStates: 1 },
    });
    const multiSocket = multi.connect();
    multiSocket.connection.handleMessage(
      awarenessFrame([
        { clientId: 7, clock: 1, state: { cursor: null } },
        { clientId: 8, clock: 1, state: { cursor: null } },
      ]),
    );
    expect(multiSocket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_awareness_state_count_exceeded",
    );

    const duplicate = createHarness();
    installRawWebSocketIngressGate(duplicate.hocuspocus, {
      ...limits,
      awareness: { maxBytes: 256, maxDepth: 4, maxStates: 3 },
    });
    const duplicateSocket = duplicate.connect();
    duplicateSocket.connection.handleMessage(
      awarenessFrame([
        { clientId: 7, clock: 1, state: { cursor: null } },
        { clientId: 7, clock: 2, state: null },
      ]),
    );
    expect(duplicateSocket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_awareness_duplicate_client",
    );
  });

  it("accepts a single removal tombstone without rewriting the frame", () => {
    const harness = createHarness();
    const gate = installRawWebSocketIngressGate(harness.hocuspocus, {
      ...limits,
      awareness: { maxBytes: 128, maxDepth: 4, maxStates: 1 },
    });
    const socket = harness.connect();
    const tombstone = awarenessFrame([{ clientId: 7, clock: 9, state: null }]);

    socket.connection.handleMessage(tombstone);

    expect(socket.delegatedFrames).toEqual([tombstone]);
    expect(socket.delegatedFrames[0]).toBe(tombstone);
    expect(socket.websocket.closeEvents).toHaveLength(0);
    gate.completeProcessing(socket.connection.socketId, tombstone);
    expect(gate.snapshot().queuedMessages).toBe(0);
  });

  it("rejects deeply nested or malformed awareness before enqueue", () => {
    const deep = createHarness();
    installRawWebSocketIngressGate(deep.hocuspocus, {
      ...limits,
      awareness: { maxBytes: 256, maxDepth: 2, maxStates: 1 },
    });
    const deepSocket = deep.connect();
    deepSocket.connection.handleMessage(
      awarenessFrame([
        { clientId: 7, clock: 1, state: { one: { two: { three: true } } } },
      ]),
    );
    expect(deepSocket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_awareness_depth_exceeded",
    );

    const malformed = createHarness();
    installRawWebSocketIngressGate(malformed.hocuspocus, {
      ...limits,
      awareness: { maxBytes: 256, maxDepth: 4, maxStates: 1 },
    });
    const malformedSocket = malformed.connect();
    const frame = awarenessFrame([{ clientId: 7, clock: 1, state: {} }]);
    malformedSocket.connection.handleMessage(Uint8Array.from([...frame, 0]));
    expect(malformedSocket.websocket.closeEvents.at(-1)?.reason).toBe(
      "raw_awareness_malformed",
    );
  });
});

interface FakeConnection {
  handleClose(event?: { code: number; reason: string }): void;
  handleMessage(frame: Uint8Array): void;
  socketId: string;
}

class FakeWebSocket implements WebSocketLike {
  closeEvents: Array<{ code?: number; reason?: string }> = [];
  readyState = 1;

  close(code?: number, reason?: string): void {
    this.closeEvents.push({ code, reason });
    this.readyState = 3;
  }

  send(): void {}
}

function createHarness() {
  let createdConnections = 0;
  let maxDelegatedWithoutCompletion = 0;
  let upstreamCloseCount = 0;
  const delegatedCounts = new Map<string, number>();
  const fake = {
    handleConnection() {
      createdConnections += 1;
      const socketId = `socket-${createdConnections}`;
      const delegatedFrames: Uint8Array[] = [];
      const connection: FakeConnection = {
        handleClose() {
          upstreamCloseCount += 1;
        },
        handleMessage(frame) {
          delegatedFrames.push(frame);
          const count = (delegatedCounts.get(socketId) ?? 0) + 1;
          delegatedCounts.set(socketId, count);
          maxDelegatedWithoutCompletion = Math.max(
            maxDelegatedWithoutCompletion,
            count,
          );
        },
        socketId,
      };
      return { connection, delegatedFrames };
    },
  };
  const pending = new Map<
    FakeWebSocket,
    { connection: FakeConnection; delegatedFrames: Uint8Array[] }
  >();
  const hocuspocus = {
    handleConnection(websocket: FakeWebSocket) {
      const created = fake.handleConnection();
      pending.set(websocket, created);
      return created.connection;
    },
  } as unknown as Hocuspocus;

  return {
    connect() {
      const websocket = new FakeWebSocket();
      const connection = hocuspocus.handleConnection(
        websocket,
        new Request("http://runtime.invalid"),
      ) as unknown as FakeConnection;
      const created = pending.get(websocket);
      return {
        connection,
        delegatedFrames: created?.delegatedFrames ?? [],
        websocket,
      };
    },
    get createdConnections() {
      return createdConnections;
    },
    hocuspocus,
    get maxDelegatedWithoutCompletion() {
      return maxDelegatedWithoutCompletion;
    },
    get upstreamCloseCount() {
      return upstreamCloseCount;
    },
  };
}

function authFrame(): Uint8Array {
  return Uint8Array.from([1, 100, 2, 0]);
}

function syncFrame(length: number): Uint8Array {
  const frame = new Uint8Array(length);
  frame.set([1, 100, 0]);
  return frame;
}

function awarenessFrame(
  states: readonly {
    clientId: number;
    clock: number;
    state: unknown;
  }[],
): Uint8Array {
  const update = [
    ...varUint(states.length),
    ...states.flatMap(({ clientId, clock, state }) => {
      const json = new TextEncoder().encode(JSON.stringify(state));
      return [
        ...varUint(clientId),
        ...varUint(clock),
        ...varUint(json.byteLength),
        ...json,
      ];
    }),
  ];
  return Uint8Array.from([1, 100, 1, ...varUint(update.length), ...update]);
}

function varUint(value: number): number[] {
  const encoded: number[] = [];
  let remaining = value;
  do {
    const next = remaining & 0x7f;
    remaining = Math.floor(remaining / 128);
    encoded.push(remaining > 0 ? next | 0x80 : next);
  } while (remaining > 0);
  return encoded;
}
