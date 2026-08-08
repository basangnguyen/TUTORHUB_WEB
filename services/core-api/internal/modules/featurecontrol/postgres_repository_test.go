package featurecontrol

import "testing"

func TestIsRateQuotaIncludesEveryHourlyQuota(t *testing.T) {
	t.Parallel()

	hourlyQuotas := []QuotaKey{
		QuotaInviteCreationsPerHour,
		QuotaAvailabilityPollCreationsPerHour,
		QuotaAvailabilityPollCapabilityCreationsPerHour,
		QuotaStudyMeetingCreationsPerHour,
		QuotaMessageSendsPerHour,
		QuotaFileUploadIntentsPerHour,
		QuotaMediaSpaceStartsPerHour,
	}
	for _, quota := range hourlyQuotas {
		if !isRateQuota(quota) {
			t.Errorf("expected %q to be classified as a rate quota", quota)
		}
	}

	if isRateQuota(QuotaActiveMediaSpaces) {
		t.Error("expected active media spaces to remain a capacity quota")
	}
}
