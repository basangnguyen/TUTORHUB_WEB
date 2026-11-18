import {
  APIRequestError,
  type MembershipInvitation,
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
import {
  Copy,
  MailPlus,
  RefreshCw,
  ShieldCheck,
  UserMinus,
} from "lucide-react";
import { useMemo, useRef, useState, type FormEvent } from "react";
import {
  useCreateMembershipInvitation,
  useMembershipInvitationList,
  useRevokeMembershipInvitation,
  type InvitableOrganizationRole,
} from "../app/invitations";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  isRecentAuthenticationRequired,
  useStepUpRequest,
  type StepUpRetry,
} from "../app/security";
import type { TenantOperationAvailability } from "../app/tenantCapabilities";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { StepUpAuthDialog } from "./StepUpAuthDialog";
import { TenantOperationNotice } from "./TenantOperationNotice";

type RequestStepUp = (retry?: StepUpRetry) => void;

interface InvitationInput {
  email: string;
  intendedRole: InvitableOrganizationRole;
}

interface MembershipInvitationPanelProps {
  createAvailability: TenantOperationAvailability;
  onRetryCapabilities: () => void;
  tenantID: string;
}

function invitationStatusKey(
  status: MembershipInvitation["status"],
): TranslationKey {
  if (status === "accepted") {
    return "invitation.statusAccepted";
  }
  if (status === "revoked") {
    return "invitation.statusRevoked";
  }
  if (status === "expired") {
    return "invitation.statusExpired";
  }
  return "invitation.statusPending";
}

function invitationStatusTone(status: MembershipInvitation["status"]) {
  if (status === "accepted") {
    return "success" as const;
  }
  if (status === "revoked") {
    return "danger" as const;
  }
  if (status === "expired") {
    return "neutral" as const;
  }
  return "info" as const;
}

function invitationRoleKey(
  role: MembershipInvitation["intended_role"],
): TranslationKey {
  if (role === "teacher") {
    return "shell.role.teacher";
  }
  if (role === "student") {
    return "shell.role.student";
  }
  return "shell.role.guest";
}

function isForbidden(error: Error | null) {
  return error instanceof APIRequestError && error.status === 403;
}

function invitationMutationErrorKey(error: Error | null): TranslationKey {
  if (isRecentAuthenticationRequired(error)) {
    return "invitation.mutationRecentAuth";
  }
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
    return "invitation.mutationForbidden";
  }
  if (error instanceof APIRequestError && error.status === 409) {
    return "invitation.mutationConflict";
  }
  if (error instanceof APIRequestError && error.status === 429) {
    return "invitation.mutationRateLimited";
  }
  return "invitation.mutationError";
}

export function MembershipInvitationPanel({
  createAvailability,
  onRetryCapabilities,
  tenantID,
}: MembershipInvitationPanelProps) {
  const { language, t } = useI18n();
  const invitationList = useMembershipInvitationList(tenantID);
  const invitationDataConcealed = shouldConcealTenantScopedData(
    invitationList.error,
  );
  const revokeInvitation = useRevokeMembershipInvitation();
  const stepUp = useStepUpRequest();
  const requestStepUp = stepUp.request;
  const [revokeTarget, setRevokeTarget] = useState<MembershipInvitation | null>(
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

  const closeRevokeDialog = () => {
    if (!revokeInvitation.isPending) {
      setRevokeTarget(null);
      revokeInvitation.reset();
    }
  };

  const revokeInvitationFor = (target: MembershipInvitation) => {
    setFeedback(null);
    revokeInvitation.mutate(
      { invitationID: target.id, tenantID },
      {
        onError: (error) => {
          // The confirmation dialog stays open underneath the step-up.
          if (isRecentAuthenticationRequired(error)) {
            requestStepUp(() => revokeInvitationFor(target));
          }
        },
        onSuccess: () => {
          setFeedback(t("invitation.revokeSuccess", { email: target.email }));
          setRevokeTarget(null);
        },
      },
    );
  };

  const confirmRevoke = () => {
    if (!revokeTarget) {
      return;
    }
    revokeInvitationFor(revokeTarget);
  };

  if (invitationDataConcealed) {
    return (
      <section
        aria-labelledby="membership-invitations-title"
        className="workspace-management__panel membership-invitations"
      >
        <div className="workspace-management__panel-heading membership-invitations__heading">
          <div>
            <h2 id="membership-invitations-title">
              {t("invitation.adminTitle")}
            </h2>
            <p>{t("invitation.adminDescription")}</p>
          </div>
        </div>
        {isForbidden(invitationList.error) ? (
          <ForbiddenState
            description={t("invitation.listForbiddenDescription")}
            title={t("invitation.listForbiddenTitle")}
          />
        ) : (
          <ErrorState
            actions={
              <Button
                leadingIcon={<RefreshCw />}
                onClick={() => void invitationList.refetch()}
                size="sm"
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("invitation.listErrorDescription")}
            title={t("invitation.listErrorTitle")}
          />
        )}
      </section>
    );
  }

  return (
    <section
      aria-labelledby="membership-invitations-title"
      className="workspace-management__panel membership-invitations"
    >
      <div className="workspace-management__panel-heading membership-invitations__heading">
        <div>
          <h2 id="membership-invitations-title">
            {t("invitation.adminTitle")}
          </h2>
          <p>{t("invitation.adminDescription")}</p>
        </div>
        <CreateInvitationDialog
          availability={createAvailability}
          onRequestStepUp={requestStepUp}
          tenantID={tenantID}
        />
      </div>

      <TenantOperationNotice
        availability={createAvailability}
        onRetry={onRetryCapabilities}
      />

      {feedback && (
        <p className="membership-invitations__success" role="status">
          {feedback}
        </p>
      )}

      {invitationList.isPending && (
        <SkeletonGroup label={t("invitation.listLoading")}>
          <Skeleton height={88} />
          <Skeleton height={88} />
        </SkeletonGroup>
      )}

      {invitationList.isError && (
        <ErrorState
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void invitationList.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={t("invitation.listErrorDescription")}
          title={t("invitation.listErrorTitle")}
        />
      )}

      {invitationList.isSuccess && invitationList.data.items.length === 0 && (
        <EmptyState
          description={t("invitation.listEmptyDescription")}
          title={t("invitation.listEmptyTitle")}
        />
      )}

      {invitationList.data && invitationList.data.items.length > 0 && (
        <ul className="membership-invitations__list">
          {invitationList.data.items.map((invitation) => (
            <li key={invitation.id}>
              <div className="membership-invitations__identity">
                <strong>{invitation.email}</strong>
                <span>{t(invitationRoleKey(invitation.intended_role))}</span>
              </div>
              <div className="membership-invitations__metadata">
                <StatusBadge tone={invitationStatusTone(invitation.status)}>
                  {t(invitationStatusKey(invitation.status))}
                </StatusBadge>
                <span>
                  {t("invitation.expiresLabel")}{" "}
                  <time dateTime={invitation.expires_at}>
                    {dateFormatter.format(new Date(invitation.expires_at))}
                  </time>
                </span>
              </div>
              {invitation.status === "pending" && (
                <Button
                  aria-label={t("invitation.revokeFor", {
                    email: invitation.email,
                  })}
                  leadingIcon={<UserMinus />}
                  onClick={() => {
                    revokeInvitation.reset();
                    setRevokeTarget(invitation);
                  }}
                  size="sm"
                  variant="danger"
                >
                  {t("invitation.revokeAction")}
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (!open) {
            closeRevokeDialog();
          }
        }}
        open={Boolean(revokeTarget)}
      >
        <DialogContent closeLabel={t("invitation.dialogCloseLabel")}>
          <DialogTitle>{t("invitation.revokeConfirmTitle")}</DialogTitle>
          <DialogDescription>
            {t("invitation.revokeConfirmDescription", {
              email: revokeTarget?.email ?? "",
            })}
          </DialogDescription>
          {revokeInvitation.isError && (
            <p className="membership-invitations__error" role="alert">
              {t(invitationMutationErrorKey(revokeInvitation.error))}
            </p>
          )}
          {isRecentAuthenticationRequired(revokeInvitation.error) &&
            revokeTarget && (
              <Button
                leadingIcon={<ShieldCheck />}
                onClick={() => {
                  const target = revokeTarget;
                  requestStepUp(() => revokeInvitationFor(target));
                }}
                size="sm"
                variant="secondary"
              >
                {t("stepUp.open")}
              </Button>
            )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={revokeInvitation.isPending} variant="secondary">
                {t("invitation.cancelAction")}
              </Button>
            </DialogClose>
            <Button
              loading={revokeInvitation.isPending}
              loadingLabel={t("invitation.revoking")}
              onClick={confirmRevoke}
              variant="danger"
            >
              {t("invitation.revokeConfirmAction")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <StepUpAuthDialog {...stepUp.dialog} />
    </section>
  );
}

function CreateInvitationDialog({
  availability,
  onRequestStepUp,
  tenantID,
}: {
  availability: TenantOperationAvailability;
  onRequestStepUp: RequestStepUp;
  tenantID: string;
}) {
  const { t } = useI18n();
  const createInvitation = useCreateMembershipInvitation();
  const [open, setOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [intendedRole, setIntendedRole] =
    useState<InvitableOrganizationRole>("student");
  const [emailError, setEmailError] = useState<string | undefined>();
  const [oneTimeURL, setOneTimeURL] = useState<string | null>(null);
  const [copyStatus, setCopyStatus] = useState<"copied" | "manual" | null>(
    null,
  );
  const linkInput = useRef<HTMLInputElement>(null);
  const lastInputRef = useRef<InvitationInput | null>(null);

  const resetDialog = () => {
    setEmail("");
    setIntendedRole("student");
    setEmailError(undefined);
    setOneTimeURL(null);
    setCopyStatus(null);
    createInvitation.reset();
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!availability.available) {
      return;
    }
    const normalizedEmail = email.trim().toLowerCase();
    if (
      normalizedEmail.length > 320 ||
      !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalizedEmail)
    ) {
      setEmailError(t("invitation.emailValidation"));
      return;
    }
    setEmailError(undefined);
    await createWith({ email: normalizedEmail, intendedRole });
  };

  /** One validated request; a step-up re-sends it without retyping. */
  async function createWith(input: InvitationInput) {
    setCopyStatus(null);
    lastInputRef.current = input;
    try {
      const response = await createInvitation.mutateAsync({
        ...input,
        tenantID,
      });
      setOneTimeURL(response.accept_url);
      createInvitation.reset();
    } catch (error) {
      // The mutation exposes its structured error in the dialog.
      if (isRecentAuthenticationRequired(error)) {
        onRequestStepUp(() => createWith(input));
      }
    }
  }

  const copyLink = async () => {
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
        if (createInvitation.isPending) {
          return;
        }
        setOpen(nextOpen);
        if (!nextOpen) {
          resetDialog();
        }
      }}
      open={open}
    >
      <DialogTrigger asChild>
        <Button
          disabled={!availability.available}
          leadingIcon={<MailPlus />}
          size="sm"
        >
          {t("invitation.createAction")}
        </Button>
      </DialogTrigger>
      <DialogContent closeLabel={t("invitation.dialogCloseLabel")}>
        <DialogTitle>{t("invitation.createTitle")}</DialogTitle>
        <DialogDescription>
          {t("invitation.createDescription")}
        </DialogDescription>

        {oneTimeURL ? (
          <div className="membership-invitations__one-time-link">
            <p role="status">{t("invitation.createSuccess")}</p>
            <TextField
              label={t("invitation.acceptURLLabel")}
              readOnly
              ref={linkInput}
              value={oneTimeURL}
            />
            <Button leadingIcon={<Copy />} onClick={() => void copyLink()}>
              {t("invitation.copyAction")}
            </Button>
            {copyStatus && (
              <p role="status">
                {t(
                  copyStatus === "copied"
                    ? "invitation.copySuccess"
                    : "invitation.copyManual",
                )}
              </p>
            )}
          </div>
        ) : (
          <form className="membership-invitations__form" onSubmit={submit}>
            <TextField
              autoCapitalize="none"
              autoComplete="email"
              error={emailError}
              label={t("invitation.emailLabel")}
              maxLength={320}
              onChange={(event) => setEmail(event.target.value)}
              required
              spellCheck={false}
              type="email"
              value={email}
            />
            <SelectField
              ariaLabel={t("invitation.roleLabel")}
              label={t("invitation.roleLabel")}
              onValueChange={(value) =>
                setIntendedRole(value as InvitableOrganizationRole)
              }
              options={[
                { label: t("shell.role.teacher"), value: "teacher" },
                { label: t("shell.role.student"), value: "student" },
                { label: t("shell.role.guest"), value: "guest" },
              ]}
              value={intendedRole}
            />
            {createInvitation.isError && (
              <p className="membership-invitations__error" role="alert">
                {t(invitationMutationErrorKey(createInvitation.error))}
              </p>
            )}
            {isRecentAuthenticationRequired(createInvitation.error) && (
              <Button
                leadingIcon={<ShieldCheck />}
                onClick={() => {
                  const input = lastInputRef.current;
                  onRequestStepUp(input ? () => createWith(input) : undefined);
                }}
                size="sm"
                variant="secondary"
              >
                {t("stepUp.open")}
              </Button>
            )}
            <DialogFooter>
              <DialogClose asChild>
                <Button
                  disabled={createInvitation.isPending}
                  variant="secondary"
                >
                  {t("invitation.cancelAction")}
                </Button>
              </DialogClose>
              <Button
                disabled={!availability.available}
                loading={createInvitation.isPending}
                loadingLabel={t("invitation.creating")}
                type="submit"
              >
                {t("invitation.createConfirmAction")}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
