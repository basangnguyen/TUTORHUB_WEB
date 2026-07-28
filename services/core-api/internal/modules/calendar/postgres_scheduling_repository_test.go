package calendar

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestNewPostgresSchedulingRepositoryRequiresFailClosedDependencies(t *testing.T) {
	t.Parallel()
	database := &schedulingConstructorDatabase{}
	controls := &schedulingConstructorControls{}
	authorizer := policy.NewEngine()

	for name, dependencies := range map[string]struct {
		database   transactionDatabase
		authorizer policy.Authorizer
		controls   featurecontrol.Enforcer
	}{
		"database":   {authorizer: authorizer, controls: controls},
		"authorizer": {database: database, controls: controls},
		"controls":   {database: database, authorizer: authorizer},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewPostgresSchedulingRepository(
				dependencies.database,
				time.Second,
				dependencies.authorizer,
				dependencies.controls,
			); err == nil {
				t.Fatal("constructor accepted a fail-open dependency set")
			}
		})
	}
	repository, err := NewPostgresSchedulingRepository(
		database, 0, authorizer, controls,
	)
	if err != nil {
		t.Fatalf("construct scheduling repository: %v", err)
	}
	if repository.queryTimeout != defaultQueryTimeout {
		t.Fatalf("default query timeout = %s", repository.queryTimeout)
	}
}

func TestExpandPersistedWorkingScheduleAppliesReplacementExceptions(t *testing.T) {
	t.Parallel()
	schedule := newPersistedWorkingSchedule(uuid.New(), "UTC")
	for weekday := 1; weekday <= 4; weekday++ {
		schedule.weekly[weekday] = []persistedCivilInterval{{
			startMinute: 9 * 60, endMinute: 17 * 60,
		}}
	}
	schedule.exceptions["2026-07-28"] = persistedScheduleException{kind: "holiday"}
	schedule.exceptions["2026-07-29"] = persistedScheduleException{
		kind: "special_hours",
		intervals: []persistedCivilInterval{{
			startMinute: 10 * 60, endMinute: 12 * 60,
		}},
	}
	schedule.exceptions["2026-07-30"] = persistedScheduleException{kind: "out_of_office"}
	params := availabilityParams{
		From: time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
	}

	working, outOfOffice, err := expandPersistedWorkingSchedule(schedule, params)
	if err != nil {
		t.Fatalf("expand working schedule: %v", err)
	}
	if len(working) != 2 {
		t.Fatalf("working intervals = %+v", working)
	}
	if got, want := working[0].StartsAt, time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("first working start = %s, want %s", got, want)
	}
	if got, want := working[1].StartsAt, time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("special-hours start = %s, want %s", got, want)
	}
	if len(outOfOffice) != 1 || outOfOffice[0].Status != "out_of_office" ||
		!outOfOffice[0].StartsAt.Equal(time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)) ||
		!outOfOffice[0].EndsAt.Equal(time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("out-of-office projection = %+v", outOfOffice)
	}
}

func TestExpandPersistedWorkingScheduleHandlesDSTGapAndOverlap(t *testing.T) {
	t.Parallel()
	overlapSchedule := newPersistedWorkingSchedule(uuid.New(), "America/New_York")
	overlapSchedule.weekly[7] = []persistedCivilInterval{{
		startMinute: 90, endMinute: 150,
	}}
	overlapWorking, _, err := expandPersistedWorkingSchedule(
		overlapSchedule,
		availabilityParams{
			From: time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("expand overlap schedule: %v", err)
	}
	if len(overlapWorking) != 2 ||
		overlapWorking[0].EndsAt.Sub(overlapWorking[0].StartsAt) != 30*time.Minute ||
		overlapWorking[1].EndsAt.Sub(overlapWorking[1].StartsAt) != time.Hour {
		t.Fatalf("fall-back working intervals = %+v", overlapWorking)
	}
	if !overlapWorking[0].StartsAt.Equal(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)) ||
		!overlapWorking[1].StartsAt.Equal(time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)) {
		t.Fatalf("fall-back boundaries = %+v", overlapWorking)
	}

	gapSchedule := newPersistedWorkingSchedule(uuid.New(), "America/New_York")
	gapSchedule.weekly[7] = []persistedCivilInterval{{
		startMinute: 150, endMinute: 210,
	}}
	gapWorking, _, err := expandPersistedWorkingSchedule(
		gapSchedule,
		availabilityParams{
			From: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		t.Fatalf("expand gap schedule: %v", err)
	}
	if len(gapWorking) != 1 ||
		gapWorking[0].EndsAt.Sub(gapWorking[0].StartsAt) != 30*time.Minute ||
		!gapWorking[0].StartsAt.Equal(time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("spring-forward working interval = %+v", gapWorking)
	}
}

func TestAvailabilitySeriesOccurrenceAssignmentOverridesBase(t *testing.T) {
	t.Parallel()
	participantID := uuid.New()
	series := availabilitySeriesProjection{
		organizerID: uuid.New(), showAs: "busy",
		base: map[uuid.UUID]availabilitySeriesAssignment{
			participantID: {status: "active", showAs: "tentative"},
		},
		occurrences: map[string]map[uuid.UUID]availabilitySeriesAssignment{
			"removed-occurrence": {
				participantID: {status: "removed", showAs: "tentative"},
			},
			"free-occurrence": {
				participantID: {status: "active", showAs: "free"},
			},
		},
	}
	if present, showAs := series.participantAvailability(participantID, "ordinary"); !present || showAs != "tentative" {
		t.Fatalf("base assignment = %t/%s", present, showAs)
	}
	if present, _ := series.participantAvailability(participantID, "removed-occurrence"); present {
		t.Fatal("occurrence removal did not override the series assignment")
	}
	if present, showAs := series.participantAvailability(participantID, "free-occurrence"); !present || showAs != "free" {
		t.Fatalf("occurrence show-as override = %t/%s", present, showAs)
	}
}

func TestAvailabilityParticipantValidationIsClassScopedAndConcealed(t *testing.T) {
	t.Parallel()
	tenantID, classID := uuid.New(), uuid.New()
	ownerID, rosterID, externalID := uuid.New(), uuid.New(), uuid.New()
	transaction := &availabilityValidationTransaction{
		rows: []pgx.Rows{
			&availabilityValidationRows{values: []uuid.UUID{ownerID, rosterID}},
			&availabilityValidationRows{values: []uuid.UUID{externalID}},
		},
	}
	if err := validateAvailabilityParticipantsForClass(
		context.Background(), transaction, tenantID, classID,
		[]uuid.UUID{ownerID, rosterID}, []uuid.UUID{externalID},
	); err != nil {
		t.Fatalf("validate class-scoped participants: %v", err)
	}
	if len(transaction.queries) != 2 {
		t.Fatalf("validation query count = %d, want 2", len(transaction.queries))
	}
	for index, args := range transaction.arguments {
		if len(args) < 3 || args[0] != tenantID || args[1] != classID {
			t.Fatalf("query %d scope args = %#v", index, args)
		}
	}
	for _, fragment := range []string{
		"tutorhub.class_enrollments", "class.id = $2", "class.owner_user_id",
	} {
		if !strings.Contains(transaction.queries[0], fragment) {
			t.Fatalf("internal eligibility query missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		"tutorhub.class_session_attendees", "attendee.class_id = $2",
		"tutorhub.calendar_external_recipients",
	} {
		if !strings.Contains(transaction.queries[1], fragment) {
			t.Fatalf("external eligibility query missing %q", fragment)
		}
	}
}

func TestAvailabilityParticipantValidationConcealsIneligibleReference(t *testing.T) {
	t.Parallel()
	transaction := &availabilityValidationTransaction{
		rows: []pgx.Rows{&availabilityValidationRows{values: []uuid.UUID{uuid.New()}}},
	}
	requested := []uuid.UUID{uuid.New(), uuid.New()}
	err := validateAvailabilityParticipantsForClass(
		context.Background(), transaction, uuid.New(), uuid.New(), requested, nil,
	)
	if !errors.Is(err, ErrSchedulingNotFound) {
		t.Fatalf("ineligible participant error = %v, want concealed not found", err)
	}
}

type schedulingConstructorDatabase struct{}

type availabilityValidationTransaction struct {
	pgx.Tx
	rows      []pgx.Rows
	queries   []string
	arguments [][]any
}

func (transaction *availabilityValidationTransaction) Query(
	_ context.Context,
	query string,
	arguments ...any,
) (pgx.Rows, error) {
	transaction.queries = append(transaction.queries, query)
	transaction.arguments = append(transaction.arguments, arguments)
	if len(transaction.rows) == 0 {
		return nil, errors.New("unexpected validation query")
	}
	rows := transaction.rows[0]
	transaction.rows = transaction.rows[1:]
	return rows, nil
}

type availabilityValidationRows struct {
	pgx.Rows
	values []uuid.UUID
	index  int
}

func (rows *availabilityValidationRows) Next() bool {
	if rows.index >= len(rows.values) {
		return false
	}
	rows.index++
	return true
}

func (rows *availabilityValidationRows) Scan(destinations ...any) error {
	if len(destinations) != 1 || rows.index == 0 || rows.index > len(rows.values) {
		return errors.New("invalid validation row scan")
	}
	destination, ok := destinations[0].(*uuid.UUID)
	if !ok {
		return errors.New("invalid validation row destination")
	}
	*destination = rows.values[rows.index-1]
	return nil
}

func (*availabilityValidationRows) Err() error { return nil }

func (*availabilityValidationRows) Close() {}

func (*schedulingConstructorDatabase) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not used")
}

type schedulingConstructorControls struct{}

func (*schedulingConstructorControls) RequireFeature(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.FeatureKey,
) error {
	return nil
}

func (*schedulingConstructorControls) RequireMemberCapacity(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
) error {
	return nil
}

func (*schedulingConstructorControls) RequireActiveClassCapacity(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
) error {
	return nil
}

func (*schedulingConstructorControls) ConsumeInviteCreation(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	time.Time,
) (featurecontrol.RateLimitResult, error) {
	return featurecontrol.RateLimitResult{}, nil
}
