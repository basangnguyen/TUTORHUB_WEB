package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func TestPhoneLoginAvailability(t *testing.T) {
	base := config.AuthenticationConfig{PhoneEnabled: true, PhoneSender: "devinbox", PhoneOTPDailyQuota: 2}
	if !phoneLoginAvailable(base) {
		t.Fatal("configured phone sender should count as available")
	}
	base.PhoneOTPDailyQuota = 0
	if phoneLoginAvailable(base) {
		t.Fatal("zero delivery budget should not count as a login method")
	}
	base.PhoneOTPDailyQuota = 2
	base.PhoneSender = "disabled"
	if phoneLoginAvailable(base) {
		t.Fatal("disabled phone sender should not count as a login method")
	}
}

func TestPaidPhoneSenderStartup(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://fixture@127.0.0.1:1/auth_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	auth := config.AuthenticationConfig{
		PhoneEnabled: true, PhoneSender: "zns", PhoneFallbackSender: "twilio",
		PhoneAllowedCountries: []string{"VN", "SG"}, PhoneOTPDailyQuota: 3, DeliveryBudgetApproved: true,
		SessionKey: bytes.Repeat([]byte{0x42}, 32), LegalTermsVersion: "fixture",
		SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour,
		ZNS:          config.ZNSConfig{AccessToken: "fixture", TemplateID: "fixture"},
		SMSBrandname: config.SMSBrandnameConfig{APIURL: "https://brand.example.test", Sender: "TutorHub", APIToken: "fixture"},
		Twilio:       config.TwilioConfig{AccountSID: "AC" + strings.Repeat("x", 32), AuthToken: "fixture", From: "MG" + strings.Repeat("x", 32)},
	}
	for _, tc := range []struct {
		primary, fallback string
		countries         []string
	}{
		{"zns", "twilio", []string{"VN", "SG"}},
		{"zns", "sms_brandname", []string{"VN"}},
		{"sms_brandname", "twilio", []string{"VN", "SG"}},
		{"twilio", "sms_brandname", []string{"VN", "SG"}},
	} {
		selected := auth
		selected.PhoneSender, selected.PhoneFallbackSender, selected.PhoneAllowedCountries = tc.primary, tc.fallback, tc.countries
		runtime, err := newAuthFoundation(config.Config{Environment: "test", Authentication: selected}, pool, nil)
		if err != nil {
			t.Fatalf("%s/%s did not initialize: %v", tc.primary, tc.fallback, err)
		}
		if runtime.native == nil || runtime.dispatcher == nil || runtime.inbox != nil {
			t.Error("paid phone runtime has incorrect service wiring")
		}
		runtime.close()
	}
	for _, change := range []func(*config.AuthenticationConfig){
		func(a *config.AuthenticationConfig) { a.DeliveryBudgetApproved = false },
		func(a *config.AuthenticationConfig) { a.PhoneSender = "disabled" },
		func(a *config.AuthenticationConfig) { a.PhoneFallbackSender = "sms_brandname" },
		func(a *config.AuthenticationConfig) { a.Twilio.AuthToken = "" },
		func(a *config.AuthenticationConfig) { a.ZNS.AccessToken = "" },
	} {
		invalid := auth
		change(&invalid)
		if runtime, err := newAuthFoundation(config.Config{Environment: "test", Authentication: invalid}, pool, nil); err == nil {
			runtime.close()
			t.Error("unsafe or incomplete paid configuration initialized")
		}
	}
}

func TestConfiguredPhoneRoutingAcrossCountries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/zns", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":0,"data":{"msg_id":"vn-primary"}}`))
	})
	mux.HandleFunc("/2010-04-01/Accounts/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sid":"sg-primary"}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	auth := config.AuthenticationConfig{
		PhoneSender: "zns", PhoneFallbackSender: "twilio", PhoneAllowedCountries: []string{"VN", "SG"},
		DeliveryBudgetApproved: true,
		ZNS:                    config.ZNSConfig{APIURL: server.URL + "/zns", AccessToken: "fixture", TemplateID: "fixture"},
		Twilio:                 config.TwilioConfig{APIBaseURL: server.URL, AccountSID: "AC" + strings.Repeat("x", 32), AuthToken: "fixture", From: "+12025550123"},
	}
	sender, err := configuredPhoneSender("test", auth, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ number, provider, ref string }{
		{"+84912345678", "zns", "vn-primary"}, {"+6591234567", "twilio", "sg-primary"},
	} {
		r, err := sender.SendChallenge(context.Background(), identity.ChallengeMessage{
			Destination: tc.number, Code: "123456", Channel: identity.DeliverySMS, ExpiresAt: time.Now().Add(time.Minute),
		})
		if err != nil || r.Provider != tc.provider || r.ProviderMessageRef != tc.ref {
			t.Fatalf("wrong country route for %s", tc.provider)
		}
	}
	if sender.Supports(1) {
		t.Fatal("unconfigured country has a route")
	}
}

type deliveryBudgetLimiter struct {
	action httpapi.InvitationRateLimitAction
	bucket string
	result httpapi.InvitationRateLimitDecision
}

func (limiter *deliveryBudgetLimiter) Allow(_ context.Context, action httpapi.InvitationRateLimitAction, bucket string, _ time.Time) httpapi.InvitationRateLimitDecision {
	limiter.action, limiter.bucket = action, bucket
	return limiter.result
}

func TestAuthDeliveryBudgetIsGlobalAndFailsClosed(t *testing.T) {
	for _, channel := range []identity.DeliveryChannel{identity.DeliveryEmail, identity.DeliverySMS} {
		limiter := &deliveryBudgetLimiter{result: httpapi.InvitationRateLimitDecision{Allowed: true}}
		budget := authDeliveryBudget{limiter: limiter}
		if !budget.AllowDelivery(context.Background(), channel) || limiter.bucket != "global" {
			t.Fatal("delivery quota must use a global bucket")
		}
		want := httpapi.AuthRateLimitDeliveryDailyPhone
		if channel == identity.DeliveryEmail {
			want = httpapi.AuthRateLimitDeliveryDailyEmail
		}
		if limiter.action != want {
			t.Fatal("delivery quota used the wrong channel")
		}
		limiter.result = httpapi.InvitationRateLimitDecision{Allowed: true, Err: errors.New("unavailable")}
		if budget.AllowDelivery(context.Background(), channel) || (authDeliveryBudget{}).AllowDelivery(context.Background(), channel) {
			t.Fatal("quota storage failure must prevent delivery")
		}
	}
}

func TestAuthFoundationDisabledNeedsNoDatabase(t *testing.T) {
	runtime, err := newAuthFoundation(config.Config{}, nil, nil)
	if err != nil || runtime.inbox != nil || runtime.dispatcher != nil {
		t.Fatal("disabled foundation must preserve database-free startup")
	}
	runtime.close()
	for _, sender := range []string{"devinbox", "smtp", "ses"} {
		cfg := config.Config{Environment: "test", Authentication: config.AuthenticationConfig{EmailSender: sender}}
		if _, err := newAuthFoundation(cfg, nil, nil); err == nil {
			t.Fatal("configured delivery must fail closed without prerequisites")
		}
	}
}

func TestNativeStartupDoesNotDiscoverOIDC(t *testing.T) {
	provider, err := configuredIdentityProvider(context.Background(), config.AuthenticationConfig{
		Enabled: true, SignupEnabled: true, PasswordEnabled: true, OIDCEnabled: false,
		IssuerURL: "this-ignored-value-must-never-be-discovered",
	})
	if err != nil || provider != nil {
		t.Fatal("native-only startup attempted OIDC initialization")
	}
	if _, err := newAuthFoundation(config.Config{Authentication: config.AuthenticationConfig{SignupEnabled: true}}, nil, nil); err == nil {
		t.Fatal("native authentication must reject missing runtime prerequisites")
	}
}

func TestEmailCodeOnlyRuntimeAndDisabledPasswordStatus(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://fixture@127.0.0.1:1/auth_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, emailCode := range []bool{false, true} {
		auth := config.AuthenticationConfig{
			Enabled: true, EmailCodeEnabled: emailCode, SessionKey: bytes.Repeat([]byte{0x42}, 32),
			LegalTermsVersion: "2026-11-16-v2.0", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 24 * time.Hour,
			SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour,
		}
		runtime, err := newAuthFoundation(config.Config{Environment: "test", Authentication: auth}, pool, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := runtime.native.(identity.NativeAccountAuthAPI); !ok {
			t.Fatal("authenticated password status and email-code routes require the native account service")
		}
		runtime.close()
		provider, err := configuredIdentityProvider(context.Background(), auth)
		if err != nil || provider != nil {
			t.Fatal("email-code-only runtime attempted OIDC discovery")
		}
	}
	if _, err := newAuthFoundation(config.Config{Authentication: config.AuthenticationConfig{EmailCodeEnabled: true}}, nil, nil); err == nil {
		t.Fatal("email-code-only runtime accepted missing database/key")
	}
}

func TestDeviceManagementStartupRequiresRuntimeAndDoesNotEnableOIDC(t *testing.T) {
	if _, err := newAuthFoundation(config.Config{Authentication: config.AuthenticationConfig{DeviceManagementEnabled: true}}, nil, nil); err == nil {
		t.Fatal("device management accepted missing database/key")
	}
	pool, err := pgxpool.New(context.Background(), "postgres://fixture@127.0.0.1:1/auth_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	auth := config.AuthenticationConfig{
		DeviceManagementEnabled: true, SessionKey: bytes.Repeat([]byte{0x42}, 32),
		LegalTermsVersion: "2026-11-16-v2.0", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 24 * time.Hour,
		SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour,
	}
	runtime, err := newAuthFoundation(config.Config{Environment: "test", Authentication: auth}, pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.close()
	if _, ok := runtime.native.(identity.NativeSecurityAPI); !ok {
		t.Fatal("device management requires the session security service")
	}
	if provider, err := configuredIdentityProvider(context.Background(), auth); err != nil || provider != nil {
		t.Fatal("device management must not activate OIDC discovery")
	}
}

// The parent DSN is only used to create an isolated database and tear it down.
// Runtime traffic (including the daily limiter) uses a role-bound pool.
func newAuthTestDatabase(t *testing.T, envKey, prefix string) (owner, runtime *pgxpool.Pool) {
	t.Helper()
	parentDSN := os.Getenv(envKey)
	if parentDSN == "" || os.Getenv("AUTH_TEST_DISPOSABLE") != "true" {
		t.Skipf("requires %s and AUTH_TEST_DISPOSABLE=true", envKey)
	}
	ctx := t.Context()
	parent, err := pgxpool.New(ctx, parentDSN)
	if err != nil {
		t.Fatal("invalid disposable owner connection")
	}
	t.Cleanup(parent.Close)
	name := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	dbName := pgx.Identifier{name}.Sanitize()
	roleName := name + "_runtime"
	role := pgx.Identifier{roleName}.Sanitize()
	childURL, err := url.Parse(parentDSN)
	if err != nil || (childURL.Scheme != "postgres" && childURL.Scheme != "postgresql") {
		t.Fatal("requires a disposable PostgreSQL URL")
	}
	childURL.Path = "/" + name
	childConfig, err := pgxpool.ParseConfig(childURL.String())
	if err != nil || childConfig.ConnConfig.Database != name {
		t.Fatal("owner URL overrides the generated database name")
	}
	if _, err = parent.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatal("create isolated paid-delivery database", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := parent.Exec(cleanup, "DROP DATABASE "+dbName); err != nil {
			t.Error("drop isolated paid-delivery database", err)
		}
		if _, err := parent.Exec(cleanup, "DROP ROLE IF EXISTS "+role); err != nil {
			t.Error("drop isolated runtime role", err)
		}
	})
	owner, err = pgxpool.NewWithConfig(ctx, childConfig)
	if err != nil {
		t.Fatal("connect to isolated database")
	}
	t.Cleanup(owner.Close)
	var currentDB string
	if err := owner.QueryRow(ctx, "SELECT current_database()").Scan(&currentDB); err != nil || currentDB != name {
		t.Fatal("disposable connection did not select generated database")
	}
	if err := migrationrunner.Up(ctx, childURL.String()); err != nil {
		t.Fatal("migrate isolated paid-delivery database")
	}
	for _, statement := range []string{
		"CREATE ROLE " + role + " NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT",
		"GRANT " + role + " TO CURRENT_USER",
		"GRANT USAGE ON SCHEMA tutorhub TO " + role,
		"GRANT SELECT,INSERT,UPDATE(display_name) ON tutorhub.users TO " + role,
		"GRANT SELECT,INSERT,UPDATE(active_tenant_id,token_hash,csrf_token_hash,last_seen_at,expires_at,absolute_expires_at,revoked_at,revoked_reason,context_version) ON tutorhub.sessions TO " + role,
		"GRANT SELECT,INSERT ON tutorhub.identities,tutorhub.auth_flows TO " + role,
		"GRANT SELECT ON tutorhub.memberships,tutorhub.tenants TO " + role,
		"GRANT INSERT ON tutorhub.outbox_events TO " + role,
		"GRANT SELECT,INSERT,UPDATE(used_count,updated_at) ON tutorhub.rate_limit_windows TO " + role,
	} {
		if _, err = owner.Exec(ctx, statement); err != nil {
			t.Fatal("prepare disposable runtime role", err)
		}
	}
	if err := identity.ProvisionAuthFoundationRuntimeACL(ctx, owner, roleName); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := owner.Config()
	runtimeConfig.ConnConfig.RuntimeParams["role"] = roleName
	runtime, err = pgxpool.NewWithConfig(ctx, runtimeConfig)
	if err != nil {
		t.Fatal("open role-bound runtime pool")
	}
	t.Cleanup(runtime.Close)
	var current string
	if err := runtime.QueryRow(ctx, "SELECT current_user").Scan(&current); err != nil || current != roleName {
		t.Fatal("runtime pool did not assume the isolated ACL")
	}
	return owner, runtime
}

func seedPaidPhoneLogin(t *testing.T, cfg config.Config, pool *pgxpool.Pool, phoneInput identity.PhoneStartInput) {
	t.Helper()
	ctx := t.Context()
	cfg.Authentication.PhoneSender = "devinbox"
	cfg.Authentication.PhoneFallbackSender = "disabled"
	cfg.Authentication.EmailSender = "devinbox"
	rt, err := newAuthFoundation(cfg, pool, &deliveryBudgetLimiter{result: httpapi.InvitationRateLimitDecision{Allowed: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.close()
	code := func(id uuid.UUID) string {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			for _, message := range rt.inbox.List() {
				if message.ChallengeID == id.String() {
					return message.Code
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("seed OTP did not reach the fixture inbox")
		return ""
	}
	const email = "phone-fixture@example.test"
	signup, err := rt.native.StartEmailSignup(ctx, identity.SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.native.CompleteEmailSignup(ctx, signup.Cookie.Value, identity.SignupCompleteInput{
		ChallengeID: signup.ChallengeID, Code: code(signup.ChallengeID), DisplayName: "Student", Password: "Cedar eclipse 41!", TermsVersion: cfg.Authentication.LegalTermsVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	var principal identity.Principal
	err = pool.QueryRow(ctx, `SELECT s.id,u.id,s.context_version,s.assurance_level FROM tutorhub.sessions s
		JOIN tutorhub.users u ON u.id=s.user_id WHERE u.email=$1`, email).Scan(&principal.SessionID, &principal.User.ID, &principal.ContextVersion, &principal.AssuranceLevel)
	if err != nil {
		t.Fatal(err)
	}
	account := rt.native.(identity.NativePhoneAPI)
	claim, err := account.StartMyPhone(ctx, principal, phoneInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := account.VerifyMyPhone(ctx, principal, claim.Cookie.Value, identity.EmailCodeVerifyInput{ChallengeID: claim.ChallengeID, Code: code(claim.ChallengeID)}); err != nil {
		t.Fatal(err)
	}
}

func TestPaidPhonePostgresDelivery(t *testing.T) {
	owner, pool := newAuthTestDatabase(t, "AUTH11_TEST_POSTGRES_ADMIN_URL", "auth11_")
	cfg := config.Config{Environment: "test", Authentication: config.AuthenticationConfig{
		Enabled: true, PhoneEnabled: true, PasswordEnabled: true, PhoneSender: "zns", PhoneFallbackSender: "sms_brandname",
		PhoneAllowedCountries: []string{"VN"}, PhoneOTPDailyQuota: 2, DeliveryBudgetApproved: true,
		SessionKey: bytes.Repeat([]byte{0x41}, 32), LegalTermsVersion: "fixture", PasswordBreachCheck: "off",
		SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour,
	}}
	cfg.Database.QueryTimeout = 5 * time.Second
	input := identity.PhoneStartInput{PhoneNumber: "+84912345678", CountryCode: "VN",
		Guard: func(context.Context, pgx.Tx, identity.NormalizedPhoneNumber, bool) (bool, error) { return true, nil },
	}
	seedPaidPhoneLogin(t, cfg, pool, input)
	crypto, err := identity.NewCrypto(cfg.Authentication.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	const providerRef = "upstream-reference"
	wantRef := hex.EncodeToString(crypto.Digest("auth-delivery-ref-v1", providerRef))
	type auditRow struct{ Channel, Provider, Status, ErrorCode, Ref string }
	for _, tc := range []struct {
		name           string
		znsUnavailable bool
		quota, used    int
		calls          []string
		audit          []auditRow
	}{
		{"primary", false, 2, 1, []string{"zns"}, []auditRow{{"zalo_zns", "zns", "accepted", "", wantRef}}},
		{"fallback", true, 2, 2, []string{"zns", "sms_brandname"}, []auditRow{{"zalo_zns", "zns", "failed", "send_failed", ""}, {"sms", "sms_brandname", "accepted", "", wantRef}}},
		{"fallback_quota_denied", true, 1, 1, []string{"zns"}, []auditRow{{"zalo_zns", "zns", "failed", "fallback_quota_denied", ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			// This database was freshly created for this suite. Give each case a fresh daily window.
			if _, err := owner.Exec(ctx, `DELETE FROM tutorhub.rate_limit_windows WHERE purpose=$1`, httpapi.AuthRateLimitDeliveryDailyPhone); err != nil {
				t.Fatal(err)
			}
			type submission struct{ provider, code string }
			submitted := make(chan submission, 8)
			mux := http.NewServeMux()
			mux.HandleFunc("/zns", func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Data map[string]string `json:"template_data"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error("invalid ZNS request")
				}
				submitted <- submission{"zns", payload.Data["otp"]}
				if tc.znsUnavailable {
					_, _ = w.Write([]byte(`{"error":-118,"message":"Zalo account not existed"}`))
					return
				}
				_, _ = w.Write([]byte(`{"error":0,"data":{"msg_id":"upstream-reference"}}`))
			})
			mux.HandleFunc("/sms", func(w http.ResponseWriter, r *http.Request) {
				var payload struct{ Message string }
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error("invalid Brandname request")
				}
				submitted <- submission{"sms_brandname", payload.Message}
				_, _ = w.Write([]byte(`{"message_id":"upstream-reference"}`))
			})
			upstream := httptest.NewServer(mux)
			t.Cleanup(upstream.Close)
			selected := cfg
			selected.Authentication.ZNS = config.ZNSConfig{APIURL: upstream.URL + "/zns", AccessToken: "fixture", TemplateID: "fixture", OTPParam: "otp"}
			selected.Authentication.SMSBrandname = config.SMSBrandnameConfig{APIURL: upstream.URL + "/sms", APIToken: "fixture", Sender: "TutorHub", Template: "{{code}}"}
			selected.Authentication.PhoneOTPDailyQuota = tc.quota
			limiter, err := httpapi.NewPostgresAuthRateLimiter(pool, selected.Database.QueryTimeout, 0, selected.Authentication.PhoneOTPDailyQuota)
			if err != nil {
				t.Fatal(err)
			}
			rt, err := newAuthFoundation(selected, pool, limiter)
			if err != nil {
				t.Fatal(err)
			}
			defer rt.close()
			account := rt.native.(identity.NativePhoneAPI)
			challenge, err := account.StartPhoneLogin(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if err := rt.dispatcher.Close(ctx); err != nil {
				t.Fatal(err)
			}
			close(submitted)
			var calls []string
			var lastCode string
			for send := range submitted {
				calls = append(calls, send.provider)
				lastCode = send.code
			}
			if !slices.Equal(calls, tc.calls) {
				t.Fatalf("unexpected sequence of provider requests: %v", calls)
			}
			var used int
			err = pool.QueryRow(ctx, `SELECT sum(used_count) FROM tutorhub.rate_limit_windows WHERE purpose=$1`, httpapi.AuthRateLimitDeliveryDailyPhone).Scan(&used)
			if err != nil || used != tc.used {
				t.Fatalf("daily slots used = %d, want %d (%v)", used, tc.used, err)
			}
			rows, err := pool.Query(ctx, `SELECT channel,provider,status,coalesce(error_code,''),coalesce(provider_message_ref,'')
				FROM tutorhub.auth_challenge_deliveries WHERE challenge_id=$1 ORDER BY (provider='zns') DESC`, challenge.ChallengeID)
			if err != nil {
				t.Fatal(err)
			}
			audit, err := pgx.CollectRows(rows, pgx.RowToStructByPos[auditRow])
			if err != nil || !slices.Equal(audit, tc.audit) {
				t.Fatal("incorrect delivery count, status, provider, error code or HMAC reference", err)
			}
			if tc.audit[len(tc.audit)-1].Status == "accepted" {
				session, err := account.VerifyPhoneLogin(ctx, challenge.Cookie.Value, identity.EmailCodeVerifyInput{ChallengeID: challenge.ChallengeID, Code: lastCode})
				if err != nil || session.SessionToken == "" {
					t.Fatal("delivered OTP did not establish a session", err)
				}
			}
		})
	}
}
