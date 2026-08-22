import { describe, expect, it } from "vitest";
import * as Y from "yjs";
import { CanonicalExcalidrawAuthority } from "@tutorhub/collaboration-client";
import {
  CheckpointCompactionError,
  compactCheckpointState,
} from "./checkpointCompaction.js";

const MAX_BYTES = 1024 * 1024;

describe("durable checkpoint compaction", () => {
  it("preserves the exact causal watermark and converges duplicate out-of-order offline updates", () => {
    const baseline = new Y.Doc();
    baseline.getMap("scene").set("seed", true);
    const baselineState = Y.encodeStateAsUpdate(baseline);
    const baselineVector = Y.encodeStateVector(baseline);

    const teacher = new Y.Doc();
    const student = new Y.Doc();
    Y.applyUpdate(teacher, baselineState);
    Y.applyUpdate(student, baselineState);
    teacher.getMap("scene").set("teacher", "triangle");
    student.getMap("scene").set("student", "circle");

    const teacherUpdate = Y.encodeStateAsUpdate(teacher, baselineVector);
    const studentUpdate = Y.encodeStateAsUpdate(student, baselineVector);
    const authority = new Y.Doc();
    Y.applyUpdate(authority, baselineState);
    Y.applyUpdate(authority, studentUpdate);
    Y.applyUpdate(authority, teacherUpdate);
    Y.applyUpdate(authority, studentUpdate);

    const compacted = compactCheckpointState(
      Y.encodeStateAsUpdate(authority),
      MAX_BYTES,
    );
    expect(compacted.causalWatermarkSha256).toMatch(/^[a-f0-9]{64}$/);
    expect([...compacted.stateVector]).toEqual([
      ...Y.encodeStateVector(authority),
    ]);

    for (const peer of [teacher, student]) {
      Y.applyUpdate(peer, compacted.state);
      expect(peer.getMap("scene").toJSON()).toEqual({
        seed: true,
        student: "circle",
        teacher: "triangle",
      });
      expect([...Y.encodeStateVector(peer)]).toEqual([
        ...compacted.stateVector,
      ]);
    }

    baseline.destroy();
    teacher.destroy();
    student.destroy();
    authority.destroy();
  });

  it("fails closed for corrupt and oversized state", () => {
    expect(() => compactCheckpointState(new Uint8Array([1, 2, 3]), 64)).toThrow(
      CheckpointCompactionError,
    );
    expect(() => compactCheckpointState(new Uint8Array(65), 64)).toThrow(
      "checkpoint_too_large",
    );
  });

  it("preserves tombstones when a delayed stale update arrives after compaction", () => {
    const source = new Y.Doc();
    source.getMap("scene").set("deleted-element", "stale");
    const staleUpdate = Y.encodeStateAsUpdate(source);
    source.getMap("scene").delete("deleted-element");

    const compacted = compactCheckpointState(
      Y.encodeStateAsUpdate(source),
      MAX_BYTES,
    );
    const recovered = new Y.Doc();
    Y.applyUpdate(recovered, compacted.state);
    Y.applyUpdate(recovered, staleUpdate);

    expect(recovered.getMap("scene").has("deleted-element")).toBe(false);
    expect([...Y.encodeStateVector(recovered)]).toEqual([
      ...compacted.stateVector,
    ]);

    recovered.destroy();
    source.destroy();
  });

  it("does not mutate the live authority or consume actor-local undo", () => {
    const scope = {
      documentId: "21aac229-f1f6-4f21-85cf-3ecfe6f43529",
      generation: 1,
      tenantId: "2a388dc1-e3a5-4888-8d65-d9bd57fd4fc7",
    };
    const live = new Y.Doc();
    const authority = new CanonicalExcalidrawAuthority(live, scope, "teacher");
    authority.initialize({
      elements: [],
      files: {},
      page: { backgroundColor: "#fff", id: "page-1", name: "Board" },
      schemaVersion: 1,
    });
    authority.putElement({
      height: 40,
      id: "teacher-note",
      text: "Keep undo local",
      type: "text",
      width: 160,
      x: 10,
      y: 20,
    });
    const before = authority.getSemanticHash();
    const compacted = compactCheckpointState(
      authority.encodeProviderState(),
      MAX_BYTES,
    );
    expect(authority.getSemanticHash()).toBe(before);

    const recovered = new Y.Doc();
    Y.applyUpdate(recovered, compacted.state);
    const recoveryAuthority = new CanonicalExcalidrawAuthority(
      recovered,
      scope,
      "recovery-probe",
    );
    expect(recoveryAuthority.getSemanticHash()).toBe(before);
    expect(authority.undo()).toBe(true);
    expect(authority.getScene().elements).toHaveLength(0);

    recoveryAuthority.destroy();
    recovered.destroy();
    authority.destroy();
    live.destroy();
  });
});
