import createClient from "openapi-fetch";
import type { components, paths } from "./generated/schema";

export type HealthResponse = components["schemas"]["HealthResponse"];
export type CurrentUser = components["schemas"]["MeResponse"];
export type CSRFResponse = components["schemas"]["CSRFResponse"];
export type LogoutResponse = components["schemas"]["LogoutResponse"];
export type UserProfile = components["schemas"]["User"];
export type ProfileResponse = components["schemas"]["ProfileResponse"];
export type ProfileUpdateRequest =
  components["schemas"]["ProfileUpdateRequest"];
export type ExternalIdentity = components["schemas"]["ExternalIdentity"];
export type IdentityListResponse =
  components["schemas"]["IdentityListResponse"];
export type IdentityLinkResponse =
  components["schemas"]["IdentityLinkResponse"];
export type CreateTenantRequest = components["schemas"]["CreateTenantRequest"];
export type TenantStatus = components["schemas"]["TenantStatus"];
export type TenantMembership = components["schemas"]["TenantMembership"];
export type Tenant = components["schemas"]["Tenant"];
export type TenantListResponse = components["schemas"]["TenantListResponse"];
export type FeatureCapability = components["schemas"]["FeatureCapability"];
export type QuotaCapability = components["schemas"]["QuotaCapability"];
export type OperationCapability = components["schemas"]["OperationCapability"];
export type TenantFeatureCapabilities =
  components["schemas"]["TenantFeatureCapabilities"];
export type TenantQuotaCapabilities =
  components["schemas"]["TenantQuotaCapabilities"];
export type TenantOperationCapabilities =
  components["schemas"]["TenantOperationCapabilities"];
export type TenantCapabilities = components["schemas"]["TenantCapabilities"];
export type HomeRecentFile = components["schemas"]["HomeRecentFile"];
export type HomeRecentFilePage = components["schemas"]["HomeRecentFilePage"];
export type AuthorizedSearchResultKind =
  components["schemas"]["AuthorizedSearchResultKind"];
export type AuthorizedSearchResult =
  components["schemas"]["AuthorizedSearchResult"];
export type AuthorizedSearchPage =
  components["schemas"]["AuthorizedSearchPage"];
export type TenantFeatureControlValues =
  components["schemas"]["TenantFeatureControlValues"];
export type TenantQuotaControlValues =
  components["schemas"]["TenantQuotaControlValues"];
export type UpdateTenantFeatureControlsRequest =
  components["schemas"]["UpdateTenantFeatureControlsRequest"];
export type ContentFileStatus = components["schemas"]["ContentFileStatus"];
export type ContentFile = components["schemas"]["ContentFile"];
export type ContentFileViewerAccess =
  components["schemas"]["ContentFileViewerAccess"];
export type ContentFilePage = components["schemas"]["ContentFilePage"];
export type CreateFileUploadIntentRequest =
  components["schemas"]["CreateFileUploadIntentRequest"];
export type FinalizeFileUploadRequest =
  components["schemas"]["FinalizeFileUploadRequest"];
export type IssueFileUploadCapabilityRequest =
  components["schemas"]["IssueFileUploadCapabilityRequest"];
export type FileUploadCapability =
  components["schemas"]["FileUploadCapability"];
export type FileDownloadCapability =
  components["schemas"]["FileDownloadCapability"];
export type CreateFileMultipartUploadRequest =
  components["schemas"]["CreateFileMultipartUploadRequest"];
export type AbortFileMultipartUploadRequest =
  components["schemas"]["AbortFileMultipartUploadRequest"];
export type FileMultipartUpload = components["schemas"]["FileMultipartUpload"];
export type IssueFileMultipartPartCapabilityRequest =
  components["schemas"]["IssueFileMultipartPartCapabilityRequest"];
export type FileMultipartPartCapability =
  components["schemas"]["FileMultipartPartCapability"];
export type CompleteFileMultipartUploadRequest =
  components["schemas"]["CompleteFileMultipartUploadRequest"];
export type CompleteFileMultipartUploadResult =
  components["schemas"]["CompleteFileMultipartUploadResult"];
export type ConversationKind = components["schemas"]["ConversationKind"];
export type ConversationParticipant =
  components["schemas"]["ConversationParticipant"];
export type ConversationViewerAccess =
  components["schemas"]["ConversationViewerAccess"];
export type Conversation = components["schemas"]["Conversation"];
export type ConversationPage = components["schemas"]["ConversationPage"];
export type CreateDirectConversationRequest =
  components["schemas"]["CreateDirectConversationRequest"];
export type MessageState = components["schemas"]["MessageState"];
export type MessageAuthor = components["schemas"]["MessageAuthor"];
export type ActiveMessage = components["schemas"]["ActiveMessage"];
export type DeletedMessage = components["schemas"]["DeletedMessage"];
export type Message = components["schemas"]["Message"];
export type MessageReadState = components["schemas"]["MessageReadState"];
export type MessagePage = components["schemas"]["MessagePage"];
export type SendMessageRequest = components["schemas"]["SendMessageRequest"];
export type EditMessageRequest = components["schemas"]["EditMessageRequest"];
export type DeleteMessageRequest =
  components["schemas"]["DeleteMessageRequest"];
export type MarkConversationReadRequest =
  components["schemas"]["MarkConversationReadRequest"];
export type Notification = components["schemas"]["Notification"];
export type NotificationPage = components["schemas"]["NotificationPage"];
export type NotificationUnreadCount =
  components["schemas"]["NotificationUnreadCount"];
export type NotificationPreference =
  components["schemas"]["NotificationPreference"];
export type UpdateNotificationPreferenceRequest =
  components["schemas"]["UpdateNotificationPreferenceRequest"];
export type CalendarItem = components["schemas"]["CalendarItem"];
export type CalendarItemStatus = components["schemas"]["CalendarItemStatus"];
export type CalendarSourceType = components["schemas"]["CalendarSourceType"];
export type CalendarItemListResponse =
  components["schemas"]["CalendarItemListResponse"];
export type CalendarDisplayPreference =
  components["schemas"]["CalendarDisplayPreference"];
export type UpdateCalendarDisplayPreferenceRequest =
  components["schemas"]["UpdateCalendarDisplayPreferenceRequest"];
export type CalendarWorkingSchedule =
  components["schemas"]["CalendarWorkingSchedule"];
export type UpdateCalendarWorkingScheduleRequest =
  components["schemas"]["UpdateCalendarWorkingScheduleRequest"];
export type CalendarAvailabilityQueryRequest =
  components["schemas"]["CalendarAvailabilityQueryRequest"];
export type CalendarAvailabilityQueryResponse =
  components["schemas"]["CalendarAvailabilityQueryResponse"];
export type CalendarAvailabilityParticipantReference =
  components["schemas"]["CalendarAvailabilityParticipantReference"];
export type CalendarAvailabilityStatus =
  components["schemas"]["CalendarAvailabilityStatus"];
export type CalendarParticipantAvailability =
  components["schemas"]["CalendarParticipantAvailability"];
export type CalendarSuggestedTime =
  components["schemas"]["CalendarSuggestedTime"];
export type MarkAllNotificationsReadResponse =
  components["schemas"]["MarkAllNotificationsReadResponse"];
export type AuditAction = components["schemas"]["AuditAction"];
export type AuditOutcome = components["schemas"]["AuditOutcome"];
export type AuditActor = components["schemas"]["AuditActor"];
export type AuditResource = components["schemas"]["AuditResource"];
export type AuditEvent = components["schemas"]["AuditEvent"];
export type AuditEventPage = components["schemas"]["AuditEventPage"];
export type MembershipInvitationStatus =
  components["schemas"]["MembershipInvitationStatus"];
export type InvitableOrganizationRole =
  components["schemas"]["InvitableOrganizationRole"];
export type MembershipInvitation =
  components["schemas"]["MembershipInvitation"];
export type MembershipInvitationListResponse =
  components["schemas"]["MembershipInvitationListResponse"];
export type CreateMembershipInvitationRequest =
  components["schemas"]["CreateMembershipInvitationRequest"];
export type CreateMembershipInvitationResponse =
  components["schemas"]["CreateMembershipInvitationResponse"];
export type MembershipInvitationTokenRequest =
  components["schemas"]["MembershipInvitationTokenRequest"];
export type MembershipInvitationPreview =
  components["schemas"]["MembershipInvitationPreview"];
export type MembershipInvitationAcceptResponse =
  components["schemas"]["MembershipInvitationAcceptResponse"];
type GeneratedUpdateTenantRequest =
  components["schemas"]["UpdateTenantRequest"];
export type UpdateTenantRequest = GeneratedUpdateTenantRequest &
  (
    | Required<Pick<GeneratedUpdateTenantRequest, "name">>
    | Required<Pick<GeneratedUpdateTenantRequest, "slug">>
    | Required<Pick<GeneratedUpdateTenantRequest, "locale">>
    | Required<Pick<GeneratedUpdateTenantRequest, "timezone">>
  );
export type ArchiveTenantRequest =
  components["schemas"]["ArchiveTenantRequest"];
export type SwitchActiveTenantRequest =
  components["schemas"]["SwitchActiveTenantRequest"];
export type ClassroomClass = components["schemas"]["Class"];
export type ClassStatus = components["schemas"]["ClassStatus"];
export type ClassListResponse = components["schemas"]["ClassListResponse"];
export type CreateClassRequest = components["schemas"]["CreateClassRequest"];
type GeneratedUpdateClassRequest = components["schemas"]["UpdateClassRequest"];
export type UpdateClassRequest = GeneratedUpdateClassRequest &
  (
    | Required<Pick<GeneratedUpdateClassRequest, "code">>
    | Required<Pick<GeneratedUpdateClassRequest, "title">>
    | Required<Pick<GeneratedUpdateClassRequest, "description">>
    | Required<Pick<GeneratedUpdateClassRequest, "timezone">>
    | Required<Pick<GeneratedUpdateClassRequest, "status">>
  );
export type ClassVersionRequest = components["schemas"]["ClassVersionRequest"];
export type TransferClassOwnershipRequest =
  components["schemas"]["TransferClassOwnershipRequest"];
export type ClassEnrollmentStatus =
  components["schemas"]["ClassEnrollmentStatus"];
export type ClassEnrollmentRole = components["schemas"]["ClassEnrollmentRole"];
export type ClassViewerAccess = components["schemas"]["ClassViewerAccess"];
export type ClassSessionStatus = components["schemas"]["ClassSessionStatus"];
export type ClassSessionViewerAccess =
  components["schemas"]["ClassSessionViewerAccess"];
export type ClassSession = components["schemas"]["ClassSession"];
export type ClassSessionListResponse =
  components["schemas"]["ClassSessionListResponse"];
export type CreateClassSessionRequest =
  components["schemas"]["CreateClassSessionRequest"];
type GeneratedUpdateClassSessionRequest =
  components["schemas"]["UpdateClassSessionRequest"];
export type UpdateClassSessionRequest = GeneratedUpdateClassSessionRequest &
  (
    | Required<Pick<GeneratedUpdateClassSessionRequest, "title">>
    | Required<Pick<GeneratedUpdateClassSessionRequest, "description">>
    | Required<Pick<GeneratedUpdateClassSessionRequest, "starts_at">>
    | Required<Pick<GeneratedUpdateClassSessionRequest, "ends_at">>
    | Required<Pick<GeneratedUpdateClassSessionRequest, "timezone">>
  );
export type CancelClassSessionRequest =
  components["schemas"]["CancelClassSessionRequest"];
export type SessionParticipationRole =
  components["schemas"]["SessionParticipationRole"];
export type SessionParticipationBusinessRole =
  components["schemas"]["SessionParticipationBusinessRole"];
export type SessionRSVPState = components["schemas"]["SessionRSVPState"];
export type SessionSelfRSVPState =
  components["schemas"]["SessionSelfRSVPState"];
export type SessionAudienceViewerAccess =
  components["schemas"]["SessionAudienceViewerAccess"];
export type SessionAudienceAttendee =
  components["schemas"]["SessionAudienceAttendee"];
export type SessionAudienceExternalAttendee =
  components["schemas"]["SessionAudienceExternalAttendee"];
export type SessionAudience = components["schemas"]["SessionAudience"];
export type ReplaceSessionAudienceRequest =
  components["schemas"]["ReplaceSessionAudienceRequest"];
export type ReplaceSessionAudienceExternalAttendeeRequest =
  components["schemas"]["ReplaceSessionAudienceExternalAttendeeRequest"];
export type ReplaceSessionAudienceResponse =
  components["schemas"]["ReplaceSessionAudienceResponse"];
export type TransferSessionOrganizerRequest =
  components["schemas"]["TransferSessionOrganizerRequest"];
export type TransferSessionOrganizerResponse =
  components["schemas"]["TransferSessionOrganizerResponse"];
export type RespondToClassSessionRequest =
  components["schemas"]["RespondToClassSessionRequest"];
export type SelfRSVPResponse = components["schemas"]["SelfRSVPResponse"];
export type ResolveExternalCalendarRSVPRequest =
  components["schemas"]["ResolveExternalCalendarRSVPRequest"];
export type RespondExternalCalendarRSVPRequest =
  components["schemas"]["RespondExternalCalendarRSVPRequest"];
export type ExternalCalendarRSVPProjection =
  components["schemas"]["ExternalCalendarRSVPProjection"];
export type ExternalCalendarRSVPMutationResponse =
  components["schemas"]["ExternalCalendarRSVPMutationResponse"];
export type AvailabilityPollAnswerState =
  components["schemas"]["AvailabilityPollAnswerState"];
export type AvailabilityPollAnswerInput =
  components["schemas"]["AvailabilityPollAnswerInput"];
export type AvailabilityPollAnswer =
  components["schemas"]["AvailabilityPollAnswer"];
export type AvailabilityPollAggregateBucket =
  components["schemas"]["AvailabilityPollAggregateBucket"];
export type AvailabilityPollStatus =
  components["schemas"]["AvailabilityPollStatus"];
export type AvailabilityPoll = components["schemas"]["AvailabilityPoll"];
export type AvailabilityPollCapability =
  components["schemas"]["AvailabilityPollCapability"];
export type AvailabilityPollCapabilityScope =
  components["schemas"]["AvailabilityPollCapabilityScope"];
export type AvailabilityPollCapabilitySecret =
  components["schemas"]["AvailabilityPollCapabilitySecret"];
export type AvailabilityPollListResponse =
  components["schemas"]["AvailabilityPollListResponse"];
export type AvailabilityPollMutationResponse =
  components["schemas"]["AvailabilityPollMutationResponse"];
export type AvailabilityPollOutcomeReference =
  components["schemas"]["AvailabilityPollOutcomeReference"];
export type AvailabilityPollOutcomeType =
  components["schemas"]["AvailabilityPollOutcomeType"];
export type AvailabilityPollParticipant =
  components["schemas"]["AvailabilityPollParticipant"];
export type AvailabilityPollParticipantInput =
  components["schemas"]["AvailabilityPollParticipantInput"];
export type AvailabilityPollParticipantKind =
  components["schemas"]["AvailabilityPollParticipantKind"];
export type AvailabilityPollParticipantStatus =
  components["schemas"]["AvailabilityPollParticipantStatus"];
export type AvailabilityPollRankedSlot =
  components["schemas"]["AvailabilityPollRankedSlot"];
export type AvailabilityPollResponseProjection =
  components["schemas"]["AvailabilityPollResponseProjection"];
export type AvailabilityPollResponseActorType =
  components["schemas"]["AvailabilityPollResponseActorType"];
export type AvailabilityPollIndividualResponse =
  components["schemas"]["AvailabilityPollIndividualResponse"];
export type AvailabilityPollIndividualResponsePage =
  components["schemas"]["AvailabilityPollIndividualResponsePage"];
export type AvailabilityPollShareMode =
  components["schemas"]["AvailabilityPollShareMode"];
export type AvailabilityPollSlot =
  components["schemas"]["AvailabilityPollSlot"];
export type AvailabilityPollSlotInput =
  components["schemas"]["AvailabilityPollSlotInput"];
export type AvailabilityPollSummary =
  components["schemas"]["AvailabilityPollSummary"];
export type AvailabilityPollVersionRequest =
  components["schemas"]["AvailabilityPollVersionRequest"];
export type AvailabilityPollViewerCapabilities =
  components["schemas"]["AvailabilityPollViewerCapabilities"];
export type CreateAvailabilityPollRequest =
  components["schemas"]["CreateAvailabilityPollRequest"];
export type UpdateAvailabilityPollRequest =
  components["schemas"]["UpdateAvailabilityPollRequest"];
export type CancelAvailabilityPollRequest =
  components["schemas"]["CancelAvailabilityPollRequest"];
export type ReopenAvailabilityPollRequest =
  components["schemas"]["ReopenAvailabilityPollRequest"];
export type RespondAvailabilityPollRequest =
  components["schemas"]["RespondAvailabilityPollRequest"];
export type CreateAvailabilityPollCapabilityRequest =
  components["schemas"]["CreateAvailabilityPollCapabilityRequest"];
export type RevokeAvailabilityPollCapabilityRequest =
  components["schemas"]["RevokeAvailabilityPollCapabilityRequest"];
export type FinalizeAvailabilityPollRequest =
  components["schemas"]["FinalizeAvailabilityPollRequest"];
export type PublicAvailabilityPoll =
  components["schemas"]["PublicAvailabilityPoll"];
export type PublicAvailabilityPollExchange =
  components["schemas"]["PublicAvailabilityPollExchange"];
export type PublicAvailabilityPollMutationResponse =
  components["schemas"]["PublicAvailabilityPollMutationResponse"];
export type ResolvePublicAvailabilityPollRequest =
  components["schemas"]["ResolvePublicAvailabilityPollRequest"];
export type RespondPublicAvailabilityPollRequest =
  components["schemas"]["RespondPublicAvailabilityPollRequest"];
export type StudyMeeting = components["schemas"]["StudyMeeting"];
export type StudyMeetingStatus = components["schemas"]["StudyMeetingStatus"];
export type StudyMeetingListResponse =
  components["schemas"]["StudyMeetingListResponse"];
export type CreateStudyMeetingRequest =
  components["schemas"]["CreateStudyMeetingRequest"];
export type UpdateStudyMeetingRequest =
  components["schemas"]["UpdateStudyMeetingRequest"];
export type CancelStudyMeetingRequest =
  components["schemas"]["CancelStudyMeetingRequest"];
export type ClassSessionMediaSpaceSource =
  components["schemas"]["ClassSessionMediaSpaceSource"];
export type ClassSessionOccurrenceMediaSpaceSource =
  components["schemas"]["ClassSessionOccurrenceMediaSpaceSource"];
export type StudyMeetingMediaSpaceSource =
  components["schemas"]["StudyMeetingMediaSpaceSource"];
export type InstantMediaSpaceSourceInput =
  components["schemas"]["InstantMediaSpaceSourceInput"];
export type MediaSpaceSource = components["schemas"]["MediaSpaceSource"];
export type CreateMediaSpaceSourceInput =
  components["schemas"]["CreateMediaSpaceSourceInput"];
export type MediaSpaceStatus = components["schemas"]["MediaSpaceStatus"];
export type MediaRoomInstanceStatus =
  components["schemas"]["MediaRoomInstanceStatus"];
export type MediaRoomInstance = components["schemas"]["MediaRoomInstance"];
export type MediaSpaceViewerOperations =
  components["schemas"]["MediaSpaceViewerOperations"];
export type MediaSpace = components["schemas"]["MediaSpace"];
export type CreateMediaSpaceRequest =
  components["schemas"]["CreateMediaSpaceRequest"];
export type MediaSpaceTransitionRequest =
  components["schemas"]["MediaSpaceTransitionRequest"];
export type RecoverMediaSpaceRequest =
  components["schemas"]["RecoverMediaSpaceRequest"];
export type MediaParticipantKey = components["schemas"]["MediaParticipantKey"];
export type MediaParticipantConnectionState =
  components["schemas"]["MediaParticipantConnectionState"];
export type MediaParticipantModerationOperations =
  components["schemas"]["MediaParticipantModerationOperations"];
export type MediaParticipant = components["schemas"]["MediaParticipant"];
export type MediaRaisedHand = components["schemas"]["MediaRaisedHand"];
export type MediaReaction = components["schemas"]["MediaReaction"];
export type MediaReactionCluster =
  components["schemas"]["MediaReactionCluster"];
export type MediaSignalViewerOperations =
  components["schemas"]["MediaSignalViewerOperations"];
export type MediaParticipantSnapshot =
  components["schemas"]["MediaParticipantSnapshot"];
export type MediaSignalKind = components["schemas"]["MediaSignalKind"];
export type MediaSignalMutationRequest =
  components["schemas"]["MediaSignalMutationRequest"];
export type MediaProviderEffectStatus =
  components["schemas"]["MediaProviderEffectStatus"];
export type MediaRequiredProviderEffectStatus =
  components["schemas"]["MediaRequiredProviderEffectStatus"];
export type MediaSpaceLockRequest =
  components["schemas"]["MediaSpaceLockRequest"];
export type MediaSpaceLockResult =
  components["schemas"]["MediaSpaceLockResult"];
export type MediaParticipantRoleRequest =
  components["schemas"]["MediaParticipantRoleRequest"];
export type MediaParticipantModerationRequest =
  components["schemas"]["MediaParticipantModerationRequest"];
export type MediaParticipantModerationResult =
  components["schemas"]["MediaParticipantModerationResult"];
export type MediaModerationResult =
  components["schemas"]["MediaModerationResult"];
export type MediaProviderConvergenceProblem =
  components["schemas"]["MediaProviderConvergenceProblem"];
export type MediaInstanceRole = components["schemas"]["MediaInstanceRole"];
export type MediaJoinAttemptRequest =
  components["schemas"]["MediaJoinAttemptRequest"];
export type MediaJoinAttemptStatus =
  components["schemas"]["MediaJoinAttemptStatus"];
export type MediaJoinAttempt = components["schemas"]["MediaJoinAttempt"];
export type MediaJoinAttemptCancelRequest =
  components["schemas"]["MediaJoinAttemptCancelRequest"];
export type MediaAdmissionStatus =
  components["schemas"]["MediaAdmissionStatus"];
export type MediaAdmission = components["schemas"]["MediaAdmission"];
export type MediaAdmissionQueue = components["schemas"]["MediaAdmissionQueue"];
export type MediaAdmissionMutationRequest =
  components["schemas"]["MediaAdmissionMutationRequest"];
export type MediaSpaceMemberStatus =
  components["schemas"]["MediaSpaceMemberStatus"];
export type MediaSpaceMember = components["schemas"]["MediaSpaceMember"];
export type MediaSpaceMemberList =
  components["schemas"]["MediaSpaceMemberList"];
export type MediaSpaceMemberInviteRequest =
  components["schemas"]["MediaSpaceMemberInviteRequest"];
export type MediaSpaceMemberMutationRequest =
  components["schemas"]["MediaSpaceMemberMutationRequest"];
export type MediaInstanceCredentialRequest =
  components["schemas"]["MediaInstanceCredentialRequest"];
export type MediaInstanceCredential =
  components["schemas"]["MediaInstanceCredential"];
export type ClassSessionRecurrenceRule =
  components["schemas"]["ClassSessionRecurrenceRule"];
export type ClassSessionSeries = components["schemas"]["ClassSessionSeries"];
export type CreateClassSessionSeriesRequest =
  components["schemas"]["CreateClassSessionSeriesRequest"];
export type ClassSessionOccurrenceMutationRequest =
  components["schemas"]["ClassSessionOccurrenceMutationRequest"];
export type ClassSessionSeriesMutationResponse =
  components["schemas"]["ClassSessionSeriesMutationResponse"];
export type ClassSessionSeriesScopePreview =
  components["schemas"]["ClassSessionSeriesScopePreview"];
export type ClassEnrollment = components["schemas"]["ClassEnrollment"];
export type CreateClassEnrollmentRequest =
  components["schemas"]["CreateClassEnrollmentRequest"];
export type ClassInviteCodeStatus =
  components["schemas"]["ClassInviteCodeStatus"];
export type ClassInviteCode = components["schemas"]["ClassInviteCode"];
export type ClassInviteCodeListResponse =
  components["schemas"]["ClassInviteCodeListResponse"];
export type CreateClassInviteCodeRequest =
  components["schemas"]["CreateClassInviteCodeRequest"];
export type CreateClassInviteCodeResponse =
  components["schemas"]["CreateClassInviteCodeResponse"];
export type ClassInvitationTokenRequest =
  components["schemas"]["ClassInvitationTokenRequest"];
export type JoinClassInvitationResponse =
  components["schemas"]["JoinClassInvitationResponse"];
export type ClassRosterUser = components["schemas"]["ClassRosterUser"];
export type ClassRosterOwner = components["schemas"]["ClassRosterOwner"];
export type ClassRosterMember = components["schemas"]["ClassRosterMember"];
export type ClassRosterPage = components["schemas"]["ClassRosterPage"];
export type ClassRosterMutationResponse =
  components["schemas"]["ClassRosterMutationResponse"];
export type UpdateClassRosterRoleRequest =
  components["schemas"]["UpdateClassRosterRoleRequest"];
export type ClassRosterBulkAction =
  components["schemas"]["ClassRosterBulkAction"];
export type ClassRosterBulkRequest =
  components["schemas"]["ClassRosterBulkRequest"];
export type ClassRosterBulkResponse =
  components["schemas"]["ClassRosterBulkResponse"];
export interface ListClassesInput {
  cursor?: string;
  limit?: number;
  status?: ClassStatus;
}
export interface ListClassFilesInput {
  cursor?: string;
  limit?: number;
}
export interface ListHomeRecentFilesInput {
  limit?: number;
}
export interface SearchAuthorizedResourcesInput {
  q: string;
  limit?: number;
}
export interface ListClassSessionsInput {
  range_start: string;
  range_end: string;
  cursor?: string;
  limit?: number;
}
export interface ListClassRosterInput {
  cursor?: string;
  limit?: number;
  search?: string;
  status?: ClassEnrollmentStatus;
}
export interface ListAuditEventsInput {
  occurredFrom?: string;
  occurredTo?: string;
  action?: AuditAction;
  resourceType?: string;
  resourceID?: string;
  outcome?: AuditOutcome;
  limit?: number;
  cursor?: string;
}
export interface ListNotificationsInput {
  cursor?: string;
  limit?: number;
  unreadOnly?: boolean;
}
export interface ListConversationsInput {
  cursor?: string;
  kind?: ConversationKind;
  limit?: number;
}
export interface ListConversationMessagesInput {
  cursor?: string;
  limit?: number;
}
export interface ListCalendarItemsInput {
  from: string;
  to: string;
  viewer_timezone: string;
  types?: readonly CalendarSourceType[];
  class_ids?: readonly string[];
  statuses?: readonly CalendarItemStatus[];
  search?: string;
  cursor?: string;
  limit?: number;
}
export interface ListAvailabilityPollsInput {
  limit?: number;
  status?: AvailabilityPollStatus;
}
export interface ListAvailabilityPollIndividualResponsesInput {
  cursor?: string;
  limit?: number;
}
export interface ListStudyMeetingsInput {
  from?: string;
  limit?: number;
  to?: string;
}
export type MediaTokenResponse = components["schemas"]["MediaTokenResponse"];
export type MediaEventRequest = components["schemas"]["MediaEventRequest"];
export type Problem = components["schemas"]["Problem"];

export class APIRequestError extends Error {
  readonly status: number;
  readonly problem?: Problem;
  readonly retryAfterSeconds?: number;

  constructor(status: number, problem?: Problem, retryAfterSeconds?: number) {
    super(
      problem?.detail ?? problem?.title ?? `Core API phản hồi HTTP ${status}.`,
    );
    this.name = "APIRequestError";
    this.status = status;
    this.problem = problem;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

export interface APIRequestOptions {
  baseUrl?: string;
  signal?: AbortSignal;
  fetch?: (request: Request) => Promise<Response>;
}

export type HealthRequestOptions = APIRequestOptions;

export function createTutorHubClient(options: APIRequestOptions = {}) {
  const baseUrl = resolveBaseUrl(options.baseUrl ?? "/api");

  return createClient<paths>({
    baseUrl,
    credentials: "include",
    fetch: createNormalizedFetch(baseUrl, options.fetch),
  });
}

export function getLoginURL(
  returnTo = "/app/home",
  options: Pick<APIRequestOptions, "baseUrl"> = {},
): string {
  const baseUrl = resolveBaseUrl(options.baseUrl ?? "/api");
  const loginURL = normalizeOverlappingPath(
    new URL(`${baseUrl}/api/v1/auth/login`),
    baseUrl,
  );
  loginURL.searchParams.set("return_to", returnTo);
  return loginURL.toString();
}

export async function getHealth(
  options: HealthRequestOptions = {},
): Promise<HealthResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/health",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  if (!response.ok || data === undefined) {
    throw new APIRequestError(
      response.status,
      isProblem(error) ? error : undefined,
      retryAfterSeconds(response),
    );
  }

  return data;
}

export async function getCurrentUser(
  options: APIRequestOptions = {},
): Promise<CurrentUser> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/me",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CurrentUser>(
    data as CurrentUser | undefined,
    error,
    response,
  );
}

export async function getProfile(
  options: APIRequestOptions = {},
): Promise<ProfileResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/me/profile",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ProfileResponse>(
    data as ProfileResponse | undefined,
    error,
    response,
  );
}

export async function updateProfile(
  input: ProfileUpdateRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ProfileResponse> {
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/me/profile",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ProfileResponse>(
    data as ProfileResponse | undefined,
    error,
    response,
  );
}

export async function listIdentities(
  options: APIRequestOptions = {},
): Promise<IdentityListResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/me/identities",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<IdentityListResponse>(
    data as IdentityListResponse | undefined,
    error,
    response,
  );
}

export async function beginIdentityLink(
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<IdentityLinkResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/me/identities/link",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<IdentityLinkResponse>(
    data as IdentityLinkResponse | undefined,
    error,
    response,
  );
}

export async function unlinkIdentity(
  identityID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<void> {
  const { error, response } = await createTutorHubClient(options).DELETE(
    "/api/v1/me/identities/{identity_id}",
    {
      params: {
        path: { identity_id: identityID },
        header: { "X-CSRF-Token": csrfToken },
      },
      signal: options.signal,
    },
  );

  if (!response.ok) {
    throw new APIRequestError(
      response.status,
      isProblem(error) ? error : undefined,
      retryAfterSeconds(response),
    );
  }
}

export async function rotateCSRFToken(
  options: APIRequestOptions = {},
): Promise<CSRFResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/auth/csrf",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CSRFResponse>(
    data as CSRFResponse | undefined,
    error,
    response,
  );
}

export async function logout(
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<LogoutResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/auth/logout",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<LogoutResponse>(
    data as LogoutResponse | undefined,
    error,
    response,
  );
}

export async function createTenant(
  input: CreateTenantRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CurrentUser> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/tenants",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CurrentUser>(
    data as CurrentUser | undefined,
    error,
    response,
  );
}

export async function listTenants(
  options: APIRequestOptions = {},
): Promise<TenantListResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/tenants",
    {
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<TenantListResponse>(
    data as TenantListResponse | undefined,
    error,
    response,
  );
}

export async function getTenant(
  tenantID: string,
  options: APIRequestOptions = {},
): Promise<Tenant> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/tenants/{tenant_id}",
    {
      params: { path: { tenant_id: tenantID } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Tenant>(data as Tenant | undefined, error, response);
}

export async function getTenantCapabilities(
  tenantID: string,
  options: APIRequestOptions = {},
): Promise<TenantCapabilities> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/tenants/{tenant_id}/capabilities",
    {
      params: { path: { tenant_id: tenantID } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<TenantCapabilities>(
    data as TenantCapabilities | undefined,
    error,
    response,
  );
}

export async function updateTenantFeatureControls(
  tenantID: string,
  input: UpdateTenantFeatureControlsRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<TenantCapabilities> {
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/tenants/{tenant_id}/feature-controls",
    {
      params: {
        path: { tenant_id: tenantID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<TenantCapabilities>(
    data as TenantCapabilities | undefined,
    error,
    response,
  );
}

export async function listHomeRecentFiles(
  tenantID: string,
  input: ListHomeRecentFilesInput = {},
  options: APIRequestOptions = {},
): Promise<HomeRecentFilePage> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/home/recent-files",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
        query: input,
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<HomeRecentFilePage>(
    data as HomeRecentFilePage | undefined,
    error,
    response,
  );
}

export async function searchAuthorizedResources(
  tenantID: string,
  input: SearchAuthorizedResourcesInput,
  options: APIRequestOptions = {},
): Promise<AuthorizedSearchPage> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/search",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
        query: input,
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AuthorizedSearchPage>(
    data as AuthorizedSearchPage | undefined,
    error,
    response,
  );
}

export async function createFileUploadIntent(
  tenantID: string,
  input: CreateFileUploadIntentRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ContentFile> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/upload-intents",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ContentFile>(
    data as ContentFile | undefined,
    error,
    response,
  );
}

export async function listClassFiles(
  tenantID: string,
  classID: string,
  input: ListClassFilesInput = {},
  options: APIRequestOptions = {},
): Promise<ContentFilePage> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/files",
    {
      params: {
        path: { class_id: classID },
        query: input,
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ContentFilePage>(
    data as ContentFilePage | undefined,
    error,
    response,
  );
}

export async function getFileMetadata(
  tenantID: string,
  fileID: string,
  options: APIRequestOptions = {},
): Promise<ContentFile> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/files/{file_id}",
    {
      params: {
        path: { file_id: fileID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ContentFile>(
    data as ContentFile | undefined,
    error,
    response,
  );
}

export async function finalizeFileUpload(
  tenantID: string,
  fileID: string,
  input: FinalizeFileUploadRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ContentFile> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/{file_id}/finalize",
    {
      params: {
        path: { file_id: fileID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ContentFile>(
    data as ContentFile | undefined,
    error,
    response,
  );
}

export async function issueFileUploadCapability(
  tenantID: string,
  fileID: string,
  input: IssueFileUploadCapabilityRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<FileUploadCapability> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/{file_id}/upload-capability",
    {
      params: {
        path: { file_id: fileID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<FileUploadCapability>(
    data as FileUploadCapability | undefined,
    error,
    response,
  );
}

export async function issueFileDownloadCapability(
  tenantID: string,
  fileID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<FileDownloadCapability> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/{file_id}/download-capability",
    {
      params: {
        path: { file_id: fileID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<FileDownloadCapability>(
    data as FileDownloadCapability | undefined,
    error,
    response,
  );
}

export async function createFileMultipartUpload(
  tenantID: string,
  fileID: string,
  input: CreateFileMultipartUploadRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<FileMultipartUpload> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/{file_id}/multipart-uploads",
    {
      params: {
        path: { file_id: fileID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<FileMultipartUpload>(
    data as FileMultipartUpload | undefined,
    error,
    response,
  );
}

export async function issueFileMultipartPartCapability(
  tenantID: string,
  fileID: string,
  multipartID: string,
  partNumber: number,
  input: IssueFileMultipartPartCapabilityRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<FileMultipartPartCapability> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/{file_id}/multipart-uploads/{multipart_id}/parts/{part_number}/capability",
    {
      params: {
        path: {
          file_id: fileID,
          multipart_id: multipartID,
          part_number: partNumber,
        },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<FileMultipartPartCapability>(
    data as FileMultipartPartCapability | undefined,
    error,
    response,
  );
}

export async function completeFileMultipartUpload(
  tenantID: string,
  fileID: string,
  multipartID: string,
  input: CompleteFileMultipartUploadRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CompleteFileMultipartUploadResult> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/{file_id}/multipart-uploads/{multipart_id}/complete",
    {
      params: {
        path: { file_id: fileID, multipart_id: multipartID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<CompleteFileMultipartUploadResult>(
    data as CompleteFileMultipartUploadResult | undefined,
    error,
    response,
  );
}

export async function abortFileMultipartUpload(
  tenantID: string,
  fileID: string,
  multipartID: string,
  input: AbortFileMultipartUploadRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<FileMultipartUpload> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/files/{file_id}/multipart-uploads/{multipart_id}/abort",
    {
      params: {
        path: { file_id: fileID, multipart_id: multipartID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<FileMultipartUpload>(
    data as FileMultipartUpload | undefined,
    error,
    response,
  );
}

export async function listConversations(
  tenantID: string,
  input: ListConversationsInput = {},
  options: APIRequestOptions = {},
): Promise<ConversationPage> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/conversations",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
        query: input,
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ConversationPage>(
    data as ConversationPage | undefined,
    error,
    response,
  );
}

export async function getConversation(
  tenantID: string,
  conversationID: string,
  options: APIRequestOptions = {},
): Promise<Conversation> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/conversations/{conversation_id}",
    {
      params: {
        path: { conversation_id: conversationID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Conversation>(
    data as Conversation | undefined,
    error,
    response,
  );
}

export async function createDirectConversation(
  tenantID: string,
  input: CreateDirectConversationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Conversation> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/conversations/direct",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Conversation>(
    data as Conversation | undefined,
    error,
    response,
  );
}

export async function ensureClassConversation(
  tenantID: string,
  classID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Conversation> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/conversation",
    {
      params: {
        path: { class_id: classID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Conversation>(
    data as Conversation | undefined,
    error,
    response,
  );
}

export async function ensureMediaSpaceConversation(
  tenantID: string,
  mediaSpaceID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Conversation> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/conversation",
    {
      params: {
        path: { space_id: mediaSpaceID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Conversation>(
    data as Conversation | undefined,
    error,
    response,
  );
}

export async function listConversationMessages(
  tenantID: string,
  conversationID: string,
  input: ListConversationMessagesInput = {},
  options: APIRequestOptions = {},
): Promise<MessagePage> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/conversations/{conversation_id}/messages",
    {
      params: {
        path: { conversation_id: conversationID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
        query: input,
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MessagePage>(
    data as MessagePage | undefined,
    error,
    response,
  );
}

export async function sendConversationMessage(
  tenantID: string,
  conversationID: string,
  input: SendMessageRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Message> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/conversations/{conversation_id}/messages",
    {
      params: {
        path: { conversation_id: conversationID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Message>(data as Message | undefined, error, response);
}

export async function editConversationMessage(
  tenantID: string,
  conversationID: string,
  messageID: string,
  input: EditMessageRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Message> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/conversations/{conversation_id}/messages/{message_id}",
    {
      params: {
        path: { conversation_id: conversationID, message_id: messageID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Message>(data as Message | undefined, error, response);
}

export async function deleteConversationMessage(
  tenantID: string,
  conversationID: string,
  messageID: string,
  input: DeleteMessageRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Message> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).DELETE(
    "/api/v1/conversations/{conversation_id}/messages/{message_id}",
    {
      params: {
        path: { conversation_id: conversationID, message_id: messageID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Message>(data as Message | undefined, error, response);
}

export async function markConversationRead(
  tenantID: string,
  conversationID: string,
  input: MarkConversationReadRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MessageReadState> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/conversations/{conversation_id}/read",
    {
      params: {
        path: { conversation_id: conversationID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MessageReadState>(
    data as MessageReadState | undefined,
    error,
    response,
  );
}

export async function listNotifications(
  tenantID: string,
  input: ListNotificationsInput = {},
  options: APIRequestOptions = {},
): Promise<NotificationPage> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/notifications",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
        query: {
          cursor: input.cursor,
          limit: input.limit,
          unread_only: input.unreadOnly,
        },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<NotificationPage>(
    data as NotificationPage | undefined,
    error,
    response,
  );
}

export async function getNotificationUnreadCount(
  tenantID: string,
  options: APIRequestOptions = {},
): Promise<NotificationUnreadCount> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/notifications/unread-count",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<NotificationUnreadCount>(
    data as NotificationUnreadCount | undefined,
    error,
    response,
  );
}

export async function markNotificationRead(
  tenantID: string,
  notificationID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Notification> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/notifications/{notification_id}/read",
    {
      params: {
        path: { notification_id: notificationID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Notification>(
    data as Notification | undefined,
    error,
    response,
  );
}

export async function markAllNotificationsRead(
  tenantID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MarkAllNotificationsReadResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/notifications/read-all",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MarkAllNotificationsReadResponse>(
    data as MarkAllNotificationsReadResponse | undefined,
    error,
    response,
  );
}

export async function getNotificationPreference(
  tenantID: string,
  options: APIRequestOptions = {},
): Promise<NotificationPreference> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/notification-preferences",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<NotificationPreference>(
    data as NotificationPreference | undefined,
    error,
    response,
  );
}

export async function updateNotificationPreference(
  tenantID: string,
  input: UpdateNotificationPreferenceRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<NotificationPreference> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/notification-preferences",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<NotificationPreference>(
    data as NotificationPreference | undefined,
    error,
    response,
  );
}

export async function listCalendarItems(
  tenantID: string,
  input: ListCalendarItemsInput,
  options: APIRequestOptions = {},
): Promise<CalendarItemListResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/items",
    {
      params: {
        query: input,
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CalendarItemListResponse>(
    data as CalendarItemListResponse | undefined,
    error,
    response,
  );
}

export async function getCalendarDisplayPreference(
  tenantID: string,
  options: APIRequestOptions = {},
): Promise<CalendarDisplayPreference> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/preferences/display",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CalendarDisplayPreference>(
    data as CalendarDisplayPreference | undefined,
    error,
    response,
  );
}

export async function updateCalendarDisplayPreference(
  tenantID: string,
  input: UpdateCalendarDisplayPreferenceRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CalendarDisplayPreference> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/calendar/preferences/display",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CalendarDisplayPreference>(
    data as CalendarDisplayPreference | undefined,
    error,
    response,
  );
}

export async function getCalendarWorkingSchedule(
  tenantID: string,
  options: APIRequestOptions = {},
): Promise<CalendarWorkingSchedule> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/working-schedule",
    {
      params: {
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CalendarWorkingSchedule>(
    data as CalendarWorkingSchedule | undefined,
    error,
    response,
  );
}

export async function updateCalendarWorkingSchedule(
  tenantID: string,
  input: UpdateCalendarWorkingScheduleRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CalendarWorkingSchedule> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/calendar/working-schedule",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CalendarWorkingSchedule>(
    data as CalendarWorkingSchedule | undefined,
    error,
    response,
  );
}

export async function queryCalendarAvailability(
  tenantID: string,
  input: CalendarAvailabilityQueryRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CalendarAvailabilityQueryResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability/query",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CalendarAvailabilityQueryResponse>(
    data as CalendarAvailabilityQueryResponse | undefined,
    error,
    response,
  );
}

export async function updateTenant(
  tenantID: string,
  input: UpdateTenantRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<Tenant> {
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/tenants/{tenant_id}",
    {
      params: {
        path: { tenant_id: tenantID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<Tenant>(data as Tenant | undefined, error, response);
}

export async function archiveTenant(
  tenantID: string,
  input: ArchiveTenantRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CurrentUser> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/tenants/{tenant_id}/archive",
    {
      params: {
        path: { tenant_id: tenantID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CurrentUser>(
    data as CurrentUser | undefined,
    error,
    response,
  );
}

export async function listAuditEvents(
  tenantID: string,
  input: ListAuditEventsInput = {},
  options: APIRequestOptions = {},
): Promise<AuditEventPage> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/tenants/{tenant_id}/audit-events",
    {
      params: {
        path: { tenant_id: tenantID },
        query: {
          occurred_from: input.occurredFrom,
          occurred_to: input.occurredTo,
          action: input.action,
          resource_type: input.resourceType,
          resource_id: input.resourceID,
          outcome: input.outcome,
          limit: input.limit,
          cursor: input.cursor,
        },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AuditEventPage>(
    data as AuditEventPage | undefined,
    error,
    response,
  );
}

export async function listMembershipInvitations(
  tenantID: string,
  options: APIRequestOptions = {},
): Promise<MembershipInvitationListResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/tenants/{tenant_id}/invitations",
    {
      params: { path: { tenant_id: tenantID } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MembershipInvitationListResponse>(
    data as MembershipInvitationListResponse | undefined,
    error,
    response,
  );
}

export async function createMembershipInvitation(
  tenantID: string,
  input: CreateMembershipInvitationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CreateMembershipInvitationResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/tenants/{tenant_id}/invitations",
    {
      params: {
        path: { tenant_id: tenantID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CreateMembershipInvitationResponse>(
    data as CreateMembershipInvitationResponse | undefined,
    error,
    response,
  );
}

export async function revokeMembershipInvitation(
  tenantID: string,
  invitationID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MembershipInvitation> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/tenants/{tenant_id}/invitations/{invitation_id}/revoke",
    {
      params: {
        path: { tenant_id: tenantID, invitation_id: invitationID },
        header: { "X-CSRF-Token": csrfToken },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MembershipInvitation>(
    data as MembershipInvitation | undefined,
    error,
    response,
  );
}

export async function previewMembershipInvitation(
  input: MembershipInvitationTokenRequest,
  options: APIRequestOptions = {},
): Promise<MembershipInvitationPreview> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/membership-invitations/preview",
    {
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MembershipInvitationPreview>(
    data as MembershipInvitationPreview | undefined,
    error,
    response,
  );
}

export async function acceptMembershipInvitation(
  input: MembershipInvitationTokenRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MembershipInvitationAcceptResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/membership-invitations/accept",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MembershipInvitationAcceptResponse>(
    data as MembershipInvitationAcceptResponse | undefined,
    error,
    response,
  );
}

export async function switchActiveTenant(
  tenantID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CurrentUser> {
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/session/active-tenant",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: { tenant_id: tenantID },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CurrentUser>(
    data as CurrentUser | undefined,
    error,
    response,
  );
}

export async function listClasses(
  input: ListClassesInput = {},
  options: APIRequestOptions = {},
): Promise<ClassListResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes",
    {
      params: { query: input },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassListResponse>(
    data as ClassListResponse | undefined,
    error,
    response,
  );
}

export async function getClass(
  classID: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}",
    {
      params: { path: { class_id: classID } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function createClass(
  input: CreateClassRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function updateClass(
  classID: string,
  input: UpdateClassRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/classes/{class_id}",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function archiveClass(
  classID: string,
  input: ClassVersionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/archive",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function restoreClass(
  classID: string,
  input: ClassVersionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/restore",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function transferClassOwnership(
  classID: string,
  input: TransferClassOwnershipRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassroomClass> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/transfer-ownership",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassroomClass>(
    data as ClassroomClass | undefined,
    error,
    response,
  );
}

export async function listClassSessions(
  classID: string,
  input: ListClassSessionsInput,
  options: APIRequestOptions = {},
): Promise<ClassSessionListResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/sessions",
    {
      params: {
        path: { class_id: classID },
        query: input,
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassSessionListResponse>(
    data as ClassSessionListResponse | undefined,
    error,
    response,
  );
}

export async function getClassSession(
  classID: string,
  sessionID: string,
  options: APIRequestOptions = {},
): Promise<ClassSession> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/sessions/{session_id}",
    {
      params: {
        path: { class_id: classID, session_id: sessionID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassSession>(
    data as ClassSession | undefined,
    error,
    response,
  );
}

export async function getClassSessionAudience(
  tenantID: string,
  classID: string,
  sessionID: string,
  options: APIRequestOptions = {},
): Promise<SessionAudience> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/sessions/{session_id}/attendees",
    {
      params: {
        path: { class_id: classID, session_id: sessionID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<SessionAudience>(
    data as SessionAudience | undefined,
    error,
    response,
  );
}

export async function replaceClassSessionAudience(
  tenantID: string,
  classID: string,
  sessionID: string,
  input: ReplaceSessionAudienceRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ReplaceSessionAudienceResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/classes/{class_id}/sessions/{session_id}/attendees",
    {
      params: {
        path: { class_id: classID, session_id: sessionID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ReplaceSessionAudienceResponse>(
    data as ReplaceSessionAudienceResponse | undefined,
    error,
    response,
  );
}

export async function transferClassSessionOrganizer(
  tenantID: string,
  classID: string,
  sessionID: string,
  input: TransferSessionOrganizerRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<TransferSessionOrganizerResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/sessions/{session_id}/organizer",
    {
      params: {
        path: { class_id: classID, session_id: sessionID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<TransferSessionOrganizerResponse>(
    data as TransferSessionOrganizerResponse | undefined,
    error,
    response,
  );
}

export async function respondToClassSession(
  tenantID: string,
  classID: string,
  sessionID: string,
  input: RespondToClassSessionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<SelfRSVPResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/sessions/{session_id}/responses",
    {
      params: {
        path: { class_id: classID, session_id: sessionID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<SelfRSVPResponse>(
    data as SelfRSVPResponse | undefined,
    error,
    response,
  );
}

export async function getClassSessionSeriesAudience(
  tenantID: string,
  classID: string,
  seriesID: string,
  options: APIRequestOptions = {},
): Promise<SessionAudience> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/session-series/{series_id}/attendees",
    {
      params: {
        path: { class_id: classID, series_id: seriesID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<SessionAudience>(
    data as SessionAudience | undefined,
    error,
    response,
  );
}

export async function replaceClassSessionSeriesAudience(
  tenantID: string,
  classID: string,
  seriesID: string,
  input: ReplaceSessionAudienceRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ReplaceSessionAudienceResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/classes/{class_id}/session-series/{series_id}/attendees",
    {
      params: {
        path: { class_id: classID, series_id: seriesID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ReplaceSessionAudienceResponse>(
    data as ReplaceSessionAudienceResponse | undefined,
    error,
    response,
  );
}

export async function transferClassSessionSeriesOrganizer(
  tenantID: string,
  classID: string,
  seriesID: string,
  input: TransferSessionOrganizerRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<TransferSessionOrganizerResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/session-series/{series_id}/organizer",
    {
      params: {
        path: { class_id: classID, series_id: seriesID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<TransferSessionOrganizerResponse>(
    data as TransferSessionOrganizerResponse | undefined,
    error,
    response,
  );
}

export async function respondToClassSessionSeries(
  tenantID: string,
  classID: string,
  seriesID: string,
  input: RespondToClassSessionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<SelfRSVPResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/session-series/{series_id}/responses",
    {
      params: {
        path: { class_id: classID, series_id: seriesID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<SelfRSVPResponse>(
    data as SelfRSVPResponse | undefined,
    error,
    response,
  );
}

export async function getClassSessionSeriesOccurrenceAudience(
  tenantID: string,
  classID: string,
  seriesID: string,
  occurrenceKey: string,
  options: APIRequestOptions = {},
): Promise<SessionAudience> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/session-series/{series_id}/occurrences/{occurrence_key}/attendees",
    {
      params: {
        path: {
          class_id: classID,
          series_id: seriesID,
          occurrence_key: occurrenceKey,
        },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<SessionAudience>(
    data as SessionAudience | undefined,
    error,
    response,
  );
}

export async function replaceClassSessionSeriesOccurrenceAudience(
  tenantID: string,
  classID: string,
  seriesID: string,
  occurrenceKey: string,
  input: ReplaceSessionAudienceRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ReplaceSessionAudienceResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/classes/{class_id}/session-series/{series_id}/occurrences/{occurrence_key}/attendees",
    {
      params: {
        path: {
          class_id: classID,
          series_id: seriesID,
          occurrence_key: occurrenceKey,
        },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ReplaceSessionAudienceResponse>(
    data as ReplaceSessionAudienceResponse | undefined,
    error,
    response,
  );
}

export async function respondToClassSessionSeriesOccurrence(
  tenantID: string,
  classID: string,
  seriesID: string,
  occurrenceKey: string,
  input: RespondToClassSessionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<SelfRSVPResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/session-series/{series_id}/occurrences/{occurrence_key}/responses",
    {
      params: {
        path: {
          class_id: classID,
          series_id: seriesID,
          occurrence_key: occurrenceKey,
        },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<SelfRSVPResponse>(
    data as SelfRSVPResponse | undefined,
    error,
    response,
  );
}

export async function resolveExternalCalendarRSVP(
  input: ResolveExternalCalendarRSVPRequest,
  options: APIRequestOptions = {},
): Promise<ExternalCalendarRSVPProjection> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/invitations/resolve",
    {
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ExternalCalendarRSVPProjection>(
    data as ExternalCalendarRSVPProjection | undefined,
    error,
    response,
  );
}

export async function respondExternalCalendarRSVP(
  input: RespondExternalCalendarRSVPRequest,
  options: APIRequestOptions = {},
): Promise<ExternalCalendarRSVPMutationResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/invitations/respond",
    {
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ExternalCalendarRSVPMutationResponse>(
    data as ExternalCalendarRSVPMutationResponse | undefined,
    error,
    response,
  );
}

export async function listAvailabilityPolls(
  tenantID: string,
  input: ListAvailabilityPollsInput = {},
  options: APIRequestOptions = {},
): Promise<AvailabilityPollListResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/availability-polls",
    {
      params: {
        query: input,
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPollListResponse>(
    data as AvailabilityPollListResponse | undefined,
    error,
    response,
  );
}

export async function getAvailabilityPoll(
  tenantID: string,
  pollID: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPoll> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/availability-polls/{poll_id}",
    {
      params: {
        path: { poll_id: pollID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPoll>(
    data as AvailabilityPoll | undefined,
    error,
    response,
  );
}

export async function createAvailabilityPoll(
  tenantID: string,
  input: CreateAvailabilityPollRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPoll> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPoll>(
    data as AvailabilityPoll | undefined,
    error,
    response,
  );
}

export async function updateAvailabilityPoll(
  tenantID: string,
  pollID: string,
  input: UpdateAvailabilityPollRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPoll> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/calendar/availability-polls/{poll_id}",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPoll>(
    data as AvailabilityPoll | undefined,
    error,
    response,
  );
}

export async function openAvailabilityPoll(
  tenantID: string,
  pollID: string,
  input: AvailabilityPollVersionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPoll> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/{poll_id}/open",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPoll>(
    data as AvailabilityPoll | undefined,
    error,
    response,
  );
}

export async function closeAvailabilityPoll(
  tenantID: string,
  pollID: string,
  input: AvailabilityPollVersionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPoll> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/{poll_id}/close",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPoll>(
    data as AvailabilityPoll | undefined,
    error,
    response,
  );
}

export async function reopenAvailabilityPoll(
  tenantID: string,
  pollID: string,
  input: ReopenAvailabilityPollRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPoll> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/{poll_id}/reopen",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPoll>(
    data as AvailabilityPoll | undefined,
    error,
    response,
  );
}

export async function cancelAvailabilityPoll(
  tenantID: string,
  pollID: string,
  input: CancelAvailabilityPollRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPoll> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/{poll_id}/cancel",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPoll>(
    data as AvailabilityPoll | undefined,
    error,
    response,
  );
}

export async function respondToAvailabilityPoll(
  tenantID: string,
  pollID: string,
  input: RespondAvailabilityPollRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPollMutationResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PUT(
    "/api/v1/calendar/availability-polls/{poll_id}/responses/me",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPollMutationResponse>(
    data as AvailabilityPollMutationResponse | undefined,
    error,
    response,
  );
}

export async function listAvailabilityPollIndividualResponses(
  tenantID: string,
  pollID: string,
  input: ListAvailabilityPollIndividualResponsesInput = {},
  options: APIRequestOptions = {},
): Promise<AvailabilityPollIndividualResponsePage> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/availability-polls/{poll_id}/responses",
    {
      params: {
        path: { poll_id: pollID },
        query: input,
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPollIndividualResponsePage>(
    data as AvailabilityPollIndividualResponsePage | undefined,
    error,
    response,
  );
}

export async function getAvailabilityPollSummary(
  tenantID: string,
  pollID: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPollSummary> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/availability-polls/{poll_id}/summary",
    {
      params: {
        path: { poll_id: pollID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPollSummary>(
    data as AvailabilityPollSummary | undefined,
    error,
    response,
  );
}

export async function createAvailabilityPollCapability(
  tenantID: string,
  pollID: string,
  input: CreateAvailabilityPollCapabilityRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPollCapabilitySecret> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/{poll_id}/capabilities",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPollCapabilitySecret>(
    data as AvailabilityPollCapabilitySecret | undefined,
    error,
    response,
  );
}

export async function revokeAvailabilityPollCapability(
  tenantID: string,
  pollID: string,
  capabilityID: string,
  input: RevokeAvailabilityPollCapabilityRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPollCapability> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/{poll_id}/capabilities/{capability_id}/revoke",
    {
      params: {
        path: { poll_id: pollID, capability_id: capabilityID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPollCapability>(
    data as AvailabilityPollCapability | undefined,
    error,
    response,
  );
}

export async function finalizeAvailabilityPoll(
  tenantID: string,
  pollID: string,
  input: FinalizeAvailabilityPollRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<AvailabilityPollMutationResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/{poll_id}/finalize",
    {
      params: {
        path: { poll_id: pollID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<AvailabilityPollMutationResponse>(
    data as AvailabilityPollMutationResponse | undefined,
    error,
    response,
  );
}

export async function listStudyMeetings(
  tenantID: string,
  input: ListStudyMeetingsInput = {},
  options: APIRequestOptions = {},
): Promise<StudyMeetingListResponse> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/study-meetings",
    {
      params: {
        query: input,
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<StudyMeetingListResponse>(
    data as StudyMeetingListResponse | undefined,
    error,
    response,
  );
}

export async function getStudyMeeting(
  tenantID: string,
  meetingID: string,
  options: APIRequestOptions = {},
): Promise<StudyMeeting> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/calendar/study-meetings/{meeting_id}",
    {
      params: {
        path: { meeting_id: meetingID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<StudyMeeting>(
    data as StudyMeeting | undefined,
    error,
    response,
  );
}

export async function createStudyMeeting(
  tenantID: string,
  input: CreateStudyMeetingRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<StudyMeeting> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/study-meetings",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<StudyMeeting>(
    data as StudyMeeting | undefined,
    error,
    response,
  );
}

export async function updateStudyMeeting(
  tenantID: string,
  meetingID: string,
  input: UpdateStudyMeetingRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<StudyMeeting> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/calendar/study-meetings/{meeting_id}",
    {
      params: {
        path: { meeting_id: meetingID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<StudyMeeting>(
    data as StudyMeeting | undefined,
    error,
    response,
  );
}

export async function cancelStudyMeeting(
  tenantID: string,
  meetingID: string,
  input: CancelStudyMeetingRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<StudyMeeting> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/study-meetings/{meeting_id}/cancel",
    {
      params: {
        path: { meeting_id: meetingID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<StudyMeeting>(
    data as StudyMeeting | undefined,
    error,
    response,
  );
}

export async function createMediaSpace(
  tenantID: string,
  input: CreateMediaSpaceRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpace> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces",
    {
      params: {
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaSpace>(
    data as MediaSpace | undefined,
    error,
    response,
  );
}

export async function getMediaSpace(
  tenantID: string,
  spaceID: string,
  options: APIRequestOptions = {},
): Promise<MediaSpace> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/media/spaces/{space_id}",
    {
      params: {
        path: { space_id: spaceID },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaSpace>(
    data as MediaSpace | undefined,
    error,
    response,
  );
}

export async function listMediaSpaceParticipants(
  tenantID: string,
  spaceID: string,
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
  options: APIRequestOptions = {},
): Promise<MediaParticipantSnapshot> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/media/spaces/{space_id}/participants",
    {
      params: {
        path: { space_id: spaceID },
        query: {
          room_instance_id: roomInstanceID,
          expected_space_version: expectedSpaceVersion,
          expected_room_instance_version: expectedRoomInstanceVersion,
        },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaParticipantSnapshot>(
    data as MediaParticipantSnapshot | undefined,
    error,
    response,
  );
}

export async function mutateMediaSpaceSignal(
  tenantID: string,
  spaceID: string,
  input: MediaSignalMutationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaParticipantSnapshot> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/signals",
    {
      params: {
        path: { space_id: spaceID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaParticipantSnapshot>(
    data as MediaParticipantSnapshot | undefined,
    error,
    response,
  );
}

export async function setMediaSpaceLock(
  tenantID: string,
  spaceID: string,
  input: MediaSpaceLockRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpaceLockResult> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/lock",
    {
      params: {
        path: { space_id: spaceID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaSpaceLockResult>(
    data as MediaSpaceLockResult | undefined,
    error,
    response,
  );
}

export async function changeMediaParticipantRole(
  tenantID: string,
  spaceID: string,
  participantKey: string,
  input: MediaParticipantRoleRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaParticipantModerationResult> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/participants/{participant_key}/role",
    {
      params: {
        path: { space_id: spaceID, participant_key: participantKey },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaParticipantModerationResult>(
    data as MediaParticipantModerationResult | undefined,
    error,
    response,
  );
}

export async function muteMediaParticipantMicrophone(
  tenantID: string,
  spaceID: string,
  participantKey: string,
  input: MediaParticipantModerationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaParticipantModerationResult> {
  return moderateMediaParticipant(
    "mute",
    tenantID,
    spaceID,
    participantKey,
    input,
    csrfToken,
    options,
  );
}

export async function removeMediaParticipant(
  tenantID: string,
  spaceID: string,
  participantKey: string,
  input: MediaParticipantModerationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaParticipantModerationResult> {
  return moderateMediaParticipant(
    "remove",
    tenantID,
    spaceID,
    participantKey,
    input,
    csrfToken,
    options,
  );
}

async function moderateMediaParticipant(
  action: "mute" | "remove",
  tenantID: string,
  spaceID: string,
  participantKey: string,
  input: MediaParticipantModerationRequest,
  csrfToken: string,
  options: APIRequestOptions,
): Promise<MediaParticipantModerationResult> {
  requireTenantScope(tenantID);
  const client = createTutorHubClient(options);
  const request = {
    params: {
      path: { space_id: spaceID, participant_key: participantKey },
      header: {
        "X-CSRF-Token": csrfToken,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
    },
    body: input,
    headers: { Accept: "application/json" },
    signal: options.signal,
  } as const;
  const result =
    action === "mute"
      ? await client.POST(
          "/api/v1/media/spaces/{space_id}/participants/{participant_key}/mute",
          request,
        )
      : await client.POST(
          "/api/v1/media/spaces/{space_id}/participants/{participant_key}/remove",
          request,
        );
  return requireData<MediaParticipantModerationResult>(
    result.data as MediaParticipantModerationResult | undefined,
    result.error,
    result.response,
  );
}

export async function issueMediaSpaceJoinCredential(
  tenantID: string,
  spaceID: string,
  input: MediaInstanceCredentialRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaInstanceCredential> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/join-credentials",
    {
      params: {
        path: { space_id: spaceID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaInstanceCredential>(
    data as MediaInstanceCredential | undefined,
    error,
    response,
  );
}

export async function createMediaSpaceJoinAttempt(
  tenantID: string,
  spaceID: string,
  input: MediaJoinAttemptRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaJoinAttempt> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/join-attempts",
    {
      params: {
        path: { space_id: spaceID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaJoinAttempt>(
    data as MediaJoinAttempt | undefined,
    error,
    response,
  );
}

export async function getMediaJoinAttempt(
  tenantID: string,
  spaceID: string,
  joinAttemptID: string,
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
  options: APIRequestOptions = {},
): Promise<MediaJoinAttempt> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/media/spaces/{space_id}/join-attempts/{join_attempt_id}",
    {
      params: {
        path: {
          space_id: spaceID,
          join_attempt_id: joinAttemptID,
        },
        query: {
          room_instance_id: roomInstanceID,
          expected_space_version: expectedSpaceVersion,
          expected_room_instance_version: expectedRoomInstanceVersion,
        },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaJoinAttempt>(
    data as MediaJoinAttempt | undefined,
    error,
    response,
  );
}

export async function cancelMediaJoinAttempt(
  tenantID: string,
  spaceID: string,
  joinAttemptID: string,
  input: MediaJoinAttemptCancelRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaJoinAttempt> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/join-attempts/{join_attempt_id}/cancel",
    {
      params: {
        path: {
          space_id: spaceID,
          join_attempt_id: joinAttemptID,
        },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaJoinAttempt>(
    data as MediaJoinAttempt | undefined,
    error,
    response,
  );
}

export async function listMediaAdmissions(
  tenantID: string,
  spaceID: string,
  roomInstanceID: string,
  expectedSpaceVersion: number,
  expectedRoomInstanceVersion: number,
  options: APIRequestOptions = {},
): Promise<MediaAdmissionQueue> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/media/spaces/{space_id}/admissions",
    {
      params: {
        path: { space_id: spaceID },
        query: {
          room_instance_id: roomInstanceID,
          expected_space_version: expectedSpaceVersion,
          expected_room_instance_version: expectedRoomInstanceVersion,
          limit: 50,
        },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaAdmissionQueue>(
    data as MediaAdmissionQueue | undefined,
    error,
    response,
  );
}

export async function resolveMediaAdmission(
  action: "admit" | "deny" | "restore",
  tenantID: string,
  spaceID: string,
  admissionID: string,
  input: MediaAdmissionMutationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaAdmission> {
  requireTenantScope(tenantID);
  const client = createTutorHubClient(options);
  const request = {
    params: {
      path: { space_id: spaceID, admission_id: admissionID },
      header: {
        "X-CSRF-Token": csrfToken,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
    },
    body: input,
    headers: { Accept: "application/json" },
    signal: options.signal,
  } as const;
  const result =
    action === "admit"
      ? await client.POST(
          "/api/v1/media/spaces/{space_id}/admissions/{admission_id}/admit",
          request,
        )
      : action === "deny"
        ? await client.POST(
            "/api/v1/media/spaces/{space_id}/admissions/{admission_id}/deny",
            request,
          )
        : await client.POST(
            "/api/v1/media/spaces/{space_id}/admissions/{admission_id}/restore",
            request,
          );
  return requireData<MediaAdmission>(
    result.data as MediaAdmission | undefined,
    result.error,
    result.response,
  );
}

export async function listMediaSpaceMembers(
  tenantID: string,
  spaceID: string,
  expectedSpaceVersion: number,
  options: APIRequestOptions = {},
): Promise<MediaSpaceMemberList> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/media/spaces/{space_id}/members",
    {
      params: {
        path: { space_id: spaceID },
        query: { expected_space_version: expectedSpaceVersion, limit: 50 },
        header: { "X-TutorHub-Expected-Tenant-ID": tenantID },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaSpaceMemberList>(
    data as MediaSpaceMemberList | undefined,
    error,
    response,
  );
}

export async function inviteMediaSpaceMember(
  tenantID: string,
  spaceID: string,
  input: MediaSpaceMemberInviteRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpaceMember> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/members",
    {
      params: {
        path: { space_id: spaceID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaSpaceMember>(
    data as MediaSpaceMember | undefined,
    error,
    response,
  );
}

export async function mutateMediaSpaceMember(
  action: "revoke" | "restore",
  tenantID: string,
  spaceID: string,
  userID: string,
  input: MediaSpaceMemberMutationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpaceMember> {
  requireTenantScope(tenantID);
  const client = createTutorHubClient(options);
  const request = {
    params: {
      path: { space_id: spaceID, user_id: userID },
      header: {
        "X-CSRF-Token": csrfToken,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
    },
    body: input,
    headers: { Accept: "application/json" },
    signal: options.signal,
  } as const;
  const result =
    action === "revoke"
      ? await client.POST(
          "/api/v1/media/spaces/{space_id}/members/{user_id}/revoke",
          request,
        )
      : await client.POST(
          "/api/v1/media/spaces/{space_id}/members/{user_id}/restore",
          request,
        );
  return requireData<MediaSpaceMember>(
    result.data as MediaSpaceMember | undefined,
    result.error,
    result.response,
  );
}

export async function startMediaSpace(
  tenantID: string,
  spaceID: string,
  input: MediaSpaceTransitionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpace> {
  return transitionMediaSpace(
    "start",
    tenantID,
    spaceID,
    input,
    csrfToken,
    options,
  );
}

export async function endMediaSpace(
  tenantID: string,
  spaceID: string,
  input: MediaSpaceTransitionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpace> {
  return transitionMediaSpace(
    "end",
    tenantID,
    spaceID,
    input,
    csrfToken,
    options,
  );
}

export async function cancelMediaSpace(
  tenantID: string,
  spaceID: string,
  input: MediaSpaceTransitionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpace> {
  return transitionMediaSpace(
    "cancel",
    tenantID,
    spaceID,
    input,
    csrfToken,
    options,
  );
}

export async function recoverMediaSpace(
  tenantID: string,
  spaceID: string,
  input: RecoverMediaSpaceRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaSpace> {
  requireTenantScope(tenantID);
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/media/spaces/{space_id}/recover",
    {
      params: {
        path: { space_id: spaceID },
        header: {
          "X-CSRF-Token": csrfToken,
          "X-TutorHub-Expected-Tenant-ID": tenantID,
        },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<MediaSpace>(
    data as MediaSpace | undefined,
    error,
    response,
  );
}

async function transitionMediaSpace(
  transition: "start" | "end" | "cancel",
  tenantID: string,
  spaceID: string,
  input: MediaSpaceTransitionRequest,
  csrfToken: string,
  options: APIRequestOptions,
): Promise<MediaSpace> {
  requireTenantScope(tenantID);
  const client = createTutorHubClient(options);
  const request = {
    params: {
      path: { space_id: spaceID },
      header: {
        "X-CSRF-Token": csrfToken,
        "X-TutorHub-Expected-Tenant-ID": tenantID,
      },
    },
    body: input,
    headers: { Accept: "application/json" },
    signal: options.signal,
  } as const;
  const result =
    transition === "start"
      ? await client.POST("/api/v1/media/spaces/{space_id}/start", request)
      : transition === "end"
        ? await client.POST("/api/v1/media/spaces/{space_id}/end", request)
        : await client.POST("/api/v1/media/spaces/{space_id}/cancel", request);
  return requireData<MediaSpace>(
    result.data as MediaSpace | undefined,
    result.error,
    result.response,
  );
}

export async function resolvePublicAvailabilityPoll(
  input: ResolvePublicAvailabilityPollRequest,
  options: APIRequestOptions = {},
): Promise<PublicAvailabilityPollExchange> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/resolve",
    {
      params: {
        header: {
          Origin: resolvePublicRequestOrigin(options.baseUrl ?? "/api"),
        },
      },
      body: input,
      cache: "no-store",
      credentials: "omit",
      headers: { Accept: "application/json" },
      referrerPolicy: "no-referrer",
      signal: options.signal,
    },
  );

  return requireData<PublicAvailabilityPollExchange>(
    data as PublicAvailabilityPollExchange | undefined,
    error,
    response,
  );
}

export async function respondPublicAvailabilityPoll(
  input: RespondPublicAvailabilityPollRequest,
  options: APIRequestOptions = {},
): Promise<PublicAvailabilityPollMutationResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/calendar/availability-polls/respond",
    {
      params: {
        header: {
          Origin: resolvePublicRequestOrigin(options.baseUrl ?? "/api"),
        },
      },
      body: input,
      cache: "no-store",
      credentials: "omit",
      headers: { Accept: "application/json" },
      referrerPolicy: "no-referrer",
      signal: options.signal,
    },
  );

  return requireData<PublicAvailabilityPollMutationResponse>(
    data as PublicAvailabilityPollMutationResponse | undefined,
    error,
    response,
  );
}

export async function createClassSession(
  classID: string,
  input: CreateClassSessionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassSession> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/sessions",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassSession>(
    data as ClassSession | undefined,
    error,
    response,
  );
}

export async function updateClassSession(
  classID: string,
  sessionID: string,
  input: UpdateClassSessionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassSession> {
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/classes/{class_id}/sessions/{session_id}",
    {
      params: {
        path: { class_id: classID, session_id: sessionID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassSession>(
    data as ClassSession | undefined,
    error,
    response,
  );
}

export async function cancelClassSession(
  classID: string,
  sessionID: string,
  input: CancelClassSessionRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassSession> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/sessions/{session_id}/cancel",
    {
      params: {
        path: { class_id: classID, session_id: sessionID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassSession>(
    data as ClassSession | undefined,
    error,
    response,
  );
}

export async function createClassSessionSeries(
  classID: string,
  input: CreateClassSessionSeriesRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassSessionSeries> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/session-series",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<ClassSessionSeries>(
    data as ClassSessionSeries | undefined,
    error,
    response,
  );
}

export async function getClassSessionSeries(
  classID: string,
  seriesID: string,
  options: APIRequestOptions = {},
): Promise<ClassSessionSeries> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/session-series/{series_id}",
    {
      params: { path: { class_id: classID, series_id: seriesID } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<ClassSessionSeries>(
    data as ClassSessionSeries | undefined,
    error,
    response,
  );
}

export async function previewClassSessionSeriesMutation(
  classID: string,
  seriesID: string,
  input: ClassSessionOccurrenceMutationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassSessionSeriesScopePreview> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/session-series/{series_id}/preview",
    {
      params: {
        path: { class_id: classID, series_id: seriesID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<ClassSessionSeriesScopePreview>(
    data as ClassSessionSeriesScopePreview | undefined,
    error,
    response,
  );
}

export async function updateClassSessionSeriesOccurrence(
  classID: string,
  seriesID: string,
  input: ClassSessionOccurrenceMutationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassSessionSeriesMutationResponse> {
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/classes/{class_id}/session-series/{series_id}/occurrence",
    {
      params: {
        path: { class_id: classID, series_id: seriesID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<ClassSessionSeriesMutationResponse>(
    data as ClassSessionSeriesMutationResponse | undefined,
    error,
    response,
  );
}

export async function cancelClassSessionSeriesOccurrence(
  classID: string,
  seriesID: string,
  input: ClassSessionOccurrenceMutationRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassSessionSeriesMutationResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/session-series/{series_id}/occurrence/cancel",
    {
      params: {
        path: { class_id: classID, series_id: seriesID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );
  return requireData<ClassSessionSeriesMutationResponse>(
    data as ClassSessionSeriesMutationResponse | undefined,
    error,
    response,
  );
}

export async function createClassEnrollment(
  classID: string,
  input: CreateClassEnrollmentRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassEnrollment> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/enrollments",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassEnrollment>(
    data as ClassEnrollment | undefined,
    error,
    response,
  );
}

export async function suspendClassEnrollment(
  classID: string,
  userID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassEnrollment> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/enrollments/{user_id}/suspend",
    {
      params: {
        path: { class_id: classID, user_id: userID },
        header: { "X-CSRF-Token": csrfToken },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassEnrollment>(
    data as ClassEnrollment | undefined,
    error,
    response,
  );
}

export async function removeClassEnrollment(
  classID: string,
  userID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassEnrollment> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/enrollments/{user_id}/remove",
    {
      params: {
        path: { class_id: classID, user_id: userID },
        header: { "X-CSRF-Token": csrfToken },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassEnrollment>(
    data as ClassEnrollment | undefined,
    error,
    response,
  );
}

export async function listClassRoster(
  classID: string,
  input: ListClassRosterInput = {},
  options: APIRequestOptions = {},
): Promise<ClassRosterPage> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/roster",
    {
      params: { path: { class_id: classID }, query: input },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassRosterPage>(
    data as ClassRosterPage | undefined,
    error,
    response,
  );
}

export async function updateClassRosterRole(
  classID: string,
  userID: string,
  input: UpdateClassRosterRoleRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassRosterMutationResponse> {
  const { data, error, response } = await createTutorHubClient(options).PATCH(
    "/api/v1/classes/{class_id}/roster/{user_id}",
    {
      params: {
        path: { class_id: classID, user_id: userID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassRosterMutationResponse>(
    data as ClassRosterMutationResponse | undefined,
    error,
    response,
  );
}

export async function bulkMutateClassRoster(
  classID: string,
  input: ClassRosterBulkRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassRosterBulkResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/roster/bulk",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassRosterBulkResponse>(
    data as ClassRosterBulkResponse | undefined,
    error,
    response,
  );
}

export async function listClassInviteCodes(
  classID: string,
  options: APIRequestOptions = {},
): Promise<ClassInviteCodeListResponse> {
  const { data, error, response } = await createTutorHubClient(options).GET(
    "/api/v1/classes/{class_id}/invite-codes",
    {
      params: { path: { class_id: classID } },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassInviteCodeListResponse>(
    data as ClassInviteCodeListResponse | undefined,
    error,
    response,
  );
}

export async function createClassInviteCode(
  classID: string,
  input: CreateClassInviteCodeRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<CreateClassInviteCodeResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/invite-codes",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<CreateClassInviteCodeResponse>(
    data as CreateClassInviteCodeResponse | undefined,
    error,
    response,
  );
}

export async function revokeClassInviteCode(
  classID: string,
  codeID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassInviteCode> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/invite-codes/{code_id}/revoke",
    {
      params: {
        path: { class_id: classID, code_id: codeID },
        header: { "X-CSRF-Token": csrfToken },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassInviteCode>(
    data as ClassInviteCode | undefined,
    error,
    response,
  );
}

export async function joinClassInvitation(
  input: ClassInvitationTokenRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<JoinClassInvitationResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/class-invitations/join",
    {
      params: { header: { "X-CSRF-Token": csrfToken } },
      body: input,
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<JoinClassInvitationResponse>(
    data as JoinClassInvitationResponse | undefined,
    error,
    response,
  );
}

export async function leaveClass(
  classID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<ClassEnrollment> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/leave",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<ClassEnrollment>(
    data as ClassEnrollment | undefined,
    error,
    response,
  );
}

export async function issueClassMediaToken(
  classID: string,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<MediaTokenResponse> {
  const { data, error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/media-token",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      headers: { Accept: "application/json" },
      signal: options.signal,
    },
  );

  return requireData<MediaTokenResponse>(
    data as MediaTokenResponse | undefined,
    error,
    response,
  );
}

export async function recordClassMediaEvent(
  classID: string,
  input: MediaEventRequest,
  csrfToken: string,
  options: APIRequestOptions = {},
): Promise<void> {
  const { error, response } = await createTutorHubClient(options).POST(
    "/api/v1/classes/{class_id}/media-events",
    {
      params: {
        path: { class_id: classID },
        header: { "X-CSRF-Token": csrfToken },
      },
      body: input,
      signal: options.signal,
    },
  );

  if (!response.ok) {
    throw new APIRequestError(
      response.status,
      isProblem(error) ? error : undefined,
      retryAfterSeconds(response),
    );
  }
}

function requireData<T>(
  data: T | undefined,
  error: unknown,
  response: Response,
): T {
  if (!response.ok || data === undefined) {
    throw new APIRequestError(
      response.status,
      isProblem(error) ? error : undefined,
      retryAfterSeconds(response),
    );
  }

  return data;
}

function requireTenantScope(tenantID: string): void {
  if (tenantID.trim() === "") {
    throw new TypeError("An active tenant ID is required.");
  }
}

function retryAfterSeconds(response: Response): number | undefined {
  const value = response.headers.get("Retry-After")?.trim();
  if (value === undefined || !/^\d+$/.test(value)) {
    return undefined;
  }

  const seconds = Number(value);
  return Number.isSafeInteger(seconds) && seconds >= 1 ? seconds : undefined;
}

function isProblem(value: unknown): value is Problem {
  if (typeof value !== "object" || value === null) {
    return false;
  }

  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.type === "string" &&
    typeof candidate.title === "string" &&
    typeof candidate.status === "number"
  );
}

function resolveBaseUrl(baseUrl: string): string {
  const normalizedBaseUrl = stripTrailingSlashes(baseUrl);

  try {
    return stripTrailingSlashes(new URL(normalizedBaseUrl).toString());
  } catch {
    const runtimeOrigin = globalThis.location?.origin;
    const origin =
      runtimeOrigin && runtimeOrigin !== "null"
        ? runtimeOrigin
        : "http://localhost";

    return stripTrailingSlashes(
      new URL(normalizedBaseUrl, `${origin}/`).toString(),
    );
  }
}

function resolvePublicRequestOrigin(baseUrl: string): string {
  const runtimeOrigin = globalThis.location?.origin;
  if (runtimeOrigin && runtimeOrigin !== "null") {
    return runtimeOrigin;
  }

  return new URL(resolveBaseUrl(baseUrl)).origin;
}

function createNormalizedFetch(
  baseUrl: string,
  fetchImplementation?: (request: Request) => Promise<Response>,
): (request: Request) => Promise<Response> {
  const execute =
    fetchImplementation ?? ((request: Request) => globalThis.fetch(request));

  return async (request: Request) => {
    const normalizedURL = normalizeOverlappingPath(
      new URL(request.url),
      baseUrl,
    );

    if (normalizedURL.toString() === request.url) {
      return execute(request);
    }

    return execute(await cloneRequestWithURL(request, normalizedURL));
  };
}

async function cloneRequestWithURL(
  request: Request,
  url: URL,
): Promise<Request> {
  const requestInit: RequestInit = {
    method: request.method,
    headers: request.headers,
    credentials: request.credentials,
    mode: request.mode,
    cache: request.cache,
    redirect: request.redirect,
    referrer: request.referrer,
    referrerPolicy: request.referrerPolicy,
    integrity: request.integrity,
    keepalive: request.keepalive,
    signal: request.signal,
  };

  if (
    request.body !== null &&
    request.method !== "GET" &&
    request.method !== "HEAD"
  ) {
    // Keep the rewritten request byte-identical without turning a bounded API
    // payload into a streamed upload, which Chromium rejects over HTTP/1.1.
    requestInit.body = await request.clone().arrayBuffer();
  }

  return new Request(url, requestInit);
}

function normalizeOverlappingPath(requestURL: URL, baseUrl: string): URL {
  const baseURL = new URL(baseUrl);
  if (requestURL.origin !== baseURL.origin) {
    return requestURL;
  }

  const baseSegments = splitPathSegments(baseURL.pathname);
  if (baseSegments.length === 0) {
    return requestURL;
  }

  const requestSegments = splitPathSegments(requestURL.pathname);
  const baseIsPrefix = baseSegments.every(
    (segment, index) => requestSegments[index] === segment,
  );
  if (!baseIsPrefix) {
    return requestURL;
  }

  const remainder = requestSegments.slice(baseSegments.length);
  const maximumOverlap = Math.min(baseSegments.length, remainder.length);
  let overlap = 0;

  for (let length = maximumOverlap; length > 0; length -= 1) {
    const baseSuffix = baseSegments.slice(baseSegments.length - length);
    const requestPrefix = remainder.slice(0, length);
    if (
      baseSuffix.every((segment, index) => segment === requestPrefix[index])
    ) {
      overlap = length;
      break;
    }
  }

  if (overlap === 0) {
    return requestURL;
  }

  const normalizedURL = new URL(requestURL);
  normalizedURL.pathname = `/${[
    ...baseSegments,
    ...remainder.slice(overlap),
  ].join("/")}`;
  return normalizedURL;
}

function splitPathSegments(pathname: string): string[] {
  return pathname.split("/").filter(Boolean);
}

function stripTrailingSlashes(value: string): string {
  let end = value.length;
  while (end > 0 && value.charCodeAt(end - 1) === 47) {
    end -= 1;
  }

  return value.slice(0, end);
}
