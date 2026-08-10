import { APIRequestError } from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  EmptyState,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextField,
} from "@tutorhub/ui";
import { RefreshCw, RotateCcw, UserPlus, UserRoundX } from "lucide-react";
import { useRef, useState, type FormEvent } from "react";
import {
  mediaLobbyIdempotencyKey,
  useInviteMediaSpaceMember,
  useMediaSpaceMembers,
  useMutateMediaSpaceMember,
  type MediaSpaceMemberItem,
} from "../app/mediaLobby";
import { useI18n } from "../app/i18n";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";

interface MediaSpaceInvitePanelProps {
  enabled: boolean;
  spaceID: string;
  spaceVersion: number;
  tenantID: string;
}

export function MediaSpaceInvitePanel({
  enabled,
  spaceID,
  spaceVersion,
  tenantID,
}: MediaSpaceInvitePanelProps) {
  const { t } = useI18n();
  const [email, setEmail] = useState("");
  const [emailError, setEmailError] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<MediaSpaceMemberItem | null>(
    null,
  );
  const inviteKey = useRef<string | null>(null);
  const lifecycleKeys = useRef(new Map<string, string>());
  const members = useMediaSpaceMembers(
    tenantID,
    spaceID,
    spaceVersion,
    enabled,
  );
  const invite = useInviteMediaSpaceMember(tenantID, spaceID);
  const mutate = useMutateMediaSpaceMember(tenantID, spaceID);

  if (!enabled) {
    return null;
  }

  const commandForbidden =
    (invite.error instanceof APIRequestError && invite.error.status === 403) ||
    (mutate.error instanceof APIRequestError && mutate.error.status === 403);
  const concealed =
    shouldConcealTenantScopedData(members.error) || commandForbidden;
  const forbidden =
    (members.error instanceof APIRequestError &&
      members.error.status === 403) ||
    (invite.error instanceof APIRequestError && invite.error.status === 403) ||
    (mutate.error instanceof APIRequestError && mutate.error.status === 403);
  const items = [...(members.data?.items ?? [])].sort((left, right) =>
    left.display_name.localeCompare(right.display_name),
  );

  const submitInvite = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalized = email.trim().toLowerCase();
    if (!validMemberEmail(normalized)) {
      setEmailError(true);
      return;
    }
    setEmailError(false);
    setFeedback(null);
    inviteKey.current ??= mediaLobbyIdempotencyKey("member-invite");
    invite.mutate(
      {
        target_member_email: normalized,
        expected_space_version: spaceVersion,
        idempotency_key: inviteKey.current,
      },
      {
        onSuccess: (member) => {
          inviteKey.current = null;
          setEmail("");
          setFeedback(
            t("media.p404.invites.invited", { name: member.display_name }),
          );
        },
      },
    );
  };

  const mutateMember = (
    action: "revoke" | "restore",
    member: MediaSpaceMemberItem,
  ) => {
    const commandKey = `${action}:${member.user_id}:${member.version}`;
    let idempotencyKey = lifecycleKeys.current.get(commandKey);
    if (!idempotencyKey) {
      idempotencyKey = mediaLobbyIdempotencyKey(`member-${action}`);
      lifecycleKeys.current.set(commandKey, idempotencyKey);
    }
    mutate.mutate(
      {
        action,
        userID: member.user_id,
        input: {
          expected_member_version: member.version,
          expected_space_version: spaceVersion,
          idempotency_key: idempotencyKey,
          reason_code: action === "revoke" ? "owner_revoked" : "owner_restored",
        },
      },
      {
        onSuccess: (updated) => {
          lifecycleKeys.current.delete(commandKey);
          setRevokeTarget(null);
          setFeedback(
            t(
              action === "revoke"
                ? "media.p404.invites.revoked"
                : "media.p404.invites.restored",
              { name: updated.display_name },
            ),
          );
        },
      },
    );
  };

  return (
    <section
      aria-labelledby="media-space-invites-title"
      className="media-p404-invites"
    >
      <div className="media-p404-panel-heading">
        <div>
          <h2 id="media-space-invites-title">
            {t("media.p404.invites.title")}
          </h2>
          <p>{t("media.p404.invites.description")}</p>
        </div>
        <Button
          leadingIcon={<RefreshCw />}
          loading={members.isRefetching}
          loadingLabel={t("media.p404.refreshing")}
          onClick={() => void members.refetch()}
          size="sm"
          variant="secondary"
        >
          {t("media.p404.refresh")}
        </Button>
      </div>

      {!concealed && (
        <form className="media-p404-invite-form" onSubmit={submitInvite}>
          <TextField
            autoComplete="off"
            error={
              emailError ? t("media.p404.invites.invalidEmail") : undefined
            }
            label={t("media.p404.invites.emailLabel")}
            maxLength={320}
            onChange={(event) => {
              setEmail(event.target.value);
              setEmailError(false);
              setFeedback(null);
              invite.reset();
              inviteKey.current = null;
            }}
            placeholder={t("media.p404.invites.emailPlaceholder")}
            type="email"
            value={email}
          />
          <Button
            disabled={!email.trim()}
            leadingIcon={<UserPlus />}
            loading={invite.isPending}
            loadingLabel={t("media.p404.invites.inviting")}
            type="submit"
          >
            {t("media.p404.invites.invite")}
          </Button>
        </form>
      )}

      {feedback && (
        <p className="media-p404-feedback" role="status">
          {feedback}
        </p>
      )}

      {invite.isError && !concealed && (
        <div className="media-p404-error" role="alert">
          <p>{t(mediaMemberMutationErrorKey(invite.error))}</p>
          {invite.error instanceof APIRequestError &&
            invite.error.status === 409 && (
              <Button
                onClick={() => void members.refetch()}
                size="sm"
                variant="secondary"
              >
                {t("media.p404.reload")}
              </Button>
            )}
        </div>
      )}

      {members.isPending && (
        <SkeletonGroup label={t("media.p404.invites.loading")}>
          <Skeleton height={72} />
          <Skeleton height={72} />
        </SkeletonGroup>
      )}

      {(members.isError || commandForbidden) &&
        (concealed || forbidden ? (
          <ForbiddenState
            actions={
              <Button
                onClick={() => void members.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("media.p404.invites.forbiddenDescription")}
            title={t("media.p404.invites.forbiddenTitle")}
          />
        ) : (
          <ErrorState
            actions={
              <Button
                onClick={() => void members.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("media.p404.invites.errorDescription")}
            title={t("media.p404.invites.errorTitle")}
          />
        ))}

      {members.isSuccess && !concealed && items.length === 0 && (
        <EmptyState
          description={t("media.p404.invites.emptyDescription")}
          title={t("media.p404.invites.emptyTitle")}
        />
      )}

      {!concealed && items.length > 0 && (
        <ul
          aria-label={t("media.p404.invites.listLabel")}
          className="media-p404-list"
        >
          {items.map((member) => (
            <li key={member.user_id}>
              <span className="media-p404-identity">
                <strong>{member.display_name}</strong>
                <StatusBadge
                  tone={member.status === "active" ? "success" : "neutral"}
                >
                  {t(
                    member.status === "active"
                      ? "media.p404.invites.active"
                      : "media.p404.invites.revokedStatus",
                  )}
                </StatusBadge>
              </span>
              <span className="media-p404-row-actions">
                {member.status === "active" ? (
                  <Button
                    aria-label={t("media.p404.invites.revokeNamed", {
                      name: member.display_name,
                    })}
                    disabled={mutate.isPending}
                    leadingIcon={<UserRoundX />}
                    onClick={() => {
                      mutate.reset();
                      setRevokeTarget(member);
                    }}
                    size="sm"
                    variant="danger"
                  >
                    {t("media.p404.invites.revoke")}
                  </Button>
                ) : (
                  <Button
                    aria-label={t("media.p404.invites.restoreNamed", {
                      name: member.display_name,
                    })}
                    disabled={mutate.isPending}
                    leadingIcon={<RotateCcw />}
                    loading={
                      mutate.isPending &&
                      mutate.variables?.userID === member.user_id
                    }
                    loadingLabel={t("media.p404.invites.updating")}
                    onClick={() => mutateMember("restore", member)}
                    size="sm"
                    variant="secondary"
                  >
                    {t("media.p404.invites.restore")}
                  </Button>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}

      {mutate.isError && !revokeTarget && !concealed && (
        <div className="media-p404-error" role="alert">
          <p>{t(mediaMemberMutationErrorKey(mutate.error))}</p>
          <Button
            onClick={() => void members.refetch()}
            size="sm"
            variant="secondary"
          >
            {t("media.p404.reload")}
          </Button>
        </div>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (!open && !mutate.isPending) {
            setRevokeTarget(null);
            mutate.reset();
          }
        }}
        open={Boolean(revokeTarget && !concealed)}
      >
        <DialogContent closeLabel={t("media.p404.closeDialog")}>
          <DialogTitle>{t("media.p404.invites.revokeTitle")}</DialogTitle>
          <DialogDescription>
            {t("media.p404.invites.revokeDescription", {
              name: revokeTarget?.display_name ?? "",
            })}
          </DialogDescription>
          {mutate.isError && (
            <p className="media-p404-error" role="alert">
              {t(mediaMemberMutationErrorKey(mutate.error))}
            </p>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={mutate.isPending} variant="secondary">
                {t("media.p404.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={!revokeTarget}
              loading={mutate.isPending}
              loadingLabel={t("media.p404.invites.updating")}
              onClick={() => {
                if (revokeTarget) mutateMember("revoke", revokeTarget);
              }}
              variant="danger"
            >
              {t("media.p404.invites.confirmRevoke")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function validMemberEmail(value: string): boolean {
  return (
    value.length >= 3 &&
    value.length <= 320 &&
    /^[^\s@]+@[^\s@]+\.[^\s@]+$/u.test(value)
  );
}

function mediaMemberMutationErrorKey(error: unknown) {
  if (error instanceof APIRequestError) {
    if (error.status === 404)
      return "media.p404.invites.memberUnavailable" as const;
    if (error.status === 409) return "media.p404.conflict" as const;
    if (error.status === 429) return "media.p404.rateLimited" as const;
    if (error.status === 503) return "media.p404.unavailable" as const;
  }
  return "media.p404.commandError" as const;
}
