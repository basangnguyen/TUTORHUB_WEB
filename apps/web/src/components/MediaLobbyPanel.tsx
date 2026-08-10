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
} from "@tutorhub/ui";
import { Check, RefreshCw, UserRoundX } from "lucide-react";
import { useRef, useState } from "react";
import {
  mediaLobbyIdempotencyKey,
  useMediaAdmissions,
  useResolveMediaAdmission,
  type MediaAdmissionItem,
} from "../app/mediaLobby";
import { useI18n } from "../app/i18n";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";

interface MediaLobbyPanelProps {
  enabled: boolean;
  roomInstanceID: string;
  roomInstanceVersion: number;
  spaceID: string;
  spaceVersion: number;
  tenantID: string;
}

export function MediaLobbyPanel({
  enabled,
  roomInstanceID,
  roomInstanceVersion,
  spaceID,
  spaceVersion,
  tenantID,
}: MediaLobbyPanelProps) {
  const { t } = useI18n();
  const heading = useRef<HTMLHeadingElement>(null);
  const idempotencyKeys = useRef(new Map<string, string>());
  const [denyTarget, setDenyTarget] = useState<MediaAdmissionItem | null>(null);
  const [feedback, setFeedback] = useState<{
    action: "admit" | "deny";
    displayName: string;
  } | null>(null);
  const admissions = useMediaAdmissions(
    tenantID,
    spaceID,
    roomInstanceID,
    spaceVersion,
    roomInstanceVersion,
    enabled,
  );
  const resolve = useResolveMediaAdmission(tenantID, spaceID, roomInstanceID);

  if (!enabled) {
    return null;
  }

  const commandForbidden =
    resolve.error instanceof APIRequestError && resolve.error.status === 403;
  const concealed =
    shouldConcealTenantScopedData(admissions.error) || commandForbidden;
  const forbidden =
    admissions.error instanceof APIRequestError &&
    admissions.error.status === 403;
  const waiting = (admissions.data?.items ?? []).filter(
    (item) => item.status === "waiting",
  );
  const actionPending = (admissionID: string) =>
    resolve.isPending && resolve.variables?.admissionID === admissionID;

  const command = (action: "admit" | "deny", item: MediaAdmissionItem) => {
    const commandKey = `${action}:${item.id}:${item.version}`;
    let idempotencyKey = idempotencyKeys.current.get(commandKey);
    if (!idempotencyKey) {
      idempotencyKey = mediaLobbyIdempotencyKey(`admission-${action}`);
      idempotencyKeys.current.set(commandKey, idempotencyKey);
    }
    resolve.mutate(
      {
        action,
        admissionID: item.id,
        input: {
          expected_space_version: spaceVersion,
          expected_room_instance_id: roomInstanceID,
          expected_room_instance_version: roomInstanceVersion,
          expected_admission_version: item.version,
          idempotency_key: idempotencyKey,
          ...(action === "deny" ? { reason_code: "moderator_denied" } : {}),
        },
      },
      {
        onSuccess: () => {
          idempotencyKeys.current.delete(commandKey);
          setDenyTarget(null);
          setFeedback({ action, displayName: item.display_name });
          globalThis.requestAnimationFrame(() => heading.current?.focus());
        },
      },
    );
  };

  return (
    <section aria-labelledby="media-lobby-title" className="media-p404-lobby">
      <div className="media-p404-panel-heading">
        <div>
          <h2 id="media-lobby-title" ref={heading} tabIndex={-1}>
            {t("media.p404.lobby.title")}
          </h2>
          <p>{t("media.p404.lobby.description")}</p>
        </div>
        <Button
          disabled={admissions.isPending}
          leadingIcon={<RefreshCw />}
          loading={admissions.isRefetching}
          loadingLabel={t("media.p404.refreshing")}
          onClick={() => void admissions.refetch()}
          size="sm"
          variant="secondary"
        >
          {t("media.p404.refresh")}
        </Button>
      </div>

      {feedback && !concealed && (
        <p className="media-p404-feedback" role="status">
          {t(
            feedback.action === "admit"
              ? "media.p404.lobby.admitted"
              : "media.p404.lobby.denied",
            { name: feedback.displayName },
          )}
        </p>
      )}

      {admissions.isPending && (
        <SkeletonGroup label={t("media.p404.lobby.loading")}>
          <Skeleton height={72} />
          <Skeleton height={72} />
        </SkeletonGroup>
      )}

      {(admissions.isError || commandForbidden) &&
        (concealed || forbidden ? (
          <ForbiddenState
            actions={
              <Button
                onClick={() => void admissions.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("media.p404.lobby.forbiddenDescription")}
            title={t("media.p404.lobby.forbiddenTitle")}
          />
        ) : (
          <ErrorState
            actions={
              <Button
                onClick={() => void admissions.refetch()}
                variant="secondary"
              >
                {t("state.retry")}
              </Button>
            }
            description={t("media.p404.lobby.errorDescription")}
            title={t("media.p404.lobby.errorTitle")}
          />
        ))}

      {admissions.isSuccess && !concealed && waiting.length === 0 && (
        <EmptyState
          description={t("media.p404.lobby.emptyDescription")}
          title={t("media.p404.lobby.emptyTitle")}
        />
      )}

      {!concealed && waiting.length > 0 && (
        <ul
          aria-label={t("media.p404.lobby.listLabel")}
          className="media-p404-list"
        >
          {waiting.map((item) => (
            <li key={item.id}>
              <span className="media-p404-identity">
                <strong>{item.display_name}</strong>
                <StatusBadge tone="warning">
                  {t("media.p404.lobby.waiting")}
                </StatusBadge>
              </span>
              <span className="media-p404-row-actions">
                <Button
                  aria-label={t("media.p404.lobby.admitNamed", {
                    name: item.display_name,
                  })}
                  disabled={resolve.isPending}
                  leadingIcon={<Check />}
                  loading={actionPending(item.id)}
                  loadingLabel={t("media.p404.lobby.resolving")}
                  onClick={() => command("admit", item)}
                  size="sm"
                >
                  {t("media.p404.lobby.admit")}
                </Button>
                <Button
                  aria-label={t("media.p404.lobby.denyNamed", {
                    name: item.display_name,
                  })}
                  disabled={resolve.isPending}
                  leadingIcon={<UserRoundX />}
                  onClick={() => {
                    resolve.reset();
                    setDenyTarget(item);
                  }}
                  size="sm"
                  variant="danger"
                >
                  {t("media.p404.lobby.deny")}
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}

      {resolve.isError && !denyTarget && (
        <div className="media-p404-error" role="alert">
          <p>{t(mediaLobbyMutationErrorKey(resolve.error))}</p>
          <Button
            onClick={() => {
              resolve.reset();
              void admissions.refetch();
            }}
            size="sm"
            variant="secondary"
          >
            {t("media.p404.reload")}
          </Button>
        </div>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (!open && !resolve.isPending) {
            setDenyTarget(null);
            resolve.reset();
          }
        }}
        open={Boolean(denyTarget && !concealed)}
      >
        <DialogContent closeLabel={t("media.p404.closeDialog")}>
          <DialogTitle>{t("media.p404.lobby.denyTitle")}</DialogTitle>
          <DialogDescription>
            {t("media.p404.lobby.denyDescription", {
              name: denyTarget?.display_name ?? "",
            })}
          </DialogDescription>
          {resolve.isError && (
            <p className="media-p404-error" role="alert">
              {t(mediaLobbyMutationErrorKey(resolve.error))}
            </p>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={resolve.isPending} variant="secondary">
                {t("media.p404.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={!denyTarget}
              loading={resolve.isPending}
              loadingLabel={t("media.p404.lobby.resolving")}
              onClick={() => {
                if (denyTarget) command("deny", denyTarget);
              }}
              variant="danger"
            >
              {t("media.p404.lobby.confirmDeny")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function mediaLobbyMutationErrorKey(error: unknown) {
  if (error instanceof APIRequestError) {
    if (error.status === 409) return "media.p404.conflict" as const;
    if (error.status === 429) return "media.p404.rateLimited" as const;
    if (error.status === 503) return "media.p404.unavailable" as const;
  }
  return "media.p404.commandError" as const;
}
