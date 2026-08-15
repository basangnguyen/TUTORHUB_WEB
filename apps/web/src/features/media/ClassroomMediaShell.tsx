import {
  ParticipantTile,
  VideoTrack,
  isTrackReference,
  useLocalParticipant,
  useMediaDeviceSelect,
  useParticipants,
  useSpeakingParticipants,
  useTracks,
  type TrackReferenceOrPlaceholder,
} from "@livekit/components-react";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerTitle,
  DrawerTrigger,
  IconButton,
  Menu,
  MenuContent,
  MenuItem,
  MenuTrigger,
} from "@tutorhub/ui";
import {
  Camera,
  CameraOff,
  ChevronLeft,
  ChevronRight,
  Grid2X2,
  Hand,
  LogOut,
  Mic,
  MicOff,
  Pin,
  PinOff,
  Presentation,
  ScreenShare,
  ScreenShareOff,
  Settings2,
  Smile,
  MessageCircle,
  UserRound,
  Users,
} from "lucide-react";
import {
  ConnectionQuality,
  ParticipantEvent,
  RemoteTrackPublication,
  Track,
  VideoQuality,
} from "livekit-client";
import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { useI18n, type TranslationKey } from "../../app/i18n";
import {
  ClassroomDegradationController,
  projectClassroomDegradation,
  type ClassroomDegradationSignal,
  type ClassroomDegradationStage,
} from "./classroomDegradation";
import {
  ActiveSpeakerHysteresis,
  enterPresentation,
  getGridCapacity,
  getRailCapacity,
  projectClassroomLayout,
  restorePresentation,
  type ClassroomLayoutItem,
  type ClassroomLayoutMode,
  type ClassroomLayoutState,
} from "./classroomLayout";
import {
  CLASSROOM_REACTION_TYPES,
  type ClassroomReactionType,
  type ClassroomSignalProjection,
} from "./classroomSignals";
import {
  ClassroomModerationControls,
  ClassroomParticipantModerationMenu,
  type ClassroomModerationControlsModel,
} from "./ClassroomModerationControls";

export type ClassroomConnectionStatus =
  "connecting" | "connected" | "reconnecting" | "disconnected" | "failed";

export interface ClassroomMediaShellProps {
  canPublishCameraMicrophone: boolean;
  canShareScreen: boolean;
  canSubscribe: boolean;
  connectionStatus: ClassroomConnectionStatus;
  controlAbortSignal?: AbortSignal;
  chat?: ReactNode;
  lobby?: ReactNode;
  moderation?: ClassroomModerationControlsModel;
  signals?: ClassroomSignalControls;
  onLeave: () => void;
  onTerminalMediaCleanup: () => Promise<void>;
}

export interface ClassroomSignalControls {
  readonly error: boolean;
  readonly loading: boolean;
  readonly mutating: boolean;
  readonly projection: ClassroomSignalProjection | null;
  readonly refreshing: boolean;
  readonly onLowerAllHands: () => Promise<void>;
  readonly onLowerHand: (participantKey: string) => Promise<void>;
  readonly onResync: () => Promise<unknown>;
  readonly onSendReaction: (reaction: ClassroomReactionType) => Promise<void>;
  readonly onToggleHand: () => Promise<void>;
}

interface MediaLayoutItem extends ClassroomLayoutItem {
  readonly displayName: string;
  readonly trackRef: TrackReferenceOrPlaceholder;
}

interface SessionParticipantProjection {
  readonly id: string;
  readonly sequence: number;
}

interface SessionParticipantProjectionSnapshot {
  readonly entries: WeakMap<object, SessionParticipantProjection>;
  readonly revision: number;
}

interface SessionParticipantProjectionStore {
  readonly getSnapshot: () => SessionParticipantProjectionSnapshot;
  readonly observe: (trackRefs: readonly TrackReferenceOrPlaceholder[]) => void;
  readonly subscribe: (listener: () => void) => () => void;
}

interface MediaTileProps {
  attachVideo: boolean;
  item: MediaLayoutItem;
  pinned: boolean;
  trackRef?: TrackReferenceOrPlaceholder;
  variant: "grid" | "rail" | "stage";
  onTogglePin: (item: MediaLayoutItem, focusTarget: HTMLElement) => void;
}

type ToolbarControlKey =
  | "microphone"
  | "camera"
  | "screen-share"
  | "devices"
  | "hand"
  | "reaction"
  | "chat"
  | "roster"
  | "layout-grid"
  | "layout-active-speaker"
  | "layout-presentation";

const initialLayoutState: ClassroomLayoutState = {
  mode: "grid",
  requestedPage: 0,
  pinnedParticipantId: null,
  presenterId: null,
  focusTargetId: null,
  presentationRestore: null,
};

const layoutModes: readonly ClassroomLayoutMode[] = [
  "grid",
  "active-speaker",
  "presentation",
];

export function ClassroomMediaShell({
  canPublishCameraMicrophone,
  canShareScreen,
  canSubscribe,
  connectionStatus,
  controlAbortSignal,
  chat,
  lobby,
  moderation,
  signals,
  onLeave,
  onTerminalMediaCleanup,
}: ClassroomMediaShellProps) {
  const { t } = useI18n();
  const shellRef = useRef<HTMLDivElement>(null);
  const toolbarRef = useRef<HTMLDivElement>(null);
  const speakerController = useRef(new ActiveSpeakerHysteresis());
  const previousPresenterID = useRef<string | null>(null);
  const managedRemoteVideos = useRef(new Set<RemoteTrackPublication>());
  const managedRemoteAudio = useRef(new Set<RemoteTrackPublication>());
  const degradationController = useRef(new ClassroomDegradationController());
  const controlLifecycle = useRef({ active: true, generation: 0 });
  const focusTimer = useRef<number | null>(null);
  const [width, setWidth] = useState(1_280);
  const [layoutState, setLayoutState] =
    useState<ClassroomLayoutState>(initialLayoutState);
  const [activeSpeakerID, setActiveSpeakerID] = useState<string | null>(null);
  const [devicePanelOpen, setDevicePanelOpen] = useState(false);
  const [railOpen, setRailOpen] = useState(false);
  const [rosterOpen, setRosterOpen] = useState(false);
  const [chatOpen, setChatOpen] = useState(false);
  const [signalFeedback, setSignalFeedback] = useState<TranslationKey | null>(
    null,
  );
  const [reactionAnnouncement, setReactionAnnouncement] = useState("");
  const lastReactionAnnouncementAt = useRef(0);
  const reactionAnnouncementTimer = useRef<number | null>(null);
  const [pendingControl, setPendingControl] = useState<string | null>(null);
  const [mediaError, setMediaError] = useState(false);
  const [controlsTerminated, setControlsTerminated] = useState(
    controlAbortSignal?.aborted ?? false,
  );
  const [toolbarFocusKey, setToolbarFocusKey] = useState<ToolbarControlKey>(
    () =>
      canPublishCameraMicrophone
        ? "microphone"
        : canShareScreen
          ? "screen-share"
          : "devices",
  );
  const cameraTrackRefs = useTracks(
    [{ source: Track.Source.Camera, withPlaceholder: true }],
    { onlySubscribed: false },
  );
  const liveKitParticipants = useParticipants();
  const screenShareTrackRefs = useTracks([Track.Source.ScreenShare], {
    onlySubscribed: false,
  });
  const audioTrackRefs = useTracks(
    [Track.Source.Microphone, Track.Source.ScreenShareAudio],
    { onlySubscribed: false },
  );
  const [sessionParticipantProjectionStore] =
    useState<SessionParticipantProjectionStore>(() =>
      createSessionParticipantProjectionStore(cameraTrackRefs),
    );
  const sessionParticipantProjection = useSyncExternalStore(
    sessionParticipantProjectionStore.subscribe,
    sessionParticipantProjectionStore.getSnapshot,
    sessionParticipantProjectionStore.getSnapshot,
  );
  const speakingParticipants = useSpeakingParticipants();
  const {
    isCameraEnabled,
    isMicrophoneEnabled,
    isScreenShareEnabled,
    localParticipant,
  } = useLocalParticipant();
  const [localConnectionQuality, setLocalConnectionQuality] =
    useState<ConnectionQuality>(localParticipant.connectionQuality);
  const [degradationStage, setDegradationStage] =
    useState<ClassroomDegradationStage>("normal");

  const participantAttributeRevision = liveKitParticipants
    .map(
      (participant) =>
        participant.attributes?.["tutorhub.participant_key"] ?? "",
    )
    .join("\u0000");

  useLayoutEffect(() => {
    sessionParticipantProjectionStore.observe(cameraTrackRefs);
  }, [cameraTrackRefs, sessionParticipantProjectionStore]);

  const scheduleElementFocus = useCallback(
    (elementID: string | null, fallbackElementID?: string) => {
      if (!elementID && !fallbackElementID) return;
      if (focusTimer.current !== null) {
        globalThis.clearTimeout(focusTimer.current);
      }
      focusTimer.current = globalThis.setTimeout(() => {
        focusTimer.current = null;
        const target = elementID ? document.getElementById(elementID) : null;
        const fallback = fallbackElementID
          ? document.getElementById(fallbackElementID)
          : null;
        (target ?? fallback)?.focus();
      }, 0);
    },
    [],
  );

  const handleDeviceError = useCallback(() => {
    if (!controlLifecycle.current.active) return;
    controlLifecycle.current.generation += 1;
    setPendingControl(null);
    setMediaError(true);
  }, []);

  useEffect(() => {
    const lifecycle = controlLifecycle.current;
    lifecycle.active = true;
    return () => {
      lifecycle.active = false;
      lifecycle.generation += 1;
    };
  }, []);

  useEffect(
    () => () => {
      if (focusTimer.current !== null) {
        globalThis.clearTimeout(focusTimer.current);
        focusTimer.current = null;
      }
      if (reactionAnnouncementTimer.current !== null) {
        globalThis.clearTimeout(reactionAnnouncementTimer.current);
        reactionAnnouncementTimer.current = null;
      }
    },
    [],
  );

  const reactionSummaryKey =
    signals?.projection?.reactions.summary
      .map(({ reaction, count }) => `${reaction}:${count}`)
      .join("|") ?? "";
  const reactionTotal =
    signals?.projection?.reactions.summary.reduce(
      (count, item) => count + item.count,
      0,
    ) ?? 0;
  useEffect(() => {
    if (reactionTotal === 0) {
      return undefined;
    }
    const now = Date.now();
    const delay = Math.max(
      0,
      2_000 - (now - lastReactionAnnouncementAt.current),
    );
    if (reactionAnnouncementTimer.current !== null) {
      globalThis.clearTimeout(reactionAnnouncementTimer.current);
    }
    reactionAnnouncementTimer.current = globalThis.setTimeout(() => {
      reactionAnnouncementTimer.current = null;
      lastReactionAnnouncementAt.current = Date.now();
      setReactionAnnouncement(
        t("media.p406.reactionAnnouncement", { count: reactionTotal }),
      );
    }, delay);
    return () => {
      if (reactionAnnouncementTimer.current !== null) {
        globalThis.clearTimeout(reactionAnnouncementTimer.current);
        reactionAnnouncementTimer.current = null;
      }
    };
  }, [reactionSummaryKey, reactionTotal, t]);

  useEffect(() => {
    if (!controlAbortSignal) return undefined;
    const invalidateControls = () => {
      const lifecycle = controlLifecycle.current;
      lifecycle.active = false;
      lifecycle.generation += 1;
      setPendingControl(null);
      setMediaError(false);
      setDevicePanelOpen(false);
      setControlsTerminated(true);
    };
    if (controlAbortSignal.aborted) {
      invalidateControls();
      return undefined;
    }
    controlAbortSignal.addEventListener("abort", invalidateControls, {
      once: true,
    });
    return () =>
      controlAbortSignal.removeEventListener("abort", invalidateControls);
  }, [controlAbortSignal]);

  useEffect(() => {
    const handleConnectionQuality = (quality: ConnectionQuality) => {
      setLocalConnectionQuality(quality);
    };
    localParticipant.on(
      ParticipantEvent.ConnectionQualityChanged,
      handleConnectionQuality,
    );
    return () => {
      localParticipant.off(
        ParticipantEvent.ConnectionQualityChanged,
        handleConnectionQuality,
      );
    };
  }, [localParticipant]);

  const allCameraItems = useMemo(() => {
    void participantAttributeRevision;
    return signals
      ? authoritativeMediaItems(
          cameraTrackRefs,
          signals.projection?.roster ?? [],
          signals.projection?.self_participant_key ?? null,
        )
      : stableMediaItems(
          cameraTrackRefs,
          sessionParticipantProjection.entries,
          t("media.p405.participantFallback"),
        );
  }, [
    cameraTrackRefs,
    participantAttributeRevision,
    sessionParticipantProjection,
    signals,
    t,
  ]);
  const cameraItems = useMemo(
    () =>
      canSubscribe
        ? allCameraItems
        : allCameraItems.filter(({ trackRef }) => trackRef.participant.isLocal),
    [allCameraItems, canSubscribe],
  );
  const itemByParticipant = useMemo(
    () =>
      new Map(
        allCameraItems.map(
          (item) => [item.trackRef.participant, item] as const,
        ),
      ),
    [allCameraItems],
  );
  const presenterTrackRef = screenShareTrackRefs[0];
  const presenterItem = presenterTrackRef
    ? (itemByParticipant.get(presenterTrackRef.participant) ?? null)
    : null;
  const presenterID = presenterItem?.id ?? null;
  const toolbarControlKeys = useMemo<readonly ToolbarControlKey[]>(
    () => [
      ...(canPublishCameraMicrophone &&
      pendingControl === null &&
      !controlsTerminated
        ? (["microphone", "camera"] as const)
        : []),
      ...(canShareScreen && pendingControl === null && !controlsTerminated
        ? (["screen-share"] as const)
        : []),
      ...(!controlsTerminated ? (["devices"] as const) : []),
      ...(signals?.projection?.viewer_operations.can_raise_hand
        ? (["hand"] as const)
        : []),
      ...(signals?.projection?.viewer_operations.can_send_reaction
        ? (["reaction"] as const)
        : []),
      ...(chat ? (["chat"] as const) : []),
      ...(signals ? (["roster"] as const) : []),
      "layout-grid",
      "layout-active-speaker",
      ...(presenterID ? (["layout-presentation"] as const) : []),
    ],
    [
      canPublishCameraMicrophone,
      canShareScreen,
      chat,
      controlsTerminated,
      pendingControl,
      presenterID,
      signals,
    ],
  );
  const effectiveToolbarFocusKey = toolbarControlKeys.includes(toolbarFocusKey)
    ? toolbarFocusKey
    : toolbarControlKeys[0];
  const toolbarTabIndex = (key: ToolbarControlKey) =>
    effectiveToolbarFocusKey === key ? 0 : -1;

  const candidateSpeaker = speakingParticipants[0];
  const candidateSpeakerID = candidateSpeaker
    ? (itemByParticipant.get(candidateSpeaker)?.id ?? null)
    : null;
  const normalVisibleVideoItems =
    layoutState.mode === "grid"
      ? getGridCapacity(width)
      : 1 +
        getRailCapacity(
          width,
          layoutState.mode === "presentation"
            ? "presentation"
            : "active-speaker",
        );
  useLayoutEffect(() => {
    const element = shellRef.current;
    if (!element) return;

    const updateWidth = () => {
      const nextWidth = element.getBoundingClientRect().width;
      setWidth(nextWidth > 0 ? nextWidth : globalThis.innerWidth || 1_280);
    };
    updateWidth();

    if (typeof ResizeObserver === "undefined") {
      globalThis.addEventListener("resize", updateWidth);
      return () => globalThis.removeEventListener("resize", updateWidth);
    }
    const observer = new ResizeObserver(updateWidth);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const tick = () => {
      const selected = speakerController.current.observe(candidateSpeakerID);
      setActiveSpeakerID((current) =>
        current === selected ? current : selected,
      );
    };
    tick();
    const timer = globalThis.setInterval(tick, 200);
    return () => globalThis.clearInterval(timer);
  }, [candidateSpeakerID]);

  useLayoutEffect(() => {
    const previousPresenter = previousPresenterID.current;
    previousPresenterID.current = presenterID;

    if (presenterID && presenterID !== previousPresenter) {
      const activeElement = document.activeElement;
      const focusTargetId =
        activeElement instanceof HTMLElement &&
        shellRef.current?.contains(activeElement)
          ? activeElement.id || "media-p405-layout-grid"
          : "media-p405-layout-grid";
      setLayoutState((current) =>
        enterPresentation({ ...current, focusTargetId }, presenterID),
      );
      return;
    }

    if (!presenterID && previousPresenter) {
      const restored = restorePresentation(layoutState, cameraItems, width);
      setLayoutState(restored.state);
      scheduleElementFocus(
        restored.state.focusTargetId,
        `media-p405-layout-${restored.state.mode}`,
      );
    }
  }, [cameraItems, layoutState, presenterID, scheduleElementFocus, width]);

  useEffect(() => {
    if (
      layoutState.pinnedParticipantId &&
      !cameraItems.some(({ id }) => id === layoutState.pinnedParticipantId)
    ) {
      const timer = globalThis.setTimeout(
        () =>
          setLayoutState((current) => ({
            ...current,
            pinnedParticipantId: null,
          })),
        0,
      );
      return () => globalThis.clearTimeout(timer);
    }
    return undefined;
  }, [cameraItems, layoutState.pinnedParticipantId]);

  const projection = useMemo(
    () =>
      projectClassroomLayout({
        items: cameraItems,
        mode: layoutState.mode,
        width,
        requestedPage: layoutState.requestedPage,
        activeSpeakerId: activeSpeakerID,
        pinnedParticipantId: layoutState.pinnedParticipantId,
        presenterId: presenterID,
      }),
    [activeSpeakerID, cameraItems, layoutState, presenterID, width],
  );
  const degradationProjection = useMemo(
    () =>
      projectClassroomDegradation(degradationStage, {
        normalVideoSubscriptionLimit: normalVisibleVideoItems,
        hasPresentation: projection.stage?.kind === "presentation",
      }),
    [degradationStage, normalVisibleVideoItems, projection.stage?.kind],
  );

  const attachedCameraVideoItemIDs = useMemo(() => {
    const orderedCameraItemIDs = [
      ...(projection.stage?.kind === "participant"
        ? [projection.stage.item.id]
        : []),
      ...projection.items.map(({ id }) => id),
    ];
    return new Set(
      orderedCameraItemIDs.slice(0, degradationProjection.maxCameraVideoItems),
    );
  }, [
    degradationProjection.maxCameraVideoItems,
    projection.items,
    projection.stage,
  ]);
  const attachPresentationVideo = Boolean(
    degradationProjection.subscribePresentationVideo &&
    projection.stage?.kind === "presentation",
  );

  useEffect(() => {
    if (layoutState.requestedPage === projection.page) return undefined;
    const timer = globalThis.setTimeout(() => {
      setLayoutState((current) =>
        current.requestedPage === projection.page
          ? current
          : { ...current, requestedPage: projection.page },
      );
      scheduleElementFocus("media-p405-pagination");
    }, 0);
    return () => globalThis.clearTimeout(timer);
  }, [layoutState.requestedPage, projection.page, scheduleElementFocus]);

  const stageItem = projection.stage?.item ?? null;
  const stageTrackRef =
    projection.stage?.kind === "presentation" && presenterTrackRef
      ? presenterTrackRef
      : stageItem?.trackRef;
  const isResponsiveDrawer = width < 1_024;

  useEffect(() => {
    const currentPublications = new Set<RemoteTrackPublication>();
    const desiredPublications = new Map<RemoteTrackPublication, VideoQuality>();
    const collect = (
      refs: readonly TrackReferenceOrPlaceholder[],
      isPresentation: boolean,
    ) => {
      for (const trackRef of refs) {
        const publication = isTrackReference(trackRef)
          ? trackRef.publication
          : undefined;
        if (!isRemoteTrackPublication(publication)) continue;
        currentPublications.add(publication);
        managedRemoteVideos.current.add(publication);
        const item = itemByParticipant.get(trackRef.participant);
        const visible = Boolean(
          canSubscribe &&
          item &&
          (isPresentation
            ? attachPresentationVideo &&
              projection.stage?.kind === "presentation" &&
              item.id === projection.stage.item.id
            : attachedCameraVideoItemIDs.has(item.id)),
        );
        if (visible) {
          const quality =
            !isPresentation &&
            degradationProjection.remoteCameraQuality === "low"
              ? VideoQuality.LOW
              : VideoQuality.HIGH;
          desiredPublications.set(publication, quality);
        }
      }
    };

    collect(cameraTrackRefs, false);
    collect(screenShareTrackRefs, true);

    // Keep the hard bound true throughout a page/share transition: retire old
    // subscriptions before enabling any replacement, regardless of provider
    // publication order.
    for (const publication of managedRemoteVideos.current) {
      if (!desiredPublications.has(publication) && publication.isDesired) {
        publication.setSubscribed(false);
      }
      if (!currentPublications.has(publication)) {
        managedRemoteVideos.current.delete(publication);
      }
    }
    for (const [publication, quality] of desiredPublications) {
      if (!publication.isDesired) publication.setSubscribed(true);
      if (publication.videoQuality !== quality) {
        publication.setVideoQuality(quality);
      }
    }
  }, [
    attachPresentationVideo,
    attachedCameraVideoItemIDs,
    cameraTrackRefs,
    canSubscribe,
    degradationProjection.remoteCameraQuality,
    itemByParticipant,
    projection.stage,
    screenShareTrackRefs,
  ]);

  useEffect(
    () => () => {
      for (const publication of managedRemoteVideos.current) {
        if (publication.isDesired) publication.setSubscribed(false);
      }
      managedRemoteVideos.current.clear();
    },
    [],
  );

  useEffect(() => {
    const controller = degradationController.current;
    const tick = () => {
      const signal = classroomDegradationSignal(
        localConnectionQuality,
        managedRemoteVideos.current,
        document.visibilityState === "visible",
      );
      const nextStage = controller.observe(signal);
      setDegradationStage((current) =>
        current === nextStage ? current : nextStage,
      );
    };
    tick();
    const timer = globalThis.setInterval(tick, 1_000);
    return () => globalThis.clearInterval(timer);
  }, [localConnectionQuality, sessionParticipantProjection]);

  useEffect(() => {
    const currentPublications = new Set<RemoteTrackPublication>();
    for (const trackRef of audioTrackRefs) {
      const publication = isTrackReference(trackRef)
        ? trackRef.publication
        : undefined;
      if (!isRemoteTrackPublication(publication)) continue;
      currentPublications.add(publication);
      managedRemoteAudio.current.add(publication);
      if (publication.isDesired !== canSubscribe) {
        publication.setSubscribed(canSubscribe);
      }
    }
    for (const publication of managedRemoteAudio.current) {
      if (!currentPublications.has(publication)) {
        if (publication.isDesired) publication.setSubscribed(false);
        managedRemoteAudio.current.delete(publication);
      }
    }
  }, [audioTrackRefs, canSubscribe]);

  useEffect(
    () => () => {
      for (const publication of managedRemoteAudio.current) {
        if (publication.isDesired) publication.setSubscribed(false);
      }
      managedRemoteAudio.current.clear();
    },
    [],
  );

  const runControl = useCallback(
    async (name: string, action: () => Promise<unknown>) => {
      const lifecycle = controlLifecycle.current;
      if (!lifecycle.active) return;
      const generation = ++lifecycle.generation;
      setPendingControl(name);
      setMediaError(false);
      try {
        await action();
      } catch {
        if (lifecycle.active && lifecycle.generation === generation) {
          setMediaError(true);
        }
      } finally {
        if (!lifecycle.active) {
          await onTerminalMediaCleanup();
        }
        if (lifecycle.active && lifecycle.generation === generation) {
          setPendingControl(null);
        }
      }
    },
    [onTerminalMediaCleanup],
  );

  useEffect(() => {
    if (
      degradationStage !== "audio-only" ||
      !isCameraEnabled ||
      controlsTerminated
    ) {
      return;
    }
    void runControl("camera", () => localParticipant.setCameraEnabled(false));
  }, [
    controlsTerminated,
    degradationStage,
    isCameraEnabled,
    localParticipant,
    runControl,
  ]);

  const runSignalAction = useCallback(
    async (action: () => Promise<void>, successKey: TranslationKey) => {
      if (!signals || controlsTerminated || signals.mutating) return;
      setSignalFeedback(null);
      try {
        await action();
        if (controlLifecycle.current.active) {
          setSignalFeedback(successKey);
        }
      } catch (error) {
        if (controlLifecycle.current.active) {
          setSignalFeedback(signalMutationErrorKey(error));
        }
      }
    },
    [controlsTerminated, signals],
  );

  const handleLeave = useCallback(() => {
    const lifecycle = controlLifecycle.current;
    lifecycle.active = false;
    lifecycle.generation += 1;
    setPendingControl(null);
    onLeave();
  }, [onLeave]);

  const handleTogglePin = useCallback(
    (item: MediaLayoutItem, focusTarget: HTMLElement) => {
      const targetID = focusTarget.id;
      setLayoutState((current) => ({
        ...current,
        mode: "active-speaker",
        requestedPage: 0,
        pinnedParticipantId:
          current.pinnedParticipantId === item.id ? null : item.id,
        focusTargetId: targetID,
      }));
      scheduleElementFocus(targetID, "media-p405-layout-active-speaker");
    },
    [scheduleElementFocus],
  );

  const handleToolbarKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) {
      return;
    }
    const controls = Array.from(
      toolbarRef.current?.querySelectorAll<HTMLElement>(
        "[data-media-control]:not(:disabled)",
      ) ?? [],
    );
    const currentIndex = controls.indexOf(
      document.activeElement as HTMLElement,
    );
    if (currentIndex < 0 || controls.length === 0) return;
    event.preventDefault();
    const nextIndex =
      event.key === "Home"
        ? 0
        : event.key === "End"
          ? controls.length - 1
          : (currentIndex +
              (event.key === "ArrowRight" ? 1 : -1) +
              controls.length) %
            controls.length;
    const nextControl = controls[nextIndex];
    const nextKey = nextControl?.dataset.mediaControl as
      ToolbarControlKey | undefined;
    if (nextKey) {
      setToolbarFocusKey(nextKey);
    }
    nextControl?.focus();
  };

  const changeLayout = (mode: ClassroomLayoutMode) => {
    setLayoutState((current) => ({
      ...current,
      mode,
      requestedPage: 0,
      presenterId: mode === "presentation" ? presenterID : null,
    }));
  };

  const rail = (
    <ParticipantRail
      attachedVideoItemIDs={attachedCameraVideoItemIDs}
      items={projection.items}
      onTogglePin={handleTogglePin}
      pinnedID={layoutState.pinnedParticipantId}
    />
  );
  const selfHand = signals?.projection?.raised_hands.find(
    ({ participant_key }) =>
      participant_key === signals.projection?.self_participant_key,
  );
  const participantCount =
    signals?.projection?.roster.length ?? cameraItems.length;

  return (
    <div
      aria-busy={connectionStatus === "connecting" || undefined}
      className="media-p405-shell"
      ref={shellRef}
    >
      <header className="media-p405-header">
        <div>
          <h1>{t("media.room.title")}</h1>
          <p aria-live="polite" role="status">
            {t(roomStatusKey(connectionStatus))}
            {" / "}
            {t("media.room.participantCount", { count: participantCount })}
          </p>
        </div>
        <div className="media-p405-header-badges">
          {!canPublishCameraMicrophone && (
            <span>{t("media.room.listenOnly")}</span>
          )}
          <span>{t("media.p405.effectsNone")}</span>
        </div>
      </header>

      {moderation && (
        <ClassroomModerationControls
          controls={moderation}
          disabled={controlsTerminated || connectionStatus !== "connected"}
        />
      )}

      {!canPublishCameraMicrophone && (
        <p className="media-p405-notice">
          {t("media.p405.listenOnlyDescription")}
        </p>
      )}
      {!canSubscribe && (
        <p className="media-p405-notice" role="status">
          {t("media.p405.subscribeRestricted")}
        </p>
      )}
      {degradationStage !== "normal" && (
        <p
          aria-atomic="true"
          aria-live="polite"
          className="media-p405-notice media-p405-degradation-status"
          role="status"
        >
          {t(degradationStatusKey(degradationStage))}
        </p>
      )}
      {signals?.loading && (
        <p className="media-p405-notice" role="status">
          {t("media.p406.loading")}
        </p>
      )}
      {signals?.error && (
        <div className="media-p405-error" role="alert">
          <span>{t("media.p406.loadError")}</span>
          <Button
            onClick={() => void signals.onResync()}
            size="sm"
            variant="quiet"
          >
            {t("media.p406.retry")}
          </Button>
        </div>
      )}
      <p
        aria-atomic="true"
        aria-live="polite"
        className="media-p406-status"
        role="status"
      >
        {signalFeedback ? t(signalFeedback) : ""}
        {selfHand
          ? ` ${t("media.p406.ownHandPosition", {
              position: selfHand.queue_position,
            })}`
          : ""}
      </p>
      <p
        aria-atomic="true"
        aria-live="polite"
        className="media-p406-sr-reactions"
        role="status"
      >
        {reactionTotal > 0 ? reactionAnnouncement : ""}
      </p>
      {mediaError && (
        <div className="media-p405-error" role="alert">
          <span>{t("media.p405.deviceUpdateError")}</span>
          <Button
            onClick={() => setMediaError(false)}
            size="sm"
            variant="quiet"
          >
            {t("media.room.dismiss")}
          </Button>
        </div>
      )}

      <div
        className={`media-p405-classroom media-p405-classroom--${projection.mode}`}
      >
        {signals?.projection && (
          <div aria-hidden="true" className="media-p406-reactions">
            {signals.projection.reactions.clusters.map((cluster) => (
              <span
                className="media-p406-reaction-cluster"
                data-reaction={cluster.reaction}
                key={cluster.cluster_id}
              >
                <span>{reactionGlyph(cluster.reaction)}</span>
                <span>{cluster.count_label}</span>
              </span>
            ))}
          </div>
        )}
        <section
          aria-label={
            projection.mode === "grid"
              ? t("media.p405.gridLabel")
              : t("media.p405.stageLabel")
          }
          className="media-p405-stage"
        >
          {projection.mode === "grid" ? (
            projection.items.length > 0 ? (
              <ul
                className={`media-p405-grid media-p405-grid--capacity-${projection.capacity}`}
              >
                {projection.items.map((item) => (
                  <li key={item.id}>
                    <MediaTile
                      attachVideo={attachedCameraVideoItemIDs.has(item.id)}
                      item={item}
                      onTogglePin={handleTogglePin}
                      pinned={layoutState.pinnedParticipantId === item.id}
                      variant="grid"
                    />
                  </li>
                ))}
              </ul>
            ) : (
              <p className="media-p405-empty">
                {t("media.p405.noParticipants")}
              </p>
            )
          ) : stageItem && stageTrackRef ? (
            <MediaTile
              attachVideo={
                projection.stage?.kind === "presentation"
                  ? attachPresentationVideo
                  : attachedCameraVideoItemIDs.has(stageItem.id)
              }
              item={stageItem}
              onTogglePin={handleTogglePin}
              pinned={layoutState.pinnedParticipantId === stageItem.id}
              trackRef={stageTrackRef}
              variant="stage"
            />
          ) : (
            <p className="media-p405-empty">{t("media.p405.noParticipants")}</p>
          )}
        </section>

        {!isResponsiveDrawer && projection.mode !== "grid" && (
          <aside
            aria-label={t("media.p405.railLabel")}
            className="media-p405-rail"
          >
            {rail}
          </aside>
        )}
      </div>

      <div
        aria-label={t("media.p405.pagination")}
        className="media-p405-pagination"
        id="media-p405-pagination"
        tabIndex={-1}
      >
        <IconButton
          disabled={projection.page <= 0}
          label={t("media.p405.previousPage")}
          onClick={() =>
            setLayoutState((current) => ({
              ...current,
              requestedPage: Math.max(0, projection.page - 1),
            }))
          }
          size="sm"
        >
          <ChevronLeft />
        </IconButton>
        <span aria-live="polite">
          {t("media.p405.page", {
            page: projection.page + 1,
            pages: projection.pageCount,
          })}
        </span>
        <IconButton
          disabled={projection.page >= projection.pageCount - 1}
          label={t("media.p405.nextPage")}
          onClick={() =>
            setLayoutState((current) => ({
              ...current,
              requestedPage: Math.min(
                projection.pageCount - 1,
                projection.page + 1,
              ),
            }))
          }
          size="sm"
        >
          <ChevronRight />
        </IconButton>
      </div>

      {isResponsiveDrawer && projection.mode !== "grid" && (
        <Drawer onOpenChange={setRailOpen} open={railOpen}>
          <DrawerTrigger asChild>
            <Button leadingIcon={<Users />} variant="secondary">
              {t("media.p405.openParticipants")}
            </Button>
          </DrawerTrigger>
          <DrawerContent
            className="media-p405-drawer"
            closeLabel={t("media.p405.close")}
            data-theme="dark"
          >
            <DrawerTitle>{t("media.p405.railLabel")}</DrawerTitle>
            {rail}
            <DrawerClose asChild>
              <Button variant="secondary">{t("media.p405.close")}</Button>
            </DrawerClose>
          </DrawerContent>
        </Drawer>
      )}

      <footer className="media-p405-footer">
        <div
          aria-label={t("media.p405.toolbar")}
          className="media-p405-toolbar"
          onKeyDown={handleToolbarKeyDown}
          ref={toolbarRef}
          role="toolbar"
        >
          {canPublishCameraMicrophone && (
            <>
              <IconButton
                data-media-control="microphone"
                disabled={pendingControl !== null || controlsTerminated}
                id="media-p405-control-microphone"
                label={t(
                  isMicrophoneEnabled
                    ? "media.p405.microphoneOn"
                    : "media.p405.microphoneOff",
                )}
                loading={pendingControl === "microphone"}
                onClick={() =>
                  void runControl("microphone", () =>
                    localParticipant.setMicrophoneEnabled(!isMicrophoneEnabled),
                  )
                }
                onFocus={() => setToolbarFocusKey("microphone")}
                tabIndex={toolbarTabIndex("microphone")}
                variant={isMicrophoneEnabled ? "secondary" : "danger"}
              >
                {isMicrophoneEnabled ? <Mic /> : <MicOff />}
              </IconButton>
              <IconButton
                data-media-control="camera"
                disabled={
                  pendingControl !== null ||
                  controlsTerminated ||
                  degradationStage === "audio-only"
                }
                id="media-p405-control-camera"
                label={t(
                  isCameraEnabled
                    ? "media.p405.cameraOn"
                    : "media.p405.cameraOff",
                )}
                loading={pendingControl === "camera"}
                onClick={() =>
                  void runControl("camera", () =>
                    localParticipant.setCameraEnabled(!isCameraEnabled),
                  )
                }
                onFocus={() => setToolbarFocusKey("camera")}
                tabIndex={toolbarTabIndex("camera")}
                variant={isCameraEnabled ? "secondary" : "danger"}
              >
                {isCameraEnabled ? <Camera /> : <CameraOff />}
              </IconButton>
            </>
          )}
          {canShareScreen && (
            <IconButton
              data-media-control="screen-share"
              disabled={pendingControl !== null || controlsTerminated}
              id="media-p405-control-screen-share"
              label={t(
                isScreenShareEnabled
                  ? "media.p405.shareStop"
                  : "media.p405.shareStart",
              )}
              loading={pendingControl === "screen-share"}
              onClick={() =>
                void runControl("screen-share", () =>
                  localParticipant.setScreenShareEnabled(!isScreenShareEnabled),
                )
              }
              onFocus={() => setToolbarFocusKey("screen-share")}
              tabIndex={toolbarTabIndex("screen-share")}
              variant={isScreenShareEnabled ? "danger" : "secondary"}
            >
              {isScreenShareEnabled ? <ScreenShareOff /> : <ScreenShare />}
            </IconButton>
          )}
          <IconButton
            aria-expanded={devicePanelOpen}
            data-media-control="devices"
            disabled={controlsTerminated}
            id="media-p405-control-devices"
            label={t("media.p405.devices")}
            onClick={() => setDevicePanelOpen((open) => !open)}
            onFocus={() => setToolbarFocusKey("devices")}
            tabIndex={toolbarTabIndex("devices")}
            variant="secondary"
          >
            <Settings2 />
          </IconButton>

          {signals?.projection?.viewer_operations.can_raise_hand && (
            <IconButton
              aria-pressed={Boolean(selfHand)}
              data-media-control="hand"
              disabled={
                signals.mutating || signals.loading || controlsTerminated
              }
              id="media-p406-control-hand"
              label={t(
                selfHand ? "media.p406.lowerOwnHand" : "media.p406.raiseHand",
              )}
              loading={signals.mutating}
              onClick={() =>
                void runSignalAction(
                  signals.onToggleHand,
                  selfHand
                    ? "media.p406.loweredOwnHand"
                    : "media.p406.raisedHand",
                )
              }
              onFocus={() => setToolbarFocusKey("hand")}
              tabIndex={toolbarTabIndex("hand")}
              variant={selfHand ? "primary" : "secondary"}
            >
              <Hand />
            </IconButton>
          )}

          {signals?.projection?.viewer_operations.can_send_reaction && (
            <Menu modal={false}>
              <MenuTrigger asChild>
                <IconButton
                  data-media-control="reaction"
                  disabled={signals.mutating || controlsTerminated}
                  id="media-p406-control-reaction"
                  label={t("media.p406.reactions")}
                  onFocus={() => setToolbarFocusKey("reaction")}
                  tabIndex={toolbarTabIndex("reaction")}
                  variant="secondary"
                >
                  <Smile />
                </IconButton>
              </MenuTrigger>
              <MenuContent
                aria-label={t("media.p406.reactionMenu")}
                className="media-p406-reaction-menu"
                data-theme="dark"
              >
                {CLASSROOM_REACTION_TYPES.map((reaction) => (
                  <MenuItem
                    key={reaction}
                    onSelect={() =>
                      void runSignalAction(
                        () => signals.onSendReaction(reaction),
                        "media.p406.reactionSent",
                      )
                    }
                  >
                    <span aria-hidden="true">{reactionGlyph(reaction)}</span>
                    {t(reactionLabelKey(reaction))}
                  </MenuItem>
                ))}
              </MenuContent>
            </Menu>
          )}

          {chat && (
            <Drawer onOpenChange={setChatOpen} open={chatOpen}>
              <DrawerTrigger asChild>
                <IconButton
                  data-media-control="chat"
                  id="media-p408-control-chat"
                  label={t("media.p408.openChat")}
                  onFocus={() => setToolbarFocusKey("chat")}
                  tabIndex={toolbarTabIndex("chat")}
                  variant="secondary"
                >
                  <MessageCircle />
                </IconButton>
              </DrawerTrigger>
              <DrawerContent
                className="media-p408-chat-drawer"
                closeLabel={t("media.p405.close")}
                data-theme="dark"
              >
                <DrawerTitle>{t("media.p408.title")}</DrawerTitle>
                {chat}
                <DrawerClose asChild>
                  <Button variant="secondary">{t("media.p405.close")}</Button>
                </DrawerClose>
              </DrawerContent>
            </Drawer>
          )}

          {signals && (
            <Drawer onOpenChange={setRosterOpen} open={rosterOpen}>
              <DrawerTrigger asChild>
                <IconButton
                  data-media-control="roster"
                  disabled={controlsTerminated}
                  id="media-p406-control-roster"
                  label={t("media.p406.openRoster")}
                  onFocus={() => setToolbarFocusKey("roster")}
                  tabIndex={toolbarTabIndex("roster")}
                  variant="secondary"
                >
                  <Users />
                </IconButton>
              </DrawerTrigger>
              <DrawerContent
                className="media-p406-roster-drawer"
                closeLabel={t("media.p405.close")}
                data-theme="dark"
              >
                <DrawerTitle>{t("media.p406.rosterTitle")}</DrawerTitle>
                <ClassroomRoster
                  moderation={moderation}
                  moderationDisabled={
                    controlsTerminated || connectionStatus !== "connected"
                  }
                  onLowerAll={() =>
                    void runSignalAction(
                      signals.onLowerAllHands,
                      "media.p406.loweredAllHands",
                    )
                  }
                  onLowerHand={(participantKey) =>
                    void runSignalAction(
                      () => signals.onLowerHand(participantKey),
                      "media.p406.loweredHand",
                    )
                  }
                  signals={signals}
                />
                <DrawerClose asChild>
                  <Button variant="secondary">{t("media.p405.close")}</Button>
                </DrawerClose>
              </DrawerContent>
            </Drawer>
          )}

          <div
            aria-label={t("media.p405.layoutGroup")}
            className="media-p405-layout-controls"
            role="group"
          >
            {layoutModes.map((mode) => (
              <IconButton
                aria-pressed={projection.mode === mode}
                data-media-control={`layout-${mode}`}
                disabled={mode === "presentation" && !presenterID}
                id={`media-p405-layout-${mode}`}
                key={mode}
                label={t(layoutModeKey(mode))}
                onClick={() => changeLayout(mode)}
                onFocus={() => setToolbarFocusKey(`layout-${mode}`)}
                tabIndex={toolbarTabIndex(`layout-${mode}`)}
                variant={projection.mode === mode ? "primary" : "quiet"}
              >
                {mode === "grid" ? (
                  <Grid2X2 />
                ) : mode === "active-speaker" ? (
                  <UserRound />
                ) : (
                  <Presentation />
                )}
              </IconButton>
            ))}
          </div>
        </div>

        <LeaveRoomDialog onLeave={handleLeave} />
      </footer>

      {devicePanelOpen && !controlsTerminated && (
        <DevicePanel
          canPublish={canPublishCameraMicrophone}
          onChange={(name, action) =>
            void runControl(name, () => Promise.resolve(action()))
          }
          onDeviceError={handleDeviceError}
        />
      )}

      {lobby && <aside className="media-p405-lobby">{lobby}</aside>}
    </div>
  );
}

function ClassroomRoster({
  moderation,
  moderationDisabled,
  onLowerAll,
  onLowerHand,
  signals,
}: {
  moderation?: ClassroomModerationControlsModel;
  moderationDisabled: boolean;
  onLowerAll: () => void;
  onLowerHand: (participantKey: string) => void;
  signals: ClassroomSignalControls;
}) {
  const { t } = useI18n();
  const projection = signals.projection;
  if (signals.loading && !projection) {
    return <p className="media-p405-empty">{t("media.p406.loading")}</p>;
  }
  if (signals.error && !projection) {
    return (
      <div className="media-p406-roster-state" role="alert">
        <p>{t("media.p406.loadError")}</p>
        <Button onClick={() => void signals.onResync()} size="sm">
          {t("media.p406.retry")}
        </Button>
      </div>
    );
  }
  if (!projection || projection.roster.length === 0) {
    return <p className="media-p405-empty">{t("media.p406.rosterEmpty")}</p>;
  }

  const raisedByParticipant = new Map(
    projection.raised_hands.map((hand) => [hand.participant_key, hand]),
  );
  const canModerate = projection.viewer_operations.can_moderate_hands;
  return (
    <section className="media-p406-roster">
      <div className="media-p406-roster-summary">
        <span>
          {t("media.room.participantCount", {
            count: projection.roster.length,
          })}
        </span>
        {signals.refreshing && <span>{t("media.p406.refreshing")}</span>}
        {canModerate && projection.raised_hands.length > 0 && (
          <Button
            disabled={signals.mutating}
            onClick={onLowerAll}
            size="sm"
            variant="secondary"
          >
            {t("media.p406.lowerAllHands")}
          </Button>
        )}
      </div>
      <ol aria-label={t("media.p406.rosterLabel")}>
        {projection.roster.map((participant) => {
          const hand = raisedByParticipant.get(participant.participant_key);
          const isSelf =
            participant.participant_key === projection.self_participant_key;
          return (
            <li key={participant.participant_key}>
              <div>
                <strong>{participant.display_name}</strong>
                {isSelf && <span> ({t("media.p406.you")})</span>}
                <span>{t(instanceRoleKey(participant.instance_role))}</span>
                <span>
                  {t(connectionStateKey(participant.connection_state))}
                </span>
              </div>
              {moderation && (
                <ClassroomParticipantModerationMenu
                  controls={moderation}
                  disabled={moderationDisabled}
                  displayName={participant.display_name}
                  isSelf={isSelf}
                  participantKey={participant.participant_key}
                />
              )}
              {hand && (
                <div className="media-p406-hand-state">
                  <span>
                    {t("media.p406.handQueuePosition", {
                      position: hand.queue_position,
                    })}
                  </span>
                  {canModerate && !isSelf && (
                    <Button
                      disabled={signals.mutating}
                      onClick={() => onLowerHand(participant.participant_key)}
                      size="sm"
                      variant="quiet"
                    >
                      {t("media.p406.lowerNamedHand", {
                        name: participant.display_name,
                      })}
                    </Button>
                  )}
                </div>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
}

function MediaTile({
  attachVideo,
  item,
  onTogglePin,
  pinned,
  trackRef = item.trackRef,
  variant,
}: MediaTileProps) {
  const { t } = useI18n();
  const displayName = item.displayName;
  const isPresentation = trackRef.source === Track.Source.ScreenShare;
  const label = t(
    isPresentation ? "media.p405.screenShare" : "media.p405.participantVideo",
    { name: displayName },
  );
  const pinButtonID = useId();

  return (
    <ParticipantTile
      aria-label={label}
      className={`media-p405-tile media-p405-tile--${variant}`}
      data-video-state={attachVideo ? "active" : "paused"}
      trackRef={trackRef}
    >
      {attachVideo && isTrackReference(trackRef) && (
        <VideoTrack
          aria-label={label}
          manageSubscription={false}
          trackRef={trackRef}
        />
      )}
      <div aria-hidden="true" className="media-p405-avatar">
        {displayName.slice(0, 1).toLocaleUpperCase()}
      </div>
      <div className="media-p405-tile-meta">
        <span>{displayName}</span>
        <IconButton
          aria-pressed={pinned}
          id={pinButtonID}
          label={t(pinned ? "media.p405.unpin" : "media.p405.pin", {
            name: displayName,
          })}
          onClick={(event) => {
            event.stopPropagation();
            onTogglePin(item, event.currentTarget);
          }}
          size="sm"
          variant="quiet"
        >
          {pinned ? <PinOff /> : <Pin />}
        </IconButton>
      </div>
    </ParticipantTile>
  );
}

function ParticipantRail({
  attachedVideoItemIDs,
  items,
  onTogglePin,
  pinnedID,
}: {
  attachedVideoItemIDs: ReadonlySet<string>;
  items: readonly MediaLayoutItem[];
  onTogglePin: MediaTileProps["onTogglePin"];
  pinnedID: string | null;
}) {
  const { t } = useI18n();
  if (items.length === 0) {
    return <p className="media-p405-empty">{t("media.p405.noParticipants")}</p>;
  }
  return (
    <ul className="media-p405-rail-list">
      {items.map((item) => (
        <li key={item.id}>
          <MediaTile
            attachVideo={attachedVideoItemIDs.has(item.id)}
            item={item}
            onTogglePin={onTogglePin}
            pinned={pinnedID === item.id}
            variant="rail"
          />
        </li>
      ))}
    </ul>
  );
}

type DeviceSelection = ReturnType<typeof useMediaDeviceSelect>;

function DevicePanel({
  canPublish,
  onChange,
  onDeviceError,
}: {
  canPublish: boolean;
  onChange: (name: string, action: () => Promise<unknown> | void) => void;
  onDeviceError: () => void;
}) {
  const { t } = useI18n();
  const speakerDevices = useMediaDeviceSelect({
    kind: "audiooutput",
    onError: onDeviceError,
  });
  return (
    <section className="media-p405-device-panel">
      {canPublish && (
        <PublishingDeviceSelectors
          onChange={onChange}
          onDeviceError={onDeviceError}
        />
      )}
      <DeviceSelect
        label={t("media.p405.speakerDevice")}
        name="speaker-device"
        onChange={onChange}
        selection={speakerDevices}
      />
    </section>
  );
}

function PublishingDeviceSelectors({
  onChange,
  onDeviceError,
}: {
  onChange: (name: string, action: () => Promise<unknown> | void) => void;
  onDeviceError: () => void;
}) {
  const { t } = useI18n();
  const microphoneDevices = useMediaDeviceSelect({
    kind: "audioinput",
    onError: onDeviceError,
  });
  const cameraDevices = useMediaDeviceSelect({
    kind: "videoinput",
    onError: onDeviceError,
  });
  return (
    <>
      <DeviceSelect
        label={t("media.p405.microphoneDevice")}
        name="microphone-device"
        onChange={onChange}
        selection={microphoneDevices}
      />
      <DeviceSelect
        label={t("media.p405.cameraDevice")}
        name="camera-device"
        onChange={onChange}
        selection={cameraDevices}
      />
    </>
  );
}

function DeviceSelect({
  label,
  name,
  onChange,
  selection,
}: {
  label: string;
  name: string;
  onChange: (name: string, action: () => Promise<unknown> | void) => void;
  selection: DeviceSelection;
}) {
  const { t } = useI18n();
  return (
    <label>
      <span>{label}</span>
      <select
        onChange={(event) =>
          onChange(name, () =>
            selection.setActiveMediaDevice(event.target.value),
          )
        }
        value={selection.activeDeviceId}
      >
        {selection.devices.length === 0 && (
          <option value="default">{t("media.p405.defaultDevice")}</option>
        )}
        {selection.devices.map((device, index) => (
          <option key={device.deviceId} value={device.deviceId}>
            {device.label || `${label} ${index + 1}`}
          </option>
        ))}
      </select>
    </label>
  );
}

function LeaveRoomDialog({ onLeave }: { onLeave: () => void }) {
  const { t } = useI18n();
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button
          id="media-p405-control-leave"
          leadingIcon={<LogOut />}
          variant="danger"
        >
          {t("media.p405.leave")}
        </Button>
      </DialogTrigger>
      <DialogContent closeLabel={t("media.p405.leaveCancel")} data-theme="dark">
        <DialogTitle>{t("media.p405.leaveTitle")}</DialogTitle>
        <DialogDescription>
          {t("media.p405.leaveDescription")}
        </DialogDescription>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="secondary">{t("media.p405.leaveCancel")}</Button>
          </DialogClose>
          <Button onClick={onLeave} variant="danger">
            {t("media.p405.leaveConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function stableMediaItems(
  trackRefs: readonly TrackReferenceOrPlaceholder[],
  participantProjection: WeakMap<object, SessionParticipantProjection>,
  participantFallback: string,
): readonly MediaLayoutItem[] {
  const observedItems: Array<{
    inputIndex: number;
    projection: SessionParticipantProjection;
    trackRef: TrackReferenceOrPlaceholder;
  }> = [];
  trackRefs.forEach((trackRef, inputIndex) => {
    const projection = participantProjection.get(trackRef.participant);
    if (projection) observedItems.push({ inputIndex, projection, trackRef });
  });
  return observedItems
    .sort(
      (left, right) =>
        left.projection.sequence - right.projection.sequence ||
        left.inputIndex - right.inputIndex,
    )
    .map(({ projection, trackRef }) => ({
      displayName: trackRef.participant.name?.trim() || participantFallback,
      id: projection.id,
      isLocal: trackRef.participant.isLocal,
      sequence: projection.sequence,
      trackRef,
    }));
}

function authoritativeMediaItems(
  trackRefs: readonly TrackReferenceOrPlaceholder[],
  roster: ClassroomSignalProjection["roster"],
  selfParticipantKey: string | null,
): readonly MediaLayoutItem[] {
  const rosterByParticipantKey = new Map(
    roster.map((participant) => [participant.participant_key, participant]),
  );
  const seen = new Set<string>();
  return trackRefs
    .flatMap((trackRef) => {
      const participantKey =
        trackRef.participant.attributes?.["tutorhub.participant_key"];
      if (typeof participantKey !== "string" || seen.has(participantKey)) {
        return [];
      }
      const participant = rosterByParticipantKey.get(participantKey);
      if (!participant) return [];
      seen.add(participantKey);
      return [
        {
          displayName: participant.display_name,
          id: participant.participant_key,
          isLocal: participant.participant_key === selfParticipantKey,
          sequence: participant.roster_sequence,
          trackRef,
        },
      ];
    })
    .sort(
      (left, right) =>
        left.sequence - right.sequence || left.id.localeCompare(right.id),
    );
}

function createSessionParticipantProjectionStore(
  initialTrackRefs: readonly TrackReferenceOrPlaceholder[],
): SessionParticipantProjectionStore {
  const entries = new WeakMap<object, SessionParticipantProjection>();
  const listeners = new Set<() => void>();
  let nextRemoteSequence = 1;
  let snapshot: SessionParticipantProjectionSnapshot = {
    entries,
    revision: 0,
  };
  const observe = (trackRefs: readonly TrackReferenceOrPlaceholder[]) => {
    let changed = false;
    const localFirstTrackRefs = [
      ...trackRefs.filter(({ participant }) => participant.isLocal),
      ...trackRefs.filter(({ participant }) => !participant.isLocal),
    ];
    for (const trackRef of localFirstTrackRefs) {
      if (entries.has(trackRef.participant)) continue;
      const isLocal = trackRef.participant.isLocal;
      const sequence = isLocal ? 0 : nextRemoteSequence;
      if (!isLocal) nextRemoteSequence += 1;
      entries.set(trackRef.participant, {
        id: isLocal
          ? "p405-session-local-participant"
          : `p405-session-participant-${sequence}`,
        sequence,
      });
      changed = true;
    }
    if (!changed) return;
    snapshot = { entries, revision: snapshot.revision + 1 };
    for (const listener of listeners) listener();
  };
  observe(initialTrackRefs);
  return {
    getSnapshot: () => snapshot,
    observe,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

function isRemoteTrackPublication(
  value: unknown,
): value is RemoteTrackPublication {
  if (value instanceof RemoteTrackPublication) return true;
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as {
    setSubscribed?: unknown;
    setVideoQuality?: unknown;
  };
  return (
    typeof candidate.setSubscribed === "function" &&
    typeof candidate.setVideoQuality === "function"
  );
}

function classroomDegradationSignal(
  quality: ConnectionQuality,
  publications: ReadonlySet<RemoteTrackPublication>,
  documentVisible: boolean,
): ClassroomDegradationSignal {
  if (!documentVisible || quality === ConnectionQuality.Lost) return "hold";
  const desiredVideoPaused = [...publications].some(
    (publication) =>
      publication.isDesired &&
      publication.isEnabled &&
      publication.track?.streamState === Track.StreamState.Paused,
  );
  if (desiredVideoPaused || quality === ConnectionQuality.Poor) {
    return "unstable";
  }
  if (
    quality === ConnectionQuality.Excellent ||
    quality === ConnectionQuality.Good
  ) {
    return "stable";
  }
  return "hold";
}

function degradationStatusKey(
  stage: Exclude<ClassroomDegradationStage, "normal">,
): TranslationKey {
  return `media.p405.degradation.${stage}` as TranslationKey;
}

function roomStatusKey(status: ClassroomConnectionStatus): TranslationKey {
  return `media.room.${status}` as TranslationKey;
}

function layoutModeKey(mode: ClassroomLayoutMode): TranslationKey {
  if (mode === "active-speaker") return "media.p405.layoutActiveSpeaker";
  if (mode === "presentation") return "media.p405.layoutPresentation";
  return "media.p405.layoutGrid";
}

function reactionGlyph(reaction: ClassroomReactionType): string {
  switch (reaction) {
    case "thumbs_up":
      return "👍";
    case "clap":
      return "👏";
    case "heart":
      return "❤️";
    case "celebrate":
      return "🎉";
    case "laugh":
      return "😂";
    case "surprised":
      return "😮";
  }
}

function reactionLabelKey(reaction: ClassroomReactionType): TranslationKey {
  return `media.p406.reaction.${reaction}` as TranslationKey;
}

function instanceRoleKey(
  role: ClassroomSignalProjection["roster"][number]["instance_role"],
): TranslationKey {
  return `media.p406.role.${role}` as TranslationKey;
}

function connectionStateKey(
  state: ClassroomSignalProjection["roster"][number]["connection_state"],
): TranslationKey {
  return `media.p406.connection.${state}` as TranslationKey;
}

function signalMutationErrorKey(error: unknown): TranslationKey {
  if (typeof error !== "object" || error === null || !("status" in error)) {
    return "media.p406.actionError";
  }
  const status = (error as { status?: unknown }).status;
  if (status === 403) return "media.p406.forbidden";
  if (status === 409) return "media.p406.conflict";
  if (status === 429) return "media.p406.rateLimited";
  return "media.p406.actionError";
}
