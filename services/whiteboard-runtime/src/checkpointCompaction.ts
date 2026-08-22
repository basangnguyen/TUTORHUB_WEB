import { createHash } from "node:crypto";
import * as Y from "yjs";

export type CheckpointCompactionErrorCode =
  | "checkpoint_compaction_diverged"
  | "checkpoint_corrupt"
  | "checkpoint_too_large";

export class CheckpointCompactionError extends Error {
  constructor(readonly code: CheckpointCompactionErrorCode) {
    super(code);
    this.name = "CheckpointCompactionError";
  }
}

export interface CompactedCheckpointState {
  causalWatermarkSha256: string;
  state: Uint8Array;
  stateVector: Uint8Array;
}

/**
 * Rebuild a durable Yjs checkpoint in an isolated document and prove that the
 * compacted update has the same causal watermark before it can replace the
 * previous durable value. The live Y.Doc is never mutated, so actor-local undo
 * managers keep their in-memory history.
 */
export function compactCheckpointState(
  candidate: Uint8Array,
  maxBytes: number,
): CompactedCheckpointState {
  if (candidate.byteLength < 1 || candidate.byteLength > maxBytes) {
    throw new CheckpointCompactionError(
      candidate.byteLength > maxBytes
        ? "checkpoint_too_large"
        : "checkpoint_corrupt",
    );
  }

  const source = new Y.Doc();
  const probe = new Y.Doc();
  try {
    Y.applyUpdate(source, candidate);
    const stateVector = Y.encodeStateVector(source);
    const state = Y.encodeStateAsUpdate(source);
    if (state.byteLength < 1 || state.byteLength > maxBytes) {
      throw new CheckpointCompactionError("checkpoint_too_large");
    }

    Y.applyUpdate(probe, state);
    if (
      !bytesEqual(stateVector, Y.encodeStateVector(probe)) ||
      !bytesEqual(state, Y.encodeStateAsUpdate(probe))
    ) {
      throw new CheckpointCompactionError("checkpoint_compaction_diverged");
    }

    return {
      causalWatermarkSha256: createHash("sha256")
        .update(stateVector)
        .digest("hex"),
      state,
      stateVector,
    };
  } catch (error) {
    if (error instanceof CheckpointCompactionError) throw error;
    throw new CheckpointCompactionError("checkpoint_corrupt");
  } finally {
    source.destroy();
    probe.destroy();
  }
}

function bytesEqual(left: Uint8Array, right: Uint8Array): boolean {
  if (left.byteLength !== right.byteLength) return false;
  return left.every((value, index) => value === right[index]);
}
