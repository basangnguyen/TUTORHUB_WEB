import { APIRequestError } from "@tutorhub/api-client";
import { Button } from "@tutorhub/ui";
import { MessageCircle } from "lucide-react";
import { useNavigate } from "react-router";
import {
  conversationCreationAvailability,
  useEnsureClassConversation,
} from "../app/conversations";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";
import { useTenantCapabilities } from "../app/tenantCapabilities";
import { TenantOperationNotice } from "./TenantOperationNotice";

export function ClassConversationAction({ classID }: { classID: string }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const capabilities = useTenantCapabilities(tenantID, Boolean(tenantID));
  const availability = conversationCreationAvailability(capabilities);
  const ensureConversation = useEnsureClassConversation(tenantID);

  const openConversation = () => {
    if (!availability.available || ensureConversation.isPending) {
      return;
    }
    ensureConversation.mutate(classID, {
      onSuccess: (conversation) => navigate(`/app/messages/${conversation.id}`),
    });
  };

  const concealed =
    ensureConversation.error instanceof APIRequestError &&
    [401, 403, 404].includes(ensureConversation.error.status);

  return (
    <section
      aria-labelledby="class-conversation-title"
      className="classroom-detail__section"
    >
      <div>
        <h2 id="class-conversation-title">
          {t("conversations.classActionTitle")}
        </h2>
        <p>{t("conversations.classActionDescription")}</p>
      </div>

      <TenantOperationNotice
        availability={availability}
        label={t("capabilities.operationCreateConversation")}
        onRetry={() => void capabilities.refetch()}
      />

      {ensureConversation.isError && (
        <p className="class-enrollments__error" role="alert">
          {t(
            concealed
              ? "conversations.classActionConcealed"
              : "conversations.classActionError",
          )}
        </p>
      )}

      <Button
        disabled={!availability.available}
        leadingIcon={<MessageCircle />}
        loading={ensureConversation.isPending}
        loadingLabel={t("conversations.classActionOpening")}
        onClick={openConversation}
      >
        {t("conversations.classActionButton")}
      </Button>
    </section>
  );
}
