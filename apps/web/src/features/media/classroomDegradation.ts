export const CLASSROOM_DEGRADATION_STAGES = [
  "normal",
  "reduced-visible-video",
  "lower-remote-quality",
  "stage-only-video",
  "audio-only",
] as const;

export type ClassroomDegradationStage =
  (typeof CLASSROOM_DEGRADATION_STAGES)[number];

export type ClassroomDegradationSignal = "stable" | "unstable" | "hold";

export type ClassroomRemoteVideoQuality = "high" | "low" | "off";

export type ClassroomVisibleVideoStrategy =
  "bounded-page" | "reduced-page" | "stage-only" | "audio-only";

export type ClassroomDegradationClock = () => number;

export const CLASSROOM_DEGRADATION_TIMING = {
  escalateAfterMs: 5_000,
  recoverAfterMs: 15_000,
} as const;

export const MAX_CLASSROOM_VIDEO_SUBSCRIPTIONS = 12;

export interface ClassroomDegradationTiming {
  readonly escalateAfterMs: number;
  readonly recoverAfterMs: number;
}

export interface ClassroomDegradationControllerOptions {
  readonly clock?: ClassroomDegradationClock;
  readonly initialStage?: ClassroomDegradationStage;
  readonly timing?: Readonly<Partial<ClassroomDegradationTiming>>;
}

export interface ClassroomDegradationSnapshot {
  readonly stage: ClassroomDegradationStage;
  readonly stageChangedAtMs: number | null;
  readonly pendingSignal: Exclude<ClassroomDegradationSignal, "hold"> | null;
  readonly pendingSinceMs: number | null;
}

export interface ClassroomDegradationProjectionInput {
  /**
   * Total visible/subscribed video slots allowed by the current responsive
   * layout. A presentation consumes one slot before any camera does; a local
   * camera can consume a visible slot without creating a remote subscription.
   */
  readonly normalVideoSubscriptionLimit: number;
  readonly hasPresentation: boolean;
}

export interface ClassroomDegradationProjection {
  readonly stage: ClassroomDegradationStage;
  readonly visibleVideoStrategy: ClassroomVisibleVideoStrategy;
  readonly maxVideoSubscriptions: number;
  readonly maxCameraVideoItems: number;
  readonly subscribePresentationVideo: boolean;
  readonly remoteCameraQuality: ClassroomRemoteVideoQuality;
  /** Audio and room controls deliberately outlive every video tier. */
  readonly subscribeRemoteAudio: true;
  readonly keepRoomControls: true;
}

const STAGE_INDEX: Readonly<Record<ClassroomDegradationStage, number>> = {
  normal: 0,
  "reduced-visible-video": 1,
  "lower-remote-quality": 2,
  "stage-only-video": 3,
  "audio-only": 4,
};

/**
 * Converts a degradation tier into a bounded subscription policy. The order
 * is intentionally encoded in the stage list rather than inferred from SDK
 * quality callbacks, keeping the policy deterministic and provider-neutral.
 */
export function projectClassroomDegradation(
  stage: ClassroomDegradationStage,
  input: ClassroomDegradationProjectionInput,
): ClassroomDegradationProjection {
  assertStage(stage);
  const normalLimit = boundedSubscriptionLimit(
    input.normalVideoSubscriptionLimit,
  );
  const reducedLimit =
    normalLimit === 0 ? 0 : Math.max(1, Math.ceil(normalLimit / 2));

  let visibleVideoStrategy: ClassroomVisibleVideoStrategy = "bounded-page";
  let maxVideoSubscriptions = normalLimit;
  let remoteCameraQuality: ClassroomRemoteVideoQuality = "high";

  switch (stage) {
    case "normal":
      break;
    case "reduced-visible-video":
      visibleVideoStrategy = "reduced-page";
      maxVideoSubscriptions = reducedLimit;
      break;
    case "lower-remote-quality":
      visibleVideoStrategy = "reduced-page";
      maxVideoSubscriptions = reducedLimit;
      remoteCameraQuality = "low";
      break;
    case "stage-only-video":
      visibleVideoStrategy = "stage-only";
      maxVideoSubscriptions = normalLimit === 0 ? 0 : 1;
      remoteCameraQuality = maxVideoSubscriptions === 0 ? "off" : "low";
      break;
    case "audio-only":
      visibleVideoStrategy = "audio-only";
      maxVideoSubscriptions = 0;
      remoteCameraQuality = "off";
      break;
  }

  const subscribePresentationVideo =
    input.hasPresentation && maxVideoSubscriptions > 0;
  const maxCameraVideoItems = Math.max(
    0,
    maxVideoSubscriptions - (subscribePresentationVideo ? 1 : 0),
  );
  const effectiveRemoteCameraQuality =
    maxCameraVideoItems === 0 ? "off" : remoteCameraQuality;

  return {
    stage,
    visibleVideoStrategy,
    maxVideoSubscriptions,
    maxCameraVideoItems,
    subscribePresentationVideo,
    remoteCameraQuality: effectiveRemoteCameraQuality,
    subscribeRemoteAudio: true,
    keepRoomControls: true,
  };
}

/**
 * Time-gated state machine for coarse, privacy-safe health signals.
 *
 * A sustained `unstable` signal advances exactly one tier per escalation
 * window. A sustained `stable` signal recovers exactly one tier per longer
 * recovery window. `hold` cancels a pending transition, so missing or
 * ambiguous telemetry never changes media policy. Recovery never jumps from
 * audio-only to normal and therefore cannot create a subscription burst.
 */
export class ClassroomDegradationController {
  readonly #clock: ClassroomDegradationClock;
  readonly #timing: ClassroomDegradationTiming;
  #stage: ClassroomDegradationStage;
  #stageChangedAtMs: number | null = null;
  #pendingSignal: Exclude<ClassroomDegradationSignal, "hold"> | null = null;
  #pendingSinceMs: number | null = null;
  #lastObservedAtMs: number | null = null;

  constructor(options: ClassroomDegradationControllerOptions = {}) {
    this.#clock = options.clock ?? defaultClock;
    this.#timing = normalizeTiming(options.timing);
    this.#stage = options.initialStage ?? "normal";
    assertStage(this.#stage);
  }

  get stage(): ClassroomDegradationStage {
    return this.#stage;
  }

  observe(
    signal: ClassroomDegradationSignal,
    nowMs = this.#clock(),
  ): ClassroomDegradationStage {
    assertSignal(signal);
    const now = this.#monotonicTime(nowMs);

    if (signal === "hold" || this.#isTerminalFor(signal)) {
      this.#clearPending();
      return this.#stage;
    }

    if (signal !== this.#pendingSignal) {
      this.#pendingSignal = signal;
      this.#pendingSinceMs = now;
      return this.#stage;
    }

    const pendingSince = this.#pendingSinceMs;
    if (pendingSince === null) {
      this.#pendingSinceMs = now;
      return this.#stage;
    }

    const threshold =
      signal === "unstable"
        ? this.#timing.escalateAfterMs
        : this.#timing.recoverAfterMs;
    if (now - pendingSince < threshold) {
      return this.#stage;
    }

    this.#stage = adjacentStage(this.#stage, signal);
    this.#stageChangedAtMs = now;
    this.#pendingSinceMs = now;
    return this.#stage;
  }

  reset(
    stage: ClassroomDegradationStage = "normal",
    nowMs = this.#clock(),
  ): void {
    assertStage(stage);
    const now = this.#monotonicTime(nowMs);
    this.#stage = stage;
    this.#stageChangedAtMs = now;
    this.#clearPending();
  }

  snapshot(): ClassroomDegradationSnapshot {
    return {
      stage: this.#stage,
      stageChangedAtMs: this.#stageChangedAtMs,
      pendingSignal: this.#pendingSignal,
      pendingSinceMs: this.#pendingSinceMs,
    };
  }

  #isTerminalFor(signal: ClassroomDegradationSignal): boolean {
    return (
      (signal === "stable" && this.#stage === "normal") ||
      (signal === "unstable" && this.#stage === "audio-only")
    );
  }

  #clearPending(): void {
    this.#pendingSignal = null;
    this.#pendingSinceMs = null;
  }

  #monotonicTime(value: number): number {
    if (!Number.isFinite(value)) {
      throw new TypeError("Classroom degradation time must be finite.");
    }
    const now =
      this.#lastObservedAtMs === null
        ? value
        : Math.max(value, this.#lastObservedAtMs);
    this.#lastObservedAtMs = now;
    return now;
  }
}

function adjacentStage(
  stage: ClassroomDegradationStage,
  signal: Exclude<ClassroomDegradationSignal, "hold">,
): ClassroomDegradationStage {
  const direction = signal === "unstable" ? 1 : -1;
  const nextIndex = Math.min(
    CLASSROOM_DEGRADATION_STAGES.length - 1,
    Math.max(0, STAGE_INDEX[stage] + direction),
  );
  return CLASSROOM_DEGRADATION_STAGES[nextIndex]!;
}

function normalizeTiming(
  timing: Readonly<Partial<ClassroomDegradationTiming>> | undefined,
): ClassroomDegradationTiming {
  const normalized = {
    escalateAfterMs:
      timing?.escalateAfterMs ?? CLASSROOM_DEGRADATION_TIMING.escalateAfterMs,
    recoverAfterMs:
      timing?.recoverAfterMs ?? CLASSROOM_DEGRADATION_TIMING.recoverAfterMs,
  };

  if (
    !Number.isFinite(normalized.escalateAfterMs) ||
    normalized.escalateAfterMs <= 0 ||
    !Number.isFinite(normalized.recoverAfterMs) ||
    normalized.recoverAfterMs <= normalized.escalateAfterMs
  ) {
    throw new RangeError(
      "Degradation timing requires a positive escalation window and a longer recovery window.",
    );
  }
  return normalized;
}

function boundedSubscriptionLimit(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.min(
    MAX_CLASSROOM_VIDEO_SUBSCRIPTIONS,
    Math.max(0, Math.trunc(value)),
  );
}

function assertStage(
  stage: ClassroomDegradationStage,
): asserts stage is ClassroomDegradationStage {
  if (!CLASSROOM_DEGRADATION_STAGES.includes(stage)) {
    throw new TypeError("Unknown classroom degradation stage.");
  }
}

function assertSignal(
  signal: ClassroomDegradationSignal,
): asserts signal is ClassroomDegradationSignal {
  if (signal !== "stable" && signal !== "unstable" && signal !== "hold") {
    throw new TypeError("Unknown classroom degradation signal.");
  }
}

function defaultClock(): number {
  return globalThis.performance?.now() ?? Date.now();
}
