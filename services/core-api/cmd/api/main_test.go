package main

import (
	"testing"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

func TestFeatureControlGuardrailsOmitFeaturesThatAreNotForcedOff(t *testing.T) {
	t.Parallel()

	configuration := config.FeatureControlConfig{
		DisableClassManagement:                        true,
		EnableClassroomMediaRooms:                     true,
		EnableInstantStudyRooms:                       true,
		MaxMembers:                                    10_000,
		MaxActiveClasses:                              1_000,
		MaxInviteCreationsPerHour:                     10_000,
		MaxActiveAvailabilityPolls:                    200,
		MaxAvailabilityPollRangeDays:                  90,
		MaxAvailabilityPollSlots:                      1_000,
		MaxAvailabilityPollParticipants:               500,
		MaxAvailabilityPollCreationsPerHour:           200,
		MaxAvailabilityPollCapabilityCreationsPerHour: 1_000,
		MaxActiveStudyMeetings:                        200,
		MaxStudyMeetingCreationsPerHour:               200,
		MaxMessagesPerTenant:                          10_000_000,
		MaxMessageSendsPerHour:                        100_000,
		MaxFilesPerTenant:                             1_000_000,
		MaxFileBytesPerTenant:                         1_099_511_627_776,
		MaxSingleFileBytes:                            5_368_709_120,
		MaxFileUploadIntentsPerHour:                   100_000,
		MaxActiveMediaSpaces:                          100,
		MaxMediaParticipantsPerSpace:                  50,
		MaxActiveMediaParticipants:                    500,
		MaxMediaSpaceStartsPerHour:                    200,
	}
	guardrails := featureControlGuardrails(configuration)

	if len(guardrails.ForcedOffFeatures) != 3 ||
		!guardrails.ForcedOffFeatures[featurecontrol.FeatureClassManagement] {
		t.Fatalf("unexpected forced-off feature map: %+v", guardrails.ForcedOffFeatures)
	}
	if !guardrails.ForcedOffFeatures[featurecontrol.FeatureInAppNotifications] {
		t.Fatalf("notification visibility must fail closed by default: %+v", guardrails.ForcedOffFeatures)
	}
	if !guardrails.ForcedOffFeatures[featurecontrol.FeatureClassSessionRecurrence] {
		t.Fatalf("recurrence visibility must fail closed by default: %+v", guardrails.ForcedOffFeatures)
	}
	if _, err := featurecontrol.NewCatalog(guardrails); err != nil {
		t.Fatalf("valid runtime configuration must initialize the catalog: %v", err)
	}
}

func TestFeatureControlGuardrailsForceOffClassSessionScheduling(t *testing.T) {
	t.Parallel()

	guardrails := featureControlGuardrails(config.FeatureControlConfig{
		DisableClassSessionScheduling:                 true,
		EnableClassSessionRecurrence:                  true,
		EnableInAppNotifications:                      true,
		EnableClassroomMediaRooms:                     true,
		EnableInstantStudyRooms:                       true,
		MaxMembers:                                    10_000,
		MaxActiveClasses:                              1_000,
		MaxInviteCreationsPerHour:                     10_000,
		MaxActiveAvailabilityPolls:                    200,
		MaxAvailabilityPollRangeDays:                  90,
		MaxAvailabilityPollSlots:                      1_000,
		MaxAvailabilityPollParticipants:               500,
		MaxAvailabilityPollCreationsPerHour:           200,
		MaxAvailabilityPollCapabilityCreationsPerHour: 1_000,
		MaxActiveStudyMeetings:                        200,
		MaxStudyMeetingCreationsPerHour:               200,
		MaxMessagesPerTenant:                          10_000_000,
		MaxMessageSendsPerHour:                        100_000,
		MaxFilesPerTenant:                             1_000_000,
		MaxFileBytesPerTenant:                         1_099_511_627_776,
		MaxSingleFileBytes:                            5_368_709_120,
		MaxFileUploadIntentsPerHour:                   100_000,
	})

	if len(guardrails.ForcedOffFeatures) != 1 ||
		!guardrails.ForcedOffFeatures[featurecontrol.FeatureClassSessionScheduling] {
		t.Fatalf(
			"class session scheduling kill-switch was not mapped exactly: %+v",
			guardrails.ForcedOffFeatures,
		)
	}
}

func TestFeatureControlGuardrailsForceOffConversations(t *testing.T) {
	t.Parallel()

	guardrails := featureControlGuardrails(config.FeatureControlConfig{
		DisableConversations:                          true,
		EnableClassSessionRecurrence:                  true,
		EnableInAppNotifications:                      true,
		EnableClassroomMediaRooms:                     true,
		EnableInstantStudyRooms:                       true,
		MaxMembers:                                    10_000,
		MaxActiveClasses:                              1_000,
		MaxInviteCreationsPerHour:                     10_000,
		MaxActiveAvailabilityPolls:                    200,
		MaxAvailabilityPollRangeDays:                  90,
		MaxAvailabilityPollSlots:                      1_000,
		MaxAvailabilityPollParticipants:               500,
		MaxAvailabilityPollCreationsPerHour:           200,
		MaxAvailabilityPollCapabilityCreationsPerHour: 1_000,
		MaxActiveStudyMeetings:                        200,
		MaxStudyMeetingCreationsPerHour:               200,
		MaxMessagesPerTenant:                          10_000_000,
		MaxMessageSendsPerHour:                        100_000,
		MaxFilesPerTenant:                             1_000_000,
		MaxFileBytesPerTenant:                         1_099_511_627_776,
		MaxSingleFileBytes:                            5_368_709_120,
		MaxFileUploadIntentsPerHour:                   100_000,
	})

	if len(guardrails.ForcedOffFeatures) != 1 ||
		!guardrails.ForcedOffFeatures[featurecontrol.FeatureConversations] {
		t.Fatalf("conversation kill-switch was not mapped exactly: %+v", guardrails.ForcedOffFeatures)
	}
}

func TestFeatureControlGuardrailsForceOffMediaAndApplyCeilings(t *testing.T) {
	t.Parallel()

	guardrails := featureControlGuardrails(config.FeatureControlConfig{
		EnableClassSessionRecurrence: true,
		EnableInAppNotifications:     true,
		MaxActiveMediaSpaces:         80,
		MaxMediaParticipantsPerSpace: 40,
		MaxActiveMediaParticipants:   400,
		MaxMediaSpaceStartsPerHour:   150,
	})
	if len(guardrails.ForcedOffFeatures) != 2 ||
		!guardrails.ForcedOffFeatures[featurecontrol.FeatureClassroomMediaRooms] ||
		!guardrails.ForcedOffFeatures[featurecontrol.FeatureInstantStudyRooms] {
		t.Fatalf("media features must fail closed: %+v", guardrails.ForcedOffFeatures)
	}
	wantCeilings := map[featurecontrol.QuotaKey]int64{
		featurecontrol.QuotaActiveMediaSpaces:         80,
		featurecontrol.QuotaMediaParticipantsPerSpace: 40,
		featurecontrol.QuotaActiveMediaParticipants:   400,
		featurecontrol.QuotaMediaSpaceStartsPerHour:   150,
	}
	for key, want := range wantCeilings {
		if got := guardrails.QuotaCeilings[key]; got != want {
			t.Fatalf("media quota ceiling %q = %d, want %d", key, got, want)
		}
	}

	enabled := featureControlGuardrails(config.FeatureControlConfig{
		EnableClassSessionRecurrence: true,
		EnableInAppNotifications:     true,
		EnableClassroomMediaRooms:    true,
		EnableInstantStudyRooms:      true,
	})
	if enabled.ForcedOffFeatures[featurecontrol.FeatureClassroomMediaRooms] ||
		enabled.ForcedOffFeatures[featurecontrol.FeatureInstantStudyRooms] {
		t.Fatalf("explicitly enabled media guardrails were forced off: %+v", enabled.ForcedOffFeatures)
	}
}

func TestAvailabilityPollFeatureFailsClosedWithoutProtectedData(t *testing.T) {
	t.Parallel()

	configuration := featureControlsWithRuntimePrerequisites(
		config.FeatureControlConfig{DisableAvailabilityPolls: false},
		false,
		true,
	)
	if !configuration.DisableAvailabilityPolls {
		t.Fatal("availability polls must be forced off when their protected-data runtime is absent")
	}
	configured := featureControlsWithRuntimePrerequisites(
		config.FeatureControlConfig{DisableAvailabilityPolls: false},
		true,
		true,
	)
	if configured.DisableAvailabilityPolls {
		t.Fatal("configured protected data must not force the availability feature off")
	}
}

func TestFileUploadFeatureFailsClosedWithoutObjectStorage(t *testing.T) {
	t.Parallel()

	configuration := featureControlsWithRuntimePrerequisites(
		config.FeatureControlConfig{},
		true,
		false,
	)
	if !configuration.DisableFileUploads {
		t.Fatal("file uploads must be forced off when object storage is absent")
	}
	configured := featureControlsWithRuntimePrerequisites(
		config.FeatureControlConfig{},
		true,
		true,
	)
	if configured.DisableFileUploads {
		t.Fatal("configured object storage must not force file uploads off")
	}
}
