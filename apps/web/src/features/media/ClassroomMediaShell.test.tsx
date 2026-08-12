// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../../app/i18n";
import { ClassroomMediaShell } from "./ClassroomMediaShell";

const liveKitState = vi.hoisted(() => {
  class MockRemoteTrackPublication {
    isDesired = false;
    isEnabled = true;
    readonly track = { streamState: "active" };
    videoQuality = "high";
    readonly setSubscribed = vi.fn();
    readonly setVideoQuality = vi.fn();

    constructor() {
      this.setSubscribed.mockImplementation((subscribed: boolean) => {
        this.isDesired = subscribed;
        subscriptionTransitions.push({ publication: this, subscribed });
      });
      this.setVideoQuality.mockImplementation((quality: string) => {
        this.videoQuality = quality;
      });
    }
  }

  const subscriptionTransitions: Array<{
    publication: MockRemoteTrackPublication;
    subscribed: boolean;
  }> = [];

  const connectionQualityListeners = new Set<(quality: string) => void>();
  const deviceSetters = {
    audioinput: vi.fn().mockResolvedValue(undefined),
    audiooutput: vi.fn().mockResolvedValue(undefined),
    videoinput: vi.fn().mockResolvedValue(undefined),
  };
  const localParticipant = {
    connectionQuality: "excellent",
    off: vi.fn((event: string, handler: (quality: string) => void) => {
      if (event === "connectionQualityChanged") {
        connectionQualityListeners.delete(handler);
      }
    }),
    on: vi.fn((event: string, handler: (quality: string) => void) => {
      if (event === "connectionQualityChanged") {
        connectionQualityListeners.add(handler);
      }
    }),
    setCameraEnabled: vi.fn().mockResolvedValue(undefined),
    setMicrophoneEnabled: vi.fn().mockResolvedValue(undefined),
    setScreenShareEnabled: vi.fn().mockResolvedValue(undefined),
  };

  return {
    MockRemoteTrackPublication,
    cameraTrackRefs: [] as MockTrackReference[],
    deviceSetters,
    screenShareTrackRefs: [] as MockTrackReference[],
    audioTrackRefs: [] as MockTrackReference[],
    speakingParticipants: [] as MockParticipant[],
    mediaDeviceSelect: vi.fn(),
    subscriptionTransitions,
    trackOptions: [] as Array<{ onlySubscribed?: boolean } | undefined>,
    terminalMediaCleanup: vi.fn().mockResolvedValue(undefined),
    localParticipant,
    emitConnectionQuality(quality: string) {
      localParticipant.connectionQuality = quality;
      for (const listener of connectionQualityListeners) listener(quality);
    },
  };
});

interface MockParticipant {
  identity: string;
  isLocal: boolean;
  joinedAt?: Date;
  name: string;
}

interface MockTrackReference {
  participant: MockParticipant;
  publication:
    InstanceType<typeof liveKitState.MockRemoteTrackPublication> | object;
  source: string;
}

vi.mock("livekit-client", () => ({
  ConnectionQuality: {
    Excellent: "excellent",
    Good: "good",
    Lost: "lost",
    Poor: "poor",
    Unknown: "unknown",
  },
  ParticipantEvent: {
    ConnectionQualityChanged: "connectionQualityChanged",
  },
  RemoteTrackPublication: liveKitState.MockRemoteTrackPublication,
  Track: {
    Source: {
      Camera: "camera",
      Microphone: "microphone",
      ScreenShare: "screen_share",
      ScreenShareAudio: "screen_share_audio",
    },
    StreamState: {
      Active: "active",
      Paused: "paused",
      Unknown: "unknown",
    },
  },
  VideoQuality: {
    HIGH: "high",
    LOW: "low",
  },
}));

vi.mock("@livekit/components-react", () => ({
  ParticipantTile: ({
    children,
    className,
    trackRef,
    ...props
  }: {
    children?: ReactNode;
    className?: string;
    trackRef: MockTrackReference;
  }) => (
    <div
      className={className}
      data-lk-source={trackRef.source}
      data-lk-video-muted="false"
      {...props}
    >
      {children}
    </div>
  ),
  VideoTrack: (props: { "aria-label"?: string }) => (
    <video aria-label={props["aria-label"]} />
  ),
  isTrackReference: (trackRef: MockTrackReference) =>
    Boolean(trackRef.publication),
  useLocalParticipant: () => ({
    isCameraEnabled: false,
    isMicrophoneEnabled: false,
    isScreenShareEnabled: false,
    localParticipant: liveKitState.localParticipant,
  }),
  useMediaDeviceSelect: ({
    kind,
  }: {
    kind: keyof typeof liveKitState.deviceSetters;
  }) => {
    liveKitState.mediaDeviceSelect(kind);
    return {
      activeDeviceId: "default",
      devices: [],
      setActiveMediaDevice: liveKitState.deviceSetters[kind],
      className: kind,
    };
  },
  useSpeakingParticipants: () => liveKitState.speakingParticipants,
  useTracks: (
    sources: Array<string | { source: string }>,
    options?: { onlySubscribed?: boolean },
  ) => {
    liveKitState.trackOptions.push(options);
    const first = sources[0];
    const source = typeof first === "string" ? first : first?.source;
    if (source === "microphone") return liveKitState.audioTrackRefs;
    return source === "screen_share"
      ? liveKitState.screenShareTrackRefs
      : liveKitState.cameraTrackRefs;
  },
}));

describe("ClassroomMediaShell", () => {
  beforeEach(() => {
    Object.defineProperty(globalThis, "innerWidth", {
      configurable: true,
      value: 1_280,
    });
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: "visible",
    });
    liveKitState.cameraTrackRefs = createCameraTracks(25);
    liveKitState.screenShareTrackRefs = [];
    liveKitState.audioTrackRefs = createAudioTracks(25);
    liveKitState.speakingParticipants = [];
    liveKitState.localParticipant.connectionQuality = "excellent";
    liveKitState.localParticipant.on.mockClear();
    liveKitState.localParticipant.off.mockClear();
    liveKitState.localParticipant.setCameraEnabled
      .mockReset()
      .mockResolvedValue(undefined);
    liveKitState.localParticipant.setMicrophoneEnabled
      .mockReset()
      .mockResolvedValue(undefined);
    liveKitState.localParticipant.setScreenShareEnabled
      .mockReset()
      .mockResolvedValue(undefined);
    for (const setter of Object.values(liveKitState.deviceSetters)) {
      setter.mockReset().mockResolvedValue(undefined);
    }
    liveKitState.mediaDeviceSelect.mockClear();
    liveKitState.terminalMediaCleanup.mockReset().mockResolvedValue(undefined);
    liveKitState.subscriptionTransitions.length = 0;
    liveKitState.trackOptions.length = 0;
  });

  afterEach(() => {
    cleanup();
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it("keeps the grid stable and subscribes only the current 12-video page", async () => {
    const { container } = renderShell();

    expect(container.querySelectorAll(".media-p405-grid > li")).toHaveLength(
      12,
    );
    expect(screen.getByText("Page 1 of 3")).toBeInTheDocument();
    expect(container.innerHTML).not.toContain("provider-participant-");

    const remotePublications = liveKitState.cameraTrackRefs
      .slice(1)
      .map(
        ({ publication }) =>
          publication as InstanceType<
            typeof liveKitState.MockRemoteTrackPublication
          >,
      );
    await waitFor(() => {
      expect(remotePublications[0]?.setSubscribed).toHaveBeenLastCalledWith(
        true,
      );
      expect(remotePublications[11]?.isDesired).toBe(false);
      expect(
        (
          liveKitState.audioTrackRefs[0]?.publication as InstanceType<
            typeof liveKitState.MockRemoteTrackPublication
          >
        ).isDesired,
      ).toBe(true);
    });

    fireEvent.click(screen.getByRole("button", { name: "Next video page" }));
    expect(screen.getByText("Page 2 of 3")).toBeInTheDocument();
    await waitFor(() => {
      expect(remotePublications[0]?.setSubscribed).toHaveBeenLastCalledWith(
        false,
      );
      expect(remotePublications[11]?.setSubscribed).toHaveBeenLastCalledWith(
        true,
      );
    });
  });

  it("observes unpublished remote sources before manually subscribing", () => {
    renderShell();

    expect(liveKitState.trackOptions.length).toBeGreaterThanOrEqual(3);
    expect(
      liveKitState.trackOptions.every(
        (options) => options?.onlySubscribed === false,
      ),
    ).toBe(true);
  });

  it("subscribes a publication that crosses a duplicate provider module boundary", async () => {
    const publication = new liveKitState.MockRemoteTrackPublication();
    Object.setPrototypeOf(publication, Object.prototype);
    liveKitState.cameraTrackRefs = [
      {
        participant: {
          identity: "provider-duplicate-module",
          isLocal: false,
          joinedAt: new Date(1_000),
          name: "Remote participant",
        },
        publication,
        source: "camera",
      },
    ];

    renderShell();

    await waitFor(() =>
      expect(publication.setSubscribed).toHaveBeenCalledWith(true),
    );
  });

  it("keeps local-first session order without provider identity or time", () => {
    const tracks = createCameraTracks(5);
    liveKitState.cameraTrackRefs = [...tracks].reverse();
    const view = renderShell();
    const visibleNames = () =>
      Array.from(
        view.container.querySelectorAll(
          ".media-p405-grid .media-p405-tile-meta > span",
        ),
      ).map((element) => element.textContent);

    expect(visibleNames()).toEqual([
      "Learner 1",
      "Learner 5",
      "Learner 4",
      "Learner 3",
      "Learner 2",
    ]);

    tracks.forEach((track, index) => {
      track.participant.identity = `provider-identity-rewritten-${index}`;
      track.participant.joinedAt = new Date(50_000 - index);
    });
    liveKitState.cameraTrackRefs = [
      tracks[2]!,
      tracks[4]!,
      tracks[0]!,
      tracks[3]!,
      tracks[1]!,
    ];
    view.rerender(shellElement());
    expect(visibleNames()).toEqual([
      "Learner 1",
      "Learner 5",
      "Learner 4",
      "Learner 3",
      "Learner 2",
    ]);

    const newcomer = createCameraTracks(1)[0]!;
    newcomer.participant.isLocal = false;
    newcomer.participant.name = "Learner 6";
    liveKitState.cameraTrackRefs = [newcomer, ...liveKitState.cameraTrackRefs];
    view.rerender(shellElement());
    expect(visibleNames()).toEqual([
      "Learner 1",
      "Learner 5",
      "Learner 4",
      "Learner 3",
      "Learner 2",
      "Learner 6",
    ]);
  });

  it("retires an old page before subscribing replacements in reversed provider order", async () => {
    liveKitState.cameraTrackRefs = [...createCameraTracks(25)].reverse();
    renderShell();
    const cameraPublications = new Set(
      liveKitState.cameraTrackRefs
        .map(({ publication }) => publication)
        .filter(
          (
            publication,
          ): publication is InstanceType<
            typeof liveKitState.MockRemoteTrackPublication
          > => publication instanceof liveKitState.MockRemoteTrackPublication,
        ),
    );
    await waitFor(() =>
      expect(
        [...cameraPublications].filter(({ isDesired }) => isDesired),
      ).toHaveLength(11),
    );
    liveKitState.subscriptionTransitions.length = 0;

    fireEvent.click(screen.getByRole("button", { name: "Next video page" }));
    await waitFor(() =>
      expect(
        [...cameraPublications].filter(({ isDesired }) => isDesired),
      ).toHaveLength(12),
    );

    const pageTransitions = liveKitState.subscriptionTransitions.filter(
      ({ publication }) => cameraPublications.has(publication),
    );
    const firstSubscribe = pageTransitions.findIndex(
      ({ subscribed }) => subscribed,
    );
    const lastUnsubscribe = pageTransitions.reduce(
      (lastIndex, { subscribed }, index) => (subscribed ? lastIndex : index),
      -1,
    );
    expect(firstSubscribe).toBeGreaterThan(0);
    expect(lastUnsubscribe).toBeLessThan(firstSubscribe);
  });

  it("commits a clamped page before the roster grows again", async () => {
    const view = renderShell();
    fireEvent.click(screen.getByRole("button", { name: "Next video page" }));
    fireEvent.click(screen.getByRole("button", { name: "Next video page" }));
    expect(screen.getByText("Page 3 of 3")).toBeInTheDocument();

    liveKitState.cameraTrackRefs = createCameraTracks(5);
    view.rerender(shellElement());
    await waitFor(() => {
      expect(screen.getByText("Page 1 of 1")).toBeInTheDocument();
      expect(document.getElementById("media-p405-pagination")).toHaveFocus();
    });

    liveKitState.cameraTrackRefs = createCameraTracks(25);
    view.rerender(shellElement());
    expect(screen.getByText("Page 1 of 3")).toBeInTheDocument();
  });

  it("renders only controls authorized by the exact credential grants", () => {
    renderShell({
      canPublishCameraMicrophone: false,
      canShareScreen: false,
      canSubscribe: false,
    });

    expect(screen.getByText("Listen-only mode")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Turn microphone on" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Turn camera on" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Share screen" }),
    ).not.toBeInTheDocument();
    expect(
      liveKitState.localParticipant.setMicrophoneEnabled,
    ).not.toHaveBeenCalled();
    expect(
      liveKitState.localParticipant.setCameraEnabled,
    ).not.toHaveBeenCalled();
    expect(
      liveKitState.localParticipant.setScreenShareEnabled,
    ).not.toHaveBeenCalled();
    expect(liveKitState.mediaDeviceSelect).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Devices" }));
    expect(liveKitState.mediaDeviceSelect).toHaveBeenCalledTimes(1);
    expect(liveKitState.mediaDeviceSelect).toHaveBeenCalledWith("audiooutput");
    expect(
      (
        liveKitState.audioTrackRefs[0]?.publication as InstanceType<
          typeof liveKitState.MockRemoteTrackPublication
        >
      ).isDesired,
    ).toBe(false);
  });

  it.each([
    {
      name: "subscribe-only",
      canPublishCameraMicrophone: false,
      canShareScreen: false,
      canSubscribe: true,
      microphoneVisible: false,
      shareVisible: false,
    },
    {
      name: "camera/microphone without share",
      canPublishCameraMicrophone: true,
      canShareScreen: false,
      canSubscribe: true,
      microphoneVisible: true,
      shareVisible: false,
    },
    {
      name: "share without camera/microphone",
      canPublishCameraMicrophone: false,
      canShareScreen: true,
      canSubscribe: true,
      microphoneVisible: false,
      shareVisible: true,
    },
  ])(
    "projects the exact $name credential grant",
    ({
      canPublishCameraMicrophone,
      canShareScreen,
      canSubscribe,
      microphoneVisible,
      shareVisible,
    }) => {
      renderShell({
        canPublishCameraMicrophone,
        canShareScreen,
        canSubscribe,
      });

      expect(
        Boolean(screen.queryByRole("button", { name: "Turn microphone on" })),
      ).toBe(microphoneVisible);
      expect(
        Boolean(screen.queryByRole("button", { name: "Turn camera on" })),
      ).toBe(microphoneVisible);
      expect(
        Boolean(screen.queryByRole("button", { name: "Share screen" })),
      ).toBe(shareVisible);
      expect(
        (
          liveKitState.audioTrackRefs[0]?.publication as InstanceType<
            typeof liveKitState.MockRemoteTrackPublication
          >
        ).isDesired,
      ).toBe(canSubscribe);
    },
  );

  it("uses custom media controls and arrow-key toolbar navigation", async () => {
    renderShell();
    const microphone = screen.getByRole("button", {
      name: "Turn microphone on",
    });
    const camera = screen.getByRole("button", { name: "Turn camera on" });

    expect(microphone).toHaveAttribute("tabindex", "0");
    expect(camera).toHaveAttribute("tabindex", "-1");
    microphone.focus();
    fireEvent.keyDown(microphone, { key: "ArrowRight" });
    expect(camera).toHaveFocus();
    expect(microphone).toHaveAttribute("tabindex", "-1");
    expect(camera).toHaveAttribute("tabindex", "0");

    fireEvent.click(camera);
    await waitFor(() =>
      expect(
        liveKitState.localParticipant.setCameraEnabled,
      ).toHaveBeenCalledWith(true),
    );
    fireEvent.click(screen.getByRole("button", { name: "Share screen" }));
    await waitFor(() =>
      expect(
        liveKitState.localParticipant.setScreenShareEnabled,
      ).toHaveBeenCalledWith(true),
    );
  });

  it("bounds the active-speaker rail and restores the prior mode after sharing", async () => {
    const view = renderShell();
    const activeSpeaker = screen.getByRole("button", {
      name: "Active speaker",
    });
    fireEvent.click(activeSpeaker);
    expect(activeSpeaker).toHaveAttribute("aria-pressed", "true");
    expect(
      view.container.querySelectorAll(".media-p405-rail-list > li"),
    ).toHaveLength(6);

    liveKitState.screenShareTrackRefs = [
      {
        participant: liveKitState.cameraTrackRefs[4]!.participant,
        publication: new liveKitState.MockRemoteTrackPublication(),
        source: "screen_share",
      },
    ];
    view.rerender(shellElement());
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Presentation" }),
      ).toHaveAttribute("aria-pressed", "true"),
    );
    const presenterCamera = liveKitState.cameraTrackRefs[4]
      ?.publication as InstanceType<
      typeof liveKitState.MockRemoteTrackPublication
    >;
    const presenterShare = liveKitState.screenShareTrackRefs[0]
      ?.publication as InstanceType<
      typeof liveKitState.MockRemoteTrackPublication
    >;
    expect(presenterCamera.isDesired).toBe(false);
    expect(presenterShare.isDesired).toBe(true);

    liveKitState.screenShareTrackRefs = [];
    view.rerender(shellElement());
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Active speaker" }),
      ).toHaveAttribute("aria-pressed", "true"),
    );
  });

  it("restores exact screen-share focus after presentation ends", async () => {
    const view = renderShell();
    const shareControl = screen.getByRole("button", { name: "Share screen" });
    shareControl.focus();
    fireEvent.click(shareControl);
    await waitFor(() =>
      expect(
        liveKitState.localParticipant.setScreenShareEnabled,
      ).toHaveBeenCalledWith(true),
    );

    liveKitState.screenShareTrackRefs = [
      {
        participant: liveKitState.cameraTrackRefs[4]!.participant,
        publication: new liveKitState.MockRemoteTrackPublication(),
        source: "screen_share",
      },
    ];
    view.rerender(shellElement());
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Presentation" }),
      ).toHaveAttribute("aria-pressed", "true"),
    );
    expect(shareControl).toHaveFocus();

    liveKitState.screenShareTrackRefs = [];
    view.rerender(shellElement());
    await waitFor(() => expect(shareControl).toHaveFocus());
  });

  it("uses drawer trigger semantics and returns focus on compact layouts", async () => {
    Object.defineProperty(globalThis, "innerWidth", {
      configurable: true,
      value: 320,
    });
    renderShell();
    fireEvent.click(screen.getByRole("button", { name: "Active speaker" }));
    const opener = await screen.findByRole("button", {
      name: "Open participant list",
    });

    opener.focus();
    fireEvent.click(opener);
    expect(opener).toHaveAttribute("aria-expanded", "true");
    const close = screen
      .getAllByRole("button", { name: "Close" })
      .find((button) => button.textContent?.trim() === "Close");
    expect(close).toBeDefined();
    fireEvent.click(close!);
    await waitFor(() => expect(opener).toHaveFocus());
  });

  it("keeps one toolbar tab stop and restores focus after a pin changes layout", async () => {
    renderShell();
    const toolbar = screen.getByRole("toolbar", {
      name: "Classroom controls",
    });
    const pin = screen.getByRole("button", { name: "Pin Learner 1" });

    fireEvent.click(pin);

    const activeSpeaker = screen.getByRole("button", {
      name: "Active speaker",
    });
    await waitFor(() => expect(activeSpeaker).toHaveFocus());
    expect(activeSpeaker).toHaveAttribute("aria-pressed", "true");
    expect(
      Array.from(toolbar.querySelectorAll("button")).filter(
        (button) => button.tabIndex === 0,
      ),
    ).toEqual([activeSpeaker]);
  });

  it("degrades video one tier at a time while preserving layout, audio and controls", () => {
    vi.useFakeTimers();
    const view = renderShell();
    const firstRemoteCamera = liveKitState.cameraTrackRefs[1]
      ?.publication as InstanceType<
      typeof liveKitState.MockRemoteTrackPublication
    >;

    expect(
      view.container.querySelectorAll(".media-p405-grid > li"),
    ).toHaveLength(12);
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(12);

    act(() => liveKitState.emitConnectionQuality("poor"));
    act(() => vi.advanceTimersByTime(5_000));
    expect(
      screen.getByText(/showing fewer videos while keeping audio/i),
    ).toBeInTheDocument();
    expect(
      view.container.querySelectorAll(".media-p405-grid > li"),
    ).toHaveLength(12);
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(6);

    act(() => vi.advanceTimersByTime(5_000));
    expect(firstRemoteCamera.setVideoQuality).toHaveBeenLastCalledWith("low");
    expect(
      firstRemoteCamera.setSubscribed.mock.invocationCallOrder[0],
    ).toBeLessThan(
      firstRemoteCamera.setVideoQuality.mock.invocationCallOrder[0]!,
    );

    act(() => vi.advanceTimersByTime(5_000));
    expect(
      view.container.querySelectorAll(".media-p405-grid > li"),
    ).toHaveLength(12);
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(1);

    act(() => vi.advanceTimersByTime(5_000));
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(0);
    expect(screen.getByRole("toolbar")).toBeInTheDocument();
    expect(
      (
        liveKitState.audioTrackRefs[0]?.publication as InstanceType<
          typeof liveKitState.MockRemoteTrackPublication
        >
      ).isDesired,
    ).toBe(true);

    act(() => liveKitState.emitConnectionQuality("excellent"));
    act(() => vi.advanceTimersByTime(15_000));
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(1);

    view.unmount();
    expect(liveKitState.localParticipant.off).toHaveBeenCalledWith(
      "connectionQualityChanged",
      expect.any(Function),
    );
    expect(vi.getTimerCount()).toBe(0);
  });

  it("uses the public paused stream state as bounded degradation evidence", () => {
    vi.useFakeTimers();
    const pausedPublication = liveKitState.cameraTrackRefs[1]
      ?.publication as InstanceType<
      typeof liveKitState.MockRemoteTrackPublication
    >;
    pausedPublication.track.streamState = "paused";
    const view = renderShell();

    act(() => vi.advanceTimersByTime(5_000));

    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(6);
    expect(
      screen.getByText(/showing fewer videos while keeping audio/i),
    ).toBeInTheDocument();
  });

  it("holds degradation for adaptive-disabled video and a hidden document", () => {
    vi.useFakeTimers();
    const pausedPublication = liveKitState.cameraTrackRefs[1]
      ?.publication as InstanceType<
      typeof liveKitState.MockRemoteTrackPublication
    >;
    pausedPublication.track.streamState = "paused";
    pausedPublication.isEnabled = false;
    const view = renderShell();

    act(() => vi.advanceTimersByTime(20_000));
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(12);

    pausedPublication.isEnabled = true;
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: "hidden",
    });
    act(() => vi.advanceTimersByTime(20_000));
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(12);

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      value: "visible",
    });
    act(() => vi.advanceTimersByTime(6_000));
    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(6);
  });

  it("keeps presentation video ahead of cameras until audio-only", async () => {
    vi.useFakeTimers();
    liveKitState.screenShareTrackRefs = [
      {
        participant: liveKitState.cameraTrackRefs[4]!.participant,
        publication: new liveKitState.MockRemoteTrackPublication(),
        source: "screen_share",
      },
    ];
    const view = renderShell();
    act(() => liveKitState.emitConnectionQuality("poor"));

    act(() => vi.advanceTimersByTime(15_000));
    const sharePublication = liveKitState.screenShareTrackRefs[0]
      ?.publication as InstanceType<
      typeof liveKitState.MockRemoteTrackPublication
    >;
    expect(
      view.container.querySelector(
        'video[aria-label="Screen shared by Learner 5"]',
      ),
    ).toBeInTheDocument();
    expect(sharePublication.isDesired).toBe(true);
    expect(sharePublication.videoQuality).toBe("high");

    act(() => vi.advanceTimersByTime(5_000));
    expect(
      view.container.querySelector(
        'video[aria-label="Screen shared by Learner 5"]',
      ),
    ).not.toBeInTheDocument();
    expect(sharePublication.isDesired).toBe(false);
  });

  it("uses the full camera budget when an active share is not on stage", async () => {
    liveKitState.screenShareTrackRefs = [
      {
        participant: liveKitState.cameraTrackRefs[4]!.participant,
        publication: new liveKitState.MockRemoteTrackPublication(),
        source: "screen_share",
      },
    ];
    const view = renderShell();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Presentation" }),
      ).toHaveAttribute("aria-pressed", "true"),
    );

    fireEvent.click(screen.getByRole("button", { name: "Grid" }));

    expect(
      view.container.querySelectorAll(".media-p405-grid video"),
    ).toHaveLength(12);
  });

  it("invalidates a pending media operation on leave while still mounted", async () => {
    const deferred = createDeferred<void>();
    liveKitState.localParticipant.setCameraEnabled.mockReturnValueOnce(
      deferred.promise,
    );
    const onLeave = vi.fn();
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    const view = renderShell({ onLeave });

    fireEvent.click(screen.getByRole("button", { name: "Turn camera on" }));
    expect(
      screen.getByRole("button", { name: "Turn microphone on" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Devices" })).toHaveAttribute(
      "tabindex",
      "0",
    );
    fireEvent.click(screen.getByRole("button", { name: "Leave room" }));
    fireEvent.click(screen.getByRole("button", { name: "Leave classroom" }));
    expect(onLeave).toHaveBeenCalledTimes(1);

    await act(async () => {
      deferred.reject(new Error("late device failure"));
      await deferred.promise.catch(() => undefined);
    });

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(consoleError).not.toHaveBeenCalled();
    view.unmount();
    consoleError.mockRestore();
  });

  it("hard-stops media when a camera operation resolves after leave", async () => {
    const deferred = createDeferred<void>();
    liveKitState.localParticipant.setCameraEnabled.mockReturnValueOnce(
      deferred.promise,
    );
    const view = renderShell();

    fireEvent.click(screen.getByRole("button", { name: "Turn camera on" }));
    fireEvent.click(screen.getByRole("button", { name: "Leave room" }));
    fireEvent.click(screen.getByRole("button", { name: "Leave classroom" }));
    await act(async () => {
      deferred.resolve();
      await deferred.promise;
    });

    expect(liveKitState.localParticipant.setCameraEnabled).toHaveBeenCalledWith(
      true,
    );
    expect(liveKitState.terminalMediaCleanup).toHaveBeenCalledTimes(1);
    view.unmount();
  });

  it("hard-stops screen capture that resolves after terminal abort", async () => {
    const deferred = createDeferred<void>();
    const controlAbortController = new AbortController();
    liveKitState.localParticipant.setScreenShareEnabled.mockReturnValueOnce(
      deferred.promise,
    );
    renderShell({ controlAbortSignal: controlAbortController.signal });

    fireEvent.click(screen.getByRole("button", { name: "Share screen" }));
    act(() => controlAbortController.abort());
    await act(async () => {
      deferred.resolve();
      await deferred.promise;
    });

    expect(
      liveKitState.localParticipant.setScreenShareEnabled,
    ).toHaveBeenCalledWith(true);
    expect(liveKitState.terminalMediaCleanup).toHaveBeenCalledTimes(1);
  });

  it("hard-stops capture after a late camera device switch", async () => {
    const deferred = createDeferred<void>();
    liveKitState.deviceSetters.videoinput.mockReturnValueOnce(deferred.promise);
    renderShell();

    fireEvent.click(screen.getByRole("button", { name: "Devices" }));
    fireEvent.change(screen.getByLabelText("Active camera"), {
      target: { value: "default" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Leave room" }));
    fireEvent.click(screen.getByRole("button", { name: "Leave classroom" }));
    await act(async () => {
      deferred.resolve();
      await deferred.promise;
    });

    expect(liveKitState.deviceSetters.videoinput).toHaveBeenCalledTimes(1);
    expect(liveKitState.terminalMediaCleanup).toHaveBeenCalledTimes(1);
  });

  it("invalidates a pending media operation when the room enters a terminal state", async () => {
    const deferred = createDeferred<void>();
    const controlAbortController = new AbortController();
    liveKitState.localParticipant.setCameraEnabled.mockReturnValueOnce(
      deferred.promise,
    );
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    renderShell({ controlAbortSignal: controlAbortController.signal });

    fireEvent.click(screen.getByRole("button", { name: "Turn camera on" }));
    act(() => controlAbortController.abort());

    expect(
      screen.getByRole("button", { name: "Turn camera on" }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: "Devices" })).toBeDisabled();
    await act(async () => {
      deferred.reject(new Error("late terminal device failure"));
      await deferred.promise.catch(() => undefined);
    });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(consoleError).not.toHaveBeenCalled();
    consoleError.mockRestore();
  });

  it("hard-stops a media operation that resolves after unmount", async () => {
    const deferred = createDeferred<void>();
    liveKitState.localParticipant.setCameraEnabled.mockReturnValueOnce(
      deferred.promise,
    );
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);
    const view = renderShell();

    fireEvent.click(screen.getByRole("button", { name: "Turn camera on" }));
    view.unmount();
    await act(async () => {
      deferred.resolve();
      await deferred.promise;
    });

    expect(liveKitState.terminalMediaCleanup).toHaveBeenCalledTimes(1);
    expect(consoleError).not.toHaveBeenCalled();
    consoleError.mockRestore();
  });

  it("cancels queued focus work when the shell unmounts", () => {
    vi.useFakeTimers();
    const focus = vi.spyOn(HTMLElement.prototype, "focus");
    const view = renderShell();
    fireEvent.click(screen.getByRole("button", { name: "Next video page" }));
    fireEvent.click(screen.getByRole("button", { name: "Next video page" }));

    liveKitState.cameraTrackRefs = createCameraTracks(5);
    view.rerender(shellElement());
    const focusCallsBeforeUnmount = focus.mock.calls.length;
    view.unmount();
    act(() => vi.runOnlyPendingTimers());

    expect(focus).toHaveBeenCalledTimes(focusCallsBeforeUnmount);
    focus.mockRestore();
  });
});

function renderShell(
  overrides: Partial<Parameters<typeof ClassroomMediaShell>[0]> = {},
) {
  return render(shellElement(overrides));
}

function shellElement(
  overrides: Partial<Parameters<typeof ClassroomMediaShell>[0]> = {},
) {
  return (
    <I18nProvider initialLanguage="en">
      <ClassroomMediaShell
        canPublishCameraMicrophone
        canShareScreen
        canSubscribe
        connectionStatus="connected"
        onLeave={vi.fn()}
        onTerminalMediaCleanup={liveKitState.terminalMediaCleanup}
        {...overrides}
      />
    </I18nProvider>
  );
}

function createCameraTracks(count: number): MockTrackReference[] {
  return Array.from({ length: count }, (_, index) => ({
    participant: {
      identity: `provider-participant-${index + 1}`,
      isLocal: index === 0,
      joinedAt: new Date(1_000 + index),
      name: `Learner ${index + 1}`,
    },
    publication:
      index === 0 ? {} : new liveKitState.MockRemoteTrackPublication(),
    source: "camera",
  }));
}

function createAudioTracks(count: number): MockTrackReference[] {
  return Array.from({ length: count }, (_, index) => ({
    participant: {
      identity: `provider-participant-${index + 1}`,
      isLocal: false,
      joinedAt: new Date(1_000 + index),
      name: `Learner ${index + 1}`,
    },
    publication: new liveKitState.MockRemoteTrackPublication(),
    source: "microphone",
  }));
}

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}
