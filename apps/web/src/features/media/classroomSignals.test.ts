import { describe, expect, it } from "vitest";
import {
  MAX_VISIBLE_REACTION_CLUSTERS,
  REACTION_GROUP_WINDOW_MS,
  REACTION_TTL_MS,
  classifyClassroomSignalSequence,
  foldClassroomSignalSnapshot,
  groupClassroomReactionEvents,
  identifyClassroomSignalSequenceGap,
  projectReactionClusters,
  sortCanonicalRoster,
  validateClassroomSignalSnapshot,
  visibleReactionCount,
  type ClassroomReactionCluster,
  type ClassroomReactionEvent,
  type ClassroomSignalSnapshot,
} from "./classroomSignals";

const BASE_TIME_MS = Date.parse("2026-08-12T00:00:00.000Z");
const ROOM_INSTANCE_ID = "018f4c7b-9b0a-7a34-8a4c-96d26cb87221";
const PARTICIPANT_A = "018f4c7b-9b0a-7a34-8a4c-96d26cb87221";
const PARTICIPANT_B = "018f4c7b-9b0a-7a34-8a4c-96d26cb87222";
const PARTICIPANT_C = "018f4c7b-9b0a-7a34-8a4c-96d26cb87223";

function snapshot(
  overrides: Partial<ClassroomSignalSnapshot> = {},
): ClassroomSignalSnapshot {
  return {
    room_instance_id: ROOM_INSTANCE_ID,
    projection_version: 3,
    last_signal_sequence: 8,
    self_participant_key: PARTICIPANT_B,
    viewer_operations: {
      can_raise_hand: true,
      can_send_reaction: true,
      can_moderate_hands: true,
    },
    participants: [
      {
        participant_key: PARTICIPANT_C,
        roster_sequence: 2,
        display_name: "Student C",
        instance_role: "attendee",
        connection_state: "reconnecting",
      },
      {
        participant_key: PARTICIPANT_B,
        roster_sequence: 1,
        display_name: "Teacher B",
        instance_role: "host",
        connection_state: "connected",
      },
      {
        participant_key: PARTICIPANT_A,
        roster_sequence: 1,
        display_name: "Student A",
        instance_role: "attendee",
        connection_state: "joining",
      },
    ],
    raised_hands: [
      {
        participant_key: PARTICIPANT_C,
        signal_sequence: 7,
        raised_at: "2026-08-12T00:00:02.000Z",
      },
      {
        participant_key: PARTICIPANT_A,
        signal_sequence: 4,
        raised_at: "2026-08-12T00:00:01.000Z",
      },
    ],
    reaction_clusters: [],
    server_time: "2026-08-12T00:00:00.000Z",
    ...overrides,
  };
}

function reactionEvent(
  eventId: string,
  type: ClassroomReactionEvent["reaction"],
  acceptedOffsetMs: number,
  sequence: number,
  expiresOffsetMs = 10_000,
): ClassroomReactionEvent {
  return {
    event_id: eventId,
    reaction: type,
    signal_sequence: sequence,
    accepted_at: new Date(BASE_TIME_MS + acceptedOffsetMs).toISOString(),
    expires_at: new Date(
      BASE_TIME_MS + acceptedOffsetMs + expiresOffsetMs,
    ).toISOString(),
  };
}

function reactionCluster(
  type: ClassroomReactionCluster["reaction"],
  acceptedOffsetMs: number,
  firstSequence: number,
  count = 1,
  expiresOffsetMs = 10_000,
): ClassroomReactionCluster {
  return {
    reaction: type,
    count,
    first_signal_sequence: firstSequence,
    last_signal_sequence: firstSequence + count - 1,
    accepted_at: new Date(BASE_TIME_MS + acceptedOffsetMs).toISOString(),
    expires_at: new Date(
      BASE_TIME_MS + acceptedOffsetMs + expiresOffsetMs,
    ).toISOString(),
  };
}

describe("classroom signal snapshot validation and folding", () => {
  it("validates a bounded snapshot and canonicalizes roster and FIFO hand order", () => {
    const result = foldClassroomSignalSnapshot(null, snapshot(), BASE_TIME_MS);

    expect(result.status).toBe("applied");
    expect(
      result.projection?.roster.map(({ participant_key }) => participant_key),
    ).toEqual([PARTICIPANT_A, PARTICIPANT_B, PARTICIPANT_C]);
    expect(result.projection?.raised_hands).toEqual([
      expect.objectContaining({
        participant_key: PARTICIPANT_A,
        display_name: "Student A",
        queue_position: 1,
      }),
      expect.objectContaining({
        participant_key: PARTICIPANT_C,
        display_name: "Student C",
        queue_position: 2,
      }),
    ]);
  });

  it("counts bounded display names by Unicode code point like the Core API", () => {
    const exactBoundary = validateClassroomSignalSnapshot({
      ...snapshot(),
      participants: snapshot().participants.map((participant, index) =>
        index === 0
          ? { ...participant, display_name: "😀".repeat(200) }
          : participant,
      ),
    });
    const overBoundary = validateClassroomSignalSnapshot({
      ...snapshot(),
      participants: snapshot().participants.map((participant, index) =>
        index === 0
          ? { ...participant, display_name: "😀".repeat(201) }
          : participant,
      ),
    });

    expect(exactBoundary.valid).toBe(true);
    expect(overBoundary.valid).toBe(false);
    expect(overBoundary.issues).toContain(
      "participants[0].display_name must be a non-empty string of at most 200 Unicode code points",
    );
  });

  it("allows only the 10-second reaction TTL plus one grouping-window tolerance", () => {
    const maximumClusterTTL = REACTION_TTL_MS + REACTION_GROUP_WINDOW_MS;
    const exactBoundary = validateClassroomSignalSnapshot({
      ...snapshot(),
      reaction_clusters: [reactionCluster("clap", 0, 8, 1, maximumClusterTTL)],
    });
    const overBoundary = validateClassroomSignalSnapshot({
      ...snapshot(),
      reaction_clusters: [
        reactionCluster("clap", 0, 8, 1, maximumClusterTTL + 1),
      ],
    });

    expect(exactBoundary.valid).toBe(true);
    expect(overBoundary.valid).toBe(false);
    expect(overBoundary.issues).toContain(
      `reaction cluster TTL must not exceed ${maximumClusterTTL} milliseconds`,
    );
  });

  it("does not mutate the server roster while applying sequence/key tie-breaking", () => {
    const input = snapshot().participants;
    const before = [...input];

    expect(
      sortCanonicalRoster(input).map(({ participant_key }) => participant_key),
    ).toEqual([PARTICIPANT_A, PARTICIPANT_B, PARTICIPANT_C]);
    expect(input).toEqual(before);
  });

  it("drops unrecognized response properties from the privacy-safe projection", () => {
    const unsafeParticipant = {
      ...snapshot().participants[0],
      email: "must-not-project@example.test",
      participant_session_id: ROOM_INSTANCE_ID,
      provider_identity: "provider-identity",
    };
    const result = foldClassroomSignalSnapshot(
      null,
      { ...snapshot(), participants: [unsafeParticipant] },
      BASE_TIME_MS,
    );

    expect(result.status).toBe("invalid");
    expect(result.issues).toContain(
      "self_participant_key is outside the roster",
    );

    const selfResult = foldClassroomSignalSnapshot(
      null,
      {
        ...snapshot(),
        self_participant_key: PARTICIPANT_C,
        participants: [unsafeParticipant],
        raised_hands: [],
      },
      BASE_TIME_MS,
    );
    expect(selfResult.status).toBe("applied");
    expect(selfResult.projection?.roster[0]).toEqual({
      participant_key: PARTICIPANT_C,
      roster_sequence: 2,
      display_name: "Student C",
      instance_role: "attendee",
      connection_state: "reconnecting",
    });
  });

  it("rejects duplicate keys, ambiguous hands, invalid enums and excessive arrays", () => {
    const tooManyClusters = Array.from({ length: 51 }, (_, index) =>
      reactionCluster("clap", index * 800, index + 1),
    );
    const unsafe = {
      ...snapshot(),
      participants: [
        ...snapshot().participants,
        {
          ...snapshot().participants[0],
          participant_key: PARTICIPANT_A,
          instance_role: "owner",
        },
      ],
      raised_hands: [...snapshot().raised_hands, snapshot().raised_hands[0]],
      reaction_clusters: tooManyClusters,
    };
    const result = validateClassroomSignalSnapshot(unsafe);

    expect(result.valid).toBe(false);
    expect(result.issues).toEqual(
      expect.arrayContaining([
        "participants[3].instance_role is not allowed",
        "raised_hands contains more than one active hand per participant",
        "reaction_clusters must be an array with at most 50 items",
      ]),
    );
  });

  it("rejects malformed time, future sequences and orphan active hands", () => {
    const result = validateClassroomSignalSnapshot({
      ...snapshot(),
      raised_hands: [
        {
          participant_key: "018f4c7b-9b0a-7a34-8a4c-96d26cb87299",
          signal_sequence: 9,
          raised_at: "not-a-date",
        },
      ],
      reaction_clusters: [reactionCluster("heart", 0, 9)],
    });

    expect(result.valid).toBe(false);
    expect(result.issues).toEqual(
      expect.arrayContaining([
        "raised_hands[0].raised_at must be a valid date-time",
        "reaction_clusters contains a sequence after last_signal_sequence",
      ]),
    );
  });

  it("keeps the newest projection when a stale snapshot arrives", () => {
    const first = foldClassroomSignalSnapshot(null, snapshot(), BASE_TIME_MS);
    expect(first.projection).not.toBeNull();

    const stale = foldClassroomSignalSnapshot(
      first.projection,
      snapshot({ projection_version: 2, last_signal_sequence: 7 }),
      BASE_TIME_MS,
    );

    expect(stale.status).toBe("stale");
    expect(stale.projection).toBe(first.projection);
  });

  it("reports a jump while accepting the full authoritative replacement snapshot", () => {
    const first = foldClassroomSignalSnapshot(null, snapshot(), BASE_TIME_MS);
    const next = foldClassroomSignalSnapshot(
      first.projection,
      snapshot({
        projection_version: 4,
        last_signal_sequence: 12,
        raised_hands: [],
      }),
      BASE_TIME_MS,
    );

    expect(next.status).toBe("applied");
    expect(next.sequence_gap).toEqual({
      expected_sequence: 9,
      received_sequence: 12,
      missing_count: 3,
    });
    expect(next.projection?.raised_hands).toEqual([]);
  });

  it("does not replace a good projection with an invalid snapshot", () => {
    const first = foldClassroomSignalSnapshot(null, snapshot(), BASE_TIME_MS);
    const invalid = foldClassroomSignalSnapshot(
      first.projection,
      { ...snapshot(), projection_version: 0 },
      BASE_TIME_MS,
    );

    expect(invalid.status).toBe("invalid");
    expect(invalid.projection).toBe(first.projection);
  });
});

describe("classroom reaction projection", () => {
  it("groups the same enum within an inclusive fixed 750 ms server window", () => {
    const clusters = groupClassroomReactionEvents([
      reactionEvent("a", "clap", 0, 1),
      reactionEvent("b", "clap", 750, 2),
      reactionEvent("c", "clap", 751, 3),
    ]);

    expect(clusters).toEqual([
      expect.objectContaining({
        reaction: "clap",
        count: 2,
        first_signal_sequence: 1,
        last_signal_sequence: 2,
        accepted_at: "2026-08-12T00:00:00.000Z",
      }),
      expect.objectContaining({
        reaction: "clap",
        count: 1,
        first_signal_sequence: 3,
        last_signal_sequence: 3,
      }),
    ]);
  });

  it("groups interleaved enums independently and uses the latest event expiry", () => {
    const clusters = groupClassroomReactionEvents([
      reactionEvent("a", "clap", 0, 1, 5_000),
      reactionEvent("b", "heart", 100, 2, 5_000),
      reactionEvent("c", "clap", 200, 3, 10_000),
    ]);

    expect(clusters).toEqual([
      expect.objectContaining({
        reaction: "clap",
        count: 2,
        accepted_at: "2026-08-12T00:00:00.000Z",
        expires_at: "2026-08-12T00:00:10.200Z",
      }),
      expect.objectContaining({ reaction: "heart", count: 1 }),
    ]);
  });

  it("expires clusters at server expires_at", () => {
    const clusters = [
      reactionCluster("heart", 0, 1, 2, 1_000),
      reactionCluster("clap", 500, 3, 1, 10_000),
    ];

    const before = projectReactionClusters(clusters, BASE_TIME_MS + 999);
    const after = projectReactionClusters(clusters, BASE_TIME_MS + 1_000);

    expect(before.clusters).toHaveLength(2);
    expect(after.clusters).toEqual([
      expect.objectContaining({ cluster_id: "clap:3", count: 1 }),
    ]);
  });

  it("caps visual clusters at three but retains bounded allowlist totals", () => {
    const projection = projectReactionClusters(
      [
        reactionCluster("clap", 0, 1),
        reactionCluster("heart", 10, 2),
        reactionCluster("laugh", 20, 3),
        reactionCluster("surprised", 30, 4),
        reactionCluster("celebrate", 40, 5),
      ],
      BASE_TIME_MS,
    );

    expect(projection.clusters).toHaveLength(MAX_VISIBLE_REACTION_CLUSTERS);
    expect(projection.clusters.map(({ cluster_id }) => cluster_id)).toEqual([
      "laugh:3",
      "surprised:4",
      "celebrate:5",
    ]);
    expect(projection.hidden_cluster_count).toBe(2);
    expect(projection.summary).toHaveLength(5);
  });

  it("renders 99+ while retaining the exact aggregate count", () => {
    const projection = projectReactionClusters(
      [reactionCluster("thumbs_up", 0, 1, 100)],
      BASE_TIME_MS,
    );

    expect(projection.clusters).toEqual([
      expect.objectContaining({ count: 100, count_label: "99+" }),
    ]);
    expect(projection.summary).toEqual([
      { reaction: "thumbs_up", count: 100, count_label: "99+" },
    ]);
    expect(visibleReactionCount(Number.POSITIVE_INFINITY)).toBe("0");
  });
});

describe("classroom signal sequence classification", () => {
  it("distinguishes duplicate, exact-next and missing-event sequence states", () => {
    expect(classifyClassroomSignalSequence(8, 8)).toBe("duplicate");
    expect(classifyClassroomSignalSequence(8, 9)).toBe("next");
    expect(classifyClassroomSignalSequence(8, 11)).toBe("gap");
    expect(identifyClassroomSignalSequenceGap(8, 11)).toEqual({
      expected_sequence: 9,
      received_sequence: 11,
      missing_count: 2,
    });
  });

  it("rejects non-integer sequence inputs instead of silently misclassifying them", () => {
    expect(() => classifyClassroomSignalSequence(1.5, 2)).toThrow(TypeError);
    expect(() => classifyClassroomSignalSequence(1, Number.NaN)).toThrow(
      TypeError,
    );
  });
});
