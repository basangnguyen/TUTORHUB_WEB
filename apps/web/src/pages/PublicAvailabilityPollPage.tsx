import {
  APIRequestError,
  type AvailabilityPollAnswerState,
  type PublicAvailabilityPoll,
} from "@tutorhub/api-client";
import { Button, ErrorState, Skeleton, SkeletonGroup } from "@tutorhub/ui";
import { CalendarClock, CheckCircle2, Clock3, RefreshCw } from "lucide-react";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { useParams } from "react-router-dom";
import {
  usePublicAvailabilityPoll,
  useRespondPublicAvailabilityPoll,
} from "../app/availabilityPoll";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  clearPublicAvailabilityPollToken,
  consumePublicAvailabilityPollToken,
} from "../app/publicAvailabilityPollToken";

const answerStates = ["preferred", "available", "unavailable"] as const;

const answerStateKeys = {
  available: "availabilityPoll.public.available",
  preferred: "availabilityPoll.public.preferred",
  unavailable: "availabilityPoll.public.unavailable",
} as const satisfies Record<AvailabilityPollAnswerState, TranslationKey>;

const aggregateKeys = {
  high: "availabilityPoll.public.aggregate.high",
  low: "availabilityPoll.public.aggregate.low",
  medium: "availabilityPoll.public.aggregate.medium",
} as const satisfies Record<
  NonNullable<
    PublicAvailabilityPoll["ranked_slots"][number]["aggregate_bucket"]
  >,
  TranslationKey
>;

function isConcealedPublicError(error: Error | null) {
  return (
    error instanceof APIRequestError && [400, 404, 410].includes(error.status)
  );
}

function closedMessageKey(
  status: PublicAvailabilityPoll["status"],
): TranslationKey {
  if (status === "cancelled") {
    return "availabilityPoll.public.cancelled";
  }
  if (status === "finalized") {
    return "availabilityPoll.public.finalized";
  }
  return "availabilityPoll.public.closed";
}

export function PublicAvailabilityPollPage() {
  const { publicId = "" } = useParams();
  const { language, t } = useI18n();
  const [capabilityToken] = useState(consumePublicAvailabilityPollToken);
  const [answerDraft, setAnswerDraft] = useState<{
    answers: Readonly<Record<string, AvailabilityPollAnswerState>>;
    responseKey: string;
  } | null>(null);
  const [validationVisible, setValidationVisible] = useState(false);
  const [pageOpenedAt] = useState(Date.now);
  const idempotencyKey = useRef<string | null>(null);
  const paintState = useRef<AvailabilityPollAnswerState | null>(null);
  const pollQuery = usePublicAvailabilityPoll(publicId, capabilityToken);
  const exchange = pollQuery.data;
  const respond = useRespondPublicAvailabilityPoll(
    publicId,
    exchange?.response_token ?? null,
  );
  const poll = exchange?.poll;

  const dateTimeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
        timeZone: poll?.timezone,
      }),
    [language, poll?.timezone],
  );
  const timeFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        hour: "2-digit",
        minute: "2-digit",
        timeZone: poll?.timezone,
      }),
    [language, poll?.timezone],
  );

  useEffect(
    () => () => {
      clearPublicAvailabilityPollToken();
    },
    [],
  );

  useEffect(() => {
    const stopPainting = () => {
      paintState.current = null;
    };
    window.addEventListener("pointerup", stopPainting);
    window.addEventListener("pointercancel", stopPainting);
    window.addEventListener("blur", stopPainting);
    return () => {
      window.removeEventListener("pointerup", stopPainting);
      window.removeEventListener("pointercancel", stopPainting);
      window.removeEventListener("blur", stopPainting);
    };
  }, []);

  const responseKey = poll
    ? `${poll.public_id}:${poll.my_response?.version ?? 0}`
    : "";
  const savedAnswers = useMemo(
    () =>
      Object.fromEntries(
        (poll?.my_response?.answers ?? []).map((answer) => [
          answer.slot_id,
          answer.state,
        ]),
      ),
    [poll?.my_response?.answers],
  );
  const answers =
    answerDraft?.responseKey === responseKey
      ? answerDraft.answers
      : savedAnswers;

  const rankedSlots = useMemo(
    () =>
      new Map(
        (poll?.ranked_slots ?? []).map((rankedSlot) => [
          rankedSlot.slot.id,
          rankedSlot,
        ]),
      ),
    [poll?.ranked_slots],
  );

  const responseClosed =
    !poll ||
    poll.status !== "open" ||
    Date.parse(poll.deadline_at) <= pageOpenedAt ||
    isConcealedPublicError(respond.error);
  const terminalLoadError = isConcealedPublicError(pollQuery.error);
  const conflict =
    respond.error instanceof APIRequestError && respond.error.status === 409;
  const selectedAnswerCount = Object.keys(answers).length;

  const chooseAnswer = (slotID: string, state: AvailabilityPollAnswerState) => {
    setAnswerDraft({
      answers: { ...answers, [slotID]: state },
      responseKey,
    });
    setValidationVisible(false);
    idempotencyKey.current = null;
    respond.reset();
  };

  const beginPainting = (
    event: ReactPointerEvent<HTMLLabelElement>,
    slotID: string,
    state: AvailabilityPollAnswerState,
  ) => {
    if (responseClosed || event.button !== 0) {
      return;
    }
    chooseAnswer(slotID, state);
    if (event.pointerType !== "touch") {
      paintState.current = state;
    }
  };

  const continuePainting = (
    event: ReactPointerEvent<HTMLLabelElement>,
    slotID: string,
  ) => {
    const state = paintState.current;
    if (responseClosed || !state || (event.buttons & 1) !== 1) {
      return;
    }
    chooseAnswer(slotID, state);
  };

  const submit = async () => {
    if (!poll || responseClosed) {
      return;
    }
    const submittedAnswers = poll.slots.flatMap((slot) => {
      const state = answers[slot.id];
      return state ? [{ slot_id: slot.id, state }] : [];
    });
    if (submittedAnswers.length === 0) {
      setValidationVisible(true);
      return;
    }

    idempotencyKey.current ??= `public-availability:${crypto.randomUUID()}`;
    try {
      await respond.mutateAsync({
        answers: submittedAnswers,
        expectedResponseVersion: poll.my_response?.version ?? 0,
        idempotencyKey: idempotencyKey.current,
      });
      idempotencyKey.current = null;
    } catch {
      // The mutation exposes a recoverable, capability-safe status below.
    }
  };

  return (
    <main className="public-availability-page">
      <section
        aria-labelledby="public-availability-title"
        className="public-availability-card"
      >
        <header className="public-availability-card__header">
          <div aria-hidden="true" className="public-availability-card__brand">
            <CalendarClock />
          </div>
          <div>
            <p className="public-availability-card__kicker">
              {t("availabilityPoll.public.kicker")}
            </p>
            <h1 id="public-availability-title">
              {poll?.title ?? t("availabilityPoll.public.pageTitle")}
            </h1>
            {poll && (
              <p className="public-availability-card__description">
                {poll.description ||
                  t("availabilityPoll.public.descriptionFallback")}
              </p>
            )}
          </div>
        </header>

        {(!capabilityToken || !publicId || terminalLoadError) && (
          <ErrorState
            description={t("availabilityPoll.public.unavailableDescription")}
            title={t("availabilityPoll.public.unavailableTitle")}
          />
        )}

        {capabilityToken && publicId && pollQuery.isPending && (
          <SkeletonGroup label={t("availabilityPoll.public.loading")}>
            <Skeleton height={26} width="72%" />
            <Skeleton height={72} />
            <Skeleton height={132} />
          </SkeletonGroup>
        )}

        {pollQuery.isError && !terminalLoadError && (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void pollQuery.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("availabilityPoll.public.loadErrorDescription")}
            title={t("availabilityPoll.public.loadErrorTitle")}
          />
        )}

        {poll && !terminalLoadError && (
          <div className="public-availability-card__content">
            <dl className="public-availability-card__facts">
              <div>
                <dt>
                  <Clock3 aria-hidden="true" />
                  {t("availabilityPoll.public.deadline")}
                </dt>
                <dd>
                  <time dateTime={poll.deadline_at}>
                    {dateTimeFormatter.format(new Date(poll.deadline_at))}
                  </time>
                </dd>
              </div>
              <div>
                <dt>{t("availabilityPoll.public.timezone")}</dt>
                <dd>{poll.timezone}</dd>
              </div>
            </dl>

            {poll.slots.length === 0 ? (
              <div className="public-availability-card__empty" role="status">
                <h2>{t("availabilityPoll.public.noSlotsTitle")}</h2>
                <p>{t("availabilityPoll.public.noSlotsDescription")}</p>
              </div>
            ) : (
              <form
                aria-describedby="public-availability-response-hint"
                className="public-availability-card__form"
                onSubmit={(event) => {
                  event.preventDefault();
                  void submit();
                }}
              >
                <p id="public-availability-response-hint">
                  {t("availabilityPoll.public.responseHint")}
                </p>
                <ol className="public-availability-slots">
                  {poll.slots.map((slot) => {
                    const rankedSlot = rankedSlots.get(slot.id);
                    const aggregateBucket = rankedSlot?.aggregate_bucket;
                    const slotLabel = `${dateTimeFormatter.format(
                      new Date(slot.starts_at),
                    )} – ${timeFormatter.format(new Date(slot.ends_at))}`;
                    return (
                      <li key={slot.id}>
                        <div className="public-availability-slot__time">
                          <time dateTime={slot.starts_at}>{slotLabel}</time>
                          <div
                            className="public-availability-slot__aggregate"
                            data-level={aggregateBucket ?? "withheld"}
                          >
                            <span aria-hidden="true" />
                            <small>
                              {rankedSlot?.cohort_satisfied && aggregateBucket
                                ? `${t("availabilityPoll.public.summary")}: ${t(
                                    aggregateKeys[aggregateBucket],
                                  )}`
                                : t(
                                    "availabilityPoll.public.aggregate.withheld",
                                  )}
                            </small>
                          </div>
                        </div>
                        <fieldset disabled={responseClosed}>
                          <legend className="visually-hidden">
                            {t("availabilityPoll.public.slotChoices", {
                              time: slotLabel,
                            })}
                          </legend>
                          <div className="public-availability-slot__choices">
                            {answerStates.map((state) => (
                              <label
                                key={state}
                                onPointerDown={(event) =>
                                  beginPainting(event, slot.id, state)
                                }
                                onPointerEnter={(event) =>
                                  continuePainting(event, slot.id)
                                }
                              >
                                <input
                                  checked={answers[slot.id] === state}
                                  name={`availability-${slot.id}`}
                                  onChange={() => chooseAnswer(slot.id, state)}
                                  type="radio"
                                  value={state}
                                />
                                <span>{t(answerStateKeys[state])}</span>
                              </label>
                            ))}
                          </div>
                        </fieldset>
                      </li>
                    );
                  })}
                </ol>

                {responseClosed ? (
                  <p className="public-availability-card__closed" role="status">
                    {t(closedMessageKey(poll.status))}
                  </p>
                ) : (
                  <div className="public-availability-card__actions">
                    <div aria-live="polite">
                      {validationVisible && (
                        <p
                          className="public-availability-card__error"
                          role="alert"
                        >
                          {t("availabilityPoll.public.selectOne")}
                        </p>
                      )}
                      {respond.isSuccess && (
                        <p
                          className="public-availability-card__success"
                          role="status"
                        >
                          <CheckCircle2 aria-hidden="true" />
                          {t("availabilityPoll.public.saved")}
                        </p>
                      )}
                      {respond.isError &&
                        !isConcealedPublicError(respond.error) && (
                          <p
                            className="public-availability-card__error"
                            role="alert"
                          >
                            {t(
                              conflict
                                ? "availabilityPoll.public.conflict"
                                : "availabilityPoll.public.saveError",
                            )}
                          </p>
                        )}
                    </div>
                    <Button
                      disabled={selectedAnswerCount === 0}
                      loading={respond.isPending}
                      loadingLabel={t("availabilityPoll.public.submitting")}
                      type="submit"
                    >
                      {t("availabilityPoll.public.submit")}
                    </Button>
                  </div>
                )}

                {conflict && (
                  <Button
                    leadingIcon={<RefreshCw />}
                    onClick={() => void pollQuery.refetch()}
                    type="button"
                    variant="secondary"
                  >
                    {t("availabilityPoll.public.reload")}
                  </Button>
                )}
              </form>
            )}
          </div>
        )}
      </section>
    </main>
  );
}
