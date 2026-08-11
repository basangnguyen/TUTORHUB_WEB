import { describe, expect, it } from "vitest";
import {
  CLASSROOM_DEGRADATION_STAGES,
  CLASSROOM_DEGRADATION_TIMING,
  ClassroomDegradationController,
  MAX_CLASSROOM_VIDEO_SUBSCRIPTIONS,
  projectClassroomDegradation,
  type ClassroomDegradationSignal,
  type ClassroomDegradationStage,
} from "./classroomDegradation";

describe("projectClassroomDegradation", () => {
  it("applies the locked degradation order without sacrificing audio/control", () => {
    const projections = CLASSROOM_DEGRADATION_STAGES.map((stage) =>
      projectClassroomDegradation(stage, {
        normalVideoSubscriptionLimit: 12,
        hasPresentation: false,
      }),
    );

    expect(
      projections.map(
        ({
          stage,
          visibleVideoStrategy,
          maxVideoSubscriptions,
          remoteCameraQuality,
        }) => ({
          stage,
          visibleVideoStrategy,
          maxVideoSubscriptions,
          remoteCameraQuality,
        }),
      ),
    ).toEqual([
      {
        stage: "normal",
        visibleVideoStrategy: "bounded-page",
        maxVideoSubscriptions: 12,
        remoteCameraQuality: "high",
      },
      {
        stage: "reduced-visible-video",
        visibleVideoStrategy: "reduced-page",
        maxVideoSubscriptions: 6,
        remoteCameraQuality: "high",
      },
      {
        stage: "lower-remote-quality",
        visibleVideoStrategy: "reduced-page",
        maxVideoSubscriptions: 6,
        remoteCameraQuality: "low",
      },
      {
        stage: "stage-only-video",
        visibleVideoStrategy: "stage-only",
        maxVideoSubscriptions: 1,
        remoteCameraQuality: "low",
      },
      {
        stage: "audio-only",
        visibleVideoStrategy: "audio-only",
        maxVideoSubscriptions: 0,
        remoteCameraQuality: "off",
      },
    ]);
    expect(
      projections.every(
        ({ subscribeRemoteAudio, keepRoomControls }) =>
          subscribeRemoteAudio && keepRoomControls,
      ),
    ).toBe(true);
  });

  it.each([
    [12, 6],
    [6, 3],
    [4, 2],
    [1, 1],
    [0, 0],
  ] as const)(
    "reduces a responsive video budget of %i to %i deterministically",
    (normalLimit, reducedLimit) => {
      expect(
        projectClassroomDegradation("reduced-visible-video", {
          normalVideoSubscriptionLimit: normalLimit,
          hasPresentation: false,
        }).maxVideoSubscriptions,
      ).toBe(reducedLimit);
    },
  );

  it("reserves the last video slot for presentation before remote cameras", () => {
    const lowerQuality = projectClassroomDegradation("lower-remote-quality", {
      normalVideoSubscriptionLimit: 12,
      hasPresentation: true,
    });
    const stageOnly = projectClassroomDegradation("stage-only-video", {
      normalVideoSubscriptionLimit: 12,
      hasPresentation: true,
    });
    const audioOnly = projectClassroomDegradation("audio-only", {
      normalVideoSubscriptionLimit: 12,
      hasPresentation: true,
    });

    expect(lowerQuality).toMatchObject({
      maxVideoSubscriptions: 6,
      maxCameraVideoItems: 5,
      subscribePresentationVideo: true,
    });
    expect(stageOnly).toMatchObject({
      maxVideoSubscriptions: 1,
      maxCameraVideoItems: 0,
      subscribePresentationVideo: true,
      remoteCameraQuality: "off",
    });
    expect(audioOnly).toMatchObject({
      maxVideoSubscriptions: 0,
      maxCameraVideoItems: 0,
      subscribePresentationVideo: false,
      subscribeRemoteAudio: true,
      keepRoomControls: true,
    });
  });

  it("normalizes invalid budgets and enforces the hard subscription bound", () => {
    expect(
      projectClassroomDegradation("normal", {
        normalVideoSubscriptionLimit: Number.NaN,
        hasPresentation: true,
      }),
    ).toMatchObject({
      maxVideoSubscriptions: 0,
      maxCameraVideoItems: 0,
      subscribePresentationVideo: false,
    });
    expect(
      projectClassroomDegradation("normal", {
        normalVideoSubscriptionLimit: 999,
        hasPresentation: false,
      }).maxVideoSubscriptions,
    ).toBe(MAX_CLASSROOM_VIDEO_SUBSCRIPTIONS);
    expect(
      projectClassroomDegradation("normal", {
        normalVideoSubscriptionLimit: -4,
        hasPresentation: false,
      }).maxVideoSubscriptions,
    ).toBe(0);
  });
});

describe("ClassroomDegradationController", () => {
  it("escalates one ordered stage per sustained unstable window", () => {
    let now = 1_000;
    const controller = new ClassroomDegradationController({
      clock: () => now,
    });

    expect(controller.observe("unstable")).toBe("normal");
    for (const expected of CLASSROOM_DEGRADATION_STAGES.slice(1)) {
      now += CLASSROOM_DEGRADATION_TIMING.escalateAfterMs - 1;
      expect(controller.observe("unstable")).not.toBe(expected);
      now += 1;
      expect(controller.observe("unstable")).toBe(expected);
    }
    now += CLASSROOM_DEGRADATION_TIMING.escalateAfterMs;
    expect(controller.observe("unstable")).toBe("audio-only");
  });

  it("recovers one ordered stage per longer stable window", () => {
    let now = 20_000;
    const controller = new ClassroomDegradationController({
      clock: () => now,
      initialStage: "audio-only",
    });
    const recoveryOrder: readonly ClassroomDegradationStage[] = [
      "stage-only-video",
      "lower-remote-quality",
      "reduced-visible-video",
      "normal",
    ];

    expect(controller.observe("stable")).toBe("audio-only");
    for (const expected of recoveryOrder) {
      now += CLASSROOM_DEGRADATION_TIMING.recoverAfterMs;
      expect(controller.observe("stable")).toBe(expected);
    }
    now += CLASSROOM_DEGRADATION_TIMING.recoverAfterMs;
    expect(controller.observe("stable")).toBe("normal");
  });

  it("uses hold and signal changes to reset pending hysteresis", () => {
    const controller = new ClassroomDegradationController({
      timing: { escalateAfterMs: 100, recoverAfterMs: 300 },
    });

    expect(controller.observe("unstable", 0)).toBe("normal");
    expect(controller.observe("unstable", 99)).toBe("normal");
    expect(controller.observe("hold", 100)).toBe("normal");
    expect(controller.snapshot()).toMatchObject({
      pendingSignal: null,
      pendingSinceMs: null,
    });

    expect(controller.observe("unstable", 200)).toBe("normal");
    expect(controller.observe("stable", 299)).toBe("normal");
    expect(controller.observe("unstable", 300)).toBe("normal");
    expect(controller.observe("unstable", 399)).toBe("normal");
    expect(controller.observe("unstable", 400)).toBe("reduced-visible-video");
  });

  it("clamps a backwards clock and rejects non-finite time", () => {
    let now = 10_000;
    const controller = new ClassroomDegradationController({
      clock: () => now,
      timing: { escalateAfterMs: 100, recoverAfterMs: 300 },
    });

    expect(controller.observe("unstable")).toBe("normal");
    now = 9_000;
    expect(controller.observe("unstable")).toBe("normal");
    now = 10_100;
    expect(controller.observe("unstable")).toBe("reduced-visible-video");
    expect(() => controller.observe("stable", Number.NaN)).toThrowError(
      "Classroom degradation time must be finite.",
    );
  });

  it("validates hysteresis timing and supports deterministic reset", () => {
    expect(
      () =>
        new ClassroomDegradationController({
          timing: { escalateAfterMs: 0 },
        }),
    ).toThrowError("Degradation timing requires");
    expect(
      () =>
        new ClassroomDegradationController({
          timing: { escalateAfterMs: 500, recoverAfterMs: 500 },
        }),
    ).toThrowError("Degradation timing requires");
    expect(() =>
      new ClassroomDegradationController().observe(
        "critical" as ClassroomDegradationSignal,
        0,
      ),
    ).toThrowError("Unknown classroom degradation signal.");

    const controller = new ClassroomDegradationController({
      initialStage: "audio-only",
    });
    controller.reset("lower-remote-quality", 42);
    expect(controller.snapshot()).toEqual({
      stage: "lower-remote-quality",
      stageChangedAtMs: 42,
      pendingSignal: null,
      pendingSinceMs: null,
    });
  });
});
