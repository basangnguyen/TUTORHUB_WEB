const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export interface NotificationTargetResource {
  context: Readonly<Record<string, string>>;
  resource_id?: string | null;
  resource_type?: string | null;
}

/**
 * Notification rows deliberately do not carry arbitrary URLs. Navigation is
 * derived from a small application-owned allowlist so an event payload can
 * never turn the notification center into an open redirect.
 */
export function notificationTarget(
  notification: NotificationTargetResource,
): string | null {
  switch (notification.resource_type) {
    case "class":
      return validID(notification.resource_id)
        ? `/app/classrooms/${notification.resource_id}`
        : null;
    case "class_session": {
      const classID = notification.context.class_id;
      return validID(classID) ? `/app/classrooms/${classID}` : null;
    }
    case "tenant":
    case "membership_invitation":
      return "/app/workspace";
    case "profile":
      return "/app/settings";
    default:
      return null;
  }
}

function validID(value: string | null | undefined): value is string {
  return Boolean(value && uuidPattern.test(value));
}
