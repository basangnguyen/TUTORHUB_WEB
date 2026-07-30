import { APIRequestError, type SessionRSVPState } from "@tutorhub/api-client";
import { Button } from "@tutorhub/ui";
import { RefreshCw } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";
import {
  participationIdempotencyKey,
  participationSourceFingerprint,
  useParticipationAudience,
  useRespondToParticipation,
  type ClassSessionParticipationSource,
} from "../../app/classSessionParticipation";
import { useI18n, type TranslationKey } from "../../app/i18n";
import { shouldConcealTenantScopedData } from "../../app/tenantDataAccess";
import type { CalendarItemViewModel } from "./model";
import { SchedulingAssistantPanel } from "./SchedulingAssistantPanel";
import { SessionAudienceEditor } from "./SessionAudienceEditor";

interface SessionParticipationPanelProps {
  hourCycle: "h12" | "h23";
  item: CalendarItemViewModel;
  locale: string;
  onUseSuggestedTime?: (startsAt: string, endsAt: string) => void;
  secondaryTimezone: string | null;
  tenantID: string | undefined;
  userID: string | undefined;
}

const rsvpStates = ["accepted", "tentative", "declined"] as const;

const rsvpKeys: Record<SessionRSVPState, TranslationKey> = {
  accepted: "calendar.participation.rsvp.accepted",
  declined: "calendar.participation.rsvp.declined",
  needs_action: "calendar.participation.rsvp.needsAction",
  tentative: "calendar.participation.rsvp.tentative",
};

const participationRoleKeys = {
  optional: "calendar.participation.role.optional",
  required: "calendar.participation.role.required",
} as const satisfies Record<string, TranslationKey>;

const businessRoleKeys = {
  co_teacher: "calendar.participation.businessRole.coTeacher",
  organizer: "calendar.participation.businessRole.organizer",
  student: "calendar.participation.businessRole.student",
  teacher: "calendar.participation.businessRole.teacher",
  teaching_assistant: "calendar.participation.businessRole.teachingAssistant",
} as const satisfies Record<string, TranslationKey>;

function abbreviatedUserID(userID: string) {
  return userID.slice(-8);
}

export function SessionParticipationPanel({
  hourCycle,
  item,
  locale,
  onUseSuggestedTime,
  secondaryTimezone,
  tenantID,
  userID,
}: SessionParticipationPanelProps) {
  const { t } = useI18n();
  const participationSource = useMemo<
    ClassSessionParticipationSource | undefined
  >(() => {
    if (item.sourceType !== "class_session") {
      return undefined;
    }
    if (item.seriesID && item.occurrenceKey) {
      return {
        kind: "occurrence",
        occurrenceKey: item.occurrenceKey,
        seriesID: item.seriesID,
      };
    }
    if (item.seriesID) {
      return { kind: "series", seriesID: item.seriesID };
    }
    return { kind: "session", sessionID: item.sourceID };
  }, [item.occurrenceKey, item.seriesID, item.sourceID, item.sourceType]);
  const sourceFingerprint = participationSourceFingerprint(participationSource);
  const enabled = participationSource !== undefined;
  const audience = useParticipationAudience(
    tenantID,
    userID,
    item.classID ?? undefined,
    participationSource,
    enabled,
  );
  const response = useRespondToParticipation(
    tenantID,
    userID,
    item.classID ?? undefined,
    participationSource,
  );
  const resetResponse = response.reset;
  const conflictButton = useRef<HTMLButtonElement>(null);
  const conflict =
    response.error instanceof APIRequestError && response.error.status === 409;
  const concealed = shouldConcealTenantScopedData(audience.error);
  const attendees = useMemo(
    () => audience.data?.attendees ?? [],
    [audience.data?.attendees],
  );
  const self = useMemo(
    () => attendees.find((attendee) => attendee.is_self),
    [attendees],
  );

  useEffect(() => {
    resetResponse();
  }, [item.classID, resetResponse, sourceFingerprint, tenantID, userID]);

  useEffect(() => {
    if (conflict) {
      conflictButton.current?.focus();
    }
  }, [conflict]);

  if (!enabled || !tenantID || !userID) {
    return null;
  }

  if (audience.isPending) {
    return (
      <section
        aria-labelledby="calendar-participation-title"
        className="calendar-participation"
      >
        <h3 id="calendar-participation-title">
          {t("calendar.participation.title")}
        </h3>
        <p aria-live="polite">{t("calendar.participation.loading")}</p>
      </section>
    );
  }

  if (audience.isError) {
    return (
      <section
        aria-labelledby="calendar-participation-title"
        className="calendar-participation"
      >
        <h3 id="calendar-participation-title">
          {t("calendar.participation.title")}
        </h3>
        <div className="calendar-participation__feedback" role="alert">
          <p>
            {concealed
              ? t("calendar.participation.concealed")
              : t("calendar.participation.loadError")}
          </p>
          {!concealed && (
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void audience.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("calendar.retry")}
            </Button>
          )}
        </div>
      </section>
    );
  }

  const viewerAccess = audience.data?.viewer_access;
  const submitResponse = (state: (typeof rsvpStates)[number]) => {
    if (!self || response.isPending) {
      return;
    }
    response.mutate({
      input: {
        expected_attendee_version: self.version,
        idempotency_key: participationIdempotencyKey("rsvp"),
        state,
      },
    });
  };

  const reloadAfterConflict = async () => {
    resetResponse();
    await audience.refetch();
  };

  return (
    <section
      aria-labelledby="calendar-participation-title"
      className="calendar-participation"
    >
      <div className="calendar-participation__heading">
        <div>
          <h3 id="calendar-participation-title">
            {t("calendar.participation.title")}
          </h3>
          <p>{t("calendar.participation.description")}</p>
        </div>
        {viewerAccess?.can_see_guest_list && (
          <span className="calendar-participation__count">
            {t("calendar.participation.count", {
              count: attendees.length,
            })}
          </span>
        )}
      </div>

      {audience.data &&
        item.classID &&
        participationSource &&
        item.status === "scheduled" &&
        viewerAccess?.can_manage_attendees && (
          <SessionAudienceEditor
            audience={audience.data}
            classID={item.classID}
            key={`audience-editor:${tenantID}:${userID}:${sourceFingerprint}:${audience.data.audience_revision}`}
            onReloadAudience={() => audience.refetch()}
            source={participationSource}
            tenantID={tenantID}
            userID={userID}
          />
        )}

      {viewerAccess?.can_see_guest_list && attendees.length === 0 && (
        <p className="calendar-participation__empty">
          {t("calendar.participation.empty")}
        </p>
      )}

      {viewerAccess?.can_see_guest_list && attendees.length > 0 && (
        <ul
          aria-label={t("calendar.participation.listLabel")}
          className="calendar-participation__list"
        >
          {attendees.map((attendee) => (
            <li key={attendee.id}>
              <span className="calendar-participation__attendee">
                <strong>
                  {attendee.is_self
                    ? t("calendar.participation.you")
                    : t("calendar.participation.internalUser", {
                        id: abbreviatedUserID(attendee.user_id),
                      })}
                </strong>
                <small>
                  {t(businessRoleKeys[attendee.business_role])} ·{" "}
                  {t(participationRoleKeys[attendee.participation_role])}
                </small>
              </span>
              <span className="calendar-participation__state">
                {t(rsvpKeys[attendee.rsvp_state])}
              </span>
            </li>
          ))}
        </ul>
      )}

      {audience.data &&
        item.classID &&
        item.status === "scheduled" &&
        viewerAccess?.can_manage_attendees && (
          <SchedulingAssistantPanel
            audience={audience.data}
            classID={item.classID}
            endsAt={item.endsAt}
            hourCycle={hourCycle}
            key={`scheduling-assistant:${tenantID}:${userID}:${sourceFingerprint}:${audience.data.audience_revision}`}
            locale={locale}
            onUseSuggestion={onUseSuggestedTime}
            primaryTimezone={item.displayTimezone}
            secondaryTimezone={secondaryTimezone}
            startsAt={item.startsAt}
            tenantID={tenantID}
            userID={userID}
          />
        )}

      {!viewerAccess?.can_see_guest_list && !self && (
        <p className="calendar-participation__empty">
          {t("calendar.participation.notInvited")}
        </p>
      )}

      {self && (
        <div className="calendar-participation__self">
          <div>
            <h4>{t("calendar.participation.yourResponse")}</h4>
            <p>
              {audience.data?.response_requested
                ? t("calendar.participation.responseRequested")
                : t("calendar.participation.responseNotRequested")}
            </p>
          </div>
          <div
            aria-label={t("calendar.participation.responseChoices")}
            className="calendar-participation__rsvp"
            role="group"
          >
            {rsvpStates.map((state) => (
              <Button
                aria-pressed={self.rsvp_state === state}
                disabled={!viewerAccess?.can_respond || response.isPending}
                key={state}
                onClick={() => submitResponse(state)}
                size="sm"
                variant={self.rsvp_state === state ? "primary" : "secondary"}
              >
                {t(rsvpKeys[state])}
              </Button>
            ))}
          </div>
        </div>
      )}

      {response.isSuccess && (
        <p aria-live="polite" className="calendar-participation__success">
          {t("calendar.participation.saved")}
        </p>
      )}
      {response.isError && (
        <div className="calendar-participation__feedback" role="alert">
          <p>
            {conflict
              ? t("calendar.participation.conflict")
              : t("calendar.participation.saveError")}
          </p>
          {conflict && (
            <Button
              onClick={() => void reloadAfterConflict()}
              ref={conflictButton}
              size="sm"
              variant="secondary"
            >
              {t("calendar.participation.reload")}
            </Button>
          )}
        </div>
      )}
    </section>
  );
}
