//go:build integration

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

// Daily delivery admission is exercised separately against PostgreSQL. This
// local worker has a real inbox and only the post-commit transport budget stub.
type phoneHTTPTestBudget struct{}

func (phoneHTTPTestBudget) AllowDelivery(context.Context, identity.DeliveryChannel) bool { return true }

func inboxOTP(t *testing.T, inbox *devinbox.Inbox, id uuid.UUID) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range inbox.List() {
			if m.ChallengeID == id.String() {
				return m.Code
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("committed challenge did not reach the development inbox")
	return ""
}

func TestPhonePostgresHTTPStartPrivacyAndConcurrentVerification(t *testing.T) {
	url := os.Getenv("AUTH_TEST_DATABASE_URL")
	if url == "" || os.Getenv("AUTH_TEST_DISPOSABLE") != "true" {
		t.Skip("requires disposable auth database")
	}
	ctx := t.Context()
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var key [32]byte
	rand.Read(key[:])
	c, err := identity.NewCrypto(key[:])
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := devinbox.New("test", 32, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inbox.Close)
	router, err := identity.NewDevinboxPhoneRouter(inbox, []string{"VN"})
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := identity.NewDeliveryDispatcher(pool, c, identity.NewDevinboxSender(inbox, identity.DeliveryEmail), router, 32, phoneHTTPTestBudget{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dispatch.Close(context.Background()) })
	cfg := authFoundationConfig()
	cfg.Database.QueryTimeout = 5 * time.Second
	auth := &cfg.Authentication
	auth.Enabled, auth.PhoneEnabled, auth.PhoneSender = true, true, "devinbox"
	auth.PhoneCountryHourlyQuota, auth.PhoneBreakerMinSends, auth.PhoneBreakerMinConversionPct = 100, 50, 30
	passwords, err := identity.NewPasswordService(identity.PasswordOptions{MinimumLength: 8, MaxConcurrency: 1, BreachMode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	native, err := identity.NewNativeAuthService(pool, c, passwords, dispatch, identity.NativeAuthConfig{PhoneEnabled: true,
		LegalTermsVersion: auth.LegalTermsVersion, SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	engine := policy.NewEngine()
	accounts, err := identity.NewService(identity.NewPostgresRepository(pool, time.Second*5, engine), nil, c, engine,
		identity.ServiceConfig{SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour, FlowTTL: 5 * time.Minute}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	handler := NewHandlerWithOptions(cfg, nil, Options{Identity: accounts, NativeAuth: native, Clock: func() time.Time { return now }})

	email := "phone-http-" + uuid.NewString() + "@example.test"
	// Both destinations have the same visible suffix; every public field
	// other than the independent random challenge and binding should match.
	suffix := fmt.Sprintf("%06d", uuid.New().ID()%1000000)
	known, unknown := "+84912"+suffix, "+84913"+suffix
	var challenges []uuid.UUID
	countryAdmissions := 0
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = dispatch.Close(cleanup)
		if _, err := pool.Exec(cleanup, `DELETE FROM tutorhub.auth_transactions WHERE id IN (
			SELECT transaction_id FROM tutorhub.auth_challenges WHERE id=ANY($1::uuid[]))`, challenges); err != nil {
			t.Error(err)
		}
		for _, stmt := range []string{
			`DELETE FROM tutorhub.outbox_events WHERE aggregate_id IN (SELECT id FROM tutorhub.users WHERE email=$1)`,
			`DELETE FROM tutorhub.user_phone_numbers WHERE user_id IN (SELECT id FROM tutorhub.users WHERE email=$1)`,
			`DELETE FROM tutorhub.users WHERE email=$1`,
		} {
			if _, err := pool.Exec(cleanup, stmt, email); err != nil {
				t.Error(err)
			}
		}
		window := now.Truncate(time.Hour)
		destPurpose := string(AuthRateLimitOTPSendDestination)
		for _, number := range []string{known, unknown} {
			hash := invitationRateLimitBucketHash(destPurpose, number)
			if _, err := pool.Exec(cleanup, `DELETE FROM tutorhub.rate_limit_windows WHERE purpose=$1 AND bucket_hash=$2 AND window_started_at=$3`,
				destPurpose, hash[:], window); err != nil {
				t.Error(err)
			}
		}
		purpose := string(AuthRateLimitOTPSendCountry)
		hash := invitationRateLimitBucketHash(purpose, "VN")
		if _, err := pool.Exec(cleanup, `UPDATE tutorhub.rate_limit_windows SET used_count=used_count-$4
			WHERE purpose=$1 AND bucket_hash=$2 AND window_started_at=$3 AND used_count >= $4`, purpose, hash[:], window, countryAdmissions); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(cleanup, `DELETE FROM tutorhub.rate_limit_windows WHERE purpose=$1 AND bucket_hash=$2 AND window_started_at=$3 AND used_count=0`, purpose, hash[:], window); err != nil {
			t.Error(err)
		}
	})
	signup, err := native.StartEmailSignup(ctx, identity.SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	challenges = append(challenges, signup.ChallengeID)
	session, err := native.CompleteEmailSignup(ctx, signup.Cookie.Value, identity.SignupCompleteInput{ChallengeID: signup.ChallengeID,
		Code: inboxOTP(t, inbox, signup.ChallengeID), DisplayName: "Phone HTTP fixture", Password: "Chartreuse orchard 92!", TermsVersion: auth.LegalTermsVersion})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := accounts.Authenticate(ctx, session.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	// Bind outside the public login limiter so the first five HTTP admissions
	// exercise all five slots of a fresh normalized destination.
	claim, err := native.StartMyPhone(ctx, principal, identity.PhoneStartInput{PhoneNumber: known,
		Guard: func(context.Context, pgx.Tx, identity.NormalizedPhoneNumber, bool) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	challenges = append(challenges, claim.ChallengeID)
	if _, err = native.VerifyMyPhone(ctx, principal, claim.Cookie.Value, identity.EmailCodeVerifyInput{ChallengeID: claim.ChallengeID, Code: inboxOTP(t, inbox, claim.ChallengeID)}); err != nil {
		t.Fatal(err)
	}

	var expectedBody map[string]any
	var expectedHeaders http.Header
	var realChallenge uuid.UUID
	var realBinding *http.Cookie
	var deliveredIDs, dummyIDs []uuid.UUID
	// The sixth canonical equivalent is exhausted. The seventh request is
	// a different, valid, unregistered mobile with the same masked suffix.
	for i, raw := range []string{known, "0" + known[3:], known[1:], known, "0" + known[3:], "0912 " + suffix[:3] + " " + suffix[3:], unknown} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, nativeRequest("/api/v1/auth/phone/start", fmt.Sprintf(`{"phone_number":%q}`, raw), false))
		if w.Code != 202 {
			t.Fatalf("phone start #%d HTTP %d", i+1, w.Code)
		}
		countryAdmissions++
		var fields map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
			t.Fatal(err)
		}
		challenge, err := uuid.Parse(fmt.Sprint(fields["challenge_id"]))
		if err != nil {
			t.Fatal("missing random challenge identifier")
		}
		challenges = append(challenges, challenge)
		delete(fields, "challenge_id")
		if fields["masked_destination"] != "+84 ***"+suffix[2:] || fields["resend_after_seconds"] != float64(60) || fields["expires_in_seconds"] != float64(300) {
			t.Fatal("inconsistent challenge metadata")
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != identity.AuthTransactionCookieName(false) || len(cookies[0].Value) != 43 ||
			!cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
			t.Fatal("unexpected auth transaction cookie")
		}
		headers := w.Header().Clone()
		headers.Del("Set-Cookie")
		if i == 0 {
			expectedBody, expectedHeaders, realChallenge, realBinding = fields, headers, challenge, cookies[0]
		} else if !reflect.DeepEqual(fields, expectedBody) || !reflect.DeepEqual(headers, expectedHeaders) {
			t.Fatal("public headers or JSON distinguish phone state")
		}
		if i < 5 {
			deliveredIDs = append(deliveredIDs, challenge)
		} else {
			dummyIDs = append(dummyIDs, challenge)
		}
	}
	if err := dispatch.Close(ctx); err != nil {
		t.Fatal(err)
	}
	received := make(map[string]string)
	for _, message := range inbox.List() {
		if message.Purpose == "phone_login" {
			received[message.ChallengeID] = message.Code
		}
	}
	if len(received) != 5 {
		t.Fatal("phone delivery count did not match available quota")
	}
	for _, id := range deliveredIDs {
		if len(received[id.String()]) != 6 {
			t.Fatal("deliverable challenge missing from inbox")
		}
	}
	for _, id := range dummyIDs {
		var dummy, skipped bool
		err := pool.QueryRow(ctx, `SELECT NOT c.deliverable,d.status='skipped' AND d.error_code='not_deliverable'
			FROM tutorhub.auth_challenges c JOIN tutorhub.auth_challenge_deliveries d ON d.challenge_id=c.id WHERE c.id=$1`, id).Scan(&dummy, &skipped)
		if err != nil || !dummy || !skipped || received[id.String()] != "" {
			t.Fatal("dummy phone delivered or left queued", err)
		}
	}
	body := fmt.Sprintf(`{"challenge_id":%q,"code":%q}`, realChallenge.String(), received[realChallenge.String()])
	results := make(chan *httptest.ResponseRecorder, 2)
	ready := make(chan struct{})
	for range 2 {
		go func() {
			w := httptest.NewRecorder()
			r := nativeRequest("/api/v1/auth/phone/verify", body, false)
			r.Header.Del("Cookie")
			r.AddCookie(realBinding)
			<-ready
			handler.ServeHTTP(w, r)
			results <- w
		}()
	}
	close(ready)
	var winners, losers int
	for range 2 {
		w := <-results
		switch w.Code {
		case 200:
			if !strings.Contains(w.Body.String(), `"status":"authenticated"`) {
				t.Fatal("missing authenticated status")
			}
			winners++
		case 401:
			if !strings.Contains(w.Body.String(), "auth_invalid_verification_code") {
				t.Fatal("wrong verification failure")
			}
			losers++
		default:
			t.Fatal("unexpected phone verify status", w.Code)
		}
	}
	var sessions int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1 AND auth_methods=ARRAY['phone_otp']::text[] AND assurance_level=1`, principal.User.ID).Scan(&sessions)
	if err != nil || winners != 1 || losers != 1 || sessions != 1 {
		t.Fatal("concurrent HTTP verification did not create exactly one AAL1 session", err)
	}
}
