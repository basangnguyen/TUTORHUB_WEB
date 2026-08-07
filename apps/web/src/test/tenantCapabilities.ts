import type { TenantCapabilities } from "@tutorhub/api-client";

export function availableTenantCapabilities(
  tenantID: string,
): TenantCapabilities {
  const available = { available: true, reason: "available" } as const;
  return {
    tenant_id: tenantID,
    version: 0,
    can_manage_overrides: true,
    features: {
      availability_polls: {
        configured_enabled: true,
        enabled: true,
      },
      membership_invitations: {
        configured_enabled: true,
        enabled: true,
      },
      class_management: { configured_enabled: true, enabled: true },
      class_invite_links: { configured_enabled: true, enabled: true },
      class_session_scheduling: {
        configured_enabled: true,
        enabled: true,
      },
      class_session_recurrence: {
        configured_enabled: false,
        enabled: false,
      },
      conversations: {
        configured_enabled: true,
        enabled: true,
      },
      file_uploads: {
        configured_enabled: true,
        enabled: true,
      },
      in_app_notifications: {
        configured_enabled: false,
        enabled: false,
      },
    },
    quotas: {
      active_availability_polls: {
        configured_limit: 20,
        limit: 20,
        used: 0,
        remaining: 20,
      },
      active_study_meetings: {
        configured_limit: 20,
        limit: 20,
        used: 0,
        remaining: 20,
      },
      members: {
        configured_limit: 100,
        limit: 100,
        used: 1,
        remaining: 99,
      },
      active_classes: {
        configured_limit: 25,
        limit: 25,
        used: 1,
        remaining: 24,
      },
      invite_creations_per_hour: {
        configured_limit: 60,
        limit: 60,
        used: 0,
        remaining: 60,
      },
      availability_poll_range_days: {
        configured_limit: 31,
        limit: 31,
        used: 0,
        remaining: 31,
      },
      availability_poll_slots: {
        configured_limit: 336,
        limit: 336,
        used: 0,
        remaining: 336,
      },
      availability_poll_participants: {
        configured_limit: 100,
        limit: 100,
        used: 0,
        remaining: 100,
      },
      availability_poll_creations_per_hour: {
        configured_limit: 20,
        limit: 20,
        used: 0,
        remaining: 20,
      },
      availability_poll_capability_creations_per_hour: {
        configured_limit: 60,
        limit: 60,
        used: 0,
        remaining: 60,
      },
      study_meeting_creations_per_hour: {
        configured_limit: 20,
        limit: 20,
        used: 0,
        remaining: 20,
      },
      messages_per_tenant: {
        configured_limit: 100000,
        limit: 100000,
        used: 0,
        remaining: 100000,
      },
      message_sends_per_hour: {
        configured_limit: 5000,
        limit: 5000,
        used: 0,
        remaining: 5000,
      },
      files_per_tenant: {
        configured_limit: 10000,
        limit: 10000,
        used: 0,
        remaining: 10000,
      },
      file_bytes_per_tenant: {
        configured_limit: 10737418240,
        limit: 10737418240,
        used: 0,
        remaining: 10737418240,
      },
      single_file_bytes: {
        configured_limit: 104857600,
        limit: 104857600,
        used: 0,
        remaining: 104857600,
      },
      file_upload_intents_per_hour: {
        configured_limit: 1000,
        limit: 1000,
        used: 0,
        remaining: 1000,
      },
    },
    operations: {
      create_membership_invitation: available,
      accept_membership_invitation: available,
      create_class: available,
      activate_class: available,
      restore_active_class: available,
      create_class_invite_link: available,
      join_class_invite_link: available,
      schedule_class_session: available,
      create_availability_poll: available,
      create_availability_poll_capability: available,
      schedule_study_meeting: available,
      create_conversation: available,
      create_file_upload_intent: available,
    },
  };
}

export function withAvailableTenantCapabilities(
  fetchImplementation: unknown,
  tenantID: string,
): typeof fetch {
  const delegate = fetchImplementation as typeof fetch;
  return async (input, init) => {
    const request = input instanceof Request ? input : new Request(input, init);
    const url = new URL(request.url);
    if (
      request.method === "GET" &&
      url.pathname.endsWith(`/api/v1/tenants/${tenantID}/capabilities`)
    ) {
      return new Response(
        JSON.stringify(availableTenantCapabilities(tenantID)),
        { headers: { "Content-Type": "application/json" } },
      );
    }
    return delegate(input, init);
  };
}
