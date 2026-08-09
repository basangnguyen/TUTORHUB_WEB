import { describe, expect, it } from "vitest";
import {
  ACTIVE_SPEAKER_TIMING,
  createParticipants,
  projectHandQueue,
  projectLayout,
  projectReactions,
  REACTION_POLICY,
  resolveMediaPresentation,
  selectActiveSpeaker,
  type HandEvent,
  type ReactionEvent,
} from "./mediaModel";

describe("layout decision model", () => {
  it.each([
    [2, 1, 2],
    [5, 1, 5],
    [25, 3, 12],
    [50, 5, 12],
  ] as const)(
    "bounds the desktop grid for a %i-person fixture",
    (fixtureSize, expectedPages, expectedVisible) => {
      const participants = createParticipants(fixtureSize);
      const result = projectLayout({
        participants,
        mode: "grid",
        viewport: "standard",
        requestedPage: 0,
        activeSpeakerId: participants[1]?.id ?? null,
        pinnedParticipantId: null,
        presenterId: participants[0]?.id ?? null,
      });

      expect(result.pageCount).toBe(expectedPages);
      expect(result.visibleParticipantIds).toHaveLength(expectedVisible);
      expect(result.subscribedParticipantIds).toHaveLength(expectedVisible);
    },
  );

  it("caps compact grids at four tiles and clamps stale page indexes", () => {
    const participants = createParticipants(50);
    const result = projectLayout({
      participants,
      mode: "grid",
      viewport: "compact",
      requestedPage: 99,
      activeSpeakerId: participants[1]?.id ?? null,
      pinnedParticipantId: null,
      presenterId: participants[0]?.id ?? null,
    });

    expect(result.pageCount).toBe(13);
    expect(result.page).toBe(12);
    expect(result.visibleParticipantIds).toHaveLength(2);
  });

  it("caps medium grids at six tiles", () => {
    const participants = createParticipants(25);
    const result = projectLayout({
      participants,
      mode: "grid",
      viewport: "medium",
      requestedPage: 0,
      activeSpeakerId: participants[1]?.id ?? null,
      pinnedParticipantId: null,
      presenterId: participants[0]?.id ?? null,
    });

    expect(result.pageCount).toBe(5);
    expect(result.visibleParticipantIds).toHaveLength(6);
  });

  it("lets a local pin win without reordering the bounded rail", () => {
    const participants = createParticipants(25);
    const pinned = participants[7];
    const active = participants[19];
    expect(pinned).toBeDefined();
    expect(active).toBeDefined();

    const result = projectLayout({
      participants,
      mode: "active-speaker",
      viewport: "standard",
      requestedPage: 0,
      activeSpeakerId: active?.id ?? null,
      pinnedParticipantId: pinned?.id ?? null,
      presenterId: participants[0]?.id ?? null,
    });

    expect(result.focus).toEqual({
      kind: "participant",
      participantId: pinned?.id,
    });
    expect(result.visibleParticipantIds).toEqual(
      participants
        .filter(({ id }) => id !== pinned?.id)
        .slice(0, 6)
        .map(({ id }) => id),
    );
    expect(result.subscribedParticipantIds).toHaveLength(7);
  });

  it("does not reorder grid focus order when active speaker changes", () => {
    const participants = createParticipants(25);
    const base = {
      participants,
      mode: "grid" as const,
      viewport: "standard" as const,
      requestedPage: 0,
      pinnedParticipantId: null,
      presenterId: participants[0]?.id ?? null,
    };

    const first = projectLayout({
      ...base,
      activeSpeakerId: participants[1]?.id ?? null,
    });
    const later = projectLayout({
      ...base,
      activeSpeakerId: participants[19]?.id ?? null,
    });

    expect(later.visibleParticipantIds).toEqual(first.visibleParticipantIds);
  });

  it("keeps presentation content as focus and uses a bounded participant rail", () => {
    const participants = createParticipants(50);
    const presenter = participants[3];
    const result = projectLayout({
      participants,
      mode: "presentation",
      viewport: "standard",
      requestedPage: 0,
      activeSpeakerId: participants[10]?.id ?? null,
      pinnedParticipantId: participants[8]?.id ?? null,
      presenterId: presenter?.id ?? null,
    });

    expect(result.focus).toEqual({
      kind: "presentation",
      participantId: presenter?.id,
    });
    expect(result.visibleParticipantIds).toEqual(
      participants.slice(0, 6).map(({ id }) => id),
    );
    expect(result.subscribedParticipantIds).toHaveLength(6);
  });
});

describe("active-speaker fake clock", () => {
  it("requires enter, hold and silence windows before selection changes", () => {
    expect(ACTIVE_SPEAKER_TIMING).toEqual({
      enterMs: 800,
      minHoldMs: 2_500,
      silenceReleaseMs: 1_500,
    });
    expect(
      selectActiveSpeaker({
        nowMs: 10_000,
        currentParticipantId: null,
        currentSelectedAtMs: null,
        currentSilentSinceMs: null,
        candidateParticipantId: "participant-02",
        candidateSpeakingSinceMs: 9_201,
      }),
    ).toBeNull();
    expect(
      selectActiveSpeaker({
        nowMs: 10_000,
        currentParticipantId: null,
        currentSelectedAtMs: null,
        currentSilentSinceMs: null,
        candidateParticipantId: "participant-02",
        candidateSpeakingSinceMs: 9_200,
      }),
    ).toBe("participant-02");

    const base = {
      nowMs: 10_000,
      currentParticipantId: "participant-01",
      candidateParticipantId: "participant-02",
      candidateSpeakingSinceMs: 9_000,
    };
    expect(
      selectActiveSpeaker({
        ...base,
        currentSelectedAtMs: 7_501,
        currentSilentSinceMs: 8_000,
      }),
    ).toBe("participant-01");
    expect(
      selectActiveSpeaker({
        ...base,
        currentSelectedAtMs: 7_500,
        currentSilentSinceMs: 8_501,
      }),
    ).toBe("participant-01");
    expect(
      selectActiveSpeaker({
        ...base,
        currentSelectedAtMs: 7_500,
        currentSilentSinceMs: 8_500,
      }),
    ).toBe("participant-02");
  });

  it("releases a silent speaker after both protection windows", () => {
    expect(
      selectActiveSpeaker({
        nowMs: 10_000,
        currentParticipantId: "participant-01",
        currentSelectedAtMs: 7_500,
        currentSilentSinceMs: 8_500,
        candidateParticipantId: null,
        candidateSpeakingSinceMs: null,
      }),
    ).toBeNull();
  });
});

describe("server-authoritative hand projection", () => {
  it("converges for duplicate and out-of-order delivery", () => {
    const events: HandEvent[] = [
      {
        eventId: "raise-b",
        participantId: "participant-02",
        serverSequence: 12,
        kind: "raise",
      },
      {
        eventId: "raise-a",
        participantId: "participant-01",
        serverSequence: 10,
        kind: "raise",
      },
      {
        eventId: "lower-a",
        participantId: "participant-01",
        serverSequence: 13,
        kind: "lower",
      },
      {
        eventId: "raise-a",
        participantId: "participant-01",
        serverSequence: 10,
        kind: "raise",
      },
      {
        eventId: "raise-c",
        participantId: "participant-03",
        serverSequence: 11,
        kind: "raise",
      },
      {
        eventId: "raise-a-again",
        participantId: "participant-01",
        serverSequence: 14,
        kind: "raise",
      },
    ];

    expect(projectHandQueue(events)).toEqual({
      queue: [
        { participantId: "participant-03", raisedSequence: 11 },
        { participantId: "participant-02", raisedSequence: 12 },
        { participantId: "participant-01", raisedSequence: 14 },
      ],
      lastServerSequence: 14,
      duplicateEventCount: 1,
    });
    expect(projectHandQueue([...events].reverse())).toEqual(
      projectHandQueue(events),
    );
  });

  it("keeps a 50-person storm ordered and idempotent", () => {
    const events = createParticipants(50).flatMap(
      (participant, index): readonly HandEvent[] => [
        {
          eventId: `raise-${participant.id}`,
          participantId: participant.id,
          serverSequence: 1_000 + index,
          kind: "raise",
        },
        {
          eventId: `raise-${participant.id}`,
          participantId: participant.id,
          serverSequence: 1_000 + index,
          kind: "raise",
        },
      ],
    );
    const projection = projectHandQueue([...events].reverse());

    expect(projection.queue).toHaveLength(50);
    expect(projection.queue[0]).toEqual({
      participantId: "participant-01",
      raisedSequence: 1_000,
    });
    expect(projection.queue.at(-1)).toEqual({
      participantId: "participant-50",
      raisedSequence: 1_049,
    });
    expect(projection.duplicateEventCount).toBe(50);
  });
});

describe("reaction projection", () => {
  it("applies allowlist, duplicate handling, actor limit, grouping and TTL", () => {
    const events: ReactionEvent[] = [
      {
        eventId: "r1",
        participantId: "participant-01",
        emoji: "👏",
        serverSequence: 1,
        acceptedAtMs: 1_000,
      },
      {
        eventId: "r2",
        participantId: "participant-01",
        emoji: "👏",
        serverSequence: 2,
        acceptedAtMs: 1_200,
      },
      {
        eventId: "r3",
        participantId: "participant-01",
        emoji: "👍",
        serverSequence: 3,
        acceptedAtMs: 1_400,
      },
      {
        eventId: "r4",
        participantId: "participant-01",
        emoji: "🎉",
        serverSequence: 4,
        acceptedAtMs: 1_600,
      },
      {
        eventId: "r5",
        participantId: "participant-02",
        emoji: "raw-payload",
        serverSequence: 5,
        acceptedAtMs: 1_800,
      },
      {
        eventId: "r1",
        participantId: "participant-01",
        emoji: "👏",
        serverSequence: 6,
        acceptedAtMs: 2_000,
      },
    ];

    const live = projectReactions(events, 2_000);
    expect(live.acceptedEventIds).toEqual(["r1", "r2", "r3"]);
    expect(live.clusters).toEqual([
      expect.objectContaining({ emoji: "👏", count: 2 }),
      expect.objectContaining({ emoji: "👍", count: 1 }),
    ]);
    expect(live.rejections).toEqual([
      { eventId: "r4", reason: "actor-burst-rate-limit" },
      { eventId: "r5", reason: "not-allowed" },
      { eventId: "r1", reason: "duplicate" },
    ]);

    const expired = projectReactions(events, 1_400 + REACTION_POLICY.ttlMs);
    expect(expired.clusters).toHaveLength(0);
  });

  it("enforces the room-wide limit across different actors", () => {
    const events = Array.from(
      { length: REACTION_POLICY.maxPerRoom + 1 },
      (_, index): ReactionEvent => ({
        eventId: `room-${index}`,
        participantId: `participant-${index}`,
        emoji: "👍",
        serverSequence: index,
        acceptedAtMs: 1_000 + index,
      }),
    );
    const result = projectReactions(events, 2_000);

    expect(result.acceptedEventIds).toHaveLength(REACTION_POLICY.maxPerRoom);
    expect(result.rejections.at(-1)).toEqual({
      eventId: `room-${REACTION_POLICY.maxPerRoom}`,
      reason: "room-rate-limit",
    });
    expect(result.clusters).toEqual([
      expect.objectContaining({
        emoji: "👍",
        count: REACTION_POLICY.maxPerRoom,
      }),
    ]);
  });

  it("applies the 20-per-minute sustained actor limit independently", () => {
    const events = Array.from(
      { length: REACTION_POLICY.maxPerActorSustained + 1 },
      (_, index): ReactionEvent => ({
        eventId: `sustained-${index}`,
        participantId: "participant-01",
        emoji: "😂",
        serverSequence: index,
        acceptedAtMs: 1_000 + index * 2_600,
      }),
    );
    const result = projectReactions(events, 55_000);

    expect(result.acceptedEventIds).toHaveLength(
      REACTION_POLICY.maxPerActorSustained,
    );
    expect(result.rejections).toContainEqual({
      eventId: `sustained-${REACTION_POLICY.maxPerActorSustained}`,
      reason: "actor-sustained-rate-limit",
    });
  });
});

describe("media degradation decision", () => {
  it("falls back in the declared order without blocking the room", () => {
    expect(resolveMediaPresentation("blur", 0, false)).toEqual(
      expect.objectContaining({
        effectiveEffect: "none",
        degradationLevel: 1,
        videoProfile: "720p/30fps",
        reason: "effect-capability-unavailable",
      }),
    );
    expect(resolveMediaPresentation("forest", 2, true)).toEqual(
      expect.objectContaining({
        effectiveEffect: "none",
        videoProfile: "360p/15fps",
        reason: "video-degraded",
      }),
    );
    expect(resolveMediaPresentation("studio", 3, true)).toEqual(
      expect.objectContaining({
        effectiveEffect: "none",
        videoProfile: "audio-only",
        reason: "camera-disabled",
      }),
    );
  });
});
