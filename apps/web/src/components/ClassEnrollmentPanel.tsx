import {
  APIRequestError,
  type ClassInviteCode,
  type ClassroomClass,
} from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
  EmptyState,
  ErrorState,
  ForbiddenState,
  SelectField,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import { Copy, Link2, RefreshCw, UserPlus, XCircle } from "lucide-react";
import { useMemo, useRef, useState, type FormEvent } from "react";
import {
  useClassInviteCodes,
  useCreateClassInviteCode,
  useDirectClassEnrollment,
  useRevokeClassInviteCode,
} from "../app/classEnrollments";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import type { TenantOperationAvailability } from "../app/tenantCapabilities";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { TenantOperationNotice } from "./TenantOperationNotice";

function isForbidden(error: Error | null) {
  return error instanceof APIRequestError && error.status === 403;
}

function mutationErrorKey(error: Error | null): TranslationKey {
  if (
    error instanceof APIRequestError &&
    error.problem?.code === "feature_disabled"
  ) {
    return "capabilities.reasonFeatureDisabled";
  }
  if (
    error instanceof APIRequestError &&
    error.problem?.code === "quota_exceeded"
  ) {
    return error.status === 429
      ? "capabilities.reasonRateLimited"
      : "capabilities.reasonQuotaExhausted";
  }
  if (error instanceof APIRequestError && error.status === 403) {
    return "classEnrollment.mutationForbidden";
  }
  if (error instanceof APIRequestError && error.status === 404) {
    return "classEnrollment.mutationNotFound";
  }
  if (error instanceof APIRequestError && error.status === 409) {
    return "classEnrollment.mutationConflict";
  }
  if (error instanceof APIRequestError && error.status === 429) {
    return "classEnrollment.mutationRateLimited";
  }
  return "classEnrollment.mutationError";
}

function inviteStatusKey(status: ClassInviteCode["status"]): TranslationKey {
  if (status === "exhausted") {
    return "classEnrollment.statusExhausted";
  }
  if (status === "expired") {
    return "classEnrollment.statusExpired";
  }
  if (status === "revoked") {
    return "classEnrollment.statusRevoked";
  }
  return "classEnrollment.statusActive";
}

function inviteStatusTone(status: ClassInviteCode["status"]) {
  if (status === "active") {
    return "info" as const;
  }
  if (status === "revoked") {
    return "danger" as const;
  }
  return "neutral" as const;
}

export function ClassEnrollmentPanel({
  classroom,
  createInviteAvailability,
  onRetryCapabilities,
}: {
  classroom: ClassroomClass;
  createInviteAvailability: TenantOperationAvailability;
  onRetryCapabilities: () => void;
}) {
  const { language, t } = useI18n();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const canManage = classroom.viewer_access.can_manage_enrollments;
  const inviteCodes = useClassInviteCodes(tenantID, classroom.id, canManage);
  const inviteCodeDataConcealed = shouldConcealTenantScopedData(
    inviteCodes.error,
  );
  const revokeInviteCode = useRevokeClassInviteCode();
  const [revokeTarget, setRevokeTarget] = useState<ClassInviteCode | null>(
    null,
  );
  const [feedback, setFeedback] = useState<string | null>(null);
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  if (!canManage || !tenantID) {
    return null;
  }

  const revoke = () => {
    if (!revokeTarget) {
      return;
    }
    setFeedback(null);
    revokeInviteCode.mutate(
      {
        classID: classroom.id,
        codeID: revokeTarget.id,
        tenantID,
      },
      {
        onSuccess: () => {
          setFeedback(t("classEnrollment.revokeSuccess"));
          setRevokeTarget(null);
        },
      },
    );
  };

  return (
    <section
      aria-labelledby="class-enrollment-title"
      className="classroom-detail__section class-enrollments"
    >
      <div className="class-enrollments__heading">
        <div>
          <h2 id="class-enrollment-title">{t("classEnrollment.title")}</h2>
          <p>{t("classEnrollment.description")}</p>
        </div>
        {!inviteCodeDataConcealed && (
          <CreateInviteCodeDialog
            classroom={classroom}
            createAvailability={createInviteAvailability}
            tenantID={tenantID}
          />
        )}
      </div>

      {!inviteCodeDataConcealed && classroom.status !== "active" && (
        <p className="class-enrollments__notice">
          {t("classEnrollment.inactiveDescription")}
        </p>
      )}

      {!inviteCodeDataConcealed && classroom.status === "active" && (
        <TenantOperationNotice
          availability={createInviteAvailability}
          label={t("capabilities.operationCreateClassInvite")}
          onRetry={onRetryCapabilities}
        />
      )}

      {!inviteCodeDataConcealed && (
        <DirectEnrollmentForm
          classroom={classroom}
          onFeedback={setFeedback}
          tenantID={tenantID}
        />
      )}

      {!inviteCodeDataConcealed && (
        <div className="class-enrollments__subheading">
          <div>
            <h3>{t("classEnrollment.inviteTitle")}</h3>
            <p>{t("classEnrollment.inviteDescription")}</p>
          </div>
        </div>
      )}

      {!inviteCodeDataConcealed && feedback && (
        <p className="class-enrollments__success" role="status">
          {feedback}
        </p>
      )}

      {inviteCodes.isPending && (
        <SkeletonGroup label={t("classEnrollment.listLoading")}>
          <Skeleton height={86} />
          <Skeleton height={86} />
        </SkeletonGroup>
      )}

      {inviteCodes.isError &&
        (isForbidden(inviteCodes.error) ? (
          <ForbiddenState
            description={t("classEnrollment.listForbiddenDescription")}
            title={t("classEnrollment.listForbiddenTitle")}
          />
        ) : (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void inviteCodes.refetch()}
                size="sm"
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("classEnrollment.listErrorDescription")}
            title={t("classEnrollment.listErrorTitle")}
          />
        ))}

      {!inviteCodeDataConcealed &&
        inviteCodes.isSuccess &&
        inviteCodes.data.items.length === 0 && (
          <EmptyState
            description={t("classEnrollment.listEmptyDescription")}
            title={t("classEnrollment.listEmptyTitle")}
          />
        )}

      {!inviteCodeDataConcealed &&
        inviteCodes.data &&
        inviteCodes.data.items.length > 0 && (
          <ul className="class-enrollments__list">
            {inviteCodes.data.items.map((code) => (
              <li key={code.id}>
                <div>
                  <StatusBadge tone={inviteStatusTone(code.status)}>
                    {t(inviteStatusKey(code.status))}
                  </StatusBadge>
                  <span>
                    {t("classEnrollment.usageCount", {
                      used: code.usage_count,
                      limit: code.usage_limit,
                    })}
                  </span>
                </div>
                <span>
                  {t("classEnrollment.expiresLabel")}{" "}
                  <time dateTime={code.expires_at}>
                    {dateFormatter.format(new Date(code.expires_at))}
                  </time>
                </span>
                {code.status === "active" && (
                  <Button
                    aria-label={t("classEnrollment.revokeCodeAction", {
                      expires: dateFormatter.format(new Date(code.expires_at)),
                    })}
                    leadingIcon={<XCircle />}
                    onClick={() => {
                      revokeInviteCode.reset();
                      setRevokeTarget(code);
                    }}
                    size="sm"
                    variant="danger"
                  >
                    {t("classEnrollment.revokeAction")}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}

      {!inviteCodeDataConcealed && (
        <Dialog
          onOpenChange={(open) => {
            if (!open && !revokeInviteCode.isPending) {
              setRevokeTarget(null);
              revokeInviteCode.reset();
            }
          }}
          open={Boolean(revokeTarget)}
        >
          <DialogContent closeLabel={t("classEnrollment.closeDialog")}>
            <DialogTitle>{t("classEnrollment.revokeConfirmTitle")}</DialogTitle>
            <DialogDescription>
              {t("classEnrollment.revokeConfirmDescription")}
            </DialogDescription>
            {revokeInviteCode.isError && (
              <p className="class-enrollments__error" role="alert">
                {t(mutationErrorKey(revokeInviteCode.error))}
              </p>
            )}
            <DialogFooter>
              <DialogClose asChild>
                <Button
                  disabled={revokeInviteCode.isPending}
                  variant="secondary"
                >
                  {t("classEnrollment.cancelAction")}
                </Button>
              </DialogClose>
              <Button
                loading={revokeInviteCode.isPending}
                loadingLabel={t("classEnrollment.revoking")}
                onClick={revoke}
                variant="danger"
              >
                {t("classEnrollment.revokeConfirm")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </section>
  );
}

function DirectEnrollmentForm({
  classroom,
  onFeedback,
  tenantID,
}: {
  classroom: ClassroomClass;
  onFeedback: (message: string | null) => void;
  tenantID: string;
}) {
  const { t } = useI18n();
  const directEnrollment = useDirectClassEnrollment(tenantID);
  const [email, setEmail] = useState("");
  const [emailError, setEmailError] = useState<string | undefined>();

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalizedEmail = email.trim().toLowerCase();
    if (
      normalizedEmail.length > 320 ||
      !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)
    ) {
      setEmailError(t("classEnrollment.emailValidation"));
      return;
    }
    setEmailError(undefined);
    onFeedback(null);
    directEnrollment.mutate(
      { classID: classroom.id, memberEmail: normalizedEmail },
      {
        onSuccess: () => {
          setEmail("");
          onFeedback(t("classEnrollment.enrollSuccess"));
        },
      },
    );
  };

  return (
    <div className="class-enrollments__direct">
      <div>
        <h3>{t("classEnrollment.directTitle")}</h3>
        <p>{t("classEnrollment.directDescription")}</p>
      </div>
      <form onSubmit={submit}>
        <TextField
          autoCapitalize="none"
          autoComplete="email"
          disabled={classroom.status !== "active"}
          error={emailError}
          label={t("classEnrollment.emailLabel")}
          maxLength={320}
          onChange={(event) => setEmail(event.target.value)}
          required
          spellCheck={false}
          type="email"
          value={email}
        />
        <Button
          disabled={classroom.status !== "active"}
          leadingIcon={<UserPlus />}
          loading={directEnrollment.isPending}
          loadingLabel={t("classEnrollment.enrolling")}
          type="submit"
        >
          {t("classEnrollment.enrollAction")}
        </Button>
      </form>
      {directEnrollment.isError && (
        <p className="class-enrollments__error" role="alert">
          {t(mutationErrorKey(directEnrollment.error))}
        </p>
      )}
    </div>
  );
}

function CreateInviteCodeDialog({
  classroom,
  createAvailability,
  tenantID,
}: {
  classroom: ClassroomClass;
  createAvailability: TenantOperationAvailability;
  tenantID: string;
}) {
  const { t } = useI18n();
  const createInviteCode = useCreateClassInviteCode();
  const [open, setOpen] = useState(false);
  const [expiresInSeconds, setExpiresInSeconds] = useState("604800");
  const [usageLimit, setUsageLimit] = useState("30");
  const [usageError, setUsageError] = useState<string | undefined>();
  const [oneTimeURL, setOneTimeURL] = useState<string | null>(null);
  const [copyStatus, setCopyStatus] = useState<"copied" | "manual" | null>(
    null,
  );
  const linkInput = useRef<HTMLInputElement>(null);

  const reset = () => {
    setExpiresInSeconds("604800");
    setUsageLimit("30");
    setUsageError(undefined);
    setOneTimeURL(null);
    setCopyStatus(null);
    createInviteCode.reset();
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!createAvailability.available) {
      return;
    }
    const limit = Number(usageLimit);
    if (!Number.isInteger(limit) || limit < 1 || limit > 1000) {
      setUsageError(t("classEnrollment.usageValidation"));
      return;
    }
    setUsageError(undefined);
    setCopyStatus(null);
    try {
      const response = await createInviteCode.mutateAsync({
        classID: classroom.id,
        input: {
          expires_in_seconds: Number(expiresInSeconds),
          usage_limit: limit,
        },
        tenantID,
      });
      setOneTimeURL(response.join_url);
      createInviteCode.reset();
    } catch {
      // The mutation exposes its structured error in the dialog.
    }
  };

  const copy = async () => {
    if (!oneTimeURL) {
      return;
    }
    try {
      if (!navigator.clipboard?.writeText) {
        throw new Error("Clipboard API unavailable");
      }
      await navigator.clipboard.writeText(oneTimeURL);
      setCopyStatus("copied");
    } catch {
      linkInput.current?.focus();
      linkInput.current?.select();
      setCopyStatus("manual");
    }
  };

  return (
    <Dialog
      onOpenChange={(nextOpen) => {
        if (createInviteCode.isPending) {
          return;
        }
        setOpen(nextOpen);
        if (!nextOpen) {
          reset();
        }
      }}
      open={open}
    >
      <DialogTrigger asChild>
        <Button
          disabled={
            classroom.status !== "active" || !createAvailability.available
          }
          leadingIcon={<Link2 />}
          size="sm"
        >
          {t("classEnrollment.createAction")}
        </Button>
      </DialogTrigger>
      <DialogContent closeLabel={t("classEnrollment.closeDialog")}>
        <DialogTitle>{t("classEnrollment.createTitle")}</DialogTitle>
        <DialogDescription>
          {t("classEnrollment.createDescription")}
        </DialogDescription>

        {oneTimeURL ? (
          <div className="class-enrollments__one-time-link">
            <p role="status">{t("classEnrollment.createSuccess")}</p>
            <TextField
              label={t("classEnrollment.linkLabel")}
              readOnly
              ref={linkInput}
              value={oneTimeURL}
            />
            <Button leadingIcon={<Copy />} onClick={() => void copy()}>
              {t("classEnrollment.copyAction")}
            </Button>
            {copyStatus && (
              <p role="status">
                {t(
                  copyStatus === "copied"
                    ? "classEnrollment.copySuccess"
                    : "classEnrollment.copyManual",
                )}
              </p>
            )}
          </div>
        ) : (
          <form className="class-enrollments__create-form" onSubmit={submit}>
            <SelectField
              ariaLabel={t("classEnrollment.ttlLabel")}
              label={t("classEnrollment.ttlLabel")}
              onValueChange={setExpiresInSeconds}
              options={[
                {
                  label: t("classEnrollment.ttlOneDay"),
                  value: "86400",
                },
                {
                  label: t("classEnrollment.ttlSevenDays"),
                  value: "604800",
                },
                {
                  label: t("classEnrollment.ttlThirtyDays"),
                  value: "2592000",
                },
              ]}
              value={expiresInSeconds}
            />
            <TextField
              error={usageError}
              inputMode="numeric"
              label={t("classEnrollment.usageLabel")}
              max={1000}
              min={1}
              onChange={(event) => setUsageLimit(event.target.value)}
              required
              type="number"
              value={usageLimit}
            />
            {createInviteCode.isError && (
              <p className="class-enrollments__error" role="alert">
                {t(mutationErrorKey(createInviteCode.error))}
              </p>
            )}
            <DialogFooter>
              <DialogClose asChild>
                <Button
                  disabled={createInviteCode.isPending}
                  variant="secondary"
                >
                  {t("classEnrollment.cancelAction")}
                </Button>
              </DialogClose>
              <Button
                disabled={!createAvailability.available}
                loading={createInviteCode.isPending}
                loadingLabel={t("classEnrollment.creating")}
                type="submit"
              >
                {t("classEnrollment.createConfirm")}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
