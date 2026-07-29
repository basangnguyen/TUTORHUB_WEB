import {
  Button,
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerTitle,
  StatusBadge,
} from "@tutorhub/ui";
import { Pencil } from "lucide-react";
import { useMemo } from "react";
import { useI18n, type TranslationKey } from "../../app/i18n";
import type { CalendarHourCycle, CalendarItemViewModel } from "./model";
import { SessionParticipationPanel } from "./SessionParticipationPanel";

interface SessionDetailDrawerProps {
  hourCycle: CalendarHourCycle;
  item: CalendarItemViewModel | null;
  locale: string;
  onClose: () => void;
  onEdit: (item: CalendarItemViewModel) => void;
  secondaryTimezone?: string | null;
  tenantID?: string;
  userID?: string;
}

function statusTranslation(status: string): TranslationKey {
  if (status === "scheduled") {
    return "calendar.status.scheduled";
  }
  if (status === "cancelled") {
    return "calendar.status.cancelled";
  }
  return "calendar.status.other";
}

function statusTone(status: string) {
  return status === "cancelled" ? "danger" : "info";
}

export function SessionDetailDrawer({
  hourCycle,
  item,
  locale,
  onClose,
  onEdit,
  secondaryTimezone = null,
  tenantID,
  userID,
}: SessionDetailDrawerProps) {
  const { t } = useI18n();
  const dateTimeFormatter = useMemo(
    () =>
      item
        ? new Intl.DateTimeFormat(locale, {
            day: "numeric",
            hour: "numeric",
            hour12: hourCycle === "h12",
            minute: "2-digit",
            month: "long",
            timeZone: item.displayTimezone,
            weekday: "long",
            year: "numeric",
          })
        : null,
    [hourCycle, item, locale],
  );

  if (!item || !dateTimeFormatter) {
    return null;
  }

  return (
    <Drawer
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      open
    >
      <DrawerContent
        className="calendar-session-detail"
        closeLabel={t("calendar.closeSessionDetails")}
      >
        <DrawerTitle>{item.title}</DrawerTitle>
        <DrawerDescription>
          {t("calendar.sessionDetailDescription")}
        </DrawerDescription>

        <dl className="calendar-session-detail__summary">
          <div>
            <dt>{t("calendar.sessionDetailTime")}</dt>
            <dd>
              <time dateTime={item.startsAt}>
                {dateTimeFormatter.format(new Date(item.startsAt))}
              </time>
              <span aria-hidden="true"> – </span>
              <time dateTime={item.endsAt}>
                {dateTimeFormatter.format(new Date(item.endsAt))}
              </time>
              <small>
                {t("calendar.sessionDetailTimezone", {
                  timezone: item.displayTimezone,
                })}
              </small>
            </dd>
          </div>
          <div>
            <dt>{t("calendar.sessionDetailClass")}</dt>
            <dd>{item.classTitle || t("calendar.classFallback")}</dd>
          </div>
          <div>
            <dt>{t("calendar.sessionDetailStatus")}</dt>
            <dd>
              <StatusBadge tone={statusTone(item.status)}>
                {t(statusTranslation(item.status), { status: item.status })}
              </StatusBadge>
            </dd>
          </div>
        </dl>

        {tenantID && userID && (
          <SessionParticipationPanel
            hourCycle={hourCycle}
            item={item}
            locale={locale}
            onUseSuggestedTime={
              item.canEdit
                ? (startsAt, endsAt) =>
                    onEdit({
                      ...item,
                      endsAt,
                      startsAt,
                    })
                : undefined
            }
            secondaryTimezone={secondaryTimezone}
            tenantID={tenantID}
            userID={userID}
          />
        )}

        {item.canEdit && (
          <div className="calendar-session-detail__actions">
            <Button
              leadingIcon={<Pencil />}
              onClick={() => onEdit(item)}
              variant="secondary"
            >
              {t("calendar.editSession")}
            </Button>
          </div>
        )}
      </DrawerContent>
    </Drawer>
  );
}
