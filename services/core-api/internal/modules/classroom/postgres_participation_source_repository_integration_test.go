//go:build integration

package classroom

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresOccurrenceParticipationCopyOnWriteRSVP(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 21 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create occurrence participation integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin occurrence participation fixture: %v", err)
	}
	defer func() { _ = setup.Rollback(context.Background()) }()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "occurrence-participation")
	studentID := seedTenantMember(
		t,
		ctx,
		setup,
		tenantID,
		"occurrence-participation-student",
		"student",
	)
	classID := uuid.New()
	if _, err := setup.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Occurrence participation integration class',
        'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"OP"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert occurrence participation class: %v", err)
	}
	if _, err := setup.Exec(ctx, `
INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
)
VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		tenantID,
		classID,
		studentID,
		ownerID,
	); err != nil {
		t.Fatalf("insert occurrence participation enrollment: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit occurrence participation fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID, studentID)

	protector, err := protecteddata.New(protecteddata.Config{
		Key:        bytes.Repeat([]byte{0x2c}, 32),
		KeyVersion: 8,
	})
	if err != nil {
		t.Fatalf("create occurrence participation test protector: %v", err)
	}
	repository := NewPostgresRepository(
		pool,
		30*time.Second,
		policy.NewEngine(),
	).WithCalendarProtectedData(protector)
	ownerContext := mustTenantContext(t, tenantID, ownerID)
	studentContext := mustTenantContext(t, tenantID, studentID)

	location := mustLocation("Asia/Ho_Chi_Minh")
	startsAt := time.Date(2026, 8, 3, 2, 0, 0, 0, time.UTC)
	createParams, _, err := normalizeCreateSeriesInput(
		ctx,
		CreateSeriesInput{
			Title:       "Occurrence audience copy-on-write",
			Description: "Series RSVP isolation fixture",
			StartsAt:    startsAt.In(location).Format(time.RFC3339),
			EndsAt:      startsAt.Add(time.Hour).In(location).Format(time.RFC3339),
			Timezone:    "Asia/Ho_Chi_Minh",
			Rule: recurrence.Rule{
				Frequency: recurrence.FrequencyDaily,
				Interval:  1,
				End: recurrence.End{
					Type:  recurrence.EndAfterCount,
					Count: 2,
				},
			},
			OverlapPolicy: recurrence.OverlapReject,
		},
		ownerID,
	)
	if err != nil {
		t.Fatalf("normalize occurrence participation series: %v", err)
	}
	series, err := repository.CreateSeries(
		ctx,
		ownerContext,
		classID,
		createParams,
		startsAt.Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("create occurrence participation series: %v", err)
	}
	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(series))
	if err != nil || len(occurrences) != 2 {
		t.Fatalf("expand occurrence participation series: count=%d err=%v", len(occurrences), err)
	}
	seriesSource := SeriesParticipationSource(series.ID)
	firstOccurrenceSource := OccurrenceParticipationSource(
		series.ID,
		occurrences[0].Key,
	)
	secondOccurrenceSource := OccurrenceParticipationSource(
		series.ID,
		occurrences[1].Key,
	)

	replaceParams, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: 0,
		IdempotencyKey:           "occurrence-series-audience-0001",
		ResponseRequested:        true,
		Attendees: []InternalAudienceAttendeeInput{
			{UserID: ownerID, ParticipationRole: ParticipationRoleRequired},
			{UserID: studentID, ParticipationRole: ParticipationRoleRequired},
		},
	}).normalized()
	if err != nil {
		t.Fatalf("normalize series participation audience: %v", err)
	}
	replaceParams, err = bindAudienceReplacementToSource(seriesSource, replaceParams)
	if err != nil {
		t.Fatalf("bind series participation audience: %v", err)
	}
	replacedAt := startsAt.Add(-2 * time.Hour)
	replaced, err := repository.ReplaceParticipationAudience(
		ctx,
		ownerContext,
		classID,
		seriesSource,
		replaceParams,
		replacedAt,
	)
	if err != nil {
		t.Fatalf("replace series participation audience: %v", err)
	}
	if replaced.Replayed || replaced.Audience.AudienceRevision != 1 ||
		!replaced.Audience.ResponseRequested || len(replaced.Audience.Attendees) != 2 {
		t.Fatalf("unexpected series participation audience: %+v", replaced)
	}
	seriesStudent := findIntegrationAudienceAttendee(t, replaced.Audience, studentID)
	if seriesStudent.Version != 1 || seriesStudent.RSVPState != RSVPStateNeedsAction {
		t.Fatalf("unexpected series student attendee: %+v", seriesStudent)
	}

	firstInherited, err := repository.GetParticipationAudience(
		ctx,
		studentContext,
		classID,
		firstOccurrenceSource,
	)
	if err != nil {
		t.Fatalf("read first inherited occurrence audience: %v", err)
	}
	assertInheritedOccurrenceAudience(
		t,
		firstInherited,
		firstOccurrenceSource,
		studentID,
	)
	secondInherited, err := repository.GetParticipationAudience(
		ctx,
		studentContext,
		classID,
		secondOccurrenceSource,
	)
	if err != nil {
		t.Fatalf("read second inherited occurrence audience: %v", err)
	}
	assertInheritedOccurrenceAudience(
		t,
		secondInherited,
		secondOccurrenceSource,
		studentID,
	)

	rsvpParams, err := (SelfRSVPInput{
		State:                   RSVPStateAccepted,
		Note:                    "Attending this occurrence",
		ExpectedAttendeeVersion: findIntegrationAudienceAttendee(t, firstInherited, studentID).Version,
		IdempotencyKey:          "occurrence-first-rsvp-0001",
	}).normalized()
	if err != nil {
		t.Fatalf("normalize occurrence RSVP: %v", err)
	}
	rsvpParams, err = bindSelfRSVPToSource(firstOccurrenceSource, rsvpParams)
	if err != nil {
		t.Fatalf("bind occurrence RSVP: %v", err)
	}
	rsvp, err := repository.RespondToParticipationSource(
		ctx,
		studentContext,
		classID,
		firstOccurrenceSource,
		rsvpParams,
		replacedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("respond to first occurrence: %v", err)
	}
	if rsvp.Replayed || rsvp.Attendee.RSVPState != RSVPStateAccepted ||
		rsvp.Attendee.Version != seriesStudent.Version+1 {
		t.Fatalf("unexpected occurrence RSVP result: %+v", rsvp)
	}

	firstOverridden, err := repository.GetParticipationAudience(
		ctx,
		studentContext,
		classID,
		firstOccurrenceSource,
	)
	if err != nil {
		t.Fatalf("read materialized first occurrence audience: %v", err)
	}
	firstStudent := findIntegrationAudienceAttendee(t, firstOverridden, studentID)
	if firstOverridden.Source != firstOccurrenceSource ||
		firstOverridden.AudienceRevision != 1 ||
		len(firstOverridden.Attendees) != 2 ||
		firstStudent.RSVPState != RSVPStateAccepted ||
		firstStudent.Version != seriesStudent.Version+1 ||
		firstStudent.ID != rsvp.Attendee.ID {
		t.Fatalf("unexpected materialized first occurrence audience: %+v", firstOverridden)
	}

	seriesAfterRSVP, err := repository.GetParticipationAudience(
		ctx,
		studentContext,
		classID,
		seriesSource,
	)
	if err != nil {
		t.Fatalf("read series audience after occurrence RSVP: %v", err)
	}
	seriesStudentAfter := findIntegrationAudienceAttendee(t, seriesAfterRSVP, studentID)
	if seriesAfterRSVP.AudienceRevision != 1 ||
		seriesStudentAfter.ID != seriesStudent.ID ||
		seriesStudentAfter.RSVPState != RSVPStateNeedsAction ||
		seriesStudentAfter.Version != seriesStudent.Version {
		t.Fatalf("occurrence RSVP changed series audience: %+v", seriesAfterRSVP)
	}

	secondAfterRSVP, err := repository.GetParticipationAudience(
		ctx,
		studentContext,
		classID,
		secondOccurrenceSource,
	)
	if err != nil {
		t.Fatalf("read second occurrence after first occurrence RSVP: %v", err)
	}
	secondStudentAfter := findIntegrationAudienceAttendee(t, secondAfterRSVP, studentID)
	if secondAfterRSVP.Source != secondOccurrenceSource ||
		secondAfterRSVP.AudienceRevision != 0 ||
		secondStudentAfter.ID != seriesStudent.ID ||
		secondStudentAfter.RSVPState != RSVPStateNeedsAction ||
		secondStudentAfter.Version != seriesStudent.Version {
		t.Fatalf("first occurrence RSVP changed another occurrence: %+v", secondAfterRSVP)
	}

	occurrenceReplaceParams, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: 0,
		IdempotencyKey:           "occurrence-second-audience-0001",
		ResponseRequested:        false,
		Attendees: []InternalAudienceAttendeeInput{
			{UserID: ownerID, ParticipationRole: ParticipationRoleRequired},
		},
	}).normalized()
	if err != nil {
		t.Fatalf("normalize second occurrence audience: %v", err)
	}
	occurrenceReplaceParams, err = bindAudienceReplacementToSource(
		secondOccurrenceSource,
		occurrenceReplaceParams,
	)
	if err != nil {
		t.Fatalf("bind second occurrence audience: %v", err)
	}
	secondReplaced, err := repository.ReplaceParticipationAudience(
		ctx,
		ownerContext,
		classID,
		secondOccurrenceSource,
		occurrenceReplaceParams,
		replacedAt.Add(2*time.Minute),
	)
	if err != nil {
		t.Fatalf("replace second occurrence audience: %v", err)
	}
	if secondReplaced.Replayed ||
		secondReplaced.Audience.Source != secondOccurrenceSource ||
		secondReplaced.Audience.AudienceRevision != 1 ||
		secondReplaced.Audience.ResponseRequested ||
		len(secondReplaced.Audience.Attendees) != 1 ||
		secondReplaced.Audience.Attendees[0].UserID != ownerID {
		t.Fatalf("unexpected replaced second occurrence audience: %+v", secondReplaced)
	}
}

func assertInheritedOccurrenceAudience(
	t *testing.T,
	audience SessionAudience,
	source ParticipationSourceRef,
	studentID uuid.UUID,
) {
	t.Helper()
	student := findIntegrationAudienceAttendee(t, audience, studentID)
	if audience.Source != source || audience.AudienceRevision != 0 ||
		!audience.ResponseRequested || len(audience.Attendees) != 2 ||
		student.RSVPState != RSVPStateNeedsAction || student.Version != 1 {
		t.Fatalf("unexpected inherited occurrence audience: %+v", audience)
	}
}
