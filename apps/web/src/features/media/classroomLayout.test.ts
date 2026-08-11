import { describe, expect, it } from "vitest";
import {
  ACTIVE_SPEAKER_TIMING,
  ActiveSpeakerHysteresis,
  clampPage,
  createLayoutFixture,
  enterPresentation,
  getBoundedRail,
  getGridCapacity,
  paginateItems,
  projectClassroomLayout,
  restorePresentation,
  type ClassroomLayoutState,
} from "./classroomLayout";

describe("classroom grid capacity and pagination", () => {
  it.each([
    [320, 4],
    [767, 4],
    [768, 6],
    [1_199, 6],
    [1_200, 12],
    [1_280, 12],
    [Number.NaN, 4],
  ] as const)("maps %s CSS pixels to a %i-tile cap", (width, expected) => {
    expect(getGridCapacity(width)).toBe(expected);
  });

  it("clamps invalid, negative and stale pages", () => {
    expect(clampPage(Number.NaN, 5)).toBe(0);
    expect(clampPage(-4, 5)).toBe(0);
    expect(clampPage(99, 5)).toBe(4);
    expect(clampPage(2.9, 5)).toBe(2);
    expect(clampPage(3, 0)).toBe(0);
  });

  it("paginates without mutating or reordering the input", () => {
    const input = ["a", "b", "c", "d", "e"] as const;
    const result = paginateItems(input, 99, 2);

    expect(result).toEqual({
      items: ["e"],
      page: 2,
      pageCount: 3,
      capacity: 2,
    });
    expect(input).toEqual(["a", "b", "c", "d", "e"]);
  });

  it.each([
    [2, 1, 2],
    [5, 1, 5],
    [25, 3, 12],
    [50, 5, 12],
  ] as const)(
    "bounds a %i-person desktop fixture to %i page(s) and %i visible item(s)",
    (fixtureSize, expectedPages, expectedVisible) => {
      const projection = projectClassroomLayout({
        items: createLayoutFixture(fixtureSize),
        mode: "grid",
        width: 1_280,
        requestedPage: 0,
        activeSpeakerId: "participant-02",
        pinnedParticipantId: null,
        presenterId: null,
      });

      expect(projection.pageCount).toBe(expectedPages);
      expect(projection.items).toHaveLength(expectedVisible);
      expect(projection.subscribedVideoItemIds).toHaveLength(expectedVisible);
    },
  );

  it("caps medium and compact grids and clamps after the final page shrinks", () => {
    const participants = createLayoutFixture(25);
    expect(
      projectClassroomLayout({
        items: participants,
        mode: "grid",
        width: 900,
        requestedPage: 0,
        activeSpeakerId: null,
        pinnedParticipantId: null,
        presenterId: null,
      }).items,
    ).toHaveLength(6);

    const compact = projectClassroomLayout({
      items: participants.slice(0, 9),
      mode: "grid",
      width: 320,
      requestedPage: 99,
      activeSpeakerId: null,
      pinnedParticipantId: null,
      presenterId: null,
    });
    expect(compact.page).toBe(2);
    expect(compact.items.map(({ id }) => id)).toEqual(["participant-09"]);
  });
});

describe("classroom stage, rail and presentation restore", () => {
  it("keeps a bounded, unique rail that excludes the stage", () => {
    const participants = createLayoutFixture(25);
    const withDuplicate = [...participants, participants[1]!];
    const rail = getBoundedRail(withDuplicate, "participant-01", 99);

    expect(rail).toHaveLength(6);
    expect(rail.map(({ id }) => id)).toEqual([
      "participant-02",
      "participant-03",
      "participant-04",
      "participant-05",
      "participant-06",
      "participant-07",
    ]);
  });

  it("lets local pin win without changing canonical grid order", () => {
    const participants = createLayoutFixture(25);
    const gridBefore = projectClassroomLayout({
      items: participants,
      mode: "grid",
      width: 1_280,
      requestedPage: 0,
      activeSpeakerId: "participant-02",
      pinnedParticipantId: null,
      presenterId: null,
    });
    const gridAfter = projectClassroomLayout({
      items: participants,
      mode: "grid",
      width: 1_280,
      requestedPage: 0,
      activeSpeakerId: "participant-20",
      pinnedParticipantId: null,
      presenterId: null,
    });
    const speaker = projectClassroomLayout({
      items: participants,
      mode: "active-speaker",
      width: 1_280,
      requestedPage: 0,
      activeSpeakerId: "participant-20",
      pinnedParticipantId: "participant-08",
      presenterId: null,
    });

    expect(gridAfter.items).toEqual(gridBefore.items);
    expect(speaker.stage).toMatchObject({
      kind: "participant",
      item: { id: "participant-08" },
    });
    expect(speaker.items).toHaveLength(6);
    expect(speaker.subscribedVideoItemIds).toHaveLength(7);
  });

  it("uses the share as deterministic stage and keeps the rail bounded", () => {
    const projection = projectClassroomLayout({
      items: createLayoutFixture(50),
      mode: "presentation",
      width: 1_280,
      requestedPage: 0,
      activeSpeakerId: "participant-20",
      pinnedParticipantId: "participant-08",
      presenterId: "participant-04",
    });

    expect(projection.stage).toMatchObject({
      kind: "presentation",
      item: { id: "participant-04" },
    });
    expect(projection.items).toHaveLength(6);
    expect(projection.items.map(({ id }) => id)).not.toContain(
      "participant-04",
    );
    expect(projection.subscribedVideoItemIds).toHaveLength(7);
  });

  it("preserves the first restore snapshot across share replacement", () => {
    const initial: ClassroomLayoutState = {
      mode: "active-speaker",
      requestedPage: 2,
      pinnedParticipantId: "participant-08",
      presenterId: null,
      focusTargetId: "pin-participant-08",
      presentationRestore: null,
    };
    const firstShare = enterPresentation(initial, "participant-04");
    const replacement = enterPresentation(firstShare, "participant-05");
    const restored = restorePresentation(
      replacement,
      createLayoutFixture(25),
      1_280,
    );

    expect(replacement.presentationRestore).toEqual(
      firstShare.presentationRestore,
    );
    expect(restored).toEqual({
      state: {
        mode: "active-speaker",
        requestedPage: 2,
        pinnedParticipantId: "participant-08",
        presenterId: null,
        focusTargetId: "pin-participant-08",
        presentationRestore: null,
      },
      restoredExactState: true,
    });
  });

  it("falls back to the local participant grid page when the old pin left", () => {
    const participants = createLayoutFixture(25).map((participant, index) => ({
      ...participant,
      isLocal: index === 17,
    }));
    const state = enterPresentation(
      {
        mode: "active-speaker",
        requestedPage: 3,
        pinnedParticipantId: "participant-08",
        presenterId: null,
        focusTargetId: "pin-participant-08",
        presentationRestore: null,
      },
      "participant-04",
    );
    const withoutPinned = participants.filter(
      ({ id }) => id !== "participant-08",
    );
    const restored = restorePresentation(state, withoutPinned, 900);

    expect(restored).toEqual({
      state: {
        mode: "grid",
        requestedPage: 2,
        pinnedParticipantId: null,
        presenterId: null,
        focusTargetId: "classroom-layout-controls",
        presentationRestore: null,
      },
      restoredExactState: false,
    });
  });
});

describe("ActiveSpeakerHysteresis", () => {
  it("uses the locked 800/2500/1500 timing constants", () => {
    expect(ACTIVE_SPEAKER_TIMING).toEqual({
      enterMs: 800,
      minHoldMs: 2_500,
      silenceReleaseMs: 1_500,
    });
  });

  it("requires a candidate to remain dominant for 800 ms", () => {
    const selector = new ActiveSpeakerHysteresis();

    expect(selector.observe("participant-02", 1_000)).toBeNull();
    expect(selector.observe("participant-02", 1_799)).toBeNull();
    expect(selector.observe("participant-02", 1_800)).toBe("participant-02");
  });

  it("requires both minimum hold and silence before switching", () => {
    const selector = new ActiveSpeakerHysteresis();
    selector.observe("participant-01", 0);
    selector.observe("participant-01", 800);

    expect(selector.observe("participant-02", 1_000)).toBe("participant-01");
    expect(selector.observe("participant-02", 2_500)).toBe("participant-01");
    expect(selector.observe("participant-02", 3_299)).toBe("participant-01");
    expect(selector.observe("participant-02", 3_300)).toBe("participant-02");
  });

  it("releases a silent stage only after both protection windows", () => {
    const selector = new ActiveSpeakerHysteresis();
    selector.observe("participant-01", 0);
    selector.observe("participant-01", 800);

    expect(selector.observe(null, 1_000)).toBe("participant-01");
    expect(selector.observe(null, 2_500)).toBe("participant-01");
    expect(selector.observe(null, 3_299)).toBe("participant-01");
    expect(selector.observe(null, 3_300)).toBeNull();
  });

  it("supports an injected clock and clamps a clock that moves backwards", () => {
    let now = 10_000;
    const selector = new ActiveSpeakerHysteresis(() => now);

    expect(selector.observe("participant-03")).toBeNull();
    now = 9_000;
    expect(selector.observe("participant-03")).toBeNull();
    now = 10_800;
    expect(selector.observe("participant-03")).toBe("participant-03");
    expect(selector.snapshot()).toMatchObject({
      selectedId: "participant-03",
      selectedAtMs: 10_800,
    });
  });
});
