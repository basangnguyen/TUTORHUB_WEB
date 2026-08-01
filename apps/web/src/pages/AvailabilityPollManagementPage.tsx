import { APIRequestError } from "@tutorhub/api-client";
import {
  Button,
  EmptyState,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextAreaField,
  TextField,
} from "@tutorhub/ui";
import {
  CalendarClock,
  Check,
  Clipboard,
  Link2,
  LockKeyhole,
  Plus,
  RefreshCw,
  RotateCcw,
  Send,
  Square,
  X,
} from "lucide-react";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { Temporal } from "temporal-polyfill";
import {
  type AvailabilityPoll,
  type AvailabilityPollAnswerState,
  type AvailabilityPollCapabilitySecret,
  type AvailabilityPollIndividualResponse,
  type CreateAvailabilityPollRequest,
  type StudyMeeting,
  generatePollSlots,
  useAvailabilityPollDetail,
  useAvailabilityPollIndividualResponses,
  useAvailabilityPollLifecycle,
  useAvailabilityPollList,
  useAvailabilityPollSummary,
  useCancelStudyMeeting,
  useCreateAvailabilityPoll,
  useCreateAvailabilityPollCapability,
  useCreateStudyMeeting,
  useFinalizeAvailabilityPoll,
  useRespondAvailabilityPoll,
  useStudyMeetingList,
} from "../app/availabilityPollManagement";
import { resolveCivilDateTime } from "../app/classSessionTime";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";
import {
  tenantOperationAvailability,
  useTenantCapabilities,
} from "../app/tenantCapabilities";
import "./AvailabilityPollManagementPage.css";

const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

const answerStates = [
  "preferred",
  "available",
  "unavailable",
] as const satisfies readonly AvailabilityPollAnswerState[];

const copy = {
  en: {
    kicker: "Schedule together",
    title: "Availability polls",
    description:
      "Collect availability, rank suitable times, and schedule an owned study meeting.",
    coreBoundary:
      "Core scheduling only: no email, automatic deadline closure, or room creation is triggered here.",
    newPoll: "Create a poll",
    pollTitle: "Poll title",
    pollDescription: "Description",
    timezone: "Timezone",
    rangeStart: "First date",
    rangeEnd: "Last date",
    workingStart: "Daily start",
    workingEnd: "Daily end",
    duration: "Meeting duration (minutes)",
    granularity: "Slot step",
    deadline: "Response deadline",
    shareMode: "Who can respond",
    invitedOnly: "Invited participants",
    anyoneLink: "Anyone with the link",
    classMembers: "Active class members",
    classID: "Class ID (optional)",
    participantIDs: "Participant user IDs (optional)",
    participantHint:
      "Separate same-workspace user IDs with commas or new lines.",
    slotPreview: "Generated slots",
    slotPreviewHint: "The server revalidates every slot and timezone offset.",
    create: "Create draft poll",
    creating: "Creating poll…",
    polls: "Your polls",
    refresh: "Refresh",
    noPolls: "No availability polls yet",
    noPollsDescription: "Create a draft to start collecting suitable times.",
    loading: "Loading availability polls",
    loadError: "Availability polls could not be loaded",
    loadErrorDescription: "Check the connection and try again.",
    forbidden: "Availability polls are unavailable",
    forbiddenDescription:
      "This workspace or feature does not currently allow this operation.",
    retry: "Try again",
    choosePoll: "Choose a poll",
    choosePollDescription: "Select an item to inspect and manage it.",
    detailLoading: "Loading poll detail",
    detailError: "Poll detail could not be loaded",
    status: "Status",
    deadlineLabel: "Deadline",
    slots: "slots",
    responses: "responses",
    open: "Open poll",
    close: "Close poll",
    reopen: "Reopen poll",
    cancel: "Cancel poll",
    cancelReason: "Cancelled by the organizer",
    lifecycleError: "The poll state could not be changed.",
    responseHeading: "Your availability",
    responseHint:
      "Drag down a state column on desktop, or use Tab and arrow keys with the radio controls.",
    preferred: "Preferred",
    available: "Available",
    unavailable: "Unavailable",
    unknown: "Not answered",
    saveResponse: "Save my response",
    responseSaved: "Your response is up to date.",
    individualResponses: "Individual responses",
    individualResponsesDescription:
      "Response identities use privacy-safe labels. Email addresses are never displayed here.",
    individualResponsesTable: "Individual response slot states",
    loadingIndividualResponses: "Loading individual responses…",
    individualResponsesError: "Individual responses could not be loaded.",
    noIndividualResponses: "No individual responses have been submitted yet.",
    loadedIndividualResponses: "Loaded responses",
    loadMoreIndividualResponses: "Load more responses",
    loadingMoreIndividualResponses: "Loading more responses…",
    respondent: "Respondent",
    internalUser: "Internal user",
    participant: "Participant",
    anonymousRespondent: "Anonymous respondent",
    submittedAt: "Submitted",
    summary: "Ranked times",
    noSummary: "No ranked times are available yet.",
    finalize: "Schedule selected time",
    finalizeStudy: "Study meeting",
    finalizeClass: "Official class session",
    selectedTime: "Selected time",
    finalizeOutcome: "Outcome",
    share: "Create a secure link",
    shareHeading: "Secure sharing",
    shareParticipant: "Invited participant",
    shareExpiry: "Link expiry",
    shareClassOnly:
      "This poll uses class membership authorization and does not need a bearer link.",
    shareOnce:
      "This link is shown once. Copy it now; TutorHub does not persist its raw secret.",
    copyLink: "Copy link",
    copied: "Copied",
    meetings: "Study meetings",
    meetingsDescription:
      "Owned scheduling intents only. Media rooms remain a Phase 4 capability.",
    noMeetings: "No study meetings scheduled",
    noMeetingsDescription:
      "Create one directly or finalize a closed availability poll.",
    meetingTitle: "Meeting title",
    startsAt: "Starts at",
    endsAt: "Ends at",
    createMeeting: "Schedule study meeting",
    meetingLoadError: "Study meetings could not be loaded",
    meetingCreateError: "The study meeting could not be scheduled.",
    meetingCancelError: "The study meeting could not be cancelled.",
    cancelMeeting: "Cancel meeting",
    scheduled: "Scheduled",
    cancelled: "Cancelled",
    operationUnavailable:
      "The workspace feature or quota currently prevents this operation.",
    required: "Complete the required fields with valid values.",
    invalidRange: "Choose a valid date range of at most 90 days.",
    invalidHours: "Daily end must be after daily start.",
    invalidTimezone: "Enter a valid IANA timezone.",
    noSlots: "No valid slots fit inside the selected daily window.",
    tooManySlots:
      "This editor generated more than 1,000 slots. Narrow the range.",
    invalidParticipant: "Every participant value must be a valid user UUID.",
    classRequired: "Class-member polls require a valid class UUID.",
    invalidDeadline: "Choose a valid future deadline in the poll timezone.",
    conflict:
      "The resource changed or the selected time now conflicts. Reload and review the latest state.",
  },
  vi: {
    kicker: "Cùng chọn thời gian",
    title: "Khảo sát thời gian",
    description:
      "Thu thập lịch rảnh, xếp hạng thời gian phù hợp và lên lịch học nhóm do bạn sở hữu.",
    coreBoundary:
      "Đây chỉ là lịch lõi: không gửi email, không tự đóng khi hết hạn và không tạo phòng học.",
    newPoll: "Tạo khảo sát",
    pollTitle: "Tên khảo sát",
    pollDescription: "Mô tả",
    timezone: "Múi giờ",
    rangeStart: "Ngày đầu",
    rangeEnd: "Ngày cuối",
    workingStart: "Bắt đầu mỗi ngày",
    workingEnd: "Kết thúc mỗi ngày",
    duration: "Thời lượng buổi học (phút)",
    granularity: "Bước thời gian",
    deadline: "Hạn phản hồi",
    shareMode: "Ai có thể phản hồi",
    invitedOnly: "Người được mời",
    anyoneLink: "Bất kỳ ai có liên kết",
    classMembers: "Thành viên lớp đang hoạt động",
    classID: "ID lớp (không bắt buộc)",
    participantIDs: "ID người tham gia (không bắt buộc)",
    participantHint:
      "Phân tách ID người dùng cùng workspace bằng dấu phẩy hoặc xuống dòng.",
    slotPreview: "Khung giờ được tạo",
    slotPreviewHint:
      "Máy chủ sẽ kiểm tra lại từng khung giờ và độ lệch múi giờ.",
    create: "Tạo bản nháp",
    creating: "Đang tạo khảo sát…",
    polls: "Khảo sát của bạn",
    refresh: "Làm mới",
    noPolls: "Chưa có khảo sát thời gian",
    noPollsDescription: "Tạo bản nháp để bắt đầu thu thập thời gian phù hợp.",
    loading: "Đang tải khảo sát thời gian",
    loadError: "Không thể tải khảo sát thời gian",
    loadErrorDescription: "Kiểm tra kết nối rồi thử lại.",
    forbidden: "Không thể dùng khảo sát thời gian",
    forbiddenDescription:
      "Workspace hoặc tính năng hiện không cho phép thao tác này.",
    retry: "Thử lại",
    choosePoll: "Chọn một khảo sát",
    choosePollDescription: "Chọn một mục để xem và quản lý.",
    detailLoading: "Đang tải chi tiết khảo sát",
    detailError: "Không thể tải chi tiết khảo sát",
    status: "Trạng thái",
    deadlineLabel: "Hạn",
    slots: "khung giờ",
    responses: "phản hồi",
    open: "Mở khảo sát",
    close: "Đóng khảo sát",
    reopen: "Mở lại khảo sát",
    cancel: "Hủy khảo sát",
    cancelReason: "Người tổ chức đã hủy",
    lifecycleError: "Không thể thay đổi trạng thái khảo sát.",
    responseHeading: "Thời gian của bạn",
    responseHint:
      "Kéo dọc một cột trạng thái trên desktop, hoặc dùng Tab và phím mũi tên với các nút chọn.",
    preferred: "Ưu tiên",
    available: "Có thể",
    unavailable: "Không thể",
    unknown: "Chưa trả lời",
    saveResponse: "Lưu phản hồi",
    responseSaved: "Phản hồi của bạn đã được cập nhật.",
    individualResponses: "Phản hồi cá nhân",
    individualResponsesDescription:
      "Danh tính người phản hồi dùng nhãn bảo vệ riêng tư. Địa chỉ email không bao giờ hiển thị tại đây.",
    individualResponsesTable: "Trạng thái khung giờ theo từng phản hồi",
    loadingIndividualResponses: "Đang tải phản hồi cá nhân…",
    individualResponsesError: "Không thể tải phản hồi cá nhân.",
    noIndividualResponses: "Chưa có phản hồi cá nhân nào được gửi.",
    loadedIndividualResponses: "Số phản hồi đã tải",
    loadMoreIndividualResponses: "Tải thêm phản hồi",
    loadingMoreIndividualResponses: "Đang tải thêm phản hồi…",
    respondent: "Người phản hồi",
    internalUser: "Người dùng nội bộ",
    participant: "Người tham gia",
    anonymousRespondent: "Người phản hồi ẩn danh",
    submittedAt: "Đã gửi",
    summary: "Thời gian xếp hạng",
    noSummary: "Chưa có thời gian được xếp hạng.",
    finalize: "Lên lịch thời gian đã chọn",
    finalizeStudy: "Lịch học nhóm",
    finalizeClass: "Buổi học chính thức",
    selectedTime: "Thời gian đã chọn",
    finalizeOutcome: "Kết quả",
    share: "Tạo liên kết bảo mật",
    shareHeading: "Chia sẻ bảo mật",
    shareParticipant: "Người được mời",
    shareExpiry: "Hạn liên kết",
    shareClassOnly:
      "Khảo sát này dùng quyền thành viên lớp và không cần liên kết mang secret.",
    shareOnce:
      "Liên kết chỉ hiện một lần. Hãy sao chép ngay; TutorHub không lưu secret thô.",
    copyLink: "Sao chép liên kết",
    copied: "Đã sao chép",
    meetings: "Lịch học nhóm",
    meetingsDescription:
      "Chỉ là lịch do thành viên sở hữu. Phòng học trực tuyến thuộc Phase 4.",
    noMeetings: "Chưa có lịch học nhóm",
    noMeetingsDescription:
      "Tạo trực tiếp hoặc chốt một khảo sát thời gian đã đóng.",
    meetingTitle: "Tên lịch học",
    startsAt: "Bắt đầu",
    endsAt: "Kết thúc",
    createMeeting: "Lên lịch học nhóm",
    meetingLoadError: "Không thể tải lịch học nhóm",
    meetingCreateError: "Không thể lên lịch học nhóm.",
    meetingCancelError: "Không thể hủy lịch học nhóm.",
    cancelMeeting: "Hủy lịch",
    scheduled: "Đã lên lịch",
    cancelled: "Đã hủy",
    operationUnavailable:
      "Tính năng hoặc hạn mức workspace hiện không cho phép thao tác này.",
    required: "Hãy điền đúng các trường bắt buộc.",
    invalidRange: "Chọn khoảng ngày hợp lệ, tối đa 90 ngày.",
    invalidHours: "Giờ kết thúc mỗi ngày phải sau giờ bắt đầu.",
    invalidTimezone: "Nhập múi giờ IANA hợp lệ.",
    noSlots: "Không có khung giờ hợp lệ trong khoảng đã chọn.",
    tooManySlots: "Editor tạo quá 1.000 khung giờ. Hãy thu hẹp khoảng ngày.",
    invalidParticipant: "Mỗi người tham gia phải có UUID người dùng hợp lệ.",
    classRequired: "Khảo sát theo lớp cần UUID lớp hợp lệ.",
    invalidDeadline: "Chọn hạn tương lai hợp lệ theo múi giờ khảo sát.",
    conflict:
      "Dữ liệu đã thay đổi hoặc thời gian mới bị trùng. Hãy tải lại và kiểm tra trạng thái mới nhất.",
  },
} as const;

type PageCopy = (typeof copy)[keyof typeof copy];

function localDateTimeInput(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(
    date.getDate(),
  )}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function defaultDates(timezone: string) {
  try {
    const date = Temporal.Now.plainDateISO(timezone).add({ days: 1 });
    return {
      rangeEnd: date.toString(),
      rangeStart: date.toString(),
    };
  } catch {
    const date = Temporal.Now.plainDateISO("UTC").add({ days: 1 });
    return {
      rangeEnd: date.toString(),
      rangeStart: date.toString(),
    };
  }
}

function mutationMessage(error: unknown, fallback: string, strings: PageCopy) {
  if (error instanceof APIRequestError && error.status === 409) {
    return strings.conflict;
  }
  if (error instanceof APIRequestError) {
    return error.problem?.detail ?? error.message;
  }
  return error instanceof Error ? error.message : fallback;
}

function isForbidden(error: unknown) {
  return (
    error instanceof APIRequestError &&
    (error.status === 403 || error.status === 404)
  );
}

function statusTone(
  status: AvailabilityPoll["status"] | StudyMeeting["status"],
) {
  if (status === "open" || status === "scheduled") return "success" as const;
  if (status === "closed") return "warning" as const;
  return "neutral" as const;
}

function formatSlot(
  startsAt: string,
  endsAt: string,
  timezone: string,
  locale: string,
) {
  const date = new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeZone: timezone,
  }).format(new Date(startsAt));
  const time = new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
    timeZone: timezone,
  });
  return `${date}, ${time.format(new Date(startsAt))}–${time.format(
    new Date(endsAt),
  )}`;
}

function PollCreateEditor({
  canCreate,
  onCreated,
  strings,
  tenantID,
  timezone: initialTimezone,
}: {
  canCreate: boolean;
  onCreated: (pollID: string) => void;
  strings: PageCopy;
  tenantID: string | undefined;
  timezone: string;
}) {
  const initialDates = useMemo(
    () => defaultDates(initialTimezone),
    [initialTimezone],
  );
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [timezone, setTimezone] = useState(initialTimezone);
  const [rangeStart, setRangeStart] = useState(initialDates.rangeStart);
  const [rangeEnd, setRangeEnd] = useState(initialDates.rangeEnd);
  const [workingStart, setWorkingStart] = useState("09:00");
  const [workingEnd, setWorkingEnd] = useState("17:00");
  const [duration, setDuration] = useState(60);
  const [granularity, setGranularity] = useState<15 | 30 | 60>(30);
  const [deadline, setDeadline] = useState(() =>
    localDateTimeInput(new Date(Date.now() + 24 * 60 * 60 * 1000)),
  );
  const [shareMode, setShareMode] =
    useState<CreateAvailabilityPollRequest["share_mode"]>("invited_only");
  const [classID, setClassID] = useState("");
  const [participantIDs, setParticipantIDs] = useState("");
  const [error, setError] = useState<string | null>(null);
  const createPoll = useCreateAvailabilityPoll(tenantID);
  const generated = useMemo(
    () =>
      generatePollSlots({
        durationMinutes: duration,
        granularityMinutes: granularity,
        rangeEnd,
        rangeStart,
        timezone,
        workingEnd,
        workingStart,
      }),
    [
      duration,
      granularity,
      rangeEnd,
      rangeStart,
      timezone,
      workingEnd,
      workingStart,
    ],
  );

  const slotError =
    generated.error === "invalid_range"
      ? strings.invalidRange
      : generated.error === "invalid_hours"
        ? strings.invalidHours
        : generated.error === "invalid_timezone"
          ? strings.invalidTimezone
          : generated.error === "no_slots"
            ? strings.noSlots
            : generated.error === "too_many_slots"
              ? strings.tooManySlots
              : null;

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(null);
    if (!title.trim() || generated.error || !tenantID) {
      setError(slotError ?? strings.required);
      return;
    }
    const normalizedClassID = classID.trim().toLowerCase();
    if (
      (normalizedClassID && !uuidPattern.test(normalizedClassID)) ||
      (shareMode === "class_members" && !normalizedClassID)
    ) {
      setError(strings.classRequired);
      return;
    }
    const userIDs = participantIDs
      .split(/[\s,;]+/)
      .map((value) => value.trim().toLowerCase())
      .filter(Boolean);
    if (userIDs.some((value) => !uuidPattern.test(value))) {
      setError(strings.invalidParticipant);
      return;
    }
    const deadlineResolution = resolveCivilDateTime(
      deadline,
      timezone,
      "earlier",
    );
    if (
      deadlineResolution.kind !== "resolved" ||
      Temporal.Instant.compare(
        Temporal.Instant.from(deadlineResolution.value),
        Temporal.Now.instant(),
      ) <= 0
    ) {
      setError(strings.invalidDeadline);
      return;
    }
    try {
      const poll = await createPoll.mutateAsync({
        class_id: normalizedClassID || null,
        deadline_at: deadlineResolution.value,
        description: description.trim(),
        duration_minutes: duration,
        idempotency_key: crypto.randomUUID(),
        participants: userIDs.map((internalUserID) => ({
          internal_user_id: internalUserID,
          kind: "internal_user" as const,
        })),
        range_end: rangeEnd,
        range_start: rangeStart,
        share_mode: shareMode,
        slot_granularity_minutes: granularity,
        slots: generated.slots,
        timezone,
        title: title.trim(),
        working_day_end: workingEnd,
        working_day_start: workingStart,
      });
      setTitle("");
      setDescription("");
      setParticipantIDs("");
      onCreated(poll.id);
    } catch (mutationError) {
      setError(mutationMessage(mutationError, strings.required, strings));
    }
  };

  return (
    <section
      aria-labelledby="availability-poll-editor-title"
      className="availability-poll-management__panel availability-poll-management__editor"
    >
      <div className="availability-poll-management__panel-heading">
        <span aria-hidden="true">
          <Plus />
        </span>
        <div>
          <h2 id="availability-poll-editor-title">{strings.newPoll}</h2>
          <p>{strings.slotPreviewHint}</p>
        </div>
      </div>
      <form onSubmit={(event) => void submit(event)}>
        <TextField
          label={strings.pollTitle}
          maxLength={160}
          onChange={(event) => setTitle(event.target.value)}
          required
          value={title}
        />
        <TextAreaField
          label={strings.pollDescription}
          maxLength={2000}
          onChange={(event) => setDescription(event.target.value)}
          rows={3}
          value={description}
        />
        <div className="availability-poll-management__form-grid">
          <TextField
            label={strings.timezone}
            onChange={(event) => setTimezone(event.target.value)}
            required
            value={timezone}
          />
          <label>
            <span>{strings.shareMode}</span>
            <select
              onChange={(event) =>
                setShareMode(
                  event.target
                    .value as CreateAvailabilityPollRequest["share_mode"],
                )
              }
              value={shareMode}
            >
              <option value="invited_only">{strings.invitedOnly}</option>
              <option value="anyone_with_link">{strings.anyoneLink}</option>
              <option value="class_members">{strings.classMembers}</option>
            </select>
          </label>
          <label>
            <span>{strings.rangeStart}</span>
            <input
              onChange={(event) => setRangeStart(event.target.value)}
              required
              type="date"
              value={rangeStart}
            />
          </label>
          <label>
            <span>{strings.rangeEnd}</span>
            <input
              onChange={(event) => setRangeEnd(event.target.value)}
              required
              type="date"
              value={rangeEnd}
            />
          </label>
          <label>
            <span>{strings.workingStart}</span>
            <input
              onChange={(event) => setWorkingStart(event.target.value)}
              required
              step={900}
              type="time"
              value={workingStart}
            />
          </label>
          <label>
            <span>{strings.workingEnd}</span>
            <input
              onChange={(event) => setWorkingEnd(event.target.value)}
              required
              step={900}
              type="time"
              value={workingEnd}
            />
          </label>
          <label>
            <span>{strings.duration}</span>
            <input
              max={480}
              min={15}
              onChange={(event) => setDuration(Number(event.target.value))}
              required
              step={15}
              type="number"
              value={duration}
            />
          </label>
          <label>
            <span>{strings.granularity}</span>
            <select
              onChange={(event) =>
                setGranularity(Number(event.target.value) as 15 | 30 | 60)
              }
              value={granularity}
            >
              <option value={15}>15 min</option>
              <option value={30}>30 min</option>
              <option value={60}>60 min</option>
            </select>
          </label>
          <label>
            <span>{strings.deadline}</span>
            <input
              onChange={(event) => setDeadline(event.target.value)}
              required
              type="datetime-local"
              value={deadline}
            />
          </label>
          <TextField
            label={strings.classID}
            onChange={(event) => setClassID(event.target.value)}
            value={classID}
          />
        </div>
        <TextAreaField
          hint={strings.participantHint}
          label={strings.participantIDs}
          onChange={(event) => setParticipantIDs(event.target.value)}
          rows={2}
          value={participantIDs}
        />
        <div
          aria-live="polite"
          className="availability-poll-management__slot-preview"
        >
          <strong>{strings.slotPreview}</strong>
          <span>
            {generated.error ? "—" : generated.slots.length} {strings.slots}
          </span>
          {slotError && <small>{slotError}</small>}
        </div>
        {!canCreate && (
          <p className="availability-poll-management__notice">
            <LockKeyhole aria-hidden="true" /> {strings.operationUnavailable}
          </p>
        )}
        {error && (
          <p className="availability-poll-management__error" role="alert">
            {error}
          </p>
        )}
        <Button
          disabled={
            !canCreate || createPoll.isPending || Boolean(generated.error)
          }
          leadingIcon={<Plus />}
          type="submit"
        >
          {createPoll.isPending ? strings.creating : strings.create}
        </Button>
      </form>
    </section>
  );
}

function PollList({
  onSelect,
  polls,
  selectedPollID,
  strings,
}: {
  onSelect: (pollID: string) => void;
  polls: readonly AvailabilityPoll[];
  selectedPollID: string | null;
  strings: PageCopy;
}) {
  if (polls.length === 0) {
    return (
      <EmptyState
        description={strings.noPollsDescription}
        title={strings.noPolls}
      />
    );
  }
  return (
    <ul
      aria-label={strings.polls}
      className="availability-poll-management__poll-list"
    >
      {polls.map((poll) => (
        <li key={poll.id}>
          <button
            aria-current={poll.id === selectedPollID ? "true" : undefined}
            onClick={() => onSelect(poll.id)}
            type="button"
          >
            <span>
              <strong>{poll.title}</strong>
              <small>
                {poll.slots.length} {strings.slots}
              </small>
            </span>
            <StatusBadge tone={statusTone(poll.status)}>
              {poll.status}
            </StatusBadge>
          </button>
        </li>
      ))}
    </ul>
  );
}

function PollResponseEditor({
  poll,
  strings,
  tenantID,
}: {
  poll: AvailabilityPoll;
  strings: PageCopy;
  tenantID: string;
}) {
  const [answers, setAnswers] = useState<
    Readonly<Record<string, AvailabilityPollAnswerState>>
  >(() =>
    Object.fromEntries(
      (poll.my_response?.answers ?? []).map((answer) => [
        answer.slot_id,
        answer.state,
      ]),
    ),
  );
  const [saved, setSaved] = useState(false);
  const paintState = useRef<AvailabilityPollAnswerState | null>(null);
  const painting = useRef(false);
  const respond = useRespondAvailabilityPoll(tenantID);
  const locale = strings === copy.vi ? "vi-VN" : "en-US";

  useEffect(() => {
    const stopPainting = () => {
      painting.current = false;
      paintState.current = null;
    };
    window.addEventListener("pointerup", stopPainting);
    window.addEventListener("pointercancel", stopPainting);
    return () => {
      window.removeEventListener("pointerup", stopPainting);
      window.removeEventListener("pointercancel", stopPainting);
    };
  }, []);

  const choose = (slotID: string, state: AvailabilityPollAnswerState) => {
    setSaved(false);
    setAnswers((current) => ({ ...current, [slotID]: state }));
  };
  const startPaint = (
    event: ReactPointerEvent<HTMLLabelElement>,
    slotID: string,
    state: AvailabilityPollAnswerState,
  ) => {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    painting.current = true;
    paintState.current = state;
    choose(slotID, state);
  };
  const continuePaint = (
    event: ReactPointerEvent<HTMLLabelElement>,
    slotID: string,
    state: AvailabilityPollAnswerState,
  ) => {
    if (
      painting.current &&
      paintState.current === state &&
      (event.buttons === 1 || event.pointerType !== "mouse")
    ) {
      choose(slotID, state);
    }
  };
  const submit = async () => {
    try {
      await respond.mutateAsync({
        input: {
          answers: Object.entries(answers).map(([slotID, state]) => ({
            slot_id: slotID,
            state,
          })),
          expected_response_version: poll.my_response?.version ?? 0,
          idempotency_key: crypto.randomUUID(),
        },
        pollID: poll.id,
      });
      setSaved(true);
    } catch {
      // The mutation exposes its typed error below without creating an unhandled rejection.
    }
  };

  if (!poll.viewer_capabilities.can_respond || poll.status !== "open") {
    return null;
  }
  return (
    <section
      aria-labelledby="poll-response-title"
      className="availability-poll-management__subpanel"
    >
      <h3 id="poll-response-title">{strings.responseHeading}</h3>
      <p>{strings.responseHint}</p>
      <div
        className="availability-poll-management__heatmap"
        role="group"
        aria-label={strings.responseHeading}
      >
        <div
          aria-hidden="true"
          className="availability-poll-management__heatmap-head"
        >
          <span />
          {answerStates.map((state) => (
            <strong key={state}>{strings[state]}</strong>
          ))}
        </div>
        {poll.slots.map((slot) => (
          <fieldset key={slot.id}>
            <legend>
              {formatSlot(slot.starts_at, slot.ends_at, poll.timezone, locale)}
            </legend>
            {answerStates.map((state) => (
              <label
                data-state={state}
                key={state}
                onPointerDown={(event) => startPaint(event, slot.id, state)}
                onPointerEnter={(event) => continuePaint(event, slot.id, state)}
              >
                <input
                  checked={answers[slot.id] === state}
                  name={`poll-slot-${slot.id}`}
                  onChange={() => choose(slot.id, state)}
                  type="radio"
                  value={state}
                />
                <span>{strings[state]}</span>
              </label>
            ))}
          </fieldset>
        ))}
      </div>
      {respond.error && (
        <p className="availability-poll-management__error" role="alert">
          {mutationMessage(respond.error, strings.required, strings)}
        </p>
      )}
      {saved && (
        <p aria-live="polite" className="availability-poll-management__success">
          <Check aria-hidden="true" /> {strings.responseSaved}
        </p>
      )}
      <Button
        disabled={respond.isPending || Object.keys(answers).length === 0}
        leadingIcon={<Send />}
        onClick={() => void submit()}
      >
        {strings.saveResponse}
      </Button>
    </section>
  );
}

function individualResponseLabel(
  response: AvailabilityPollIndividualResponse,
  index: number,
  strings: PageCopy,
) {
  const ordinal = index + 1;
  if (response.actor_type === "internal_member" || response.internal_user_id) {
    return `${strings.internalUser} ${ordinal}`;
  }
  if (response.participant_id) {
    return `${strings.participant} ${ordinal}`;
  }
  return `${strings.anonymousRespondent} ${ordinal}`;
}

function IndividualResponsesPanel({
  locale,
  poll,
  strings,
  tenantID,
}: {
  locale: string;
  poll: AvailabilityPoll;
  strings: PageCopy;
  tenantID: string;
}) {
  const responsesQuery = useAvailabilityPollIndividualResponses(
    tenantID,
    poll.id,
    poll.viewer_capabilities.can_view_individual_responses,
  );
  const responses =
    responsesQuery.data?.pages.flatMap((page) => page.responses) ?? [];
  const submittedAt = new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: poll.timezone,
  });
  const tableRegionID = `poll-individual-responses-${poll.id}`;

  return (
    <section
      aria-labelledby="poll-individual-responses-title"
      className="availability-poll-management__subpanel"
    >
      <h3 id="poll-individual-responses-title">
        {strings.individualResponses}
      </h3>
      <p>{strings.individualResponsesDescription}</p>
      {responsesQuery.isPending ? (
        <p aria-live="polite" role="status">
          {strings.loadingIndividualResponses}
        </p>
      ) : responsesQuery.isError && responses.length === 0 ? (
        <div className="availability-poll-management__query-error">
          <p role="alert">{strings.individualResponsesError}</p>
          <Button
            leadingIcon={<RefreshCw />}
            onClick={() => void responsesQuery.refetch()}
            variant="secondary"
          >
            {strings.retry}
          </Button>
        </div>
      ) : responses.length === 0 ? (
        <p>{strings.noIndividualResponses}</p>
      ) : (
        <>
          <p aria-live="polite" className="availability-poll-management__count">
            {strings.loadedIndividualResponses}: {responses.length}
          </p>
          <div
            aria-label={strings.individualResponsesTable}
            className="availability-poll-management__individual-responses"
            id={tableRegionID}
            role="region"
            tabIndex={0}
          >
            <table>
              <caption>{strings.individualResponsesTable}</caption>
              <thead>
                <tr>
                  <th scope="col">{strings.respondent}</th>
                  {poll.slots.map((slot) => (
                    <th key={slot.id} scope="col">
                      {formatSlot(
                        slot.starts_at,
                        slot.ends_at,
                        poll.timezone,
                        locale,
                      )}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {responses.map((response, index) => {
                  const answersBySlot = new Map(
                    response.answers.map((answer) => [
                      answer.slot_id,
                      answer.state,
                    ]),
                  );
                  return (
                    <tr key={response.response_id}>
                      <th scope="row">
                        <strong>
                          {individualResponseLabel(response, index, strings)}
                        </strong>
                        <small>
                          {strings.submittedAt}:{" "}
                          <time dateTime={response.submitted_at}>
                            {submittedAt.format(
                              new Date(response.submitted_at),
                            )}
                          </time>
                        </small>
                      </th>
                      {poll.slots.map((slot) => {
                        const state = answersBySlot.get(slot.id) ?? "unknown";
                        return (
                          <td key={slot.id}>
                            <span data-state={state}>
                              {state === "unknown"
                                ? strings.unknown
                                : strings[state]}
                            </span>
                          </td>
                        );
                      })}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          {responsesQuery.isFetchNextPageError && (
            <p className="availability-poll-management__error" role="alert">
              {strings.individualResponsesError}
            </p>
          )}
          {responsesQuery.hasNextPage && (
            <Button
              aria-controls={tableRegionID}
              disabled={responsesQuery.isFetchingNextPage}
              leadingIcon={<RefreshCw />}
              onClick={() => void responsesQuery.fetchNextPage()}
              variant="secondary"
            >
              {responsesQuery.isFetchingNextPage
                ? strings.loadingMoreIndividualResponses
                : strings.loadMoreIndividualResponses}
            </Button>
          )}
        </>
      )}
    </section>
  );
}

function PollDetail({
  canCreateCapability,
  pollID,
  strings,
  tenantID,
}: {
  canCreateCapability: boolean;
  pollID: string | null;
  strings: PageCopy;
  tenantID: string;
}) {
  const pollQuery = useAvailabilityPollDetail(tenantID, pollID ?? undefined);
  const poll = pollQuery.data;
  const summaryQuery = useAvailabilityPollSummary(
    tenantID,
    pollID ?? undefined,
    Boolean(poll && poll.status !== "draft" && poll.status !== "cancelled"),
  );
  const lifecycle = useAvailabilityPollLifecycle(tenantID);
  const finalize = useFinalizeAvailabilityPoll(tenantID);
  const share = useCreateAvailabilityPollCapability(tenantID);
  const [shareSecret, setShareSecret] =
    useState<AvailabilityPollCapabilitySecret | null>(null);
  const [copied, setCopied] = useState(false);
  const [reopenDeadline, setReopenDeadline] = useState(() =>
    localDateTimeInput(new Date(Date.now() + 48 * 60 * 60 * 1000)),
  );
  const [shareExpiry, setShareExpiry] = useState(() =>
    localDateTimeInput(new Date(Date.now() + 7 * 24 * 60 * 60 * 1000)),
  );
  const [participantID, setParticipantID] = useState("");
  const [selectedSlotID, setSelectedSlotID] = useState("");
  const [outcomeType, setOutcomeType] = useState<
    "study_meeting" | "class_session"
  >("study_meeting");
  const locale = strings === copy.vi ? "vi-VN" : "en-US";

  if (!pollID) {
    return (
      <EmptyState
        description={strings.choosePollDescription}
        title={strings.choosePoll}
      />
    );
  }
  if (pollQuery.isPending) {
    return (
      <SkeletonGroup label={strings.detailLoading}>
        <Skeleton height={30} width="52%" />
        <Skeleton height={120} />
        <Skeleton height={280} />
      </SkeletonGroup>
    );
  }
  if (pollQuery.isError || !poll) {
    const State = isForbidden(pollQuery.error) ? ForbiddenState : ErrorState;
    return (
      <State
        actions={
          <Button
            leadingIcon={<RefreshCw />}
            onClick={() => void pollQuery.refetch()}
            variant="secondary"
          >
            {strings.retry}
          </Button>
        }
        description={
          isForbidden(pollQuery.error)
            ? strings.forbiddenDescription
            : strings.loadErrorDescription
        }
        title={
          isForbidden(pollQuery.error) ? strings.forbidden : strings.detailError
        }
      />
    );
  }

  const rankedSlots = summaryQuery.data?.ranked_slots ?? [];
  const effectiveSelectedSlotID = rankedSlots.some(
    (ranked) => ranked.slot.id === selectedSlotID,
  )
    ? selectedSlotID
    : (rankedSlots[0]?.slot.id ?? "");
  const effectiveOutcomeType =
    (outcomeType === "study_meeting" &&
      poll.viewer_capabilities.can_finalize_study_meeting) ||
    (outcomeType === "class_session" &&
      poll.viewer_capabilities.can_finalize_class_session)
      ? outcomeType
      : poll.viewer_capabilities.can_finalize_study_meeting
        ? "study_meeting"
        : "class_session";

  const transition = async (kind: "open" | "close" | "cancel" | "reopen") => {
    if (kind === "reopen") {
      const deadlineResolution = resolveCivilDateTime(
        reopenDeadline,
        poll.timezone,
        "earlier",
      );
      if (deadlineResolution.kind !== "resolved") return;
      await lifecycle.mutateAsync({
        action: {
          deadlineAt: deadlineResolution.value,
          expectedVersion: poll.version,
          kind,
        },
        pollID: poll.id,
      });
      return;
    }
    await lifecycle.mutateAsync({
      action:
        kind === "cancel"
          ? {
              expectedVersion: poll.version,
              kind,
              reason: strings.cancelReason,
            }
          : { expectedVersion: poll.version, kind },
      pollID: poll.id,
    });
  };

  const issueShareLink = async () => {
    const expiryResolution = resolveCivilDateTime(
      shareExpiry,
      poll.timezone,
      "earlier",
    );
    if (expiryResolution.kind !== "resolved") return;
    const secret = await share.mutateAsync({
      input: {
        expected_version: poll.version,
        expires_at: expiryResolution.value,
        participant_id:
          poll.share_mode === "invited_only" ? participantID : null,
        scope:
          poll.share_mode === "invited_only"
            ? "invited_participant"
            : "public_link",
      },
      pollID: poll.id,
    });
    setShareSecret(secret);
    setCopied(false);
    share.reset();
  };

  const copyShareLink = async () => {
    if (!shareSecret) return;
    try {
      await navigator.clipboard.writeText(shareSecret.share_url);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  const finalizePoll = async () => {
    if (!effectiveSelectedSlotID) return;
    await finalize.mutateAsync({
      input: {
        class_id:
          effectiveOutcomeType === "class_session" ? poll.class_id : null,
        expected_version: poll.version,
        idempotency_key: crypto.randomUUID(),
        outcome_type: effectiveOutcomeType,
        slot_id: effectiveSelectedSlotID,
      },
      pollID: poll.id,
    });
  };

  const responseCount = summaryQuery.data?.response_count;
  return (
    <div className="availability-poll-management__detail">
      <header>
        <div>
          <p>{poll.description}</p>
          <h2>{poll.title}</h2>
          <span>
            {poll.timezone} · {poll.slots.length} {strings.slots}
          </span>
        </div>
        <StatusBadge tone={statusTone(poll.status)}>{poll.status}</StatusBadge>
      </header>
      <dl className="availability-poll-management__facts">
        <div>
          <dt>{strings.deadlineLabel}</dt>
          <dd>
            {new Intl.DateTimeFormat(locale, {
              dateStyle: "medium",
              timeStyle: "short",
              timeZone: poll.timezone,
            }).format(new Date(poll.deadline_at))}
          </dd>
        </div>
        <div>
          <dt>{strings.responses}</dt>
          <dd>{responseCount ?? "—"}</dd>
        </div>
        <div>
          <dt>{strings.status}</dt>
          <dd>{poll.status}</dd>
        </div>
      </dl>

      {poll.viewer_capabilities.can_manage && (
        <div
          className="availability-poll-management__actions"
          aria-label={strings.status}
        >
          {poll.status === "draft" && (
            <Button
              leadingIcon={<Check />}
              onClick={() => void transition("open").catch(() => undefined)}
            >
              {strings.open}
            </Button>
          )}
          {poll.status === "open" && (
            <Button
              leadingIcon={<Square />}
              onClick={() => void transition("close").catch(() => undefined)}
              variant="secondary"
            >
              {strings.close}
            </Button>
          )}
          {poll.status === "closed" && (
            <label className="availability-poll-management__inline-action">
              <span>{strings.deadline}</span>
              <input
                onChange={(event) => setReopenDeadline(event.target.value)}
                type="datetime-local"
                value={reopenDeadline}
              />
              <Button
                leadingIcon={<RotateCcw />}
                onClick={() => void transition("reopen").catch(() => undefined)}
                variant="secondary"
              >
                {strings.reopen}
              </Button>
            </label>
          )}
          {["draft", "open", "closed"].includes(poll.status) && (
            <Button
              leadingIcon={<X />}
              onClick={() => void transition("cancel").catch(() => undefined)}
              variant="danger"
            >
              {strings.cancel}
            </Button>
          )}
        </div>
      )}
      {lifecycle.error && (
        <p className="availability-poll-management__error" role="alert">
          {mutationMessage(lifecycle.error, strings.lifecycleError, strings)}
        </p>
      )}

      <PollResponseEditor
        key={poll.id}
        poll={poll}
        strings={strings}
        tenantID={tenantID}
      />

      {poll.viewer_capabilities.can_view_individual_responses && (
        <IndividualResponsesPanel
          locale={locale}
          poll={poll}
          strings={strings}
          tenantID={tenantID}
        />
      )}

      {poll.viewer_capabilities.can_share &&
        !["finalized", "cancelled"].includes(poll.status) && (
          <section
            aria-labelledby="poll-share-title"
            className="availability-poll-management__subpanel"
          >
            <h3 id="poll-share-title">
              <Link2 aria-hidden="true" /> {strings.shareHeading}
            </h3>
            {poll.share_mode === "class_members" ? (
              <p>{strings.shareClassOnly}</p>
            ) : (
              <>
                <div className="availability-poll-management__form-grid">
                  {poll.share_mode === "invited_only" && (
                    <label>
                      <span>{strings.shareParticipant}</span>
                      <select
                        onChange={(event) =>
                          setParticipantID(event.target.value)
                        }
                        value={participantID}
                      >
                        <option value="">—</option>
                        {poll.participants
                          .filter(
                            (participant) => participant.status === "active",
                          )
                          .map((participant) => (
                            <option key={participant.id} value={participant.id}>
                              {participant.id}
                            </option>
                          ))}
                      </select>
                    </label>
                  )}
                  <label>
                    <span>{strings.shareExpiry}</span>
                    <input
                      onChange={(event) => setShareExpiry(event.target.value)}
                      type="datetime-local"
                      value={shareExpiry}
                    />
                  </label>
                </div>
                <Button
                  disabled={
                    !canCreateCapability ||
                    share.isPending ||
                    (poll.share_mode === "invited_only" && !participantID)
                  }
                  leadingIcon={<Link2 />}
                  onClick={() => void issueShareLink().catch(() => undefined)}
                  variant="secondary"
                >
                  {strings.share}
                </Button>
                {!canCreateCapability && (
                  <p className="availability-poll-management__notice">
                    <LockKeyhole aria-hidden="true" />
                    {strings.operationUnavailable}
                  </p>
                )}
                {share.error && (
                  <p
                    className="availability-poll-management__error"
                    role="alert"
                  >
                    {mutationMessage(share.error, strings.required, strings)}
                  </p>
                )}
                {shareSecret && (
                  <div
                    className="availability-poll-management__secret"
                    role="status"
                  >
                    <p>
                      <LockKeyhole aria-hidden="true" /> {strings.shareOnce}
                    </p>
                    <input
                      aria-label={strings.share}
                      readOnly
                      value={shareSecret.share_url}
                    />
                    <Button
                      leadingIcon={copied ? <Check /> : <Clipboard />}
                      onClick={() => void copyShareLink()}
                      variant="secondary"
                    >
                      {copied ? strings.copied : strings.copyLink}
                    </Button>
                  </div>
                )}
              </>
            )}
          </section>
        )}

      <section
        aria-labelledby="poll-ranking-title"
        className="availability-poll-management__subpanel"
      >
        <h3 id="poll-ranking-title">{strings.summary}</h3>
        {summaryQuery.isPending ? (
          <SkeletonGroup label={strings.summary}>
            <Skeleton height={100} />
          </SkeletonGroup>
        ) : summaryQuery.isError ? (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void summaryQuery.refetch()}
                variant="secondary"
              >
                {strings.retry}
              </Button>
            }
            description={strings.loadErrorDescription}
            title={strings.detailError}
          />
        ) : summaryQuery.data?.ranked_slots.length ? (
          <ol className="availability-poll-management__ranking">
            {summaryQuery.data.ranked_slots.map((ranked) => (
              <li key={ranked.slot.id}>
                <strong>#{ranked.rank}</strong>
                <span>
                  {formatSlot(
                    ranked.slot.starts_at,
                    ranked.slot.ends_at,
                    poll.timezone,
                    locale,
                  )}
                </span>
                {ranked.preferred_count !== null && (
                  <small>
                    {strings.preferred}: {ranked.preferred_count} ·{" "}
                    {strings.available}: {ranked.available_count} ·{" "}
                    {strings.unavailable}: {ranked.unavailable_count}
                  </small>
                )}
              </li>
            ))}
          </ol>
        ) : (
          <p>{strings.noSummary}</p>
        )}
      </section>

      {poll.status === "closed" &&
      (poll.viewer_capabilities.can_finalize_study_meeting ||
        poll.viewer_capabilities.can_finalize_class_session) &&
      summaryQuery.data?.ranked_slots.length ? (
        <section
          aria-labelledby="poll-finalize-title"
          className="availability-poll-management__subpanel"
        >
          <h3 id="poll-finalize-title">
            <CalendarClock aria-hidden="true" /> {strings.finalize}
          </h3>
          <div className="availability-poll-management__form-grid">
            <label>
              <span>{strings.selectedTime}</span>
              <select
                onChange={(event) => setSelectedSlotID(event.target.value)}
                value={effectiveSelectedSlotID}
              >
                {summaryQuery.data.ranked_slots.map((ranked) => (
                  <option key={ranked.slot.id} value={ranked.slot.id}>
                    #{ranked.rank} ·{" "}
                    {formatSlot(
                      ranked.slot.starts_at,
                      ranked.slot.ends_at,
                      poll.timezone,
                      locale,
                    )}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>{strings.finalizeOutcome}</span>
              <select
                onChange={(event) =>
                  setOutcomeType(
                    event.target.value as "study_meeting" | "class_session",
                  )
                }
                value={effectiveOutcomeType}
              >
                {poll.viewer_capabilities.can_finalize_study_meeting && (
                  <option value="study_meeting">{strings.finalizeStudy}</option>
                )}
                {poll.viewer_capabilities.can_finalize_class_session && (
                  <option value="class_session">{strings.finalizeClass}</option>
                )}
              </select>
            </label>
          </div>
          {finalize.error && (
            <p className="availability-poll-management__error" role="alert">
              {mutationMessage(finalize.error, strings.required, strings)}
            </p>
          )}
          <Button
            disabled={finalize.isPending || !effectiveSelectedSlotID}
            leadingIcon={<CalendarClock />}
            onClick={() => void finalizePoll().catch(() => undefined)}
          >
            {strings.finalize}
          </Button>
        </section>
      ) : null}
    </div>
  );
}

function StudyMeetingPanel({
  canCreate,
  strings,
  tenantID,
  timezone: initialTimezone,
}: {
  canCreate: boolean;
  strings: PageCopy;
  tenantID: string;
  timezone: string;
}) {
  const meetingsQuery = useStudyMeetingList(tenantID);
  const createMeeting = useCreateStudyMeeting(tenantID);
  const cancelMeeting = useCancelStudyMeeting(tenantID);
  const [title, setTitle] = useState("");
  const [timezone, setTimezone] = useState(initialTimezone);
  const [startsAt, setStartsAt] = useState(() =>
    localDateTimeInput(new Date(Date.now() + 24 * 60 * 60 * 1000)),
  );
  const [endsAt, setEndsAt] = useState(() =>
    localDateTimeInput(new Date(Date.now() + 25 * 60 * 60 * 1000)),
  );
  const [error, setError] = useState<string | null>(null);
  const locale = strings === copy.vi ? "vi-VN" : "en-US";

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setError(null);
    const start = resolveCivilDateTime(startsAt, timezone, "earlier");
    const end = resolveCivilDateTime(endsAt, timezone, "earlier");
    if (
      !title.trim() ||
      start.kind !== "resolved" ||
      end.kind !== "resolved" ||
      Temporal.Instant.compare(
        Temporal.Instant.from(start.value),
        Temporal.Instant.from(end.value),
      ) >= 0
    ) {
      setError(strings.required);
      return;
    }
    try {
      await createMeeting.mutateAsync({
        class_id: null,
        ends_at: end.value,
        idempotency_key: crypto.randomUUID(),
        starts_at: start.value,
        timezone,
        title: title.trim(),
      });
      setTitle("");
    } catch (mutationError) {
      setError(
        mutationMessage(mutationError, strings.meetingCreateError, strings),
      );
    }
  };

  return (
    <section
      aria-labelledby="study-meetings-title"
      className="availability-poll-management__panel availability-poll-management__meetings"
    >
      <div className="availability-poll-management__panel-heading">
        <span aria-hidden="true">
          <CalendarClock />
        </span>
        <div>
          <h2 id="study-meetings-title">{strings.meetings}</h2>
          <p>{strings.meetingsDescription}</p>
        </div>
      </div>
      <form
        className="availability-poll-management__meeting-form"
        onSubmit={(event) => void submit(event)}
      >
        <TextField
          label={strings.meetingTitle}
          onChange={(event) => setTitle(event.target.value)}
          required
          value={title}
        />
        <label>
          <span>{strings.startsAt}</span>
          <input
            onChange={(event) => setStartsAt(event.target.value)}
            required
            type="datetime-local"
            value={startsAt}
          />
        </label>
        <label>
          <span>{strings.endsAt}</span>
          <input
            onChange={(event) => setEndsAt(event.target.value)}
            required
            type="datetime-local"
            value={endsAt}
          />
        </label>
        <TextField
          label={strings.timezone}
          onChange={(event) => setTimezone(event.target.value)}
          required
          value={timezone}
        />
        <Button
          disabled={!canCreate || createMeeting.isPending}
          leadingIcon={<Plus />}
          type="submit"
        >
          {strings.createMeeting}
        </Button>
      </form>
      {!canCreate && (
        <p className="availability-poll-management__notice">
          <LockKeyhole aria-hidden="true" /> {strings.operationUnavailable}
        </p>
      )}
      {error && (
        <p className="availability-poll-management__error" role="alert">
          {error}
        </p>
      )}
      {cancelMeeting.error && (
        <p className="availability-poll-management__error" role="alert">
          {mutationMessage(
            cancelMeeting.error,
            strings.meetingCancelError,
            strings,
          )}
        </p>
      )}
      {meetingsQuery.isPending ? (
        <SkeletonGroup label={strings.meetings}>
          <Skeleton height={120} />
        </SkeletonGroup>
      ) : meetingsQuery.isError ? (
        isForbidden(meetingsQuery.error) ? (
          <ForbiddenState
            description={strings.forbiddenDescription}
            title={strings.forbidden}
          />
        ) : (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void meetingsQuery.refetch()}
                variant="secondary"
              >
                {strings.retry}
              </Button>
            }
            description={strings.loadErrorDescription}
            title={strings.meetingLoadError}
          />
        )
      ) : meetingsQuery.data.meetings.length === 0 ? (
        <EmptyState
          description={strings.noMeetingsDescription}
          title={strings.noMeetings}
        />
      ) : (
        <ul
          aria-label={strings.meetings}
          className="availability-poll-management__meeting-list"
        >
          {meetingsQuery.data.meetings.map((meeting) => (
            <li key={meeting.id}>
              <div>
                <strong>{meeting.title}</strong>
                <span>
                  {formatSlot(
                    meeting.starts_at,
                    meeting.ends_at,
                    meeting.timezone,
                    locale,
                  )}
                </span>
              </div>
              <StatusBadge tone={statusTone(meeting.status)}>
                {meeting.status === "scheduled"
                  ? strings.scheduled
                  : strings.cancelled}
              </StatusBadge>
              {meeting.status === "scheduled" && (
                <Button
                  disabled={cancelMeeting.isPending}
                  onClick={() =>
                    void cancelMeeting
                      .mutateAsync({
                        meetingID: meeting.id,
                        reason: strings.cancelReason,
                        version: meeting.version,
                      })
                      .catch(() => undefined)
                  }
                  variant="danger"
                >
                  {strings.cancelMeeting}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function AvailabilityPollManagementPage() {
  const { language } = useI18n();
  const strings = copy[language];
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const timezone =
    session.currentUser?.user.timezone ||
    Intl.DateTimeFormat().resolvedOptions().timeZone ||
    "UTC";
  const capabilities = useTenantCapabilities(tenantID);
  const pollsQuery = useAvailabilityPollList(tenantID);
  const [selectedPollID, setSelectedPollID] = useState<string | null>(null);
  const createPollAvailability = tenantOperationAvailability(
    capabilities,
    "create_availability_poll",
  );
  const createMeetingAvailability = tenantOperationAvailability(
    capabilities,
    "schedule_study_meeting",
  );
  const createCapabilityAvailability = tenantOperationAvailability(
    capabilities,
    "create_availability_poll_capability",
  );
  const availablePolls = pollsQuery.data?.polls ?? [];
  const effectiveSelectedPollID = availablePolls.some(
    (poll) => poll.id === selectedPollID,
  )
    ? selectedPollID
    : (availablePolls[0]?.id ?? null);

  if (!tenantID) {
    return (
      <div className="page-content availability-poll-management">
        <ForbiddenState
          description={strings.forbiddenDescription}
          title={strings.forbidden}
        />
      </div>
    );
  }
  if (pollsQuery.isPending || capabilities.isPending) {
    return (
      <div className="page-content availability-poll-management">
        <SkeletonGroup label={strings.loading}>
          <Skeleton height={44} width="42%" />
          <Skeleton height={180} />
          <Skeleton height={420} />
        </SkeletonGroup>
      </div>
    );
  }
  if (pollsQuery.isError || capabilities.isError) {
    const forbidden =
      isForbidden(pollsQuery.error) || isForbidden(capabilities.error);
    const State = forbidden ? ForbiddenState : ErrorState;
    return (
      <div className="page-content availability-poll-management">
        <State
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() =>
                void Promise.all([pollsQuery.refetch(), capabilities.refetch()])
              }
              variant="secondary"
            >
              {strings.retry}
            </Button>
          }
          description={
            forbidden
              ? strings.forbiddenDescription
              : strings.loadErrorDescription
          }
          title={forbidden ? strings.forbidden : strings.loadError}
        />
      </div>
    );
  }

  return (
    <div className="page-content availability-poll-management">
      <header className="page-heading availability-poll-management__header">
        <div>
          <p>{strings.kicker}</p>
          <h1>{strings.title}</h1>
          <span>{strings.description}</span>
        </div>
        <p className="availability-poll-management__boundary">
          <LockKeyhole aria-hidden="true" /> {strings.coreBoundary}
        </p>
      </header>
      <PollCreateEditor
        canCreate={createPollAvailability.available}
        onCreated={setSelectedPollID}
        strings={strings}
        tenantID={tenantID}
        timezone={timezone}
      />
      <div className="availability-poll-management__workspace">
        <section
          aria-labelledby="availability-poll-list-title"
          className="availability-poll-management__panel availability-poll-management__sidebar"
        >
          <div className="availability-poll-management__panel-heading availability-poll-management__panel-heading--inline">
            <div>
              <h2 id="availability-poll-list-title">{strings.polls}</h2>
            </div>
            <Button
              aria-label={strings.refresh}
              leadingIcon={<RefreshCw />}
              onClick={() => void pollsQuery.refetch()}
              variant="quiet"
            >
              {strings.refresh}
            </Button>
          </div>
          <PollList
            onSelect={setSelectedPollID}
            polls={pollsQuery.data.polls}
            selectedPollID={effectiveSelectedPollID}
            strings={strings}
          />
        </section>
        <section
          aria-label={strings.choosePoll}
          className="availability-poll-management__panel availability-poll-management__poll-detail"
        >
          <PollDetail
            canCreateCapability={createCapabilityAvailability.available}
            key={effectiveSelectedPollID ?? "unselected"}
            pollID={effectiveSelectedPollID}
            strings={strings}
            tenantID={tenantID}
          />
        </section>
      </div>
      <StudyMeetingPanel
        canCreate={createMeetingAvailability.available}
        strings={strings}
        tenantID={tenantID}
        timezone={timezone}
      />
    </div>
  );
}
