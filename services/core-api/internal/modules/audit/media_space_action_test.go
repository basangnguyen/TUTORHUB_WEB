package audit

import "testing"

func TestMediaSpaceDomainEventsMapToAuditableActions(t *testing.T) {
	t.Parallel()

	tests := map[string]Action{
		"media_space.created.v1":         ActionMediaSpaceCreate,
		"media_space.started.v1":         ActionMediaSpaceStart,
		"media_space.ended.v1":           ActionMediaSpaceEnd,
		"media_space.cancelled.v1":       ActionMediaSpaceCancel,
		"media_space.recovered.v1":       ActionMediaSpaceRecover,
		"media_space_member.invited.v1":  ActionMediaSpaceMemberInvite,
		"media_space_member.revoked.v1":  ActionMediaSpaceMemberRevoke,
		"media_space_member.restored.v1": ActionMediaSpaceMemberRestore,
		"media_admission.admitted.v1":    ActionMediaAdmissionAdmit,
		"media_admission.denied.v1":      ActionMediaAdmissionDeny,
		"media_admission.cancelled.v1":   ActionMediaAdmissionCancel,
		"media_admission.restored.v1":    ActionMediaAdmissionRestore,
		"media_admission.expired.v1":     ActionMediaAdmissionExpire,
		"media_space.locked.v1":          ActionMediaSpaceLock,
		"media_space.unlocked.v1":        ActionMediaSpaceUnlock,
		"media_participant.promoted.v1":  ActionMediaParticipantPromote,
		"media_participant.demoted.v1":   ActionMediaParticipantDemote,
		"media_participant.muted.v1":     ActionMediaParticipantMute,
		"media_participant.removed.v1":   ActionMediaParticipantRemove,
	}
	for eventType, want := range tests {
		got, ok := ActionForDomainEvent(eventType)
		if !ok || got != want {
			t.Fatalf("event %q mapped to %q, %t; want %q", eventType, got, ok, want)
		}
	}
}
