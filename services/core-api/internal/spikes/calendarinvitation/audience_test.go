package calendarinvitation

import "testing"

func TestDiffAudienceClassifiesChangesAndRSVPPolicy(t *testing.T) {
	t.Parallel()

	previous := []Attendee{
		testAttendee("unchanged", "unchanged@example.edu", RoleRequired, RSVPAccepted),
		testAttendee("removed", "removed@example.edu", RoleRequired, RSVPAccepted),
		testAttendee("role", "role@example.edu", RoleRequired, RSVPTentative),
		testAttendee("address", "old-address@example.edu", RoleRequired, RSVPAccepted),
		testAttendee("policy", "policy@example.edu", RoleRequired, RSVPAccepted),
	}
	policyReset := testAttendee(
		"policy",
		"policy@example.edu",
		RoleRequired,
		RSVPNeedsAction,
	)
	policyReset.ResponseRequested = false
	current := []Attendee{
		testAttendee("added", "added@example.edu", RoleOptional, RSVPNeedsAction),
		testAttendee("unchanged", "unchanged@example.edu", RoleRequired, RSVPAccepted),
		testAttendee("role", "role@example.edu", RoleOptional, RSVPNeedsAction),
		testAttendee("address", "new-address@example.edu", RoleRequired, RSVPNeedsAction),
		policyReset,
	}

	changes, err := DiffAudience(previous, current)
	if err != nil {
		t.Fatalf("DiffAudience(): %v", err)
	}
	want := []struct {
		recipientID string
		kind        AudienceChangeKind
		retainRSVP  bool
		hasPrevious bool
		hasCurrent  bool
	}{
		{
			recipientID: "added",
			kind:        AudienceAdded,
			retainRSVP:  false,
			hasCurrent:  true,
		},
		{
			recipientID: "address",
			kind:        AudienceAddressChanged,
			retainRSVP:  false,
			hasPrevious: true,
			hasCurrent:  true,
		},
		{
			recipientID: "policy",
			kind:        AudiencePolicyChanged,
			retainRSVP:  false,
			hasPrevious: true,
			hasCurrent:  true,
		},
		{
			recipientID: "removed",
			kind:        AudienceRemoved,
			retainRSVP:  false,
			hasPrevious: true,
		},
		{
			recipientID: "role",
			kind:        AudienceRoleChanged,
			retainRSVP:  false,
			hasPrevious: true,
			hasCurrent:  true,
		},
		{
			recipientID: "unchanged",
			kind:        AudienceUnchanged,
			retainRSVP:  true,
			hasPrevious: true,
			hasCurrent:  true,
		},
	}
	if len(changes) != len(want) {
		t.Fatalf("change count = %d, want %d", len(changes), len(want))
	}
	for index, expected := range want {
		got := changes[index]
		if got.RecipientID != expected.recipientID ||
			got.Kind != expected.kind ||
			got.RetainRSVP != expected.retainRSVP ||
			(got.Previous != nil) != expected.hasPrevious ||
			(got.Current != nil) != expected.hasCurrent {
			t.Fatalf("change[%d] = %#v, want %#v", index, got, expected)
		}
	}

	addressChange := changes[1]
	if addressChange.Previous.RSVP != RSVPAccepted ||
		addressChange.Current.RSVP != RSVPNeedsAction ||
		addressChange.Current.RSVPSource != RSVPSourceNone {
		t.Fatalf("address replacement did not carry the explicit reset snapshot: %#v", addressChange)
	}
	policyChange := changes[2]
	if policyChange.Previous.RSVP != RSVPAccepted ||
		policyChange.Current.RSVP != RSVPNeedsAction ||
		policyChange.Current.RSVPSource != RSVPSourceNone ||
		policyChange.Current.ResponseRequested {
		t.Fatalf("response policy change did not reset RSVP: %#v", policyChange)
	}
	roleChange := changes[4]
	if roleChange.Previous.RSVP != RSVPTentative ||
		roleChange.Current.RSVP != RSVPNeedsAction ||
		roleChange.Current.RSVPSource != RSVPSourceNone {
		t.Fatalf("role change did not carry the explicit RSVP reset snapshot: %#v", roleChange)
	}
}

func TestResolveAudienceManualEntriesOverrideRosterByStableIdentity(t *testing.T) {
	t.Parallel()

	rosterStudent := testAttendee(
		"student-01",
		"roster@example.edu",
		RoleRequired,
		RSVPAccepted,
	)
	otherRosterStudent := testAttendee(
		"student-02",
		"second@example.edu",
		RoleRequired,
		RSVPNeedsAction,
	)
	manualOverride := testAttendee(
		"student-01",
		"manual@example.edu",
		RoleOptional,
		RSVPNeedsAction,
	)
	manualOverride.Source = AudienceSourceManual
	manualOverride.External = true
	manualGuest := testAttendee(
		"guest-01",
		"guest@example.net",
		RoleOptional,
		RSVPNeedsAction,
	)
	manualGuest.Source = AudienceSourceManual
	manualGuest.External = true

	resolved, err := ResolveAudience(
		[]Attendee{otherRosterStudent, rosterStudent},
		[]Attendee{manualOverride, manualGuest},
	)
	if err != nil {
		t.Fatalf("ResolveAudience(): %v", err)
	}
	if len(resolved) != 3 {
		t.Fatalf("resolved audience count = %d, want 3", len(resolved))
	}
	if resolved[0].RecipientID != "guest-01" ||
		resolved[1].RecipientID != "student-01" ||
		resolved[2].RecipientID != "student-02" {
		t.Fatalf("resolved audience is not stable-ID sorted: %#v", resolved)
	}
	if got := resolved[1]; got.Source != AudienceSourceManual ||
		got.Email != "manual@example.edu" ||
		got.Role != RoleOptional ||
		!got.External {
		t.Fatalf("manual override was not authoritative: %#v", got)
	}

	invalidManual := manualGuest
	invalidManual.Source = AudienceSourceRoster
	if _, err := ResolveAudience(nil, []Attendee{invalidManual}); err == nil {
		t.Fatal("ResolveAudience() accepted roster source in manual input")
	}
}

func TestDiffAudienceReturnsDefensiveCopiesAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	previous := []Attendee{
		testAttendee("student-01", "student@example.edu", RoleRequired, RSVPAccepted),
	}
	current := []Attendee{
		testAttendee("student-01", "student@example.edu", RoleRequired, RSVPAccepted),
	}
	changes, err := DiffAudience(previous, current)
	if err != nil {
		t.Fatalf("DiffAudience(): %v", err)
	}
	previous[0].DisplayName = "mutated"
	current[0].DisplayName = "mutated"
	if changes[0].Previous.DisplayName == "mutated" ||
		changes[0].Current.DisplayName == "mutated" {
		t.Fatal("DiffAudience() retained caller-owned attendee storage")
	}

	duplicate := append(current, current[0])
	if _, err := DiffAudience(nil, duplicate); err == nil {
		t.Fatal("DiffAudience() duplicate error = nil")
	}
}

func testAttendee(
	recipientID string,
	email string,
	role ParticipantRole,
	rsvp RSVPState,
) Attendee {
	source := RSVPSourceAuthenticated
	if rsvp == RSVPNeedsAction {
		source = RSVPSourceNone
	}
	return Attendee{
		RecipientID:       recipientID,
		Email:             email,
		DisplayName:       "Người học " + recipientID,
		Role:              role,
		Source:            AudienceSourceRoster,
		ResponseRequested: true,
		RSVP:              rsvp,
		RSVPSource:        source,
	}
}
