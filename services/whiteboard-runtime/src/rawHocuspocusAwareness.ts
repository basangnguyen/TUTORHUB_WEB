const MESSAGE_TYPE_AWARENESS = 1;
const MAX_YJS_CLIENT_ID = 0xffff_ffff;
const DEFAULT_MAX_ADDRESS_BYTES = 1_024;
const UTF8 = new TextDecoder("utf-8", { fatal: true });

export type RawAwarenessInspectionErrorCode =
  | "raw_address_too_large"
  | "raw_awareness_depth_exceeded"
  | "raw_awareness_malformed"
  | "raw_awareness_state_count_invalid"
  | "raw_awareness_state_invalid"
  | "raw_awareness_too_large"
  | "raw_envelope_malformed"
  | "raw_frame_too_large";

export class RawAwarenessInspectionError extends Error {
  constructor(readonly code: RawAwarenessInspectionErrorCode) {
    super(code);
    this.name = "RawAwarenessInspectionError";
  }
}

export interface RawAwarenessInspectionLimits {
  maxAddressBytes?: number;
  maxAwarenessBytes: number;
  maxDepth: number;
  maxFrameBytes: number;
}

/**
 * Opaque, bounded state decoded from one Y-awareness update. The client id and
 * state stay captured in the closure so callers can restore clock-zero states
 * and removal tombstones without exposing either value to logs or telemetry.
 */
export interface RawAwarenessStatePayload {
  restoreInto(states: Map<number, unknown>): void;
}

export type RawHocuspocusAwarenessInspection =
  | {
      kind: "non_awareness";
      messageType: number;
    }
  | {
      awarenessBytes: number;
      kind: "awareness";
      maxDepth: number;
      messageType: typeof MESSAGE_TYPE_AWARENESS;
      /** Non-enumerable so generic serialization cannot expose awareness data. */
      statePayload: RawAwarenessStatePayload;
      stateCount: 1;
    };

/**
 * Inspects a raw Hocuspocus frame before Hocuspocus decodes awareness data.
 *
 * Only bounded metadata leaves this function. Normal awareness state and its
 * client id are deliberately not returned. A non-enumerable opaque helper can
 * restore a clock-zero state or null tombstone after Hocuspocus's scratch
 * Awareness has discarded it.
 */
export function inspectRawHocuspocusAwareness(
  frame: Uint8Array,
  limits: RawAwarenessInspectionLimits,
): RawHocuspocusAwarenessInspection {
  const maxAddressBytes = limits.maxAddressBytes ?? DEFAULT_MAX_ADDRESS_BYTES;
  validateLimits(limits, maxAddressBytes);
  if (!(frame instanceof Uint8Array)) {
    throw failure("raw_envelope_malformed");
  }
  if (frame.byteLength > limits.maxFrameBytes) {
    throw failure("raw_frame_too_large");
  }

  const envelope = new ByteCursor(frame, 0, frame.byteLength);
  let address: Uint8Array;
  let messageType: number;
  try {
    address = envelope.readLengthDelimited(
      maxAddressBytes,
      "raw_address_too_large",
    );
    // Validate the bounded document address but never return it.
    UTF8.decode(address);
    messageType = envelope.readVarUint();
  } catch (error) {
    throw preserveKnownFailure(error, "raw_envelope_malformed");
  }

  if (messageType !== MESSAGE_TYPE_AWARENESS) {
    return { kind: "non_awareness", messageType };
  }

  let update: Uint8Array;
  try {
    update = envelope.readLengthDelimited(
      limits.maxAwarenessBytes,
      "raw_awareness_too_large",
    );
    envelope.assertDone();
  } catch (error) {
    throw preserveKnownFailure(error, "raw_awareness_malformed");
  }

  const decoded = inspectAwarenessUpdate(update, limits.maxDepth);
  const inspection = {
    awarenessBytes: update.byteLength,
    kind: "awareness",
    maxDepth: decoded.maxDepth,
    messageType: MESSAGE_TYPE_AWARENESS,
    stateCount: 1,
  } as Extract<RawHocuspocusAwarenessInspection, { kind: "awareness" }>;
  Object.defineProperty(inspection, "statePayload", {
    configurable: false,
    enumerable: false,
    value: decoded.statePayload,
    writable: false,
  });
  return inspection;
}

function inspectAwarenessUpdate(
  update: Uint8Array,
  maxDepth: number,
): { maxDepth: number; statePayload: RawAwarenessStatePayload } {
  const cursor = new ByteCursor(update, 0, update.byteLength);
  let clientId: number;
  let stateBytes: Uint8Array;
  try {
    const stateCount = cursor.readVarUint();
    if (stateCount !== 1) {
      throw failure("raw_awareness_state_count_invalid");
    }
    clientId = cursor.readVarUint();
    if (clientId > MAX_YJS_CLIENT_ID) {
      throw failure("raw_awareness_state_invalid");
    }
    cursor.readVarUint(); // Awareness clock.
    stateBytes = cursor.readLengthDelimited(
      cursor.remaining,
      "raw_awareness_malformed",
    );
    cursor.assertDone();
  } catch (error) {
    throw preserveKnownFailure(error, "raw_awareness_malformed");
  }

  let state: unknown;
  try {
    state = JSON.parse(UTF8.decode(stateBytes));
  } catch {
    throw failure("raw_awareness_state_invalid");
  }

  if (state !== null && !isPlainObject(state)) {
    throw failure("raw_awareness_state_invalid");
  }
  return {
    maxDepth:
      state === null
        ? 0
        : validateJsonDepth(state, maxDepth, update.byteLength),
    statePayload: {
      restoreInto(states) {
        states.set(clientId, state);
      },
    },
  };
}

function validateJsonDepth(
  root: Record<string, unknown>,
  maxDepth: number,
  byteLimit: number,
): number {
  const pending: Array<{ depth: number; value: unknown }> = [
    { depth: 0, value: root },
  ];
  let maxObservedDepth = 0;
  let observedNodes = 0;
  while (pending.length > 0) {
    const current = pending.pop();
    if (!current) break;
    observedNodes += 1;
    if (observedNodes > Math.max(1, byteLimit)) {
      throw failure("raw_awareness_state_invalid");
    }
    if (current.depth > maxDepth) {
      throw failure("raw_awareness_depth_exceeded");
    }
    maxObservedDepth = Math.max(maxObservedDepth, current.depth);
    if (isJsonPrimitive(current.value)) continue;
    if (Array.isArray(current.value)) {
      for (const value of current.value) {
        pending.push({ depth: current.depth + 1, value });
      }
      continue;
    }
    if (!isPlainObject(current.value)) {
      throw failure("raw_awareness_state_invalid");
    }
    for (const value of Object.values(current.value)) {
      pending.push({ depth: current.depth + 1, value });
    }
  }
  return maxObservedDepth;
}

function isJsonPrimitive(
  value: unknown,
): value is boolean | null | number | string {
  return (
    value === null ||
    typeof value === "boolean" ||
    (typeof value === "number" && Number.isFinite(value)) ||
    typeof value === "string"
  );
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function validateLimits(
  limits: RawAwarenessInspectionLimits,
  maxAddressBytes: number,
): void {
  if (
    ![
      limits.maxAwarenessBytes,
      limits.maxDepth,
      limits.maxFrameBytes,
      maxAddressBytes,
    ].every(positiveSafeInteger) ||
    limits.maxAwarenessBytes > limits.maxFrameBytes ||
    maxAddressBytes > limits.maxFrameBytes
  ) {
    throw new Error("raw_awareness_inspector_configuration_invalid");
  }
}

function positiveSafeInteger(value: number): boolean {
  return Number.isSafeInteger(value) && value > 0;
}

function failure(
  code: RawAwarenessInspectionErrorCode,
): RawAwarenessInspectionError {
  return new RawAwarenessInspectionError(code);
}

function preserveKnownFailure(
  error: unknown,
  fallback: RawAwarenessInspectionErrorCode,
): RawAwarenessInspectionError {
  return error instanceof RawAwarenessInspectionError
    ? error
    : failure(fallback);
}

class ByteCursor {
  private offset: number;

  constructor(
    private readonly bytes: Uint8Array,
    start: number,
    private readonly end: number,
  ) {
    this.offset = start;
  }

  get remaining(): number {
    return this.end - this.offset;
  }

  assertDone(): void {
    if (this.offset !== this.end) throw new Error("trailing_bytes");
  }

  readLengthDelimited(
    maxBytes: number,
    oversizeCode: RawAwarenessInspectionErrorCode,
  ): Uint8Array {
    const length = this.readVarUint();
    if (length > maxBytes) throw failure(oversizeCode);
    const next = this.offset + length;
    if (next < this.offset || next > this.end) {
      throw new Error("truncated_bytes");
    }
    const value = this.bytes.subarray(this.offset, next);
    this.offset = next;
    return value;
  }

  readVarUint(): number {
    let value = 0;
    let multiplier = 1;
    let byteCount = 0;
    while (this.offset < this.end && byteCount < 8) {
      const byte = this.bytes[this.offset];
      if (byte === undefined) break;
      this.offset += 1;
      byteCount += 1;
      const chunk = byte & 0x7f;
      const contribution = chunk * multiplier;
      if (!Number.isSafeInteger(contribution + value)) {
        throw new Error("unsafe_varuint");
      }
      value += contribution;
      if ((byte & 0x80) === 0) {
        if (byteCount > 1 && chunk === 0) {
          throw new Error("non_canonical_varuint");
        }
        return value;
      }
      multiplier *= 128;
      if (!Number.isSafeInteger(multiplier)) {
        throw new Error("unsafe_varuint");
      }
    }
    throw new Error("truncated_varuint");
  }
}
