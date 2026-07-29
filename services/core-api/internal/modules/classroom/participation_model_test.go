package classroom

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestReplaceAudienceInputNormalizesDeterministically(t *testing.T) {
	t.Parallel()

	first := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	second := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	input := ReplaceAudienceInput{
		ExpectedAudienceRevision: 0,
		IdempotencyKey:           "  audience-replace-0001  ",
		ResponseRequested:        true,
		Attendees: []InternalAudienceAttendeeInput{
			{UserID: second, ParticipationRole: ParticipationRoleOptional},
			{UserID: first, ParticipationRole: ParticipationRoleRequired},
			{UserID: second, ParticipationRole: ParticipationRoleOptional},
		},
	}

	params, err := input.normalized()
	if err != nil {
		t.Fatalf("normalize audience replacement: %v", err)
	}
	if params.IdempotencyKey != "audience-replace-0001" {
		t.Fatalf("idempotency key = %q", params.IdempotencyKey)
	}
	if len(params.Attendees) != 2 {
		t.Fatalf("attendees = %d, want 2", len(params.Attendees))
	}
	if params.Attendees[0].UserID != first ||
		params.Attendees[0].ParticipationRole != ParticipationRoleRequired ||
		params.Attendees[1].UserID != second ||
		params.Attendees[1].ParticipationRole != ParticipationRoleOptional {
		t.Fatalf("unexpected canonical attendees: %#v", params.Attendees)
	}

	reordered := input
	reordered.Attendees = []InternalAudienceAttendeeInput{
		{UserID: first, ParticipationRole: ParticipationRoleRequired},
		{UserID: second, ParticipationRole: ParticipationRoleOptional},
	}
	reorderedParams, err := reordered.normalized()
	if err != nil {
		t.Fatalf("normalize reordered audience replacement: %v", err)
	}
	if params.Fingerprint != reorderedParams.Fingerprint {
		t.Fatalf("fingerprints differ: %q != %q", params.Fingerprint, reorderedParams.Fingerprint)
	}
}

func TestReplaceAudienceInputRejectsInvalidOrConflictingAudience(t *testing.T) {
	t.Parallel()

	member := uuid.New()
	valid := ReplaceAudienceInput{
		ExpectedAudienceRevision: 1,
		IdempotencyKey:           "audience-replace-0002",
		Attendees: []InternalAudienceAttendeeInput{
			{UserID: member, ParticipationRole: ParticipationRoleRequired},
		},
	}
	tests := []struct {
		name  string
		input ReplaceAudienceInput
	}{
		{
			name: "negative revision",
			input: ReplaceAudienceInput{
				ExpectedAudienceRevision: -1,
				IdempotencyKey:           valid.IdempotencyKey,
			},
		},
		{
			name: "short key",
			input: ReplaceAudienceInput{
				ExpectedAudienceRevision: 0,
				IdempotencyKey:           "too-short",
			},
		},
		{
			name: "postgres text NUL",
			input: ReplaceAudienceInput{
				ExpectedAudienceRevision: 0,
				IdempotencyKey:           "audience\x00replace-0002",
			},
		},
		{
			name: "nil user",
			input: ReplaceAudienceInput{
				ExpectedAudienceRevision: 0,
				IdempotencyKey:           valid.IdempotencyKey,
				Attendees: []InternalAudienceAttendeeInput{{
					ParticipationRole: ParticipationRoleRequired,
				}},
			},
		},
		{
			name: "unsupported role",
			input: ReplaceAudienceInput{
				ExpectedAudienceRevision: 0,
				IdempotencyKey:           valid.IdempotencyKey,
				Attendees: []InternalAudienceAttendeeInput{{
					UserID: member, ParticipationRole: "teacher",
				}},
			},
		},
		{
			name: "conflicting duplicate role",
			input: ReplaceAudienceInput{
				ExpectedAudienceRevision: 0,
				IdempotencyKey:           valid.IdempotencyKey,
				Attendees: []InternalAudienceAttendeeInput{
					{UserID: member, ParticipationRole: ParticipationRoleRequired},
					{UserID: member, ParticipationRole: ParticipationRoleOptional},
				},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.input.normalized(); !errors.Is(err, ErrInvalidParticipationInput) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidParticipationInput)
			}
		})
	}
}

func TestReplaceAudienceInputEnforcesDistinctAudienceCap(t *testing.T) {
	t.Parallel()

	attendees := make([]InternalAudienceAttendeeInput, maximumAudienceAttendees+1)
	for index := range attendees {
		attendees[index] = InternalAudienceAttendeeInput{
			UserID:            uuid.New(),
			ParticipationRole: ParticipationRoleRequired,
		}
	}
	_, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: 0,
		IdempotencyKey:           "audience-replace-0003",
		Attendees:                attendees,
	}).normalized()
	if !errors.Is(err, ErrInvalidParticipationInput) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidParticipationInput)
	}
}

func TestSelfRSVPInputNormalizesAndFingerprintsCanonicalResponse(t *testing.T) {
	t.Parallel()

	params, err := (SelfRSVPInput{
		State:                   RSVPStateTentative,
		Note:                    "  Need to leave early.  ",
		ExpectedAttendeeVersion: 4,
		IdempotencyKey:          "  rsvp-respond-0001  ",
	}).normalized()
	if err != nil {
		t.Fatalf("normalize self RSVP: %v", err)
	}
	if params.Note != "Need to leave early." || params.IdempotencyKey != "rsvp-respond-0001" {
		t.Fatalf("unexpected normalized RSVP params: %#v", params)
	}
	repeat, err := (SelfRSVPInput{
		State:                   RSVPStateTentative,
		Note:                    "Need to leave early.",
		ExpectedAttendeeVersion: 4,
		IdempotencyKey:          "another-rsvp-key-0001",
	}).normalized()
	if err != nil {
		t.Fatalf("normalize equivalent self RSVP: %v", err)
	}
	if params.Fingerprint != repeat.Fingerprint {
		t.Fatalf("fingerprints differ: %q != %q", params.Fingerprint, repeat.Fingerprint)
	}
}

func TestSelfRSVPInputRejectsInvalidStateVersionAndNote(t *testing.T) {
	t.Parallel()

	longNote := make([]rune, maximumRSVPResponseNoteLength+1)
	for index := range longNote {
		longNote[index] = 'a'
	}
	validKey := "rsvp-respond-0002"
	tests := []struct {
		name  string
		input SelfRSVPInput
	}{
		{
			name: "needs action is not a self response",
			input: SelfRSVPInput{
				State: RSVPStateNeedsAction, ExpectedAttendeeVersion: 1, IdempotencyKey: validKey,
			},
		},
		{
			name: "missing version",
			input: SelfRSVPInput{
				State: RSVPStateAccepted, ExpectedAttendeeVersion: 0, IdempotencyKey: validKey,
			},
		},
		{
			name: "note too long",
			input: SelfRSVPInput{
				State: RSVPStateDeclined, Note: string(longNote), ExpectedAttendeeVersion: 1, IdempotencyKey: validKey,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := test.input.normalized(); !errors.Is(err, ErrInvalidParticipationInput) {
				t.Fatalf("error = %v, want %v", err, ErrInvalidParticipationInput)
			}
		})
	}
}
