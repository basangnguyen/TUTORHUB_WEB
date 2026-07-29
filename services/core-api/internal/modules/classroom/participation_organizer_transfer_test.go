package classroom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type organizerTransferRepositoryStub struct {
	*participationServiceRepositoryStub
	result        OrganizerTransferResult
	err           error
	calls         int
	tenantContext tenancy.Context
	classID       uuid.UUID
	source        ParticipationSourceRef
	params        OrganizerTransferParams
	transferredAt time.Time
}

func (stub *organizerTransferRepositoryStub) TransferParticipationOrganizer(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	params OrganizerTransferParams,
	transferredAt time.Time,
) (OrganizerTransferResult, error) {
	stub.calls++
	stub.tenantContext = tenantContext
	stub.classID = classID
	stub.source = source
	stub.params = params
	stub.transferredAt = transferredAt
	return stub.result, stub.err
}

func TestTransferOrganizerInputIsSourceBoundAndDeterministic(t *testing.T) {
	t.Parallel()

	organizerID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	input := TransferOrganizerInput{
		NewOrganizerUserID:    organizerID,
		ExpectedSourceVersion: 7,
		IdempotencyKey:        "organizer-transfer-0001",
	}
	session := SessionParticipationSource(
		uuid.MustParse("20000000-0000-4000-8000-000000000002"),
	)
	first, err := input.normalized(session)
	if err != nil {
		t.Fatalf("normalize organizer transfer: %v", err)
	}
	second, err := input.normalized(session)
	if err != nil {
		t.Fatalf("normalize organizer transfer again: %v", err)
	}
	if first != second || first.Fingerprint == "" {
		t.Fatalf("organizer transfer fingerprint is not deterministic: %+v %+v", first, second)
	}
	other, err := input.normalized(
		SessionParticipationSource(uuid.MustParse("30000000-0000-4000-8000-000000000003")),
	)
	if err != nil {
		t.Fatalf("normalize other source: %v", err)
	}
	if other.Fingerprint == first.Fingerprint {
		t.Fatal("organizer transfer fingerprint must be source-bound")
	}
}

func TestTransferOrganizerInputRejectsOccurrenceAndMissingCAS(t *testing.T) {
	t.Parallel()

	seriesID := uuid.New()
	tests := []TransferOrganizerInput{
		{ExpectedSourceVersion: 1, IdempotencyKey: "organizer-transfer-0001"},
		{NewOrganizerUserID: uuid.New(), IdempotencyKey: "organizer-transfer-0001"},
	}
	for _, input := range tests {
		if _, err := input.normalized(SeriesParticipationSource(seriesID)); !errors.Is(err, ErrInvalidParticipationInput) {
			t.Fatalf("error = %v, want invalid participation input", err)
		}
	}
	valid := TransferOrganizerInput{
		NewOrganizerUserID: uuid.New(), ExpectedSourceVersion: 1,
		IdempotencyKey: "organizer-transfer-0001",
	}
	if _, err := valid.normalized(OccurrenceParticipationSource(seriesID, "20260729T010000Z")); !errors.Is(err, ErrInvalidParticipationInput) {
		t.Fatalf("occurrence error = %v, want invalid participation input", err)
	}
}

func TestTransferredOrganizerAudienceAddsEligibleOrganizerOutsideAudience(t *testing.T) {
	t.Parallel()

	previousOrganizerID := uuid.New()
	newOrganizerID := uuid.New()
	desired := transferredOrganizerAudience(
		[]persistedSessionAttendee{{
			UserID:            previousOrganizerID,
			ParticipationRole: ParticipationRoleRequired,
			BusinessRole:      "organizer",
			AudienceSource:    "manual",
		}},
		previousOrganizerID,
		"teacher",
		"roster",
		true,
		newOrganizerID,
	)

	if len(desired) != 2 {
		t.Fatalf("desired attendee count = %d, want 2", len(desired))
	}
	if desired[0].BusinessRole != "teacher" || desired[0].AudienceSource != "roster" {
		t.Fatalf("previous organizer was not restored: %+v", desired[0])
	}
	if desired[1].UserID != newOrganizerID ||
		desired[1].ParticipationRole != ParticipationRoleRequired ||
		desired[1].BusinessRole != "organizer" ||
		desired[1].AudienceSource != "manual" {
		t.Fatalf("new organizer attendee = %+v", desired[1])
	}
}

func TestTransferredOrganizerAudienceDoesNotDuplicateExistingAttendee(t *testing.T) {
	t.Parallel()

	previousOrganizerID := uuid.New()
	newOrganizerID := uuid.New()
	desired := transferredOrganizerAudience(
		[]persistedSessionAttendee{
			{
				UserID:            previousOrganizerID,
				ParticipationRole: ParticipationRoleRequired,
				BusinessRole:      "organizer",
				AudienceSource:    "manual",
			},
			{
				UserID:            newOrganizerID,
				ParticipationRole: ParticipationRoleOptional,
				BusinessRole:      "co_teacher",
				AudienceSource:    "roster",
			},
		},
		previousOrganizerID,
		"teacher",
		"manual",
		true,
		newOrganizerID,
	)

	if len(desired) != 2 {
		t.Fatalf("desired attendee count = %d, want 2", len(desired))
	}
	if desired[1].ParticipationRole != ParticipationRoleOptional ||
		desired[1].BusinessRole != "organizer" ||
		desired[1].AudienceSource != "manual" {
		t.Fatalf("existing new organizer was not promoted in place: %+v", desired[1])
	}
}

func TestTransferredOrganizerAudienceDropsPreviousOrganizerAtCap(t *testing.T) {
	t.Parallel()

	previousOrganizerID := uuid.New()
	newOrganizerID := uuid.New()
	current := make([]persistedSessionAttendee, 0, maximumAudienceAttendees)
	current = append(current, persistedSessionAttendee{
		UserID:            previousOrganizerID,
		ParticipationRole: ParticipationRoleRequired,
		BusinessRole:      "organizer",
		AudienceSource:    "manual",
	})
	for len(current) < maximumAudienceAttendees {
		current = append(current, persistedSessionAttendee{
			UserID:            uuid.New(),
			ParticipationRole: ParticipationRoleRequired,
			BusinessRole:      "student",
			AudienceSource:    "roster",
		})
	}

	desired := transferredOrganizerAudience(
		current,
		previousOrganizerID,
		"",
		"",
		true,
		newOrganizerID,
	)

	if len(desired) != maximumAudienceAttendees {
		t.Fatalf("desired attendee count = %d, want %d", len(desired), maximumAudienceAttendees)
	}
	for _, member := range desired {
		if member.UserID == previousOrganizerID {
			t.Fatal("previous organizer should have been removed at cap")
		}
	}
	if !containsOrganizerUserID(desired, newOrganizerID) {
		t.Fatal("new organizer not added at cap")
	}
}

func TestTransferredOrganizerAudienceAllowsTransferWhenPreviousOrganizerUnavailable(t *testing.T) {
	t.Parallel()

	newOrganizerID := uuid.New()
	desired := transferredOrganizerAudience(
		[]persistedSessionAttendee{{
			UserID:            uuid.New(),
			ParticipationRole: ParticipationRoleRequired,
			BusinessRole:      "teacher",
			AudienceSource:    "manual",
		}},
		uuid.New(),
		"",
		"",
		false,
		newOrganizerID,
	)

	if len(desired) != 2 {
		t.Fatalf("desired attendee count = %d, want 2", len(desired))
	}
	if !containsOrganizerUserID(desired, newOrganizerID) {
		t.Fatal("new organizer not inserted when previous organizer unavailable")
	}
}

func TestSessionParticipationServiceTransferOrganizerAuthorizesAndBindsSource(t *testing.T) {
	t.Parallel()

	tenantID, actorID, classID := uuid.New(), uuid.New(), uuid.New()
	seriesID, organizerID := uuid.New(), uuid.New()
	fixedClock := time.Date(2026, 7, 29, 15, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	repository := &organizerTransferRepositoryStub{
		participationServiceRepositoryStub: &participationServiceRepositoryStub{},
		result: OrganizerTransferResult{
			OrganizerUserID: organizerID,
			SourceVersion:   9,
			Audience: SessionAudience{
				Source: SeriesParticipationSource(seriesID),
			},
		},
	}
	authorizer := &participationServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: tenantID, OwnerUserID: actorID,
		ViewerAccess: ViewerAccess{CanScheduleSessions: true},
	}}
	service, err := NewSessionParticipationService(
		repository,
		authorizer,
		SessionParticipationServiceConfig{Clock: func() time.Time { return fixedClock }},
	)
	if err != nil {
		t.Fatalf("new participation service: %v", err)
	}

	result, err := service.TransferOrganizer(
		context.Background(),
		participationServiceAccess(tenantID, actorID),
		classID,
		SeriesParticipationSource(seriesID),
		TransferOrganizerInput{
			NewOrganizerUserID: organizerID, ExpectedSourceVersion: 8,
			IdempotencyKey: "organizer-transfer-0001",
		},
	)
	if err != nil {
		t.Fatalf("transfer organizer: %v", err)
	}
	if repository.calls != 1 || repository.tenantContext.TenantID != tenantID ||
		repository.tenantContext.ActorID != actorID || repository.classID != classID ||
		repository.source != SeriesParticipationSource(seriesID) ||
		repository.params.NewOrganizerUserID != organizerID ||
		repository.params.ExpectedSourceVersion != 8 ||
		repository.params.Fingerprint == "" ||
		!repository.transferredAt.Equal(fixedClock.UTC()) {
		t.Fatalf("unexpected organizer repository call: %+v", repository)
	}
	if len(authorizer.calls) != 1 || authorizer.calls[0].ClassID != classID ||
		authorizer.calls[0].Action != policy.ActionSessionSchedule {
		t.Fatalf("unexpected organizer authorization: %+v", authorizer.calls)
	}
	if result.OrganizerUserID != organizerID || result.SourceVersion != 9 ||
		!result.Audience.ViewerAccess.CanManageAttendees {
		t.Fatalf("unexpected organizer result projection: %+v", result)
	}
}

func TestSessionParticipationServiceTransferOrganizerDeniesNonOwnerTeacher(t *testing.T) {
	t.Parallel()

	tenantID, actorID, classID := uuid.New(), uuid.New(), uuid.New()
	repository := &organizerTransferRepositoryStub{
		participationServiceRepositoryStub: &participationServiceRepositoryStub{},
	}
	authorizer := &participationServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: tenantID, OwnerUserID: uuid.New(),
		ViewerAccess: ViewerAccess{CanScheduleSessions: true},
	}}
	service, err := NewSessionParticipationService(repository, authorizer)
	if err != nil {
		t.Fatalf("new participation service: %v", err)
	}
	_, err = service.TransferOrganizer(
		context.Background(),
		participationServiceAccess(tenantID, actorID),
		classID,
		SeriesParticipationSource(uuid.New()),
		TransferOrganizerInput{
			NewOrganizerUserID: uuid.New(), ExpectedSourceVersion: 1,
			IdempotencyKey: "organizer-transfer-0002",
		},
	)
	if !errors.Is(err, ErrSessionParticipationAccessDenied) {
		t.Fatalf("transfer error = %v, want access denied", err)
	}
	if repository.calls != 0 {
		t.Fatalf("non-owner teacher reached repository: %+v", repository)
	}
}

func containsOrganizerUserID(desired []resolvedAudienceMember, userID uuid.UUID) bool {
	for _, member := range desired {
		if member.UserID == userID {
			return true
		}
	}
	return false
}

func TestSessionParticipationServiceTransferOrganizerDeniesBeforeRepository(t *testing.T) {
	t.Parallel()

	tenantID, actorID, classID := uuid.New(), uuid.New(), uuid.New()
	repository := &organizerTransferRepositoryStub{
		participationServiceRepositoryStub: &participationServiceRepositoryStub{},
	}
	authorizer := &participationServiceClassAuthorizerStub{err: ErrClassAccessDenied}
	service, err := NewSessionParticipationService(repository, authorizer)
	if err != nil {
		t.Fatalf("new participation service: %v", err)
	}
	_, err = service.TransferOrganizer(
		context.Background(),
		participationServiceAccess(tenantID, actorID),
		classID,
		SessionParticipationSource(uuid.New()),
		TransferOrganizerInput{
			NewOrganizerUserID: uuid.New(), ExpectedSourceVersion: 1,
			IdempotencyKey: "organizer-transfer-0001",
		},
	)
	if !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("transfer error = %v, want access denied", err)
	}
	if repository.calls != 0 {
		t.Fatalf("denied transfer reached repository: %+v", repository)
	}
}
