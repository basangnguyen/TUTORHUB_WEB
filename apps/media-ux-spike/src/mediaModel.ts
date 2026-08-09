export const FIXTURE_SIZES = [2, 5, 25, 50] as const;
export type FixtureSize = (typeof FIXTURE_SIZES)[number];

export const LAYOUT_MODES = ["grid", "active-speaker", "presentation"] as const;
export type LayoutMode = (typeof LAYOUT_MODES)[number];
export const VIEWPORT_PROFILES = ["standard", "medium", "compact"] as const;
export type ViewportProfile = (typeof VIEWPORT_PROFILES)[number];

export interface Participant {
  id: string;
  displayName: string;
  stableIndex: number;
  role: "teacher" | "student";
  isLocal: boolean;
}

export interface LayoutInput {
  participants: readonly Participant[];
  mode: LayoutMode;
  viewport: ViewportProfile;
  requestedPage: number;
  activeSpeakerId: string | null;
  pinnedParticipantId: string | null;
  presenterId: string | null;
}

export interface LayoutFocus {
  kind: "participant" | "presentation";
  participantId: string;
}

export interface LayoutProjection {
  mode: LayoutMode;
  focus: LayoutFocus | null;
  page: number;
  pageCount: number;
  pageSize: number;
  visibleParticipantIds: readonly string[];
  subscribedParticipantIds: readonly string[];
}

const PAGE_SIZES: Record<LayoutMode, Record<ViewportProfile, number>> = {
  grid: { standard: 12, medium: 6, compact: 4 },
  "active-speaker": { standard: 6, medium: 5, compact: 3 },
  presentation: { standard: 6, medium: 4, compact: 3 },
};

export const ACTIVE_SPEAKER_TIMING = {
  enterMs: 800,
  minHoldMs: 2_500,
  silenceReleaseMs: 1_500,
} as const;

export interface ActiveSpeakerSelectionInput {
  nowMs: number;
  currentParticipantId: string | null;
  currentSelectedAtMs: number | null;
  currentSilentSinceMs: number | null;
  candidateParticipantId: string | null;
  candidateSpeakingSinceMs: number | null;
}

export function selectActiveSpeaker(
  input: ActiveSpeakerSelectionInput,
): string | null {
  const candidateQualified =
    input.candidateParticipantId !== null &&
    input.candidateSpeakingSinceMs !== null &&
    input.nowMs - input.candidateSpeakingSinceMs >=
      ACTIVE_SPEAKER_TIMING.enterMs;

  if (input.currentParticipantId === null) {
    return candidateQualified ? input.candidateParticipantId : null;
  }
  if (input.candidateParticipantId === input.currentParticipantId) {
    return input.currentParticipantId;
  }

  const minimumHoldReached =
    input.currentSelectedAtMs !== null &&
    input.nowMs - input.currentSelectedAtMs >= ACTIVE_SPEAKER_TIMING.minHoldMs;
  const silenceReleaseReached =
    input.currentSilentSinceMs !== null &&
    input.nowMs - input.currentSilentSinceMs >=
      ACTIVE_SPEAKER_TIMING.silenceReleaseMs;

  if (!minimumHoldReached || !silenceReleaseReached) {
    return input.currentParticipantId;
  }
  return candidateQualified ? input.candidateParticipantId : null;
}

export function createParticipants(count: FixtureSize): readonly Participant[] {
  return Array.from({ length: count }, (_, index) => {
    const number = index + 1;
    return {
      id: `participant-${number.toString().padStart(2, "0")}`,
      displayName:
        index === 0
          ? "Bạn · Giáo viên"
          : `Học viên ${number.toString().padStart(2, "0")}`,
      stableIndex: index,
      role: index === 0 ? "teacher" : "student",
      isLocal: index === 0,
    } satisfies Participant;
  });
}

function stableParticipants(
  participants: readonly Participant[],
): readonly Participant[] {
  return [...participants].sort(
    (left, right) =>
      left.stableIndex - right.stableIndex || left.id.localeCompare(right.id),
  );
}

function validParticipantId(
  participants: readonly Participant[],
  candidate: string | null,
): string | null {
  return candidate && participants.some(({ id }) => id === candidate)
    ? candidate
    : null;
}

function boundedPage(requestedPage: number, pageCount: number): number {
  if (!Number.isFinite(requestedPage)) {
    return 0;
  }
  return Math.min(Math.max(Math.trunc(requestedPage), 0), pageCount - 1);
}

function uniqueIds(ids: readonly string[]): readonly string[] {
  return [...new Set(ids)];
}

export function projectLayout(input: LayoutInput): LayoutProjection {
  const ordered = stableParticipants(input.participants);
  const pageSize = PAGE_SIZES[input.mode][input.viewport];
  const pinned = validParticipantId(ordered, input.pinnedParticipantId);
  const active = validParticipantId(ordered, input.activeSpeakerId);
  const presenter = validParticipantId(ordered, input.presenterId);

  let focus: LayoutFocus | null = null;
  let pageCandidates = ordered;

  if (input.mode === "active-speaker") {
    const participantId = pinned ?? active ?? ordered[0]?.id ?? null;
    if (participantId) {
      focus = { kind: "participant", participantId };
      pageCandidates = ordered.filter(({ id }) => id !== participantId);
    }
  }

  if (input.mode === "presentation") {
    if (presenter) {
      focus = { kind: "presentation", participantId: presenter };
    } else {
      const participantId = pinned ?? active ?? ordered[0]?.id ?? null;
      if (participantId) {
        focus = { kind: "participant", participantId };
        pageCandidates = ordered.filter(({ id }) => id !== participantId);
      }
    }
  }

  const pageCount = Math.max(1, Math.ceil(pageCandidates.length / pageSize));
  const page = boundedPage(input.requestedPage, pageCount);
  const visibleParticipantIds = pageCandidates
    .slice(page * pageSize, page * pageSize + pageSize)
    .map(({ id }) => id);
  const focusedVideoId =
    focus?.kind === "participant" ? focus.participantId : null;
  const subscribedParticipantIds = uniqueIds([
    ...(focusedVideoId ? [focusedVideoId] : []),
    ...visibleParticipantIds,
  ]);

  return {
    mode: input.mode,
    focus,
    page,
    pageCount,
    pageSize,
    visibleParticipantIds,
    subscribedParticipantIds,
  };
}

export type HandEventKind = "raise" | "lower";

export interface HandEvent {
  eventId: string;
  participantId: string;
  serverSequence: number;
  kind: HandEventKind;
}

export interface RaisedHand {
  participantId: string;
  raisedSequence: number;
}

export interface HandProjection {
  queue: readonly RaisedHand[];
  lastServerSequence: number;
  duplicateEventCount: number;
}

export function projectHandQueue(events: readonly HandEvent[]): HandProjection {
  const ordered = [...events].sort(
    (left, right) =>
      left.serverSequence - right.serverSequence ||
      left.eventId.localeCompare(right.eventId),
  );
  const seen = new Set<string>();
  const raised = new Map<string, RaisedHand>();
  let duplicateEventCount = 0;
  let lastServerSequence = 0;

  for (const event of ordered) {
    if (seen.has(event.eventId)) {
      duplicateEventCount += 1;
      continue;
    }
    seen.add(event.eventId);
    lastServerSequence = Math.max(lastServerSequence, event.serverSequence);

    if (event.kind === "raise") {
      if (!raised.has(event.participantId)) {
        raised.set(event.participantId, {
          participantId: event.participantId,
          raisedSequence: event.serverSequence,
        });
      }
    } else {
      raised.delete(event.participantId);
    }
  }

  return {
    queue: [...raised.values()].sort(
      (left, right) =>
        left.raisedSequence - right.raisedSequence ||
        left.participantId.localeCompare(right.participantId),
    ),
    lastServerSequence,
    duplicateEventCount,
  };
}

export const REACTION_ALLOWLIST = ["👍", "👏", "❤️", "🎉", "😂", "😮"] as const;
export type AllowedReaction = (typeof REACTION_ALLOWLIST)[number];

export interface ReactionEvent {
  eventId: string;
  participantId: string;
  emoji: string;
  serverSequence: number;
  acceptedAtMs: number;
}

export interface ReactionPolicy {
  ttlMs: number;
  groupWindowMs: number;
  actorBurstWindowMs: number;
  maxPerActorBurst: number;
  actorSustainedWindowMs: number;
  maxPerActorSustained: number;
  roomWindowMs: number;
  maxPerRoom: number;
  maxVisibleClusters: number;
}

export const REACTION_POLICY: ReactionPolicy = {
  ttlMs: 10_000,
  groupWindowMs: 750,
  actorBurstWindowMs: 5_000,
  maxPerActorBurst: 3,
  actorSustainedWindowMs: 60_000,
  maxPerActorSustained: 20,
  roomWindowMs: 5_000,
  maxPerRoom: 100,
  maxVisibleClusters: 3,
};

export type ReactionRejectionReason =
  | "duplicate"
  | "not-allowed"
  | "future-event"
  | "actor-burst-rate-limit"
  | "actor-sustained-rate-limit"
  | "room-rate-limit";

export interface ReactionRejection {
  eventId: string;
  reason: ReactionRejectionReason;
}

export interface ReactionCluster {
  emoji: AllowedReaction;
  count: number;
  participantIds: readonly string[];
  firstAcceptedAtMs: number;
  lastAcceptedAtMs: number;
  expiresAtMs: number;
}

export interface ReactionProjection {
  acceptedEventIds: readonly string[];
  clusters: readonly ReactionCluster[];
  rejections: readonly ReactionRejection[];
}

interface MutableReactionCluster {
  emoji: AllowedReaction;
  count: number;
  participantIds: string[];
  firstAcceptedAtMs: number;
  lastAcceptedAtMs: number;
  expiresAtMs: number;
}

function isAllowedReaction(value: string): value is AllowedReaction {
  return (REACTION_ALLOWLIST as readonly string[]).includes(value);
}

function withinRateWindow(
  timestamps: readonly number[],
  currentTimestamp: number,
  windowMs: number,
): readonly number[] {
  return timestamps.filter(
    (timestamp) =>
      timestamp > currentTimestamp - windowMs && timestamp <= currentTimestamp,
  );
}

export function projectReactions(
  events: readonly ReactionEvent[],
  nowMs: number,
  policy: ReactionPolicy = REACTION_POLICY,
): ReactionProjection {
  const ordered = [...events].sort(
    (left, right) =>
      left.serverSequence - right.serverSequence ||
      left.eventId.localeCompare(right.eventId),
  );
  const seen = new Set<string>();
  const acceptedEvents: ReactionEvent[] = [];
  const actorAcceptedTimes = new Map<string, number[]>();
  let roomAcceptedTimes: number[] = [];
  const rejections: ReactionRejection[] = [];

  for (const event of ordered) {
    if (seen.has(event.eventId)) {
      rejections.push({ eventId: event.eventId, reason: "duplicate" });
      continue;
    }
    seen.add(event.eventId);

    if (!isAllowedReaction(event.emoji)) {
      rejections.push({ eventId: event.eventId, reason: "not-allowed" });
      continue;
    }
    if (event.acceptedAtMs > nowMs) {
      rejections.push({ eventId: event.eventId, reason: "future-event" });
      continue;
    }

    const actorSustainedTimes = withinRateWindow(
      actorAcceptedTimes.get(event.participantId) ?? [],
      event.acceptedAtMs,
      policy.actorSustainedWindowMs,
    );
    const actorBurstTimes = withinRateWindow(
      actorSustainedTimes,
      event.acceptedAtMs,
      policy.actorBurstWindowMs,
    );
    if (actorBurstTimes.length >= policy.maxPerActorBurst) {
      rejections.push({
        eventId: event.eventId,
        reason: "actor-burst-rate-limit",
      });
      continue;
    }
    if (actorSustainedTimes.length >= policy.maxPerActorSustained) {
      rejections.push({
        eventId: event.eventId,
        reason: "actor-sustained-rate-limit",
      });
      continue;
    }

    roomAcceptedTimes = [
      ...withinRateWindow(
        roomAcceptedTimes,
        event.acceptedAtMs,
        policy.roomWindowMs,
      ),
    ];
    if (roomAcceptedTimes.length >= policy.maxPerRoom) {
      rejections.push({ eventId: event.eventId, reason: "room-rate-limit" });
      continue;
    }

    acceptedEvents.push(event);
    actorAcceptedTimes.set(event.participantId, [
      ...actorSustainedTimes,
      event.acceptedAtMs,
    ]);
    roomAcceptedTimes.push(event.acceptedAtMs);
  }

  const liveEvents = acceptedEvents.filter(
    (event) => event.acceptedAtMs + policy.ttlMs > nowMs,
  );
  const clusters: MutableReactionCluster[] = [];

  for (const event of liveEvents) {
    if (!isAllowedReaction(event.emoji)) {
      continue;
    }
    const matchingCluster = [...clusters]
      .reverse()
      .find(
        (cluster) =>
          cluster.emoji === event.emoji &&
          event.acceptedAtMs - cluster.lastAcceptedAtMs <= policy.groupWindowMs,
      );

    if (matchingCluster) {
      matchingCluster.count += 1;
      matchingCluster.lastAcceptedAtMs = event.acceptedAtMs;
      matchingCluster.expiresAtMs = event.acceptedAtMs + policy.ttlMs;
      if (!matchingCluster.participantIds.includes(event.participantId)) {
        matchingCluster.participantIds.push(event.participantId);
      }
    } else {
      clusters.push({
        emoji: event.emoji,
        count: 1,
        participantIds: [event.participantId],
        firstAcceptedAtMs: event.acceptedAtMs,
        lastAcceptedAtMs: event.acceptedAtMs,
        expiresAtMs: event.acceptedAtMs + policy.ttlMs,
      });
    }
  }

  const visibleClusters = clusters
    .sort(
      (left, right) =>
        left.lastAcceptedAtMs - right.lastAcceptedAtMs ||
        left.emoji.localeCompare(right.emoji),
    )
    .slice(-policy.maxVisibleClusters)
    .map((cluster) => ({
      ...cluster,
      participantIds: [...cluster.participantIds],
    }));

  return {
    acceptedEventIds: acceptedEvents.map(({ eventId }) => eventId),
    clusters: visibleClusters,
    rejections,
  };
}

export const EFFECT_CHOICES = [
  "none",
  "blur",
  "studio",
  "classroom",
  "forest",
] as const;
export type EffectChoice = (typeof EFFECT_CHOICES)[number];
export type DegradationLevel = 0 | 1 | 2 | 3;

export interface MediaPresentationDecision {
  requestedEffect: EffectChoice;
  effectiveEffect: EffectChoice;
  degradationLevel: DegradationLevel;
  videoProfile: "720p/30fps" | "360p/15fps" | "audio-only";
  reason:
    | "full-capability"
    | "effect-capability-unavailable"
    | "effect-disabled-for-budget"
    | "video-degraded"
    | "camera-disabled";
}

export function resolveMediaPresentation(
  requestedEffect: EffectChoice,
  requestedDegradationLevel: DegradationLevel,
  effectCapabilityEligible: boolean,
): MediaPresentationDecision {
  const capabilityLevel: DegradationLevel =
    requestedEffect !== "none" && !effectCapabilityEligible ? 1 : 0;
  const level = Math.max(
    requestedDegradationLevel,
    capabilityLevel,
  ) as DegradationLevel;

  if (level === 3) {
    return {
      requestedEffect,
      effectiveEffect: "none",
      degradationLevel: level,
      videoProfile: "audio-only",
      reason: "camera-disabled",
    };
  }
  if (level === 2) {
    return {
      requestedEffect,
      effectiveEffect: "none",
      degradationLevel: level,
      videoProfile: "360p/15fps",
      reason: "video-degraded",
    };
  }
  if (level === 1) {
    return {
      requestedEffect,
      effectiveEffect: "none",
      degradationLevel: level,
      videoProfile: "720p/30fps",
      reason: effectCapabilityEligible
        ? "effect-disabled-for-budget"
        : "effect-capability-unavailable",
    };
  }
  return {
    requestedEffect,
    effectiveEffect: requestedEffect,
    degradationLevel: level,
    videoProfile: "720p/30fps",
    reason: "full-capability",
  };
}
