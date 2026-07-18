import { APIRequestError, type Tenant } from "@tutorhub/api-client";
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
import { Archive, Building2, RefreshCw, Save } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { useI18n, type TranslationKey } from "../app/i18n";
import { useSession } from "../app/session";
import {
  useArchiveTenant,
  useTenantDetail,
  useTenantList,
  useUpdateTenant,
} from "../app/workspaces";

type EditableLocale = "vi" | "en";

interface WorkspaceFormErrors {
  name?: string;
  slug?: string;
  timezone?: string;
}

interface WorkspaceDraftBase {
  locale: EditableLocale;
  name: string;
  slug: string;
  tenantID: string;
  timezone: string;
  version: number;
}

interface WorkspaceDraft {
  base: WorkspaceDraftBase;
  locale: EditableLocale;
  name: string;
  slug: string;
  timezone: string;
}

function workspaceDraftBase(tenant: Tenant): WorkspaceDraftBase {
  return {
    locale: tenant.locale === "en" ? "en" : "vi",
    name: tenant.name,
    slug: tenant.slug,
    tenantID: tenant.id,
    timezone: tenant.timezone,
    version: tenant.version,
  };
}

function workspaceDraft(tenant: Tenant): WorkspaceDraft {
  const base = workspaceDraftBase(tenant);
  return {
    base,
    locale: base.locale,
    name: base.name,
    slug: base.slug,
    timezone: base.timezone,
  };
}

function normalizeWorkspaceSlug(value: string) {
  return value
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/đ/g, "d")
    .replace(/Đ/g, "D")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 63)
    .replace(/-+$/g, "");
}

function workspaceDraftChanged(draft: WorkspaceDraft) {
  return (
    draft.name.trim() !== draft.base.name ||
    normalizeWorkspaceSlug(draft.slug) !== draft.base.slug ||
    draft.locale !== draft.base.locale ||
    draft.timezone.trim() !== draft.base.timezone
  );
}

function isValidTimeZone(timezone: string) {
  try {
    new Intl.DateTimeFormat("en", { timeZone: timezone }).format();
    return true;
  } catch {
    return false;
  }
}

function isConflict(error: Error | null) {
  return error instanceof APIRequestError && error.status === 409;
}

function isForbidden(error: Error | null) {
  return error instanceof APIRequestError && error.status === 403;
}

function tenantStatusKey(status: Tenant["status"]): TranslationKey {
  return status === "archived"
    ? "workspace.statusArchived"
    : status === "suspended"
      ? "workspace.statusSuspended"
      : "workspace.statusActive";
}

function tenantStatusTone(status: Tenant["status"]) {
  return status === "archived"
    ? ("neutral" as const)
    : status === "suspended"
      ? ("warning" as const)
      : ("success" as const);
}

function roleKey(role: Tenant["role"]): TranslationKey {
  return role === "org_admin"
    ? "shell.role.admin"
    : role === "teacher"
      ? "shell.role.teacher"
      : role === "student"
        ? "shell.role.student"
        : "shell.role.guest";
}

export function WorkspaceManagementPage() {
  const { language, t } = useI18n();
  const session = useSession();
  const activeTenantID = session.currentUser?.active_tenant?.id;
  const tenantList = useTenantList();
  const tenantDetail = useTenantDetail(activeTenantID);
  const canManage =
    session.currentUser?.permissions.includes("tenant.manage") ?? false;
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  const retryWorkspace = async () => {
    await Promise.all([tenantDetail.refetch(), tenantList.refetch()]);
  };

  if (tenantDetail.isPending) {
    return (
      <div className="page-content workspace-management">
        <SkeletonGroup label={t("workspace.managementLoading")}>
          <Skeleton height={34} width="44%" />
          <Skeleton height={18} width="68%" />
          <Skeleton height={280} />
        </SkeletonGroup>
      </div>
    );
  }

  if (tenantDetail.isError || !tenantDetail.data) {
    const forbidden = isForbidden(tenantDetail.error);
    const State = forbidden ? ForbiddenState : ErrorState;
    return (
      <div className="page-content workspace-management">
        <State
          actions={
            <Button
              leadingIcon={<RefreshCw />}
              onClick={() => void retryWorkspace()}
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={
            forbidden
              ? t("workspace.managementForbiddenDescription")
              : t("workspace.managementLoadErrorDescription")
          }
          title={
            forbidden
              ? t("workspace.managementForbiddenTitle")
              : t("workspace.managementLoadErrorTitle")
          }
        />
      </div>
    );
  }

  const tenant = tenantDetail.data;

  return (
    <div className="page-content workspace-management">
      <header className="page-heading workspace-management__header">
        <div>
          <p>{t("workspace.managementKicker")}</p>
          <h1>{t("workspace.managementTitle")}</h1>
          <span>{t("workspace.managementDescription")}</span>
        </div>
        <StatusBadge tone={tenantStatusTone(tenant.status)}>
          {t(tenantStatusKey(tenant.status))}
        </StatusBadge>
      </header>

      <div className="workspace-management__layout">
        <div className="workspace-management__main">
          <section
            aria-labelledby="workspace-overview-title"
            className="workspace-management__panel"
          >
            <div className="workspace-management__panel-heading">
              <span aria-hidden="true">
                <Building2 />
              </span>
              <div>
                <h2 id="workspace-overview-title">
                  {t("workspace.overviewTitle")}
                </h2>
                <p>{t("workspace.overviewDescription")}</p>
              </div>
            </div>
            <dl className="workspace-management__facts">
              <div>
                <dt>{t("workspace.nameLabel")}</dt>
                <dd>{tenant.name}</dd>
              </div>
              <div>
                <dt>{t("workspace.slugLabel")}</dt>
                <dd>{tenant.slug}</dd>
              </div>
              <div>
                <dt>{t("workspace.localeLabel")}</dt>
                <dd>{tenant.locale === "en" ? "English" : "Tiếng Việt"}</dd>
              </div>
              <div>
                <dt>{t("workspace.timezoneLabel")}</dt>
                <dd>{tenant.timezone}</dd>
              </div>
              <div>
                <dt>{t("workspace.roleLabel")}</dt>
                <dd>{t(roleKey(tenant.role))}</dd>
              </div>
              <div>
                <dt>{t("workspace.updatedLabel")}</dt>
                <dd>{dateFormatter.format(new Date(tenant.updated_at))}</dd>
              </div>
            </dl>
          </section>

          {canManage ? (
            <WorkspaceEditForm
              onReload={async () => (await tenantDetail.refetch()).data}
              tenant={tenant}
            />
          ) : (
            <ForbiddenState
              description={t("workspace.manageRestrictedDescription")}
              title={t("workspace.manageRestrictedTitle")}
            />
          )}

          {canManage && tenant.status === "active" && (
            <WorkspaceArchivePanel
              onReload={() => tenantDetail.refetch()}
              tenant={tenant}
            />
          )}
        </div>

        <WorkspaceListPanel
          activeTenantID={activeTenantID}
          query={tenantList}
        />
      </div>
    </div>
  );
}

function WorkspaceEditForm({
  onReload,
  tenant,
}: {
  onReload: () => Promise<Tenant | undefined>;
  tenant: Tenant;
}) {
  const { t } = useI18n();
  const updateTenant = useUpdateTenant();
  const [draftOverride, setDraftOverride] = useState<WorkspaceDraft | null>(
    null,
  );
  const [errors, setErrors] = useState<WorkspaceFormErrors>({});
  const [feedback, setFeedback] = useState<string | null>(null);

  const draft =
    draftOverride?.base.tenantID === tenant.id &&
    workspaceDraftChanged(draftOverride)
      ? draftOverride
      : workspaceDraft(tenant);
  const { base, locale, name, slug, timezone } = draft;

  const updateDraft = (change: Partial<Omit<WorkspaceDraft, "base">>) => {
    setDraftOverride((current) => {
      const source =
        current?.base.tenantID === tenant.id && workspaceDraftChanged(current)
          ? current
          : workspaceDraft(tenant);
      return { ...source, ...change };
    });
  };

  const normalizedName = name.trim();
  const normalizedSlug = normalizeWorkspaceSlug(slug);
  const normalizedTimezone = timezone.trim();
  const changed = workspaceDraftChanged(draft);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const nextErrors: WorkspaceFormErrors = {};
    const nameLength = Array.from(normalizedName).length;
    if (nameLength < 2 || nameLength > 120) {
      nextErrors.name = t("workspace.nameValidation");
    }
    if (!/^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$/.test(normalizedSlug)) {
      nextErrors.slug = t("workspace.slugValidation");
    }
    if (
      !normalizedTimezone ||
      normalizedTimezone.length > 100 ||
      !isValidTimeZone(normalizedTimezone)
    ) {
      nextErrors.timezone = t("workspace.timezoneValidation");
    }
    setErrors(nextErrors);
    setFeedback(null);
    if (Object.keys(nextErrors).length > 0 || !changed) {
      return;
    }

    updateTenant.mutate(
      {
        tenantID: tenant.id,
        input: {
          expected_version: base.version,
          name: normalizedName,
          slug: normalizedSlug,
          locale,
          timezone: normalizedTimezone,
        },
      },
      {
        onSuccess: () => {
          setDraftOverride(null);
          setFeedback(t("workspace.updateSuccess"));
        },
      },
    );
  };

  const reloadLatest = async () => {
    const latest = await onReload();
    if (latest) {
      setDraftOverride(null);
      setErrors({});
    }
    updateTenant.reset();
    setFeedback(null);
  };

  return (
    <section
      aria-labelledby="workspace-edit-title"
      className="workspace-management__panel"
    >
      <div className="workspace-management__panel-heading">
        <div>
          <h2 id="workspace-edit-title">{t("workspace.editTitle")}</h2>
          <p>{t("workspace.editDescription")}</p>
        </div>
      </div>
      <form className="workspace-management__form" onSubmit={submit}>
        <TextField
          autoComplete="organization"
          error={errors.name}
          label={t("workspace.nameLabel")}
          maxLength={120}
          onChange={(event) => updateDraft({ name: event.target.value })}
          required
          value={name}
        />
        <TextField
          autoCapitalize="none"
          autoComplete="off"
          error={errors.slug}
          hint={t("workspace.slugHelp")}
          label={t("workspace.slugLabel")}
          maxLength={63}
          onChange={(event) =>
            updateDraft({ slug: normalizeWorkspaceSlug(event.target.value) })
          }
          required
          spellCheck={false}
          value={slug}
        />
        <SelectField
          ariaLabel={t("workspace.localeLabel")}
          label={t("workspace.localeLabel")}
          onValueChange={(value) =>
            updateDraft({ locale: value as EditableLocale })
          }
          options={[
            { label: "Tiếng Việt", value: "vi" },
            { label: "English", value: "en" },
          ]}
          value={locale}
        />
        <TextField
          autoComplete="off"
          error={errors.timezone}
          hint={t("workspace.timezoneHelp")}
          label={t("workspace.timezoneLabel")}
          maxLength={100}
          onChange={(event) => updateDraft({ timezone: event.target.value })}
          required
          value={timezone}
        />

        {updateTenant.isError && (
          <div className="workspace-management__feedback" role="alert">
            <span>
              {isConflict(updateTenant.error)
                ? t("workspace.updateConflict")
                : isForbidden(updateTenant.error)
                  ? t("workspace.updateForbidden")
                  : t("workspace.updateError")}
            </span>
            {isConflict(updateTenant.error) && (
              <Button
                onClick={() => void reloadLatest()}
                size="sm"
                variant="secondary"
              >
                {t("workspace.reloadLatest")}
              </Button>
            )}
          </div>
        )}

        {feedback && (
          <p className="workspace-management__success" role="status">
            {feedback}
          </p>
        )}

        <div className="workspace-management__form-actions">
          <Button
            disabled={!changed}
            leadingIcon={<Save />}
            loading={updateTenant.isPending}
            loadingLabel={t("workspace.updating")}
            type="submit"
          >
            {t("workspace.updateAction")}
          </Button>
        </div>
      </form>
    </section>
  );
}

function WorkspaceArchivePanel({
  onReload,
  tenant,
}: {
  onReload: () => Promise<unknown>;
  tenant: Tenant;
}) {
  const { t } = useI18n();
  const archiveTenant = useArchiveTenant();
  const [open, setOpen] = useState(false);

  const reloadLatest = async () => {
    await onReload();
    archiveTenant.reset();
    setOpen(false);
  };

  return (
    <section
      aria-labelledby="workspace-archive-title"
      className="workspace-management__panel workspace-management__panel--danger"
    >
      <div>
        <h2 id="workspace-archive-title">{t("workspace.archiveTitle")}</h2>
        <p>{t("workspace.archiveDescription")}</p>
      </div>
      <Dialog
        onOpenChange={(nextOpen) => {
          if (!archiveTenant.isPending) {
            setOpen(nextOpen);
            if (!nextOpen) {
              archiveTenant.reset();
            }
          }
        }}
        open={open}
      >
        <DialogTrigger asChild>
          <Button leadingIcon={<Archive />} variant="danger">
            {t("workspace.archiveAction")}
          </Button>
        </DialogTrigger>
        <DialogContent closeLabel={t("workspace.archiveCloseLabel")}>
          <DialogTitle>{t("workspace.archiveConfirmTitle")}</DialogTitle>
          <DialogDescription>
            {t("workspace.archiveConfirmDescription", { name: tenant.name })}
          </DialogDescription>
          <p className="workspace-management__archive-warning">
            {t("workspace.archiveWarning")}
          </p>

          {archiveTenant.isError && (
            <div className="workspace-management__feedback" role="alert">
              <span>
                {isConflict(archiveTenant.error)
                  ? t("workspace.archiveConflict")
                  : isForbidden(archiveTenant.error)
                    ? t("workspace.archiveForbidden")
                    : t("workspace.archiveError")}
              </span>
              {isConflict(archiveTenant.error) && (
                <Button
                  onClick={() => void reloadLatest()}
                  size="sm"
                  variant="secondary"
                >
                  {t("workspace.reloadLatest")}
                </Button>
              )}
            </div>
          )}

          <DialogFooter>
            <DialogClose asChild>
              <Button disabled={archiveTenant.isPending} variant="secondary">
                {t("workspace.cancelAction")}
              </Button>
            </DialogClose>
            <Button
              loading={archiveTenant.isPending}
              loadingLabel={t("workspace.archiving")}
              onClick={() =>
                archiveTenant.mutate({
                  tenantID: tenant.id,
                  input: { expected_version: tenant.version },
                })
              }
              variant="danger"
            >
              {t("workspace.archiveConfirmAction")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function WorkspaceListPanel({
  activeTenantID,
  query,
}: {
  activeTenantID: string | undefined;
  query: ReturnType<typeof useTenantList>;
}) {
  const { t } = useI18n();

  return (
    <aside
      aria-labelledby="workspace-list-title"
      className="workspace-management__panel workspace-management__list-panel"
    >
      <div className="workspace-management__panel-heading">
        <div>
          <h2 id="workspace-list-title">{t("workspace.listTitle")}</h2>
          <p>{t("workspace.listDescription")}</p>
        </div>
      </div>

      {query.isPending && (
        <SkeletonGroup label={t("workspace.listLoading")}>
          <Skeleton height={72} />
          <Skeleton height={72} />
        </SkeletonGroup>
      )}

      {query.isError && (
        <ErrorState
          actions={
            <Button
              onClick={() => void query.refetch()}
              size="sm"
              variant="secondary"
            >
              {t("state.retry")}
            </Button>
          }
          description={t("workspace.listErrorDescription")}
          title={t("workspace.listErrorTitle")}
        />
      )}

      {query.isSuccess && query.data.items.length === 0 && (
        <EmptyState
          description={t("workspace.listEmptyDescription")}
          title={t("workspace.listEmptyTitle")}
        />
      )}

      {query.data && query.data.items.length > 0 && (
        <ul className="workspace-management__list">
          {query.data.items.map((tenant) => (
            <li key={tenant.id}>
              <div>
                <strong>{tenant.name}</strong>
                <small>{tenant.slug}</small>
              </div>
              <div>
                {tenant.id === activeTenantID && (
                  <span className="workspace-management__active-label">
                    {t("workspace.activeShort")}
                  </span>
                )}
                <StatusBadge tone={tenantStatusTone(tenant.status)}>
                  {t(tenantStatusKey(tenant.status))}
                </StatusBadge>
              </div>
            </li>
          ))}
        </ul>
      )}
    </aside>
  );
}
