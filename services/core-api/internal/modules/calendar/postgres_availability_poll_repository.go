package calendar

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	availabilityPollQueryTimeout = 10 * time.Second
	availabilityResponseTTL      = 30 * time.Minute
)

type availabilityPollDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresAvailabilityPollRepository struct {
	database      availabilityPollDatabase
	queryTimeout  time.Duration
	authorizer    policy.Authorizer
	controls      featurecontrol.Enforcer
	protector     *protecteddata.Protector
	classSessions ClassSessionOutcomeWriter
	webOrigin     string
	clock         func() time.Time
}

type pollAuthorization struct {
	OrganizationRole policy.OrganizationRole
	ClassRole        policy.ClassRole
	ClassActive      bool
	ClassMember      bool
	Owner            bool
	SafetyAdmin      bool
}

func NewPostgresAvailabilityPollRepository(
	database availabilityPollDatabase,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	controls featurecontrol.Enforcer,
	protector *protecteddata.Protector,
	classSessions ClassSessionOutcomeWriter,
	webOrigin string,
	clock func() time.Time,
) (*PostgresAvailabilityPollRepository, error) {
	if database == nil || authorizer == nil || controls == nil || protector == nil ||
		classSessions == nil || strings.TrimSpace(webOrigin) == "" {
		return nil, fmt.Errorf("availability poll PostgreSQL dependencies are required")
	}
	if queryTimeout <= 0 {
		queryTimeout = availabilityPollQueryTimeout
	}
	if clock == nil {
		clock = time.Now
	}
	return &PostgresAvailabilityPollRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer,
		controls: controls, protector: protector, classSessions: classSessions,
		webOrigin: strings.TrimRight(strings.TrimSpace(webOrigin), "/"), clock: clock,
	}, nil
}

func (repository *PostgresAvailabilityPollRepository) ListPolls(
	ctx context.Context,
	scope tenancy.Context,
	input ListAvailabilityPollsInput,
) ([]AvailabilityPoll, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, repository.unavailable("begin poll list", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	organizationRole, err := repository.requireActiveMembership(queryContext, transaction, scope)
	if err != nil {
		return nil, err
	}
	arguments := []any{scope.TenantID, scope.ActorID, organizationRole == policy.OrganizationRoleAdmin}
	statusPredicate := ""
	if input.Status != "" {
		arguments = append(arguments, input.Status)
		statusPredicate = fmt.Sprintf(" AND poll.status = $%d", len(arguments))
	}
	arguments = append(arguments, input.Limit)
	rows, err := transaction.Query(
		queryContext,
		`SELECT poll.id
FROM tutorhub.availability_polls AS poll
LEFT JOIN tutorhub.availability_poll_participants AS participant
  ON participant.tenant_id = poll.tenant_id
 AND participant.poll_id = poll.id
 AND participant.internal_user_id = $2
 AND participant.status = 'active'
LEFT JOIN tutorhub.classes AS class
  ON class.tenant_id = poll.tenant_id AND class.id = poll.class_id
LEFT JOIN tutorhub.class_enrollments AS enrollment
  ON enrollment.tenant_id = poll.tenant_id
 AND enrollment.class_id = poll.class_id
 AND enrollment.user_id = $2
 AND enrollment.status = 'active'
WHERE poll.tenant_id = $1
  AND (
      poll.owner_user_id = $2
      OR $3::boolean
      OR participant.id IS NOT NULL
      OR (
          poll.share_mode = 'class_members'
          AND (class.owner_user_id = $2 OR enrollment.user_id IS NOT NULL)
      )
  )`+statusPredicate+`
ORDER BY poll.updated_at DESC, poll.id
LIMIT $`+fmt.Sprintf("%d", len(arguments)),
		arguments...,
	)
	if err != nil {
		return nil, repository.unavailable("query poll list", err)
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0, input.Limit)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, repository.unavailable("scan poll list", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, repository.unavailable("iterate poll list", err)
	}
	polls := make([]AvailabilityPoll, 0, len(ids))
	for _, id := range ids {
		poll, _, err := repository.loadPoll(queryContext, transaction, scope, id, false)
		if errors.Is(err, ErrAvailabilityPollNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		polls = append(polls, poll)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, repository.unavailable("commit poll list", err)
	}
	return polls, nil
}

func (repository *PostgresAvailabilityPollRepository) CreatePoll(
	ctx context.Context,
	scope tenancy.Context,
	input CreateAvailabilityPollInput,
) (AvailabilityPoll, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("begin poll creation", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	now := repository.clock().UTC()
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	organizationRole, err := repository.requireActiveMembership(queryContext, transaction, scope)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if !repository.authorizeOrganizationAction(
		scope, organizationRole, policy.ActionAvailabilityPollCreate,
	) {
		return AvailabilityPoll{}, ErrAvailabilityPollAccessDenied
	}
	if err := acquireAvailabilityPollTransactionLock(
		queryContext,
		transaction,
		"poll-create:"+scope.TenantID.String()+":"+scope.ActorID.String()+":"+input.IdempotencyKey,
	); err != nil {
		return AvailabilityPoll{}, repository.unavailable("lock poll creation receipt", err)
	}
	fingerprint, err := pollRequestFingerprint(input)
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("fingerprint poll creation", err)
	}
	var existingID uuid.UUID
	var existingFingerprint []byte
	err = transaction.QueryRow(
		queryContext,
		`SELECT id, create_request_fingerprint
FROM tutorhub.availability_polls
WHERE tenant_id = $1 AND owner_user_id = $2 AND create_idempotency_key = $3`,
		scope.TenantID, scope.ActorID, input.IdempotencyKey,
	).Scan(&existingID, &existingFingerprint)
	if err == nil {
		if !hmac.Equal(existingFingerprint, fingerprint[:]) {
			return AvailabilityPoll{}, ErrAvailabilityPollIdempotencyConflict
		}
		poll, _, loadErr := repository.loadPoll(
			queryContext, transaction, scope, existingID, false,
		)
		if loadErr != nil {
			return AvailabilityPoll{}, loadErr
		}
		if commitErr := transaction.Commit(queryContext); commitErr != nil {
			return AvailabilityPoll{}, repository.unavailable("commit replayed poll creation", commitErr)
		}
		return poll, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPoll{}, repository.unavailable("read poll creation receipt", err)
	}
	if input.ClassID != nil {
		if _, err := repository.requireClassMemberView(
			queryContext, transaction, scope, organizationRole, *input.ClassID,
		); err != nil {
			return AvailabilityPoll{}, err
		}
	}
	if err := requireActivePollParticipants(
		queryContext, transaction, scope.TenantID, input.Participants,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	if err := acquireAvailabilityPollTransactionLock(
		queryContext,
		transaction,
		"active-poll-quota:"+scope.TenantID.String(),
	); err != nil {
		return AvailabilityPoll{}, repository.unavailable("lock active poll quota", err)
	}
	var activePolls int64
	if err := transaction.QueryRow(
		queryContext,
		`SELECT count(*) FROM tutorhub.availability_polls
WHERE tenant_id = $1 AND status IN ('draft', 'open', 'closed')`,
		scope.TenantID,
	).Scan(&activePolls); err != nil {
		return AvailabilityPoll{}, repository.unavailable("count active polls", err)
	}
	checks := []struct {
		key       featurecontrol.QuotaKey
		requested int64
	}{
		{featurecontrol.QuotaActiveAvailabilityPolls, activePolls + 1},
		{featurecontrol.QuotaAvailabilityPollRangeDays, pollRangeDayCount(input)},
		{featurecontrol.QuotaAvailabilityPollSlots, int64(len(input.Slots))},
		{featurecontrol.QuotaAvailabilityPollParticipants, int64(len(input.Participants))},
	}
	for _, check := range checks {
		if err := repository.controls.RequireQuotaAtMost(
			queryContext, transaction, scope.TenantID, check.key, check.requested,
		); err != nil {
			return AvailabilityPoll{}, err
		}
	}
	if _, err := repository.controls.ConsumeRateQuota(
		queryContext, transaction, scope.TenantID,
		featurecontrol.QuotaAvailabilityPollCreationsPerHour, now,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	poll, err := scanAvailabilityPoll(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.availability_polls (
    tenant_id, class_id, owner_user_id, title, description, timezone,
    range_start, range_end, working_day_start, working_day_end,
    duration_minutes, slot_granularity_minutes, deadline_at, share_mode,
    retention_until, create_idempotency_key, create_request_fingerprint,
    created_at, updated_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7::date, $8::date, $9::time, $10::time,
    $11, $12, $13, $14, $13 + interval '180 days', $15, $16, $17, $17
)
RETURNING id, public_id, class_id, owner_user_id, title, description,
          timezone, range_start::text, range_end::text,
          working_day_start::text, working_day_end::text,
          duration_minutes, slot_granularity_minutes, deadline_at,
          share_mode, status, version, selected_slot_id, outcome_type,
          outcome_id, created_at, updated_at`,
		scope.TenantID, nullableUUID(input.ClassID), scope.ActorID,
		input.Title, input.Description, input.Timezone, input.RangeStart, input.RangeEnd,
		input.WorkingDayStart, input.WorkingDayEnd, input.DurationMinutes,
		input.SlotGranularityMinutes, input.DeadlineAt.UTC(), input.ShareMode,
		input.IdempotencyKey, fingerprint[:], now,
	))
	if err != nil {
		return AvailabilityPoll{}, mapAvailabilityPollPostgresError("create poll", err)
	}
	for ordinal, slot := range input.Slots {
		if _, err := transaction.Exec(
			queryContext,
			`INSERT INTO tutorhub.availability_poll_slots (
    tenant_id, poll_id, starts_at, ends_at, ordinal, created_at
) VALUES ($1, $2, $3, $4, $5, $6)`,
			scope.TenantID, poll.ID, slot.StartsAt.UTC(), slot.EndsAt.UTC(), ordinal, now,
		); err != nil {
			return AvailabilityPoll{}, mapAvailabilityPollPostgresError("create poll slot", err)
		}
	}
	for _, participant := range input.Participants {
		if _, err := transaction.Exec(
			queryContext,
			`INSERT INTO tutorhub.availability_poll_participants (
    tenant_id, poll_id, kind, internal_user_id, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $5)`,
			scope.TenantID, poll.ID, participant.Kind,
			nullableUUID(participant.InternalUserID), now,
		); err != nil {
			return AvailabilityPoll{}, mapAvailabilityPollPostgresError("create poll participant", err)
		}
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, poll, scope.ActorID,
		"availability_poll.created.v1", nil, now,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	poll, _, err = repository.loadPoll(queryContext, transaction, scope, poll.ID, false)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPoll{}, repository.unavailable("commit poll creation", err)
	}
	return poll, nil
}

func (repository *PostgresAvailabilityPollRepository) GetPoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
) (AvailabilityPoll, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("begin poll read", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, false)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if poll.ViewerCapabilities.CanViewIndividual && !authorization.Owner {
		poll.Participants, err = loadPollParticipants(
			queryContext, transaction, scope.TenantID, pollID,
		)
		if err != nil {
			return AvailabilityPoll{}, err
		}
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPoll{}, repository.unavailable("commit poll read", err)
	}
	return poll, nil
}

func (repository *PostgresAvailabilityPollRepository) ListIndividualResponses(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input ListAvailabilityPollResponsesInput,
) (AvailabilityPollIndividualResponsePage, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPollIndividualResponsePage{}, repository.unavailable(
			"begin individual poll response list", err,
		)
	}
	defer rollbackAvailabilityPoll(transaction)
	poll, _, err := repository.loadPoll(queryContext, transaction, scope, pollID, false)
	if err != nil {
		return AvailabilityPollIndividualResponsePage{}, err
	}
	if !poll.ViewerCapabilities.CanViewIndividual {
		return AvailabilityPollIndividualResponsePage{}, ErrAvailabilityPollNotFound
	}
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPollIndividualResponsePage{}, err
	}
	after, limit, err := normalizeIndividualResponseListInput(scope, pollID, input)
	if err != nil {
		return AvailabilityPollIndividualResponsePage{}, err
	}
	responses, err := loadPollIndividualResponses(
		queryContext, transaction, scope.TenantID, pollID, after, limit+1,
	)
	if err != nil {
		return AvailabilityPollIndividualResponsePage{}, err
	}
	page := AvailabilityPollIndividualResponsePage{
		Responses: responses,
	}
	if len(responses) > limit {
		page.Responses = responses[:limit]
		nextCursor, err := encodeIndividualResponseCursor(
			scope, pollID, limit, page.Responses[len(page.Responses)-1],
		)
		if err != nil {
			return AvailabilityPollIndividualResponsePage{}, err
		}
		page.NextCursor = &nextCursor
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPollIndividualResponsePage{}, repository.unavailable(
			"commit individual poll response list", err,
		)
	}
	return page, nil
}

func (repository *PostgresAvailabilityPollRepository) UpdatePoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input UpdateAvailabilityPollInput,
) (AvailabilityPoll, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("begin poll update", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, true)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if !authorization.Owner {
		return AvailabilityPoll{}, ErrAvailabilityPollNotFound
	}
	if poll.Version != input.ExpectedVersion ||
		poll.Status == PollStatusFinalized || poll.Status == PollStatusCancelled {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	if input.ClassID != nil {
		if _, err := repository.requireClassMemberView(
			queryContext, transaction, scope, authorization.OrganizationRole, *input.ClassID,
		); err != nil {
			return AvailabilityPoll{}, err
		}
	}
	if err := requireActivePollParticipants(
		queryContext, transaction, scope.TenantID, input.Participants,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	var responseCount int
	if err := transaction.QueryRow(
		queryContext,
		`SELECT count(*) FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2`,
		scope.TenantID, pollID,
	).Scan(&responseCount); err != nil {
		return AvailabilityPoll{}, repository.unavailable("count poll responses", err)
	}
	structuralChanged := !pollStructureMatchesInput(poll, input.CreateAvailabilityPollInput)
	if responseCount > 0 && structuralChanged {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	if structuralChanged {
		var historicalSlots, historicalParticipants int64
		if err := transaction.QueryRow(
			queryContext,
			`SELECT
    (SELECT count(*) FROM tutorhub.availability_poll_slots
     WHERE tenant_id = $1 AND poll_id = $2),
    (SELECT count(*) FROM tutorhub.availability_poll_participants
     WHERE tenant_id = $1 AND poll_id = $2)`,
			scope.TenantID, pollID,
		).Scan(&historicalSlots, &historicalParticipants); err != nil {
			return AvailabilityPoll{}, repository.unavailable("count poll structural history", err)
		}
		if historicalSlots+int64(len(input.Slots)) > maximumPollSlotHistory ||
			historicalParticipants+int64(len(input.Participants)) > maximumPollParticipantHistory {
			return AvailabilityPoll{}, ErrAvailabilityPollConflict
		}
		currentRangeDays := pollRangeDayCount(CreateAvailabilityPollInput{
			RangeStart: poll.RangeStart, RangeEnd: poll.RangeEnd,
		})
		expansions := []struct {
			key       featurecontrol.QuotaKey
			current   int64
			requested int64
		}{
			{featurecontrol.QuotaAvailabilityPollRangeDays, currentRangeDays, pollRangeDayCount(input.CreateAvailabilityPollInput)},
			{featurecontrol.QuotaAvailabilityPollSlots, int64(len(poll.Slots)), int64(len(input.Slots))},
			{featurecontrol.QuotaAvailabilityPollParticipants, int64(len(poll.Participants)), int64(len(input.Participants))},
		}
		for _, expansion := range expansions {
			if expansion.requested <= expansion.current {
				continue
			}
			if err := repository.controls.RequireQuotaAtMost(
				queryContext, transaction, scope.TenantID, expansion.key, expansion.requested,
			); err != nil {
				return AvailabilityPoll{}, err
			}
		}
	}
	now := repository.clock().UTC()
	if poll.ShareMode != input.ShareMode || structuralChanged {
		if _, err := transaction.Exec(
			queryContext,
			`UPDATE tutorhub.availability_poll_capabilities
SET revoked_at = $3, revoked_by = $4
WHERE tenant_id = $1 AND poll_id = $2 AND revoked_at IS NULL`,
			scope.TenantID, pollID, now, scope.ActorID,
		); err != nil {
			return AvailabilityPoll{}, repository.unavailable("revoke changed poll capabilities", err)
		}
	}
	if structuralChanged {
		if _, err := transaction.Exec(
			queryContext,
			`UPDATE tutorhub.availability_poll_participants
SET status = 'revoked', updated_at = $3
WHERE tenant_id = $1 AND poll_id = $2 AND status = 'active'`,
			scope.TenantID, pollID, now,
		); err != nil {
			return AvailabilityPoll{}, repository.unavailable("retire poll participants", err)
		}
		if _, err := transaction.Exec(
			queryContext,
			`UPDATE tutorhub.availability_poll_slots
SET retired_at = $3
WHERE tenant_id = $1 AND poll_id = $2 AND retired_at IS NULL`,
			scope.TenantID, pollID, now,
		); err != nil {
			return AvailabilityPoll{}, repository.unavailable("retire poll slots", err)
		}
		for ordinal, slot := range input.Slots {
			if _, err := transaction.Exec(
				queryContext,
				`INSERT INTO tutorhub.availability_poll_slots
    (tenant_id, poll_id, starts_at, ends_at, ordinal, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`,
				scope.TenantID, pollID, slot.StartsAt.UTC(), slot.EndsAt.UTC(), ordinal, now,
			); err != nil {
				return AvailabilityPoll{}, mapAvailabilityPollPostgresError("replace poll slot", err)
			}
		}
		for _, participant := range input.Participants {
			if _, err := transaction.Exec(
				queryContext,
				`INSERT INTO tutorhub.availability_poll_participants
    (tenant_id, poll_id, kind, internal_user_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)`,
				scope.TenantID, pollID, participant.Kind,
				nullableUUID(participant.InternalUserID), now,
			); err != nil {
				return AvailabilityPoll{}, mapAvailabilityPollPostgresError("replace poll participant", err)
			}
		}
	}
	updated, err := scanAvailabilityPoll(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_polls
SET class_id = $4, title = $5, description = $6, timezone = $7,
    range_start = $8::date, range_end = $9::date,
    working_day_start = $10::time, working_day_end = $11::time,
    duration_minutes = $12, slot_granularity_minutes = $13,
    deadline_at = $14, share_mode = $15, version = version + 1,
    retention_until = $14 + interval '180 days', updated_at = $16
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, public_id, class_id, owner_user_id, title, description,
          timezone, range_start::text, range_end::text,
          working_day_start::text, working_day_end::text,
          duration_minutes, slot_granularity_minutes, deadline_at,
          share_mode, status, version, selected_slot_id, outcome_type,
          outcome_id, created_at, updated_at`,
		scope.TenantID, pollID, input.ExpectedVersion, nullableUUID(input.ClassID),
		input.Title, input.Description, input.Timezone, input.RangeStart, input.RangeEnd,
		input.WorkingDayStart, input.WorkingDayEnd, input.DurationMinutes,
		input.SlotGranularityMinutes, input.DeadlineAt.UTC(), input.ShareMode, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	if err != nil {
		return AvailabilityPoll{}, mapAvailabilityPollPostgresError("update poll", err)
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID,
		"availability_poll.updated.v1", nil, now,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	updated, _, err = repository.loadPoll(queryContext, transaction, scope, pollID, false)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPoll{}, repository.unavailable("commit poll update", err)
	}
	return updated, nil
}

func (repository *PostgresAvailabilityPollRepository) OpenPoll(
	ctx context.Context, scope tenancy.Context, pollID uuid.UUID, expectedVersion int64,
) (AvailabilityPoll, error) {
	return repository.transitionPoll(
		ctx, scope, pollID, expectedVersion, PollStatusDraft, PollStatusOpen,
		"availability_poll.opened.v1", nil,
	)
}

func (repository *PostgresAvailabilityPollRepository) ClosePoll(
	ctx context.Context, scope tenancy.Context, pollID uuid.UUID, expectedVersion int64,
) (AvailabilityPoll, error) {
	return repository.transitionPoll(
		ctx, scope, pollID, expectedVersion, PollStatusOpen, PollStatusClosed,
		"availability_poll.closed.v1", nil,
	)
}

func (repository *PostgresAvailabilityPollRepository) ReopenPoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	expectedVersion int64,
	deadlineAt time.Time,
) (AvailabilityPoll, error) {
	return repository.transitionPoll(
		ctx, scope, pollID, expectedVersion, PollStatusClosed, PollStatusOpen,
		"availability_poll.reopened.v1", &deadlineAt,
	)
}

func (repository *PostgresAvailabilityPollRepository) CancelPoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	expectedVersion int64,
	reason string,
) (AvailabilityPoll, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("begin poll cancellation", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, true)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if !authorization.Owner && !authorization.SafetyAdmin {
		return AvailabilityPoll{}, ErrAvailabilityPollNotFound
	}
	if poll.Status == PollStatusCancelled {
		if poll.Version != expectedVersion && poll.Version != expectedVersion+1 {
			return AvailabilityPoll{}, ErrAvailabilityPollConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return AvailabilityPoll{}, repository.unavailable("commit replayed poll cancellation", err)
		}
		return poll, nil
	}
	if poll.Version != expectedVersion {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	if poll.Status == PollStatusFinalized {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	now := repository.clock().UTC()
	updated, err := scanAvailabilityPoll(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_polls
SET status = 'cancelled', version = version + 1,
    retention_until = $4 + interval '180 days', updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, public_id, class_id, owner_user_id, title, description,
          timezone, range_start::text, range_end::text,
          working_day_start::text, working_day_end::text,
          duration_minutes, slot_granularity_minutes, deadline_at,
          share_mode, status, version, selected_slot_id, outcome_type,
          outcome_id, created_at, updated_at`,
		scope.TenantID, pollID, expectedVersion, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("cancel poll", err)
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.availability_poll_capabilities
SET revoked_at = $3, revoked_by = $4
WHERE tenant_id = $1 AND poll_id = $2 AND revoked_at IS NULL`,
		scope.TenantID, pollID, now, scope.ActorID,
	); err != nil {
		return AvailabilityPoll{}, repository.unavailable("revoke cancelled poll capabilities", err)
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID,
		"availability_poll.cancelled.v1", audit.Metadata{"reason": reason}, now,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	updated, _, err = repository.loadPoll(queryContext, transaction, scope, pollID, false)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPoll{}, repository.unavailable("commit poll cancellation", err)
	}
	return updated, nil
}

func (repository *PostgresAvailabilityPollRepository) transitionPoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	expectedVersion int64,
	fromStatus string,
	toStatus string,
	eventType string,
	deadlineAt *time.Time,
) (AvailabilityPoll, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("begin poll transition", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, true)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if !authorization.Owner && !(authorization.SafetyAdmin && toStatus == PollStatusClosed) {
		return AvailabilityPoll{}, ErrAvailabilityPollNotFound
	}
	if poll.Status == toStatus {
		sameDeadline := deadlineAt == nil || poll.DeadlineAt.Equal(deadlineAt.UTC())
		if !sameDeadline || (poll.Version != expectedVersion && poll.Version != expectedVersion+1) {
			return AvailabilityPoll{}, ErrAvailabilityPollConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return AvailabilityPoll{}, repository.unavailable("commit replayed poll transition", err)
		}
		return poll, nil
	}
	if poll.Version != expectedVersion {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	if poll.Status != fromStatus {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	now := repository.clock().UTC()
	newDeadline := poll.DeadlineAt
	if deadlineAt != nil {
		newDeadline = deadlineAt.UTC()
	}
	if toStatus == PollStatusOpen && !newDeadline.After(now) {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	updated, err := scanAvailabilityPoll(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_polls
SET status = $4, deadline_at = $5, version = version + 1,
    retention_until = $5 + interval '180 days', updated_at = $6
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, public_id, class_id, owner_user_id, title, description,
          timezone, range_start::text, range_end::text,
          working_day_start::text, working_day_end::text,
          duration_minutes, slot_granularity_minutes, deadline_at,
          share_mode, status, version, selected_slot_id, outcome_type,
          outcome_id, created_at, updated_at`,
		scope.TenantID, pollID, expectedVersion, toStatus, newDeadline, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPoll{}, ErrAvailabilityPollConflict
	}
	if err != nil {
		return AvailabilityPoll{}, repository.unavailable("transition poll", err)
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID, eventType, nil, now,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	updated, _, err = repository.loadPoll(queryContext, transaction, scope, pollID, false)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPoll{}, repository.unavailable("commit poll transition", err)
	}
	return updated, nil
}

func (repository *PostgresAvailabilityPollRepository) Respond(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input RespondAvailabilityPollInput,
) (AvailabilityPollMutationResponse, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("begin poll response", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, true)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	actorKey := "user:" + scope.ActorID.String()
	fingerprint, err := pollRequestFingerprint(input)
	if err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("fingerprint poll response", err)
	}
	if replayed, err := repository.loadMutationReceipt(
		queryContext, transaction, scope.TenantID, pollID, "respond", actorKey,
		input.IdempotencyKey, fingerprint,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	} else if replayed {
		result, loadErr := repository.buildMutationResponse(
			queryContext, transaction, scope, pollID, authorization,
		)
		if loadErr != nil {
			return AvailabilityPollMutationResponse{}, loadErr
		}
		if commitErr := transaction.Commit(queryContext); commitErr != nil {
			return AvailabilityPollMutationResponse{}, repository.unavailable("commit replayed response", commitErr)
		}
		return result, nil
	}
	if !poll.ViewerCapabilities.CanRespond || poll.Status != PollStatusOpen ||
		!poll.DeadlineAt.After(repository.clock().UTC()) {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollClosed
	}
	if err := validateAnswerSlotSet(poll.Slots, input.Answers); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	participantID, err := findInternalPollParticipant(
		queryContext, transaction, scope.TenantID, pollID, scope.ActorID,
	)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	now := repository.clock().UTC()
	responseID, responseVersion, err := repository.upsertPollResponse(
		queryContext, transaction, scope.TenantID, pollID, participantID,
		&scope.ActorID, nil, input.ExpectedResponseVersion, now,
	)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if err := replacePollAnswers(
		queryContext, transaction, scope.TenantID, pollID, responseID, input.Answers, now,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if err := repository.insertMutationReceipt(
		queryContext, transaction, scope.TenantID, pollID, "respond", actorKey,
		input.IdempotencyKey, fingerprint, responseVersion, "", uuid.Nil, now,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, poll, scope.ActorID,
		"availability_poll.response_recorded.v1",
		audit.Metadata{"response_version": fmt.Sprintf("%d", responseVersion)}, now,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	result, err := repository.buildMutationResponse(
		queryContext, transaction, scope, pollID, authorization,
	)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("commit poll response", err)
	}
	return result, nil
}

func (repository *PostgresAvailabilityPollRepository) Summary(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
) (AvailabilityPollSummary, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPollSummary{}, repository.unavailable("begin poll summary", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	poll, _, err := repository.loadPoll(queryContext, transaction, scope, pollID, false)
	if err != nil {
		return AvailabilityPollSummary{}, err
	}
	summary, err := repository.loadSummary(
		queryContext, transaction, scope.TenantID, poll,
		poll.ViewerCapabilities.CanViewExactAggregate, true,
	)
	if err != nil {
		return AvailabilityPollSummary{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPollSummary{}, repository.unavailable("commit poll summary", err)
	}
	return summary, nil
}

func (repository *PostgresAvailabilityPollRepository) Finalize(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input FinalizeAvailabilityPollInput,
) (AvailabilityPollMutationResponse, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("begin poll finalize", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, true)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if !authorization.Owner {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollNotFound
	}
	actorKey := "user:" + scope.ActorID.String()
	fingerprint, err := pollRequestFingerprint(input)
	if err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("fingerprint poll finalize", err)
	}
	if replayed, err := repository.loadMutationReceipt(
		queryContext, transaction, scope.TenantID, pollID, "finalize", actorKey,
		input.IdempotencyKey, fingerprint,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	} else if replayed {
		result, loadErr := repository.buildMutationResponse(
			queryContext, transaction, scope, pollID, authorization,
		)
		if loadErr != nil {
			return AvailabilityPollMutationResponse{}, loadErr
		}
		if commitErr := transaction.Commit(queryContext); commitErr != nil {
			return AvailabilityPollMutationResponse{}, repository.unavailable("commit replayed finalize", commitErr)
		}
		return result, nil
	}
	if poll.Version != input.ExpectedVersion || poll.Status != PollStatusClosed {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollConflict
	}
	var slot AvailabilityPollSlot
	if err := transaction.QueryRow(
		queryContext,
		`SELECT id, starts_at, ends_at, ordinal
FROM tutorhub.availability_poll_slots
WHERE tenant_id = $1 AND poll_id = $2 AND id = $3
  AND retired_at IS NULL`,
		scope.TenantID, pollID, input.SlotID,
	).Scan(&slot.ID, &slot.StartsAt, &slot.EndsAt, &slot.Ordinal); errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollInvalid
	} else if err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("load finalized poll slot", err)
	}
	now := repository.clock().UTC()
	var outcomeID uuid.UUID
	switch input.OutcomeType {
	case PollOutcomeStudyMeeting:
		if !poll.ViewerCapabilities.CanFinalizeStudyMeeting {
			return AvailabilityPollMutationResponse{}, ErrAvailabilityPollAccessDenied
		}
		if err := repository.requireStudyMeetingCreationQuota(
			queryContext, transaction, scope.TenantID, slot.EndsAt, now,
		); err != nil {
			return AvailabilityPollMutationResponse{}, err
		}
		if err := requireNoStudyMeetingConflict(
			queryContext, transaction, scope.TenantID, scope.ActorID, uuid.Nil,
			slot.StartsAt, slot.EndsAt,
		); err != nil {
			return AvailabilityPollMutationResponse{}, err
		}
		meeting, err := scanStudyMeeting(transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.study_meetings (
    tenant_id, owner_user_id, source_poll_id, title, starts_at, ends_at,
    timezone, create_idempotency_key, create_request_fingerprint,
    created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING id, class_id, owner_user_id, source_poll_id, title, starts_at,
          ends_at, timezone, status, version, cancelled_at, created_at, updated_at`,
			scope.TenantID, scope.ActorID, pollID, poll.Title, slot.StartsAt,
			slot.EndsAt, poll.Timezone, input.IdempotencyKey, fingerprint[:], now,
		))
		if err != nil {
			return AvailabilityPollMutationResponse{}, mapAvailabilityPollPostgresError(
				"create finalized study meeting", err,
			)
		}
		outcomeID = meeting.ID
		if err := appendStudyMeetingEvent(
			queryContext, transaction, scope.TenantID, meeting, scope.ActorID,
			"study_meeting.scheduled.v1", nil, now,
		); err != nil {
			return AvailabilityPollMutationResponse{}, err
		}
	case PollOutcomeClassSession:
		if !poll.ViewerCapabilities.CanFinalizeClassSession || input.ClassID == nil {
			return AvailabilityPollMutationResponse{}, ErrAvailabilityPollAccessDenied
		}
		if poll.ClassID != nil && *poll.ClassID != *input.ClassID {
			return AvailabilityPollMutationResponse{}, ErrAvailabilityPollNotFound
		}
		outcomeID, err = repository.classSessions.CreatePollOutcomeInTransaction(
			queryContext, transaction, scope, *input.ClassID, ClassSessionOutcomeInput{
				Title: poll.Title, Description: poll.Description, StartsAt: slot.StartsAt,
				EndsAt: slot.EndsAt, Timezone: poll.Timezone, CreatedAt: now,
			},
		)
		if err != nil {
			switch {
			case errors.Is(err, ErrClassSessionOutcomeNotFound):
				return AvailabilityPollMutationResponse{}, ErrAvailabilityPollNotFound
			case errors.Is(err, ErrClassSessionOutcomeAccessDenied):
				return AvailabilityPollMutationResponse{}, ErrAvailabilityPollNotFound
			case errors.Is(err, ErrClassSessionOutcomeConflict):
				return AvailabilityPollMutationResponse{}, ErrStudyMeetingConflict
			default:
				return AvailabilityPollMutationResponse{}, err
			}
		}
	}
	updated, err := scanAvailabilityPoll(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_polls
SET status = 'finalized', selected_slot_id = $4, outcome_type = $5,
    outcome_id = $6, version = version + 1,
    retention_until = $7 + interval '180 days', updated_at = $7
WHERE tenant_id = $1 AND id = $2 AND version = $3 AND status = 'closed'
RETURNING id, public_id, class_id, owner_user_id, title, description,
          timezone, range_start::text, range_end::text,
          working_day_start::text, working_day_end::text,
          duration_minutes, slot_granularity_minutes, deadline_at,
          share_mode, status, version, selected_slot_id, outcome_type,
          outcome_id, created_at, updated_at`,
		scope.TenantID, pollID, input.ExpectedVersion, input.SlotID,
		input.OutcomeType, outcomeID, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollConflict
	}
	if err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("finalize poll", err)
	}
	if err := repository.insertMutationReceipt(
		queryContext, transaction, scope.TenantID, pollID, "finalize", actorKey,
		input.IdempotencyKey, fingerprint, updated.Version,
		input.OutcomeType, outcomeID, now,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID,
		"availability_poll.finalized.v1",
		audit.Metadata{"outcome_type": input.OutcomeType, "outcome_id": outcomeID.String()}, now,
	); err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	result, err := repository.buildMutationResponse(
		queryContext, transaction, scope, pollID, authorization,
	)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPollMutationResponse{}, repository.unavailable("commit poll finalize", err)
	}
	return result, nil
}

func (repository *PostgresAvailabilityPollRepository) CreateCapability(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input CreateAvailabilityPollCapabilityInput,
) (AvailabilityPollCapabilitySecret, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPollCapabilitySecret{}, repository.unavailable("begin capability creation", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return AvailabilityPollCapabilitySecret{}, err
	}
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, true)
	if err != nil {
		return AvailabilityPollCapabilitySecret{}, err
	}
	if !authorization.Owner ||
		poll.Version != input.ExpectedVersion || poll.Status == PollStatusFinalized ||
		poll.Status == PollStatusCancelled ||
		(input.Scope == PollCapabilityInvited && poll.ShareMode != PollShareInvitedOnly) ||
		(input.Scope == PollCapabilityPublic && poll.ShareMode != PollShareAnyoneWithLink) {
		return AvailabilityPollCapabilitySecret{}, ErrAvailabilityPollConflict
	}
	if input.ParticipantID != nil {
		var active bool
		if err := transaction.QueryRow(
			queryContext,
			`SELECT status = 'active'
FROM tutorhub.availability_poll_participants
WHERE tenant_id = $1 AND poll_id = $2 AND id = $3`,
			scope.TenantID, pollID, *input.ParticipantID,
		).Scan(&active); errors.Is(err, pgx.ErrNoRows) || !active {
			return AvailabilityPollCapabilitySecret{}, ErrAvailabilityPollNotFound
		} else if err != nil {
			return AvailabilityPollCapabilitySecret{}, repository.unavailable("load capability participant", err)
		}
	}
	now := repository.clock().UTC()
	var activeCapabilities, totalCapabilities int64
	if err := transaction.QueryRow(
		queryContext,
		`SELECT
    count(*) FILTER (WHERE revoked_at IS NULL AND expires_at > $3),
    count(*)
FROM tutorhub.availability_poll_capabilities
WHERE tenant_id = $1 AND poll_id = $2 AND purpose = 'poll_access'`,
		scope.TenantID, pollID, now,
	).Scan(&activeCapabilities, &totalCapabilities); err != nil {
		return AvailabilityPollCapabilitySecret{}, repository.unavailable(
			"count poll access capabilities", err,
		)
	}
	if activeCapabilities >= maximumPollParticipants {
		return AvailabilityPollCapabilitySecret{}, ErrAvailabilityPollCapacityExceeded
	}
	if _, err := repository.controls.ConsumeRateQuota(
		queryContext, transaction, scope.TenantID,
		featurecontrol.QuotaAvailabilityPollCapabilityCreationsPerHour, now,
	); err != nil {
		return AvailabilityPollCapabilitySecret{}, err
	}
	token, err := generatePollCapabilityToken(repository.protector)
	if err != nil {
		return AvailabilityPollCapabilitySecret{}, err
	}
	capability, err := scanAvailabilityPollCapability(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_poll_capabilities AS reusable
SET participant_id = $3, scope = $4, token_version = $5, token_digest = $6,
    expires_at = $7, revoked_at = NULL, revoked_by = NULL,
    use_count = 0, last_used_at = NULL, created_by = $8, created_at = $9
WHERE reusable.id = (
    SELECT candidate.id
    FROM tutorhub.availability_poll_capabilities AS candidate
    WHERE candidate.tenant_id = $1 AND candidate.poll_id = $2
      AND candidate.purpose = 'poll_access'
      AND (candidate.revoked_at IS NOT NULL OR candidate.expires_at <= $9)
      AND NOT EXISTS (
          SELECT 1
          FROM tutorhub.availability_poll_capabilities AS child
          WHERE child.tenant_id = candidate.tenant_id
            AND child.poll_id = candidate.poll_id
            AND child.parent_capability_id = candidate.id
      )
    ORDER BY candidate.expires_at, candidate.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING id, poll_id, participant_id, scope, expires_at, revoked_at, created_at`,
		scope.TenantID, pollID, nullableUUID(input.ParticipantID), input.Scope,
		token.Version, token.Digest[:], input.ExpiresAt.UTC(), scope.ActorID, now,
	))
	if errors.Is(err, pgx.ErrNoRows) && totalCapabilities >= maximumPollAccessCapabilities {
		return AvailabilityPollCapabilitySecret{}, ErrAvailabilityPollCapacityExceeded
	}
	if errors.Is(err, pgx.ErrNoRows) {
		capability, err = scanAvailabilityPollCapability(transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.availability_poll_capabilities (
    tenant_id, poll_id, participant_id, purpose, scope, token_version,
    token_digest, expires_at, created_by, created_at
)
VALUES ($1, $2, $3, 'poll_access', $4, $5, $6, $7, $8, $9)
RETURNING id, poll_id, participant_id, scope, expires_at, revoked_at, created_at`,
			scope.TenantID, pollID, nullableUUID(input.ParticipantID), input.Scope,
			token.Version, token.Digest[:], input.ExpiresAt.UTC(), scope.ActorID, now,
		))
	}
	if err != nil {
		return AvailabilityPollCapabilitySecret{}, mapAvailabilityPollPostgresError("create capability", err)
	}
	updated, err := scanAvailabilityPoll(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_polls
SET version = version + 1, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, public_id, class_id, owner_user_id, title, description,
          timezone, range_start::text, range_end::text,
          working_day_start::text, working_day_end::text,
          duration_minutes, slot_granularity_minutes, deadline_at,
          share_mode, status, version, selected_slot_id, outcome_type,
          outcome_id, created_at, updated_at`,
		scope.TenantID, pollID, input.ExpectedVersion, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPollCapabilitySecret{}, ErrAvailabilityPollConflict
	}
	if err != nil {
		return AvailabilityPollCapabilitySecret{}, repository.unavailable("version capability creation", err)
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID,
		"availability_poll.capability_issued.v1",
		audit.Metadata{"scope": input.Scope}, now,
	); err != nil {
		return AvailabilityPollCapabilitySecret{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPollCapabilitySecret{}, repository.unavailable("commit capability creation", err)
	}
	return AvailabilityPollCapabilitySecret{
		Capability: capability,
		RawToken:   token.Raw,
		ShareURL: repository.webOrigin + "/availability/" + poll.PublicID.String() +
			"#token=" + url.QueryEscape(token.Raw),
	}, nil
}

func (repository *PostgresAvailabilityPollRepository) RevokeCapability(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	capabilityID uuid.UUID,
	expectedVersion int64,
	reason string,
) (AvailabilityPollCapability, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return AvailabilityPollCapability{}, repository.unavailable("begin capability revocation", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	poll, authorization, err := repository.loadPoll(queryContext, transaction, scope, pollID, true)
	if err != nil {
		return AvailabilityPollCapability{}, err
	}
	if !authorization.Owner && !authorization.SafetyAdmin {
		return AvailabilityPollCapability{}, ErrAvailabilityPollConflict
	}
	now := repository.clock().UTC()
	capability, err := scanAvailabilityPollCapability(transaction.QueryRow(
		queryContext,
		`SELECT id, poll_id, participant_id, scope, expires_at, revoked_at, created_at
FROM tutorhub.availability_poll_capabilities
WHERE tenant_id = $1 AND poll_id = $2 AND id = $3 AND purpose = 'poll_access'
	FOR UPDATE`,
		scope.TenantID, pollID, capabilityID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPollCapability{}, ErrAvailabilityPollNotFound
	}
	if err != nil {
		return AvailabilityPollCapability{}, repository.unavailable("load capability for revocation", err)
	}
	if capability.RevokedAt != nil {
		if poll.Version != expectedVersion && poll.Version != expectedVersion+1 {
			return AvailabilityPollCapability{}, ErrAvailabilityPollConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return AvailabilityPollCapability{}, repository.unavailable(
				"commit replayed capability revocation", err,
			)
		}
		return capability, nil
	}
	if poll.Version != expectedVersion {
		return AvailabilityPollCapability{}, ErrAvailabilityPollConflict
	}
	capability, err = scanAvailabilityPollCapability(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_poll_capabilities
SET revoked_at = $4, revoked_by = $5
WHERE tenant_id = $1 AND poll_id = $2 AND id = $3
  AND purpose = 'poll_access' AND revoked_at IS NULL
RETURNING id, poll_id, participant_id, scope, expires_at, revoked_at, created_at`,
		scope.TenantID, pollID, capabilityID, now, scope.ActorID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPollCapability{}, ErrAvailabilityPollConflict
	}
	if err != nil {
		return AvailabilityPollCapability{}, repository.unavailable("revoke capability", err)
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.availability_poll_capabilities
SET revoked_at = COALESCE(revoked_at, $4),
    revoked_by = CASE WHEN revoked_at IS NULL THEN $5 ELSE revoked_by END
WHERE tenant_id = $1 AND poll_id = $2 AND parent_capability_id = $3`,
		scope.TenantID, pollID, capabilityID, now, scope.ActorID,
	); err != nil {
		return AvailabilityPollCapability{}, repository.unavailable("revoke child response sessions", err)
	}
	updated, err := scanAvailabilityPoll(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_polls
SET version = version + 1, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, public_id, class_id, owner_user_id, title, description,
          timezone, range_start::text, range_end::text,
          working_day_start::text, working_day_end::text,
          duration_minutes, slot_granularity_minutes, deadline_at,
          share_mode, status, version, selected_slot_id, outcome_type,
          outcome_id, created_at, updated_at`,
		scope.TenantID, pollID, expectedVersion, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPollCapability{}, ErrAvailabilityPollConflict
	}
	if err != nil {
		return AvailabilityPollCapability{}, repository.unavailable("version capability revocation", err)
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID,
		"availability_poll.capability_revoked.v1",
		audit.Metadata{
			"capability_id": capability.ID.String(), "scope": capability.Scope, "reason": reason,
		},
		now,
	); err != nil {
		return AvailabilityPollCapability{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return AvailabilityPollCapability{}, repository.unavailable("commit capability revocation", err)
	}
	return capability, nil
}

func (repository *PostgresAvailabilityPollRepository) ResolvePublic(
	ctx context.Context,
	publicID uuid.UUID,
	rawToken string,
) (PublicAvailabilityPollExchange, error) {
	version, digest, err := digestPollCapabilityToken(repository.protector, rawToken)
	if err != nil {
		return PublicAvailabilityPollExchange{}, err
	}
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return PublicAvailabilityPollExchange{}, repository.unavailable("begin public poll resolve", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	now := repository.clock().UTC()
	preflightCapability, _, err := repository.loadPublicCapability(
		queryContext, transaction, publicID, version, digest[:], "poll_access", now, false,
	)
	if err != nil {
		return PublicAvailabilityPollExchange{}, err
	}
	if err := repository.controls.RequireFeature(
		queryContext,
		transaction,
		preflightCapability.TenantID,
		featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		if errors.Is(err, featurecontrol.ErrFeatureDisabled) {
			return PublicAvailabilityPollExchange{}, ErrAvailabilityPollCapabilityUnavailable
		}
		return PublicAvailabilityPollExchange{}, repository.unavailable(
			"enforce public poll feature", err,
		)
	}
	capability, poll, err := repository.loadPublicCapability(
		queryContext, transaction, publicID, version, digest[:], "poll_access", now, true,
	)
	if err != nil {
		return PublicAvailabilityPollExchange{}, err
	}
	if poll.Status != PollStatusOpen || !poll.DeadlineAt.After(now) ||
		poll.ShareMode == PollShareClassMembers ||
		(poll.ShareMode == PollShareInvitedOnly && capability.Scope != PollCapabilityInvited) ||
		(poll.ShareMode == PollShareAnyoneWithLink && capability.Scope != PollCapabilityPublic) {
		return PublicAvailabilityPollExchange{}, ErrAvailabilityPollCapabilityUnavailable
	}
	var activeResponseSessions, totalResponseSessions int64
	if err := transaction.QueryRow(
		queryContext,
		`SELECT
    count(*) FILTER (WHERE revoked_at IS NULL AND expires_at > $3),
    count(*)
FROM tutorhub.availability_poll_capabilities
WHERE tenant_id = $1 AND poll_id = $2 AND purpose = 'response_session'`,
		capability.TenantID, capability.PollID, now,
	).Scan(&activeResponseSessions, &totalResponseSessions); err != nil {
		return PublicAvailabilityPollExchange{}, repository.unavailable(
			"count active public response sessions", err,
		)
	}
	if activeResponseSessions >= maximumPollResponseSessions {
		return PublicAvailabilityPollExchange{}, ErrAvailabilityPollCapabilityUnavailable
	}
	responseToken, err := generatePollCapabilityToken(repository.protector)
	if err != nil {
		return PublicAvailabilityPollExchange{}, err
	}
	expiresAt := now.Add(availabilityResponseTTL)
	if capability.ExpiresAt.Before(expiresAt) {
		expiresAt = capability.ExpiresAt
	}
	var responseCapabilityID uuid.UUID
	err = transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.availability_poll_capabilities AS reusable
SET parent_capability_id = $3, participant_id = $4, scope = $5,
    token_version = $6, token_digest = $7, expires_at = $8,
    revoked_at = NULL, revoked_by = NULL, use_count = 0, last_used_at = NULL,
    created_at = $9
WHERE reusable.id = (
    SELECT candidate.id
    FROM tutorhub.availability_poll_capabilities AS candidate
    WHERE candidate.tenant_id = $1 AND candidate.poll_id = $2
      AND candidate.purpose = 'response_session'
      AND (candidate.revoked_at IS NOT NULL OR candidate.expires_at <= $9)
      AND NOT EXISTS (
          SELECT 1
          FROM tutorhub.availability_poll_responses AS response
          WHERE response.tenant_id = candidate.tenant_id
            AND response.poll_id = candidate.poll_id
            AND response.response_capability_id = candidate.id
      )
    ORDER BY candidate.expires_at, candidate.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING reusable.id`,
		capability.TenantID, capability.PollID, capability.ID,
		nullableUUID(capability.ParticipantID), capability.Scope,
		responseToken.Version, responseToken.Digest[:], expiresAt, now,
	).Scan(&responseCapabilityID)
	if errors.Is(err, pgx.ErrNoRows) &&
		totalResponseSessions >= maximumPollResponseSessionHistory {
		return PublicAvailabilityPollExchange{}, ErrAvailabilityPollCapabilityUnavailable
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.availability_poll_capabilities (
    tenant_id, poll_id, parent_capability_id, participant_id, purpose, scope,
    token_version, token_digest, expires_at, created_at
)
VALUES ($1, $2, $3, $4, 'response_session', $5, $6, $7, $8, $9)
RETURNING id`,
			capability.TenantID, capability.PollID, capability.ID,
			nullableUUID(capability.ParticipantID), capability.Scope,
			responseToken.Version, responseToken.Digest[:], expiresAt, now,
		).Scan(&responseCapabilityID)
	}
	if err != nil {
		return PublicAvailabilityPollExchange{}, mapAvailabilityPollPostgresError(
			"create public response session", err,
		)
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.availability_poll_capabilities
SET use_count = use_count + 1, last_used_at = $4
WHERE tenant_id = $1 AND poll_id = $2 AND id = $3`,
		capability.TenantID, capability.PollID, capability.ID, now,
	); err != nil {
		return PublicAvailabilityPollExchange{}, repository.unavailable("record public capability use", err)
	}
	projection, err := repository.loadPublicPoll(
		queryContext, transaction, capability.TenantID, poll,
		responseCapabilityID, capability.ParticipantID,
	)
	if err != nil {
		return PublicAvailabilityPollExchange{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return PublicAvailabilityPollExchange{}, repository.unavailable("commit public poll resolve", err)
	}
	return PublicAvailabilityPollExchange{
		Poll: projection, ResponseToken: responseToken.Raw,
		ResponseTokenExpiresAt: expiresAt,
	}, nil
}

func (repository *PostgresAvailabilityPollRepository) RespondPublic(
	ctx context.Context,
	input RespondPublicAvailabilityPollInput,
) (PublicAvailabilityPoll, error) {
	version, digest, err := digestPollCapabilityToken(repository.protector, input.ResponseToken)
	if err != nil {
		return PublicAvailabilityPoll{}, err
	}
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return PublicAvailabilityPoll{}, repository.unavailable("begin public poll response", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	now := repository.clock().UTC()
	preflightCapability, _, err := repository.loadPublicCapability(
		queryContext, transaction, input.PublicID, version, digest[:],
		"response_session", now, false,
	)
	if err != nil {
		return PublicAvailabilityPoll{}, err
	}
	if err := repository.controls.RequireFeature(
		queryContext,
		transaction,
		preflightCapability.TenantID,
		featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		if errors.Is(err, featurecontrol.ErrFeatureDisabled) {
			return PublicAvailabilityPoll{}, ErrAvailabilityPollCapabilityUnavailable
		}
		return PublicAvailabilityPoll{}, repository.unavailable(
			"enforce public poll response feature", err,
		)
	}
	capability, poll, err := repository.loadPublicCapability(
		queryContext, transaction, input.PublicID, version, digest[:],
		"response_session", now, true,
	)
	if err != nil {
		return PublicAvailabilityPoll{}, err
	}
	actorKey := "capability:" + capability.ID.String()
	fingerprint, err := pollRequestFingerprint(input)
	if err != nil {
		return PublicAvailabilityPoll{}, repository.unavailable("fingerprint public response", err)
	}
	if replayed, err := repository.loadMutationReceipt(
		queryContext, transaction, capability.TenantID, poll.ID, "respond", actorKey,
		input.IdempotencyKey, fingerprint,
	); err != nil {
		return PublicAvailabilityPoll{}, err
	} else if replayed {
		projection, loadErr := repository.loadPublicPoll(
			queryContext, transaction, capability.TenantID, poll,
			capability.ID, capability.ParticipantID,
		)
		if loadErr != nil {
			return PublicAvailabilityPoll{}, loadErr
		}
		if commitErr := transaction.Commit(queryContext); commitErr != nil {
			return PublicAvailabilityPoll{}, repository.unavailable("commit replayed public response", commitErr)
		}
		return projection, nil
	}
	if poll.Status != PollStatusOpen || !poll.DeadlineAt.After(now) ||
		poll.ShareMode == PollShareClassMembers {
		return PublicAvailabilityPoll{}, ErrAvailabilityPollClosed
	}
	if err := validateAnswerSlotSet(poll.Slots, input.Answers); err != nil {
		return PublicAvailabilityPoll{}, err
	}
	responseID, responseVersion, err := repository.upsertPollResponse(
		queryContext, transaction, capability.TenantID, poll.ID,
		capability.ParticipantID, nil, &capability.ID,
		input.ExpectedResponseVersion, now,
	)
	if err != nil {
		return PublicAvailabilityPoll{}, err
	}
	if err := replacePollAnswers(
		queryContext, transaction, capability.TenantID, poll.ID,
		responseID, input.Answers, now,
	); err != nil {
		return PublicAvailabilityPoll{}, err
	}
	if err := repository.insertMutationReceipt(
		queryContext, transaction, capability.TenantID, poll.ID, "respond", actorKey,
		input.IdempotencyKey, fingerprint, responseVersion, "", uuid.Nil, now,
	); err != nil {
		return PublicAvailabilityPoll{}, err
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.availability_poll_capabilities
SET use_count = use_count + 1, last_used_at = $4
WHERE tenant_id = $1 AND poll_id = $2 AND id = $3`,
		capability.TenantID, poll.ID, capability.ID, now,
	); err != nil {
		return PublicAvailabilityPoll{}, repository.unavailable("record response session use", err)
	}
	if err := appendAvailabilityPollEvent(
		queryContext, transaction, capability.TenantID, poll, uuid.Nil,
		"availability_poll.response_recorded.v1",
		audit.Metadata{"response_version": fmt.Sprintf("%d", responseVersion)}, now,
	); err != nil {
		return PublicAvailabilityPoll{}, err
	}
	projection, err := repository.loadPublicPoll(
		queryContext, transaction, capability.TenantID, poll,
		capability.ID, capability.ParticipantID,
	)
	if err != nil {
		return PublicAvailabilityPoll{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return PublicAvailabilityPoll{}, repository.unavailable("commit public poll response", err)
	}
	return projection, nil
}

func (repository *PostgresAvailabilityPollRepository) ListStudyMeetings(
	ctx context.Context,
	scope tenancy.Context,
	input ListStudyMeetingsInput,
) ([]StudyMeeting, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, repository.unavailable("begin study meeting list", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	_, err = repository.requireActiveMembership(queryContext, transaction, scope)
	if err != nil {
		return nil, err
	}
	from, to := time.Time{}, time.Time{}
	if input.From != nil {
		from = input.From.UTC()
	}
	if input.To != nil {
		to = input.To.UTC()
	}
	rows, err := transaction.Query(
		queryContext,
		`SELECT id, class_id, owner_user_id, source_poll_id, title, starts_at,
       ends_at, timezone, status, version, cancelled_at, created_at, updated_at
FROM tutorhub.study_meetings
WHERE tenant_id = $1
  AND owner_user_id = $2
  AND ($3::timestamptz = '-infinity'::timestamptz OR ends_at > $3)
  AND ($4::timestamptz = '-infinity'::timestamptz OR starts_at < $4)
ORDER BY starts_at, id
LIMIT $5`,
		scope.TenantID, scope.ActorID, nullableInfinityTime(from), nullableInfinityTime(to),
		input.Limit,
	)
	if err != nil {
		return nil, repository.unavailable("query study meeting list", err)
	}
	defer rows.Close()
	meetings := make([]StudyMeeting, 0, input.Limit)
	for rows.Next() {
		meeting, err := scanStudyMeeting(rows)
		if err != nil {
			return nil, repository.unavailable("scan study meeting list", err)
		}
		meetings = append(meetings, meeting)
	}
	if err := rows.Err(); err != nil {
		return nil, repository.unavailable("iterate study meeting list", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, repository.unavailable("commit study meeting list", err)
	}
	return meetings, nil
}

func (repository *PostgresAvailabilityPollRepository) CreateStudyMeeting(
	ctx context.Context,
	scope tenancy.Context,
	input CreateStudyMeetingInput,
) (StudyMeeting, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return StudyMeeting{}, repository.unavailable("begin study meeting creation", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return StudyMeeting{}, err
	}
	organizationRole, err := repository.requireActiveMembership(queryContext, transaction, scope)
	if err != nil {
		return StudyMeeting{}, err
	}
	if !repository.authorizeOrganizationAction(
		scope, organizationRole, policy.ActionStudyMeetingScheduleOwn,
	) {
		return StudyMeeting{}, ErrAvailabilityPollAccessDenied
	}
	if input.ClassID != nil {
		if _, err := repository.requireClassMemberView(
			queryContext, transaction, scope, organizationRole, *input.ClassID,
		); err != nil {
			return StudyMeeting{}, err
		}
	}
	if err := acquireAvailabilityPollTransactionLock(
		queryContext,
		transaction,
		"study-meeting-create:"+scope.TenantID.String()+":"+scope.ActorID.String()+":"+input.IdempotencyKey,
	); err != nil {
		return StudyMeeting{}, repository.unavailable("lock study meeting creation receipt", err)
	}
	fingerprint, err := pollRequestFingerprint(input)
	if err != nil {
		return StudyMeeting{}, repository.unavailable("fingerprint study meeting creation", err)
	}
	existing, err := scanStudyMeeting(transaction.QueryRow(
		queryContext,
		`SELECT id, class_id, owner_user_id, source_poll_id, title, starts_at,
       ends_at, timezone, status, version, cancelled_at, created_at, updated_at
FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND owner_user_id = $2 AND create_idempotency_key = $3`,
		scope.TenantID, scope.ActorID, input.IdempotencyKey,
	))
	if err == nil {
		var persisted []byte
		if err := transaction.QueryRow(
			queryContext,
			`SELECT create_request_fingerprint FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND id = $2`, scope.TenantID, existing.ID,
		).Scan(&persisted); err != nil {
			return StudyMeeting{}, repository.unavailable("read study meeting receipt", err)
		}
		if !hmac.Equal(persisted, fingerprint[:]) {
			return StudyMeeting{}, ErrAvailabilityPollIdempotencyConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return StudyMeeting{}, repository.unavailable("commit replayed study meeting", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return StudyMeeting{}, repository.unavailable("read study meeting creation receipt", err)
	}
	now := repository.clock().UTC()
	if err := repository.requireStudyMeetingCreationQuota(
		queryContext, transaction, scope.TenantID, input.EndsAt, now,
	); err != nil {
		return StudyMeeting{}, err
	}
	if err := requireNoStudyMeetingConflict(
		queryContext, transaction, scope.TenantID, scope.ActorID, uuid.Nil,
		input.StartsAt, input.EndsAt,
	); err != nil {
		return StudyMeeting{}, err
	}
	meeting, err := scanStudyMeeting(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.study_meetings (
    tenant_id, class_id, owner_user_id, title, starts_at, ends_at, timezone,
    create_idempotency_key, create_request_fingerprint, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
RETURNING id, class_id, owner_user_id, source_poll_id, title, starts_at,
          ends_at, timezone, status, version, cancelled_at, created_at, updated_at`,
		scope.TenantID, nullableUUID(input.ClassID), scope.ActorID, input.Title,
		input.StartsAt, input.EndsAt, input.Timezone, input.IdempotencyKey,
		fingerprint[:], now,
	))
	if err != nil {
		return StudyMeeting{}, mapAvailabilityPollPostgresError("create study meeting", err)
	}
	if err := appendStudyMeetingEvent(
		queryContext, transaction, scope.TenantID, meeting, scope.ActorID,
		"study_meeting.scheduled.v1", nil, now,
	); err != nil {
		return StudyMeeting{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return StudyMeeting{}, repository.unavailable("commit study meeting creation", err)
	}
	return meeting, nil
}

func (repository *PostgresAvailabilityPollRepository) GetStudyMeeting(
	ctx context.Context,
	scope tenancy.Context,
	meetingID uuid.UUID,
) (StudyMeeting, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return StudyMeeting{}, repository.unavailable("begin study meeting read", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	_, err = repository.requireActiveMembership(queryContext, transaction, scope)
	if err != nil {
		return StudyMeeting{}, err
	}
	meeting, err := repository.loadStudyMeeting(
		queryContext, transaction, scope, meetingID, false, false,
	)
	if err != nil {
		return StudyMeeting{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return StudyMeeting{}, repository.unavailable("commit study meeting read", err)
	}
	return meeting, nil
}

func (repository *PostgresAvailabilityPollRepository) UpdateStudyMeeting(
	ctx context.Context,
	scope tenancy.Context,
	meetingID uuid.UUID,
	input UpdateStudyMeetingInput,
) (StudyMeeting, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return StudyMeeting{}, repository.unavailable("begin study meeting update", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureAvailabilityPolls,
	); err != nil {
		return StudyMeeting{}, err
	}
	organizationRole, err := repository.requireActiveMembership(queryContext, transaction, scope)
	if err != nil {
		return StudyMeeting{}, err
	}
	current, err := repository.loadStudyMeeting(
		queryContext, transaction, scope, meetingID, true, false,
	)
	if err != nil {
		return StudyMeeting{}, err
	}
	if current.Version != input.ExpectedVersion || current.Status != StudyMeetingScheduled {
		return StudyMeeting{}, ErrStudyMeetingConflict
	}
	if input.ClassID != nil {
		if _, err := repository.requireClassMemberView(
			queryContext, transaction, scope, organizationRole, *input.ClassID,
		); err != nil {
			return StudyMeeting{}, err
		}
	}
	now := repository.clock().UTC()
	if !current.EndsAt.After(now) && input.EndsAt.After(now) {
		if err := repository.requireStudyMeetingCapacity(
			queryContext, transaction, scope.TenantID, meetingID, now,
		); err != nil {
			return StudyMeeting{}, err
		}
	}
	if err := requireNoStudyMeetingConflict(
		queryContext, transaction, scope.TenantID, current.OwnerUserID,
		meetingID, input.StartsAt, input.EndsAt,
	); err != nil {
		return StudyMeeting{}, err
	}
	updated, err := scanStudyMeeting(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.study_meetings
SET class_id = $4, title = $5, starts_at = $6, ends_at = $7,
    timezone = $8, version = version + 1, updated_at = $9
WHERE tenant_id = $1 AND id = $2 AND version = $3 AND status = 'scheduled'
RETURNING id, class_id, owner_user_id, source_poll_id, title, starts_at,
          ends_at, timezone, status, version, cancelled_at, created_at, updated_at`,
		scope.TenantID, meetingID, input.ExpectedVersion, nullableUUID(input.ClassID),
		input.Title, input.StartsAt, input.EndsAt, input.Timezone, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return StudyMeeting{}, ErrStudyMeetingConflict
	}
	if err != nil {
		return StudyMeeting{}, mapAvailabilityPollPostgresError("update study meeting", err)
	}
	if err := appendStudyMeetingEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID,
		"study_meeting.rescheduled.v1", nil, now,
	); err != nil {
		return StudyMeeting{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return StudyMeeting{}, repository.unavailable("commit study meeting update", err)
	}
	return updated, nil
}

func (repository *PostgresAvailabilityPollRepository) CancelStudyMeeting(
	ctx context.Context,
	scope tenancy.Context,
	meetingID uuid.UUID,
	expectedVersion int64,
	reason string,
) (StudyMeeting, error) {
	queryContext, cancel := repository.context(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return StudyMeeting{}, repository.unavailable("begin study meeting cancellation", err)
	}
	defer rollbackAvailabilityPoll(transaction)
	organizationRole, err := repository.requireActiveMembership(queryContext, transaction, scope)
	if err != nil {
		return StudyMeeting{}, err
	}
	current, err := repository.loadStudyMeeting(
		queryContext,
		transaction,
		scope,
		meetingID,
		true,
		organizationRole == policy.OrganizationRoleAdmin,
	)
	if err != nil {
		return StudyMeeting{}, err
	}
	if current.Status == StudyMeetingCancelled {
		if current.Version != expectedVersion && current.Version != expectedVersion+1 {
			return StudyMeeting{}, ErrStudyMeetingConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return StudyMeeting{}, repository.unavailable("commit replayed study meeting cancellation", err)
		}
		return current, nil
	}
	if current.Version != expectedVersion {
		return StudyMeeting{}, ErrStudyMeetingConflict
	}
	now := repository.clock().UTC()
	updated, err := scanStudyMeeting(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.study_meetings
SET status = 'cancelled', version = version + 1, cancelled_at = $4,
    cancelled_by = $5, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $3 AND status = 'scheduled'
RETURNING id, class_id, owner_user_id, source_poll_id, title, starts_at,
          ends_at, timezone, status, version, cancelled_at, created_at, updated_at`,
		scope.TenantID, meetingID, expectedVersion, now, scope.ActorID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return StudyMeeting{}, ErrStudyMeetingConflict
	}
	if err != nil {
		return StudyMeeting{}, repository.unavailable("cancel study meeting", err)
	}
	if err := appendStudyMeetingEvent(
		queryContext, transaction, scope.TenantID, updated, scope.ActorID,
		"study_meeting.cancelled.v1", audit.Metadata{"reason": reason}, now,
	); err != nil {
		return StudyMeeting{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return StudyMeeting{}, repository.unavailable("commit study meeting cancellation", err)
	}
	return updated, nil
}

func (repository *PostgresAvailabilityPollRepository) context(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, repository.queryTimeout)
}

func (repository *PostgresAvailabilityPollRepository) requireActiveMembership(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
) (policy.OrganizationRole, error) {
	if err := scope.Validate(); err != nil {
		return "", ErrAvailabilityPollAccessDenied
	}
	var role policy.OrganizationRole
	if err := transaction.QueryRow(
		ctx,
		`SELECT membership.role
FROM tutorhub.memberships AS membership
JOIN tutorhub.tenants AS tenant ON tenant.id = membership.tenant_id
WHERE membership.tenant_id = $1 AND membership.user_id = $2
  AND membership.status = 'active' AND tenant.status = 'active'`,
		scope.TenantID, scope.ActorID,
	).Scan(&role); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAvailabilityPollAccessDenied
	} else if err != nil {
		return "", repository.unavailable("authorize poll membership", err)
	}
	return role, nil
}

func (repository *PostgresAvailabilityPollRepository) authorizeOrganizationAction(
	scope tenancy.Context,
	role policy.OrganizationRole,
	action policy.Action,
) bool {
	return repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: scope.ActorID, ActiveTenantID: scope.TenantID,
			MembershipActive: true, OrganizationRoles: []policy.OrganizationRole{role},
		},
		Action: action,
		Resource: policy.Resource{
			TenantID: scope.TenantID, State: policy.ResourceStateUnknown,
		},
	}).Allowed
}

func (repository *PostgresAvailabilityPollRepository) requireClassView(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	organizationRole policy.OrganizationRole,
	classID uuid.UUID,
) (policy.ClassRole, error) {
	var ownerID uuid.UUID
	var classStatus string
	var enrollmentRole sql.NullString
	var enrollmentStatus sql.NullString
	if err := transaction.QueryRow(
		ctx,
		`SELECT class.owner_user_id, class.status,
       enrollment.class_role, enrollment.status
FROM tutorhub.classes AS class
LEFT JOIN tutorhub.class_enrollments AS enrollment
  ON enrollment.tenant_id = class.tenant_id
 AND enrollment.class_id = class.id
 AND enrollment.user_id = $3
WHERE class.tenant_id = $1 AND class.id = $2`,
		scope.TenantID, classID, scope.ActorID,
	).Scan(&ownerID, &classStatus, &enrollmentRole, &enrollmentStatus); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAvailabilityPollNotFound
	} else if err != nil {
		return "", repository.unavailable("load poll class", err)
	}
	classRole := policy.ClassRole("")
	if ownerID == scope.ActorID {
		classRole = policy.ClassRoleOwner
	} else if enrollmentStatus.Valid && enrollmentStatus.String == "active" && enrollmentRole.Valid {
		classRole = policy.ClassRole(enrollmentRole.String)
	}
	classRoles := []policy.ClassRole{}
	if classRole != "" {
		classRoles = append(classRoles, classRole)
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: scope.ActorID, ActiveTenantID: scope.TenantID,
			MembershipActive:  true,
			OrganizationRoles: []policy.OrganizationRole{organizationRole},
			ClassRoles:        classRoles,
		},
		Action: policy.ActionClassView,
		Resource: policy.Resource{
			TenantID: scope.TenantID, ClassID: classID,
			State: classResourceState(classStatus),
		},
	})
	if !decision.Allowed {
		return "", ErrAvailabilityPollNotFound
	}
	return classRole, nil
}

func (repository *PostgresAvailabilityPollRepository) requireClassMemberView(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	organizationRole policy.OrganizationRole,
	classID uuid.UUID,
) (policy.ClassRole, error) {
	classRole, err := repository.requireClassView(
		ctx, transaction, scope, organizationRole, classID,
	)
	if err != nil {
		return "", err
	}
	if classRole == "" {
		return "", ErrAvailabilityPollNotFound
	}
	return classRole, nil
}

func (repository *PostgresAvailabilityPollRepository) loadPoll(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	pollID uuid.UUID,
	lock bool,
) (AvailabilityPoll, pollAuthorization, error) {
	organizationRole, err := repository.requireActiveMembership(ctx, transaction, scope)
	if err != nil {
		return AvailabilityPoll{}, pollAuthorization{}, err
	}
	query := `SELECT id, public_id, class_id, owner_user_id, title, description,
       timezone, range_start::text, range_end::text,
       working_day_start::text, working_day_end::text,
       duration_minutes, slot_granularity_minutes, deadline_at,
       share_mode, status, version, selected_slot_id, outcome_type,
       outcome_id, created_at, updated_at
FROM tutorhub.availability_polls
WHERE tenant_id = $1 AND id = $2`
	if lock {
		query += " FOR UPDATE"
	}
	poll, err := scanAvailabilityPoll(transaction.QueryRow(ctx, query, scope.TenantID, pollID))
	if errors.Is(err, pgx.ErrNoRows) {
		return AvailabilityPoll{}, pollAuthorization{}, ErrAvailabilityPollNotFound
	}
	if err != nil {
		return AvailabilityPoll{}, pollAuthorization{}, repository.unavailable("load poll", err)
	}
	authorization := pollAuthorization{
		OrganizationRole: organizationRole,
		Owner:            poll.OwnerUserID == scope.ActorID,
		SafetyAdmin:      organizationRole == policy.OrganizationRoleAdmin,
	}
	if poll.ClassID != nil {
		classRole, classErr := repository.requireClassView(
			ctx, transaction, scope, organizationRole, *poll.ClassID,
		)
		if classErr != nil {
			return AvailabilityPoll{}, pollAuthorization{}, classErr
		}
		authorization.ClassRole = classRole
		authorization.ClassMember = classRole != ""
		authorization.ClassActive = true
	}
	var participantVisible bool
	if err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1 FROM tutorhub.availability_poll_participants
    WHERE tenant_id = $1 AND poll_id = $2 AND internal_user_id = $3
      AND status = 'active'
)`,
		scope.TenantID, pollID, scope.ActorID,
	).Scan(&participantVisible); err != nil {
		return AvailabilityPoll{}, pollAuthorization{}, repository.unavailable("authorize poll participant", err)
	}
	visible := authorization.Owner || authorization.SafetyAdmin || participantVisible ||
		(poll.ShareMode == PollShareClassMembers && authorization.ClassMember)
	if !visible {
		return AvailabilityPoll{}, pollAuthorization{}, ErrAvailabilityPollNotFound
	}
	slots, err := loadPollSlots(ctx, transaction, scope.TenantID, pollID)
	if err != nil {
		return AvailabilityPoll{}, pollAuthorization{}, err
	}
	participants := []AvailabilityPollParticipant{}
	if authorization.Owner {
		participants, err = loadPollParticipants(ctx, transaction, scope.TenantID, pollID)
		if err != nil {
			return AvailabilityPoll{}, pollAuthorization{}, err
		}
	}
	myResponse, err := loadInternalPollResponse(
		ctx, transaction, scope.TenantID, pollID, scope.ActorID,
	)
	if err != nil {
		return AvailabilityPoll{}, pollAuthorization{}, err
	}
	poll.Slots, poll.Participants, poll.MyResponse = slots, participants, myResponse
	canManage := authorization.Owner && repository.authorizeOrganizationAction(
		scope, organizationRole, policy.ActionAvailabilityPollManageOwn,
	)
	canStudy := canManage && repository.authorizeOrganizationAction(
		scope, organizationRole, policy.ActionStudyMeetingScheduleOwn,
	)
	subject := policy.Subject{
		ActorID: scope.ActorID, ActiveTenantID: scope.TenantID,
		MembershipActive: true, OrganizationRoles: []policy.OrganizationRole{organizationRole},
	}
	if authorization.ClassRole != "" {
		subject.ClassRoles = []policy.ClassRole{authorization.ClassRole}
	}
	canScheduleSession := false
	if poll.ClassID == nil {
		canScheduleSession = hasPolicyPermission(
			repository.authorizer.EffectivePermissions(subject), policy.PermissionSessionSchedule,
		)
	} else {
		canScheduleSession = repository.authorizer.Authorize(policy.Input{
			Subject: subject, Action: policy.ActionSessionSchedule,
			Resource: policy.Resource{
				TenantID: scope.TenantID, ClassID: *poll.ClassID,
				State: policy.ResourceStateActive,
			},
		}).Allowed
	}
	canSession := canManage && canScheduleSession
	canViewIndividual := canViewPollIndividualResponses(
		authorization, poll.ClassID != nil, canScheduleSession,
	)
	responseVisible := authorization.Owner || participantVisible ||
		(poll.ShareMode == PollShareClassMembers && authorization.ClassMember)
	poll.ViewerCapabilities = AvailabilityPollViewerCapabilities{
		CanManage: canManage,
		CanRespond: responseVisible && poll.Status == PollStatusOpen &&
			poll.DeadlineAt.After(repository.clock().UTC()),
		CanShare:                canManage,
		CanFinalizeClassSession: canSession,
		CanFinalizeStudyMeeting: canStudy,
		CanViewExactAggregate:   canViewIndividual,
		CanViewIndividual:       canViewIndividual,
	}
	return poll, authorization, nil
}

func canViewPollIndividualResponses(
	authorization pollAuthorization,
	pollHasClass bool,
	canScheduleSession bool,
) bool {
	return authorization.Owner || authorization.SafetyAdmin ||
		(pollHasClass && authorization.ClassMember && canScheduleSession)
}

func scanAvailabilityPoll(row interface{ Scan(...any) error }) (AvailabilityPoll, error) {
	var poll AvailabilityPoll
	var classID uuid.NullUUID
	var selectedSlotID uuid.NullUUID
	var outcomeType sql.NullString
	var outcomeID uuid.NullUUID
	if err := row.Scan(
		&poll.ID, &poll.PublicID, &classID, &poll.OwnerUserID,
		&poll.Title, &poll.Description, &poll.Timezone, &poll.RangeStart, &poll.RangeEnd,
		&poll.WorkingDayStart, &poll.WorkingDayEnd, &poll.DurationMinutes,
		&poll.SlotGranularityMinutes, &poll.DeadlineAt, &poll.ShareMode, &poll.Status,
		&poll.Version, &selectedSlotID, &outcomeType, &outcomeID,
		&poll.CreatedAt, &poll.UpdatedAt,
	); err != nil {
		return AvailabilityPoll{}, err
	}
	if classID.Valid {
		value := classID.UUID
		poll.ClassID = &value
	}
	if outcomeType.Valid && outcomeID.Valid {
		poll.Outcome = &AvailabilityPollOutcomeReference{
			Type: outcomeType.String, ID: outcomeID.UUID,
		}
	}
	poll.DeadlineAt = poll.DeadlineAt.UTC()
	poll.CreatedAt, poll.UpdatedAt = poll.CreatedAt.UTC(), poll.UpdatedAt.UTC()
	return poll, nil
}

func loadPollSlots(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
) ([]AvailabilityPollSlot, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT id, starts_at, ends_at, ordinal
FROM tutorhub.availability_poll_slots
WHERE tenant_id = $1 AND poll_id = $2 AND retired_at IS NULL
ORDER BY ordinal, id`, tenantID, pollID,
	)
	if err != nil {
		return nil, fmt.Errorf("load poll slots: %w", err)
	}
	defer rows.Close()
	slots := []AvailabilityPollSlot{}
	for rows.Next() {
		var slot AvailabilityPollSlot
		if err := rows.Scan(&slot.ID, &slot.StartsAt, &slot.EndsAt, &slot.Ordinal); err != nil {
			return nil, fmt.Errorf("scan poll slot: %w", err)
		}
		slot.StartsAt, slot.EndsAt = slot.StartsAt.UTC(), slot.EndsAt.UTC()
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate poll slots: %w", err)
	}
	return slots, nil
}

func loadPollParticipants(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
) ([]AvailabilityPollParticipant, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT participant.id, participant.kind, participant.internal_user_id,
       participant.status, EXISTS (
           SELECT 1 FROM tutorhub.availability_poll_responses AS response
           WHERE response.tenant_id = participant.tenant_id
             AND response.poll_id = participant.poll_id
             AND response.participant_id = participant.id
       )
FROM tutorhub.availability_poll_participants AS participant
WHERE participant.tenant_id = $1 AND participant.poll_id = $2
  AND participant.status = 'active'
ORDER BY participant.created_at, participant.id`, tenantID, pollID,
	)
	if err != nil {
		return nil, fmt.Errorf("load poll participants: %w", err)
	}
	defer rows.Close()
	participants := []AvailabilityPollParticipant{}
	for rows.Next() {
		var participant AvailabilityPollParticipant
		var internalUserID uuid.NullUUID
		if err := rows.Scan(
			&participant.ID, &participant.Kind, &internalUserID,
			&participant.Status, &participant.HasResponded,
		); err != nil {
			return nil, fmt.Errorf("scan poll participant: %w", err)
		}
		if internalUserID.Valid {
			value := internalUserID.UUID
			participant.InternalUserID = &value
		}
		participants = append(participants, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate poll participants: %w", err)
	}
	return participants, nil
}

func loadInternalPollResponse(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	userID uuid.UUID,
) (*AvailabilityPollResponseProjection, error) {
	var responseID uuid.UUID
	var projection AvailabilityPollResponseProjection
	err := transaction.QueryRow(
		ctx,
		`SELECT id, version, submitted_at
FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2 AND internal_user_id = $3`,
		tenantID, pollID, userID,
	).Scan(&responseID, &projection.Version, &projection.SubmittedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load poll response: %w", err)
	}
	answers, err := loadPollAnswers(ctx, transaction, tenantID, pollID, responseID)
	if err != nil {
		return nil, err
	}
	projection.Answers = answers
	projection.SubmittedAt = projection.SubmittedAt.UTC()
	return &projection, nil
}

func loadPollIndividualResponses(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	after individualResponseCursor,
	limit int,
) ([]AvailabilityPollIndividualResponse, error) {
	var afterCreatedAt any
	var afterID any
	if !after.CreatedAt.IsZero() {
		afterCreatedAt = after.CreatedAt.UTC()
		afterID = after.ID
	}
	rows, err := transaction.Query(
		ctx,
		`WITH selected_response AS (
    SELECT response.id, response.participant_id, response.internal_user_id,
           response.actor_type, response.version, response.submitted_at,
           response.created_at
    FROM tutorhub.availability_poll_responses AS response
    WHERE response.tenant_id = $1 AND response.poll_id = $2
      AND (
          $3::timestamptz IS NULL
          OR response.created_at > $3
          OR (response.created_at = $3 AND response.id > $4::uuid)
      )
    ORDER BY response.created_at, response.id
    LIMIT $5
)
SELECT response.id, response.participant_id, response.internal_user_id,
       response.actor_type, response.version, response.submitted_at,
       response.created_at,
       answer.slot_id, answer.state
FROM selected_response AS response
LEFT JOIN tutorhub.availability_poll_answers AS answer
  ON answer.tenant_id = $1
 AND answer.poll_id = $2
 AND answer.response_id = response.id
 AND answer.cleared_at IS NULL
ORDER BY response.created_at, response.id, answer.slot_id`,
		tenantID, pollID, afterCreatedAt, afterID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("load individual poll responses: %w", err)
	}
	defer rows.Close()

	responses := []AvailabilityPollIndividualResponse{}
	responseIndexes := make(map[uuidKey]int)
	for rows.Next() {
		var responseID uuid.UUID
		var participantID uuid.NullUUID
		var internalUserID uuid.NullUUID
		var actorType string
		var version int64
		var submittedAt time.Time
		var createdAt time.Time
		var slotID uuid.NullUUID
		var state sql.NullString
		if err := rows.Scan(
			&responseID, &participantID, &internalUserID, &actorType, &version,
			&submittedAt, &createdAt, &slotID, &state,
		); err != nil {
			return nil, fmt.Errorf("scan individual poll response: %w", err)
		}
		index, exists := responseIndexes[uuidKey(responseID)]
		if !exists {
			response := AvailabilityPollIndividualResponse{
				ResponseID:  responseID,
				ActorType:   actorType,
				Version:     version,
				Answers:     []AvailabilityPollAnswer{},
				SubmittedAt: submittedAt.UTC(),
				createdAt:   createdAt.UTC(),
			}
			if participantID.Valid {
				value := participantID.UUID
				response.ParticipantID = &value
			}
			if internalUserID.Valid {
				value := internalUserID.UUID
				response.InternalUserID = &value
			}
			responses = append(responses, response)
			index = len(responses) - 1
			responseIndexes[uuidKey(responseID)] = index
		}
		if slotID.Valid && state.Valid {
			responses[index].Answers = append(
				responses[index].Answers,
				AvailabilityPollAnswer{SlotID: slotID.UUID, State: state.String},
			)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate individual poll responses: %w", err)
	}
	return responses, nil
}

func loadPublicPollResponse(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	responseCapabilityID uuid.UUID,
	participantID *uuid.UUID,
) (*AvailabilityPollResponseProjection, error) {
	var responseID uuid.UUID
	var projection AvailabilityPollResponseProjection
	participant := nullableUUID(participantID)
	err := transaction.QueryRow(
		ctx,
		`SELECT id, version, submitted_at
FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2
  AND (response_capability_id = $3 OR ($4::uuid IS NOT NULL AND participant_id = $4))
ORDER BY CASE WHEN response_capability_id = $3 THEN 0 ELSE 1 END
LIMIT 1`,
		tenantID, pollID, responseCapabilityID, participant,
	).Scan(&responseID, &projection.Version, &projection.SubmittedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load public poll response: %w", err)
	}
	answers, err := loadPollAnswers(ctx, transaction, tenantID, pollID, responseID)
	if err != nil {
		return nil, err
	}
	projection.Answers = answers
	projection.SubmittedAt = projection.SubmittedAt.UTC()
	return &projection, nil
}

func loadPollAnswers(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	responseID uuid.UUID,
) ([]AvailabilityPollAnswer, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT slot_id, state
FROM tutorhub.availability_poll_answers
WHERE tenant_id = $1 AND poll_id = $2 AND response_id = $3
  AND cleared_at IS NULL
ORDER BY slot_id`, tenantID, pollID, responseID,
	)
	if err != nil {
		return nil, fmt.Errorf("load poll answers: %w", err)
	}
	defer rows.Close()
	answers := []AvailabilityPollAnswer{}
	for rows.Next() {
		var answer AvailabilityPollAnswer
		if err := rows.Scan(&answer.SlotID, &answer.State); err != nil {
			return nil, fmt.Errorf("scan poll answer: %w", err)
		}
		answers = append(answers, answer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate poll answers: %w", err)
	}
	return answers, nil
}

func (repository *PostgresAvailabilityPollRepository) loadSummary(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	poll AvailabilityPoll,
	exposeExact bool,
	exposeAggregate bool,
) (AvailabilityPollSummary, error) {
	var responseCount int
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2`, tenantID, poll.ID,
	).Scan(&responseCount); err != nil {
		return AvailabilityPollSummary{}, repository.unavailable("count poll summary responses", err)
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT slot.id,
       count(*) FILTER (WHERE answer.state = 'preferred')::integer,
       count(*) FILTER (WHERE answer.state = 'available')::integer,
       count(*) FILTER (WHERE answer.state = 'unavailable')::integer
FROM tutorhub.availability_poll_slots AS slot
LEFT JOIN tutorhub.availability_poll_answers AS answer
  ON answer.tenant_id = slot.tenant_id
 AND answer.poll_id = slot.poll_id
 AND answer.slot_id = slot.id
 AND answer.cleared_at IS NULL
WHERE slot.tenant_id = $1 AND slot.poll_id = $2 AND slot.retired_at IS NULL
GROUP BY slot.id`, tenantID, poll.ID,
	)
	if err != nil {
		return AvailabilityPollSummary{}, repository.unavailable("query poll summary", err)
	}
	defer rows.Close()
	counts := make(map[uuidKey]pollSlotCounts, len(poll.Slots))
	for rows.Next() {
		var slotID uuid.UUID
		var count pollSlotCounts
		if err := rows.Scan(
			&slotID, &count.Preferred, &count.Available, &count.Unavailable,
		); err != nil {
			return AvailabilityPollSummary{}, repository.unavailable("scan poll summary", err)
		}
		counts[uuidKey(slotID)] = count
	}
	if err := rows.Err(); err != nil {
		return AvailabilityPollSummary{}, repository.unavailable("iterate poll summary", err)
	}
	var exactResponseCount *int
	if exposeExact {
		value := responseCount
		exactResponseCount = &value
	}
	return AvailabilityPollSummary{
		PollID: poll.ID, PollVersion: poll.Version, Status: poll.Status,
		ResponseCount: exactResponseCount,
		RankedSlots: rankAvailabilityPollSlots(
			poll.Slots, counts, responseCount, exposeExact, exposeAggregate,
		),
	}, nil
}

func (repository *PostgresAvailabilityPollRepository) buildMutationResponse(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	pollID uuid.UUID,
	authorization pollAuthorization,
) (AvailabilityPollMutationResponse, error) {
	poll, refreshedAuthorization, err := repository.loadPoll(
		ctx, transaction, scope, pollID, false,
	)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	if refreshedAuthorization.OrganizationRole != "" {
		authorization = refreshedAuthorization
	}
	exact := poll.ViewerCapabilities.CanViewExactAggregate
	summary, err := repository.loadSummary(
		ctx, transaction, scope.TenantID, poll, exact, true,
	)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	return AvailabilityPollMutationResponse{Poll: poll, Summary: summary}, nil
}

type publicPollCapabilityRow struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	PollID        uuid.UUID
	ParentID      *uuid.UUID
	ParticipantID *uuid.UUID
	Purpose       string
	Scope         string
	TokenDigest   []byte
	ExpiresAt     time.Time
}

func (repository *PostgresAvailabilityPollRepository) loadPublicCapability(
	ctx context.Context,
	transaction pgx.Tx,
	publicID uuid.UUID,
	version int16,
	digest []byte,
	purpose string,
	now time.Time,
	lock bool,
) (publicPollCapabilityRow, AvailabilityPoll, error) {
	query := `SELECT capability.id, capability.tenant_id, capability.poll_id,
       capability.parent_capability_id, capability.participant_id,
       capability.purpose, capability.scope, capability.token_digest,
       capability.expires_at,
       poll.id, poll.public_id, poll.class_id, poll.owner_user_id,
       poll.title, poll.description, poll.timezone,
       poll.range_start::text, poll.range_end::text,
       poll.working_day_start::text, poll.working_day_end::text,
       poll.duration_minutes, poll.slot_granularity_minutes, poll.deadline_at,
       poll.share_mode, poll.status, poll.version, poll.selected_slot_id,
       poll.outcome_type, poll.outcome_id, poll.created_at, poll.updated_at
FROM tutorhub.availability_poll_capabilities AS capability
JOIN tutorhub.availability_polls AS poll
  ON poll.tenant_id = capability.tenant_id AND poll.id = capability.poll_id
LEFT JOIN tutorhub.availability_poll_capabilities AS parent
  ON parent.tenant_id = capability.tenant_id
 AND parent.poll_id = capability.poll_id
 AND parent.id = capability.parent_capability_id
WHERE poll.public_id = $1
  AND capability.token_version = $2
  AND capability.token_digest = $3
  AND capability.purpose = $4
  AND capability.revoked_at IS NULL
  AND capability.expires_at > $5
  AND (
      capability.purpose = 'poll_access'
      OR (
          parent.id IS NOT NULL AND parent.revoked_at IS NULL
          AND parent.expires_at > $5
      )
  )`
	if lock {
		query += " FOR UPDATE OF capability, poll"
	}
	row := transaction.QueryRow(ctx, query, publicID, version, digest, purpose, now)
	var capability publicPollCapabilityRow
	var parentID uuid.NullUUID
	var participantID uuid.NullUUID
	var poll AvailabilityPoll
	var classID uuid.NullUUID
	var selectedSlotID uuid.NullUUID
	var outcomeType sql.NullString
	var outcomeID uuid.NullUUID
	if err := row.Scan(
		&capability.ID, &capability.TenantID, &capability.PollID,
		&parentID, &participantID, &capability.Purpose, &capability.Scope,
		&capability.TokenDigest, &capability.ExpiresAt,
		&poll.ID, &poll.PublicID, &classID, &poll.OwnerUserID,
		&poll.Title, &poll.Description, &poll.Timezone,
		&poll.RangeStart, &poll.RangeEnd, &poll.WorkingDayStart, &poll.WorkingDayEnd,
		&poll.DurationMinutes, &poll.SlotGranularityMinutes, &poll.DeadlineAt,
		&poll.ShareMode, &poll.Status, &poll.Version, &selectedSlotID,
		&outcomeType, &outcomeID, &poll.CreatedAt, &poll.UpdatedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return publicPollCapabilityRow{}, AvailabilityPoll{}, ErrAvailabilityPollCapabilityUnavailable
	} else if err != nil {
		return publicPollCapabilityRow{}, AvailabilityPoll{}, repository.unavailable("load public capability", err)
	}
	if !hmac.Equal(capability.TokenDigest, digest) {
		return publicPollCapabilityRow{}, AvailabilityPoll{}, ErrAvailabilityPollCapabilityUnavailable
	}
	if parentID.Valid {
		value := parentID.UUID
		capability.ParentID = &value
	}
	if participantID.Valid {
		value := participantID.UUID
		capability.ParticipantID = &value
	}
	if classID.Valid {
		value := classID.UUID
		poll.ClassID = &value
	}
	if outcomeType.Valid && outcomeID.Valid {
		poll.Outcome = &AvailabilityPollOutcomeReference{Type: outcomeType.String, ID: outcomeID.UUID}
	}
	slots, err := loadPollSlots(ctx, transaction, capability.TenantID, poll.ID)
	if err != nil {
		return publicPollCapabilityRow{}, AvailabilityPoll{}, repository.unavailable(
			"load public poll slots", err,
		)
	}
	poll.Slots = slots
	if poll.Slots == nil {
		poll.Slots = []AvailabilityPollSlot{}
	}
	return capability, poll, nil
}

func (repository *PostgresAvailabilityPollRepository) loadPublicPoll(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	poll AvailabilityPoll,
	responseCapabilityID uuid.UUID,
	participantID *uuid.UUID,
) (PublicAvailabilityPoll, error) {
	if len(poll.Slots) == 0 {
		slots, err := loadPollSlots(ctx, transaction, tenantID, poll.ID)
		if err != nil {
			return PublicAvailabilityPoll{}, repository.unavailable("load public poll slots", err)
		}
		poll.Slots = slots
	}
	myResponse, err := loadPublicPollResponse(
		ctx, transaction, tenantID, poll.ID, responseCapabilityID, participantID,
	)
	if err != nil {
		return PublicAvailabilityPoll{}, repository.unavailable("load public poll response", err)
	}
	summary, err := repository.loadSummary(
		ctx, transaction, tenantID, poll, false, myResponse != nil,
	)
	if err != nil {
		return PublicAvailabilityPoll{}, err
	}
	return PublicAvailabilityPoll{
		PublicID: poll.PublicID, Title: poll.Title, Description: poll.Description,
		Timezone: poll.Timezone, DeadlineAt: poll.DeadlineAt.UTC(), Status: poll.Status,
		Slots: poll.Slots, MyResponse: myResponse, RankedSlots: summary.RankedSlots,
	}, nil
}

func findInternalPollParticipant(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	userID uuid.UUID,
) (*uuid.UUID, error) {
	var participantID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		`SELECT id FROM tutorhub.availability_poll_participants
WHERE tenant_id = $1 AND poll_id = $2 AND internal_user_id = $3
  AND status = 'active'`, tenantID, pollID, userID,
	).Scan(&participantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load internal poll participant: %w", err)
	}
	return &participantID, nil
}

func requireActivePollParticipants(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	participants []AvailabilityPollParticipantInput,
) error {
	userIDs := make([]uuid.UUID, 0, len(participants))
	for _, participant := range participants {
		if participant.Kind == PollParticipantInternal && participant.InternalUserID != nil {
			userIDs = append(userIDs, *participant.InternalUserID)
		}
	}
	if len(userIDs) == 0 {
		return nil
	}
	var activeCount int
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.memberships
WHERE tenant_id = $1 AND user_id = ANY($2::uuid[]) AND status = 'active'`,
		tenantID, userIDs,
	).Scan(&activeCount); err != nil {
		return fmt.Errorf("validate poll participants: %w", err)
	}
	if activeCount != len(userIDs) {
		return ErrAvailabilityPollNotFound
	}
	return nil
}

func (repository *PostgresAvailabilityPollRepository) upsertPollResponse(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	participantID *uuid.UUID,
	internalUserID *uuid.UUID,
	responseCapabilityID *uuid.UUID,
	expectedVersion int64,
	now time.Time,
) (uuid.UUID, int64, error) {
	var responseID uuid.UUID
	var currentVersion int64
	var err error
	switch {
	case internalUserID != nil:
		err = transaction.QueryRow(
			ctx,
			`SELECT id, version FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2 AND internal_user_id = $3
FOR UPDATE`, tenantID, pollID, *internalUserID,
		).Scan(&responseID, &currentVersion)
	case participantID != nil:
		err = transaction.QueryRow(
			ctx,
			`SELECT id, version FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2 AND participant_id = $3
FOR UPDATE`, tenantID, pollID, *participantID,
		).Scan(&responseID, &currentVersion)
	case responseCapabilityID != nil:
		err = transaction.QueryRow(
			ctx,
			`SELECT id, version FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2 AND response_capability_id = $3
FOR UPDATE`, tenantID, pollID, *responseCapabilityID,
		).Scan(&responseID, &currentVersion)
	default:
		return uuid.Nil, 0, ErrAvailabilityPollInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if expectedVersion != 0 {
			return uuid.Nil, 0, ErrAvailabilityPollConflict
		}
		var responseCount int64
		if err := transaction.QueryRow(
			ctx,
			`SELECT count(*)
FROM tutorhub.availability_poll_responses
WHERE tenant_id = $1 AND poll_id = $2`,
			tenantID, pollID,
		).Scan(&responseCount); err != nil {
			return uuid.Nil, 0, repository.unavailable("count poll responses", err)
		}
		if err := repository.controls.RequireQuotaAtMost(
			ctx,
			transaction,
			tenantID,
			featurecontrol.QuotaAvailabilityPollParticipants,
			responseCount+1,
		); err != nil {
			return uuid.Nil, 0, err
		}
		actorType := "internal_member"
		if internalUserID == nil {
			actorType = "external_session"
		}
		if err := transaction.QueryRow(
			ctx,
			`INSERT INTO tutorhub.availability_poll_responses (
    tenant_id, poll_id, participant_id, internal_user_id,
    response_capability_id, actor_type, submitted_at, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $7)
RETURNING id, version`,
			tenantID, pollID, nullableUUID(participantID), nullableUUID(internalUserID),
			nullableUUID(responseCapabilityID), actorType, now,
		).Scan(&responseID, &currentVersion); err != nil {
			return uuid.Nil, 0, mapAvailabilityPollPostgresError("create poll response", err)
		}
		return responseID, currentVersion, nil
	}
	if err != nil {
		return uuid.Nil, 0, repository.unavailable("lock poll response", err)
	}
	if currentVersion != expectedVersion {
		return uuid.Nil, 0, ErrAvailabilityPollConflict
	}
	if currentVersion >= maximumPollResponseVersion {
		return uuid.Nil, 0, ErrAvailabilityPollConflict
	}
	if err := transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.availability_poll_responses
SET response_capability_id = CASE
        WHEN actor_type = 'external_session' THEN $4 ELSE response_capability_id END,
    version = version + 1, submitted_at = $5, updated_at = $5
WHERE tenant_id = $1 AND poll_id = $2 AND id = $3 AND version = $6
RETURNING version`,
		tenantID, pollID, responseID, nullableUUID(responseCapabilityID), now, expectedVersion,
	).Scan(&currentVersion); errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, ErrAvailabilityPollConflict
	} else if err != nil {
		return uuid.Nil, 0, repository.unavailable("update poll response", err)
	}
	return responseID, currentVersion, nil
}

func replacePollAnswers(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	responseID uuid.UUID,
	answers []AvailabilityPollAnswerInput,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.availability_poll_answers
SET cleared_at = $4, updated_at = $4
WHERE tenant_id = $1 AND poll_id = $2 AND response_id = $3
  AND cleared_at IS NULL`,
		tenantID, pollID, responseID, now,
	); err != nil {
		return fmt.Errorf("clear poll answers: %w", err)
	}
	for _, answer := range answers {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.availability_poll_answers (
    tenant_id, poll_id, response_id, slot_id, state, created_at, updated_at, cleared_at
)
VALUES ($1, $2, $3, $4, $5, $6, $6, NULL)
ON CONFLICT (tenant_id, response_id, slot_id) DO UPDATE
SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at, cleared_at = NULL
WHERE availability_poll_answers.poll_id = EXCLUDED.poll_id`,
			tenantID, pollID, responseID, answer.SlotID, answer.State, now,
		); err != nil {
			return mapAvailabilityPollPostgresError("create poll answer", err)
		}
	}
	return nil
}

func (repository *PostgresAvailabilityPollRepository) loadMutationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	operation string,
	actorKey string,
	idempotencyKey string,
	fingerprint [sha256.Size]byte,
) (bool, error) {
	var persisted []byte
	err := transaction.QueryRow(
		ctx,
		`SELECT request_fingerprint
FROM tutorhub.availability_poll_mutation_receipts
WHERE tenant_id = $1 AND poll_id = $2 AND operation = $3
  AND actor_key = $4 AND idempotency_key = $5`,
		tenantID, pollID, operation, actorKey, idempotencyKey,
	).Scan(&persisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, repository.unavailable("load poll mutation receipt", err)
	}
	if !hmac.Equal(persisted, fingerprint[:]) {
		return false, ErrAvailabilityPollIdempotencyConflict
	}
	return true, nil
}

func (repository *PostgresAvailabilityPollRepository) insertMutationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	pollID uuid.UUID,
	operation string,
	actorKey string,
	idempotencyKey string,
	fingerprint [sha256.Size]byte,
	resultVersion int64,
	outcomeType string,
	outcomeID uuid.UUID,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.availability_poll_mutation_receipts (
    tenant_id, poll_id, operation, actor_key, idempotency_key,
    request_fingerprint, result_version, outcome_type, outcome_id, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		tenantID, pollID, operation, actorKey, idempotencyKey, fingerprint[:],
		resultVersion, nullableString(outcomeType), nullableUUIDValue(outcomeID), now,
	); err != nil {
		return mapAvailabilityPollPostgresError("create poll mutation receipt", err)
	}
	return nil
}

func validateAnswerSlotSet(
	slots []AvailabilityPollSlot,
	answers []AvailabilityPollAnswerInput,
) error {
	allowed := make(map[uuid.UUID]struct{}, len(slots))
	for _, slot := range slots {
		allowed[slot.ID] = struct{}{}
	}
	for _, answer := range answers {
		if _, ok := allowed[answer.SlotID]; !ok {
			return ErrAvailabilityPollInvalid
		}
	}
	return nil
}

func pollStructureMatchesInput(
	poll AvailabilityPoll,
	input CreateAvailabilityPollInput,
) bool {
	if !nullableUUIDEqual(poll.ClassID, input.ClassID) ||
		poll.Timezone != input.Timezone || poll.RangeStart != input.RangeStart ||
		poll.RangeEnd != input.RangeEnd || poll.WorkingDayStart != input.WorkingDayStart ||
		poll.WorkingDayEnd != input.WorkingDayEnd ||
		poll.DurationMinutes != input.DurationMinutes ||
		poll.SlotGranularityMinutes != input.SlotGranularityMinutes ||
		len(poll.Slots) != len(input.Slots) ||
		len(poll.Participants) != len(input.Participants) {
		return false
	}
	for index, slot := range poll.Slots {
		if !slot.StartsAt.Equal(input.Slots[index].StartsAt) ||
			!slot.EndsAt.Equal(input.Slots[index].EndsAt) {
			return false
		}
	}
	existingUsers := make([]string, 0, len(poll.Participants))
	requestedUsers := make([]string, 0, len(input.Participants))
	for _, participant := range poll.Participants {
		value := participant.Kind + ":"
		if participant.InternalUserID != nil {
			value += participant.InternalUserID.String()
		}
		existingUsers = append(existingUsers, value)
	}
	for _, participant := range input.Participants {
		value := participant.Kind + ":"
		if participant.InternalUserID != nil {
			value += participant.InternalUserID.String()
		}
		requestedUsers = append(requestedUsers, value)
	}
	sort.Strings(existingUsers)
	sort.Strings(requestedUsers)
	return strings.Join(existingUsers, "|") == strings.Join(requestedUsers, "|")
}

func pollRequestFingerprint(value any) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func (repository *PostgresAvailabilityPollRepository) loadStudyMeeting(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	meetingID uuid.UUID,
	lock bool,
	allowSafetyAdmin bool,
) (StudyMeeting, error) {
	query := `SELECT id, class_id, owner_user_id, source_poll_id, title, starts_at,
       ends_at, timezone, status, version, cancelled_at, created_at, updated_at
FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND id = $2 AND (owner_user_id = $3 OR $4::boolean)`
	if lock {
		query += " FOR UPDATE"
	}
	meeting, err := scanStudyMeeting(transaction.QueryRow(
		ctx, query, scope.TenantID, meetingID, scope.ActorID, allowSafetyAdmin,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return StudyMeeting{}, ErrStudyMeetingNotFound
	}
	if err != nil {
		return StudyMeeting{}, repository.unavailable("load study meeting", err)
	}
	return meeting, nil
}

func scanStudyMeeting(row interface{ Scan(...any) error }) (StudyMeeting, error) {
	var meeting StudyMeeting
	var classID uuid.NullUUID
	var sourcePollID uuid.NullUUID
	if err := row.Scan(
		&meeting.ID, &classID, &meeting.OwnerUserID, &sourcePollID,
		&meeting.Title, &meeting.StartsAt, &meeting.EndsAt, &meeting.Timezone,
		&meeting.Status, &meeting.Version, &meeting.CancelledAt,
		&meeting.CreatedAt, &meeting.UpdatedAt,
	); err != nil {
		return StudyMeeting{}, err
	}
	if classID.Valid {
		value := classID.UUID
		meeting.ClassID = &value
	}
	if sourcePollID.Valid {
		value := sourcePollID.UUID
		meeting.SourcePollID = &value
	}
	meeting.StartsAt, meeting.EndsAt = meeting.StartsAt.UTC(), meeting.EndsAt.UTC()
	meeting.CreatedAt, meeting.UpdatedAt = meeting.CreatedAt.UTC(), meeting.UpdatedAt.UTC()
	if meeting.CancelledAt != nil {
		value := meeting.CancelledAt.UTC()
		meeting.CancelledAt = &value
	}
	return meeting, nil
}

func requireNoStudyMeetingConflict(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	ownerID uuid.UUID,
	excludedMeetingID uuid.UUID,
	startsAt time.Time,
	endsAt time.Time,
) error {
	lockKey := "study-meeting-conflict:" + tenantID.String() + ":" + ownerID.String()
	if err := acquireAvailabilityPollTransactionLock(ctx, transaction, lockKey); err != nil {
		return fmt.Errorf("lock study meeting conflict scope: %w", err)
	}
	var conflict bool
	if err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.study_meetings AS meeting
    WHERE meeting.tenant_id = $1
      AND meeting.owner_user_id = $2
      AND meeting.status = 'scheduled'
      AND meeting.id <> $3
      AND meeting.starts_at < $5 AND meeting.ends_at > $4
    UNION ALL
    SELECT 1
    FROM tutorhub.class_sessions AS session
    WHERE session.tenant_id = $1
      AND session.status IN ('scheduled', 'live')
      AND session.starts_at < $5 AND session.ends_at > $4
      AND (
          session.organizer_user_id = $2
          OR EXISTS (
              SELECT 1 FROM tutorhub.class_session_attendees AS attendee
              WHERE attendee.tenant_id = session.tenant_id
                AND attendee.session_id = session.id
                AND attendee.internal_user_id = $2
                AND attendee.status = 'active'
          )
      )
)`,
		tenantID, ownerID, excludedMeetingID, startsAt.UTC(), endsAt.UTC(),
	).Scan(&conflict); err != nil {
		return fmt.Errorf("check study meeting conflict: %w", err)
	}
	if conflict {
		return ErrStudyMeetingConflict
	}
	participant := schedulingParticipant{ID: ownerID}
	sources := []availabilitySource{{Participant: participant}}
	if err := loadRecurringAvailability(
		ctx,
		transaction,
		tenantID,
		[]uuid.UUID{ownerID},
		availabilityParams{
			From: startsAt.UTC(), To: endsAt.UTC(), Timezone: "UTC",
		},
		sources,
		map[string]int{"internal_user:" + ownerID.String(): 0},
		true,
	); err != nil {
		return fmt.Errorf("check recurring class session conflict: %w", err)
	}
	if len(sources[0].Intervals) > 0 {
		return ErrStudyMeetingConflict
	}
	return nil
}

func (repository *PostgresAvailabilityPollRepository) requireStudyMeetingCreationQuota(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	endsAt time.Time,
	now time.Time,
) error {
	if endsAt.After(now) {
		if err := repository.requireStudyMeetingCapacity(
			ctx, transaction, tenantID, uuid.Nil, now,
		); err != nil {
			return err
		}
	}
	_, err := repository.controls.ConsumeRateQuota(
		ctx,
		transaction,
		tenantID,
		featurecontrol.QuotaStudyMeetingCreationsPerHour,
		now,
	)
	return err
}

func (repository *PostgresAvailabilityPollRepository) requireStudyMeetingCapacity(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	excludedMeetingID uuid.UUID,
	now time.Time,
) error {
	if err := acquireAvailabilityPollTransactionLock(
		ctx, transaction, "active-study-meeting-quota:"+tenantID.String(),
	); err != nil {
		return repository.unavailable("lock active study meeting quota", err)
	}
	var activeMeetings int64
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND status = 'scheduled' AND ends_at > $2 AND id <> $3`,
		tenantID, now.UTC(), excludedMeetingID,
	).Scan(&activeMeetings); err != nil {
		return repository.unavailable("count active study meetings", err)
	}
	return repository.controls.RequireQuotaAtMost(
		ctx,
		transaction,
		tenantID,
		featurecontrol.QuotaActiveStudyMeetings,
		activeMeetings+1,
	)
}

func acquireAvailabilityPollTransactionLock(
	ctx context.Context,
	transaction pgx.Tx,
	key string,
) error {
	_, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`,
		key,
	)
	return err
}

func appendAvailabilityPollEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	poll AvailabilityPoll,
	actorID uuid.UUID,
	eventType string,
	metadata audit.Metadata,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'availability_poll', $2, $3,
    jsonb_build_object(
        'poll_id', $2::uuid,
        'actor_user_id', $4::uuid,
        'status', $5::text,
        'version', $6::bigint
    ),
    $7, $7
)`,
		tenantID, poll.ID, eventType, nullableUUIDValue(actorID),
		poll.Status, poll.Version, now,
	); err != nil {
		return fmt.Errorf("append %s outbox event: %w", eventType, err)
	}
	if metadata == nil {
		metadata = audit.Metadata{}
	}
	metadata["status"] = poll.Status
	metadata["version"] = fmt.Sprintf("%d", poll.Version)
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: tenantID, ActorID: actorID, EventType: eventType,
		AggregateType: "availability_poll", AggregateID: poll.ID,
		Metadata: metadata, OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("append %s audit event: %w", eventType, err)
	}
	return nil
}

func appendStudyMeetingEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	meeting StudyMeeting,
	actorID uuid.UUID,
	eventType string,
	metadata audit.Metadata,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'study_meeting', $2, $3,
    jsonb_build_object(
        'meeting_id', $2::uuid,
        'actor_user_id', $4::uuid,
        'status', $5::text,
        'version', $6::bigint,
        'starts_at', $7::timestamptz,
        'ends_at', $8::timestamptz,
        'timezone', $9::text
    ),
    $10, $10
)`,
		tenantID, meeting.ID, eventType, nullableUUIDValue(actorID), meeting.Status,
		meeting.Version, meeting.StartsAt, meeting.EndsAt, meeting.Timezone, now,
	); err != nil {
		return fmt.Errorf("append %s outbox event: %w", eventType, err)
	}
	if metadata == nil {
		metadata = audit.Metadata{}
	}
	metadata["status"] = meeting.Status
	metadata["version"] = fmt.Sprintf("%d", meeting.Version)
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: tenantID, ActorID: actorID, EventType: eventType,
		AggregateType: "study_meeting", AggregateID: meeting.ID,
		Metadata:   metadata,
		OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("append %s audit event: %w", eventType, err)
	}
	return nil
}

func scanAvailabilityPollCapability(
	row interface{ Scan(...any) error },
) (AvailabilityPollCapability, error) {
	var capability AvailabilityPollCapability
	var participantID uuid.NullUUID
	if err := row.Scan(
		&capability.ID, &capability.PollID, &participantID, &capability.Scope,
		&capability.ExpiresAt, &capability.RevokedAt, &capability.CreatedAt,
	); err != nil {
		return AvailabilityPollCapability{}, err
	}
	if participantID.Valid {
		value := participantID.UUID
		capability.ParticipantID = &value
	}
	capability.ExpiresAt, capability.CreatedAt =
		capability.ExpiresAt.UTC(), capability.CreatedAt.UTC()
	if capability.RevokedAt != nil {
		value := capability.RevokedAt.UTC()
		capability.RevokedAt = &value
	}
	return capability, nil
}

func mapAvailabilityPollPostgresError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch postgresError.Code {
	case "23505":
		if strings.Contains(postgresError.ConstraintName, "idempotency") ||
			strings.Contains(postgresError.ConstraintName, "receipts") {
			return ErrAvailabilityPollIdempotencyConflict
		}
		return ErrAvailabilityPollConflict
	case "23503":
		return ErrAvailabilityPollNotFound
	case "23514", "22P02", "22007":
		return ErrAvailabilityPollInvalid
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func (repository *PostgresAvailabilityPollRepository) unavailable(
	operation string,
	err error,
) error {
	return fmt.Errorf("%s: %w: %v", operation, ErrAvailabilityPollUnavailable, err)
}

func pollRangeDayCount(input CreateAvailabilityPollInput) int64 {
	start, _ := time.Parse("2006-01-02", input.RangeStart)
	end, _ := time.Parse("2006-01-02", input.RangeEnd)
	return int64(end.Sub(start)/(24*time.Hour)) + 1
}

func classResourceState(status string) policy.ResourceState {
	switch status {
	case "draft":
		return policy.ResourceStateDraft
	case "active":
		return policy.ResourceStateActive
	case "archived":
		return policy.ResourceStateArchived
	default:
		return policy.ResourceStateUnknown
	}
}

func hasPolicyPermission(values []policy.Permission, target policy.Permission) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableUUIDValue(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableUUIDEqual(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func nullableInfinityTime(value time.Time) any {
	if value.IsZero() {
		return "-infinity"
	}
	return value.UTC()
}

func rollbackAvailabilityPoll(transaction pgx.Tx) {
	if transaction != nil {
		_ = transaction.Rollback(context.Background())
	}
}

var _ AvailabilityPollRepository = (*PostgresAvailabilityPollRepository)(nil)
