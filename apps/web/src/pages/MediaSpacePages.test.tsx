// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { CurrentUser, MediaSpace } from "@tutorhub/api-client";
import { StrictMode } from "react";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "../app/i18n";
import {
  clearMediaRoomEscrow,
  finalizeMediaRoomEscrowClaim,
  putMediaRoomEscrow,
  takeMediaRoomEscrow,
} from "../app/mediaPrejoin";
import { SessionProvider } from "../app/session";
import { MediaSpacePreJoinPage, MediaSpaceRoomPage } from "./MediaSpacePages";

const apiMocks = vi.hoisted(() => ({
  cancelJoinAttempt: vi.fn(),
  createJoinAttempt: vi.fn(),
  getJoinAttempt: vi.fn(),
  getMediaSpace: vi.fn(),
  issueJoinCredential: vi.fn(),
  rotateCSRFToken: vi.fn(),
}));

const liveKitMocks = vi.hoisted(() => ({
  roomDisconnect: vi.fn().mockResolvedValue(undefined),
  roomRender: vi.fn(),
  startAudioRender: vi.fn(),
  switchActiveDevice: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@tutorhub/api-client", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@tutorhub/api-client")>();
  return {
    ...original,
    cancelMediaJoinAttempt: apiMocks.cancelJoinAttempt,
    createMediaSpaceJoinAttempt: apiMocks.createJoinAttempt,
    getMediaJoinAttempt: apiMocks.getJoinAttempt,
    getMediaSpace: apiMocks.getMediaSpace,
    issueMediaSpaceJoinCredential: apiMocks.issueJoinCredential,
    rotateCSRFToken: apiMocks.rotateCSRFToken,
  };
});

vi.mock("@livekit/components-react", () => ({
  LiveKitRoom: (props: { children?: React.ReactNode }) => {
    liveKitMocks.roomRender(props);
    return props.children ?? null;
  },
  RoomAudioRenderer: () => null,
  StartAudio: (props: { label: string }) => {
    liveKitMocks.startAudioRender(props);
    return <button type="button">{props.label}</button>;
  },
}));

vi.mock("livekit-client", () => ({
  Room: class MockRoom {
    disconnect = liveKitMocks.roomDisconnect;
    switchActiveDevice = liveKitMocks.switchActiveDevice;
  },
}));

const tenantID = "4b18543a-74de-419f-9fe8-d0c3dfc991eb";
const userID = "1d7d65eb-904e-4a0d-bd24-a8ec1b453d64";
const spaceID = "c2dc1048-1d90-4c90-ae50-5fb436bfb607";
const otherSpaceID = "595666d8-969e-49cb-bb12-25d624d1aad6";
const sessionID = "8477ee76-c4aa-431f-bb65-405f4b6575c9";
const roomInstanceID = "c5f918a5-a09e-4f94-9fab-fb0ab5702a4d";
const participantSessionID = "f680fd29-c7f1-4083-af9b-52ad1db14ba9";
const joinAttemptID = "a860f06d-34f9-4c57-89f8-1541bfb3b6d7";

const currentUser: CurrentUser = {
  user: {
    id: userID,
    email: "student@example.test",
    display_name: "Student One",
    locale: "en",
    timezone: "Asia/Ho_Chi_Minh",
  },
  active_tenant: {
    id: tenantID,
    slug: "tutorhub-test",
    name: "TutorHub Test",
    role: "student",
    is_active: true,
    status: "active",
    version: 1,
  },
  memberships: [],
  permissions: ["session.join"],
};

const mediaSpace: MediaSpace = {
  id: spaceID,
  source: { kind: "class_session", class_session_id: sessionID },
  status: "open",
  version: 4,
  active_room_instance: {
    id: roomInstanceID,
    status: "active",
    version: 2,
    created_at: "2030-08-03T00:00:00Z",
    updated_at: "2030-08-03T00:01:00Z",
  },
  viewer_operations: {
    can_start: false,
    can_end: false,
    can_cancel: false,
    can_manage_admissions: false,
    can_manage_invites: false,
  },
  created_at: "2030-08-03T00:00:00Z",
  updated_at: "2030-08-03T00:01:00Z",
};

type FakeTrack = Omit<MediaStreamTrack, "stop"> & {
  stop: ReturnType<typeof vi.fn>;
};

function fakeTrack(kind: "audio" | "video", deviceId: string): FakeTrack {
  return {
    contentHint: "",
    getSettings: () =>
      kind === "audio"
        ? {
            autoGainControl: true,
            deviceId,
            echoCancellation: true,
            noiseSuppression: true,
          }
        : { deviceId },
    kind,
    stop: vi.fn(),
  } as unknown as FakeTrack;
}

function fakeStream() {
  const audio = fakeTrack("audio", "microphone-1");
  const video = fakeTrack("video", "camera-1");
  const stream = {
    getAudioTracks: () => [audio],
    getTracks: () => [audio, video],
    getVideoTracks: () => [video],
  } as unknown as MediaStream;
  return { audio, stream, video };
}

function device(
  kind: MediaDeviceKind,
  deviceId: string,
  label: string,
): MediaDeviceInfo {
  return {
    deviceId,
    groupId: `${deviceId}-group`,
    kind,
    label,
    toJSON: () => ({ deviceId, kind, label }),
  };
}

function fakeMediaDevices(stream: MediaStream) {
  const events = new EventTarget();
  return {
    addEventListener: events.addEventListener.bind(events),
    dispatchEvent: events.dispatchEvent.bind(events),
    enumerateDevices: vi
      .fn()
      .mockResolvedValue([
        device("audioinput", "microphone-1", "Class microphone"),
        device("videoinput", "camera-1", "Class camera"),
        device("audiooutput", "speaker-1", "Class speaker"),
      ]),
    getSupportedConstraints: () => ({
      autoGainControl: true,
      echoCancellation: true,
      noiseSuppression: true,
    }),
    getUserMedia: vi.fn().mockResolvedValue(stream),
    removeEventListener: events.removeEventListener.bind(events),
  } as unknown as MediaDevices & {
    enumerateDevices: ReturnType<typeof vi.fn>;
    getUserMedia: ReturnType<typeof vi.fn>;
  };
}

function LocationProbe() {
  const location = useLocation();
  return (
    <>
      <output data-testid="route-path">{location.pathname}</output>
      <output data-testid="route-state">
        {JSON.stringify(location.state)}
      </output>
    </>
  );
}

function RoomScopeNavigation() {
  const navigate = useNavigate();
  return (
    <nav aria-label="Test room scope">
      <button
        onClick={() =>
          void navigate(
            `/app/media/spaces/${otherSpaceID}/instances/${roomInstanceID}/room`,
          )
        }
        type="button"
      >
        Open other room scope
      </button>
      <button
        onClick={() =>
          void navigate(
            `/app/media/spaces/${spaceID}/instances/${roomInstanceID}/room`,
          )
        }
        type="button"
      >
        Return to original room scope
      </button>
    </nav>
  );
}

function renderPrejoin(mediaDevices: MediaDevices) {
  vi.stubGlobal("navigator", {
    mediaDevices,
    onLine: true,
  });
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ status: "ok" }), {
        headers: { "Content-Type": "application/json" },
        status: 200,
      }),
    ),
  );
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLanguage="en">
        <SessionProvider mode={{ kind: "static", currentUser }}>
          <MemoryRouter
            initialEntries={[`/app/media/spaces/${spaceID}/prejoin`]}
          >
            <Routes>
              <Route
                element={<MediaSpacePreJoinPage />}
                path="/app/media/spaces/:spaceId/prejoin"
              />
              <Route
                element={<p>Canonical room destination</p>}
                path="/app/media/spaces/:spaceId/instances/:roomInstanceId/room"
              />
            </Routes>
            <LocationProbe />
          </MemoryRouter>
        </SessionProvider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function renderCanonicalRoom() {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <I18nProvider initialLanguage="en">
          <SessionProvider mode={{ kind: "static", currentUser }}>
            <MemoryRouter
              initialEntries={[
                `/app/media/spaces/${spaceID}/instances/${roomInstanceID}/room`,
              ]}
            >
              <Routes>
                <Route
                  element={<MediaSpaceRoomPage />}
                  path="/app/media/spaces/:spaceId/instances/:roomInstanceId/room"
                />
              </Routes>
              <RoomScopeNavigation />
            </MemoryRouter>
          </SessionProvider>
        </I18nProvider>
      </QueryClientProvider>
    </StrictMode>,
  );
}

function putCanonicalRoomEscrow(
  choices: {
    audioEnabled: boolean;
    videoEnabled: boolean;
    audioDeviceId: string;
    videoDeviceId: string;
    speakerDeviceId: string;
    audioMode: "speech" | "original_sound";
  } = {
    audioEnabled: false,
    videoEnabled: false,
    audioDeviceId: "",
    videoDeviceId: "",
    speakerDeviceId: "",
    audioMode: "speech",
  },
) {
  putMediaRoomEscrow({
    scope: {
      tenantId: tenantID,
      userId: userID,
      spaceId: spaceID,
      roomInstanceId: roomInstanceID,
    },
    credential: {
      access_token: "strict-mode-memory-token",
      server_url: "wss://media.example.test",
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      join_attempt_id: joinAttemptID,
      instance_role: "attendee",
      can_publish_camera_microphone: true,
      can_share_screen: false,
      can_subscribe: true,
      expires_at: "2030-08-03T00:05:00Z",
    },
    choices,
  });
}

describe("MediaSpacePreJoinPage P4-03 boundaries", () => {
  beforeEach(() => {
    clearMediaRoomEscrow();
    apiMocks.getMediaSpace.mockResolvedValue(mediaSpace);
    apiMocks.getJoinAttempt.mockResolvedValue({
      admission_request_id: "d48a301d-c468-4f65-8da2-029fc379ee74",
      admission_version: 1,
      join_attempt_id: joinAttemptID,
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      status: "waiting",
      version: 1,
    });
    apiMocks.rotateCSRFToken.mockResolvedValue({ csrf_token: "media-csrf" });
    vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
    vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(
      () => undefined,
    );
  });

  afterEach(() => {
    cleanup();
    clearMediaRoomEscrow();
    window.localStorage.clear();
    window.sessionStorage.clear();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    apiMocks.createJoinAttempt.mockReset();
    apiMocks.cancelJoinAttempt.mockReset();
    apiMocks.getJoinAttempt.mockReset();
    apiMocks.getMediaSpace.mockReset();
    apiMocks.issueJoinCredential.mockReset();
    apiMocks.rotateCSRFToken.mockReset();
    liveKitMocks.roomRender.mockReset();
    liveKitMocks.roomDisconnect.mockClear();
    liveKitMocks.startAudioRender.mockReset();
    liveKitMocks.switchActiveDevice.mockClear();
  });

  it("does not capture media or create a join attempt during initial render", async () => {
    const media = fakeMediaDevices(fakeStream().stream);
    renderPrejoin(media);

    expect(
      await screen.findByRole("heading", { name: "Get ready for class" }),
    ).toBeInTheDocument();
    expect(media.getUserMedia).not.toHaveBeenCalled();
    expect(media.enumerateDevices).not.toHaveBeenCalled();
    expect(apiMocks.rotateCSRFToken).not.toHaveBeenCalled();
    expect(apiMocks.createJoinAttempt).not.toHaveBeenCalled();
    expect(apiMocks.issueJoinCredential).not.toHaveBeenCalled();
    expect(liveKitMocks.roomRender).not.toHaveBeenCalled();
  });

  it("starts one local preview only after the explicit device-check action", async () => {
    const media = fakeMediaDevices(fakeStream().stream);
    renderPrejoin(media);
    await screen.findByRole("heading", { name: "Get ready for class" });

    fireEvent.click(
      screen.getByRole("button", { name: "Test camera and microphone" }),
    );

    await waitFor(() => expect(media.getUserMedia).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(media.enumerateDevices).toHaveBeenCalledTimes(1),
    );
    expect(screen.getByRole("combobox", { name: "Microphone" })).toHaveValue(
      "microphone-1",
    );
    expect(screen.getByRole("combobox", { name: "Camera" })).toHaveValue(
      "camera-1",
    );
    expect(media.getUserMedia).toHaveBeenCalledWith({
      audio: {
        autoGainControl: { ideal: true },
        echoCancellation: { ideal: true },
        noiseSuppression: { ideal: true },
      },
      video: {},
    });
    expect(apiMocks.createJoinAttempt).not.toHaveBeenCalled();
    expect(apiMocks.issueJoinCredential).not.toHaveBeenCalled();
  });

  it("keeps listen-only waiting outside LiveKit and does not request a credential", async () => {
    const media = fakeMediaDevices(fakeStream().stream);
    apiMocks.createJoinAttempt.mockResolvedValue({
      admission_request_id: "d48a301d-c468-4f65-8da2-029fc379ee74",
      admission_version: 1,
      join_attempt_id: joinAttemptID,
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      status: "waiting",
      version: 1,
    });
    renderPrejoin(media);
    await screen.findByRole("heading", { name: "Get ready for class" });

    fireEvent.click(screen.getByRole("button", { name: "Join listen-only" }));

    expect(
      await screen.findByRole("heading", { name: "Waiting to be admitted" }),
    ).toBeInTheDocument();
    expect(media.getUserMedia).not.toHaveBeenCalled();
    expect(apiMocks.createJoinAttempt).toHaveBeenCalledTimes(1);
    expect(apiMocks.issueJoinCredential).not.toHaveBeenCalled();
    expect(liveKitMocks.roomRender).not.toHaveBeenCalled();
    expect(screen.getByTestId("route-path")).toHaveTextContent(
      `/app/media/spaces/${spaceID}/prejoin`,
    );
  });

  it("renders an immediate terminal provider projection without requesting a credential", async () => {
    const media = fakeMediaDevices(fakeStream().stream);
    apiMocks.createJoinAttempt.mockResolvedValue({
      join_attempt_id: joinAttemptID,
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      status: "provider_unavailable",
      version: 1,
    });
    renderPrejoin(media);
    await screen.findByRole("heading", { name: "Get ready for class" });

    fireEvent.click(screen.getByRole("button", { name: "Join listen-only" }));

    const terminalHeading = await screen.findByRole("heading", {
      name: "The media service is unavailable",
    });
    expect(terminalHeading).toHaveFocus();
    expect(apiMocks.issueJoinCredential).not.toHaveBeenCalled();
    expect(liveKitMocks.roomRender).not.toHaveBeenCalled();
  });

  it.each([
    {
      status: "timeout" as const,
      heading: "Your join request expired",
      canCreateNewRequest: true,
    },
    {
      status: "meeting_ended" as const,
      heading: "The classroom session ended",
      canCreateNewRequest: false,
    },
  ])(
    "renders the $status terminal projection with the bounded recovery action",
    async ({ status, heading, canCreateNewRequest }) => {
      const media = fakeMediaDevices(fakeStream().stream);
      apiMocks.createJoinAttempt.mockResolvedValue({
        join_attempt_id: joinAttemptID,
        participant_session_id: participantSessionID,
        room_instance_id: roomInstanceID,
        status,
        version: 1,
      });
      renderPrejoin(media);
      await screen.findByRole("heading", { name: "Get ready for class" });

      fireEvent.click(screen.getByRole("button", { name: "Join listen-only" }));

      const terminalHeading = await screen.findByRole("heading", {
        name: heading,
      });
      expect(terminalHeading).toHaveFocus();
      const newRequest = screen.queryByRole("button", {
        name: "Create a new join request",
      });
      if (canCreateNewRequest) {
        expect(newRequest).toBeVisible();
        fireEvent.click(newRequest!);
        expect(
          screen.getByRole("button", { name: "Join listen-only" }),
        ).toBeEnabled();
      } else {
        expect(newRequest).not.toBeInTheDocument();
      }
      expect(apiMocks.issueJoinCredential).not.toHaveBeenCalled();
      expect(liveKitMocks.roomRender).not.toHaveBeenCalled();
    },
  );

  it("retains the original device choices while admission polling completes", async () => {
    const token = "admitted-after-wait-token";
    const media = fakeMediaDevices(fakeStream().stream);
    const waitingAttempt = {
      admission_request_id: "d48a301d-c468-4f65-8da2-029fc379ee74",
      admission_version: 1,
      join_attempt_id: joinAttemptID,
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      status: "waiting" as const,
      version: 1,
      expires_at: "2030-08-03T00:07:00Z",
    };
    apiMocks.createJoinAttempt.mockResolvedValue(waitingAttempt);
    apiMocks.getJoinAttempt
      .mockResolvedValueOnce(waitingAttempt)
      .mockResolvedValue({ ...waitingAttempt, status: "admitted", version: 2 });
    apiMocks.issueJoinCredential.mockResolvedValue({
      access_token: token,
      server_url: "wss://media.example.test",
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      join_attempt_id: joinAttemptID,
      instance_role: "attendee",
      can_publish_camera_microphone: true,
      can_share_screen: false,
      can_subscribe: true,
      expires_at: "2030-08-03T00:05:00Z",
    });
    renderPrejoin(media);
    await screen.findByRole("heading", { name: "Get ready for class" });
    fireEvent.click(
      screen.getByRole("button", { name: "Test camera and microphone" }),
    );
    await waitFor(() => expect(media.getUserMedia).toHaveBeenCalledTimes(1));
    fireEvent.click(
      screen.getByRole("button", { name: "Join with enabled devices" }),
    );
    await screen.findByRole("heading", { name: "Waiting to be admitted" });
    expect(screen.getByText(/waiting request expires at/i)).toBeVisible();
    await waitFor(() =>
      expect(apiMocks.getJoinAttempt).toHaveBeenCalledTimes(1),
    );
    const refresh = screen.getByRole("button", {
      name: "Check admission status",
    });
    await waitFor(() => expect(refresh).toBeEnabled());
    fireEvent.click(refresh);

    await screen.findByText("Canonical room destination");
    const handoff = takeMediaRoomEscrow({
      tenantId: tenantID,
      userId: userID,
      spaceId: spaceID,
      roomInstanceId: roomInstanceID,
    });
    expect(handoff).toMatchObject({
      choices: {
        audioEnabled: true,
        videoEnabled: true,
        audioDeviceId: "microphone-1",
        videoDeviceId: "camera-1",
      },
      credential: { access_token: token },
    });
  });

  it("cancels a waiting request without minting a credential", async () => {
    const media = fakeMediaDevices(fakeStream().stream);
    const waitingAttempt = {
      admission_request_id: "d48a301d-c468-4f65-8da2-029fc379ee74",
      admission_version: 1,
      join_attempt_id: joinAttemptID,
      participant_session_id: participantSessionID,
      room_instance_id: roomInstanceID,
      status: "waiting" as const,
      version: 1,
    };
    apiMocks.createJoinAttempt.mockResolvedValue(waitingAttempt);
    apiMocks.cancelJoinAttempt.mockResolvedValue({
      ...waitingAttempt,
      status: "cancelled",
      version: 2,
    });
    renderPrejoin(media);
    await screen.findByRole("heading", { name: "Get ready for class" });
    fireEvent.click(screen.getByRole("button", { name: "Join listen-only" }));
    await screen.findByRole("heading", { name: "Waiting to be admitted" });
    fireEvent.click(screen.getByRole("button", { name: "Leave waiting room" }));

    const cancelledHeading = await screen.findByRole("heading", {
      name: "You left the waiting room",
    });
    expect(cancelledHeading).toBeVisible();
    expect(cancelledHeading).toHaveFocus();
    expect(apiMocks.cancelJoinAttempt).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      joinAttemptID,
      expect.objectContaining({
        expected_space_version: 4,
        expected_room_instance_id: roomInstanceID,
        expected_room_instance_version: 2,
        expected_admission_version: 1,
      }),
      "media-csrf",
    );
    expect(apiMocks.issueJoinCredential).not.toHaveBeenCalled();
    expect(liveKitMocks.roomRender).not.toHaveBeenCalled();
  });

  it("requests an admitted attempt before the credential and hands the token off outside history state", async () => {
    const token = "memory-only-livekit-token";
    const order: string[] = [];
    const media = fakeMediaDevices(fakeStream().stream);
    apiMocks.rotateCSRFToken.mockImplementation(async () => {
      order.push("csrf");
      return { csrf_token: "media-csrf" };
    });
    apiMocks.createJoinAttempt.mockImplementation(async () => {
      order.push("attempt");
      return {
        admission_request_id: null,
        join_attempt_id: joinAttemptID,
        participant_session_id: participantSessionID,
        room_instance_id: roomInstanceID,
        status: "admitted",
        version: 1,
      };
    });
    apiMocks.issueJoinCredential.mockImplementation(async () => {
      order.push("credential");
      return {
        access_token: token,
        server_url: "wss://media.example.test",
        participant_session_id: participantSessionID,
        room_instance_id: roomInstanceID,
        join_attempt_id: joinAttemptID,
        instance_role: "attendee",
        can_publish_camera_microphone: true,
        can_share_screen: false,
        can_subscribe: true,
        expires_at: "2030-08-03T00:05:00Z",
      };
    });
    renderPrejoin(media);
    await screen.findByRole("heading", { name: "Get ready for class" });

    fireEvent.click(screen.getByRole("button", { name: "Join listen-only" }));

    expect(
      await screen.findByText("Canonical room destination"),
    ).toBeInTheDocument();
    expect(order).toEqual(["csrf", "attempt", "credential"]);
    expect(apiMocks.createJoinAttempt).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      expect.objectContaining({
        expected_room_instance_id: roomInstanceID,
        expected_space_version: mediaSpace.version,
      }),
      "media-csrf",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(apiMocks.issueJoinCredential).toHaveBeenCalledWith(
      tenantID,
      spaceID,
      { join_attempt_id: joinAttemptID },
      "media-csrf",
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
    expect(screen.getByTestId("route-state")).toHaveTextContent("null");
    expect(screen.getByTestId("route-path")).toHaveTextContent(
      `/app/media/spaces/${spaceID}/instances/${roomInstanceID}/room`,
    );
    expect(window.location.href).not.toContain(token);
    expect(JSON.stringify(window.history.state ?? null)).not.toContain(token);
    expect(window.localStorage).toHaveLength(0);
    expect(window.sessionStorage).toHaveLength(0);
    expect(liveKitMocks.roomRender).not.toHaveBeenCalled();

    const handoff = takeMediaRoomEscrow({
      tenantId: tenantID,
      userId: userID,
      spaceId: spaceID,
      roomInstanceId: roomInstanceID,
    });
    expect(handoff).toMatchObject({
      choices: { audioEnabled: false, videoEnabled: false },
      credential: { access_token: token },
    });
    finalizeMediaRoomEscrowClaim({
      tenantId: tenantID,
      userId: userID,
      spaceId: spaceID,
      roomInstanceId: roomInstanceID,
    });
    expect(
      takeMediaRoomEscrow({
        tenantId: tenantID,
        userId: userID,
        spaceId: spaceID,
        roomInstanceId: roomInstanceID,
      }),
    ).toBeNull();
  });

  it("survives StrictMode handoff and applies the original-sound profile to LiveKit", async () => {
    const token = "strict-mode-memory-token";
    putCanonicalRoomEscrow({
      audioEnabled: true,
      videoEnabled: false,
      audioDeviceId: "microphone-1",
      videoDeviceId: "",
      speakerDeviceId: "speaker-1",
      audioMode: "original_sound",
    });

    renderCanonicalRoom();

    expect(
      await screen.findByRole("heading", { name: "TutorHub classroom" }),
    ).toBeInTheDocument();
    const liveKitProps = liveKitMocks.roomRender.mock.lastCall?.[0] as
      | {
          token?: string;
          audio?: unknown;
        }
      | undefined;
    expect(liveKitProps).toMatchObject({
      token,
      connect: true,
      audio: {
        deviceId: "microphone-1",
        echoCancellation: { ideal: false },
        noiseSuppression: { ideal: false },
        autoGainControl: { ideal: false },
      },
    });
    expect(screen.getByRole("main")).toHaveClass("media-p403-room");
    expect(
      screen.getByRole("button", { name: "Enable classroom audio" }),
    ).toBeInTheDocument();
    expect(liveKitMocks.startAudioRender).toHaveBeenCalled();
    const stableCallbacks = liveKitMocks.roomRender.mock.lastCall?.[0] as
      | {
          onConnected?: () => void;
          onDisconnected?: () => void;
          onError?: () => void;
        }
      | undefined;
    act(() => stableCallbacks?.onConnected?.());
    const rerenderedCallbacks = liveKitMocks.roomRender.mock.lastCall?.[0] as
      | {
          onConnected?: () => void;
          onDisconnected?: () => void;
          onError?: () => void;
        }
      | undefined;
    expect(rerenderedCallbacks?.onConnected).toBe(stableCallbacks?.onConnected);
    expect(rerenderedCallbacks?.onDisconnected).toBe(
      stableCallbacks?.onDisconnected,
    );
    expect(rerenderedCallbacks?.onError).toBe(stableCallbacks?.onError);
    expect(liveKitMocks.switchActiveDevice).toHaveBeenCalledWith(
      "audiooutput",
      "speaker-1",
    );
    expect(
      takeMediaRoomEscrow({
        tenantId: tenantID,
        userId: userID,
        spaceId: spaceID,
        roomInstanceId: roomInstanceID,
      }),
    ).toBeNull();
  });

  it("terminates the room handoff after disconnect without reconnecting", async () => {
    putCanonicalRoomEscrow();
    renderCanonicalRoom();

    await screen.findByRole("heading", { name: "TutorHub classroom" });
    const before = liveKitMocks.roomRender.mock.calls.length;
    const props = liveKitMocks.roomRender.mock.lastCall?.[0] as
      { onDisconnected?: () => void } | undefined;
    expect(props?.onDisconnected).toBeTypeOf("function");

    act(() => props?.onDisconnected?.());

    expect(
      await screen.findByRole("heading", {
        name: "Return to the prejoin check",
      }),
    ).toBeInTheDocument();
    expect(screen.getByRole("main")).toHaveClass("media-p403-room-recovery");
    expect(liveKitMocks.roomRender.mock.calls.length).toBe(before);
    expect(
      takeMediaRoomEscrow({
        tenantId: tenantID,
        userId: userID,
        spaceId: spaceID,
        roomInstanceId: roomInstanceID,
      }),
    ).toBeNull();
  });

  it("purges the in-memory credential across an A to B to A route scope change", async () => {
    putCanonicalRoomEscrow();
    renderCanonicalRoom();

    await screen.findByRole("heading", { name: "TutorHub classroom" });
    const initialRoomRenders = liveKitMocks.roomRender.mock.calls.length;
    fireEvent.click(
      screen.getByRole("button", { name: "Open other room scope" }),
    );
    expect(
      await screen.findByRole("heading", {
        name: "Return to the prejoin check",
      }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Return to original room scope" }),
    );

    expect(
      await screen.findByRole("heading", {
        name: "Return to the prejoin check",
      }),
    ).toBeInTheDocument();
    expect(liveKitMocks.roomRender.mock.calls.length).toBe(initialRoomRenders);
    expect(
      takeMediaRoomEscrow({
        tenantId: tenantID,
        userId: userID,
        spaceId: spaceID,
        roomInstanceId: roomInstanceID,
      }),
    ).toBeNull();
  });
});
