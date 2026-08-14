import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  IconButton,
  Menu,
  MenuContent,
  MenuItem,
  MenuTrigger,
} from "@tutorhub/ui";
import {
  Lock,
  LogOut,
  MicOff,
  MoreHorizontal,
  Unlock,
  UserMinus,
  UserPlus,
  UserRoundX,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useI18n, type TranslationKey } from "../../app/i18n";

export type ClassroomModerationAction =
  | "lock_room"
  | "unlock_room"
  | "end_room"
  | "promote_co_host"
  | "demote_co_host"
  | "remote_mute"
  | "remove_participant";

export type ClassroomModerationFailureReason =
  | "forbidden"
  | "conflict"
  | "rate_limited"
  | "provider_unavailable"
  | "unknown";

export type ClassroomModerationMutationState =
  | { readonly status: "idle" }
  | {
      readonly status: "submitting";
      readonly action: ClassroomModerationAction;
      readonly targetParticipantKey?: string;
    }
  | {
      readonly status: "failed";
      readonly action: ClassroomModerationAction;
      readonly reason: ClassroomModerationFailureReason;
      readonly targetParticipantKey?: string;
    };

export type ClassroomModerationProviderEffect =
  | { readonly status: "idle" }
  | {
      readonly status: "pending";
      readonly action: ClassroomModerationAction;
      readonly targetParticipantKey?: string;
    }
  | {
      readonly status: "applied";
      readonly action: ClassroomModerationAction;
      readonly targetParticipantKey?: string;
    }
  | {
      readonly status: "reconcile_required";
      readonly action: ClassroomModerationAction;
      readonly targetParticipantKey?: string;
    };

/**
 * Fail-closed presentation contract. Every capability is an exact server
 * projection. The browser must never infer an operation from instance_role or
 * a LiveKit participant attribute.
 */
export interface ClassroomModerationParticipantOperations {
  readonly participantKey: string;
  readonly canPromoteCoHost: boolean;
  readonly canDemoteCoHost: boolean;
  readonly canRemoteMute: boolean;
  readonly canRemove: boolean;
}

export interface ClassroomModerationControlsModel {
  readonly roomLocked: boolean;
  readonly canLockRoom: boolean;
  readonly canEndRoom: boolean;
  readonly participantOperations: readonly ClassroomModerationParticipantOperations[];
  readonly mutationState: ClassroomModerationMutationState;
  readonly providerEffect: ClassroomModerationProviderEffect;
  readonly onSetRoomLocked: (locked: boolean) => Promise<void>;
  readonly onEndRoom: () => Promise<void>;
  readonly onPromoteCoHost: (participantKey: string) => Promise<void>;
  readonly onDemoteCoHost: (participantKey: string) => Promise<void>;
  readonly onRemoteMute: (participantKey: string) => Promise<void>;
  readonly onRemoveParticipant: (participantKey: string) => Promise<void>;
}

interface ClassroomModerationControlsProps {
  readonly controls: ClassroomModerationControlsModel;
  readonly disabled?: boolean;
}

interface ClassroomParticipantModerationMenuProps extends ClassroomModerationControlsProps {
  readonly displayName: string;
  readonly isSelf: boolean;
  readonly participantKey: string;
}

export function ClassroomModerationControls({
  controls,
  disabled = false,
}: ClassroomModerationControlsProps) {
  const { t } = useI18n();
  const [endDialogOpen, setEndDialogOpen] = useState(false);
  const endTrigger = useRef<HTMLButtonElement>(null);
  const busy = moderationBusy(controls);
  const status = moderationStatus(controls);

  useEffect(() => {
    if (
      endDialogOpen &&
      controls.providerEffect.status === "applied" &&
      controls.providerEffect.action === "end_room"
    ) {
      const frame = globalThis.requestAnimationFrame(() => {
        setEndDialogOpen(false);
      });
      return () => globalThis.cancelAnimationFrame(frame);
    }
    return undefined;
  }, [controls.providerEffect, endDialogOpen]);

  if (!controls.canLockRoom && !controls.canEndRoom && status === null) {
    return null;
  }

  return (
    <section
      aria-label={t("media.p407.roomControls")}
      className="media-p407-room-controls"
    >
      <div className="media-p407-room-actions" role="group">
        {controls.canLockRoom && (
          <Button
            aria-pressed={controls.roomLocked}
            disabled={disabled || busy}
            leadingIcon={controls.roomLocked ? <Unlock /> : <Lock />}
            onClick={() =>
              runModerationCommand(() =>
                controls.onSetRoomLocked(!controls.roomLocked),
              )
            }
            size="sm"
            variant={controls.roomLocked ? "primary" : "secondary"}
          >
            {t(
              controls.roomLocked
                ? "media.p407.unlockRoom"
                : "media.p407.lockRoom",
            )}
          </Button>
        )}

        {controls.canEndRoom && (
          <Button
            disabled={disabled || busy}
            leadingIcon={<LogOut />}
            onClick={() => setEndDialogOpen(true)}
            ref={endTrigger}
            size="sm"
            variant="danger"
          >
            {t("media.p407.endRoom")}
          </Button>
        )}
      </div>

      {status && (
        <p
          aria-atomic="true"
          aria-live={status.alert ? "assertive" : "polite"}
          className="media-p407-status"
          role={status.alert ? "alert" : "status"}
        >
          {t(status.key)}
        </p>
      )}

      <Dialog
        onOpenChange={(open) => {
          if (open) {
            setEndDialogOpen(true);
          } else if (!busy) {
            setEndDialogOpen(false);
            globalThis.requestAnimationFrame(() => endTrigger.current?.focus());
          }
        }}
        open={endDialogOpen}
      >
        <DialogContent
          closeLabel={t("media.p407.cancel")}
          data-theme="dark"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            endTrigger.current?.focus();
          }}
        >
          <DialogTitle>{t("media.p407.endTitle")}</DialogTitle>
          <DialogDescription>
            {t("media.p407.endDescription")}
          </DialogDescription>
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={busy} variant="secondary">
                {t("media.p407.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={busy}
              loading={
                controls.mutationState.status === "submitting" &&
                controls.mutationState.action === "end_room"
              }
              loadingLabel={t("media.p407.submitting")}
              onClick={() => runModerationCommand(controls.onEndRoom)}
              variant="danger"
            >
              {t("media.p407.confirmEnd")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

export function ClassroomParticipantModerationMenu({
  controls,
  disabled = false,
  displayName,
  isSelf,
  participantKey,
}: ClassroomParticipantModerationMenuProps) {
  const { t } = useI18n();
  const trigger = useRef<HTMLButtonElement>(null);
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const operations = controls.participantOperations.find(
    (candidate) => candidate.participantKey === participantKey,
  );
  const busy = moderationBusy(controls);

  useEffect(() => {
    if (
      removeDialogOpen &&
      controls.providerEffect.status === "applied" &&
      controls.providerEffect.action === "remove_participant" &&
      controls.providerEffect.targetParticipantKey === participantKey
    ) {
      const frame = globalThis.requestAnimationFrame(() => {
        setRemoveDialogOpen(false);
      });
      return () => globalThis.cancelAnimationFrame(frame);
    }
    return undefined;
  }, [controls.providerEffect, participantKey, removeDialogOpen]);

  const hasAction = Boolean(
    operations &&
    (operations.canPromoteCoHost ||
      operations.canDemoteCoHost ||
      operations.canRemoteMute ||
      operations.canRemove),
  );
  if (isSelf || !operations || !hasAction) return null;

  return (
    <span className="media-p407-participant-actions">
      <Menu modal={false}>
        <MenuTrigger asChild>
          <IconButton
            disabled={disabled || busy}
            label={t("media.p407.participantMenu", { name: displayName })}
            ref={trigger}
            size="sm"
            variant="quiet"
          >
            <MoreHorizontal />
          </IconButton>
        </MenuTrigger>
        <MenuContent
          aria-label={t("media.p407.participantMenu", { name: displayName })}
          className="media-p407-participant-menu"
          data-theme="dark"
        >
          {operations.canPromoteCoHost && (
            <MenuItem
              onSelect={() =>
                runModerationCommand(() =>
                  controls.onPromoteCoHost(participantKey),
                )
              }
            >
              <UserPlus aria-hidden="true" />
              {t("media.p407.promoteCoHost")}
            </MenuItem>
          )}
          {operations.canDemoteCoHost && (
            <MenuItem
              onSelect={() =>
                runModerationCommand(() =>
                  controls.onDemoteCoHost(participantKey),
                )
              }
            >
              <UserMinus aria-hidden="true" />
              {t("media.p407.demoteCoHost")}
            </MenuItem>
          )}
          {operations.canRemoteMute && (
            <MenuItem
              onSelect={() =>
                runModerationCommand(() =>
                  controls.onRemoteMute(participantKey),
                )
              }
            >
              <MicOff aria-hidden="true" />
              {t("media.p407.remoteMute")}
            </MenuItem>
          )}
          {operations.canRemove && (
            <MenuItem onSelect={() => setRemoveDialogOpen(true)} tone="danger">
              <UserRoundX aria-hidden="true" />
              {t("media.p407.remove")}
            </MenuItem>
          )}
        </MenuContent>
      </Menu>

      <Dialog
        onOpenChange={(open) => {
          if (open) {
            setRemoveDialogOpen(true);
          } else if (!busy) {
            setRemoveDialogOpen(false);
            globalThis.requestAnimationFrame(() => trigger.current?.focus());
          }
        }}
        open={removeDialogOpen}
      >
        <DialogContent
          closeLabel={t("media.p407.cancel")}
          data-theme="dark"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            trigger.current?.focus();
          }}
        >
          <DialogTitle>{t("media.p407.removeTitle")}</DialogTitle>
          <DialogDescription>
            {t("media.p407.removeDescription", { name: displayName })}
          </DialogDescription>
          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={busy} variant="secondary">
                {t("media.p407.cancel")}
              </Button>
            </DialogClose>
            <Button
              disabled={busy}
              loading={
                controls.mutationState.status === "submitting" &&
                controls.mutationState.action === "remove_participant" &&
                controls.mutationState.targetParticipantKey === participantKey
              }
              loadingLabel={t("media.p407.submitting")}
              onClick={() =>
                runModerationCommand(() =>
                  controls.onRemoveParticipant(participantKey),
                )
              }
              variant="danger"
            >
              {t("media.p407.confirmRemove")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </span>
  );
}

function moderationBusy(controls: ClassroomModerationControlsModel): boolean {
  return (
    controls.mutationState.status === "submitting" ||
    controls.providerEffect.status === "pending"
  );
}

function moderationStatus(
  controls: ClassroomModerationControlsModel,
): { readonly alert: boolean; readonly key: TranslationKey } | null {
  if (controls.mutationState.status === "failed") {
    return {
      alert: true,
      key: moderationFailureKey(controls.mutationState.reason),
    };
  }
  if (controls.providerEffect.status === "reconcile_required") {
    return { alert: true, key: "media.p407.reconcileRequired" };
  }
  if (controls.providerEffect.status === "pending") {
    return { alert: false, key: "media.p407.providerPending" };
  }
  if (controls.providerEffect.status === "applied") {
    return { alert: false, key: "media.p407.applied" };
  }
  return null;
}

function moderationFailureKey(
  reason: ClassroomModerationFailureReason,
): TranslationKey {
  return `media.p407.error.${reason}` as TranslationKey;
}

function runModerationCommand(command: () => Promise<void>): void {
  void command().catch(() => undefined);
}
