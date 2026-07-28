package calendar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	maximumAvailabilityIntervalsPerParticipant = 2_000
	maximumAvailabilitySeries                  = 128
)

// PostgresSchedulingRepository is deliberately separate from the Calendar
// read-projection repository. Scheduling Assistant has a stronger feature,
// authorization, privacy and resource-boundary contract than the ordinary
// calendar feed.
type PostgresSchedulingRepository struct {
	database     transactionDatabase
	queryTimeout time.Duration
	authorizer   policy.Authorizer
	controls     featurecontrol.Enforcer
}

type schedulingScopeDefaults struct {
	organizationRole policy.OrganizationRole
	tenantTimezone   string
}

type persistedCivilInterval struct {
	startMinute int
	endMinute   int
}

type persistedScheduleException struct {
	kind      string
	intervals []persistedCivilInterval
}

type persistedWorkingSchedule struct {
	id         uuid.UUID
	timezone   string
	weekly     map[int][]persistedCivilInterval
	exceptions map[string]persistedScheduleException
}

func NewPostgresSchedulingRepository(
	database transactionDatabase,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	controls featurecontrol.Enforcer,
) (*PostgresSchedulingRepository, error) {
	if database == nil || authorizer == nil || controls == nil {
		return nil, fmt.Errorf(
			"calendar scheduling database, policy authorizer and feature controls are required",
		)
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresSchedulingRepository{
		database: database, queryTimeout: queryTimeout,
		authorizer: authorizer, controls: controls,
	}, nil
}

func (repository *PostgresSchedulingRepository) GetWorkingSchedule(
	ctx context.Context,
	scope tenancy.Context,
	now time.Time,
) (WorkingSchedule, error) {
	if repository == nil {
		return WorkingSchedule{}, ErrSchedulingUnavailable
	}
	if err := scope.Validate(); err != nil {
		return WorkingSchedule{}, ErrAccessDenied
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return WorkingSchedule{}, schedulingStorageError("begin working schedule query", err)
	}
	defer rollback(transaction)
	if err := setSchedulingRepeatableRead(queryContext, transaction); err != nil {
		return WorkingSchedule{}, err
	}
	if err := repository.requireSchedulingFeature(
		queryContext, transaction, scope.TenantID,
	); err != nil {
		return WorkingSchedule{}, err
	}
	defaults, err := repository.authorizeSchedulingScope(
		queryContext, transaction, scope,
	)
	if err != nil {
		return WorkingSchedule{}, err
	}

	scheduleID, timezone, version, updatedAt, source, found, err :=
		lockEffectiveWorkingSchedule(queryContext, transaction, scope)
	if err != nil {
		return WorkingSchedule{}, err
	}
	if !found {
		result := WorkingSchedule{
			Timezone: defaults.tenantTimezone, Source: "tenant_default", Version: 0,
			WeeklyIntervals: []WeeklyWorkingInterval{},
			Exceptions:      []WorkingScheduleException{},
			UpdatedAt:       now.UTC(),
		}
		if err := transaction.Commit(queryContext); err != nil {
			return WorkingSchedule{}, schedulingStorageError(
				"commit synthetic working schedule query", err,
			)
		}
		return result, nil
	}
	weekly, exceptions, err := loadWorkingScheduleChildren(
		queryContext, transaction, scope.TenantID, scheduleID,
	)
	if err != nil {
		return WorkingSchedule{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return WorkingSchedule{}, schedulingStorageError("commit working schedule query", err)
	}
	if source == "tenant_default" {
		// The version exposed by this self-scoped endpoint is the user's override
		// CAS version, not the administrator-owned default schedule version.
		version = 0
	}
	return WorkingSchedule{
		Timezone: timezone, WeeklyIntervals: weekly, Exceptions: exceptions,
		Source: source, Version: version, UpdatedAt: updatedAt.UTC(),
	}, nil
}

func (repository *PostgresSchedulingRepository) PutWorkingSchedule(
	ctx context.Context,
	scope tenancy.Context,
	input PutWorkingScheduleInput,
	updatedAt time.Time,
) (WorkingSchedule, error) {
	if repository == nil {
		return WorkingSchedule{}, ErrSchedulingUnavailable
	}
	if err := scope.Validate(); err != nil {
		return WorkingSchedule{}, ErrAccessDenied
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return WorkingSchedule{}, schedulingStorageError("begin working schedule update", err)
	}
	defer rollback(transaction)
	if err := repository.requireSchedulingFeature(
		queryContext, transaction, scope.TenantID,
	); err != nil {
		return WorkingSchedule{}, err
	}
	if _, err := repository.authorizeSchedulingScope(
		queryContext, transaction, scope,
	); err != nil {
		return WorkingSchedule{}, err
	}

	var scheduleID uuid.UUID
	var version int64
	var storedUpdatedAt time.Time
	if input.ExpectedVersion == 0 {
		err = transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.calendar_working_schedules (
    tenant_id, scope, owner_user_id, timezone, version,
    created_by, updated_by, created_at, updated_at
)
VALUES ($1, 'user_override', $2, $3, 1, $2, $2, $4, $4)
ON CONFLICT DO NOTHING
RETURNING id, version, updated_at`,
			scope.TenantID,
			scope.ActorID,
			input.Timezone,
			updatedAt.UTC(),
		).Scan(&scheduleID, &version, &storedUpdatedAt)
	} else {
		err = transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.calendar_working_schedules
SET timezone = $3,
    version = version + 1,
    updated_by = $2,
    updated_at = GREATEST($4, updated_at)
WHERE tenant_id = $1
  AND scope = 'user_override'
  AND owner_user_id = $2
  AND version = $5
RETURNING id, version, updated_at`,
			scope.TenantID,
			scope.ActorID,
			input.Timezone,
			updatedAt.UTC(),
			input.ExpectedVersion,
		).Scan(&scheduleID, &version, &storedUpdatedAt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkingSchedule{}, ErrWorkingScheduleStale
	}
	if err != nil {
		return WorkingSchedule{}, mapWorkingScheduleWriteError(
			"write working schedule parent", err,
		)
	}
	if err := replaceWorkingScheduleChildren(
		queryContext,
		transaction,
		scope.TenantID,
		scheduleID,
		scope.ActorID,
		input,
	); err != nil {
		return WorkingSchedule{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return WorkingSchedule{}, schedulingStorageError("commit working schedule update", err)
	}
	return workingScheduleFromInput(input, version, storedUpdatedAt), nil
}

func (repository *PostgresSchedulingRepository) LoadAvailability(
	ctx context.Context,
	scope tenancy.Context,
	params availabilityParams,
) ([]availabilitySource, error) {
	if repository == nil {
		return nil, ErrSchedulingUnavailable
	}
	if err := scope.Validate(); err != nil {
		return nil, ErrAccessDenied
	}
	budget := repository.queryTimeout
	if budget > AvailabilityExecutionBudget {
		budget = AvailabilityExecutionBudget
	}
	queryContext, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, schedulingStorageError("begin availability query", err)
	}
	defer rollback(transaction)
	if err := setSchedulingRepeatableRead(queryContext, transaction); err != nil {
		return nil, err
	}
	if err := repository.requireSchedulingFeature(
		queryContext, transaction, scope.TenantID,
	); err != nil {
		return nil, err
	}
	defaults, err := repository.authorizeSchedulingScope(
		queryContext, transaction, scope,
	)
	if err != nil {
		return nil, err
	}
	if err := repository.authorizeAvailabilityClass(
		queryContext, transaction, scope, defaults, params.ClassID,
	); err != nil {
		return nil, err
	}

	sources := make([]availabilitySource, len(params.Participants))
	sourceIndex := make(map[string]int, len(params.Participants))
	internalIDs := make([]uuid.UUID, 0, len(params.Participants))
	externalIDs := make([]uuid.UUID, 0, len(params.Participants))
	for index, participant := range params.Participants {
		sources[index] = availabilitySource{Participant: participant}
		key := availabilityParticipantKey(participant.Reference)
		sourceIndex[key] = index
		switch participant.Reference.Kind {
		case "internal_user":
			internalIDs = append(internalIDs, participant.ID)
		case "external_guest":
			externalIDs = append(externalIDs, participant.ID)
			sources[index].Unknown = true
		default:
			return nil, ErrInvalidInput
		}
	}
	if err := validateAvailabilityParticipantsForClass(
		queryContext,
		transaction,
		scope.TenantID,
		params.ClassID,
		internalIDs,
		externalIDs,
	); err != nil {
		return nil, err
	}

	schedules, err := loadEffectiveParticipantSchedules(
		queryContext,
		transaction,
		scope.TenantID,
		defaults.tenantTimezone,
		internalIDs,
		params,
	)
	if err != nil {
		return nil, err
	}
	type expandedSchedule struct {
		working     []AvailabilityWorkingInterval
		outOfOffice []AvailabilityStatusInterval
	}
	expandedSchedules := make(map[*persistedWorkingSchedule]expandedSchedule)
	for _, participantID := range internalIDs {
		source := &sources[sourceIndex["internal_user:"+participantID.String()]]
		schedule := schedules[participantID]
		expanded, ok := expandedSchedules[schedule]
		if !ok {
			working, outOfOffice, expandErr := expandPersistedWorkingSchedule(
				schedule, params,
			)
			if expandErr != nil {
				return nil, expandErr
			}
			expanded = expandedSchedule{working: working, outOfOffice: outOfOffice}
			expandedSchedules[schedule] = expanded
		}
		source.WorkingIntervals = append(
			[]AvailabilityWorkingInterval(nil), expanded.working...,
		)
		source.Intervals = append(source.Intervals, expanded.outOfOffice...)
	}
	if err := loadOneTimeAvailability(
		queryContext,
		transaction,
		scope.TenantID,
		internalIDs,
		params,
		sources,
		sourceIndex,
	); err != nil {
		return nil, err
	}
	if err := loadRecurringAvailability(
		queryContext,
		transaction,
		scope.TenantID,
		internalIDs,
		params,
		sources,
		sourceIndex,
	); err != nil {
		return nil, err
	}
	for index := range sources {
		if sources[index].Unknown {
			continue
		}
		sources[index].Intervals = compactAvailabilityStatusIntervals(
			sources[index].Intervals,
		)
		if len(sources[index].Intervals) > maximumAvailabilityIntervalsPerParticipant {
			return nil, ErrSchedulingUnavailable
		}
		sources[index].WorkingIntervals = compactAvailabilityWorkingIntervals(
			sources[index].WorkingIntervals,
		)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, schedulingStorageError("commit availability query", err)
	}
	return sources, nil
}

func (repository *PostgresSchedulingRepository) authorizeAvailabilityClass(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	defaults schedulingScopeDefaults,
	classID uuid.UUID,
) error {
	var classState policy.ResourceState
	var ownerID uuid.UUID
	var enrollmentRole, enrollmentStatus sql.NullString
	err := transaction.QueryRow(
		ctx,
		`SELECT class.status, class.owner_user_id,
       enrollment.class_role, enrollment.status
FROM tutorhub.classes AS class
LEFT JOIN tutorhub.class_enrollments AS enrollment
  ON enrollment.tenant_id = class.tenant_id
 AND enrollment.class_id = class.id
 AND enrollment.user_id = $3
WHERE class.tenant_id = $1 AND class.id = $2
FOR SHARE OF class`,
		scope.TenantID,
		classID,
		scope.ActorID,
	).Scan(&classState, &ownerID, &enrollmentRole, &enrollmentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSchedulingNotFound
	}
	if err != nil {
		return schedulingStorageError("authorize availability class", err)
	}
	classRoles := make([]policy.ClassRole, 0, 2)
	if ownerID == scope.ActorID {
		classRoles = append(classRoles, policy.ClassRoleOwner)
	}
	if enrollmentRole.Valid && enrollmentStatus.Valid &&
		enrollmentStatus.String == "active" {
		classRoles = append(classRoles, policy.ClassRole(enrollmentRole.String))
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: scope.ActorID, ActiveTenantID: scope.TenantID,
			MembershipActive:  true,
			OrganizationRoles: []policy.OrganizationRole{defaults.organizationRole},
			ClassRoles:        classRoles,
		},
		Action: policy.ActionSessionSchedule,
		Resource: policy.Resource{
			TenantID: scope.TenantID, ClassID: classID, State: classState,
		},
	})
	if !decision.Allowed {
		return ErrSchedulingNotFound
	}
	return nil
}

// validateAvailabilityParticipantsForClass prevents a class-scoped scheduling
// capability from being used to enumerate availability elsewhere in the tenant.
// Internal members must be the class owner or an active class enrollment. External
// guests must already be attached to an active audience row for this same class.
// Missing and ineligible references deliberately share one concealed outcome.
func validateAvailabilityParticipantsForClass(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	internalIDs []uuid.UUID,
	externalIDs []uuid.UUID,
) error {
	if len(internalIDs) > 0 {
		rows, err := transaction.Query(
			ctx,
			`SELECT DISTINCT requested.user_id
FROM unnest($3::uuid[]) AS requested(user_id)
JOIN tutorhub.classes AS class
  ON class.tenant_id = $1
 AND class.id = $2
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = $1
 AND membership.user_id = requested.user_id
 AND membership.status = 'active'
JOIN tutorhub.users AS user_account
  ON user_account.id = requested.user_id
 AND user_account.status = 'active'
LEFT JOIN tutorhub.class_enrollments AS enrollment
  ON enrollment.tenant_id = $1
 AND enrollment.class_id = $2
 AND enrollment.user_id = requested.user_id
 AND enrollment.status = 'active'
WHERE class.owner_user_id = requested.user_id
   OR enrollment.user_id IS NOT NULL`,
			tenantID,
			classID,
			internalIDs,
		)
		if err != nil {
			return schedulingStorageError("validate class-scoped internal availability participants", err)
		}
		found := make(map[uuid.UUID]struct{}, len(internalIDs))
		for rows.Next() {
			var userID uuid.UUID
			if err := rows.Scan(&userID); err != nil {
				rows.Close()
				return schedulingStorageError("scan internal availability participant", err)
			}
			found[userID] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return schedulingStorageError("iterate internal availability participants", err)
		}
		rows.Close()
		if len(found) != len(internalIDs) {
			return ErrSchedulingNotFound
		}
	}
	if len(externalIDs) > 0 {
		rows, err := transaction.Query(
			ctx,
			`SELECT DISTINCT requested.id
FROM unnest($3::uuid[]) AS requested(id)
JOIN tutorhub.class_session_attendees AS attendee
  ON attendee.tenant_id = $1
 AND attendee.class_id = $2
 AND attendee.external_recipient_id = requested.id
 AND attendee.status = 'active'
JOIN tutorhub.calendar_external_recipients AS recipient
  ON recipient.tenant_id = attendee.tenant_id
 AND recipient.id = attendee.external_recipient_id
 AND recipient.status = 'active'`,
			tenantID,
			classID,
			externalIDs,
		)
		if err != nil {
			return schedulingStorageError("validate class-scoped external availability participants", err)
		}
		found := make(map[uuid.UUID]struct{}, len(externalIDs))
		for rows.Next() {
			var recipientID uuid.UUID
			if err := rows.Scan(&recipientID); err != nil {
				rows.Close()
				return schedulingStorageError("scan external availability participant", err)
			}
			found[recipientID] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return schedulingStorageError("iterate external availability participant", err)
		}
		rows.Close()
		if len(found) != len(externalIDs) {
			return ErrSchedulingNotFound
		}
	}
	return nil
}

func loadEffectiveParticipantSchedules(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	tenantTimezone string,
	participantIDs []uuid.UUID,
	params availabilityParams,
) (map[uuid.UUID]*persistedWorkingSchedule, error) {
	result := make(map[uuid.UUID]*persistedWorkingSchedule, len(participantIDs))
	if len(participantIDs) == 0 {
		return result, nil
	}
	rows, err := transaction.Query(
		ctx,
		`WITH requested AS (
    SELECT unnest($2::uuid[]) AS user_id
)
SELECT requested.user_id,
       COALESCE(user_schedule.id, tenant_schedule.id),
       COALESCE(user_schedule.timezone, tenant_schedule.timezone, $3)
FROM requested
LEFT JOIN tutorhub.calendar_working_schedules AS user_schedule
  ON user_schedule.tenant_id = $1
 AND user_schedule.scope = 'user_override'
 AND user_schedule.owner_user_id = requested.user_id
LEFT JOIN tutorhub.calendar_working_schedules AS tenant_schedule
  ON tenant_schedule.tenant_id = $1
 AND tenant_schedule.scope = 'tenant_default'
 AND tenant_schedule.owner_user_id IS NULL
ORDER BY requested.user_id`,
		tenantID,
		participantIDs,
		tenantTimezone,
	)
	if err != nil {
		return nil, schedulingStorageError("query effective participant schedules", err)
	}
	byScheduleID := make(map[uuid.UUID]*persistedWorkingSchedule)
	scheduleIDs := make([]uuid.UUID, 0, len(participantIDs))
	for rows.Next() {
		var participantID uuid.UUID
		var scheduleID uuid.NullUUID
		var timezone string
		if err := rows.Scan(&participantID, &scheduleID, &timezone); err != nil {
			rows.Close()
			return nil, schedulingStorageError("scan effective participant schedule", err)
		}
		if !scheduleID.Valid {
			result[participantID] = newPersistedWorkingSchedule(uuid.Nil, timezone)
			continue
		}
		schedule := byScheduleID[scheduleID.UUID]
		if schedule == nil {
			schedule = newPersistedWorkingSchedule(scheduleID.UUID, timezone)
			byScheduleID[scheduleID.UUID] = schedule
			scheduleIDs = append(scheduleIDs, scheduleID.UUID)
		}
		result[participantID] = schedule
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, schedulingStorageError("iterate effective participant schedules", err)
	}
	rows.Close()
	if len(result) != len(participantIDs) {
		return nil, ErrSchedulingUnavailable
	}
	if len(scheduleIDs) == 0 {
		return result, nil
	}

	rows, err = transaction.Query(
		ctx,
		`SELECT schedule_id, weekday, start_minute, end_minute
FROM tutorhub.calendar_working_schedule_intervals
WHERE tenant_id = $1 AND schedule_id = ANY($2::uuid[])
ORDER BY schedule_id, weekday, start_minute, id`,
		tenantID,
		scheduleIDs,
	)
	if err != nil {
		return nil, schedulingStorageError("query participant weekly schedules", err)
	}
	for rows.Next() {
		var scheduleID uuid.UUID
		var weekday, startMinute, endMinute int
		if err := rows.Scan(&scheduleID, &weekday, &startMinute, &endMinute); err != nil {
			rows.Close()
			return nil, schedulingStorageError("scan participant weekly schedule", err)
		}
		schedule := byScheduleID[scheduleID]
		if schedule == nil {
			rows.Close()
			return nil, ErrSchedulingUnavailable
		}
		schedule.weekly[weekday] = append(
			schedule.weekly[weekday],
			persistedCivilInterval{startMinute: startMinute, endMinute: endMinute},
		)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, schedulingStorageError("iterate participant weekly schedules", err)
	}
	rows.Close()

	minimumDate := params.From.UTC().Add(-48 * time.Hour).Format("2006-01-02")
	maximumDate := params.To.UTC().Add(48 * time.Hour).Format("2006-01-02")
	rows, err = transaction.Query(
		ctx,
		`SELECT exception.schedule_id, exception.exception_date,
       exception.exception_type, interval.start_minute, interval.end_minute
FROM tutorhub.calendar_working_schedule_exceptions AS exception
LEFT JOIN tutorhub.calendar_working_schedule_exception_intervals AS interval
  ON interval.tenant_id = exception.tenant_id
 AND interval.schedule_id = exception.schedule_id
 AND interval.exception_id = exception.id
WHERE exception.tenant_id = $1
  AND exception.schedule_id = ANY($2::uuid[])
  AND exception.exception_date BETWEEN $3::date AND $4::date
ORDER BY exception.schedule_id, exception.exception_date,
         interval.start_minute, interval.id`,
		tenantID,
		scheduleIDs,
		minimumDate,
		maximumDate,
	)
	if err != nil {
		return nil, schedulingStorageError("query participant schedule exceptions", err)
	}
	for rows.Next() {
		var scheduleID uuid.UUID
		var exceptionDate time.Time
		var kind string
		var startMinute, endMinute sql.NullInt64
		if err := rows.Scan(
			&scheduleID, &exceptionDate, &kind, &startMinute, &endMinute,
		); err != nil {
			rows.Close()
			return nil, schedulingStorageError("scan participant schedule exception", err)
		}
		schedule := byScheduleID[scheduleID]
		if schedule == nil {
			rows.Close()
			return nil, ErrSchedulingUnavailable
		}
		key := exceptionDate.Format("2006-01-02")
		exception := schedule.exceptions[key]
		exception.kind = kind
		if startMinute.Valid && endMinute.Valid {
			exception.intervals = append(exception.intervals, persistedCivilInterval{
				startMinute: int(startMinute.Int64), endMinute: int(endMinute.Int64),
			})
		}
		schedule.exceptions[key] = exception
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, schedulingStorageError("iterate participant schedule exceptions", err)
	}
	rows.Close()
	return result, nil
}

func newPersistedWorkingSchedule(
	id uuid.UUID,
	timezone string,
) *persistedWorkingSchedule {
	return &persistedWorkingSchedule{
		id: id, timezone: timezone,
		weekly:     make(map[int][]persistedCivilInterval, 7),
		exceptions: make(map[string]persistedScheduleException),
	}
}

func expandPersistedWorkingSchedule(
	schedule *persistedWorkingSchedule,
	params availabilityParams,
) ([]AvailabilityWorkingInterval, []AvailabilityStatusInterval, error) {
	if schedule == nil {
		return nil, nil, ErrSchedulingUnavailable
	}
	location, err := time.LoadLocation(schedule.timezone)
	if err != nil {
		return nil, nil, ErrSchedulingUnavailable
	}
	firstLocal := params.From.In(location)
	lastLocal := params.To.In(location)
	date := time.Date(
		firstLocal.Year(), firstLocal.Month(), firstLocal.Day(), 0, 0, 0, 0, time.UTC,
	).AddDate(0, 0, -1)
	lastDate := time.Date(
		lastLocal.Year(), lastLocal.Month(), lastLocal.Day(), 0, 0, 0, 0, time.UTC,
	).AddDate(0, 0, 1)
	working := make([]AvailabilityWorkingInterval, 0, 32)
	outOfOffice := make([]AvailabilityStatusInterval, 0, 4)
	for !date.After(lastDate) {
		weekday := databaseWeekday(date.Weekday())
		intervals := schedule.weekly[weekday]
		exception, hasException := schedule.exceptions[date.Format("2006-01-02")]
		if hasException {
			switch exception.kind {
			case "holiday":
				intervals = nil
			case "out_of_office":
				intervals = nil
				dayStarts := resolveScheduleBoundary(date, 0, location)
				dayEnds := resolveScheduleBoundary(date.AddDate(0, 0, 1), 0, location)
				if len(dayStarts) > 0 && len(dayEnds) > 0 {
					start := dayStarts[0]
					end := dayEnds[len(dayEnds)-1]
					if intervalOverlapsRange(start, end, params.From, params.To) {
						outOfOffice = append(outOfOffice, AvailabilityStatusInterval{
							StartsAt: maxTime(start, params.From),
							EndsAt:   minTime(end, params.To),
							Status:   "out_of_office",
						})
					}
				}
			case "special_hours":
				intervals = exception.intervals
			default:
				return nil, nil, ErrSchedulingUnavailable
			}
		}
		for _, value := range resolveScheduleCivilIntervals(date, intervals, location) {
			if !intervalOverlapsRange(
				value.StartsAt, value.EndsAt, params.From, params.To,
			) {
				continue
			}
			working = append(working, AvailabilityWorkingInterval{
				StartsAt: maxTime(value.StartsAt, params.From),
				EndsAt:   minTime(value.EndsAt, params.To),
			})
		}
		date = date.AddDate(0, 0, 1)
	}
	return compactAvailabilityWorkingIntervals(working),
		compactAvailabilityStatusIntervals(outOfOffice), nil
}

func databaseWeekday(value time.Weekday) int {
	if value == time.Sunday {
		return 7
	}
	return int(value)
}

// resolveScheduleCivilIntervals evaluates a local civil day minute-by-minute.
// This is bounded (at most 26 hours) and, unlike pairing ambiguous boundaries,
// represents a fall-back overlap without marking the repeated gap between two
// civil ranges as working time.
func resolveScheduleCivilIntervals(
	date time.Time,
	intervals []persistedCivilInterval,
	location *time.Location,
) []AvailabilityWorkingInterval {
	if len(intervals) == 0 {
		return []AvailabilityWorkingInterval{}
	}
	dayStarts := resolveScheduleBoundary(date, 0, location)
	dayEnds := resolveScheduleBoundary(date.AddDate(0, 0, 1), 0, location)
	if len(dayStarts) == 0 || len(dayEnds) == 0 {
		return []AvailabilityWorkingInterval{}
	}
	dayStart := dayStarts[0]
	dayEnd := dayEnds[len(dayEnds)-1]
	dateKey := date.Format("2006-01-02")
	result := make([]AvailabilityWorkingInterval, 0, len(intervals)+1)
	var runStart time.Time
	for instant := dayStart; instant.Before(dayEnd); instant = instant.Add(time.Minute) {
		local := instant.In(location)
		minute := local.Hour()*60 + local.Minute()
		working := local.Format("2006-01-02") == dateKey &&
			civilMinuteInIntervals(minute, intervals)
		if working && runStart.IsZero() {
			runStart = instant
		}
		if !working && !runStart.IsZero() {
			result = append(result, AvailabilityWorkingInterval{
				StartsAt: runStart.UTC(), EndsAt: instant.UTC(),
			})
			runStart = time.Time{}
		}
	}
	if !runStart.IsZero() {
		result = append(result, AvailabilityWorkingInterval{
			StartsAt: runStart.UTC(), EndsAt: dayEnd.UTC(),
		})
	}
	return result
}

func civilMinuteInIntervals(
	minute int,
	intervals []persistedCivilInterval,
) bool {
	for _, interval := range intervals {
		if minute >= interval.startMinute && minute < interval.endMinute {
			return true
		}
	}
	return false
}

func resolveScheduleBoundary(
	date time.Time,
	minute int,
	location *time.Location,
) []time.Time {
	civil := time.Date(
		date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC,
	).Add(time.Duration(minute) * time.Minute)
	for adjustment := 0; adjustment <= 180; adjustment++ {
		resolved := resolveCivilInstants(civil.Add(time.Duration(adjustment)*time.Minute), location)
		if len(resolved) > 0 {
			return resolved
		}
	}
	return nil
}

func loadOneTimeAvailability(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	participantIDs []uuid.UUID,
	params availabilityParams,
	sources []availabilitySource,
	sourceIndex map[string]int,
) error {
	if len(participantIDs) == 0 {
		return nil
	}
	rows, err := transaction.Query(
		ctx,
		`WITH requested AS (
    SELECT unnest($2::uuid[]) AS user_id
), attendee_busy AS (
    SELECT requested.user_id, session.starts_at, session.ends_at,
           attendee.show_as
    FROM requested
    JOIN tutorhub.class_session_attendees AS attendee
      ON attendee.tenant_id = $1
     AND attendee.internal_user_id = requested.user_id
     AND attendee.session_id IS NOT NULL
     AND attendee.status = 'active'
    JOIN tutorhub.class_sessions AS session
      ON session.tenant_id = attendee.tenant_id
     AND session.class_id = attendee.class_id
     AND session.id = attendee.session_id
    WHERE session.status <> 'cancelled'
      AND session.starts_at < $4
      AND session.ends_at > $3
), organizer_busy AS (
    SELECT requested.user_id, session.starts_at, session.ends_at,
           session.show_as
    FROM requested
    JOIN tutorhub.class_sessions AS session
      ON session.tenant_id = $1
     AND session.organizer_user_id = requested.user_id
    WHERE session.status <> 'cancelled'
      AND session.starts_at < $4
      AND session.ends_at > $3
      AND NOT EXISTS (
          SELECT 1
          FROM tutorhub.class_session_attendees AS attendee
          WHERE attendee.tenant_id = session.tenant_id
            AND attendee.class_id = session.class_id
            AND attendee.session_id = session.id
            AND attendee.internal_user_id = requested.user_id
            AND attendee.status = 'active'
      )
), ranked AS (
    SELECT busy.user_id, busy.starts_at, busy.ends_at, busy.show_as,
           row_number() OVER (
               PARTITION BY busy.user_id
               ORDER BY busy.starts_at, busy.ends_at, busy.show_as
           ) AS ordinal
    FROM (
        SELECT * FROM attendee_busy
        UNION ALL
        SELECT * FROM organizer_busy
    ) AS busy
)
SELECT user_id, starts_at, ends_at, show_as, ordinal
FROM ranked
WHERE ordinal <= $5
ORDER BY user_id, starts_at, ends_at, show_as`,
		tenantID,
		participantIDs,
		params.From.UTC(),
		params.To.UTC(),
		maximumAvailabilityIntervalsPerParticipant+1,
	)
	if err != nil {
		return schedulingStorageError("query one-time participant availability", err)
	}
	defer rows.Close()
	for rows.Next() {
		var participantID uuid.UUID
		var startsAt, endsAt time.Time
		var showAs string
		var ordinal int
		if err := rows.Scan(
			&participantID, &startsAt, &endsAt, &showAs, &ordinal,
		); err != nil {
			return schedulingStorageError("scan one-time participant availability", err)
		}
		if ordinal > maximumAvailabilityIntervalsPerParticipant {
			return ErrSchedulingUnavailable
		}
		index, ok := sourceIndex["internal_user:"+participantID.String()]
		if !ok {
			return ErrSchedulingUnavailable
		}
		if err := appendAvailabilityStatus(
			&sources[index], startsAt, endsAt, showAs, params,
		); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return schedulingStorageError("iterate one-time participant availability", err)
	}
	return nil
}

type availabilitySeriesAssignment struct {
	status string
	showAs string
}

type availabilitySeriesProjection struct {
	projection  recurrence.SeriesProjection
	organizerID uuid.UUID
	showAs      string
	base        map[uuid.UUID]availabilitySeriesAssignment
	occurrences map[string]map[uuid.UUID]availabilitySeriesAssignment
}

func loadRecurringAvailability(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	participantIDs []uuid.UUID,
	params availabilityParams,
	sources []availabilitySource,
	sourceIndex map[string]int,
) error {
	if len(participantIDs) == 0 {
		return nil
	}
	seriesRows, err := transaction.Query(
		ctx,
		`SELECT series.id, series.class_id, series.local_start, series.timezone,
       series.duration_minutes, series.recurrence_frequency,
       series.recurrence_interval, series.recurrence_weekdays,
       series.recurrence_month_days, series.recurrence_months,
       series.recurrence_end_type, series.recurrence_until_date,
       series.recurrence_count, series.overlap_policy,
       series.organizer_user_id, series.show_as
FROM tutorhub.class_session_series AS series
WHERE series.tenant_id = $1
  AND series.status = 'scheduled'
  AND series.local_start < $4::timestamp
  AND (
      series.recurrence_end_type = 'after_count'
      OR series.recurrence_until_date >= $3::date
  )
  AND (
      series.organizer_user_id = ANY($2::uuid[])
      OR EXISTS (
          SELECT 1
          FROM tutorhub.class_session_attendees AS attendee
          WHERE attendee.tenant_id = series.tenant_id
            AND attendee.class_id = series.class_id
            AND attendee.series_id = series.id
            AND attendee.internal_user_id = ANY($2::uuid[])
            AND attendee.status = 'active'
      )
  )
ORDER BY series.id
LIMIT $5`,
		tenantID,
		participantIDs,
		params.From.UTC().Add(-48*time.Hour).Format("2006-01-02"),
		params.To.UTC().Add(48*time.Hour),
		maximumAvailabilitySeries+1,
	)
	if err != nil {
		return schedulingStorageError("query recurring participant series", err)
	}
	series := make([]availabilitySeriesProjection, 0)
	seriesIDs := make([]uuid.UUID, 0)
	for seriesRows.Next() {
		var value availabilitySeriesProjection
		var localStart time.Time
		var durationMinutes int
		var frequency recurrence.Frequency
		var recurrenceInterval int
		var weekdays []string
		var monthDays, months []int16
		var endType recurrence.EndType
		var untilDate sql.NullTime
		var count sql.NullInt32
		var overlapPolicy recurrence.OverlapPolicy
		if err := seriesRows.Scan(
			&value.projection.ID,
			&value.projection.ClassID,
			&localStart,
			&value.projection.Definition.TimeZone,
			&durationMinutes,
			&frequency,
			&recurrenceInterval,
			&weekdays,
			&monthDays,
			&months,
			&endType,
			&untilDate,
			&count,
			&overlapPolicy,
			&value.organizerID,
			&value.showAs,
		); err != nil {
			seriesRows.Close()
			return schedulingStorageError("scan recurring participant series", err)
		}
		value.projection.Definition.ID = value.projection.ID.String()
		value.projection.Definition.StartLocal = localStart.Format("2006-01-02T15:04:05")
		value.projection.Definition.Duration = time.Duration(durationMinutes) * time.Minute
		value.projection.Definition.OverlapPolicy = overlapPolicy
		value.projection.DisplayTimezone = params.Timezone
		value.projection.Definition.Rule = recurrence.Rule{
			Frequency: frequency, Interval: recurrenceInterval,
			Weekdays:  availabilityCalendarWeekdays(weekdays),
			MonthDays: availabilityCalendarIntegers(monthDays),
			Months:    availabilityCalendarIntegers(months),
			End:       recurrence.End{Type: endType},
		}
		if untilDate.Valid {
			value.projection.Definition.Rule.End.Date = untilDate.Time.Format("2006-01-02")
		}
		if count.Valid {
			value.projection.Definition.Rule.End.Count = int(count.Int32)
		}
		value.base = make(map[uuid.UUID]availabilitySeriesAssignment)
		value.occurrences = make(map[string]map[uuid.UUID]availabilitySeriesAssignment)
		series = append(series, value)
		seriesIDs = append(seriesIDs, value.projection.ID)
	}
	if err := seriesRows.Err(); err != nil {
		seriesRows.Close()
		return schedulingStorageError("iterate recurring participant series", err)
	}
	seriesRows.Close()
	if len(series) > maximumAvailabilitySeries {
		return ErrSchedulingUnavailable
	}
	if len(series) == 0 {
		return nil
	}
	seriesByID := make(map[uuid.UUID]*availabilitySeriesProjection, len(series))
	for index := range series {
		seriesByID[series[index].projection.ID] = &series[index]
	}
	if err := loadAvailabilitySeriesAssignments(
		ctx, transaction, tenantID, participantIDs, seriesIDs, seriesByID,
	); err != nil {
		return err
	}
	exceptions, err := loadAvailabilitySeriesExceptions(
		ctx, transaction, tenantID, seriesIDs,
	)
	if err != nil {
		return err
	}
	for index := range series {
		value := &series[index]
		projected, err := recurrence.Project(
			ctx,
			value.projection,
			recurrence.Window{Start: params.From.UTC(), End: params.To.UTC()},
			exceptions[value.projection.ID],
			recurrence.MaxOccurrencesPerRequest,
		)
		if err != nil {
			return schedulingStorageError("expand recurring participant series", err)
		}
		for _, occurrence := range projected {
			if !intervalOverlapsRange(
				occurrence.StartsAt, occurrence.EndsAt, params.From, params.To,
			) {
				continue
			}
			for _, participantID := range participantIDs {
				present, showAs := value.participantAvailability(
					participantID, occurrence.OccurrenceKey,
				)
				if !present {
					continue
				}
				sourcePosition, ok := sourceIndex["internal_user:"+participantID.String()]
				if !ok {
					return ErrSchedulingUnavailable
				}
				if err := appendAvailabilityStatus(
					&sources[sourcePosition],
					occurrence.StartsAt,
					occurrence.EndsAt,
					showAs,
					params,
				); err != nil {
					return err
				}
				if len(sources[sourcePosition].Intervals) >
					maximumAvailabilityIntervalsPerParticipant {
					return ErrSchedulingUnavailable
				}
			}
		}
	}
	return nil
}

func loadAvailabilitySeriesAssignments(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	participantIDs []uuid.UUID,
	seriesIDs []uuid.UUID,
	seriesByID map[uuid.UUID]*availabilitySeriesProjection,
) error {
	rows, err := transaction.Query(
		ctx,
		`SELECT DISTINCT ON (
    attendee.series_id, attendee.occurrence_key, attendee.internal_user_id
)
    attendee.series_id, attendee.internal_user_id,
    attendee.occurrence_key, attendee.status, attendee.show_as
FROM tutorhub.class_session_attendees AS attendee
WHERE attendee.tenant_id = $1
  AND attendee.series_id = ANY($2::uuid[])
  AND attendee.internal_user_id = ANY($3::uuid[])
ORDER BY attendee.series_id, attendee.occurrence_key NULLS FIRST,
         attendee.internal_user_id, attendee.updated_at DESC,
         attendee.version DESC, attendee.id DESC`,
		tenantID,
		seriesIDs,
		participantIDs,
	)
	if err != nil {
		return schedulingStorageError("query recurring participant assignments", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seriesID, participantID uuid.UUID
		var occurrenceKey sql.NullString
		var assignment availabilitySeriesAssignment
		if err := rows.Scan(
			&seriesID, &participantID, &occurrenceKey,
			&assignment.status, &assignment.showAs,
		); err != nil {
			return schedulingStorageError("scan recurring participant assignment", err)
		}
		value := seriesByID[seriesID]
		if value == nil {
			return ErrSchedulingUnavailable
		}
		if !occurrenceKey.Valid {
			value.base[participantID] = assignment
			continue
		}
		byParticipant := value.occurrences[occurrenceKey.String]
		if byParticipant == nil {
			byParticipant = make(map[uuid.UUID]availabilitySeriesAssignment)
			value.occurrences[occurrenceKey.String] = byParticipant
		}
		byParticipant[participantID] = assignment
	}
	if err := rows.Err(); err != nil {
		return schedulingStorageError("iterate recurring participant assignments", err)
	}
	return nil
}

func loadAvailabilitySeriesExceptions(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	seriesIDs []uuid.UUID,
) (map[uuid.UUID][]recurrence.ExceptionProjection, error) {
	result := make(map[uuid.UUID][]recurrence.ExceptionProjection, len(seriesIDs))
	rows, err := transaction.Query(
		ctx,
		`SELECT series_id, occurrence_key, exception_type,
       override_local_start, override_timezone, override_duration_minutes
FROM tutorhub.class_session_exceptions
WHERE tenant_id = $1 AND series_id = ANY($2::uuid[])
ORDER BY series_id, occurrence_key`,
		tenantID,
		seriesIDs,
	)
	if err != nil {
		return nil, schedulingStorageError("query recurring availability exceptions", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seriesID uuid.UUID
		var exception recurrence.ExceptionProjection
		var localStart sql.NullTime
		var timezone sql.NullString
		var duration sql.NullInt32
		if err := rows.Scan(
			&seriesID,
			&exception.OccurrenceKey,
			&exception.Type,
			&localStart,
			&timezone,
			&duration,
		); err != nil {
			return nil, schedulingStorageError("scan recurring availability exception", err)
		}
		if localStart.Valid {
			value := localStart.Time.Format("2006-01-02T15:04:05")
			exception.OverrideLocalStart = &value
		}
		if timezone.Valid {
			value := timezone.String
			exception.OverrideTimezone = &value
		}
		if duration.Valid {
			value := time.Duration(duration.Int32) * time.Minute
			exception.OverrideDuration = &value
		}
		result[seriesID] = append(result[seriesID], exception)
	}
	if err := rows.Err(); err != nil {
		return nil, schedulingStorageError("iterate recurring availability exceptions", err)
	}
	return result, nil
}

func (series *availabilitySeriesProjection) participantAvailability(
	participantID uuid.UUID,
	occurrenceKey string,
) (bool, string) {
	present := participantID == series.organizerID
	showAs := series.showAs
	if assignment, ok := series.base[participantID]; ok {
		if assignment.status == "active" {
			present = true
			showAs = assignment.showAs
		} else if participantID != series.organizerID {
			present = false
		}
	}
	if assignments := series.occurrences[occurrenceKey]; assignments != nil {
		if assignment, ok := assignments[participantID]; ok {
			if assignment.status == "active" {
				present = true
				showAs = assignment.showAs
			} else if participantID != series.organizerID {
				present = false
			}
		}
	}
	return present, showAs
}

func availabilityCalendarWeekdays(values []string) []recurrence.Weekday {
	result := make([]recurrence.Weekday, 0, len(values))
	for _, value := range values {
		result = append(result, recurrence.Weekday(value))
	}
	return result
}

func availabilityCalendarIntegers(values []int16) []int {
	result := make([]int, 0, len(values))
	for _, value := range values {
		result = append(result, int(value))
	}
	return result
}

func appendAvailabilityStatus(
	source *availabilitySource,
	startsAt time.Time,
	endsAt time.Time,
	showAs string,
	params availabilityParams,
) error {
	if showAs == "free" {
		return nil
	}
	if showAs != "tentative" && showAs != "busy" && showAs != "out_of_office" {
		return ErrSchedulingUnavailable
	}
	startsAt, endsAt = startsAt.UTC(), endsAt.UTC()
	if !intervalOverlapsRange(startsAt, endsAt, params.From, params.To) {
		return nil
	}
	source.Intervals = append(source.Intervals, AvailabilityStatusInterval{
		StartsAt: maxTime(startsAt, params.From),
		EndsAt:   minTime(endsAt, params.To),
		Status:   showAs,
	})
	return nil
}

func compactAvailabilityStatusIntervals(
	values []AvailabilityStatusInterval,
) []AvailabilityStatusInterval {
	if len(values) == 0 {
		return []AvailabilityStatusInterval{}
	}
	byStatus := make(map[string][]AvailabilityStatusInterval, 4)
	for _, value := range values {
		if value.EndsAt.After(value.StartsAt) {
			value.StartsAt = value.StartsAt.UTC()
			value.EndsAt = value.EndsAt.UTC()
			byStatus[value.Status] = append(byStatus[value.Status], value)
		}
	}
	result := make([]AvailabilityStatusInterval, 0, len(values))
	for _, grouped := range byStatus {
		sort.Slice(grouped, func(left int, right int) bool {
			if grouped[left].StartsAt.Equal(grouped[right].StartsAt) {
				return grouped[left].EndsAt.Before(grouped[right].EndsAt)
			}
			return grouped[left].StartsAt.Before(grouped[right].StartsAt)
		})
		for _, value := range grouped {
			if len(result) == 0 || result[len(result)-1].Status != value.Status ||
				result[len(result)-1].EndsAt.Before(value.StartsAt) {
				result = append(result, value)
				continue
			}
			if value.EndsAt.After(result[len(result)-1].EndsAt) {
				result[len(result)-1].EndsAt = value.EndsAt
			}
		}
	}
	sort.Slice(result, func(left int, right int) bool {
		if !result[left].StartsAt.Equal(result[right].StartsAt) {
			return result[left].StartsAt.Before(result[right].StartsAt)
		}
		if result[left].Status != result[right].Status {
			return availabilityStatusRank(result[left].Status) >
				availabilityStatusRank(result[right].Status)
		}
		return result[left].EndsAt.Before(result[right].EndsAt)
	})
	return result
}

func compactAvailabilityWorkingIntervals(
	values []AvailabilityWorkingInterval,
) []AvailabilityWorkingInterval {
	if len(values) == 0 {
		return []AvailabilityWorkingInterval{}
	}
	filtered := values[:0]
	for _, value := range values {
		if value.EndsAt.After(value.StartsAt) {
			value.StartsAt = value.StartsAt.UTC()
			value.EndsAt = value.EndsAt.UTC()
			filtered = append(filtered, value)
		}
	}
	sort.Slice(filtered, func(left int, right int) bool {
		if filtered[left].StartsAt.Equal(filtered[right].StartsAt) {
			return filtered[left].EndsAt.Before(filtered[right].EndsAt)
		}
		return filtered[left].StartsAt.Before(filtered[right].StartsAt)
	})
	result := make([]AvailabilityWorkingInterval, 0, len(filtered))
	for _, value := range filtered {
		if len(result) == 0 || result[len(result)-1].EndsAt.Before(value.StartsAt) {
			result = append(result, value)
			continue
		}
		if value.EndsAt.After(result[len(result)-1].EndsAt) {
			result[len(result)-1].EndsAt = value.EndsAt
		}
	}
	if result == nil {
		return []AvailabilityWorkingInterval{}
	}
	return result
}

func availabilityParticipantKey(reference AvailabilityParticipantReference) string {
	return reference.Kind + ":" + reference.ID
}

func intervalOverlapsRange(
	startsAt time.Time,
	endsAt time.Time,
	rangeStart time.Time,
	rangeEnd time.Time,
) bool {
	return endsAt.After(startsAt) && startsAt.Before(rangeEnd) && endsAt.After(rangeStart)
}

func minTime(left time.Time, right time.Time) time.Time {
	if left.Before(right) {
		return left.UTC()
	}
	return right.UTC()
}

func maxTime(left time.Time, right time.Time) time.Time {
	if left.After(right) {
		return left.UTC()
	}
	return right.UTC()
}

func (repository *PostgresSchedulingRepository) requireSchedulingFeature(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
) error {
	if repository == nil || repository.controls == nil {
		return ErrSchedulingUnavailable
	}
	return repository.controls.RequireFeature(
		ctx,
		transaction,
		tenantID,
		featurecontrol.FeatureClassSessionScheduling,
	)
}

func (repository *PostgresSchedulingRepository) authorizeSchedulingScope(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
) (schedulingScopeDefaults, error) {
	var result schedulingScopeDefaults
	err := transaction.QueryRow(
		ctx,
		`SELECT membership.role, tenant.timezone
FROM tutorhub.tenants AS tenant
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = tenant.id
 AND membership.user_id = $2
JOIN tutorhub.users AS user_account
  ON user_account.id = membership.user_id
WHERE tenant.id = $1
  AND tenant.status = 'active'
  AND membership.status = 'active'
  AND user_account.status = 'active'
FOR SHARE OF tenant, membership, user_account`,
		scope.TenantID,
		scope.ActorID,
	).Scan(&result.organizationRole, &result.tenantTimezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return schedulingScopeDefaults{}, ErrAccessDenied
	}
	if err != nil {
		return schedulingScopeDefaults{}, schedulingStorageError(
			"authorize calendar scheduling scope", err,
		)
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: scope.ActorID, ActiveTenantID: scope.TenantID,
			MembershipActive:  true,
			OrganizationRoles: []policy.OrganizationRole{result.organizationRole},
		},
		Action: policy.ActionTenantView,
		Resource: policy.Resource{
			TenantID: scope.TenantID,
			State:    policy.ResourceStateActive,
		},
	})
	if !decision.Allowed {
		return schedulingScopeDefaults{}, ErrAccessDenied
	}
	return result, nil
}

func setSchedulingRepeatableRead(ctx context.Context, transaction pgx.Tx) error {
	if _, err := transaction.Exec(
		ctx,
		"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ",
	); err != nil {
		return schedulingStorageError("configure scheduling read snapshot", err)
	}
	return nil
}

func lockEffectiveWorkingSchedule(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
) (
	scheduleID uuid.UUID,
	timezone string,
	version int64,
	updatedAt time.Time,
	source string,
	found bool,
	err error,
) {
	err = transaction.QueryRow(
		ctx,
		`SELECT id, timezone, version, updated_at
FROM tutorhub.calendar_working_schedules
WHERE tenant_id = $1
  AND scope = 'user_override'
  AND owner_user_id = $2
FOR SHARE`,
		scope.TenantID,
		scope.ActorID,
	).Scan(&scheduleID, &timezone, &version, &updatedAt)
	if err == nil {
		return scheduleID, timezone, version, updatedAt, "user_override", true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", 0, time.Time{}, "", false,
			schedulingStorageError("query user working schedule", err)
	}
	err = transaction.QueryRow(
		ctx,
		`SELECT id, timezone, version, updated_at
FROM tutorhub.calendar_working_schedules
WHERE tenant_id = $1
  AND scope = 'tenant_default'
  AND owner_user_id IS NULL
FOR SHARE`,
		scope.TenantID,
	).Scan(&scheduleID, &timezone, &version, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", 0, time.Time{}, "", false, nil
	}
	if err != nil {
		return uuid.Nil, "", 0, time.Time{}, "", false,
			schedulingStorageError("query tenant working schedule", err)
	}
	return scheduleID, timezone, version, updatedAt, "tenant_default", true, nil
}

func loadWorkingScheduleChildren(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	scheduleID uuid.UUID,
) ([]WeeklyWorkingInterval, []WorkingScheduleException, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT weekday, start_minute, end_minute
FROM tutorhub.calendar_working_schedule_intervals
WHERE tenant_id = $1 AND schedule_id = $2
ORDER BY weekday, start_minute, id`,
		tenantID,
		scheduleID,
	)
	if err != nil {
		return nil, nil, schedulingStorageError("query weekly working intervals", err)
	}
	weekly := make([]WeeklyWorkingInterval, 0)
	for rows.Next() {
		var weekday, startMinute, endMinute int
		if err := rows.Scan(&weekday, &startMinute, &endMinute); err != nil {
			rows.Close()
			return nil, nil, schedulingStorageError("scan weekly working interval", err)
		}
		weekdayName, ok := calendarWeekdayName(weekday)
		if !ok {
			rows.Close()
			return nil, nil, ErrSchedulingUnavailable
		}
		weekly = append(weekly, WeeklyWorkingInterval{
			Weekday: weekdayName,
			CivilTimeInterval: CivilTimeInterval{
				StartsAt: formatCivilMinute(startMinute),
				EndsAt:   formatCivilMinute(endMinute),
			},
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, schedulingStorageError("iterate weekly working intervals", err)
	}
	rows.Close()

	rows, err = transaction.Query(
		ctx,
		`SELECT exception.id, exception.exception_date, exception.exception_type,
       interval.start_minute, interval.end_minute
FROM tutorhub.calendar_working_schedule_exceptions AS exception
LEFT JOIN tutorhub.calendar_working_schedule_exception_intervals AS interval
  ON interval.tenant_id = exception.tenant_id
 AND interval.schedule_id = exception.schedule_id
 AND interval.exception_id = exception.id
WHERE exception.tenant_id = $1 AND exception.schedule_id = $2
ORDER BY exception.exception_date, exception.id, interval.start_minute, interval.id`,
		tenantID,
		scheduleID,
	)
	if err != nil {
		return nil, nil, schedulingStorageError("query working schedule exceptions", err)
	}
	exceptions := make([]WorkingScheduleException, 0)
	var currentID uuid.UUID
	for rows.Next() {
		var exceptionID uuid.UUID
		var exceptionDate time.Time
		var kind string
		var startMinute, endMinute sql.NullInt64
		if err := rows.Scan(
			&exceptionID, &exceptionDate, &kind, &startMinute, &endMinute,
		); err != nil {
			rows.Close()
			return nil, nil, schedulingStorageError("scan working schedule exception", err)
		}
		if exceptionID != currentID {
			exceptions = append(exceptions, WorkingScheduleException{
				Date: exceptionDate.Format("2006-01-02"), Kind: kind,
				Intervals: []CivilTimeInterval{},
			})
			currentID = exceptionID
		}
		if startMinute.Valid && endMinute.Valid {
			last := &exceptions[len(exceptions)-1]
			last.Intervals = append(last.Intervals, CivilTimeInterval{
				StartsAt: formatCivilMinute(int(startMinute.Int64)),
				EndsAt:   formatCivilMinute(int(endMinute.Int64)),
			})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, schedulingStorageError("iterate working schedule exceptions", err)
	}
	rows.Close()
	return weekly, exceptions, nil
}

func replaceWorkingScheduleChildren(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	scheduleID uuid.UUID,
	actorID uuid.UUID,
	input PutWorkingScheduleInput,
) error {
	if _, err := transaction.Exec(
		ctx,
		`DELETE FROM tutorhub.calendar_working_schedule_exceptions
WHERE tenant_id = $1 AND schedule_id = $2`,
		tenantID,
		scheduleID,
	); err != nil {
		return schedulingStorageError("delete working schedule exceptions", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`DELETE FROM tutorhub.calendar_working_schedule_intervals
WHERE tenant_id = $1 AND schedule_id = $2`,
		tenantID,
		scheduleID,
	); err != nil {
		return schedulingStorageError("delete weekly working intervals", err)
	}

	if len(input.WeeklyIntervals) > 0 {
		weekdays := make([]int16, 0, len(input.WeeklyIntervals))
		starts := make([]int16, 0, len(input.WeeklyIntervals))
		ends := make([]int16, 0, len(input.WeeklyIntervals))
		for _, interval := range input.WeeklyIntervals {
			weekday, ok := calendarWeekdayOrder[interval.Weekday]
			startMinute, startOK := canonicalCivilMinute(interval.StartsAt)
			endMinute, endOK := canonicalCivilMinute(interval.EndsAt)
			if !ok || !startOK || !endOK {
				return ErrInvalidInput
			}
			weekdays = append(weekdays, int16(weekday))
			starts = append(starts, int16(startMinute))
			ends = append(ends, int16(endMinute))
		}
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.calendar_working_schedule_intervals (
    tenant_id, schedule_id, weekday, start_minute, end_minute
)
SELECT $1, $2, input.weekday, input.start_minute, input.end_minute
FROM unnest($3::smallint[], $4::smallint[], $5::smallint[])
    AS input(weekday, start_minute, end_minute)`,
			tenantID,
			scheduleID,
			weekdays,
			starts,
			ends,
		); err != nil {
			return mapWorkingScheduleWriteError("insert weekly working intervals", err)
		}
	}

	if len(input.Exceptions) == 0 {
		return nil
	}
	exceptionIDs := make([]uuid.UUID, 0, len(input.Exceptions))
	exceptionDates := make([]time.Time, 0, len(input.Exceptions))
	exceptionKinds := make([]string, 0, len(input.Exceptions))
	intervalExceptionIDs := make([]uuid.UUID, 0)
	intervalStarts := make([]int16, 0)
	intervalEnds := make([]int16, 0)
	for _, exception := range input.Exceptions {
		exceptionID := uuid.New()
		exceptionDate, err := time.Parse("2006-01-02", exception.Date)
		if err != nil {
			return ErrInvalidInput
		}
		exceptionIDs = append(exceptionIDs, exceptionID)
		exceptionDates = append(exceptionDates, exceptionDate)
		exceptionKinds = append(exceptionKinds, exception.Kind)
		for _, interval := range exception.Intervals {
			startMinute, startOK := canonicalCivilMinute(interval.StartsAt)
			endMinute, endOK := canonicalCivilMinute(interval.EndsAt)
			if !startOK || !endOK {
				return ErrInvalidInput
			}
			intervalExceptionIDs = append(intervalExceptionIDs, exceptionID)
			intervalStarts = append(intervalStarts, int16(startMinute))
			intervalEnds = append(intervalEnds, int16(endMinute))
		}
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_working_schedule_exceptions (
    id, tenant_id, schedule_id, exception_date, exception_type, created_by
)
SELECT input.id, $1, $2, input.exception_date, input.exception_type, $3
FROM unnest($4::uuid[], $5::date[], $6::text[])
    AS input(id, exception_date, exception_type)`,
		tenantID,
		scheduleID,
		actorID,
		exceptionIDs,
		exceptionDates,
		exceptionKinds,
	); err != nil {
		return mapWorkingScheduleWriteError("insert working schedule exceptions", err)
	}
	if len(intervalExceptionIDs) > 0 {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.calendar_working_schedule_exception_intervals (
    tenant_id, schedule_id, exception_id, start_minute, end_minute
)
SELECT $1, $2, input.exception_id, input.start_minute, input.end_minute
FROM unnest($3::uuid[], $4::smallint[], $5::smallint[])
    AS input(exception_id, start_minute, end_minute)`,
			tenantID,
			scheduleID,
			intervalExceptionIDs,
			intervalStarts,
			intervalEnds,
		); err != nil {
			return mapWorkingScheduleWriteError(
				"insert special-hours working intervals", err,
			)
		}
	}
	return nil
}

func workingScheduleFromInput(
	input PutWorkingScheduleInput,
	version int64,
	updatedAt time.Time,
) WorkingSchedule {
	weekly := append([]WeeklyWorkingInterval(nil), input.WeeklyIntervals...)
	if weekly == nil {
		weekly = []WeeklyWorkingInterval{}
	}
	exceptions := make([]WorkingScheduleException, len(input.Exceptions))
	for index, exception := range input.Exceptions {
		exceptions[index] = exception
		exceptions[index].Intervals = append(
			[]CivilTimeInterval(nil), exception.Intervals...,
		)
		if exceptions[index].Intervals == nil {
			exceptions[index].Intervals = []CivilTimeInterval{}
		}
	}
	if exceptions == nil {
		exceptions = []WorkingScheduleException{}
	}
	return WorkingSchedule{
		Timezone: input.Timezone, WeeklyIntervals: weekly, Exceptions: exceptions,
		Source: "user_override", Version: version, UpdatedAt: updatedAt.UTC(),
	}
}

func calendarWeekdayName(value int) (string, bool) {
	switch value {
	case 1:
		return "monday", true
	case 2:
		return "tuesday", true
	case 3:
		return "wednesday", true
	case 4:
		return "thursday", true
	case 5:
		return "friday", true
	case 6:
		return "saturday", true
	case 7:
		return "sunday", true
	default:
		return "", false
	}
}

func canonicalCivilMinute(value string) (int, bool) {
	canonical, minute, err := parseCivilMinute(value)
	return minute, err == nil && canonical == value
}

func formatCivilMinute(value int) string {
	return fmt.Sprintf("%02d:%02d", value/60, value%60)
}

func mapWorkingScheduleWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23505", "23514", "22001", "22003", "22P02":
			return fmt.Errorf("%s: %w", operation, ErrInvalidInput)
		}
	}
	return schedulingStorageError(operation, err)
}

func schedulingStorageError(operation string, err error) error {
	return fmt.Errorf("%s: %w: %v", operation, ErrSchedulingUnavailable, err)
}

var _ SchedulingRepository = (*PostgresSchedulingRepository)(nil)
