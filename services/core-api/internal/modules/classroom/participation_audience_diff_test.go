package classroom

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestPlanAudienceDiffClassifiesAndOrdersEveryLifecycle(t *testing.T) {
	t.Parallel()

	addedID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	roleChangedID := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	metadataID := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	removedID := uuid.MustParse("40000000-0000-4000-8000-000000000004")
	responseID := uuid.MustParse("50000000-0000-4000-8000-000000000005")
	unchangedID := uuid.MustParse("60000000-0000-4000-8000-000000000006")

	current := []audienceDiffMember{
		{UserID: removedID, ParticipationRole: ParticipationRoleRequired, BusinessRole: "student", AudienceSource: "manual", ResponseRequested: true},
		{UserID: unchangedID, ParticipationRole: ParticipationRoleOptional, BusinessRole: "student", AudienceSource: "roster", ResponseRequested: true},
		{UserID: metadataID, ParticipationRole: ParticipationRoleRequired, BusinessRole: "student", AudienceSource: "manual", ResponseRequested: true},
		{UserID: roleChangedID, ParticipationRole: ParticipationRoleOptional, BusinessRole: "student", AudienceSource: "manual", ResponseRequested: true},
		{UserID: responseID, ParticipationRole: ParticipationRoleRequired, BusinessRole: "student", AudienceSource: "manual", ResponseRequested: false},
	}
	desired := []audienceDiffMember{
		{UserID: responseID, ParticipationRole: ParticipationRoleRequired, BusinessRole: "student", AudienceSource: "manual", ResponseRequested: true},
		{UserID: roleChangedID, ParticipationRole: ParticipationRoleRequired, BusinessRole: "student", AudienceSource: "manual", ResponseRequested: true},
		{UserID: addedID, ParticipationRole: ParticipationRoleOptional, BusinessRole: "student", AudienceSource: "manual", ResponseRequested: true},
		{UserID: metadataID, ParticipationRole: ParticipationRoleRequired, BusinessRole: "teacher", AudienceSource: "roster", ResponseRequested: true},
		{UserID: unchangedID, ParticipationRole: ParticipationRoleOptional, BusinessRole: "student", AudienceSource: "roster", ResponseRequested: true},
	}

	diff, err := planAudienceDiff(current, desired)
	if err != nil {
		t.Fatalf("plan audience diff: %v", err)
	}
	if len(diff) != 6 {
		t.Fatalf("diff count = %d, want 6", len(diff))
	}
	assertAudienceDiffEntry(t, diff[0], addedID, AudienceDiffAdded, AudienceRSVPReset, true)
	assertAudienceDiffEntry(t, diff[1], roleChangedID, AudienceDiffRoleChange, AudienceRSVPReset, true)
	assertAudienceDiffEntry(t, diff[2], metadataID, AudienceDiffUnchanged, AudienceRSVPRetain, true)
	assertAudienceDiffEntry(t, diff[3], removedID, AudienceDiffRemoved, AudienceRSVPClose, true)
	assertAudienceDiffEntry(t, diff[4], responseID, AudienceDiffUnchanged, AudienceRSVPReset, true)
	assertAudienceDiffEntry(t, diff[5], unchangedID, AudienceDiffUnchanged, AudienceRSVPRetain, false)
}

func TestPlanAudienceDiffRejectsMissingAndDuplicateStableIdentity(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tests := []struct {
		name    string
		current []audienceDiffMember
		desired []audienceDiffMember
	}{
		{name: "missing", desired: []audienceDiffMember{{}}},
		{name: "duplicate current", current: []audienceDiffMember{{UserID: userID}, {UserID: userID}}},
		{name: "duplicate desired", desired: []audienceDiffMember{{UserID: userID}, {UserID: userID}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := planAudienceDiff(test.current, test.desired); !errors.Is(err, ErrInvalidParticipationInput) {
				t.Fatalf("error = %v, want invalid participation input", err)
			}
		})
	}
}

func assertAudienceDiffEntry(
	t *testing.T,
	entry audienceDiffEntry,
	userID uuid.UUID,
	kind AudienceDiffKind,
	disposition AudienceRSVPDisposition,
	metadataChanged bool,
) {
	t.Helper()
	if entry.UserID != userID || entry.Kind != kind ||
		entry.RSVPDisposition != disposition ||
		entry.MetadataChanged != metadataChanged {
		t.Fatalf("unexpected diff entry: %+v", entry)
	}
}
