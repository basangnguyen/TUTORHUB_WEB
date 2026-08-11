import {
  ParticipantTile,
  VideoTrack,
  isTrackReference,
  useLocalParticipant,
  useMediaDeviceSelect,
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
} from "@tutorhub/ui";
import {
  Camera,
  CameraOff,
  ChevronLeft,
  ChevronRight,
  Grid2X2,
  LogOut,
  Mic,
  MicOff,
  Pin,
  PinOff,
  Presentation,
  ScreenShare,
  ScreenShareOff,
  Settings2,
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

export type ClassroomConnectionStatus =
  "connecting" | "connected" | "disconnected" | "failed";

export interface ClassroomMediaShellProps {
  canPublishCameraMicrophone: boolean;
  canShareScreen: boolean;
  canSubscribe: boolean;
  connectionStatus: ClassroomConnectionStatus;
  controlAbortSignal?: AbortSignal;
  lobby?: ReactNode;
  onLeave: () => void;
  onTerminalMediaCleanup: () => Promise<void>;
}

interface MediaLayoutItem extends ClassroomLayoutItem {
  readonly participantIdentity: string;
  readonly trackRef: TrackReferenceOrPlaceholder;
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
  lobby,
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
  const screenShareTrackRefs = useTracks([Track.Source.ScreenShare], {
    onlySubscribed: false,
  });
  const audioTrackRefs = useTracks(
    [Track.Source.Microphone, Track.Source.ScreenShareAudio],
    { onlySubscribed: false },
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
    },
    [],
  );

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

  const allCameraItems = useMemo(
    () => stableMediaItems(cameraTrackRefs),
    [cameraTrackRefs],
  );
  const cameraItems = useMemo(
    () =>
      canSubscribe
        ? allCameraItems
        : allCameraItems.filter(({ trackRef }) => trackRef.participant.isLocal),
    [allCameraItems, canSubscribe],
  );
  const itemByIdentity = useMemo(
    () =>
      new Map(
        allCameraItems.map((item) => [item.participantIdentity, item] as const),
      ),
    [allCameraItems],
  );
  const presenterTrackRef = screenShareTrackRefs[0];
  const presenterItem = presenterTrackRef
    ? (itemByIdentity.get(presenterTrackRef.participant.identity) ?? null)
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
      "layout-grid",
      "layout-active-speaker",
      ...(presenterID ? (["layout-presentation"] as const) : []),
    ],
    [
      canPublishCameraMicrophone,
      canShareScreen,
      controlsTerminated,
      pendingControl,
      presenterID,
    ],
  );
  const effectiveToolbarFocusKey = toolbarControlKeys.includes(toolbarFocusKey)
    ? toolbarFocusKey
    : toolbarControlKeys[0];
  const toolbarTabIndex = (key: ToolbarControlKey) =>
    effectiveToolbarFocusKey === key ? 0 : -1;

  const candidateSpeakerID =
    itemByIdentity.get(speakingParticipants[0]?.identity ?? "")?.id ?? null;
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
        const item = itemByIdentity.get(trackRef.participant.identity);
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
    itemByIdentity,
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
  }, [localConnectionQuality]);

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
            {t("media.room.participantCount", { count: cameraItems.length })}
          </p>
        </div>
        <div className="media-p405-header-badges">
          {!canPublishCameraMicrophone && (
            <span>{t("media.room.listenOnly")}</span>
          )}
          <span>{t("media.p405.effectsNone")}</span>
        </div>
      </header>

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
                disabled={pendingControl !== null || controlsTerminated}
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

function MediaTile({
  attachVideo,
  item,
  onTogglePin,
  pinned,
  trackRef = item.trackRef,
  variant,
}: MediaTileProps) {
  const { t } = useI18n();
  const displayName =
    trackRef.participant.name?.trim() || t("media.p405.participantFallback");
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
): readonly MediaLayoutItem[] {
  return trackRefs
    .map((trackRef, inputIndex) => ({
      inputIndex,
      joinedAt: finiteJoinedAt(trackRef.participant.joinedAt),
      trackRef,
    }))
    .sort(
      (left, right) =>
        left.joinedAt - right.joinedAt ||
        left.trackRef.participant.identity.localeCompare(
          right.trackRef.participant.identity,
        ) ||
        left.inputIndex - right.inputIndex,
    )
    .map(({ trackRef }, sequence) => ({
      id: trackRef.participant.identity,
      isLocal: trackRef.participant.isLocal,
      participantIdentity: trackRef.participant.identity,
      sequence,
      trackRef,
    }));
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

function finiteJoinedAt(value: Date | undefined): number {
  const joinedAt = value?.getTime();
  return typeof joinedAt === "number" && Number.isFinite(joinedAt)
    ? joinedAt
    : Number.MAX_SAFE_INTEGER;
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
