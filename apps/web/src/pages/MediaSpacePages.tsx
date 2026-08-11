import {
  APIRequestError,
  createMediaSpaceJoinAttempt,
  getMediaSpace,
  issueMediaSpaceJoinCredential,
  rotateCSRFToken,
} from "@tutorhub/api-client";
import { useQuery } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type FormEvent,
} from "react";
import { Link, useNavigate, useParams } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  clearMediaRoomEscrow,
  MediaPrejoinController,
  probeMediaNetwork,
  putMediaRoomEscrow,
  type MediaJoinChoices,
  type MediaJoinAttemptProjection,
  type MediaNetworkStatus,
  type MediaPrejoinErrorCode,
  type MediaPrejoinSnapshot,
} from "../app/mediaPrejoin";
import {
  mediaLobbyIdempotencyKey,
  useCancelMediaJoinAttempt,
  useMediaJoinAttemptStatus,
} from "../app/mediaLobby";
import { useSession } from "../app/session";
import { MediaSpaceInvitePanel } from "../components/MediaSpaceInvitePanel";

type JoinStatus =
  | "idle"
  | "creating_attempt"
  | "waiting"
  | "credential"
  | "denied"
  | "cancelled"
  | "timeout"
  | "meeting_ended"
  | "provider_unavailable"
  | "failed";
interface MediaJoinState {
  scopeKey: string;
  status: JoinStatus;
  errorKey: TranslationKey | null;
  attempt: MediaJoinAttemptProjection | null;
  choices: MediaJoinChoices | null;
}
const unsupportedSnapshot: MediaPrejoinSnapshot = {
  status: "degraded",
  permissionGranted: false,
  microphones: [],
  cameras: [],
  speakers: [],
  selectedMicrophoneId: "",
  selectedCameraId: "",
  selectedSpeakerId: "",
  audioMode: "speech",
  micLevel: 0,
  actualAudioProcessing: {
    echoCancellation: null,
    noiseSuppression: null,
    autoGainControl: null,
  },
  errorCode: "media_capture_unsupported_or_policy_blocked",
  announcement: "",
  speakerTestStatus: "unsupported",
};

export function MediaSpacePreJoinPage() {
  const { spaceId } = useParams();
  const navigate = useNavigate();
  const { language, t } = useI18n();
  const session = useSession();
  const tenantId = session.currentUser?.active_tenant?.id ?? "";
  const userId = session.currentUser?.user.id ?? "";
  const [joinState, setJoinState] = useState<MediaJoinState>({
    scopeKey: "",
    status: "idle",
    errorKey: null,
    attempt: null,
    choices: null,
  });
  const [networkStatus, setNetworkStatus] =
    useState<MediaNetworkStatus>("checking");
  const [networkLatency, setNetworkLatency] = useState<
    "fast" | "moderate" | "slow" | "unknown"
  >("unknown");
  const handoffCreated = useRef(false);
  const terminalHeading = useRef<HTMLHeadingElement>(null);
  const activeJoinRequest = useRef<AbortController | null>(null);
  const cancelCommand = useRef<{ attemptID: string; key: string } | null>(null);
  const joinScopeKey = `${tenantId}\u0000${userId}\u0000${spaceId ?? ""}`;
  const joinStateIsCurrent = joinState.scopeKey === joinScopeKey;
  const currentJoinStatus: JoinStatus = joinStateIsCurrent
    ? joinState.status
    : "idle";
  const currentJoinErrorKey = joinStateIsCurrent ? joinState.errorKey : null;
  const currentJoinAttempt = joinStateIsCurrent ? joinState.attempt : null;

  const supported = Boolean(
    typeof navigator !== "undefined" &&
    navigator.mediaDevices &&
    typeof navigator.mediaDevices.getUserMedia === "function" &&
    typeof navigator.mediaDevices.enumerateDevices === "function",
  );
  const controller = useMemo(() => {
    if (!supported || !spaceId || !tenantId || !userId) {
      return null;
    }
    return new MediaPrejoinController();
  }, [spaceId, supported, tenantId, userId]);
  const snapshot = useSyncExternalStore(
    controller?.subscribe ?? emptySubscribe,
    controller?.getSnapshot ?? getUnsupportedSnapshot,
    getUnsupportedSnapshot,
  );

  useEffect(() => {
    if (!controller) {
      return;
    }
    controller.connect();
    return () => {
      void controller.disconnect();
      if (!handoffCreated.current) {
        clearMediaRoomEscrow();
      }
    };
  }, [controller]);

  useEffect(() => {
    const abort = new AbortController();
    void probeMediaNetwork({ signal: abort.signal }).then((result) => {
      if (!abort.signal.aborted) {
        setNetworkStatus(result.status);
        setNetworkLatency(result.latency);
      }
    });
    return () => abort.abort();
  }, [spaceId, tenantId, userId]);

  useEffect(
    () => () => {
      activeJoinRequest.current?.abort();
      activeJoinRequest.current = null;
      cancelCommand.current = null;
    },
    [spaceId, tenantId, userId],
  );

  const mediaSpace = useQuery({
    enabled: Boolean(spaceId && tenantId),
    queryKey: ["media-space-prejoin", tenantId, spaceId],
    queryFn: ({ signal }) => getMediaSpace(tenantId, spaceId ?? "", { signal }),
    retry: false,
  });
  const projectedRoom = mediaSpace.data?.active_room_instance;
  const joinAttemptStatus = useMediaJoinAttemptStatus(
    tenantId,
    spaceId ?? "",
    currentJoinAttempt?.join_attempt_id ?? "",
    projectedRoom?.id ?? "",
    mediaSpace.data?.version ?? 0,
    projectedRoom?.version ?? 0,
    currentJoinStatus === "waiting",
  );
  const cancelJoinAttempt = useCancelMediaJoinAttempt(tenantId, spaceId ?? "");

  const completeCredentialHandoff = useCallback(
    async (
      attempt: MediaJoinAttemptProjection,
      choices: MediaJoinChoices,
      csrfToken: string,
      abort: AbortController,
    ) => {
      if (
        !spaceId ||
        !tenantId ||
        !userId ||
        (attempt.status !== "admitted" && attempt.status !== "joining")
      ) {
        return;
      }
      setJoinState({
        scopeKey: joinScopeKey,
        status: "credential",
        errorKey: null,
        attempt,
        choices,
      });
      const credential = await issueMediaSpaceJoinCredential(
        tenantId,
        spaceId,
        { join_attempt_id: attempt.join_attempt_id },
        csrfToken,
        { signal: abort.signal },
      );
      if (abort.signal.aborted) {
        return;
      }
      if (
        credential.room_instance_id !== attempt.room_instance_id ||
        credential.participant_session_id !== attempt.participant_session_id ||
        credential.join_attempt_id !== attempt.join_attempt_id ||
        !credential.server_url.startsWith("wss://") ||
        Date.parse(credential.expires_at) <= Date.now()
      ) {
        throw new Error("invalid_media_credential_projection");
      }
      await controller?.stopPreview();
      const effectiveChoices = {
        ...choices,
        audioEnabled:
          choices.audioEnabled && credential.can_publish_camera_microphone,
        videoEnabled:
          choices.videoEnabled && credential.can_publish_camera_microphone,
      };
      putMediaRoomEscrow({
        scope: {
          tenantId,
          userId,
          spaceId,
          roomInstanceId: credential.room_instance_id,
        },
        credential,
        choices: effectiveChoices,
      });
      handoffCreated.current = true;
      void navigate(
        `/app/media/spaces/${spaceId}/instances/${credential.room_instance_id}/room`,
      );
    },
    [controller, joinScopeKey, navigate, spaceId, tenantId, userId],
  );

  const submitJoin = async (choices: MediaJoinChoices) => {
    const room = mediaSpace.data?.active_room_instance;
    if (
      !spaceId ||
      !tenantId ||
      !userId ||
      !room ||
      room.status !== "active" ||
      mediaSpace.data?.status !== "open" ||
      activeJoinRequest.current !== null ||
      currentJoinStatus === "creating_attempt" ||
      currentJoinStatus === "credential"
    ) {
      return;
    }
    const currentAttempt = currentJoinAttempt;
    setJoinState({
      scopeKey: joinScopeKey,
      status: "creating_attempt",
      errorKey: null,
      attempt: currentAttempt,
      choices,
    });
    const attemptId =
      currentAttempt?.join_attempt_id ?? globalThis.crypto.randomUUID();
    const abort = new AbortController();
    activeJoinRequest.current = abort;
    try {
      const csrf = await rotateCSRFToken({ signal: abort.signal });
      if (abort.signal.aborted) {
        return;
      }
      const attempt = (await createMediaSpaceJoinAttempt(
        tenantId,
        spaceId,
        {
          join_attempt_id: attemptId,
          expected_room_instance_id: room.id,
          expected_space_version: mediaSpace.data.version,
        },
        csrf.csrf_token,
        { signal: abort.signal },
      )) as MediaJoinAttemptProjection;
      if (abort.signal.aborted) {
        return;
      }
      if (attempt.status === "waiting") {
        setJoinState({
          scopeKey: joinScopeKey,
          status: "waiting",
          errorKey: null,
          attempt,
          choices,
        });
        return;
      }
      if (isTerminalJoinStatus(attempt.status)) {
        const terminalStatus: TerminalJoinStatus = attempt.status;
        setJoinState({
          scopeKey: joinScopeKey,
          status: terminalStatus,
          errorKey: null,
          attempt,
          choices,
        });
        return;
      }
      await completeCredentialHandoff(attempt, choices, csrf.csrf_token, abort);
    } catch (error) {
      if (abort.signal.aborted) {
        return;
      }
      setJoinState((current) =>
        current.scopeKey === joinScopeKey
          ? {
              ...current,
              status: "failed",
              errorKey: joinProblemKey(error),
            }
          : current,
      );
    } finally {
      if (activeJoinRequest.current === abort) {
        activeJoinRequest.current = null;
      }
    }
  };

  useEffect(() => {
    const attempt = joinAttemptStatus.data;
    const choices =
      currentJoinAttempt?.join_attempt_id === attempt?.join_attempt_id
        ? joinState.choices
        : null;
    if (
      !attempt ||
      !choices ||
      currentJoinStatus !== "waiting" ||
      activeJoinRequest.current !== null
    ) {
      return;
    }
    if (attempt.status === "waiting") {
      setJoinState((current) =>
        current.scopeKey === joinScopeKey &&
        current.attempt?.version !== attempt.version
          ? { ...current, attempt }
          : current,
      );
      return;
    }
    if (isTerminalJoinStatus(attempt.status)) {
      const terminalStatus: TerminalJoinStatus = attempt.status;
      setJoinState((current) =>
        current.scopeKey === joinScopeKey
          ? {
              ...current,
              attempt,
              errorKey: null,
              status: terminalStatus,
            }
          : current,
      );
      return;
    }
    if (attempt.status !== "admitted" && attempt.status !== "joining") {
      return;
    }

    const abort = new AbortController();
    activeJoinRequest.current = abort;
    void (async () => {
      try {
        const csrf = await rotateCSRFToken({ signal: abort.signal });
        if (!abort.signal.aborted) {
          await completeCredentialHandoff(
            attempt,
            choices,
            csrf.csrf_token,
            abort,
          );
        }
      } catch (error) {
        if (!abort.signal.aborted) {
          setJoinState((current) =>
            current.scopeKey === joinScopeKey
              ? {
                  ...current,
                  status: "failed",
                  errorKey: joinProblemKey(error),
                }
              : current,
          );
        }
      } finally {
        if (activeJoinRequest.current === abort) {
          activeJoinRequest.current = null;
        }
      }
    })();
    return () => abort.abort();
  }, [
    completeCredentialHandoff,
    currentJoinAttempt?.join_attempt_id,
    currentJoinStatus,
    joinAttemptStatus.data,
    joinScopeKey,
    joinState.choices,
  ]);

  useLayoutEffect(() => {
    if (isTerminalJoinStatus(currentJoinStatus)) {
      terminalHeading.current?.focus();
    }
  }, [currentJoinStatus]);

  const cancelWaiting = () => {
    const attempt = currentJoinAttempt;
    if (
      !attempt ||
      !attempt.admission_version ||
      !projectedRoom ||
      cancelJoinAttempt.isPending
    ) {
      return;
    }
    if (cancelCommand.current?.attemptID !== attempt.join_attempt_id) {
      cancelCommand.current = {
        attemptID: attempt.join_attempt_id,
        key: mediaLobbyIdempotencyKey("join-cancel"),
      };
    }
    cancelJoinAttempt.mutate(
      {
        attemptID: attempt.join_attempt_id,
        input: {
          expected_space_version: mediaSpace.data?.version ?? 0,
          expected_room_instance_id: attempt.room_instance_id,
          expected_room_instance_version: projectedRoom.version,
          expected_admission_version: attempt.admission_version,
          idempotency_key: cancelCommand.current.key,
        },
      },
      {
        onSuccess: (cancelled) => {
          cancelCommand.current = null;
          setJoinState((current) =>
            current.scopeKey === joinScopeKey
              ? {
                  ...current,
                  attempt: cancelled,
                  errorKey: null,
                  status: "cancelled",
                }
              : current,
          );
        },
      },
    );
  };

  const resetJoinRequest = () => {
    cancelJoinAttempt.reset();
    cancelCommand.current = null;
    setJoinState({
      scopeKey: joinScopeKey,
      status: "idle",
      errorKey: null,
      attempt: null,
      choices: null,
    });
  };

  const recheckTerminalAdmission = () => {
    setJoinState((current) =>
      current.scopeKey === joinScopeKey
        ? { ...current, status: "waiting", errorKey: null }
        : current,
    );
    void joinAttemptStatus.refetch();
  };

  if (!spaceId || !tenantId || !userId) {
    return <MediaSpaceFailure message={t("media.p403.invalidSpace")} />;
  }
  if (mediaSpace.isPending) {
    return (
      <div className="media-p403-page" aria-busy="true">
        <p>{t("media.p403.loadingSpace")}</p>
      </div>
    );
  }
  if (mediaSpace.isError) {
    return (
      <div className="media-p403-page">
        <section className="media-p403-alert" role="alert">
          <h1>{t("media.p403.spaceUnavailable")}</h1>
          <p>{t("media.p403.spaceUnavailableDescription")}</p>
          <button onClick={() => void mediaSpace.refetch()} type="button">
            {t("state.retry")}
          </button>
        </section>
      </div>
    );
  }
  const room = projectedRoom;
  if (mediaSpace.data.status !== "open" || room?.status !== "active") {
    return <MediaSpaceFailure message={t("media.p403.roomNotOpen")} />;
  }
  const previewActive =
    snapshot.status === "preview_ready" ||
    snapshot.status === "switching_device";
  const joinActionLocked =
    currentJoinStatus !== "idle" && currentJoinStatus !== "failed";
  const viewerOperations = mediaP404ViewerOperations(
    mediaSpace.data.viewer_operations,
  );

  return (
    <div className="media-p403-page">
      <Link to="/app/home">{t("media.p403.back")}</Link>
      <header className="media-p403-heading">
        <p>{t("media.prejoin.kicker")}</p>
        <h1>{t("media.p403.title")}</h1>
        <span>{t("media.p403.description")}</span>
      </header>

      <section
        className="media-p403-grid"
        aria-busy={
          currentJoinStatus === "creating_attempt" ||
          currentJoinStatus === "credential"
        }
      >
        <div className="media-p403-preview-panel">
          <div className="media-p403-preview">
            <video
              aria-label={t("media.p403.previewLabel")}
              autoPlay
              muted
              playsInline
              ref={(element) => controller?.attachPreview(element)}
            />
            {!previewActive && (
              <div className="media-p403-preview-placeholder">
                <strong>{t("media.p403.previewOff")}</strong>
                <span>{t("media.p403.previewConsent")}</span>
              </div>
            )}
          </div>

          <div className="media-p403-actions">
            <button
              disabled={
                !controller ||
                snapshot.status === "requesting_permission" ||
                snapshot.status === "switching_device"
              }
              onClick={() => void controller?.startPreview()}
              type="button"
            >
              {snapshot.permissionGranted
                ? t("media.p403.retryPreview")
                : t("media.p403.startPreview")}
            </button>
            {previewActive && (
              <button
                onClick={() => void controller?.stopPreview()}
                type="button"
              >
                {t("media.p403.stopPreview")}
              </button>
            )}
          </div>

          {snapshot.errorCode && (
            <section className="media-p403-alert" role="alert">
              <strong>{t("media.p403.deviceProblem")}</strong>
              <p>{t(deviceErrorKey(snapshot.errorCode))}</p>
            </section>
          )}

          {snapshot.permissionGranted && controller && (
            <DeviceControls controller={controller} snapshot={snapshot} />
          )}
        </div>

        <aside
          className="media-p403-readiness"
          aria-label={t("media.p403.readinessLabel")}
        >
          <h2>{t("media.prejoin.checkHeading")}</h2>
          <dl>
            <div>
              <dt>{t("media.p403.network")}</dt>
              <dd aria-live="polite">
                {t(networkStatusKey(networkStatus))}
                {networkStatus === "ready" && networkLatency !== "unknown"
                  ? ` · ${t(networkLatencyKey(networkLatency))}`
                  : ""}
              </dd>
            </div>
            <div>
              <dt>{t("media.p403.effect")}</dt>
              <dd>{t("media.p403.effectNone")}</dd>
            </div>
          </dl>
          <p>{t("media.p403.networkPrivacy")}</p>

          <div className="media-p403-join-actions">
            <button
              disabled={joinActionLocked}
              onClick={() =>
                void submitJoin(
                  controller?.choices(false) ?? listenOnlyChoices(),
                )
              }
              type="button"
            >
              {currentJoinStatus === "creating_attempt" ||
              currentJoinStatus === "credential"
                ? t("media.prejoin.joining")
                : t("media.p403.joinWithDevices")}
            </button>
            <button
              disabled={joinActionLocked}
              onClick={() =>
                void submitJoin(
                  controller?.choices(true) ?? listenOnlyChoices(),
                )
              }
              type="button"
            >
              {t("media.p403.joinListenOnly")}
            </button>
          </div>
        </aside>
      </section>

      {currentJoinStatus === "waiting" && (
        <section className="media-p403-waiting">
          <h2>{t("media.p403.waitingTitle")}</h2>
          <p aria-live="polite" role="status">
            {t("media.p403.waitingDescription")}
          </p>
          {currentJoinAttempt?.expires_at && (
            <p>
              {t("media.p404.waiting.expiresAt", {
                time: formatLobbyExpiry(
                  currentJoinAttempt.expires_at,
                  language,
                ),
              })}
            </p>
          )}
          <button
            disabled={joinAttemptStatus.isFetching}
            onClick={() => void joinAttemptStatus.refetch()}
            type="button"
          >
            {joinAttemptStatus.isFetching
              ? t("media.p404.waiting.checking")
              : t("media.p403.checkAdmission")}
          </button>
          <button
            disabled={
              cancelJoinAttempt.isPending ||
              !currentJoinAttempt?.admission_version
            }
            onClick={cancelWaiting}
            type="button"
          >
            {cancelJoinAttempt.isPending
              ? t("media.p404.waiting.cancelling")
              : t("media.p404.waiting.cancel")}
          </button>
          {(joinAttemptStatus.isError || cancelJoinAttempt.isError) && (
            <p className="media-p404-error" role="alert">
              {t(
                cancelJoinAttempt.isError
                  ? "media.p404.waiting.cancelError"
                  : "media.p404.waiting.statusError",
              )}
            </p>
          )}
        </section>
      )}

      {isTerminalJoinStatus(currentJoinStatus) && (
        <section className="media-p403-alert" role="alert">
          <h2 ref={terminalHeading} tabIndex={-1}>
            {t(terminalJoinTitleKey(currentJoinStatus))}
          </h2>
          <p>{t(terminalJoinDescriptionKey(currentJoinStatus))}</p>
          <div className="media-p404-terminal-actions">
            {(currentJoinStatus === "cancelled" ||
              currentJoinStatus === "timeout") && (
              <button onClick={resetJoinRequest} type="button">
                {t("media.p404.waiting.newRequest")}
              </button>
            )}
            {(currentJoinStatus === "denied" ||
              currentJoinStatus === "provider_unavailable") && (
              <button onClick={recheckTerminalAdmission} type="button">
                {t("media.p404.waiting.checkAgain")}
              </button>
            )}
          </div>
        </section>
      )}

      {snapshot.announcement && (
        <p className="sr-only" role="status" aria-live="polite">
          {t("media.p403.deviceReset")}
        </p>
      )}

      {currentJoinErrorKey && (
        <section className="media-p403-alert" role="alert">
          <h2>{t("media.prejoin.cannotJoin")}</h2>
          <p>{t(currentJoinErrorKey)}</p>
          <button
            onClick={() => {
              setJoinState((current) =>
                current.scopeKey === joinScopeKey
                  ? { ...current, status: "idle", errorKey: null }
                  : {
                      scopeKey: joinScopeKey,
                      status: "idle",
                      errorKey: null,
                      attempt: null,
                      choices: null,
                    },
              );
            }}
            type="button"
          >
            {t("state.retry")}
          </button>
        </section>
      )}

      <MediaSpaceInvitePanel
        enabled={
          mediaSpace.data.source.kind === "study_meeting" &&
          viewerOperations.canManageInvites
        }
        spaceID={spaceId}
        spaceVersion={mediaSpace.data.version}
        tenantID={tenantId}
      />
    </div>
  );
}

function emptySubscribe(): () => void {
  return () => undefined;
}

function getUnsupportedSnapshot(): MediaPrejoinSnapshot {
  return unsupportedSnapshot;
}

function DeviceControls({
  controller,
  snapshot,
}: {
  controller: MediaPrejoinController;
  snapshot: MediaPrejoinSnapshot;
}) {
  const { t } = useI18n();
  const submitAudioMode = (event: FormEvent<HTMLInputElement>) => {
    controller.setAudioMode(
      event.currentTarget.value === "original_sound"
        ? "original_sound"
        : "speech",
    );
  };
  return (
    <div className="media-p403-device-controls">
      <label>
        <span>{t("media.prejoin.microphone")}</span>
        <select
          onChange={(event) =>
            void controller.switchDevice(
              "audioinput",
              event.currentTarget.value,
            )
          }
          value={snapshot.selectedMicrophoneId}
        >
          {snapshot.microphones.map((item) => (
            <option key={item.deviceId} value={item.deviceId}>
              {item.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        <span>{t("media.prejoin.camera")}</span>
        <select
          onChange={(event) =>
            void controller.switchDevice(
              "videoinput",
              event.currentTarget.value,
            )
          }
          value={snapshot.selectedCameraId}
        >
          {snapshot.cameras.map((item) => (
            <option key={item.deviceId} value={item.deviceId}>
              {item.label}
            </option>
          ))}
        </select>
      </label>
      <label>
        <span>{t("media.p403.speaker")}</span>
        <select
          onChange={(event) =>
            controller.setSpeakerDevice(event.currentTarget.value)
          }
          value={snapshot.selectedSpeakerId}
        >
          <option value="">{t("media.p403.systemDefault")}</option>
          {snapshot.speakers.map((item) => (
            <option key={item.deviceId} value={item.deviceId}>
              {item.label}
            </option>
          ))}
        </select>
      </label>

      <div className="media-p403-meter">
        <label htmlFor="media-p403-mic-level">{t("media.p403.micLevel")}</label>
        <meter
          id="media-p403-mic-level"
          min="0"
          max="1"
          value={snapshot.micLevel}
        />
      </div>

      <fieldset>
        <legend>{t("media.p403.audioMode")}</legend>
        <label>
          <input
            checked={snapshot.audioMode === "speech"}
            name="media-audio-mode"
            onChange={submitAudioMode}
            type="radio"
            value="speech"
          />
          {t("media.p403.speech")}
        </label>
        <label>
          <input
            checked={snapshot.audioMode === "original_sound"}
            name="media-audio-mode"
            onChange={submitAudioMode}
            type="radio"
            value="original_sound"
          />
          {t("media.p403.originalSound")}
        </label>
      </fieldset>

      <p className="media-p403-processing">
        {t("media.p403.actualProcessing", {
          echo: processingValue(
            snapshot.actualAudioProcessing.echoCancellation,
            t,
          ),
          noise: processingValue(
            snapshot.actualAudioProcessing.noiseSuppression,
            t,
          ),
          gain: processingValue(
            snapshot.actualAudioProcessing.autoGainControl,
            t,
          ),
        })}
      </p>

      <div className="media-p403-speaker-actions">
        <button
          disabled={snapshot.speakerTestStatus === "playing"}
          onClick={() => void controller.testSpeaker()}
          type="button"
        >
          {snapshot.speakerTestStatus === "playing"
            ? t("media.prejoin.speakerPlaying")
            : t("media.prejoin.speakerTest")}
        </button>
        {snapshot.speakerTestStatus === "playing" && (
          <button
            onClick={() => void controller.stopSpeakerTest()}
            type="button"
          >
            {t("media.p403.stopSpeaker")}
          </button>
        )}
        <span aria-live="polite">
          {t(speakerStatusKey(snapshot.speakerTestStatus))}
        </span>
      </div>
    </div>
  );
}

function MediaSpaceFailure({ message }: { message: string }) {
  const { t } = useI18n();
  return (
    <div className="media-p403-page">
      <section className="media-p403-alert" role="alert">
        <h1>{t("media.prejoin.unavailableTitle")}</h1>
        <p>{message}</p>
        <Link to="/app/home">{t("media.p403.back")}</Link>
      </section>
    </div>
  );
}

function listenOnlyChoices(): MediaJoinChoices {
  return {
    audioEnabled: false,
    videoEnabled: false,
    audioDeviceId: "",
    videoDeviceId: "",
    speakerDeviceId: "",
    audioMode: "speech",
  };
}

function formatLobbyExpiry(expiresAt: string, language: "vi" | "en"): string {
  const parsed = new Date(expiresAt);
  if (!Number.isFinite(parsed.getTime())) {
    return expiresAt;
  }
  return new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(parsed);
}

function deviceErrorKey(code: MediaPrejoinErrorCode): TranslationKey {
  return `media.p403.error.${code}` as TranslationKey;
}

function joinProblemKey(error: unknown): TranslationKey {
  if (error instanceof APIRequestError) {
    const type = error.problem?.type ?? "";
    if (type.endsWith(":room_not_open")) return "media.p403.roomNotOpen";
    if (type.endsWith(":room_locked")) return "media.p403.roomLocked";
    if (type.endsWith(":admission_required"))
      return "media.p403.waitingDescription";
    if (type.endsWith(":feature_disabled")) return "media.p403.featureDisabled";
    if (type.endsWith(":quota_exceeded")) return "media.p403.quotaExceeded";
    if (error.status === 404) return "media.p403.spaceUnavailableDescription";
    if (error.status === 429) return "media.p403.rateLimited";
    if (error.status === 503) return "media.p403.providerUnavailable";
  }
  return "media.prejoin.joinError";
}

function networkStatusKey(status: MediaNetworkStatus): TranslationKey {
  return `media.p403.network.${status}` as TranslationKey;
}

function networkLatencyKey(
  latency: "fast" | "moderate" | "slow",
): TranslationKey {
  return `media.p403.network.${latency}` as TranslationKey;
}

function speakerStatusKey(
  status: MediaPrejoinSnapshot["speakerTestStatus"],
): TranslationKey {
  return `media.p403.speaker.${status}` as TranslationKey;
}

function processingValue(
  value: boolean | null,
  t: (key: TranslationKey, values?: Record<string, string | number>) => string,
): string {
  if (value === null) return t("media.p403.unknown");
  return value ? t("media.p403.on") : t("media.p403.off");
}

type TerminalJoinStatus = Extract<
  JoinStatus,
  "denied" | "cancelled" | "timeout" | "meeting_ended" | "provider_unavailable"
>;

function isTerminalJoinStatus(
  status: JoinStatus | MediaJoinAttemptProjection["status"],
): status is TerminalJoinStatus {
  return (
    status === "denied" ||
    status === "cancelled" ||
    status === "timeout" ||
    status === "meeting_ended" ||
    status === "provider_unavailable"
  );
}

function terminalJoinTitleKey(status: TerminalJoinStatus): TranslationKey {
  return `media.p404.waiting.${status}.title` as TranslationKey;
}

function terminalJoinDescriptionKey(
  status: TerminalJoinStatus,
): TranslationKey {
  return `media.p404.waiting.${status}.description` as TranslationKey;
}

function mediaP404ViewerOperations(value: unknown): {
  canManageAdmissions: boolean;
  canManageInvites: boolean;
} {
  if (typeof value !== "object" || value === null) {
    return { canManageAdmissions: false, canManageInvites: false };
  }
  const operations = value as Record<string, unknown>;
  return {
    canManageAdmissions: operations.can_manage_admissions === true,
    canManageInvites: operations.can_manage_invites === true,
  };
}
