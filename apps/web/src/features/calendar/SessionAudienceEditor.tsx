import {
  APIRequestError,
  type ClassEnrollmentRole,
  type ClassRosterUser,
  type SessionAudience,
  type SessionParticipationRole,
} from "@tutorhub/api-client";
import { Button, TextField } from "@tutorhub/ui";
import { RefreshCw, Search, UsersRound } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import {
  participationIdempotencyKey,
  useActiveClassRosterForAudience,
  useReplaceParticipationAudience,
  type ClassSessionParticipationSource,
} from "../../app/classSessionParticipation";
import { useI18n, type TranslationKey } from "../../app/i18n";
import { shouldConcealTenantScopedData } from "../../app/tenantDataAccess";

const maximumAudienceSize = 128;

interface SessionAudienceEditorProps {
  audience: SessionAudience;
  classID: string;
  onReloadAudience: () => Promise<unknown>;
  source: ClassSessionParticipationSource;
  tenantID: string;
  userID: string;
}

interface RosterCandidate {
  role: "owner" | ClassEnrollmentRole;
  user: ClassRosterUser;
}

const rosterRoleKeys = {
  co_teacher: "classRoster.roleCoTeacher",
  owner: "classRoster.roleOwner",
  student: "classRoster.roleStudent",
  teaching_assistant: "classRoster.roleTeachingAssistant",
} as const satisfies Record<RosterCandidate["role"], TranslationKey>;

function selectionFromAudience(audience: SessionAudience) {
  return new Map(
    audience.attendees.map((attendee) => [
      attendee.user_id,
      attendee.participation_role,
    ]),
  );
}

function draftFromAudience(audience: SessionAudience) {
  return {
    responseRequested: audience.response_requested,
    revision: audience.audience_revision,
    selection: selectionFromAudience(audience),
  };
}

function canonicalSelection(
  selection: ReadonlyMap<string, SessionParticipationRole>,
) {
  return [...selection.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([id, role]) => `${id}:${role}`)
    .join("|");
}

export function SessionAudienceEditor({
  audience,
  classID,
  onReloadAudience,
  source,
  tenantID,
  userID,
}: SessionAudienceEditorProps) {
  const { t } = useI18n();
  const [searchDraft, setSearchDraft] = useState("");
  const [search, setSearch] = useState("");
  const [draft, setDraft] = useState(() => draftFromAudience(audience));
  const conflictButton = useRef<HTMLButtonElement>(null);
  const idempotencyKey = useRef<string | null>(null);
  const roster = useActiveClassRosterForAudience(
    tenantID,
    userID,
    classID,
    search,
    audience.viewer_access.can_manage_attendees,
  );
  const replaceAudience = useReplaceParticipationAudience(
    tenantID,
    userID,
    classID,
    source,
  );
  const conflict =
    replaceAudience.error instanceof APIRequestError &&
    replaceAudience.error.status === 409;
  const concealed =
    shouldConcealTenantScopedData(roster.error) ||
    shouldConcealTenantScopedData(replaceAudience.error);

  useEffect(() => {
    if (conflict) {
      conflictButton.current?.focus();
    }
  }, [conflict]);

  const candidates = useMemo(() => {
    const byUserID = new Map<string, RosterCandidate>();
    for (const page of roster.data?.pages ?? []) {
      byUserID.set(page.class_owner.user.id, {
        role: page.class_owner.class_role,
        user: page.class_owner.user,
      });
      for (const member of page.items) {
        if (member.enrollment.status !== "active") {
          continue;
        }
        byUserID.set(member.user.id, {
          role: member.enrollment.class_role,
          user: member.user,
        });
      }
    }
    return [...byUserID.values()].sort((left, right) =>
      left.user.display_name.localeCompare(right.user.display_name),
    );
  }, [roster.data?.pages]);

  if (!audience.viewer_access.can_manage_attendees) {
    return null;
  }

  if (concealed) {
    return (
      <div className="calendar-audience-editor__feedback" role="alert">
        <p>{t("calendar.audienceEditor.accessChanged")}</p>
      </div>
    );
  }

  const { responseRequested, selection } = draft;
  const initialSelection = canonicalSelection(selectionFromAudience(audience));
  const dirty =
    canonicalSelection(selection) !== initialSelection ||
    responseRequested !== audience.response_requested;
  const selectionLimitReached = selection.size >= maximumAudienceSize;

  const submitSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSearch(searchDraft);
  };

  const updateRole = (
    targetUserID: string,
    role: SessionParticipationRole,
    checked: boolean,
  ) => {
    replaceAudience.reset();
    idempotencyKey.current = null;
    setDraft((current) => {
      const selection = new Map(current.selection);
      if (checked) {
        selection.set(targetUserID, role);
      } else if (selection.get(targetUserID) === role) {
        selection.delete(targetUserID);
      }
      return { ...current, selection };
    });
  };

  const save = () => {
    if (!dirty || replaceAudience.isPending) {
      return;
    }
    idempotencyKey.current ??= participationIdempotencyKey("audience");
    replaceAudience.mutate({
      input: {
        attendees: [...selection.entries()]
          .sort(([left], [right]) => left.localeCompare(right))
          .map(([targetUserID, participationRole]) => ({
            participation_role: participationRole,
            user_id: targetUserID,
          })),
        expected_audience_revision: audience.audience_revision,
        idempotency_key: idempotencyKey.current,
        response_requested: responseRequested,
      },
    });
  };

  const reloadAfterConflict = async () => {
    replaceAudience.reset();
    idempotencyKey.current = null;
    await Promise.all([onReloadAudience(), roster.refetch()]);
  };

  return (
    <div className="calendar-audience-editor">
      <div className="calendar-audience-editor__heading">
        <div>
          <h4>{t("calendar.audienceEditor.title")}</h4>
          <p>{t("calendar.audienceEditor.description")}</p>
        </div>
        <span>
          <UsersRound aria-hidden="true" />
          {t("calendar.audienceEditor.selectedCount", {
            count: selection.size,
          })}
        </span>
      </div>

      <form
        aria-label={t("calendar.audienceEditor.searchForm")}
        className="calendar-audience-editor__search"
        onSubmit={submitSearch}
      >
        <TextField
          autoComplete="off"
          label={t("calendar.audienceEditor.searchLabel")}
          maxLength={200}
          onChange={(event) => setSearchDraft(event.target.value)}
          placeholder={t("calendar.audienceEditor.searchPlaceholder")}
          type="search"
          value={searchDraft}
        />
        <Button
          leadingIcon={<Search />}
          size="sm"
          type="submit"
          variant="secondary"
        >
          {t("calendar.audienceEditor.searchAction")}
        </Button>
      </form>

      <label className="calendar-audience-editor__response-requested">
        <input
          checked={responseRequested}
          onChange={(event) => {
            replaceAudience.reset();
            idempotencyKey.current = null;
            setDraft((current) => ({
              ...current,
              responseRequested: event.target.checked,
            }));
          }}
          type="checkbox"
        />
        <span>
          <strong>{t("calendar.audienceEditor.requestResponses")}</strong>
          <small>{t("calendar.audienceEditor.requestResponsesHint")}</small>
        </span>
      </label>

      {selectionLimitReached && (
        <p className="calendar-audience-editor__notice" role="status">
          {t("calendar.audienceEditor.limit", { count: maximumAudienceSize })}
        </p>
      )}

      {roster.isPending && (
        <p aria-live="polite">{t("calendar.audienceEditor.loading")}</p>
      )}

      {roster.isError && !concealed && (
        <div className="calendar-audience-editor__feedback" role="alert">
          <p>{t("calendar.audienceEditor.loadError")}</p>
          <Button
            leadingIcon={<RefreshCw />}
            onClick={() => void roster.refetch()}
            size="sm"
            variant="secondary"
          >
            {t("calendar.retry")}
          </Button>
        </div>
      )}

      {roster.isSuccess && candidates.length === 0 && (
        <p className="calendar-audience-editor__notice">
          {t("calendar.audienceEditor.empty")}
        </p>
      )}

      {candidates.length > 0 && (
        <ul
          aria-label={t("calendar.audienceEditor.rosterLabel")}
          className="calendar-audience-editor__list"
        >
          {candidates.map((candidate) => {
            const selectedRole = selection.get(candidate.user.id);
            const disabledByLimit =
              selectionLimitReached && selectedRole === undefined;
            return (
              <li key={candidate.user.id}>
                <span className="calendar-audience-editor__identity">
                  <strong>{candidate.user.display_name}</strong>
                  <small>{t(rosterRoleKeys[candidate.role])}</small>
                </span>
                <span
                  aria-label={t("calendar.audienceEditor.roleChoices", {
                    name: candidate.user.display_name,
                  })}
                  className="calendar-audience-editor__roles"
                  role="group"
                >
                  {(["required", "optional"] as const).map((role) => (
                    <label key={role}>
                      <input
                        checked={selectedRole === role}
                        disabled={disabledByLimit}
                        onChange={(event) =>
                          updateRole(
                            candidate.user.id,
                            role,
                            event.target.checked,
                          )
                        }
                        type="checkbox"
                      />
                      {t(
                        role === "required"
                          ? "calendar.participation.role.required"
                          : "calendar.participation.role.optional",
                      )}
                    </label>
                  ))}
                </span>
              </li>
            );
          })}
        </ul>
      )}

      {roster.hasNextPage && (
        <Button
          disabled={roster.isFetchingNextPage}
          onClick={() => void roster.fetchNextPage()}
          size="sm"
          variant="secondary"
        >
          {roster.isFetchingNextPage
            ? t("calendar.audienceEditor.loadingMore")
            : t("calendar.audienceEditor.loadMore")}
        </Button>
      )}

      <div className="calendar-audience-editor__actions">
        <Button
          disabled={!dirty || replaceAudience.isPending}
          onClick={save}
          size="sm"
        >
          {replaceAudience.isPending
            ? t("calendar.audienceEditor.saving")
            : t("calendar.audienceEditor.save")}
        </Button>
      </div>

      {replaceAudience.isSuccess && (
        <p aria-live="polite" className="calendar-participation__success">
          {t("calendar.audienceEditor.saved")}
        </p>
      )}
      {replaceAudience.isError && !concealed && (
        <div className="calendar-audience-editor__feedback" role="alert">
          <p>
            {conflict
              ? t("calendar.audienceEditor.conflict")
              : t("calendar.audienceEditor.saveError")}
          </p>
          {conflict && (
            <Button
              onClick={() => void reloadAfterConflict()}
              ref={conflictButton}
              size="sm"
              variant="secondary"
            >
              {t("calendar.audienceEditor.reload")}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
