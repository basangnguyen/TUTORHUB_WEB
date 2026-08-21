import type { Hocuspocus, WebSocketLike } from "@hocuspocus/server";
import {
  RuntimePolicyError,
  validateAwarenessEnvelope,
  type AwarenessPolicyLimits,
} from "./runtimePolicy.js";

export type RawIngressDenialReason =
  | "raw_awareness_bytes_exceeded"
  | "raw_awareness_depth_exceeded"
  | "raw_awareness_duplicate_client"
  | "raw_awareness_malformed"
  | "raw_awareness_state_count_exceeded"
  | "raw_byte_rate_exceeded"
  | "raw_clock_invalid"
  | "raw_frame_too_large"
  | "raw_message_rate_exceeded"
  | "raw_pending_auth_quota_exceeded"
  | "raw_queue_bytes_exceeded"
  | "raw_queue_messages_exceeded";

export interface RawWebSocketIngressLimits {
  maxBytesPerWindow: number;
  maxFrameBytes: number;
  maxMessagesPerWindow: number;
  maxPendingUnauthenticatedSockets: number;
  maxQueuedBytesPerSocket: number;
  maxQueuedMessagesPerSocket: number;
  windowMs: number;
}

export interface RawWebSocketIngressSnapshot {
  activeSockets: number;
  pendingUnauthenticatedSockets: number;
  queuedBytes: number;
  queuedMessages: number;
}

export interface RawWebSocketIngressGate {
  /**
   * Release one admitted frame after Hocuspocus has finished processing it.
   * The exact Uint8Array received by afterHandleMessage must be supplied.
   */
  completeProcessing(socketId: string, frame: Uint8Array): void;
  dispose(): void;
  /** Mark a socket only after the collaboration grant has authenticated. */
  markAuthenticated(socketId: string): void;
  snapshot(): RawWebSocketIngressSnapshot;
}

export interface RawWebSocketIngressOptions extends RawWebSocketIngressLimits {
  awareness?: AwarenessPolicyLimits;
  now?: () => number;
  onReject?: (reason: RawIngressDenialReason) => void;
}

interface IngressEvent {
  bytes: number;
  observedAt: number;
}

interface SocketState {
  authenticated: boolean;
  bytesInWindow: number;
  closed: boolean;
  connection: InterceptableConnection;
  events: IngressEvent[];
  lastObservedAt: number;
  pendingFrames: Map<Uint8Array, number>;
  queuedBytes: number;
  originalHandleClose: (event?: { code: number; reason: string }) => void;
  socketId: string;
  upstreamClosed: boolean;
  websocket: WebSocketLike;
}

interface InterceptableConnection {
  handleClose(event?: { code: number; reason: string }): void;
  handleMessage(data: Uint8Array): void;
  readonly socketId?: unknown;
}

const CLOSE_POLICY_VIOLATION = 1008;
const CLOSE_MESSAGE_TOO_BIG = 1009;
const MESSAGE_TYPE_AWARENESS = 1;
const MESSAGE_TYPE_AUTH = 2;
const UTF8 = new TextDecoder("utf-8", { fatal: true });

/**
 * Installs a process-local ingress shim around Hocuspocus.handleConnection.
 * crossws calls the returned handleMessage function directly, so the shim runs
 * before ClientConnection and Connection can append a frame to either internal
 * queue. No decoded document, actor, tenant, token, or frame content is logged.
 */
export function installRawWebSocketIngressGate<Context>(
  hocuspocus: Hocuspocus<Context>,
  options: RawWebSocketIngressOptions,
): RawWebSocketIngressGate {
  validateOptions(options);
  const now = options.now ?? Date.now;
  const originalHandleConnection = hocuspocus.handleConnection;
  const sockets = new Map<string, SocketState>();
  let pendingUnauthenticatedSockets = 0;
  let disposed = false;

  const reject = (
    websocket: WebSocketLike,
    reason: RawIngressDenialReason,
    state?: SocketState,
  ): void => {
    if (state) releaseState(state);
    try {
      websocket.close(
        reason === "raw_frame_too_large"
          ? CLOSE_MESSAGE_TOO_BIG
          : CLOSE_POLICY_VIOLATION,
        reason,
      );
    } catch {
      // The socket may already be closing. State has still been released.
    }
    try {
      options.onReject?.(reason);
    } catch {
      // Observability must never prevent fail-closed transport cleanup.
    }
  };

  const releaseState = (state: SocketState): void => {
    if (state.closed) return;
    state.closed = true;
    if (!state.authenticated) {
      pendingUnauthenticatedSockets = Math.max(
        0,
        pendingUnauthenticatedSockets - 1,
      );
    }
    state.pendingFrames.clear();
    state.queuedBytes = 0;
    sockets.delete(state.socketId);
  };

  const closeUpstream = (
    state: SocketState,
    event?: { code: number; reason: string },
  ): void => {
    if (state.upstreamClosed) return;
    state.upstreamClosed = true;
    state.originalHandleClose(event);
  };

  const terminate = (
    state: SocketState,
    reason: RawIngressDenialReason,
  ): void => {
    if (state.closed) return;
    const event = {
      code:
        reason === "raw_frame_too_large"
          ? CLOSE_MESSAGE_TOO_BIG
          : CLOSE_POLICY_VIOLATION,
      reason,
    };
    try {
      releaseState(state);
      closeUpstream(state, event);
    } catch {
      // Continue with transport closure even if upstream cleanup rejects.
    }
    reject(state.websocket, reason);
  };

  const admitFrame = (state: SocketState, frame: Uint8Array): boolean => {
    if (state.closed) return false;
    if (frame.byteLength > options.maxFrameBytes) {
      terminate(state, "raw_frame_too_large");
      return false;
    }

    const observedAt = readClock(now);
    if (observedAt === undefined || observedAt < state.lastObservedAt) {
      terminate(state, "raw_clock_invalid");
      return false;
    }
    state.lastObservedAt = observedAt;
    pruneEvents(state, observedAt - options.windowMs);
    if (state.events.length >= options.maxMessagesPerWindow) {
      terminate(state, "raw_message_rate_exceeded");
      return false;
    }
    if (state.bytesInWindow + frame.byteLength > options.maxBytesPerWindow) {
      terminate(state, "raw_byte_rate_exceeded");
      return false;
    }

    const awarenessDenial = inspectRawAwarenessFrame(frame, options.awareness);
    if (awarenessDenial) {
      terminate(state, awarenessDenial);
      return false;
    }

    // Authentication frames are consumed by ClientConnection and never enter
    // Connection.messageQueue, so they count toward rate but not queue depth.
    if (!isAuthenticationFrame(frame)) {
      if (state.pendingFrames.size >= options.maxQueuedMessagesPerSocket) {
        terminate(state, "raw_queue_messages_exceeded");
        return false;
      }
      if (
        state.queuedBytes + frame.byteLength >
        options.maxQueuedBytesPerSocket
      ) {
        terminate(state, "raw_queue_bytes_exceeded");
        return false;
      }
      state.pendingFrames.set(frame, frame.byteLength);
      state.queuedBytes += frame.byteLength;
    }

    state.events.push({ bytes: frame.byteLength, observedAt });
    state.bytesInWindow += frame.byteLength;
    return true;
  };

  hocuspocus.handleConnection = ((...args) => {
    const websocket = args[0] as WebSocketLike;
    if (
      disposed ||
      pendingUnauthenticatedSockets >= options.maxPendingUnauthenticatedSockets
    ) {
      reject(websocket, "raw_pending_auth_quota_exceeded");
      return rejectedConnection();
    }

    pendingUnauthenticatedSockets += 1;
    let connection: InterceptableConnection;
    try {
      connection = originalHandleConnection.apply(
        hocuspocus,
        args,
      ) as unknown as InterceptableConnection;
    } catch (error) {
      pendingUnauthenticatedSockets = Math.max(
        0,
        pendingUnauthenticatedSockets - 1,
      );
      throw error;
    }

    const socketId = readSocketId(connection);
    const originalHandleMessage = connection.handleMessage.bind(connection);
    const originalHandleClose = connection.handleClose.bind(connection);
    const state: SocketState = {
      authenticated: false,
      bytesInWindow: 0,
      closed: false,
      connection,
      events: [],
      lastObservedAt: readClock(now) ?? 0,
      originalHandleClose,
      pendingFrames: new Map(),
      queuedBytes: 0,
      socketId,
      upstreamClosed: false,
      websocket,
    };
    sockets.set(socketId, state);

    connection.handleMessage = (frame: Uint8Array): void => {
      if (!admitFrame(state, frame)) return;
      try {
        originalHandleMessage(frame);
      } catch {
        terminate(state, "raw_queue_messages_exceeded");
      }
    };
    connection.handleClose = (event): void => {
      releaseState(state);
      closeUpstream(state, event);
    };
    return connection;
  }) as Hocuspocus<Context>["handleConnection"];

  return {
    completeProcessing(socketId, frame) {
      const state = sockets.get(socketId);
      const byteLength = state?.pendingFrames.get(frame);
      if (!state || byteLength === undefined) return;
      state.pendingFrames.delete(frame);
      state.queuedBytes = Math.max(0, state.queuedBytes - byteLength);
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      hocuspocus.handleConnection = originalHandleConnection;
      for (const state of [...sockets.values()]) releaseState(state);
    },
    markAuthenticated(socketId) {
      const state = sockets.get(socketId);
      if (!state || state.closed || state.authenticated) return;
      state.authenticated = true;
      pendingUnauthenticatedSockets = Math.max(
        0,
        pendingUnauthenticatedSockets - 1,
      );
    },
    snapshot() {
      let queuedBytes = 0;
      let queuedMessages = 0;
      for (const state of sockets.values()) {
        queuedBytes += state.queuedBytes;
        queuedMessages += state.pendingFrames.size;
      }
      return {
        activeSockets: sockets.size,
        pendingUnauthenticatedSockets,
        queuedBytes,
        queuedMessages,
      };
    },
  };
}

function pruneEvents(state: SocketState, cutoff: number): void {
  while ((state.events[0]?.observedAt ?? Number.POSITIVE_INFINITY) <= cutoff) {
    const event = state.events.shift();
    if (event) {
      state.bytesInWindow = Math.max(0, state.bytesInWindow - event.bytes);
    }
  }
}

function readSocketId(connection: InterceptableConnection): string {
  if (
    typeof connection.socketId !== "string" ||
    connection.socketId.length === 0 ||
    connection.socketId.length > 256
  ) {
    throw new Error("raw_ingress_socket_id_unavailable");
  }
  return connection.socketId;
}

function rejectedConnection(): ReturnType<Hocuspocus["handleConnection"]> {
  return {
    handleClose() {},
    handleMessage() {},
  } as unknown as ReturnType<Hocuspocus["handleConnection"]>;
}

function readClock(now: () => number): number | undefined {
  const value = now();
  return Number.isSafeInteger(value) && value >= 0 ? value : undefined;
}

function validateOptions(options: RawWebSocketIngressOptions): void {
  const limits = [
    options.maxBytesPerWindow,
    options.maxFrameBytes,
    options.maxMessagesPerWindow,
    options.maxPendingUnauthenticatedSockets,
    options.maxQueuedBytesPerSocket,
    options.maxQueuedMessagesPerSocket,
    options.windowMs,
  ];
  if (!limits.every((value) => Number.isSafeInteger(value) && value > 0)) {
    throw new Error("raw_ingress_configuration_invalid");
  }
  if (
    options.maxFrameBytes > options.maxBytesPerWindow ||
    options.maxFrameBytes > options.maxQueuedBytesPerSocket
  ) {
    throw new Error("raw_ingress_configuration_invalid");
  }
  if (options.awareness) {
    const awarenessLimits = [
      options.awareness.maxBytes,
      options.awareness.maxDepth,
      options.awareness.maxStates,
    ];
    if (
      !awarenessLimits.every(
        (value) => Number.isSafeInteger(value) && value > 0,
      ) ||
      options.awareness.maxBytes > options.maxFrameBytes
    ) {
      throw new Error("raw_ingress_configuration_invalid");
    }
  }
}

/** Reads only the bounded Hocuspocus envelope prefix: address + message type. */
function isAuthenticationFrame(frame: Uint8Array): boolean {
  return readEnvelope(frame)?.type === MESSAGE_TYPE_AUTH;
}

/**
 * Validates an awareness update from its original bytes before Hocuspocus can
 * decode or enqueue it. Tombstones are accepted and the frame is never
 * rewritten; downstream must preserve the original client id/clock when it
 * sanitizes and re-encodes the update.
 */
function inspectRawAwarenessFrame(
  frame: Uint8Array,
  limits: AwarenessPolicyLimits | undefined,
): RawIngressDenialReason | undefined {
  if (!limits) return undefined;
  const envelope = readEnvelope(frame);
  if (!envelope || envelope.type !== MESSAGE_TYPE_AWARENESS) return undefined;

  const payloadLength = readVarUint(frame, envelope.payloadOffset);
  if (!payloadLength) return "raw_awareness_malformed";
  const payloadStart = payloadLength.next;
  const payloadEnd = payloadStart + payloadLength.value;
  if (
    payloadEnd !== frame.byteLength ||
    payloadEnd < payloadStart ||
    payloadLength.value > limits.maxBytes
  ) {
    return payloadLength.value > limits.maxBytes
      ? "raw_awareness_bytes_exceeded"
      : "raw_awareness_malformed";
  }

  const stateCount = readVarUint(frame, payloadStart);
  if (!stateCount || stateCount.value > limits.maxStates) {
    return stateCount?.value !== undefined &&
      stateCount.value > limits.maxStates
      ? "raw_awareness_state_count_exceeded"
      : "raw_awareness_malformed";
  }

  const clientIds = new Set<number>();
  const states: unknown[] = [];
  let offset = stateCount.next;
  try {
    for (let index = 0; index < stateCount.value; index += 1) {
      const clientId = readVarUint(frame, offset);
      if (!clientId) return "raw_awareness_malformed";
      offset = clientId.next;
      if (clientIds.has(clientId.value)) {
        return "raw_awareness_duplicate_client";
      }
      clientIds.add(clientId.value);

      const clock = readVarUint(frame, offset);
      if (!clock) return "raw_awareness_malformed";
      offset = clock.next;

      const stateLength = readVarUint(frame, offset);
      if (!stateLength) return "raw_awareness_malformed";
      offset = stateLength.next;
      const stateEnd = offset + stateLength.value;
      if (stateEnd > payloadEnd || stateEnd < offset) {
        return "raw_awareness_malformed";
      }
      const state = JSON.parse(UTF8.decode(frame.subarray(offset, stateEnd)));
      // A null state is an intentional Y-awareness removal tombstone. It
      // counts toward the raw state cardinality but needs no shape validation.
      if (state !== null) states.push(state);
      offset = stateEnd;
    }
  } catch {
    return "raw_awareness_malformed";
  }
  if (offset !== payloadEnd) return "raw_awareness_malformed";

  try {
    validateAwarenessEnvelope(
      { byteLength: payloadLength.value, states },
      limits,
    );
  } catch (error) {
    if (error instanceof RuntimePolicyError) {
      if (error.code === "awareness_depth_exceeded") {
        return "raw_awareness_depth_exceeded";
      }
      if (error.code === "awareness_bytes_exceeded") {
        return "raw_awareness_bytes_exceeded";
      }
      if (error.code === "awareness_state_count_exceeded") {
        return "raw_awareness_state_count_exceeded";
      }
    }
    return "raw_awareness_malformed";
  }
  return undefined;
}

function readEnvelope(
  frame: Uint8Array,
): { payloadOffset: number; type: number } | undefined {
  const addressLength = readVarUint(frame, 0);
  if (!addressLength) return undefined;
  const typeOffset = addressLength.next + addressLength.value;
  if (typeOffset > frame.byteLength) return undefined;
  const type = readVarUint(frame, typeOffset);
  if (!type) return undefined;
  return { payloadOffset: type.next, type: type.value };
}

function readVarUint(
  bytes: Uint8Array,
  start: number,
): { next: number; value: number } | undefined {
  let value = 0;
  let shift = 0;
  for (let index = start; index < bytes.byteLength && shift <= 49; index += 1) {
    const byte = bytes[index];
    if (byte === undefined) return undefined;
    value += (byte & 0x7f) * 2 ** shift;
    if (!Number.isSafeInteger(value)) return undefined;
    if ((byte & 0x80) === 0) return { next: index + 1, value };
    shift += 7;
  }
  return undefined;
}
