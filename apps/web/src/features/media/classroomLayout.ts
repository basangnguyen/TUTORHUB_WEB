export const P4_LAYOUT_FIXTURE_SIZES = [2, 5, 25, 50] as const;

export type P4LayoutFixtureSize = (typeof P4_LAYOUT_FIXTURE_SIZES)[number];

export const CLASSROOM_LAYOUT_MODES = [
  "grid",
  "active-speaker",
  "presentation",
] as const;

export type ClassroomLayoutMode = (typeof CLASSROOM_LAYOUT_MODES)[number];

export const ACTIVE_SPEAKER_TIMING = {
  enterMs: 800,
  minHoldMs: 2_500,
  silenceReleaseMs: 1_500,
} as const;

export const MAX_CLASSROOM_RAIL_ITEMS = 6;

const COMPACT_GRID_MAX_WIDTH = 767;
const MEDIUM_GRID_MAX_WIDTH = 1_199;

export interface ClassroomLayoutItem {
  readonly id: string;
  readonly sequence: number;
  readonly isLocal?: boolean;
}

export interface ClassroomPage<T> {
  readonly items: readonly T[];
  readonly page: number;
  readonly pageCount: number;
  readonly capacity: number;
}

export interface ClassroomLayoutStage<T extends ClassroomLayoutItem> {
  readonly kind: "participant" | "presentation";
  readonly item: T;
}

export interface ClassroomLayoutProjection<T extends ClassroomLayoutItem> {
  readonly mode: ClassroomLayoutMode;
  readonly stage: ClassroomLayoutStage<T> | null;
  readonly items: readonly T[];
  readonly page: number;
  readonly pageCount: number;
  readonly capacity: number;
  readonly subscribedVideoItemIds: readonly string[];
}

export interface ProjectClassroomLayoutInput<T extends ClassroomLayoutItem> {
  readonly items: readonly T[];
  readonly mode: ClassroomLayoutMode;
  readonly width: number;
  readonly requestedPage: number;
  readonly activeSpeakerId: string | null;
  readonly pinnedParticipantId: string | null;
  readonly presenterId: string | null;
}

export interface ClassroomLayoutState {
  readonly mode: ClassroomLayoutMode;
  readonly requestedPage: number;
  readonly pinnedParticipantId: string | null;
  readonly presenterId: string | null;
  readonly focusTargetId: string | null;
  readonly presentationRestore: PresentationRestoreSnapshot | null;
}

export interface PresentationRestoreSnapshot {
  readonly mode: Exclude<ClassroomLayoutMode, "presentation">;
  readonly requestedPage: number;
  readonly pinnedParticipantId: string | null;
  readonly focusTargetId: string | null;
}

export interface PresentationRestoreResult {
  readonly state: ClassroomLayoutState;
  readonly restoredExactState: boolean;
}

export interface ActiveSpeakerSnapshot {
  readonly selectedId: string | null;
  readonly selectedAtMs: number | null;
  readonly silentSinceMs: number | null;
  readonly candidateId: string | null;
  readonly candidateSinceMs: number | null;
}

type ClassroomClock = () => number;

/**
 * Product grid capacity for the current CSS-pixel width. It intentionally
 * stays independent from provider layout heuristics.
 */
export function getGridCapacity(width: number): 4 | 6 | 12 {
  if (!Number.isFinite(width) || width <= COMPACT_GRID_MAX_WIDTH) {
    return 4;
  }
  if (width <= MEDIUM_GRID_MAX_WIDTH) {
    return 6;
  }
  return 12;
}

export function clampPage(page: number, pageCount: number): number {
  const boundedPageCount = Math.max(1, Math.trunc(finiteOr(pageCount, 1)));
  const requestedPage = Math.trunc(finiteOr(page, 0));
  return Math.min(Math.max(requestedPage, 0), boundedPageCount - 1);
}

export function paginateItems<T>(
  items: readonly T[],
  page: number,
  capacity: number,
): ClassroomPage<T> {
  const boundedCapacity = positiveInteger(capacity);
  const pageCount = Math.max(1, Math.ceil(items.length / boundedCapacity));
  const boundedPage = clampPage(page, pageCount);
  const start = boundedPage * boundedCapacity;
  return {
    items: items.slice(start, start + boundedCapacity),
    page: boundedPage,
    pageCount,
    capacity: boundedCapacity,
  };
}

export function getBoundedRail<T extends { readonly id: string }>(
  items: readonly T[],
  stageId: string | null,
  requestedLimit = MAX_CLASSROOM_RAIL_ITEMS,
): readonly T[] {
  const limit = Math.min(
    positiveInteger(requestedLimit),
    MAX_CLASSROOM_RAIL_ITEMS,
  );
  const seen = new Set<string>();
  const rail: T[] = [];

  for (const item of items) {
    if (!validItemId(item.id) || item.id === stageId || seen.has(item.id)) {
      continue;
    }
    seen.add(item.id);
    rail.push(item);
    if (rail.length === limit) {
      break;
    }
  }
  return rail;
}

export function getRailCapacity(
  width: number,
  mode: Exclude<ClassroomLayoutMode, "grid">,
): 3 | 4 | 5 | 6 {
  const gridCapacity = getGridCapacity(width);
  if (gridCapacity === 12) {
    return 6;
  }
  if (gridCapacity === 6) {
    return mode === "active-speaker" ? 5 : 4;
  }
  return 3;
}

export function projectClassroomLayout<T extends ClassroomLayoutItem>(
  input: ProjectClassroomLayoutInput<T>,
): ClassroomLayoutProjection<T> {
  const ordered = stableLayoutItems(input.items);
  const pinned = findItem(ordered, input.pinnedParticipantId);
  const activeSpeaker = findItem(ordered, input.activeSpeakerId);
  const presenter = findItem(ordered, input.presenterId);

  let stage: ClassroomLayoutStage<T> | null = null;
  let pageCandidates = ordered;
  let capacity: number = getGridCapacity(input.width);

  if (input.mode === "active-speaker") {
    const stageItem = pinned ?? activeSpeaker ?? ordered[0] ?? null;
    if (stageItem) {
      stage = { kind: "participant", item: stageItem };
      pageCandidates = ordered.filter(({ id }) => id !== stageItem.id);
    }
    capacity = getRailCapacity(input.width, "active-speaker");
  }

  if (input.mode === "presentation") {
    const stageItem =
      presenter ?? pinned ?? activeSpeaker ?? ordered[0] ?? null;
    if (stageItem) {
      stage = {
        kind: presenter ? "presentation" : "participant",
        item: stageItem,
      };
      pageCandidates = ordered.filter(({ id }) => id !== stageItem.id);
    }
    capacity = getRailCapacity(input.width, "presentation");
  }

  const page = paginateItems(pageCandidates, input.requestedPage, capacity);
  const subscribedVideoItemIds = uniqueIds([
    ...(stage ? [stage.item.id] : []),
    ...page.items.map(({ id }) => id),
  ]);

  return {
    mode: input.mode,
    stage,
    items: page.items,
    page: page.page,
    pageCount: page.pageCount,
    capacity: page.capacity,
    subscribedVideoItemIds,
  };
}

export function enterPresentation(
  state: ClassroomLayoutState,
  presenterId: string,
): ClassroomLayoutState {
  if (!validItemId(presenterId)) {
    return state;
  }

  const presentationRestore =
    state.presentationRestore ??
    ({
      mode: state.mode === "presentation" ? "grid" : state.mode,
      requestedPage: state.requestedPage,
      pinnedParticipantId: state.pinnedParticipantId,
      focusTargetId: state.focusTargetId,
    } satisfies PresentationRestoreSnapshot);

  return {
    ...state,
    mode: "presentation",
    requestedPage: 0,
    presenterId,
    presentationRestore,
  };
}

export function restorePresentation<T extends ClassroomLayoutItem>(
  state: ClassroomLayoutState,
  items: readonly T[],
  width: number,
  layoutControlFocusTargetId = "classroom-layout-controls",
): PresentationRestoreResult {
  const ordered = stableLayoutItems(items);
  const previous = state.presentationRestore;
  const previousPinExists = Boolean(
    !previous?.pinnedParticipantId ||
    findItem(ordered, previous.pinnedParticipantId),
  );

  if (previous && previousPinExists) {
    const projection = projectClassroomLayout({
      items: ordered,
      mode: previous.mode,
      width,
      requestedPage: previous.requestedPage,
      activeSpeakerId: null,
      pinnedParticipantId: previous.pinnedParticipantId,
      presenterId: null,
    });
    return {
      state: {
        mode: previous.mode,
        requestedPage: projection.page,
        pinnedParticipantId: previous.pinnedParticipantId,
        presenterId: null,
        focusTargetId: previous.focusTargetId ?? layoutControlFocusTargetId,
        presentationRestore: null,
      },
      restoredExactState: projection.page === previous.requestedPage,
    };
  }

  const gridCapacity = getGridCapacity(width);
  const localIndex = ordered.findIndex(({ isLocal }) => isLocal === true);
  const fallbackPage =
    localIndex < 0 ? 0 : Math.floor(localIndex / gridCapacity);
  return {
    state: {
      mode: "grid",
      requestedPage: fallbackPage,
      pinnedParticipantId: null,
      presenterId: null,
      focusTargetId: layoutControlFocusTargetId,
      presentationRestore: null,
    },
    restoredExactState: false,
  };
}

export function createLayoutFixture(
  count: P4LayoutFixtureSize,
): readonly ClassroomLayoutItem[] {
  return Array.from({ length: count }, (_, index) => ({
    id: `participant-${String(index + 1).padStart(2, "0")}`,
    sequence: index,
    isLocal: index === 0,
  }));
}

/**
 * Deterministic active-speaker selector. `observe` accepts an explicit
 * monotonic timestamp for fake-clock tests; a clock can also be injected for
 * the production controller.
 */
export class ActiveSpeakerHysteresis {
  readonly #clock: ClassroomClock;
  #selectedId: string | null = null;
  #selectedAtMs: number | null = null;
  #silentSinceMs: number | null = null;
  #candidateId: string | null = null;
  #candidateSinceMs: number | null = null;
  #lastObservedAtMs: number | null = null;

  constructor(clock: ClassroomClock = defaultClock) {
    this.#clock = clock;
  }

  get selectedId(): string | null {
    return this.#selectedId;
  }

  observe(candidateId: string | null, nowMs = this.#clock()): string | null {
    const now = this.#monotonicTime(nowMs);
    const candidate = validItemId(candidateId) ? candidateId : null;

    if (candidate !== this.#candidateId) {
      this.#candidateId = candidate;
      this.#candidateSinceMs = candidate === null ? null : now;
    }

    if (this.#selectedId === null) {
      if (this.#candidateQualified(now)) {
        this.#select(candidate, now);
      }
      return this.#selectedId;
    }

    if (candidate === this.#selectedId) {
      this.#silentSinceMs = null;
      return this.#selectedId;
    }

    this.#silentSinceMs ??= now;
    const minimumHoldReached =
      this.#selectedAtMs !== null &&
      now - this.#selectedAtMs >= ACTIVE_SPEAKER_TIMING.minHoldMs;
    const silenceReleaseReached =
      now - this.#silentSinceMs >= ACTIVE_SPEAKER_TIMING.silenceReleaseMs;

    if (!minimumHoldReached || !silenceReleaseReached) {
      return this.#selectedId;
    }

    if (this.#candidateQualified(now)) {
      this.#select(candidate, now);
    } else {
      this.#select(null, now);
    }
    return this.#selectedId;
  }

  reset(selectedId: string | null = null, nowMs = this.#clock()): void {
    const now = this.#monotonicTime(nowMs);
    this.#candidateId = null;
    this.#candidateSinceMs = null;
    this.#silentSinceMs = null;
    this.#selectedId = validItemId(selectedId) ? selectedId : null;
    this.#selectedAtMs = this.#selectedId === null ? null : now;
  }

  snapshot(): ActiveSpeakerSnapshot {
    return {
      selectedId: this.#selectedId,
      selectedAtMs: this.#selectedAtMs,
      silentSinceMs: this.#silentSinceMs,
      candidateId: this.#candidateId,
      candidateSinceMs: this.#candidateSinceMs,
    };
  }

  #candidateQualified(nowMs: number): boolean {
    return Boolean(
      this.#candidateId !== null &&
      this.#candidateSinceMs !== null &&
      nowMs - this.#candidateSinceMs >= ACTIVE_SPEAKER_TIMING.enterMs,
    );
  }

  #select(participantId: string | null, nowMs: number): void {
    this.#selectedId = participantId;
    this.#selectedAtMs = participantId === null ? null : nowMs;
    this.#silentSinceMs = null;
  }

  #monotonicTime(value: number): number {
    if (!Number.isFinite(value)) {
      throw new TypeError("Active-speaker time must be finite.");
    }
    const now =
      this.#lastObservedAtMs === null
        ? value
        : Math.max(value, this.#lastObservedAtMs);
    this.#lastObservedAtMs = now;
    return now;
  }
}

function defaultClock(): number {
  return globalThis.performance?.now() ?? Date.now();
}

function finiteOr(value: number, fallback: number): number {
  return Number.isFinite(value) ? value : fallback;
}

function positiveInteger(value: number): number {
  return Math.max(1, Math.trunc(finiteOr(value, 1)));
}

function validItemId(value: string | null | undefined): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function stableLayoutItems<T extends ClassroomLayoutItem>(
  items: readonly T[],
): readonly T[] {
  const seen = new Set<string>();
  return [...items]
    .filter(({ id }) => validItemId(id))
    .sort(
      (left, right) =>
        finiteOr(left.sequence, Number.MAX_SAFE_INTEGER) -
          finiteOr(right.sequence, Number.MAX_SAFE_INTEGER) ||
        left.id.localeCompare(right.id),
    )
    .filter(({ id }) => {
      if (seen.has(id)) {
        return false;
      }
      seen.add(id);
      return true;
    });
}

function findItem<T extends ClassroomLayoutItem>(
  items: readonly T[],
  id: string | null,
): T | null {
  if (!validItemId(id)) {
    return null;
  }
  return items.find((item) => item.id === id) ?? null;
}

function uniqueIds(ids: readonly string[]): readonly string[] {
  return [...new Set(ids)];
}
