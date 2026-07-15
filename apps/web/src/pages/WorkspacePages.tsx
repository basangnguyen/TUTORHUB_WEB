import { APIRequestError } from "@tutorhub/api-client";
import { Button, TextField } from "@tutorhub/ui";
import { LogOut } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";
import { useI18n } from "../app/i18n";
import { useSession } from "../app/session";
import { useWorkspaceActions } from "../app/workspaces";

function workspaceSlug(value: string) {
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

function mutationMessage(error: unknown, fallback: string) {
  if (error instanceof APIRequestError) {
    return error.problem?.detail ?? error.message;
  }
  return error instanceof Error ? error.message : fallback;
}

function WorkspaceGateHeader() {
  const { language, setLanguage, t } = useI18n();
  const session = useSession();

  return (
    <header className="workspace-gate__header">
      <div className="workspace-gate__brand">
        <span aria-hidden="true">TH</span>
        <strong>{t("brand.product")}</strong>
      </div>
      <div className="workspace-gate__actions">
        <span>{session.currentUser?.user.display_name}</span>
        <label className="language-select">
          <span className="visually-hidden">{t("shell.language")}</span>
          <select
            aria-label={t("shell.language")}
            onChange={(event) =>
              setLanguage(event.target.value as typeof language)
            }
            value={language}
          >
            <option value="vi">Tiếng Việt</option>
            <option value="en">English</option>
          </select>
        </label>
        <Button
          leadingIcon={<LogOut />}
          onClick={() => void session.signOut()}
          size="sm"
          variant="quiet"
        >
          {t("auth.signOut")}
        </Button>
      </div>
    </header>
  );
}

export function WorkspaceOnboardingPage() {
  const { t } = useI18n();
  const { createWorkspace } = useWorkspaceActions();
  const [name, setName] = useState("");
  const [slugOverride, setSlugOverride] = useState<string | null>(null);
  const slug = slugOverride ?? workspaceSlug(name);

  const isValid = useMemo(
    () =>
      name.trim().length >= 2 && /^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$/.test(slug),
    [name, slug],
  );

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!isValid || createWorkspace.isPending) {
      return;
    }
    createWorkspace.mutate({ name: name.trim(), slug });
  };

  return (
    <div className="workspace-gate">
      <WorkspaceGateHeader />
      <main className="workspace-onboarding">
        <section className="workspace-onboarding__intro">
          <p>{t("workspace.kicker")}</p>
          <h1>{t("workspace.createTitle")}</h1>
          <span>{t("workspace.createDescription")}</span>
          <dl>
            <div>
              <dt>01</dt>
              <dd>{t("workspace.stepIdentity")}</dd>
            </div>
            <div>
              <dt>02</dt>
              <dd>{t("workspace.stepClasses")}</dd>
            </div>
            <div>
              <dt>03</dt>
              <dd>{t("workspace.stepInvite")}</dd>
            </div>
          </dl>
        </section>

        <form className="workspace-form" onSubmit={submit}>
          <div className="workspace-form__heading">
            <h2>{t("workspace.detailsTitle")}</h2>
            <p>{t("workspace.detailsDescription")}</p>
          </div>
          <TextField
            autoComplete="organization"
            autoFocus
            className="workspace-form__field"
            label={t("workspace.nameLabel")}
            maxLength={120}
            onChange={(event) => setName(event.target.value)}
            placeholder={t("workspace.namePlaceholder")}
            required
            value={name}
          />
          <label>
            <span>{t("workspace.slugLabel")}</span>
            <div className="workspace-slug-field">
              <span>tutorhub.app/</span>
              <input
                aria-label={t("workspace.slugLabel")}
                aria-describedby="workspace-slug-help"
                autoCapitalize="none"
                autoComplete="off"
                maxLength={63}
                onChange={(event) => {
                  setSlugOverride(workspaceSlug(event.target.value));
                }}
                required
                spellCheck={false}
                value={slug}
              />
            </div>
          </label>
          <small id="workspace-slug-help">{t("workspace.slugHelp")}</small>

          {createWorkspace.isError && (
            <div className="workspace-form__error" role="alert">
              {mutationMessage(
                createWorkspace.error,
                t("workspace.createError"),
              )}
            </div>
          )}

          <Button
            disabled={!isValid || createWorkspace.isPending}
            loading={createWorkspace.isPending}
            loadingLabel={t("workspace.creating")}
            size="lg"
            type="submit"
          >
            {t("workspace.createAction")}
          </Button>
        </form>
      </main>
    </div>
  );
}

export function WorkspaceSelectionPage() {
  const { t } = useI18n();
  const session = useSession();
  const { switchWorkspace } = useWorkspaceActions();

  return (
    <div className="workspace-gate">
      <WorkspaceGateHeader />
      <main className="workspace-selection">
        <header>
          <p>{t("workspace.kicker")}</p>
          <h1>{t("workspace.selectTitle")}</h1>
          <span>{t("workspace.selectDescription")}</span>
        </header>

        {switchWorkspace.isError && (
          <div className="workspace-form__error" role="alert">
            {mutationMessage(switchWorkspace.error, t("workspace.selectError"))}
          </div>
        )}

        <ul className="workspace-selection__list">
          {session.currentUser?.memberships.map((membership) => (
            <li key={membership.id}>
              <button
                disabled={switchWorkspace.isPending}
                onClick={() => switchWorkspace.mutate(membership.id)}
                type="button"
              >
                <span>
                  <strong>{membership.name}</strong>
                  <small>{membership.slug}</small>
                </span>
                <span className="workspace-selection__role">
                  {membership.role === "org_admin"
                    ? t("shell.role.admin")
                    : membership.role === "teacher"
                      ? t("shell.role.teacher")
                      : membership.role === "student"
                        ? t("shell.role.student")
                        : t("shell.role.guest")}
                </span>
              </button>
            </li>
          ))}
        </ul>
        {switchWorkspace.isPending && (
          <p className="workspace-selection__status" role="status">
            {t("workspace.switching")}
          </p>
        )}
      </main>
    </div>
  );
}
