import {
  APIRequestError,
  getFacebookDeletionStatus,
  type FacebookDeletionStatus,
} from "@tutorhub/api-client";
import { Button, ErrorState, Skeleton, SkeletonGroup } from "@tutorhub/ui";
import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useId, useState } from "react";
import { Link, useLocation } from "react-router";
import { useI18n, type TranslationKey } from "../app/i18n";
import { getApiBaseUrl } from "./SignInPage.shared";
import "./SignInPage.css";
import "./AccountLinkPage.css";
import "./CompleteRegistrationPage.css";

/** Upper bound for a displayed Retry-After; the server stays authoritative. */
const MAX_RETRY_AFTER_SECONDS = 3600;
/** Meta receipts are 32 lowercase hex characters (OpenAPI pattern). */
const CONFIRMATION_CODE_PATTERN = /^[0-9a-f]{32}$/;

/**
 * Returns the single `code` query value when it is well-formed. Anything else
 * (missing, repeated, wrong shape) is treated as an unknown code without an
 * API call, and the raw value is never rendered.
 */
function readConfirmationCode(search: string): string | null {
  const values = new URLSearchParams(search).getAll("code");
  const value = values.length === 1 ? values[0] : undefined;
  return value !== undefined && CONFIRMATION_CODE_PATTERN.test(value)
    ? value
    : null;
}

type StatusState =
  | { status: "loading" }
  | { status: "ready"; receipt: FacebookDeletionStatus["status"] }
  | { status: "notFound" }
  | { status: "rateLimited"; seconds?: number }
  | { status: "busy" }
  | { status: "error" };

function boundedSeconds(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.min(Math.ceil(value), MAX_RETRY_AFTER_SECONDS)
    : undefined;
}

function stateFromError(error: unknown): StatusState {
  if (!(error instanceof APIRequestError)) return { status: "error" };
  switch (error.status) {
    case 404:
      return { status: "notFound" };
    case 429:
      return {
        status: "rateLimited",
        seconds: boundedSeconds(error.retryAfterSeconds),
      };
    case 503:
      return { status: "busy" };
    default:
      return { status: "error" };
  }
}

function useDeletionStatus(code: string | null) {
  const [state, setState] = useState<StatusState>(() =>
    code ? { status: "loading" } : { status: "notFound" },
  );
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    if (!code) return;
    const controller = new AbortController();
    getFacebookDeletionStatus(code, {
      baseUrl: getApiBaseUrl(),
      signal: controller.signal,
    }).then(
      (result) => {
        if (!controller.signal.aborted) {
          setState({ status: "ready", receipt: result.status });
        }
      },
      (error: unknown) => {
        if (!controller.signal.aborted) setState(stateFromError(error));
      },
    );
    return () => controller.abort();
  }, [code, generation]);

  const reload = useCallback(() => {
    setState({ status: "loading" });
    setGeneration((value) => value + 1);
  }, []);
  return { reload, state };
}

const RECEIPT_COPY: Record<
  FacebookDeletionStatus["status"],
  { description: TranslationKey; title: TranslationKey }
> = {
  completed: {
    description: "dataDeletion.completedDescription",
    title: "dataDeletion.completedTitle",
  },
  received: {
    description: "dataDeletion.receivedDescription",
    title: "dataDeletion.receivedTitle",
  },
};

/**
 * AUTH-06B: public receipt page for Meta's data deletion callback
 * (`/legal/data-deletion?code=<confirmation_code>`). It only shows the coarse
 * status the server returns — never personal data — plus general guidance.
 */
export function DataDeletionStatusPage() {
  const location = useLocation();
  const code = readConfirmationCode(location.search);
  // A different code starts from a clean state.
  return <DataDeletionStatusView code={code} key={code ?? ""} />;
}

function DataDeletionStatusView({ code }: { code: string | null }) {
  const { t } = useI18n();
  const titleId = useId();
  const { reload, state } = useDeletionStatus(code);

  const retry = (
    <Button leadingIcon={<RefreshCw />} onClick={reload} variant="secondary">
      {t("dataDeletion.retry")}
    </Button>
  );

  return (
    <main className="account-link-page data-deletion-page">
      <section
        aria-busy={state.status === "loading" || undefined}
        aria-labelledby={titleId}
        className="account-link-card"
      >
        <p className="account-link-card__kicker">{t("dataDeletion.kicker")}</p>
        <h1 id={titleId}>{t("dataDeletion.title")}</h1>

        <div className="account-link-card__content">
          {state.status === "loading" && (
            <SkeletonGroup label={t("dataDeletion.loading")}>
              <Skeleton height={22} width="60%" />
              <Skeleton height={64} />
            </SkeletonGroup>
          )}

          {state.status === "ready" && code && (
            <div role="status">
              <dl className="data-deletion__status">
                <div>
                  <dt>{t("dataDeletion.statusLabel")}</dt>
                  <dd>
                    <strong>{t(RECEIPT_COPY[state.receipt].title)}</strong>
                  </dd>
                </div>
                <div>
                  <dt>{t("dataDeletion.codeLabel")}</dt>
                  <dd className="data-deletion__code">{code}</dd>
                </div>
              </dl>
              <p className="account-link-card__lead">
                {t(RECEIPT_COPY[state.receipt].description)}
              </p>
            </div>
          )}

          {state.status === "notFound" && (
            <ErrorState
              description={t("dataDeletion.notFoundDescription")}
              title={t("dataDeletion.notFoundTitle")}
            />
          )}

          {state.status === "rateLimited" && (
            <ErrorState
              actions={retry}
              description={
                state.seconds
                  ? t("dataDeletion.rateLimitedWait", {
                      seconds: state.seconds,
                    })
                  : t("dataDeletion.rateLimited")
              }
              title={t("dataDeletion.errorTitle")}
            />
          )}

          {state.status === "busy" && (
            <ErrorState
              actions={retry}
              description={t("dataDeletion.busy")}
              title={t("dataDeletion.errorTitle")}
            />
          )}

          {state.status === "error" && (
            <ErrorState
              actions={retry}
              description={t("dataDeletion.errorDescription")}
              title={t("dataDeletion.errorTitle")}
            />
          )}

          <section
            aria-labelledby={`${titleId}-guide`}
            className="data-deletion__guide"
          >
            <h2 id={`${titleId}-guide`}>{t("dataDeletion.guideTitle")}</h2>
            <h3>{t("dataDeletion.guideFacebookTitle")}</h3>
            <p>{t("dataDeletion.guideFacebook")}</p>
            <p>{t("dataDeletion.guideFacebookEffect")}</p>
            <h3>{t("dataDeletion.guideAccountTitle")}</h3>
            <p>{t("dataDeletion.guideAccount")}</p>
            <p>{t("dataDeletion.guideNote")}</p>
          </section>

          <p>
            <Link className="account-link-card__continue" to="/sign-in">
              {t("dataDeletion.backToSignIn")}
            </Link>
          </p>
        </div>
      </section>
    </main>
  );
}
