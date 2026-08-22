import { APIRequestError, type WhiteboardDocument } from "@tutorhub/api-client";
import { Button } from "@tutorhub/ui";
import { lazy, Suspense, type ReactNode } from "react";
import { useI18n } from "../../app/i18n";
import {
  usePrepareWhiteboard,
  useTransitionWhiteboard,
  useWhiteboardTool,
} from "../../app/whiteboards";

const LazyWhiteboardEngine = lazy(() => import("./LazyWhiteboardEngine"));

export interface ClassroomWhiteboardToolProps {
  actorID: string;
  enabled: boolean;
  mediaSpaceID: string;
  tenantID: string;
}

export function ClassroomWhiteboardTool({
  actorID,
  enabled,
  mediaSpaceID,
  tenantID,
}: ClassroomWhiteboardToolProps) {
  const { t } = useI18n();
  const tool = useWhiteboardTool(tenantID, mediaSpaceID, enabled);
  const prepare = usePrepareWhiteboard(tenantID, mediaSpaceID);
  const transition = useTransitionWhiteboard(tenantID, mediaSpaceID);

  if (!enabled) {
    return <WhiteboardState message={t("whiteboard.featureOff")} />;
  }
  if (tool.isPending) {
    return <WhiteboardState message={t("whiteboard.loading")} />;
  }
  if (tool.isError) {
    const state = classifyWhiteboardError(tool.error);
    return (
      <WhiteboardState
        alert={state !== "featureOff"}
        message={t(`whiteboard.${state}`)}
      >
        {state === "error" ? (
          <Button onClick={() => void tool.refetch()} variant="secondary">
            {t("whiteboard.retry")}
          </Button>
        ) : null}
      </WhiteboardState>
    );
  }

  const projection = tool.data;
  if (projection.document === null) {
    return (
      <WhiteboardState message={t("whiteboard.empty")}>
        {projection.can_create ? (
          <Button disabled={prepare.isPending} onClick={() => prepare.mutate()}>
            {prepare.isPending
              ? t("whiteboard.preparing")
              : t("whiteboard.prepare")}
          </Button>
        ) : null}
        {prepare.isError ? (
          <p className="whiteboard-tool-error" role="alert">
            {t("whiteboard.mutationError")}
          </p>
        ) : null}
      </WhiteboardState>
    );
  }

  return (
    <WhiteboardDocumentView
      actorID={actorID}
      document={projection.document}
      mutationPending={transition.isPending}
      onRecoveryRequired={() => void tool.refetch()}
      onTransition={(operation) =>
        transition.mutate({ document: projection.document!, operation })
      }
      tenantID={tenantID}
      transitionError={transition.isError}
    />
  );
}

function WhiteboardDocumentView({
  actorID,
  document,
  mutationPending,
  onRecoveryRequired,
  onTransition,
  tenantID,
  transitionError,
}: {
  actorID: string;
  document: WhiteboardDocument;
  mutationPending: boolean;
  onRecoveryRequired: () => void;
  onTransition: (operation: "open" | "suspend" | "resume" | "close") => void;
  tenantID: string;
  transitionError: boolean;
}) {
  const { t } = useI18n();
  const capabilities = document.viewer_capabilities;
  return (
    <section
      aria-labelledby="whiteboard-tool-title"
      className="whiteboard-tool"
    >
      <header className="whiteboard-tool-header">
        <div>
          <h2 id="whiteboard-tool-title">{t("whiteboard.title")}</h2>
          <p>{t(`whiteboard.status.${document.status}`)}</p>
        </div>
        <div className="whiteboard-tool-actions">
          {capabilities.can_open ? (
            <Button
              disabled={mutationPending}
              onClick={() => onTransition("open")}
            >
              {t("whiteboard.open")}
            </Button>
          ) : null}
          {capabilities.can_suspend ? (
            <Button
              disabled={mutationPending}
              onClick={() => onTransition("suspend")}
            >
              {t("whiteboard.suspend")}
            </Button>
          ) : null}
          {capabilities.can_resume ? (
            <Button
              disabled={mutationPending}
              onClick={() => onTransition("resume")}
            >
              {t("whiteboard.resume")}
            </Button>
          ) : null}
          {capabilities.can_close ? (
            <Button
              disabled={mutationPending}
              onClick={() => onTransition("close")}
              variant="danger"
            >
              {t("whiteboard.close")}
            </Button>
          ) : null}
        </div>
      </header>
      {transitionError ? (
        <p className="whiteboard-tool-error" role="alert">
          {t("whiteboard.mutationError")}
        </p>
      ) : null}
      {document.status === "open" && capabilities.can_exchange_grant ? (
        <Suspense
          fallback={<WhiteboardState message={t("whiteboard.engineLoading")} />}
        >
          <LazyWhiteboardEngine
            actorID={actorID}
            capability={capabilities.capability}
            document={document}
            key={`${document.id}:${document.current_generation}:${document.revoke_generation}:${capabilities.capability}`}
            onRecoveryRequired={onRecoveryRequired}
            tenantID={tenantID}
          />
        </Suspense>
      ) : (
        <WhiteboardState message={t(`whiteboard.state.${document.status}`)} />
      )}
    </section>
  );
}

function WhiteboardState({
  alert = false,
  children,
  message,
}: {
  alert?: boolean;
  children?: ReactNode;
  message: string;
}) {
  return (
    <div className="whiteboard-tool-state">
      <p aria-live="polite" role={alert ? "alert" : "status"}>
        {message}
      </p>
      {children}
    </div>
  );
}

function classifyWhiteboardError(
  error: Error,
): "error" | "featureOff" | "forbidden" {
  if (error instanceof APIRequestError) {
    if (error.status === 503) return "featureOff";
    if ([401, 403, 404].includes(error.status)) return "forbidden";
  }
  return "error";
}
