package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type emailAuthUnitDB struct {
	crypto                                              *Crypto
	payload                                             nativeEmailPayload
	kind                                                string
	now                                                 time.Time
	id, challengeID, identityID                         uuid.UUID
	transactions                                        []*emailAuthUnitTx
	active, deliverable, wrongCode, admin, wrongPurpose bool
	failAt                                              string
	sends                                               int
	lastSent                                            *time.Time
}
type emailAuthUnitTx struct {
	pgx.Tx
	db                *emailAuthUnitDB
	closed, committed bool
	writes            []string
}

func (db *emailAuthUnitDB) Begin(context.Context) (pgx.Tx, error) {
	tx := &emailAuthUnitTx{db: db}
	db.transactions = append(db.transactions, tx)
	return tx, nil
}
func (tx *emailAuthUnitTx) Commit(context.Context) error {
	tx.closed = true
	if tx.db.failAt == "commit" {
		return errors.New("private commit detail")
	}
	tx.committed = true
	return nil
}
func (tx *emailAuthUnitTx) Rollback(context.Context) error { tx.closed = true; return nil }
func (tx *emailAuthUnitTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.writes = append(tx.writes, sql)
	if tx.db.failAt != "" && strings.Contains(sql, tx.db.failAt) {
		return pgconn.CommandTag{}, errors.New("private storage detail")
	}
	if strings.Contains(sql, "INSERT INTO tutorhub.auth_challenges") {
		tx.db.deliverable = args[6].(bool)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *emailAuthUnitTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	db := tx.db
	if db.failAt != "" && strings.Contains(sql, db.failAt) {
		return foundationTestRow{err: errors.New("private storage detail")}
	}
	switch {
	case strings.HasPrefix(sql, "SELECT id,kind,state"):
		return foundationTestRow{values: []any{db.id, db.kind, "pending", db.now, db.now.Add(AuthTransactionTTL), (*time.Time)(nil)}}
	case strings.HasPrefix(sql, "SELECT payload_ciphertext"):
		raw, _ := json.Marshal(db.payload)
		ciphertext, _ := db.crypto.Encrypt(string(raw))
		return foundationTestRow{values: []any{ciphertext}}
	case strings.HasPrefix(sql, "SELECT display_name"):
		if !db.active || db.payload.UserID == uuid.Nil {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{"Existing Person"}}
	case strings.HasPrefix(sql, "SELECT id FROM tutorhub.users"):
		if !db.active {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{db.payload.UserID}}
	case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM tutorhub.auth_challenges"):
		return foundationTestRow{values: []any{!db.wrongPurpose}}
	case strings.HasPrefix(sql, "SELECT code_hash"):
		code := "420381"
		if db.wrongCode {
			code = "000000"
		}
		return foundationTestRow{values: []any{db.crypto.Digest("auth-otp-v1", db.challengeID.String()+":"+code), db.deliverable, 0, 5, db.now.Add(EmailChallengeTTL), (*time.Time)(nil), (*time.Time)(nil)}}
	case strings.HasPrefix(sql, "INSERT INTO tutorhub.identities"):
		tx.writes = append(tx.writes, sql)
		return foundationTestRow{values: []any{db.identityID}}
	case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM tutorhub.memberships"):
		return foundationTestRow{values: []any{db.admin}}
	case strings.HasPrefix(sql, "SELECT m.tenant_id"):
		return foundationTestRow{err: pgx.ErrNoRows}
	case strings.HasPrefix(sql, "SELECT count(*),max(created_at)"):
		return foundationTestRow{values: []any{db.sends, db.lastSent}}
	default:
		return foundationTestRow{err: errors.New("unexpected email auth fixture query")}
	}
}

type emailAuthPasswordSpy struct {
	db      *emailAuthUnitDB
	calls   int
	failure error
}

func (spy *emailAuthPasswordSpy) ValidateAndHash(context.Context, string, string, string) (string, error) {
	spy.calls++
	for _, tx := range spy.db.transactions {
		if !tx.closed {
			return "", errors.New("hash ran with an open database transaction")
		}
	}
	return dummyPasswordPHC, spy.failure
}
func (*emailAuthPasswordSpy) Verify(context.Context, string, string) (bool, bool, error) {
	return false, false, nil
}
func (*emailAuthPasswordSpy) Hash(context.Context, string) (string, error) {
	return dummyPasswordPHC, nil
}
func (*emailAuthPasswordSpy) DummyVerify(context.Context, string) error { return nil }

func emailAuthUnitFixture(t *testing.T, kind string) (*NativeAuthService, *emailAuthUnitDB, *emailAuthPasswordSpy) {
	t.Helper()
	crypto, err := NewCrypto(bytes.Repeat([]byte{27}, 32))
	if err != nil {
		t.Fatal(err)
	}
	purpose := "login_code"
	if kind == "recovery" {
		purpose = "password_recovery"
	}
	db := &emailAuthUnitDB{crypto: crypto, kind: kind, now: time.Now().UTC(), id: uuid.New(), challengeID: uuid.New(), identityID: uuid.New(), active: true, deliverable: true, payload: nativeEmailPayload{Email: "existing@example.test", UserID: uuid.New(), Purpose: purpose}}
	spy := &emailAuthPasswordSpy{db: db}
	service, err := NewNativeAuthService(db, crypto, spy, nil, NativeAuthConfig{CookieSecure: true, LegalTermsVersion: "v1", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 48 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return db.now }
	service.challenges.now = service.now
	service.challenges.code = func() (string, error) { return "420381", nil }
	return service, db, spy
}

func TestNativeEmailLoginAtomicCompletionAndFailureRollback(t *testing.T) {
	for _, failure := range []string{"", "INSERT INTO tutorhub.identities", "UPDATE tutorhub.users", "INSERT INTO tutorhub.sessions", "UPDATE tutorhub.auth_transactions", "commit"} {
		t.Run(failure, func(t *testing.T) {
			service, db, _ := emailAuthUnitFixture(t, "login")
			db.failAt = failure
			result, err := service.VerifyEmailCodeLogin(context.Background(), strings.Repeat("a", 43), EmailCodeVerifyInput{ChallengeID: db.challengeID, Code: "420381", KeepSignedIn: true})
			if len(db.transactions) != 1 {
				t.Fatal("login must use one transaction")
			}
			if failure != "" {
				if !errors.Is(err, ErrAuthFoundationStorage) || db.transactions[0].committed || result.SessionToken != "" {
					t.Fatal("failed login committed or leaked result")
				}
				return
			}
			if err != nil || !db.transactions[0].committed || !result.Persistent || result.AbsoluteExpiresAt.Sub(db.now) != 30*24*time.Hour {
				t.Fatal("successful email login did not atomically create persistent session")
			}
		})
	}
	service, db, _ := emailAuthUnitFixture(t, "login")
	db.admin = true
	result, err := service.VerifyEmailCodeLogin(context.Background(), strings.Repeat("a", 43), EmailCodeVerifyInput{ChallengeID: db.challengeID, Code: "420381", KeepSignedIn: true})
	if err != nil || result.Persistent || result.AbsoluteExpiresAt.Sub(db.now) != 24*time.Hour {
		t.Fatal("email login ignored administrator cap")
	}
}

func TestNativeEmailInvalidProofNeverCreatesSession(t *testing.T) {
	for _, scenario := range []string{"wrong_code", "dummy", "inactive", "cross_purpose", "wrong_kind"} {
		t.Run(scenario, func(t *testing.T) {
			service, db, _ := emailAuthUnitFixture(t, "login")
			switch scenario {
			case "wrong_code":
				db.wrongCode = true
			case "dummy":
				db.payload.UserID = uuid.Nil
				db.deliverable = false
			case "inactive":
				db.active = false
			case "cross_purpose":
				db.wrongPurpose = true
			case "wrong_kind":
				db.kind = "recovery"
			}
			result, err := service.VerifyEmailCodeLogin(context.Background(), strings.Repeat("a", 43), EmailCodeVerifyInput{ChallengeID: db.challengeID, Code: "420381"})
			if !errors.Is(err, ErrNativeInvalidCode) || result.SessionToken != "" {
				t.Fatal("invalid proof was accepted")
			}
			for _, tx := range db.transactions {
				for _, sql := range tx.writes {
					if strings.Contains(sql, "INSERT INTO tutorhub.sessions") || strings.Contains(sql, "INSERT INTO tutorhub.identities") {
						t.Fatal("invalid proof mutated account")
					}
				}
			}
			if scenario == "wrong_code" && (!db.transactions[0].committed || len(db.transactions[0].writes) != 1) {
				t.Fatal("wrong code must persist attempts only")
			}
		})
	}
}

func TestNativeRecoveryHashesOutsideTransactionAndRollsBackEveryWrite(t *testing.T) {
	for _, failure := range []string{"", "INSERT INTO tutorhub.identities", "UPDATE tutorhub.users", "INSERT INTO tutorhub.user_password_credentials", "UPDATE tutorhub.sessions", "UPDATE tutorhub.auth_transactions", "commit"} {
		t.Run(failure, func(t *testing.T) {
			service, db, spy := emailAuthUnitFixture(t, "recovery")
			db.failAt = failure
			// A bounded queue without a worker lets the test observe post-commit notice admission.
			dispatcher := &DeliveryDispatcher{queue: make(chan deliveryJob, 1)}
			service.challenges.dispatcher = dispatcher
			err := service.CompletePasswordRecovery(context.Background(), strings.Repeat("a", 43), PasswordRecoveryCompleteInput{ChallengeID: db.challengeID, Code: "420381", NewPassword: "Strong replacement 42!"})
			if spy.calls != 1 || len(db.transactions) != 2 || !db.transactions[0].closed {
				t.Fatal("recovery must hash after closing snapshot")
			}
			if failure != "" {
				if !errors.Is(err, ErrAuthFoundationStorage) || db.transactions[1].committed || len(dispatcher.queue) != 0 {
					t.Fatal("partial recovery or pre-commit notice escaped")
				}
				return
			}
			if err != nil || !db.transactions[1].committed || len(dispatcher.queue) != 1 {
				t.Fatal("recovery did not commit before notice")
			}
			job := <-dispatcher.queue
			if job.notice == nil || job.notice.Kind != "password_changed" || job.message.Code != "" {
				t.Fatal("security notice missing or includes OTP")
			}
			writes := strings.Join(db.transactions[1].writes, "\n")
			if !strings.Contains(writes, "revoked_reason='password_reset'") || strings.Contains(writes, "INSERT INTO tutorhub.sessions") {
				t.Fatal("recovery must revoke every session without auto-login")
			}
		})
	}
	service, db, spy := emailAuthUnitFixture(t, "recovery")
	spy.failure = ErrPasswordPolicy
	err := service.CompletePasswordRecovery(context.Background(), strings.Repeat("a", 43), PasswordRecoveryCompleteInput{ChallengeID: db.challengeID, Code: "420381", NewPassword: "weak"})
	if !errors.Is(err, ErrPasswordPolicy) || len(db.transactions) != 1 || len(db.transactions[0].writes) != 0 {
		t.Fatal("password policy failure consumed OTP attempt")
	}
}

func TestNativeEmailStartDummyAndDestinationThrottleShareResponse(t *testing.T) {
	for _, kind := range []string{"login", "recovery"} {
		for _, scenario := range []string{"real", "dummy", "throttled"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				service, db, _ := emailAuthUnitFixture(t, kind)
				db.active = scenario != "dummy"
				start := service.StartEmailCodeLogin
				if kind == "recovery" {
					start = service.StartPasswordRecovery
				}
				result, err := start(context.Background(), SignupStartInput{Email: "EXISTING@example.test", DestinationAllowed: scenario != "throttled"})
				if err != nil || result.Cookie == nil || result.CooldownSeconds != 60 || result.ExpiresInSeconds != 600 || result.ChallengeID == uuid.Nil {
					t.Fatal("start response differs")
				}
				if db.deliverable != (scenario == "real") {
					t.Fatal("dummy or throttled destination was deliverable")
				}
			})
		}
	}
}

func TestNativeEmailResendFlagsPurposeCooldownAndImmutableDestination(t *testing.T) {
	for _, kind := range []string{"login", "recovery"} {
		service, _, _ := emailAuthUnitFixture(t, kind)
		if _, err := service.ResendEmailChallenge(context.Background(), strings.Repeat("a", 43), AuthMethodFlags{}, nil); !errors.Is(err, ErrNativeMethodDisabled) {
			t.Fatal("disabled method accepted resend")
		}
	}
	service, db, _ := emailAuthUnitFixture(t, "recovery")
	flags := AuthMethodFlags{EmailCodeEnabled: true, PasswordEnabled: true}
	var destination string
	result, err := service.ResendEmailChallenge(context.Background(), strings.Repeat("a", 43), flags, func(_ context.Context, email string) (bool, error) { destination = email; return false, nil })
	if err != nil || result.ChallengeID == uuid.Nil || destination != db.payload.Email || db.deliverable {
		t.Fatal("resend changed destination or exposed throttling")
	}
	for _, sends := range []int{1, 5} {
		service, db, _ = emailAuthUnitFixture(t, "login")
		db.sends = sends
		db.lastSent = &db.now
		if _, err := service.ResendEmailChallenge(context.Background(), strings.Repeat("a", 43), AuthMethodFlags{EmailCodeEnabled: true}, nil); !errors.Is(err, ErrAuthChallengeThrottled) {
			t.Fatal("resend did not enforce cooldown/send limit")
		}
	}
}

func TestNativeRecoveryKeepsDisplayNamePolicyPrivateUntilOTPProof(t *testing.T) {
	for _, scenario := range []string{"wrong code for existing account", "dummy account", "valid owner proof"} {
		t.Run(scenario, func(t *testing.T) {
			service, db, _ := emailAuthUnitFixture(t, "recovery")
			db.payload.Email = "member@example.test"
			passwords, err := NewPasswordService(PasswordOptions{BreachMode: "offline"})
			if err != nil {
				t.Fatal(err)
			}
			service.passwords = passwords
			input := PasswordRecoveryCompleteInput{ChallengeID: db.challengeID, Code: "000001", NewPassword: "Existing Person forest 42!"}
			want := ErrNativeInvalidCode
			if scenario == "dummy account" {
				db.payload.UserID = uuid.Nil
				db.deliverable = false
			} else if scenario == "valid owner proof" {
				input.Code = "420381"
				want = ErrPasswordPolicy
			}
			err = service.CompletePasswordRecovery(context.Background(), strings.Repeat("a", 43), input)
			if !errors.Is(err, want) {
				t.Fatalf("unexpected recovery outcome: %v", err)
			}
			if scenario == "valid owner proof" && (len(db.transactions) != 1 || len(db.transactions[0].writes) != 0) {
				t.Fatal("name-based policy rejection consumed an OTP attempt")
			}
			for _, tx := range db.transactions {
				for _, sql := range tx.writes {
					if strings.Contains(sql, "INSERT INTO tutorhub.user_password_credentials") {
						t.Fatal("invalid recovery changed a credential")
					}
				}
			}
		})
	}
}
