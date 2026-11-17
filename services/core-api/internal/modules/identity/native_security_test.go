package identity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMySecurityAvailableMethodsAndRecentExpiry(t *testing.T) {
	for _, test := range []struct {
		name                     string
		active, verified, sender bool
		age                      time.Duration
		want                     string
		recent                   bool
	}{
		{"password and email", true, true, true, time.Minute, "password,email_code", true},
		{"disabled sender", true, true, false, time.Minute, "password", true},
		{"unverified email", true, false, true, time.Minute, "password", true},
		{"OIDC only", false, true, true, time.Minute, "email_code", true},
		{"no available method", false, false, true, time.Minute, "", true},
		{"expired", true, true, true, 11 * time.Minute, "password,email_code", false},
		{"exact expiry", true, true, true, 10 * time.Minute, "password,email_code", false},
		{"future auth", true, true, true, -2 * time.Minute, "password,email_code", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, db, _, principal, _ := passwordSettingsUnitFixture(t)
			db.active, db.verified = test.active, test.verified
			service.config.EmailCodeAvailable = test.sender
			service.now = func() time.Time { return db.now }
			principal.AuthenticatedAt = db.now.Add(-test.age)
			got, err := service.GetMySecurity(context.Background(), principal)
			if err != nil || got.HasPassword != test.active || got.EmailVerified != test.verified || got.MFAEnabled || strings.Join(got.ReauthMethods, ",") != test.want || (got.RecentAuthExpiresAt != nil) != test.recent {
				t.Fatalf("unexpected security overview: %+v %v", got, err)
			}
			encoded, _ := json.Marshal(got)
			for _, secret := range []string{"password_hash", "session_id", "token", "member@example.test"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatal("overview leaks private details")
				}
			}
		})
	}
	service, db, _, principal, _ := passwordSettingsUnitFixture(t)
	db.active, db.verified = true, true
	service.config.EmailCodeAvailable = true
	principal.AssuranceLevel = 2
	got, err := service.GetMySecurity(context.Background(), principal)
	if err != nil || len(got.ReauthMethods) != 0 {
		t.Fatalf("AAL2 session advertised AAL1-only reauth methods: %+v %v", got, err)
	}
}
