import { useMemo, useState } from "react";
import {
  EFFECT_CHOICES,
  FIXTURE_SIZES,
  LAYOUT_MODES,
  REACTION_ALLOWLIST,
  VIEWPORT_PROFILES,
  createParticipants,
  projectHandQueue,
  projectLayout,
  projectReactions,
  resolveMediaPresentation,
  type AllowedReaction,
  type DegradationLevel,
  type EffectChoice,
  type FixtureSize,
  type HandEvent,
  type LayoutMode,
  type Participant,
  type ReactionEvent,
  type ViewportProfile,
} from "./mediaModel";

const LAYOUT_LABELS: Record<LayoutMode, string> = {
  grid: "Lưới",
  "active-speaker": "Người đang nói",
  presentation: "Trình chiếu",
};

const EFFECT_LABELS: Record<EffectChoice, string> = {
  none: "Không hiệu ứng",
  blur: "Làm mờ",
  studio: "Studio ấm",
  classroom: "Lớp học",
  forest: "Rừng dịu",
};

const REACTION_LABELS: Record<AllowedReaction, string> = {
  "👍": "Đồng ý",
  "👏": "Vỗ tay",
  "❤️": "Yêu thích",
  "🎉": "Chúc mừng",
  "😂": "Cười",
  "😮": "Ngạc nhiên",
};

const VIEWPORT_LABELS: Record<ViewportProfile, string> = {
  standard: "Desktop · 12 tile",
  medium: "Trung bình · 6 tile",
  compact: "Hẹp 320px · 4 tile",
};

const DEGRADATION_LABELS: Record<DegradationLevel, string> = {
  0: "Đầy đủ",
  1: "Tắt effect",
  2: "360p / 15fps",
  3: "Chỉ âm thanh",
};

interface BrowserCapability {
  label: string;
  supported: boolean;
}

function detectBrowserCapabilities(): readonly BrowserCapability[] {
  return [
    {
      label: "mediaDevices",
      supported:
        typeof navigator !== "undefined" && "mediaDevices" in navigator,
    },
    {
      label: "MediaStreamTrackProcessor",
      supported:
        typeof window !== "undefined" && "MediaStreamTrackProcessor" in window,
    },
    {
      label: "MediaStreamTrackGenerator",
      supported:
        typeof window !== "undefined" && "MediaStreamTrackGenerator" in window,
    },
    {
      label: "OffscreenCanvas",
      supported: typeof window !== "undefined" && "OffscreenCanvas" in window,
    },
    {
      label: "VideoFrame",
      supported: typeof window !== "undefined" && "VideoFrame" in window,
    },
    {
      label: "WebGL2 API",
      supported:
        typeof window !== "undefined" && "WebGL2RenderingContext" in window,
    },
  ];
}

function findParticipant(
  participants: readonly Participant[],
  participantId: string | null | undefined,
): Participant | null {
  return participants.find(({ id }) => id === participantId) ?? null;
}

interface ParticipantTileProps {
  participant: Participant;
  activeSpeakerId: string | null;
  pinnedParticipantId: string | null;
  raised: boolean;
  featured?: boolean;
  onPin: (participantId: string) => void;
}

function ParticipantTile({
  participant,
  activeSpeakerId,
  pinnedParticipantId,
  raised,
  featured = false,
  onPin,
}: ParticipantTileProps) {
  const isSpeaking = participant.id === activeSpeakerId;
  const isPinned = participant.id === pinnedParticipantId;

  return (
    <article
      className={`participant-tile${featured ? " participant-tile--featured" : ""}`}
      data-speaking={isSpeaking ? "true" : "false"}
      data-pinned={isPinned ? "true" : "false"}
      aria-label={`${participant.displayName}, ${participant.role === "teacher" ? "giáo viên" : "học viên"}`}
    >
      <div className="avatar" aria-hidden="true">
        {participant.stableIndex + 1}
      </div>
      <div className="tile-copy">
        <strong>{participant.displayName}</strong>
        <span className="tile-states">
          {isSpeaking ? <span>Đang nói</span> : <span>Mic yên lặng</span>}
          {raised ? <span>✋ Đã giơ tay</span> : null}
          {isPinned ? <span>Đã ghim cục bộ</span> : null}
        </span>
      </div>
      <button
        type="button"
        className="quiet-button"
        aria-label={`${isPinned ? "Bỏ ghim" : "Ghim"} ${participant.displayName}`}
        aria-pressed={isPinned}
        onClick={() => onPin(participant.id)}
      >
        {isPinned ? "Bỏ ghim" : "Ghim"}
      </button>
    </article>
  );
}

function App() {
  const [fixtureSize, setFixtureSize] = useState<FixtureSize>(25);
  const [layoutMode, setLayoutMode] = useState<LayoutMode>("grid");
  const [viewport, setViewport] = useState<ViewportProfile>("standard");
  const [page, setPage] = useState(0);
  const [activeSpeakerIndex, setActiveSpeakerIndex] = useState(1);
  const [pinnedParticipantId, setPinnedParticipantId] = useState<string | null>(
    null,
  );
  const [handEvents, setHandEvents] = useState<readonly HandEvent[]>([]);
  const [reactionEvents, setReactionEvents] = useState<
    readonly ReactionEvent[]
  >([]);
  const [virtualNowMs, setVirtualNowMs] = useState(20_000);
  const [effect, setEffect] = useState<EffectChoice>("none");
  const [degradationLevel, setDegradationLevel] = useState<DegradationLevel>(0);
  const [effectCapabilityEligible, setEffectCapabilityEligible] =
    useState(true);
  const [status, setStatus] = useState(
    "Harness đã sẵn sàng. Không camera, microphone hoặc LiveKit nào được khởi động.",
  );

  const participants = useMemo(
    () => createParticipants(fixtureSize),
    [fixtureSize],
  );
  const activeSpeaker =
    participants[activeSpeakerIndex % participants.length] ??
    participants[0] ??
    null;
  const presenter = participants[0] ?? null;
  const handProjection = useMemo(
    () => projectHandQueue(handEvents),
    [handEvents],
  );
  const raisedIds = useMemo(
    () =>
      new Set(handProjection.queue.map(({ participantId }) => participantId)),
    [handProjection.queue],
  );
  const reactionProjection = useMemo(
    () => projectReactions(reactionEvents, virtualNowMs),
    [reactionEvents, virtualNowMs],
  );
  const layout = useMemo(
    () =>
      projectLayout({
        participants,
        mode: layoutMode,
        viewport,
        requestedPage: page,
        activeSpeakerId: activeSpeaker?.id ?? null,
        pinnedParticipantId,
        presenterId: presenter?.id ?? null,
      }),
    [
      activeSpeaker?.id,
      layoutMode,
      page,
      participants,
      pinnedParticipantId,
      presenter?.id,
      viewport,
    ],
  );
  const capabilities = useMemo(() => detectBrowserCapabilities(), []);
  const mediaDecision = resolveMediaPresentation(
    effect,
    degradationLevel,
    effectCapabilityEligible,
  );
  const visibleParticipants = layout.visibleParticipantIds
    .map((id) => findParticipant(participants, id))
    .filter((participant): participant is Participant => participant !== null);
  const focusedParticipant = findParticipant(
    participants,
    layout.focus?.participantId,
  );
  const localParticipant = participants[0] ?? null;

  const chooseFixture = (nextSize: FixtureSize) => {
    setFixtureSize(nextSize);
    setPage(0);
    setActiveSpeakerIndex(1);
    setPinnedParticipantId(null);
    setHandEvents([]);
    setReactionEvents([]);
    setStatus(`Đã nạp fixture xác định ${nextSize} người và xóa state cũ.`);
  };

  const chooseLayout = (nextMode: LayoutMode) => {
    setLayoutMode(nextMode);
    setPage(0);
    setStatus(`Đã chuyển sang layout ${LAYOUT_LABELS[nextMode]}.`);
  };

  const togglePin = (participantId: string) => {
    const next = pinnedParticipantId === participantId ? null : participantId;
    setPinnedParticipantId(next);
    const participant = findParticipant(participants, participantId);
    setStatus(
      next
        ? `Đã ghim cục bộ ${participant?.displayName ?? "người tham gia"}. Pin thắng active speaker nhưng không đổi authority.`
        : "Đã bỏ ghim cục bộ.",
    );
  };

  const rotateActiveSpeaker = () => {
    setActiveSpeakerIndex((current) => (current + 1) % participants.length);
    setStatus(
      "Đã đổi active speaker mock. Thứ tự DOM của grid/rail vẫn theo stable index.",
    );
  };

  const appendHandCommands = (
    participantIds: readonly string[],
    kind: HandEvent["kind"],
  ) => {
    if (participantIds.length === 0) {
      return;
    }
    setHandEvents((current) => {
      const highestSequence = current.reduce(
        (highest, event) => Math.max(highest, event.serverSequence),
        100,
      );
      const additions = participantIds.map(
        (participantId, index): HandEvent => ({
          eventId: `hand-${highestSequence + index + 1}-${kind}`,
          participantId,
          serverSequence: highestSequence + index + 1,
          kind,
        }),
      );
      return [...current, ...additions];
    });
  };

  const toggleLocalHand = () => {
    if (!localParticipant) {
      return;
    }
    const isRaised = raisedIds.has(localParticipant.id);
    appendHandCommands([localParticipant.id], isRaised ? "lower" : "raise");
    setStatus(
      isRaised
        ? "Server mock đã nhận lệnh tự hạ tay."
        : "Server mock đã cấp sequence mới cho lệnh giơ tay.",
    );
  };

  const lowerFirstHand = () => {
    const first = handProjection.queue[0];
    if (!first) {
      setStatus("Hàng đợi giơ tay đang trống.");
      return;
    }
    appendHandCommands([first.participantId], "lower");
    setStatus("Moderator mock đã hạ người đầu hàng đợi.");
  };

  const lowerAllHands = () => {
    const ids = handProjection.queue.map(({ participantId }) => participantId);
    appendHandCommands(ids, "lower");
    setStatus(
      ids.length > 0
        ? `Moderator mock đã hạ ${ids.length} tay theo server sequence.`
        : "Hàng đợi giơ tay đang trống.",
    );
  };

  const addReaction = (emoji: AllowedReaction) => {
    if (!localParticipant) {
      return;
    }
    const nextSequence =
      reactionEvents.reduce(
        (highest, event) => Math.max(highest, event.serverSequence),
        0,
      ) + 1;
    const event: ReactionEvent = {
      eventId: `reaction-${nextSequence}`,
      participantId: localParticipant.id,
      emoji,
      serverSequence: nextSequence,
      acceptedAtMs: virtualNowMs,
    };
    const candidate = [...reactionEvents, event];
    const candidateProjection = projectReactions(candidate, virtualNowMs);
    const rejection = candidateProjection.rejections.find(
      ({ eventId }) => eventId === event.eventId,
    );
    setReactionEvents(candidate);
    setStatus(
      rejection
        ? `Reaction bị từ chối an toàn: ${rejection.reason}.`
        : `${REACTION_LABELS[emoji]} đã được nhận; hiển thị tối đa 10 giây.`,
    );
  };

  const advanceClock = (milliseconds: number) => {
    setVirtualNowMs((current) => current + milliseconds);
    setStatus(`Đã tiến clock mock thêm ${milliseconds / 1_000} giây.`);
  };

  const selectEffect = (nextEffect: EffectChoice) => {
    setEffect(nextEffect);
    setStatus(
      `Đã chọn UX state ${EFFECT_LABELS[nextEffect]}; không capture camera và không chạy segmentation.`,
    );
  };

  return (
    <main
      className={
        viewport === "standard"
          ? "app"
          : viewport === "medium"
            ? "app app--medium"
            : "app app--compact"
      }
    >
      <header className="hero">
        <div>
          <p className="eyebrow">P4-MEDIA-UX-00 · local mock</p>
          <h1>Classroom media UX research harness</h1>
          <p>
            Kiểm chứng geometry, server projection và fallback trước khi nối
            LiveKit.
          </p>
        </div>
        <div className="safety-badge" role="note">
          <strong>Cô lập</strong>
          <span>Không credential · không media capture · không network</span>
        </div>
      </header>

      <p
        className="sr-only"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >
        {status}
      </p>

      <section className="control-deck" aria-labelledby="fixture-heading">
        <div>
          <h2 id="fixture-heading">Fixture và viewport</h2>
          <fieldset className="segmented-control">
            <legend>Số người tham gia</legend>
            {FIXTURE_SIZES.map((size) => (
              <button
                type="button"
                key={size}
                aria-pressed={fixtureSize === size}
                onClick={() => chooseFixture(size)}
              >
                {size} người
              </button>
            ))}
          </fieldset>
        </div>
        <fieldset className="segmented-control">
          <legend>Profile viewport</legend>
          {VIEWPORT_PROFILES.map((profile) => (
            <button
              type="button"
              key={profile}
              aria-pressed={viewport === profile}
              onClick={() => {
                setViewport(profile);
                setPage(0);
                setStatus(
                  `Đã bật profile ${VIEWPORT_LABELS[profile]}; focus order không đổi.`,
                );
              }}
            >
              {VIEWPORT_LABELS[profile]}
            </button>
          ))}
        </fieldset>
      </section>

      <section className="room-card" aria-labelledby="room-heading">
        <div className="room-toolbar">
          <div>
            <p className="eyebrow">Deterministic layout</p>
            <h2 id="room-heading">Phòng học {fixtureSize} người</h2>
          </div>
          <fieldset className="segmented-control">
            <legend>Chế độ bố cục</legend>
            {LAYOUT_MODES.map((mode) => (
              <button
                type="button"
                key={mode}
                aria-pressed={layoutMode === mode}
                onClick={() => chooseLayout(mode)}
              >
                {LAYOUT_LABELS[mode]}
              </button>
            ))}
          </fieldset>
        </div>

        <div className="layout-summary" aria-label="Tóm tắt layout">
          <span>
            Trang {layout.page + 1}/{layout.pageCount}
          </span>
          <span>
            {layout.visibleParticipantIds.length} tile/rail đang hiển thị
          </span>
          <span>
            Giới hạn {layout.subscribedParticipantIds.length}/{fixtureSize}{" "}
            video mock
          </span>
          <button type="button" onClick={rotateActiveSpeaker}>
            Đổi active speaker
          </button>
        </div>

        <div className="stage" data-layout={layoutMode}>
          {layout.focus?.kind === "presentation" ? (
            <section
              className="presentation-focus"
              aria-label="Nội dung đang trình chiếu"
            >
              <span aria-hidden="true">▧</span>
              <strong>Màn hình của {focusedParticipant?.displayName}</strong>
              <small>Mock screen share · không có track thật</small>
            </section>
          ) : null}

          {layout.focus?.kind === "participant" && focusedParticipant ? (
            <div className="speaker-focus">
              <ParticipantTile
                participant={focusedParticipant}
                activeSpeakerId={activeSpeaker?.id ?? null}
                pinnedParticipantId={pinnedParticipantId}
                raised={raisedIds.has(focusedParticipant.id)}
                featured
                onPin={togglePin}
              />
            </div>
          ) : null}

          <ol
            className={
              layoutMode === "grid" ? "participant-grid" : "participant-rail"
            }
            aria-label={
              layoutMode === "grid"
                ? "Lưới người tham gia"
                : "Dải người tham gia"
            }
          >
            {visibleParticipants.map((participant) => (
              <li key={participant.id}>
                <ParticipantTile
                  participant={participant}
                  activeSpeakerId={activeSpeaker?.id ?? null}
                  pinnedParticipantId={pinnedParticipantId}
                  raised={raisedIds.has(participant.id)}
                  onPin={togglePin}
                />
              </li>
            ))}
          </ol>
        </div>

        <nav className="pagination" aria-label="Phân trang người tham gia">
          <button
            type="button"
            disabled={layout.page === 0}
            onClick={() => {
              setPage(Math.max(layout.page - 1, 0));
              setStatus("Đã chuyển về trang người tham gia trước.");
            }}
          >
            Trang trước
          </button>
          <span aria-current="page">
            {layout.page + 1} / {layout.pageCount}
          </span>
          <button
            type="button"
            disabled={layout.page + 1 >= layout.pageCount}
            onClick={() => {
              setPage(Math.min(layout.page + 1, layout.pageCount - 1));
              setStatus("Đã chuyển sang trang người tham gia tiếp theo.");
            }}
          >
            Trang sau
          </button>
        </nav>
      </section>

      <div className="evidence-grid">
        <section className="evidence-card" aria-labelledby="hand-heading">
          <p className="eyebrow">Core API projection mock</p>
          <h2 id="hand-heading">Hàng đợi giơ tay</h2>
          <p>
            FIFO theo server sequence; duplicate/out-of-order được pure model
            hội tụ.
          </p>
          <div className="button-row">
            <button
              type="button"
              aria-pressed={
                localParticipant ? raisedIds.has(localParticipant.id) : false
              }
              onClick={toggleLocalHand}
            >
              {localParticipant && raisedIds.has(localParticipant.id)
                ? "Hạ tay của tôi"
                : "Giơ tay"}
            </button>
            <button type="button" onClick={lowerFirstHand}>
              Moderator hạ người đầu
            </button>
            <button type="button" onClick={lowerAllHands}>
              Hạ tất cả
            </button>
          </div>
          {handProjection.queue.length > 0 ? (
            <ol className="queue-list">
              {handProjection.queue.map(({ participantId, raisedSequence }) => {
                const participant = findParticipant(
                  participants,
                  participantId,
                );
                return (
                  <li key={participantId}>
                    <span>
                      {participant?.displayName ??
                        "Người tham gia đã rời phòng"}
                    </span>
                    <code>seq {raisedSequence}</code>
                  </li>
                );
              })}
            </ol>
          ) : (
            <p className="empty-state">Chưa có ai giơ tay.</p>
          )}
        </section>

        <section className="evidence-card" aria-labelledby="reaction-heading">
          <p className="eyebrow">Bounded ephemeral projection</p>
          <h2 id="reaction-heading">Reaction</h2>
          <p>
            Allowlist sáu emoji · TTL 10 giây · gom 750ms/tối đa 3 nhóm · mỗi
            người 3/5 giây và 20/phút · phòng 100/5 giây.
          </p>
          <div className="reaction-buttons" aria-label="Gửi reaction mock">
            {REACTION_ALLOWLIST.map((emoji) => (
              <button
                type="button"
                key={emoji}
                aria-label={REACTION_LABELS[emoji]}
                onClick={() => addReaction(emoji)}
              >
                <span aria-hidden="true">{emoji}</span>
              </button>
            ))}
          </div>
          <div
            className="reaction-clusters"
            aria-label="Reaction đang hiển thị"
          >
            {reactionProjection.clusters.length > 0 ? (
              reactionProjection.clusters.map((cluster) => (
                <span
                  className="reaction-cluster"
                  key={`${cluster.emoji}-${cluster.firstAcceptedAtMs}`}
                  title={cluster.participantIds
                    .map(
                      (id) =>
                        findParticipant(participants, id)?.displayName ??
                        "Ẩn danh",
                    )
                    .join(", ")}
                >
                  <span aria-hidden="true">{cluster.emoji}</span>
                  <span>
                    {REACTION_LABELS[cluster.emoji]} × {cluster.count}
                  </span>
                </span>
              ))
            ) : (
              <p className="empty-state">Không có reaction đang hiển thị.</p>
            )}
          </div>
          <div className="button-row">
            <button type="button" onClick={() => advanceClock(1_000)}>
              +1 giây
            </button>
            <button type="button" onClick={() => advanceClock(10_000)}>
              +10 giây
            </button>
            <output aria-label="Thời gian clock mock">
              Clock mock: {(virtualNowMs / 1_000).toFixed(0)}s
            </output>
          </div>
        </section>
      </div>

      <section className="effect-card" aria-labelledby="effect-heading">
        <div>
          <p className="eyebrow">Capability UX only</p>
          <h2 id="effect-heading">Background và degraded mode</h2>
          <p className="important-note">
            Đây là preview CSS, không phải segmentation thật và không phải bằng
            chứng processor. Chọn effect không xin camera permission và không
            được lưu.
          </p>
          <fieldset className="effect-options">
            <legend>Hiệu ứng yêu cầu</legend>
            {EFFECT_CHOICES.map((choice) => (
              <button
                type="button"
                key={choice}
                aria-pressed={effect === choice}
                onClick={() => selectEffect(choice)}
              >
                {EFFECT_LABELS[choice]}
              </button>
            ))}
          </fieldset>
          <label className="check-control">
            <input
              type="checkbox"
              checked={effectCapabilityEligible}
              onChange={(event) => {
                setEffectCapabilityEligible(event.currentTarget.checked);
                setStatus(
                  event.currentTarget.checked
                    ? "Đã mô phỏng candidate đủ capability; vẫn không tải processor."
                    : "Đã mô phỏng effect unsupported; fallback về raw track.",
                );
              }}
            />
            Mô phỏng processor candidate đủ gate
          </label>
          <fieldset className="segmented-control degradation-control">
            <legend>Mức degrade mô phỏng</legend>
            {([0, 1, 2, 3] as const).map((level) => (
              <button
                type="button"
                key={level}
                aria-pressed={degradationLevel === level}
                onClick={() => {
                  setDegradationLevel(level);
                  setStatus(`Đã chọn degrade: ${DEGRADATION_LABELS[level]}.`);
                }}
              >
                {DEGRADATION_LABELS[level]}
              </button>
            ))}
          </fieldset>
        </div>

        <div className="effect-evidence">
          <div
            className="effect-preview"
            role="img"
            data-effect={mediaDecision.effectiveEffect}
            data-audio-only={
              mediaDecision.videoProfile === "audio-only" ? "true" : "false"
            }
            aria-label={`Preview mock: ${EFFECT_LABELS[mediaDecision.effectiveEffect]}, ${mediaDecision.videoProfile}`}
          >
            <div className="mock-background" aria-hidden="true" />
            <div className="mock-person" aria-hidden="true">
              <span />
            </div>
            {mediaDecision.videoProfile === "audio-only" ? (
              <strong>Camera đã tắt · tiếp tục bằng âm thanh</strong>
            ) : (
              <strong>{EFFECT_LABELS[mediaDecision.effectiveEffect]}</strong>
            )}
          </div>
          <dl className="decision-list">
            <div>
              <dt>Yêu cầu</dt>
              <dd>{EFFECT_LABELS[mediaDecision.requestedEffect]}</dd>
            </div>
            <div>
              <dt>Thực tế</dt>
              <dd>{EFFECT_LABELS[mediaDecision.effectiveEffect]}</dd>
            </div>
            <div>
              <dt>Video</dt>
              <dd>{mediaDecision.videoProfile}</dd>
            </div>
            <div>
              <dt>Lý do</dt>
              <dd>{mediaDecision.reason}</dd>
            </div>
          </dl>
        </div>

        <details className="capability-list">
          <summary>Feature-detect advisory của browser hiện tại</summary>
          <p>
            Có API không đồng nghĩa processor đã tương thích. Harness không gọi
            API media.
          </p>
          <ul>
            {capabilities.map(({ label, supported }) => (
              <li key={label}>
                <code>{label}</code>
                <strong>{supported ? "Có" : "Không/không phát hiện"}</strong>
              </li>
            ))}
          </ul>
        </details>
      </section>

      <footer>
        <p>
          Prototype không chứng minh browser matrix, hiệu năng 360p/540p/720p,
          NVDA thủ công, adaptive stream thực hoặc LiveKit reconnect. Các gate
          đó vẫn thuộc P4-03/P4-05/P4-11.
        </p>
      </footer>
    </main>
  );
}

export { App };
