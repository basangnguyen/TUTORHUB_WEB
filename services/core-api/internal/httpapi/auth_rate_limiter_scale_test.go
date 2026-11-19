package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestScaledAuthRateLimitsOnlyMultiplyIPBuckets(t *testing.T) {
	t.Parallel()
	base := authRateLimitPolicies(7, 3)
	scaled := scaledAuthRateLimitPolicies(7, 3, 10)
	if len(base) != len(scaled) {
		t.Fatal("scaling must keep every purpose")
	}
	for action, policy := range base {
		want := policy.Limit
		if strings.HasSuffix(string(action), ".ip") {
			want *= 10
		}
		if scaled[action].Limit != want || scaled[action].Window != policy.Window {
			t.Errorf("%s: got %+v want limit %d window %s", action, scaled[action], want, policy.Window)
		}
	}
	if scaled[AuthRateLimitSignupIP].Limit != 100 || scaled[AuthRateLimitOTPSendDestination].Limit != 5 ||
		scaled[AuthRateLimitReauthUser].Limit != 10 || scaled[AuthRateLimitDeliveryDailyEmail].Limit != 7 {
		t.Fatal("only per-IP buckets may scale")
	}
	if _, err := NewScaledPostgresAuthRateLimiter(nil, time.Second, 0, 0, 1); err == nil {
		t.Fatal("database is required")
	}
	if _, err := NewScaledPostgresAuthRateLimiter(&recordingInvitationRateLimitDatabase{}, time.Second, 0, 0, 0); err == nil {
		t.Fatal("multiplier must be positive")
	}
}
