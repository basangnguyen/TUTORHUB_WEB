import { APIRequestError, type ClassroomClass } from "@tutorhub/api-client";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
  TextField,
} from "@tutorhub/ui";
import { UserPlus } from "lucide-react";
import { useRef, useState, type FormEvent } from "react";
import { parseClassInvitationToken } from "../app/classInvitationToken";
import { useJoinClassInvitation } from "../app/classEnrollments";
import { useI18n, type TranslationKey } from "../app/i18n";

function joinErrorKey(error: Error | null): TranslationKey {
  if (error instanceof APIRequestError && error.status === 401) {
    return "classInvitation.sessionExpired";
  }
  if (error instanceof APIRequestError && error.status === 403) {
    return "classInvitation.forbidden";
  }
  if (error instanceof APIRequestError && error.status === 429) {
    return "classInvitation.rateLimited";
  }
  if (
    error instanceof APIRequestError &&
    [400, 404, 409, 410].includes(error.status)
  ) {
    return "classInvitation.unavailableDescription";
  }
  return "classInvitation.joinError";
}

export function ClassJoinDialog({
  onJoined,
  tenantID,
}: {
  onJoined: (classroom: ClassroomClass, tenantID: string) => void;
  tenantID: string;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [input, setInput] = useState("");
  const [validationError, setValidationError] = useState(false);
  const token = useRef<string | null>(null);
  const joinInvitation = useJoinClassInvitation(() => token.current, tenantID);
  const errorMessage = validationError
    ? t("classInvitation.tokenValidation")
    : joinInvitation.isError
      ? t(joinErrorKey(joinInvitation.error))
      : undefined;

  const reset = () => {
    token.current = null;
    setInput("");
    setValidationError(false);
    joinInvitation.reset();
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const parsedToken = input
      ? parseClassInvitationToken(input)
      : token.current;
    if (!parsedToken) {
      setValidationError(true);
      return;
    }

    token.current = parsedToken;
    setInput("");
    setValidationError(false);
    try {
      const result = await joinInvitation.mutateAsync();
      token.current = null;
      onJoined(result.classroom, tenantID);
      setOpen(false);
      reset();
    } catch {
      // Keep only the in-memory ref so a transient failure can be retried.
    }
  };

  return (
    <Dialog
      onOpenChange={(nextOpen) => {
        if (joinInvitation.isPending) {
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
        <Button leadingIcon={<UserPlus />} variant="secondary">
          {t("classInvitation.openDialog")}
        </Button>
      </DialogTrigger>
      <DialogContent closeLabel={t("classInvitation.closeDialog")}>
        <DialogTitle>{t("classInvitation.title")}</DialogTitle>
        <DialogDescription>
          {t("classInvitation.description")}
        </DialogDescription>
        <form
          className="class-join-form"
          onSubmit={(event) => void submit(event)}
        >
          <TextField
            autoCapitalize="none"
            autoComplete="off"
            error={errorMessage}
            hint={t("classInvitation.tokenHint")}
            label={t("classInvitation.tokenLabel")}
            maxLength={2048}
            onChange={(event) => {
              token.current = null;
              setInput(event.target.value);
              setValidationError(false);
              joinInvitation.reset();
            }}
            placeholder={t("classInvitation.tokenPlaceholder")}
            required={!joinInvitation.isError}
            spellCheck={false}
            type="password"
            value={input}
          />
          <DialogFooter className="class-join-form__actions">
            <DialogClose asChild>
              <Button
                disabled={joinInvitation.isPending}
                type="button"
                variant="secondary"
              >
                {t("classroom.cancelAction")}
              </Button>
            </DialogClose>
            <Button
              loading={joinInvitation.isPending}
              loadingLabel={t("classInvitation.joining")}
              type="submit"
            >
              {joinInvitation.isError
                ? t("classInvitation.retryJoin")
                : t("classInvitation.joinAction")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
