package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type reauthTestDB struct {
	*passwordSettingsTestDB
	service          *NativeAuthService
	principal        Principal
	aal              int
	payloadVersion   int64
	deadlineObserved bool
	trace            []string
	sends            int
	lastSent         *time.Time
	wrongPurpose     bool
}

type reauthTestTx struct {
	*passwordSettingsTestTx
	reauth *reauthTestDB
}

func (db *reauthTestDB) Begin(ctx context.Context) (pgx.Tx, error) {
	deadline, ok := ctx.Deadline()
	db.deadlineObserved = ok && time.Until(deadline) <= db.service.queryTimeout()
	base, err := db.passwordSettingsTestDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &reauthTestTx{passwordSettingsTestTx: base.(*passwordSettingsTestTx), reauth: db}, nil
}

func (tx *reauthTestTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	db := tx.reauth
	db.trace = append(db.trace, query)
	switch {
	case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM tutorhub.auth_challenges"):
		return foundationTestRow{values: []any{!db.wrongPurpose}}
	case strings.Contains(query, "SELECT count(*),max("):
		return foundationTestRow{values: []any{db.sends, db.lastSent}}
	case strings.Contains(query, "FROM tutorhub.users u WHERE"):
		return foundationTestRow{values: []any{"member@example.test", db.verified}}
	case strings.Contains(query, "FROM tutorhub.sessions WHERE"):
		version := int64(1)
		if db.changedSession {
			version++
		}
		return foundationTestRow{values: []any{db.now, db.now.Add(8 * time.Hour), db.now.Add(30 * 24 * time.Hour), db.persistent, db.authenticatedAt, version, db.aal}}
	case strings.Contains(query, "FROM tutorhub.auth_transactions WHERE"):
		raw, _ := json.Marshal(reauthEmailPayload{Email: "member@example.test", ContextVersion: db.payloadVersion})
		ciphertext, _ := db.service.crypto.Encrypt(string(raw))
		return foundationTestRow{values: []any{uuid.New(), "reauth", "pending", db.now, db.now.Add(AuthTransactionTTL), (*time.Time)(nil), ciphertext}}
	case strings.Contains(query, "FROM tutorhub.auth_challenges WHERE id="):
		id := args[0].(uuid.UUID)
		return foundationTestRow{values: []any{db.service.crypto.Digest("auth-otp-v1", id.String()+":420381"), true, 0, 5, db.now.Add(time.Minute), (*time.Time)(nil), (*time.Time)(nil)}}
	default:
		return tx.passwordSettingsTestTx.QueryRow(ctx, query, args...)
	}
}

func reauthUnitFixture(t *testing.T) (*NativeAuthService, *reauthTestDB, *passwordSettingsTestHasher, Principal) {
	t.Helper()
	service, base, hasher, principal, _ := passwordSettingsUnitFixture(t)
	base.authenticatedAt = base.now.Add(-time.Hour)
	principal.AuthenticatedAt = base.authenticatedAt
	service.config.QueryTimeout = 750 * time.Millisecond
	service.config.EmailCodeAvailable = true
	db := &reauthTestDB{passwordSettingsTestDB: base, service: service, principal: principal, aal: 1, payloadVersion: principal.ContextVersion}
	service.database = db
	service.challenges.database = db
	service.challenges.now = service.now
	return service, db, hasher, principal
}

func TestReauthPasswordRotatesOldSessionWithoutExtendingPersistentExpiry(t *testing.T) {
	service, db, hasher, principal := reauthUnitFixture(t)
	result, err := service.Reauthenticate(context.Background(), principal, "", ReauthenticateInput{Method: "password", Password: "correct"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RecentAuthExpiresAt.Equal(db.now.Add(10*time.Minute)) || !result.CookieExpiresAt.Equal(db.now.Add(30*24*time.Hour)) || len(result.SessionToken) != 43 || len(result.CSRFToken) != 43 || result.SessionToken == result.CSRFToken {
		t.Fatal("reauthentication did not rotate distinct credentials and preserve lifetime")
	}
	if hasher.verifies != 1 || !db.deadlineObserved {
		t.Fatal("password verification or configured timeout was not used")
	}
	tx := db.transactions[len(db.transactions)-1]
	if !tx.committed {
		t.Fatal("rotation did not commit")
	}
	var rotation []any
	for i, query := range tx.writes {
		if strings.Contains(query, "SET reauthenticated_at=") {
			rotation = tx.arguments[i]
			if !strings.Contains(query, "context_version=context_version+1") || !strings.Contains(query, "array_append(auth_methods,$6)") {
				t.Fatal("session context and method were not advanced")
			}
		}
	}
	if len(rotation) != 10 || rotation[5] != "pwd" || rotation[6] != true || rotation[9] != principal.ContextVersion {
		t.Fatal("incorrect rotation inputs")
	}
	for _, arg := range rotation {
		if text, ok := arg.(string); ok && (text == result.SessionToken || text == result.CSRFToken) {
			t.Fatal("raw credentials persisted")
		}
	}
}

func TestReauthRecentExpiryMatchesGreatestAuthenticationTime(t *testing.T) {
	service, db, _, principal := reauthUnitFixture(t)
	db.authenticatedAt = db.now.Add(30 * time.Second)
	result, err := service.Reauthenticate(context.Background(), principal, "", ReauthenticateInput{Method: "password", Password: "correct"})
	if err != nil || !result.RecentAuthExpiresAt.Equal(db.authenticatedAt.Add(nativePasswordRecentAuthTTL)) {
		t.Fatal("response recent-auth expiry differs from GREATEST principal projection")
	}
}

func TestReauthPasswordFailuresAndAdminCap(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*reauthTestDB, *passwordSettingsTestHasher)
		want error
	}{
		{"wrong password", func(db *reauthTestDB, h *passwordSettingsTestHasher) { db.attempts = 4; h.matched = false }, ErrNativeInvalidCurrentPassword},
		{"locked credential", func(db *reauthTestDB, _ *passwordSettingsTestHasher) {
			until := db.now.Add(time.Minute)
			db.lockedUntil = &until
		}, ErrNativeInvalidCurrentPassword},
		{"changed hash", func(db *reauthTestDB, _ *passwordSettingsTestHasher) { db.changedHash = true }, ErrNativeInvalidCurrentPassword},
		{"changed context", func(db *reauthTestDB, _ *passwordSettingsTestHasher) { db.changedSession = true }, ErrSessionContextConflict},
		{"stronger assurance", func(db *reauthTestDB, _ *passwordSettingsTestHasher) { db.aal = 2 }, ErrNativeMethodDisabled},
		{"commit failure", func(db *reauthTestDB, _ *passwordSettingsTestHasher) { db.failAt = "commit" }, ErrAuthFoundationStorage},
		{"administrator", func(db *reauthTestDB, _ *passwordSettingsTestHasher) { db.admin = true }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, db, hasher, principal := reauthUnitFixture(t)
			tc.set(db, hasher)
			result, err := service.Reauthenticate(context.Background(), principal, "", ReauthenticateInput{Method: "password", Password: "fixture"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("unexpected outcome: %v", err)
			}
			if tc.name == "wrong password" {
				tx := db.transactions[len(db.transactions)-1]
				if !tx.committed || len(tx.arguments) != 1 || tx.arguments[0][1] != 5 || tx.arguments[0][3] == nil {
					t.Fatal("failed proof did not persist the shared account lockout")
				}
			}
			if tc.name == "administrator" && !result.CookieExpiresAt.Equal(db.now.Add(8*time.Hour)) {
				t.Fatal("administrator received persistent cookie")
			}
		})
	}
}

func TestReauthEmailProofConsumptionAndSessionGeneration(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		version    int64
		verified   bool
		want       error
	}{
		{"success", "420381", 1, true, nil},
		{"wrong code", "000000", 1, true, ErrNativeInvalidCode},
		{"old generation", "420381", 2, true, ErrNativeInvalidCode},
		{"email unverified", "420381", 1, false, ErrNativeInvalidCode},
		{"wrong purpose", "420381", 1, true, ErrNativeInvalidCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, db, _, principal := reauthUnitFixture(t)
			db.payloadVersion, db.verified = tc.version, tc.verified
			db.wrongPurpose = tc.name == "wrong purpose"
			_, err := service.Reauthenticate(context.Background(), principal, strings.Repeat("b", 43), ReauthenticateInput{Method: "email_code", ChallengeID: uuid.New(), Code: tc.code})
			if !errors.Is(err, tc.want) {
				t.Fatalf("unexpected email outcome: %v", err)
			}
			tx := db.transactions[len(db.transactions)-1]
			if (tc.want == nil || tc.name == "wrong code") && !tx.committed {
				t.Fatal("successful consumption or failed-attempt accounting not committed")
			}
			if (tc.name == "old generation" || !tc.verified) && tx.committed {
				t.Fatal("ineligible proof was committed")
			}
		})
	}
}

func TestReauthInputBoundariesAndRedaction(t *testing.T) {
	service, db, _, principal := reauthUnitFixture(t)
	for _, method := range []string{"", "oidc", "totp"} {
		if _, err := service.Reauthenticate(context.Background(), principal, "", ReauthenticateInput{Method: method}); !errors.Is(err, ErrAuthFoundationInput) {
			t.Fatal("unknown proof method accepted")
		}
	}
	service.config.EmailCodeAvailable = false
	if _, err := service.StartReauthEmailCode(context.Background(), principal, "", nil); !errors.Is(err, ErrNativeMethodDisabled) {
		t.Fatal("disabled email sender accepted")
	}
	if _, err := service.Reauthenticate(context.Background(), principal, "", ReauthenticateInput{Method: "email_code"}); !errors.Is(err, ErrNativeMethodDisabled) {
		t.Fatal("disabled email proof accepted")
	}
	if len(db.transactions) != 0 {
		t.Fatal("invalid or disabled method used database")
	}
	for _, value := range []any{ReauthenticateInput{Password: "private-password", Code: "420381"}, ReauthenticateResult{SessionToken: "secret-token", CSRFToken: "secret-csrf"}} {
		for _, rendered := range []string{fmt.Sprint(value), fmt.Sprintf("%+v", value), fmt.Sprint(value.(interface{ String() string }).String())} {
			if strings.Contains(rendered, "private-password") || strings.Contains(rendered, "420381") || strings.Contains(rendered, "secret-token") || strings.Contains(rendered, "secret-csrf") {
				t.Fatal("reauthentication value disclosed proof or cookie")
			}
		}
	}
}

func TestReauthStartUsesVerifiedServerEmailAndPostCommitDelivery(t *testing.T) {
	service, db, hasher, principal := reauthUnitFixture(t)
	service.challenges.code = func() (string, error) { return "420381", nil }
	allowedCalled := false
	result, err := service.StartReauthEmailCode(context.Background(), principal, "", func(_ context.Context, email string) (bool, error) {
		allowedCalled = true
		if email != "member@example.test" || hasher.assertNoConnection() != nil {
			t.Fatal("destination limit did not use own email outside database connection")
		}
		return true, nil
	})
	if err != nil || !allowedCalled || result.Cookie == nil || result.Cookie.Value == "" || result.CooldownSeconds != 60 || result.ExpiresInSeconds != 600 {
		t.Fatalf("unexpected email start: %v", err)
	}
	if len(db.transactions) != 3 || !db.transactions[1].committed || !db.transactions[2].committed {
		t.Fatal("challenge must commit before the delivery recorder begins")
	}
	var transactionBound, challengeCreated bool
	for i, query := range db.transactions[1].writes {
		args := db.transactions[1].arguments[i]
		if strings.Contains(query, "INSERT INTO tutorhub.auth_transactions") {
			transactionBound = args[2] == principal.User.ID && args[3] == principal.SessionID
		}
		if strings.Contains(query, "INSERT INTO tutorhub.auth_challenges") {
			challengeCreated = args[2] == "reauth" && args[6] == true
		}
		for _, arg := range args {
			if value, ok := arg.(string); ok && (value == "member@example.test" || value == "420381" || value == result.Cookie.Value) {
				t.Fatal("raw destination, code or transaction binding persisted")
			}
		}
	}
	if !transactionBound || !challengeCreated {
		t.Fatal("reauth transaction or challenge not scoped to this user and session")
	}
}

func TestReauthStartEnforcesCooldownSendCapAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*reauthTestDB)
		want error
	}{
		{"cooldown", func(db *reauthTestDB) { db.sends = 1; sent := db.now.Add(-30 * time.Second); db.lastSent = &sent }, ErrAuthChallengeThrottled},
		{"send cap", func(db *reauthTestDB) { db.sends = MaxChallengeSends }, ErrAuthChallengeThrottled},
		{"unverified", func(db *reauthTestDB) { db.verified = false }, ErrNativeVerifiedEmailRequired},
		{"commit failure", func(db *reauthTestDB) { db.failAt = "commit" }, ErrAuthFoundationStorage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, db, _, principal := reauthUnitFixture(t)
			tc.set(db)
			_, err := service.StartReauthEmailCode(context.Background(), principal, "", func(context.Context, string) (bool, error) { return true, nil })
			if !errors.Is(err, tc.want) {
				t.Fatalf("unexpected start error: %v", err)
			}
			for _, tx := range db.transactions {
				if tx.committed {
					t.Fatal("failed start committed or dispatched delivery")
				}
			}
		})
	}
}
