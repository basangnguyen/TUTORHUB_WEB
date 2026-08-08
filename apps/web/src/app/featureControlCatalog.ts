import type { TenantCapabilities } from "@tutorhub/api-client";

export type TenantFeatureKey = keyof TenantCapabilities["features"];
export type TenantQuotaKey = keyof TenantCapabilities["quotas"];

export const tenantFeatureKeys = [
  "membership_invitations",
  "class_management",
  "class_invite_links",
  "class_session_scheduling",
  "class_session_recurrence",
  "conversations",
  "file_uploads",
  "in_app_notifications",
  "availability_polls",
] as const satisfies readonly TenantFeatureKey[];

export const tenantQuotaKeys = [
  "members",
  "active_classes",
  "invite_creations_per_hour",
  "active_availability_polls",
  "availability_poll_range_days",
  "availability_poll_slots",
  "availability_poll_participants",
  "availability_poll_creations_per_hour",
  "availability_poll_capability_creations_per_hour",
  "active_study_meetings",
  "study_meeting_creations_per_hour",
  "messages_per_tenant",
  "message_sends_per_hour",
  "files_per_tenant",
  "file_bytes_per_tenant",
  "single_file_bytes",
  "file_upload_intents_per_hour",
] as const satisfies readonly TenantQuotaKey[];

type MissingFeatureKey = Exclude<
  TenantFeatureKey,
  (typeof tenantFeatureKeys)[number]
>;
type MissingQuotaKey = Exclude<
  TenantQuotaKey,
  (typeof tenantQuotaKeys)[number]
>;
void (true satisfies [MissingFeatureKey] extends [never] ? true : never);
void (true satisfies [MissingQuotaKey] extends [never] ? true : never);
