//go:build integration

package identity_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type resendTestBudget struct{ limiter httpapi.InvitationRateLimiter }

func (b resendTestBudget) AllowDelivery(ctx context.Context, channel identity.DeliveryChannel) bool {
	if channel != identity.DeliveryEmail {
		return false
	}
	decision := b.limiter.Allow(ctx, httpapi.AuthRateLimitDeliveryDailyEmail, "global", time.Now().UTC())
	return decision.Err == nil && decision.Allowed
}

func TestResendEmailPostgresDelivery(t *testing.T) {
	owner, runtime := identity.NewAuthRuntimeTestPools(t)
	ctx := t.Context()
	email := "resend-" + uuid.NewString() + "@example.test"
	var challengeIDs []uuid.UUID
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, stmt := range []string{
			`DELETE FROM tutorhub.auth_challenge_deliveries WHERE challenge_id=ANY($1::uuid[])`,
			`DELETE FROM tutorhub.auth_transactions WHERE id IN (SELECT transaction_id FROM tutorhub.auth_challenges WHERE id=ANY($1::uuid[]))`,
		} {
			if _, err := owner.Exec(cleanup, stmt, challengeIDs); err != nil {
				t.Error("remove fixture challenge data", err)
			}
		}
		if _, err := owner.Exec(cleanup, "DELETE FROM tutorhub.users WHERE email=$1", email); err != nil {
			t.Error("remove fixture account", err)
		}
	})

	// Roll back the daily bucket after the test. Both its real SQL upserts
	// and the ordinary service/dispatcher transactions use the runtime ACL.
	quotaTx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer quotaTx.Rollback(context.Background())
	limiter, err := httpapi.NewPostgresAuthRateLimiter(quotaTx, 5*time.Second, 10000, 0)
	if err != nil {
		t.Fatal(err)
	}
	used := func() int {
		t.Helper()
		var n int
		if err := quotaTx.QueryRow(ctx, `SELECT COALESCE(sum(used_count),0) FROM tutorhub.rate_limit_windows WHERE purpose=$1`,
			httpapi.AuthRateLimitDeliveryDailyEmail).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	initial := used()
	sent := make(chan string, 2)
	otp := regexp.MustCompile(`\b[0-9]{6}\b`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			To   []string
			Text string
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.To) != 1 || body.To[0] != email {
			http.Error(w, "invalid fixture message", http.StatusBadRequest)
			return
		}
		sent <- otp.FindString(body.Text)
		fmt.Fprint(w, `{"id":"fixture-message-id"}`)
	}))
	defer server.Close()
	sender, err := identity.NewResendEmailSender("test", config.ResendConfig{APIURL: server.URL, APIKey: "fixture", FromEmail: "sender@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := identity.NewCrypto(bytes.Repeat([]byte{0x14}, 32))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := identity.NewDeliveryDispatcher(runtime, crypto, sender, identity.DisabledSender{}, 4, resendTestBudget{limiter})
	if err != nil {
		t.Fatal(err)
	}
	defer dispatcher.Close(ctx)
	passwords, err := identity.NewPasswordService(identity.PasswordOptions{MinimumLength: 8, MaxConcurrency: 1, BreachMode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewNativeAuthService(runtime, crypto, passwords, dispatcher, identity.NativeAuthConfig{
		PasswordEnabled: true, EmailCodeLoginEnabled: true, EmailCodeAvailable: true, CookieSecure: true,
		LegalTermsVersion: "fixture", SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	readCode := func(expectedUsage int) string {
		t.Helper()
		select {
		case code := <-sent:
			if code == "" || used()-initial != expectedUsage {
				t.Fatal("expected a delivered OTP and one daily email charge per challenge")
			}
			return code
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for loopback email")
			return ""
		}
	}
	requireSession := func(session identity.NativeSessionResult, err error) {
		t.Helper()
		var active bool
		if err != nil || session.SessionToken == "" {
			t.Fatal("delivered OTP failed to establish a session", err)
		}
		err = runtime.QueryRow(ctx, `SELECT EXISTS(SELECT FROM tutorhub.sessions s JOIN tutorhub.users u ON u.id=s.user_id
			WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND u.email=$2 AND u.email_verified_at IS NOT NULL)`,
			crypto.Digest("session-token", session.SessionToken), email).Scan(&active)
		if err != nil || !active {
			t.Fatal("new session is not active for the verified fixture user", err)
		}
	}
	input := identity.SignupStartInput{Email: email, DestinationAllowed: true}
	signup, err := service.StartEmailSignup(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	challengeIDs = append(challengeIDs, signup.ChallengeID)
	requireSession(service.CompleteEmailSignup(ctx, signup.Cookie.Value, identity.SignupCompleteInput{
		ChallengeID: signup.ChallengeID, Code: readCode(1), TermsVersion: "fixture", DisplayName: "Student", Password: "Cedar eclipse 41!",
	}))
	login, err := service.StartEmailCodeLogin(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	challengeIDs = append(challengeIDs, login.ChallengeID)
	requireSession(service.VerifyEmailCodeLogin(ctx, login.Cookie.Value, identity.EmailCodeVerifyInput{ChallengeID: login.ChallengeID, Code: readCode(2)}))
	if err := dispatcher.Close(ctx); err != nil {
		t.Fatal(err)
	}
	wantRef := hex.EncodeToString(crypto.Digest("auth-delivery-ref-v1", "fixture-message-id"))
	for _, challengeID := range challengeIDs {
		var valid bool
		err := runtime.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(channel='email' AND provider='resend' AND status='accepted'
			AND delivered_at IS NOT NULL AND provider_message_ref=$2 AND provider_message_ref ~ '^[0-9a-f]{64}$')
			FROM tutorhub.auth_challenge_deliveries WHERE challenge_id=$1`, challengeID, wantRef).Scan(&valid)
		if err != nil || !valid {
			t.Fatal("expected one accepted Resend receipt with the HMAC of the provider ID", err)
		}
	}
}
