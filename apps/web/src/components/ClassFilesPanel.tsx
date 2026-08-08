import { APIRequestError, type ContentFile } from "@tutorhub/api-client";
import { Button, Skeleton, SkeletonGroup } from "@tutorhub/ui";
import {
  Download,
  FileText,
  RefreshCw,
  ShieldCheck,
  UploadCloud,
} from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import type { ClassroomClass } from "@tutorhub/api-client";
import {
  useClassFileDownload,
  useClassFiles,
  useClassFileUpload,
} from "../app/classFiles";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import { useTenantCapabilities } from "../app/tenantCapabilities";

export function ClassFilesPanel({ classroom }: { classroom: ClassroomClass }) {
  const { t } = useI18n();
  const session = useSession();
  const tenantID = session.currentUser?.active_tenant?.id;
  const filesQuery = useClassFiles(tenantID, classroom.id);
  const capabilities = useTenantCapabilities(tenantID);
  const upload = useClassFileUpload(tenantID, classroom.id);
  const download = useClassFileDownload(tenantID);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [clientRequestID, setClientRequestID] = useState(() =>
    crypto.randomUUID(),
  );
  const files = useMemo(() => {
    const byID = new Map<string, ContentFile>();
    for (const page of filesQuery.data?.pages ?? []) {
      for (const file of page.items) {
        byID.set(file.id, file);
      }
    }
    return [...byID.values()];
  }, [filesQuery.data?.pages]);
  const viewerAccess = filesQuery.data?.pages[0]?.viewer_access;
  const featureEnabled =
    capabilities.data?.features.file_uploads?.enabled === true;
  const canStartUpload =
    viewerAccess?.can_upload === true &&
    featureEnabled &&
    classroom.status === "active";

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!selectedFile || !canStartUpload) {
      return;
    }
    upload.mutate({ file: selectedFile, clientRequestID });
  };

  const resetDraft = () => {
    upload.resetTransfer();
    setSelectedFile(null);
    setClientRequestID(crypto.randomUUID());
  };

  return (
    <section
      aria-labelledby="class-files-title"
      className="classroom-detail__section class-files"
    >
      <div className="class-files__heading">
        <div>
          <h2 id="class-files-title">{t("classFiles.title")}</h2>
          <p>{t("classFiles.description")}</p>
        </div>
        <span className="class-files__safety">
          <ShieldCheck aria-hidden="true" />
          {t("classFiles.attachmentOnly")}
        </span>
      </div>

      {filesQuery.isPending && <ClassFilesSkeleton />}
      {filesQuery.isError && (
        <ClassFilesError
          error={filesQuery.error}
          onRetry={() => void filesQuery.refetch()}
        />
      )}

      {filesQuery.data && !filesQuery.isError && (
        <>
          {viewerAccess?.can_upload && (
            <div className="class-files__transfer">
              {capabilities.isPending ? (
                <SkeletonGroup label={t("classFiles.loadingAvailability")}>
                  <Skeleton />
                </SkeletonGroup>
              ) : capabilities.isError ? (
                <div className="class-files__load-error" role="alert">
                  <div>
                    <strong>{t("classFiles.availabilityErrorTitle")}</strong>
                    <p>{t("classFiles.availabilityErrorDescription")}</p>
                  </div>
                  <Button
                    leadingIcon={<RefreshCw />}
                    onClick={() => void capabilities.refetch()}
                    size="sm"
                    variant="secondary"
                  >
                    {t("state.retry")}
                  </Button>
                </div>
              ) : !featureEnabled ? (
                <div className="class-files__gate" role="status">
                  <UploadCloud aria-hidden="true" />
                  <div>
                    <strong>{t("classFiles.uploadUnavailableTitle")}</strong>
                    <p>{t("classFiles.uploadUnavailableDescription")}</p>
                  </div>
                </div>
              ) : (
                <form onSubmit={submit}>
                  <label htmlFor="class-file-input">
                    {t("classFiles.chooseFile")}
                  </label>
                  <input
                    disabled={upload.isPending || !canStartUpload}
                    id="class-file-input"
                    onChange={(event) => {
                      upload.resetTransfer();
                      setSelectedFile(event.target.files?.[0] ?? null);
                      setClientRequestID(crypto.randomUUID());
                    }}
                    type="file"
                  />
                  <Button
                    disabled={!selectedFile || !canStartUpload}
                    leadingIcon={<UploadCloud />}
                    loading={upload.isPending}
                    loadingLabel={t("classFiles.uploading")}
                    type="submit"
                  >
                    {upload.isError
                      ? t("classFiles.retryUpload")
                      : t("classFiles.uploadAction")}
                  </Button>
                  {upload.progress.phase !== "idle" && (
                    <div
                      aria-label={t(transferPhaseKey(upload.progress.phase))}
                      aria-valuemax={100}
                      aria-valuemin={0}
                      aria-valuenow={upload.progress.percent}
                      className="class-files__progress"
                      role="progressbar"
                    >
                      <span style={{ width: `${upload.progress.percent}%` }} />
                    </div>
                  )}
                  {upload.isError && (
                    <p className="class-files__error" role="alert">
                      {t("classFiles.uploadError")}
                    </p>
                  )}
                  {upload.isSuccess && (
                    <p className="class-files__success" role="status">
                      {t("classFiles.uploadedAwaitingProcessing")}
                      <Button
                        onClick={resetDraft}
                        size="sm"
                        variant="secondary"
                      >
                        {t("classFiles.clearTransfer")}
                      </Button>
                    </p>
                  )}
                </form>
              )}
            </div>
          )}

          {files.length === 0 ? (
            <div className="class-files__empty">
              <FileText aria-hidden="true" />
              <h3>{t("classFiles.emptyTitle")}</h3>
              <p>{t("classFiles.emptyDescription")}</p>
            </div>
          ) : (
            <ul
              aria-label={t("classFiles.listLabel")}
              className="class-files__list"
            >
              {files.map((file) => (
                <ClassFileRow
                  downloading={
                    download.isPending && download.variables?.id === file.id
                  }
                  file={file}
                  key={file.id}
                  onDownload={() => download.mutate(file)}
                />
              ))}
            </ul>
          )}

          {download.isError && (
            <p className="class-files__error" role="alert">
              {t("classFiles.downloadError")}
            </p>
          )}
          {filesQuery.hasNextPage && (
            <Button
              loading={filesQuery.isFetchingNextPage}
              loadingLabel={t("classFiles.loadingMore")}
              onClick={() => void filesQuery.fetchNextPage()}
              variant="secondary"
            >
              {t("classFiles.loadMore")}
            </Button>
          )}
        </>
      )}
    </section>
  );
}

function ClassFileRow({
  downloading,
  file,
  onDownload,
}: {
  downloading: boolean;
  file: ContentFile;
  onDownload: () => void;
}) {
  const { language, t } = useI18n();
  const formatter = new Intl.DateTimeFormat(
    language === "vi" ? "vi-VN" : "en-US",
    { dateStyle: "medium", timeStyle: "short" },
  );
  return (
    <li>
      <FileText aria-hidden="true" />
      <div>
        <strong>{file.display_name}</strong>
        <span>
          {formatBytes(file.expected_size_bytes)} · {file.declared_media_type}
        </span>
        <time dateTime={file.updated_at}>
          {formatter.format(new Date(file.updated_at))}
        </time>
      </div>
      <span className="class-files__status" data-status={file.status}>
        {t(fileStatusKey(file.status))}
      </span>
      {file.viewer_access.can_download && (
        <Button
          leadingIcon={<Download />}
          loading={downloading}
          loadingLabel={t("classFiles.downloading")}
          onClick={onDownload}
          size="sm"
          variant="secondary"
        >
          {t("classFiles.downloadAction")}
        </Button>
      )}
    </li>
  );
}

function ClassFilesError({
  error,
  onRetry,
}: {
  error: Error;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  const concealed =
    error instanceof APIRequestError && [403, 404].includes(error.status);
  return (
    <div className="class-files__load-error" role="alert">
      <div>
        <strong>
          {concealed
            ? t("classFiles.forbiddenTitle")
            : t("classFiles.errorTitle")}
        </strong>
        <p>
          {concealed
            ? t("classFiles.forbiddenDescription")
            : t("classFiles.errorDescription")}
        </p>
      </div>
      {!concealed && (
        <Button
          leadingIcon={<RefreshCw />}
          onClick={onRetry}
          size="sm"
          variant="secondary"
        >
          {t("state.retry")}
        </Button>
      )}
    </div>
  );
}

function ClassFilesSkeleton() {
  const { t } = useI18n();
  return (
    <SkeletonGroup label={t("classFiles.loading")}>
      <Skeleton />
      <Skeleton />
    </SkeletonGroup>
  );
}

function fileStatusKey(status: ContentFile["status"]): TranslationKey {
  return `classFiles.status.${status}` as TranslationKey;
}

function transferPhaseKey(phase: string): TranslationKey {
  return `classFiles.phase.${phase}` as TranslationKey;
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  if (value < 1024 * 1024 * 1024) {
    return `${(value / (1024 * 1024)).toFixed(1)} MB`;
  }
  return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
