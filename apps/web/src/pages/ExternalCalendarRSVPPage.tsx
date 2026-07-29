import {
  APIRequestError,
  type ExternalCalendarRSVPProjection,
  type SessionSelfRSVPState,
} from "@tutorhub/api-client";
import { Button, ErrorState, Skeleton, SkeletonGroup } from "@tutorhub/ui";
import { CalendarCheck2, CheckCircle2, RefreshCw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  clearFragmentTokenEscrow,
  consumeFragmentTokens,
} from "../app/fragmentToken";
import {
  useExternalCalendarRSVP,
  useRespondExternalCalendarRSVP,
} from "../app/externalCalendarRSVP";
import { useI18n, type TranslationKey } from "../app/i18n";

const externalRSVPEscrowKey = "external-calendar-rsvp";
const responseStates = ["accepted", "tentative", "declined"] as const;

const responseKeys = {
  accepted: "calendar.participation.rsvp.accepted",
  declined: "calendar.participation.rsvp.declined",
  needs_action: "calendar.participation.rsvp.needsAction",
  tentative: "calendar.participation.rsvp.tentative",
} as const satisfies Record<
  ExternalCalendarRSVPProjection["rsvp_state"],
  TranslationKey
>;

function unavailable(error: Error | null) {
  return (
    error instanceof APIRequestError && [400, 404, 410].includes(error.status)
  );
}

export function ExternalCalendarRSVPPage() {
  const [tokens] = useState(() =>
    consumeFragmentTokens(
      externalRSVPEscrowKey,
      ["resolve_token", "respond_token"] as const,
      512,
    ),
  );
  const { language, t } = useI18n();
  const [state, setState] = useState<SessionSelfRSVPState>("accepted");
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const idempotencyKey = useRef<string | null>(null);
  const resolve = useExternalCalendarRSVP(
    tokens.resolve_token,
    Boolean(tokens.resolve_token && tokens.respond_token),
  );
  const respond = useRespondExternalCalendarRSVP(tokens.respond_token);
  const projection = respond.data?.projection ?? resolve.data;
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "full",
        timeStyle: "short",
      }),
    [language],
  );

  useEffect(
    () => () => {
      clearFragmentTokenEscrow(externalRSVPEscrowKey);
    },
    [],
  );

  const submit = async () => {
    if (!projection || !confirmed || !tokens.respond_token) {
      return;
    }
    idempotencyKey.current ??= `external-rsvp:${crypto.randomUUID()}`;
    try {
      await respond.mutateAsync({
        expectedAttendeeVersion: projection.attendee_version,
        idempotencyKey: idempotencyKey.current,
        note,
        state,
      });
    } catch {
      // The mutation exposes a privacy-preserving recoverable state below.
    }
  };

  const terminalError =
    unavailable(resolve.error) || unavailable(respond.error);
  const conflict =
    respond.error instanceof APIRequestError && respond.error.status === 409;

  return (
    <main className="external-calendar-rsvp-page">
      <section
        aria-labelledby="external-calendar-rsvp-title"
        className="external-calendar-rsvp-card"
      >
        <div className="external-calendar-rsvp-card__brand" aria-hidden="true">
          <CalendarCheck2 />
        </div>
        <p className="external-calendar-rsvp-card__kicker">
          {t("calendar.externalRSVP.kicker")}
        </p>
        <h1 id="external-calendar-rsvp-title">
          {t("calendar.externalRSVP.title")}
        </h1>

        {(!tokens.resolve_token || !tokens.respond_token || terminalError) && (
          <ErrorState
            description={t("calendar.externalRSVP.unavailableDescription")}
            title={t("calendar.externalRSVP.unavailableTitle")}
          />
        )}

        {tokens.resolve_token && tokens.respond_token && resolve.isPending && (
          <SkeletonGroup label={t("calendar.externalRSVP.loading")}>
            <Skeleton height={26} width="70%" />
            <Skeleton height={84} />
            <Skeleton height={44} />
          </SkeletonGroup>
        )}

        {resolve.isError && !terminalError && (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void resolve.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("calendar.externalRSVP.loadErrorDescription")}
            title={t("calendar.externalRSVP.loadErrorTitle")}
          />
        )}

        {projection && !terminalError && (
          <div className="external-calendar-rsvp-card__content">
            <dl className="external-calendar-rsvp-card__facts">
              <div>
                <dt>{t("calendar.externalRSVP.eventLabel")}</dt>
                <dd>{projection.title}</dd>
              </div>
              <div>
                <dt>{t("calendar.externalRSVP.timeLabel")}</dt>
                <dd>
                  <time dateTime={projection.starts_at}>
                    {dateFormatter.format(new Date(projection.starts_at))}
                  </time>
                  <span aria-hidden="true"> – </span>
                  <time dateTime={projection.ends_at}>
                    {dateFormatter.format(new Date(projection.ends_at))}
                  </time>
                </dd>
              </div>
              <div>
                <dt>{t("calendar.externalRSVP.timezoneLabel")}</dt>
                <dd>{projection.timezone}</dd>
              </div>
              <div>
                <dt>{t("calendar.externalRSVP.currentLabel")}</dt>
                <dd>{t(responseKeys[projection.rsvp_state])}</dd>
              </div>
            </dl>

            {respond.isSuccess ? (
              <div
                aria-live="polite"
                className="external-calendar-rsvp-card__success"
                role="status"
              >
                <CheckCircle2 aria-hidden="true" />
                <div>
                  <h2>{t("calendar.externalRSVP.savedTitle")}</h2>
                  <p>{t("calendar.externalRSVP.savedDescription")}</p>
                </div>
              </div>
            ) : projection.response_requested ? (
              <form
                className="external-calendar-rsvp-card__form"
                onSubmit={(event) => {
                  event.preventDefault();
                  void submit();
                }}
              >
                <fieldset>
                  <legend>{t("calendar.externalRSVP.choiceLabel")}</legend>
                  <div className="external-calendar-rsvp-card__choices">
                    {responseStates.map((responseState) => (
                      <label key={responseState}>
                        <input
                          checked={state === responseState}
                          name="external-rsvp-state"
                          onChange={() => {
                            respond.reset();
                            idempotencyKey.current = null;
                            setState(responseState);
                          }}
                          type="radio"
                          value={responseState}
                        />
                        <span>{t(responseKeys[responseState])}</span>
                      </label>
                    ))}
                  </div>
                </fieldset>
                <label className="external-calendar-rsvp-card__note">
                  <span>{t("calendar.externalRSVP.noteLabel")}</span>
                  <textarea
                    maxLength={500}
                    onChange={(event) => {
                      respond.reset();
                      idempotencyKey.current = null;
                      setNote(event.target.value);
                    }}
                    rows={3}
                    value={note}
                  />
                  <small>{t("calendar.externalRSVP.noteHint")}</small>
                </label>
                <label className="external-calendar-rsvp-card__confirm">
                  <input
                    checked={confirmed}
                    onChange={(event) => setConfirmed(event.target.checked)}
                    type="checkbox"
                  />
                  <span>
                    <strong>{t("calendar.externalRSVP.confirmation")}</strong>
                    <small>{t("calendar.externalRSVP.confirmationHint")}</small>
                  </span>
                </label>

                {respond.isError && !terminalError && (
                  <p
                    className="external-calendar-rsvp-card__error"
                    role="alert"
                  >
                    {t(
                      conflict
                        ? "calendar.externalRSVP.conflict"
                        : "calendar.externalRSVP.saveError",
                    )}
                  </p>
                )}

                <Button
                  disabled={!confirmed}
                  loading={respond.isPending}
                  loadingLabel={t("calendar.externalRSVP.submitting")}
                  type="submit"
                >
                  {t("calendar.externalRSVP.submit")}
                </Button>
              </form>
            ) : (
              <p className="external-calendar-rsvp-card__closed">
                {t("calendar.externalRSVP.responseClosed")}
              </p>
            )}
          </div>
        )}
      </section>
    </main>
  );
}
