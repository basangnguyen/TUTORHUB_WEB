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
    },
    quotas: {
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
