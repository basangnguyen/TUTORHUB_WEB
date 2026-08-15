import { APIRequestError } from "@tutorhub/api-client";
import {
  Button,
  EmptyState,
  ErrorState,
  ForbiddenState,
  Skeleton,
  SkeletonGroup,
} from "@tutorhub/ui";
import { RefreshCw } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";
import { useEnsureMediaSpaceConversation } from "../app/conversations";
import { useI18n } from "../app/i18n";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { ConversationMessages } from "./ConversationMessages";
import "../pages/ConversationsPage.css";

export function MediaSpaceChatPanel({
  actorID,
  enabled,
  mediaSpaceID,
  tenantID,
}: {
  actorID: string;
  enabled: boolean;
  mediaSpaceID: string;
  tenantID: string;
}) {
  const { language, t } = useI18n();
  const ensureConversation = useEnsureMediaSpaceConversation(tenantID);
  const startedScope = useRef<string | null>(null);
  const scope = `${tenantID}\u0000${mediaSpaceID}`;
  const formatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  useEffect(() => {
    if (
      !enabled ||
      !tenantID ||
      !mediaSpaceID ||
      startedScope.current === scope
    ) {
      return;
    }
    startedScope.current = scope;
    ensureConversation.mutate(mediaSpaceID);
  }, [enabled, ensureConversation, mediaSpaceID, scope, tenantID]);

  if (!enabled) {
    return (
      <EmptyState
        description={t("media.p408.unavailableDescription")}
        title={t("media.p408.unavailableTitle")}
      />
    );
  }

  if (ensureConversation.isIdle || ensureConversation.isPending) {
    return (
      <SkeletonGroup label={t("media.p408.loading")}>
        <Skeleton height={72} />
        <Skeleton height={72} />
        <Skeleton height={112} />
      </SkeletonGroup>
    );
  }

  if (ensureConversation.isError) {
    const concealed = shouldConcealTenantScopedData(ensureConversation.error);
    const forbidden =
      ensureConversation.error instanceof APIRequestError &&
      ensureConversation.error.status === 403;
    const State = forbidden
      ? ForbiddenState
      : concealed
        ? EmptyState
        : ErrorState;
    return (
      <State
        actions={
          concealed ? undefined : (
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => ensureConversation.mutate(mediaSpaceID)}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          )
        }
        description={
          forbidden
            ? t("media.p408.forbiddenDescription")
            : concealed
              ? t("media.p408.unavailableDescription")
              : t("media.p408.errorDescription")
        }
        title={
          forbidden
            ? t("media.p408.forbiddenTitle")
            : concealed
              ? t("media.p408.unavailableTitle")
              : t("media.p408.errorTitle")
        }
      />
    );
  }

  return (
    <ConversationMessages
      actorID={actorID}
      canPostMessages={ensureConversation.data.viewer_access.can_post_messages}
      conversationID={ensureConversation.data.id}
      formatter={formatter}
      tenantID={tenantID}
    />
  );
}
