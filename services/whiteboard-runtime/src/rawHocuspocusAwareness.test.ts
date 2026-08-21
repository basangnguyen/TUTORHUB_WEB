import { describe, expect, it } from "vitest";
import {
  inspectRawHocuspocusAwareness,
  RawAwarenessInspectionError,
  type RawAwarenessInspectionLimits,
} from "./rawHocuspocusAwareness.js";

const limits: RawAwarenessInspectionLimits = {
  maxAddressBytes: 64,
  maxAwarenessBytes: 256,
  maxDepth: 3,
  maxFrameBytes: 512,
};

describe("raw Hocuspocus awareness inspector", () => {
  it("reads a bounded non-awareness envelope without decoding its body", () => {
    const inspection = inspectRawHocuspocusAwareness(
      concat(varString("room-safe"), varUint(0), Uint8Array.of(0xff, 0xff)),
      limits,
    );

    expect(inspection).toEqual({ kind: "non_awareness", messageType: 0 });
  });

  it("returns metadata only for a valid normal awareness state", () => {
    const inspection = inspectRawHocuspocusAwareness(
      awarenessFrame({ cursor: { x: 1, y: 2 } }),
      limits,
    );

    expect(inspection).toEqual({
      awarenessBytes: expect.any(Number),
      kind: "awareness",
      maxDepth: 2,
      messageType: 1,
      stateCount: 1,
    });
    expect(JSON.stringify(inspection)).not.toContain("cursor");
    expect(JSON.stringify(inspection)).not.toContain("424242");
    expect(Object.keys(inspection)).not.toContain("statePayload");
    if (inspection.kind !== "awareness") throw new Error("wrong_kind");
    const restored = new Map<number, unknown>();
    inspection.statePayload.restoreInto(restored);
    expect(restored.get(424_242)).toEqual({ cursor: { x: 1, y: 2 } });
  });

  it("preserves a removal as an opaque tombstone", () => {
    const frame = awarenessFrame(null);
    const inspection = inspectRawHocuspocusAwareness(frame, limits);
    expect(inspection.kind).toBe("awareness");
    if (inspection.kind !== "awareness") throw new Error("wrong_kind");

    const states = new Map<number, unknown>([[7, { user: "scratch" }]]);
    inspection.statePayload.restoreInto(states);
    expect(states.get(424_242)).toBeNull();
    expect(Object.keys(inspection.statePayload)).not.toContain("clientId");
    expect(JSON.stringify(inspection)).not.toContain("424242");
  });

  it("rejects raw frames and addresses over their byte limits", () => {
    expectCode(new Uint8Array(513), "raw_frame_too_large");
    expectCode(
      concat(varUint(65), new Uint8Array(65), varUint(0)),
      "raw_address_too_large",
    );
  });

  it("rejects a declared awareness body over the limit before allocation", () => {
    expectCode(
      concat(varString("room"), varUint(1), varUint(257)),
      "raw_awareness_too_large",
    );
  });

  it("fails closed on malformed envelopes and awareness bodies", () => {
    expectCode(Uint8Array.of(0x80), "raw_envelope_malformed");
    expectCode(
      concat(varString("room"), varUint(1), Uint8Array.of(0x80)),
      "raw_awareness_malformed",
    );
    const valid = awarenessFrame({ ok: true });
    expectCode(concat(valid, Uint8Array.of(0)), "raw_awareness_malformed");

    const inner = awarenessUpdate({ ok: true });
    expectCode(
      awarenessEnvelope(concat(inner, Uint8Array.of(0))),
      "raw_awareness_malformed",
    );
  });

  it("rejects zero-state and multi-state updates", () => {
    expectCode(
      awarenessEnvelope(varUint(0)),
      "raw_awareness_state_count_invalid",
    );
    expectCode(
      awarenessEnvelope(
        concat(
          varUint(2),
          awarenessEntry(1, { ok: true }),
          awarenessEntry(2, { ok: true }),
        ),
      ),
      "raw_awareness_state_count_invalid",
    );
  });

  it("rejects invalid UTF-8, JSON and non-object live states", () => {
    expectCode(
      awarenessEnvelope(
        concat(varUint(1), varUint(1), varUint(1), varUint(1), [0xff]),
      ),
      "raw_awareness_state_invalid",
    );
    expectCode(
      awarenessEnvelope(
        concat(varUint(1), varUint(1), varUint(1), varString("{")),
      ),
      "raw_awareness_state_invalid",
    );
    expectCode(
      awarenessFrame(["not", "an", "object"]),
      "raw_awareness_state_invalid",
    );
    expectCode(awarenessFrame("private-state"), "raw_awareness_state_invalid");
    expectCode(
      awarenessEnvelope(
        concat(varUint(1), varUint(1), varUint(1), varString("1e400")),
      ),
      "raw_awareness_state_invalid",
    );
  });

  it("accepts the depth boundary and rejects one nested level beyond it", () => {
    expect(
      inspectRawHocuspocusAwareness(
        awarenessFrame({ a: { b: { c: true } } }),
        limits,
      ),
    ).toMatchObject({ maxDepth: 3 });
    expectCode(
      awarenessFrame({ a: { b: { c: { d: true } } } }),
      "raw_awareness_depth_exceeded",
    );
  });

  it("rejects non-canonical varuints and client ids outside Yjs uint32", () => {
    expectCode(
      concat(Uint8Array.of(0x80, 0), varUint(1)),
      "raw_envelope_malformed",
    );
    expectCode(
      awarenessEnvelope(
        concat(varUint(1), varUint(0x1_0000_0000), varUint(1), varString("{}")),
      ),
      "raw_awareness_state_invalid",
    );
  });

  it("uses bounded error codes without private address, state or client id", () => {
    const frame = concat(
      varString("private-room-marker"),
      varUint(1),
      varBytes(
        concat(
          varUint(1),
          varUint(424_242),
          varUint(1),
          varString('{"private-state-marker":'),
        ),
      ),
    );

    try {
      inspectRawHocuspocusAwareness(frame, limits);
      throw new Error("expected_failure");
    } catch (error) {
      expect(error).toBeInstanceOf(RawAwarenessInspectionError);
      const serialized = String(error);
      expect(serialized).not.toContain("private-room-marker");
      expect(serialized).not.toContain("private-state-marker");
      expect(serialized).not.toContain("424242");
    }
  });

  it("rejects invalid inspector configuration", () => {
    expect(() =>
      inspectRawHocuspocusAwareness(awarenessFrame({ ok: true }), {
        ...limits,
        maxAwarenessBytes: 513,
      }),
    ).toThrow("raw_awareness_inspector_configuration_invalid");
  });
});

function expectCode(
  frame: Uint8Array,
  code: RawAwarenessInspectionError["code"],
): void {
  try {
    inspectRawHocuspocusAwareness(frame, limits);
    throw new Error("expected_failure");
  } catch (error) {
    expect(error).toBeInstanceOf(RawAwarenessInspectionError);
    expect((error as RawAwarenessInspectionError).code).toBe(code);
  }
}

function awarenessFrame(state: unknown): Uint8Array {
  return awarenessEnvelope(awarenessUpdate(state));
}

function awarenessEnvelope(update: Uint8Array): Uint8Array {
  return concat(varString("room-safe"), varUint(1), varBytes(update));
}

function awarenessUpdate(state: unknown): Uint8Array {
  return concat(varUint(1), awarenessEntry(424_242, state));
}

function awarenessEntry(clientId: number, state: unknown): Uint8Array {
  return concat(
    varUint(clientId),
    varUint(7),
    varString(JSON.stringify(state)),
  );
}

function varBytes(value: Uint8Array): Uint8Array {
  return concat(varUint(value.byteLength), value);
}

function varString(value: string): Uint8Array {
  return varBytes(new TextEncoder().encode(value));
}

function varUint(value: number): Uint8Array {
  const bytes: number[] = [];
  let remaining = value;
  do {
    const byte = remaining % 128;
    remaining = Math.floor(remaining / 128);
    bytes.push(remaining > 0 ? byte | 0x80 : byte);
  } while (remaining > 0);
  return Uint8Array.from(bytes);
}

function concat(...parts: ArrayLike<number>[]): Uint8Array {
  const result = new Uint8Array(
    parts.reduce((total, part) => total + part.length, 0),
  );
  let offset = 0;
  for (const part of parts) {
    result.set(part, offset);
    offset += part.length;
  }
  return result;
}
