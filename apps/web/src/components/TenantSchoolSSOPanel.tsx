import {
  type SSOChallenge,
  type SSOConnection,
  type SSODomain,
  type UpdateSSOConnectionRequest,
} from "@tutorhub/api-client";
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
  SelectField,
  Skeleton,
  SkeletonGroup,
  StatusBadge,
  TextAreaField,
  TextField,
} from "@tutorhub/ui";
import {
  Copy,
  Globe,
  KeyRound,
  Plus,
  RefreshCw,
  Save,
  ShieldCheck,
} from "lucide-react";
import {
  useCallback,
  useId,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { useI18n, type TranslationKey } from "../app/i18n";
import {
  stepUpRequirementFor,
  useStepUpRequest,
  type StepUpRequestOptions,
} from "../app/security";
import {
  isPlausibleDomain,
  parseOrgIDs,
  schoolSSOErrorText,
  useCreateSchoolSSOConnection,
  useCreateSchoolSSODomain,
  useSchoolSSOConnections,
  useSchoolSSODomainAction,
  useSchoolSSODomains,
  useUpdateSchoolSSOConnection,
  type SchoolSSODomainAction,
  type SchoolSSOErrorContext as ErrorContext,
} from "../app/schoolSSO";
import { useSession } from "../app/session";
import { shouldConcealTenantScopedData } from "../app/tenantDataAccess";
import { StepUpAuthDialog } from "./StepUpAuthDialog";
import "./TenantSchoolSSOPanel.css";

type ConnectionKind = SSOConnection["kind"];
type JoinPolicy = "invitation" | "student";

interface Feedback {
  text: string;
  tone: "error" | "success";
}

/**
 * Runs a mutation once; a step-up refusal opens the shared dialog and, after
 * a successful confirmation, runs the same mutation exactly one more time.
 */
type RunGuarded = <T>(
  key: string,
  perform: () => Promise<T>,
  onDone: (result: T) => void,
  context?: ErrorContext,
) => void;

function kindLabelKey(kind: ConnectionKind): TranslationKey {
  return kind === "entra" ? "schoolSSO.kindEntra" : "schoolSSO.kindGoogle";
}

function domainStatusKey(status: SSODomain["status"]): TranslationKey {
  return status === "verified"
    ? "schoolSSO.domainVerified"
    : status === "disabled"
      ? "schoolSSO.domainDisabled"
      : "schoolSSO.domainPending";
}

function domainStatusTone(status: SSODomain["status"]) {
  return status === "verified"
    ? ("success" as const)
    : status === "disabled"
      ? ("neutral" as const)
      : ("warning" as const);
}

export function TenantSchoolSSOPanel({ tenantID }: { tenantID: string }) {
  const { language, t } = useI18n();
  const session = useSession();
  const titleId = useId();
  const membership = session.currentUser?.active_tenant;
  const canManage =
    membership?.id === tenantID &&
    membership.role === "org_admin" &&
    (session.currentUser?.permissions.includes("tenant.manage") ?? false);
  const connections = useSchoolSSOConnections(tenantID, canManage);
  const domains = useSchoolSSODomains(tenantID, canManage);
  const createConnection = useCreateSchoolSSOConnection(tenantID);
  const updateConnection = useUpdateSchoolSSOConnection(tenantID);
  const createDomain = useCreateSchoolSSODomain(tenantID);
  const domainAction = useSchoolSSODomainAction(tenantID);
  const stepUp = useStepUpRequest();
  const requestStepUp = stepUp.request;
  const [feedback, setFeedback] = useState<Feedback | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  // The one-time TXT value lives only in this state, never in a cache or
  // browser storage, and disappears once dismissed or verified.
  const [challenge, setChallenge] = useState<SSOChallenge | null>(null);
  const dateFormatter = useMemo(
    () =>
      new Intl.DateTimeFormat(language === "vi" ? "vi-VN" : "en-US", {
        dateStyle: "medium",
        timeStyle: "short",
      }),
    [language],
  );

  const runGuarded: RunGuarded = useCallback(
    function run<T>(
      key: string,
      perform: () => Promise<T>,
      onDone: (result: T) => void,
      context: ErrorContext = {},
      attempt = 0,
    ) {
      setFeedback(null);
      setBusy(key);
      perform().then(
        (result) => {
          setBusy(null);
          onDone(result);
        },
        (error: unknown) => {
          setBusy(null);
          const requirement = stepUpRequirementFor(error);
          if (requirement && attempt === 0) {
            requestStepUp(
              () => run(key, perform, onDone, context, attempt + 1),
              requirement,
            );
            return;
          }
          setFeedback({
            text: schoolSSOErrorText(error, t, context),
            tone: "error",
          });
        },
      );
    },
    [requestStepUp, t],
  );

  const succeed = useCallback((text: string) => {
    setFeedback({ text, tone: "success" });
  }, []);

  const refetchAll = useCallback(() => {
    void connections.refetch();
    void domains.refetch();
  }, [connections, domains]);

  const heading = (
    <div className="workspace-management__panel-heading">
      <span aria-hidden="true">
        <ShieldCheck />
      </span>
      <div>
        <h2 id={titleId}>{t("schoolSSO.title")}</h2>
        <p>{t("schoolSSO.description")}</p>
      </div>
    </div>
  );

  const shell = (children: ReactNode) => (
    <section
      aria-labelledby={titleId}
      className="workspace-management__panel school-sso"
    >
      {heading}
      {children}
      <StepUpAuthDialog {...stepUp.dialog} />
    </section>
  );

  if (!canManage) {
    return shell(
      <ForbiddenState
        description={t("schoolSSO.forbiddenDescription")}
        title={t("schoolSSO.forbiddenTitle")}
      />,
    );
  }

  if (connections.isPending || domains.isPending) {
    return shell(
      <SkeletonGroup label={t("schoolSSO.loading")}>
        <Skeleton height={28} width="42%" />
        <Skeleton height={120} />
      </SkeletonGroup>,
    );
  }

  const loadError = connections.error ?? domains.error;
  if (loadError || !connections.data || !domains.data) {
    const requirement: Required<StepUpRequestOptions> | null =
      stepUpRequirementFor(loadError);
    if (requirement) {
      return shell(
        <ForbiddenState
          actions={
            <Button
              leadingIcon={<KeyRound />}
              onClick={() => requestStepUp(refetchAll, requirement)}
              variant="secondary"
            >
              {t("schoolSSO.verifyNow")}
            </Button>
          }
          description={
            requirement.requireAal2
              ? t("schoolSSO.assuranceDescription")
              : t("stepUp.description")
          }
          title={
            requirement.requireAal2
              ? t("schoolSSO.assuranceTitle")
              : t("stepUp.title")
          }
        />,
      );
    }
    if (shouldConcealTenantScopedData(loadError)) {
      return shell(
        <ForbiddenState
          description={t("schoolSSO.forbiddenDescription")}
          title={t("schoolSSO.forbiddenTitle")}
        />,
      );
    }
    return shell(
      <ErrorState
        actions={
          <Button
            leadingIcon={<RefreshCw />}
            onClick={refetchAll}
            variant="secondary"
          >
            {t("state.retry")}
          </Button>
        }
        description={t("schoolSSO.errorDescription")}
        title={t("schoolSSO.errorTitle")}
      />,
    );
  }

  const connectionItems = connections.data.items;
  const domainItems = domains.data.items;

  return shell(
    <>
      {feedback ? (
        feedback.tone === "error" ? (
          <p
            className="school-sso__feedback school-sso__feedback--error"
            role="alert"
          >
            {feedback.text}
          </p>
        ) : (
          <p
            className="school-sso__feedback school-sso__feedback--success"
            role="status"
          >
            {feedback.text}
          </p>
        )
      ) : null}

      {connectionItems.length === 0 ? (
        <EmptyState
          description={t("schoolSSO.emptyDescription")}
          title={t("schoolSSO.emptyTitle")}
        />
      ) : (
        <ul
          aria-label={t("schoolSSO.connectionsLabel")}
          className="school-sso__connections"
        >
          {connectionItems.map((connection) => (
            <li key={connection.id}>
              <ConnectionCard
                busy={busy}
                challenge={
                  challenge?.domain.connection_id === connection.id
                    ? challenge
                    : null
                }
                connection={connection}
                dateFormatter={dateFormatter}
                domains={domainItems.filter(
                  (domain) => domain.connection_id === connection.id,
                )}
                onAddDomain={(domain, done) =>
                  runGuarded(
                    `domain-create:${connection.id}`,
                    () =>
                      createDomain.mutateAsync({
                        connectionID: connection.id,
                        domain,
                      }),
                    (created) => {
                      // Drop the mutation result: the TXT value stays only in
                      // this component state.
                      createDomain.reset();
                      setChallenge(created);
                      succeed(
                        t("schoolSSO.domainCreatedSuccess", {
                          domain: created.domain.domain,
                        }),
                      );
                      done();
                    },
                    { domain },
                  )
                }
                onDismissChallenge={() => setChallenge(null)}
                onDomainAction={(domain, action) =>
                  runGuarded(
                    `domain-${action}:${domain.id}`,
                    () =>
                      domainAction.mutateAsync({ action, domainID: domain.id }),
                    (result) => {
                      domainAction.reset();
                      if (result.action === "rotate") {
                        setChallenge(result.challenge);
                        succeed(
                          t("schoolSSO.domainRotatedSuccess", {
                            domain: domain.domain,
                          }),
                        );
                        return;
                      }
                      setChallenge((current) =>
                        current?.domain.id === domain.id ? null : current,
                      );
                      succeed(
                        t(
                          result.action === "verify"
                            ? "schoolSSO.domainVerifiedSuccess"
                            : "schoolSSO.domainDisabledSuccess",
                          { domain: domain.domain },
                        ),
                      );
                    },
                    { domain: domain.domain },
                  )
                }
                onUpdate={(patch, success) =>
                  runGuarded(
                    `connection:${connection.id}`,
                    () =>
                      updateConnection.mutateAsync({
                        connectionID: connection.id,
                        patch,
                      }),
                    () => succeed(t(success)),
                  )
                }
              />
            </li>
          ))}
        </ul>
      )}

      <CreateConnectionForm
        busy={busy === "connection-create"}
        onCreate={(input, done) =>
          runGuarded(
            "connection-create",
            () => createConnection.mutateAsync(input),
            () => {
              succeed(t("schoolSSO.createSuccess"));
              done();
            },
          )
        }
      />
    </>,
  );
}

function CreateConnectionForm({
  busy,
  onCreate,
}: {
  busy: boolean;
  onCreate: (
    input: { kind: ConnectionKind; allowed_org_ids: string[] },
    done: () => void,
  ) => void;
}) {
  const { t } = useI18n();
  const headingId = useId();
  const [kind, setKind] = useState<ConnectionKind>("google_workspace");
  const [orgIDs, setOrgIDs] = useState("");
  const [error, setError] = useState<TranslationKey | null>(null);
  const orgIDsRef = useRef<HTMLTextAreaElement>(null);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    const parsed = parseOrgIDs(kind, orgIDs);
    if (parsed.error) {
      setError(parsed.error);
      orgIDsRef.current?.focus();
      return;
    }
    setError(null);
    onCreate({ kind, allowed_org_ids: parsed.ids }, () => setOrgIDs(""));
  };

  return (
    <form
      aria-labelledby={headingId}
      className="school-sso__create"
      noValidate
      onSubmit={submit}
    >
      <h3 id={headingId}>{t("schoolSSO.createTitle")}</h3>
      <SelectField
        ariaLabel={t("schoolSSO.kindLabel")}
        label={t("schoolSSO.kindLabel")}
        onValueChange={(value) => {
          setKind(value === "entra" ? "entra" : "google_workspace");
          setError(null);
        }}
        options={[
          { label: t("schoolSSO.kindGoogle"), value: "google_workspace" },
          { label: t("schoolSSO.kindEntra"), value: "entra" },
        ]}
        value={kind}
      />
      <TextAreaField
        autoCapitalize="none"
        autoComplete="off"
        error={error ? t(error) : undefined}
        hint={
          kind === "entra"
            ? t("schoolSSO.orgIDsEntraHint")
            : t("schoolSSO.orgIDsGoogleHint")
        }
        label={
          kind === "entra"
            ? t("schoolSSO.orgIDsEntraLabel")
            : t("schoolSSO.orgIDsGoogleLabel")
        }
        onChange={(event) => {
          setOrgIDs(event.target.value);
          setError(null);
        }}
        ref={orgIDsRef}
        rows={2}
        spellCheck={false}
        value={orgIDs}
      />
      <div className="school-sso__actions">
        <Button
          leadingIcon={<Plus />}
          loading={busy}
          loadingLabel={t("schoolSSO.createAction")}
          type="submit"
        >
          {t("schoolSSO.createAction")}
        </Button>
      </div>
    </form>
  );
}

interface ConnectionCardProps {
  busy: string | null;
  challenge: SSOChallenge | null;
  connection: SSOConnection;
  dateFormatter: Intl.DateTimeFormat;
  domains: SSODomain[];
  onAddDomain: (domain: string, done: () => void) => void;
  onDismissChallenge: () => void;
  onDomainAction: (domain: SSODomain, action: SchoolSSODomainAction) => void;
  onUpdate: (
    patch: UpdateSSOConnectionRequest,
    success: TranslationKey,
  ) => void;
}

function ConnectionCard({
  busy,
  challenge,
  connection,
  dateFormatter,
  domains,
  onAddDomain,
  onDismissChallenge,
  onDomainAction,
  onUpdate,
}: ConnectionCardProps) {
  const { t } = useI18n();
  const headingId = useId();
  const enforceId = useId();
  const enforceHintId = useId();
  const [enforceConfirmOpen, setEnforceConfirmOpen] = useState(false);
  const [disableTarget, setDisableTarget] = useState<SSODomain | null>(null);
  const connectionBusy = busy === `connection:${connection.id}`;
  const anyBusy = busy !== null;
  const enabled = connection.status === "enabled";
  const verifiedDomains = domains.filter(
    (domain) => domain.status === "verified",
  );
  const canEnforce = enabled && verifiedDomains.length > 0;
  const provider = connection.kind === "entra" ? "Microsoft" : "Google";
  // Remount the policy forms whenever the server state changes, so drafts
  // always start from what is saved.
  const revision = `${connection.id}:${connection.updated_at}`;

  return (
    <article aria-labelledby={headingId} className="school-sso__connection">
      <header className="school-sso__connection-header">
        <div>
          <h3 id={headingId}>{t(kindLabelKey(connection.kind))}</h3>
          <p className="school-sso__org-ids">
            {connection.allowed_org_ids.join(", ")}
          </p>
        </div>
        <div className="school-sso__badges">
          <StatusBadge tone={enabled ? "success" : "neutral"}>
            {t(
              enabled ? "schoolSSO.statusEnabled" : "schoolSSO.statusDisabled",
            )}
          </StatusBadge>
          {connection.enforce_sso ? (
            <StatusBadge tone="info">
              {t("schoolSSO.enforcedBadge")}
            </StatusBadge>
          ) : null}
        </div>
      </header>

      <div className="school-sso__actions">
        <Button
          disabled={anyBusy && !connectionBusy}
          loading={connectionBusy}
          loadingLabel={t(enabled ? "schoolSSO.disable" : "schoolSSO.enable")}
          onClick={() =>
            onUpdate(
              { status: enabled ? "disabled" : "enabled" },
              enabled
                ? "schoolSSO.disabledSuccess"
                : "schoolSSO.enabledSuccess",
            )
          }
          size="sm"
          variant={enabled ? "secondary" : "primary"}
        >
          {t(enabled ? "schoolSSO.disable" : "schoolSSO.enable")}
        </Button>
      </div>

      <div className="school-sso__enforce">
        <input
          aria-describedby={enforceHintId}
          checked={connection.enforce_sso}
          disabled={anyBusy || (!connection.enforce_sso && !canEnforce)}
          id={enforceId}
          onChange={(event) => {
            if (event.target.checked) {
              setEnforceConfirmOpen(true);
              return;
            }
            onUpdate({ enforce_sso: false }, "schoolSSO.enforceOff");
          }}
          role="switch"
          type="checkbox"
        />
        <div>
          <label htmlFor={enforceId}>{t("schoolSSO.enforceLabel")}</label>
          <p id={enforceHintId}>
            {!connection.enforce_sso && !canEnforce
              ? t("schoolSSO.enforceUnavailable")
              : t("schoolSSO.enforceHint")}
          </p>
        </div>
      </div>

      <OrgIDsForm
        busy={anyBusy}
        connection={connection}
        key={`org:${revision}`}
        onSave={(ids) =>
          onUpdate({ allowed_org_ids: ids }, "schoolSSO.orgIDsSaved")
        }
      />
      <JoinPolicyForm
        busy={anyBusy}
        connection={connection}
        key={`join:${revision}`}
        onSave={(policy) =>
          onUpdate(
            { auto_join_role: policy === "student" ? "student" : null },
            "schoolSSO.joinPolicySaved",
          )
        }
      />

      <section className="school-sso__domains">
        <h4>{t("schoolSSO.domainsTitle")}</h4>
        {challenge ? (
          <ChallengeBox challenge={challenge} onDismiss={onDismissChallenge} />
        ) : null}
        {domains.length === 0 ? (
          <p className="school-sso__muted">{t("schoolSSO.domainsEmpty")}</p>
        ) : (
          <ul className="school-sso__domain-list">
            {domains.map((domain) => (
              <li key={domain.id}>
                <div className="school-sso__domain-name">
                  <Globe aria-hidden="true" />
                  <strong>{domain.domain}</strong>
                  <StatusBadge tone={domainStatusTone(domain.status)}>
                    {t(domainStatusKey(domain.status))}
                  </StatusBadge>
                  {domain.status === "verified" && domain.verified_at ? (
                    <span className="school-sso__muted">
                      {t("schoolSSO.verifiedAt", {
                        time: dateFormatter.format(
                          new Date(domain.verified_at),
                        ),
                      })}
                    </span>
                  ) : null}
                </div>
                <div className="school-sso__actions">
                  {domain.status === "pending" ? (
                    <>
                      <Button
                        aria-label={`${t("schoolSSO.verifyDNS")} ${domain.domain}`}
                        disabled={
                          anyBusy && busy !== `domain-verify:${domain.id}`
                        }
                        loading={busy === `domain-verify:${domain.id}`}
                        loadingLabel={t("schoolSSO.verifyDNS")}
                        onClick={() => onDomainAction(domain, "verify")}
                        size="sm"
                      >
                        {t("schoolSSO.verifyDNS")}
                      </Button>
                      <Button
                        aria-label={`${t("schoolSSO.rotate")} ${domain.domain}`}
                        disabled={
                          anyBusy && busy !== `domain-rotate:${domain.id}`
                        }
                        loading={busy === `domain-rotate:${domain.id}`}
                        loadingLabel={t("schoolSSO.rotate")}
                        onClick={() => onDomainAction(domain, "rotate")}
                        size="sm"
                        variant="secondary"
                      >
                        {t("schoolSSO.rotate")}
                      </Button>
                    </>
                  ) : null}
                  {domain.status !== "disabled" ? (
                    <Button
                      aria-label={`${t("schoolSSO.disableDomain")} ${domain.domain}`}
                      disabled={anyBusy}
                      onClick={() => setDisableTarget(domain)}
                      size="sm"
                      variant="quiet"
                    >
                      {t("schoolSSO.disableDomain")}
                    </Button>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
        )}
        <DomainForm
          busy={busy === `domain-create:${connection.id}`}
          disabled={anyBusy}
          onAdd={onAddDomain}
        />
      </section>

      <Dialog onOpenChange={setEnforceConfirmOpen} open={enforceConfirmOpen}>
        <DialogContent closeLabel={t("schoolSSO.dialogCloseLabel")}>
          <DialogTitle>{t("schoolSSO.enforceConfirmTitle")}</DialogTitle>
          <DialogDescription>
            {t("schoolSSO.enforceConfirmDescription", {
              domains: verifiedDomains
                .map((domain) => domain.domain)
                .join(", "),
              provider,
            })}
          </DialogDescription>
          <p className="school-sso__warning">
            {t("schoolSSO.enforceConfirmAdminNote")}
          </p>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="secondary">{t("schoolSSO.cancel")}</Button>
            </DialogClose>
            <Button
              onClick={() => {
                setEnforceConfirmOpen(false);
                onUpdate({ enforce_sso: true }, "schoolSSO.enforceOn");
              }}
              variant="danger"
            >
              {t("schoolSSO.enforceConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        onOpenChange={(open) => {
          if (!open) setDisableTarget(null);
        }}
        open={disableTarget !== null}
      >
        <DialogContent closeLabel={t("schoolSSO.dialogCloseLabel")}>
          <DialogTitle>
            {t("schoolSSO.disableConfirmTitle", {
              domain: disableTarget?.domain ?? "",
            })}
          </DialogTitle>
          <DialogDescription>
            {t("schoolSSO.disableConfirmDescription")}
          </DialogDescription>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="secondary">{t("schoolSSO.cancel")}</Button>
            </DialogClose>
            <Button
              onClick={() => {
                const target = disableTarget;
                setDisableTarget(null);
                if (target) onDomainAction(target, "disable");
              }}
              variant="danger"
            >
              {t("schoolSSO.disableConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </article>
  );
}

function OrgIDsForm({
  busy,
  connection,
  onSave,
}: {
  busy: boolean;
  connection: SSOConnection;
  onSave: (ids: string[]) => void;
}) {
  const { t } = useI18n();
  const [value, setValue] = useState(connection.allowed_org_ids.join("\n"));
  const [error, setError] = useState<TranslationKey | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);
  const kind = connection.kind;

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    const parsed = parseOrgIDs(kind, value);
    if (parsed.error) {
      setError(parsed.error);
      ref.current?.focus();
      return;
    }
    setError(null);
    onSave(parsed.ids);
  };

  return (
    <form className="school-sso__form" noValidate onSubmit={submit}>
      <TextAreaField
        autoCapitalize="none"
        autoComplete="off"
        error={error ? t(error) : undefined}
        hint={
          kind === "entra"
            ? t("schoolSSO.orgIDsEntraHint")
            : t("schoolSSO.orgIDsGoogleHint")
        }
        label={
          kind === "entra"
            ? t("schoolSSO.orgIDsEntraLabel")
            : t("schoolSSO.orgIDsGoogleLabel")
        }
        onChange={(event) => {
          setValue(event.target.value);
          setError(null);
        }}
        ref={ref}
        rows={2}
        spellCheck={false}
        value={value}
      />
      <div className="school-sso__actions">
        <Button
          disabled={busy}
          leadingIcon={<Save />}
          size="sm"
          type="submit"
          variant="secondary"
        >
          {t("schoolSSO.saveOrgIDs")}
        </Button>
      </div>
    </form>
  );
}

function JoinPolicyForm({
  busy,
  connection,
  onSave,
}: {
  busy: boolean;
  connection: SSOConnection;
  onSave: (policy: JoinPolicy) => void;
}) {
  const { t } = useI18n();
  const saved: JoinPolicy =
    connection.auto_join_role === "student" ? "student" : "invitation";
  const [policy, setPolicy] = useState<JoinPolicy>(saved);

  return (
    <form
      className="school-sso__form"
      onSubmit={(event) => {
        event.preventDefault();
        if (!busy) onSave(policy);
      }}
    >
      <SelectField
        ariaLabel={t("schoolSSO.joinPolicyLabel")}
        hint={t("schoolSSO.joinPolicyHint")}
        label={t("schoolSSO.joinPolicyLabel")}
        onValueChange={(value) =>
          setPolicy(value === "student" ? "student" : "invitation")
        }
        options={[
          { label: t("schoolSSO.joinInvitation"), value: "invitation" },
          { label: t("schoolSSO.joinStudent"), value: "student" },
        ]}
        value={policy}
      />
      <div className="school-sso__actions">
        <Button
          disabled={busy || policy === saved}
          leadingIcon={<Save />}
          size="sm"
          type="submit"
          variant="secondary"
        >
          {t("schoolSSO.saveJoinPolicy")}
        </Button>
      </div>
    </form>
  );
}

function DomainForm({
  busy,
  disabled,
  onAdd,
}: {
  busy: boolean;
  disabled: boolean;
  onAdd: (domain: string, done: () => void) => void;
}) {
  const { t } = useI18n();
  const [value, setValue] = useState("");
  const [error, setError] = useState(false);
  const ref = useRef<HTMLInputElement>(null);

  return (
    <form
      className="school-sso__form school-sso__domain-form"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        if (disabled) return;
        const domain = value.trim().toLowerCase();
        if (!isPlausibleDomain(domain)) {
          setError(true);
          ref.current?.focus();
          return;
        }
        setError(false);
        onAdd(domain, () => setValue(""));
      }}
    >
      <TextField
        autoCapitalize="none"
        autoComplete="off"
        error={error ? t("schoolSSO.domainInvalid") : undefined}
        hint={t("schoolSSO.domainHint")}
        inputMode="url"
        label={t("schoolSSO.domainLabel")}
        maxLength={253}
        onChange={(event) => {
          setValue(event.target.value);
          setError(false);
        }}
        ref={ref}
        spellCheck={false}
        value={value}
      />
      <div className="school-sso__actions">
        <Button
          disabled={disabled && !busy}
          leadingIcon={<Plus />}
          loading={busy}
          loadingLabel={t("schoolSSO.addDomain")}
          size="sm"
          type="submit"
          variant="secondary"
        >
          {t("schoolSSO.addDomain")}
        </Button>
      </div>
    </form>
  );
}

function ChallengeBox({
  challenge,
  onDismiss,
}: {
  challenge: SSOChallenge;
  onDismiss: () => void;
}) {
  const { t } = useI18n();
  const headingId = useId();
  const nameRef = useRef<HTMLInputElement>(null);
  const valueRef = useRef<HTMLInputElement>(null);
  const [copyStatus, setCopyStatus] = useState<"copied" | "manual" | null>(
    null,
  );

  const copy = async (
    text: string,
    input: RefObject<HTMLInputElement | null>,
  ) => {
    try {
      if (!navigator.clipboard?.writeText) {
        throw new Error("Clipboard API unavailable");
      }
      await navigator.clipboard.writeText(text);
      setCopyStatus("copied");
    } catch {
      input.current?.focus();
      input.current?.select();
      setCopyStatus("manual");
    }
  };

  return (
    <div
      aria-labelledby={headingId}
      className="school-sso__challenge"
      role="group"
    >
      <h5 id={headingId}>
        {t("schoolSSO.challengeTitle", { domain: challenge.domain.domain })}
      </h5>
      <p>{t("schoolSSO.challengeDescription")}</p>
      <div className="school-sso__challenge-field">
        <TextField
          label={t("schoolSSO.txtNameLabel")}
          readOnly
          ref={nameRef}
          spellCheck={false}
          value={challenge.txt_name}
        />
        <Button
          leadingIcon={<Copy />}
          onClick={() => void copy(challenge.txt_name, nameRef)}
          size="sm"
          variant="secondary"
        >
          {t("schoolSSO.copyName")}
        </Button>
      </div>
      <div className="school-sso__challenge-field">
        <TextField
          label={t("schoolSSO.txtValueLabel")}
          readOnly
          ref={valueRef}
          spellCheck={false}
          value={challenge.txt_value}
        />
        <Button
          leadingIcon={<Copy />}
          onClick={() => void copy(challenge.txt_value, valueRef)}
          size="sm"
          variant="secondary"
        >
          {t("schoolSSO.copyValue")}
        </Button>
      </div>
      <p className="school-sso__warning">{t("schoolSSO.challengeOneTime")}</p>
      <p aria-live="polite" className="school-sso__muted" role="status">
        {copyStatus
          ? t(
              copyStatus === "copied"
                ? "schoolSSO.copied"
                : "schoolSSO.copyManual",
            )
          : null}
      </p>
      <div className="school-sso__actions">
        <Button onClick={onDismiss} size="sm" variant="quiet">
          {t("schoolSSO.challengeDone")}
        </Button>
      </div>
    </div>
  );
}
