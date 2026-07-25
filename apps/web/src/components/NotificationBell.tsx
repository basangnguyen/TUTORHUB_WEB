import { Bell } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useI18n } from "../app/i18n";
import { useNotificationUnreadCount } from "../app/notifications";

export function NotificationBell({ tenantID }: { tenantID: string }) {
  const { t } = useI18n();
  const unread = useNotificationUnreadCount(tenantID, true);
  const previousCount = useRef<number | null>(null);
  const [announcement, setAnnouncement] = useState("");
  const count = unread.data?.count ?? 0;

  useEffect(() => {
    if (!unread.data) {
      return;
    }
    if (previousCount.current !== null && previousCount.current !== count) {
      setAnnouncement(
        t(
          unread.data.is_capped
            ? "notificationBell.unreadChangedCapped"
            : "notificationBell.unreadChanged",
          { count },
        ),
      );
    }
    previousCount.current = count;
  }, [count, t, unread.data]);

  const label = count
    ? t(
        unread.data?.is_capped
          ? "notificationBell.openUnreadCapped"
          : "notificationBell.openUnread",
        { count },
      )
    : t("notificationBell.open");

  return (
    <>
      <Link
        aria-label={label}
        className="notification-bell"
        title={label}
        to="/app/notifications"
      >
        <Bell aria-hidden="true" />
        {unread.isSuccess && count > 0 && (
          <span className="notification-bell__count" aria-hidden="true">
            {count > 99 || unread.data.is_capped ? "99+" : count}
          </span>
        )}
      </Link>
      <span aria-live="polite" className="visually-hidden">
        {announcement}
      </span>
    </>
  );
}
