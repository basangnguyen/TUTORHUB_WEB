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
export type MediaTokenResponse = components["schemas"]["MediaTokenResponse"];
export type MediaEventRequest = components["schemas"]["MediaEventRequest"];
export type Problem = components["schemas"]["Problem"];

export class APIRequestError extends Error {
  readonly status: number;
  readonly problem?: Problem;

  constructor(status: number, problem?: Problem) {
    super(
      problem?.detail ?? problem?.title ?? `Core API phản hồi HTTP ${status}.`,
    );
    this.name = "APIRequestError";
    this.status = status;
    this.problem = problem;
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
    );
  }

  return data;
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
