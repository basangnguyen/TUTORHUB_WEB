package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Drive the full first-factor method; the remaining row-lock and passkey UV
// properties are exercised against PostgreSQL and signed WebAuthn assertions.
type phoneLoginTestDB struct {
	pgx.Tx
	t                                       *testing.T
	now, last                               time.Time
	factor                                  uuid.NullUUID
	registered, usable, alternative, active bool
	user, ident, phoneTx, challenge         uuid.UUID
	code, binding                           string
	crypto                                  *Crypto
	sealed, continuation                    []byte
	attempts, commits                       int
	completed, consumed, session, used      bool
}

func (db *phoneLoginTestDB) Begin(context.Context) (pgx.Tx, error) { return db, nil }
func (db *phoneLoginTestDB) Commit(context.Context) error          { db.commits++; return nil }
func (db *phoneLoginTestDB) Rollback(context.Context) error        { return nil }
func (db *phoneLoginTestDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	var vals []any
	switch {
	case strings.HasPrefix(query, "SELECT id,created_at,expires_at,user_id,session_id,payload_ciphertext"):
		if db.completed || !slices.Equal(args[0].([]byte), db.crypto.Digest("auth-tx-binding", db.binding)) {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		vals = []any{db.phoneTx, db.now.Add(-time.Minute), db.now.Add(5 * time.Minute), uuid.NullUUID{UUID: db.user, Valid: true}, uuid.NullUUID{}, db.sealed}
	case strings.HasPrefix(query, "SELECT EXISTS(SELECT FROM tutorhub.auth_challenges"):
		vals = []any{args[0] == db.challenge && args[1] == db.phoneTx && args[2] == "phone_login"}
	case strings.HasPrefix(query, "SELECT code_hash,deliverable"):
		vals = []any{db.crypto.Digest("auth-otp-v1", db.challenge.String()+":"+db.code), true, db.attempts, 5, db.now.Add(time.Minute), (*time.Time)(nil), (*time.Time)(nil)}
	case strings.HasPrefix(query, "SELECT GREATEST(n.verified_at,n.last_used_at),"):
		if args[0] != db.user || args[1] != db.ident || !db.active {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		vals = []any{db.last, db.alternative, db.factor.Valid, db.registered, db.usable}
	case strings.HasPrefix(query, "SELECT (SELECT id FROM tutorhub.mfa_totp_factors"):
		vals = []any{db.factor, db.registered, db.usable}
	default:
		db.t.Fatalf("unexpected phone login query %s", query)
	}
	return foundationTestRow{values: vals}
}
func (db *phoneLoginTestDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT id FROM tutorhub.users"):
		return pgconn.NewCommandTag("SELECT 1"), nil
	case strings.HasPrefix(sql, "UPDATE tutorhub.auth_challenges SET attempts"):
		db.attempts = args[1].(int)
		db.consumed = args[2].(*time.Time) != nil
	case strings.HasPrefix(sql, "UPDATE tutorhub.auth_transactions SET state='completed'"):
		db.completed = true
	case strings.HasPrefix(sql, "INSERT INTO tutorhub.auth_transactions") && strings.Contains(sql, "'mfa'"):
		db.continuation = args[5].([]byte)
	case strings.HasPrefix(sql, "UPDATE tutorhub.user_phone_numbers"):
		db.used = true
	case strings.HasPrefix(sql, "INSERT INTO tutorhub.sessions"):
		db.session = true
	default:
		db.t.Fatalf("unexpected phone login write %s", sql)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func newPhoneLoginTest(t *testing.T) (*NativeAuthService, *phoneLoginTestDB, EmailCodeVerifyInput) {
	t.Helper()
	key := make([]byte, 32)
	crypto, err := NewCrypto(key)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	db := &phoneLoginTestDB{t: t, crypto: crypto, now: now, last: now.Add(-24 * time.Hour), active: true, alternative: true,
		user: uuid.New(), ident: uuid.New(), phoneTx: uuid.New(), challenge: uuid.New(), binding: strings.Repeat("b", 43), code: "654321"}
	raw, err := json.Marshal(phonePayload{Number: NormalizedPhoneNumber{E164: "+84912345678"}, Purpose: "phone_login", TransactionID: db.phoneTx, UserID: db.user, IdentityID: db.ident, KeepSignedIn: true})
	if err != nil {
		t.Fatal(err)
	}
	db.sealed, err = crypto.Encrypt(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	s := &NativeAuthService{database: db, crypto: crypto, now: func() time.Time { return now }, webauthn: &PasskeyProtocol{}, config: NativeAuthConfig{PhoneEnabled: true, PhoneStaleAfter: 60 * 24 * time.Hour, MFATOTPEnabled: true}}
	s.challenges = NewChallengeService(db, crypto, nil)
	s.challenges.now = s.now
	return s, db, EmailCodeVerifyInput{ChallengeID: db.challenge, Code: db.code}
}

func TestPhoneLoginRequiresUsableEnrolledFactor(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		totp, passkey, usable, protocol, mfa bool
		methods                              []string
	}{
		{"passkey only", false, true, true, true, true, []string{"passkey"}},
		{"totp only", true, false, false, false, true, []string{"totp", "recovery_code"}},
		{"quarantined passkey", false, true, false, true, true, nil},
		{"passkey protocol off", false, true, true, false, true, nil},
		{"passkey with MFA routes off", false, true, true, true, false, nil},
		{"totp with MFA routes off", true, false, false, false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, input := newPhoneLoginTest(t)
			db.registered, db.usable = tc.passkey, tc.usable
			if tc.totp {
				db.factor = uuid.NullUUID{UUID: uuid.New(), Valid: true}
			}
			if !tc.protocol {
				s.webauthn = nil
			}
			s.config.MFATOTPEnabled = tc.mfa
			got, err := s.VerifyPhoneLogin(context.Background(), db.binding, input)
			if !db.completed || !db.consumed || db.commits != 1 || db.used || db.session || got.SessionToken != "" {
				t.Fatal("phone proof was not consumed independently of final session")
			}
			if len(tc.methods) == 0 {
				if !errors.Is(err, ErrPhoneSecondFactorRequired) || got.MFA != nil || db.continuation != nil {
					t.Fatal("unusable enrolled factor allowed login", err)
				}
			} else {
				if err != nil || got.MFA == nil || !slices.Equal(got.MFA.Methods, tc.methods) || got.MFA.Cookie == nil || got.MFA.Cookie.Value == db.binding || got.MFA.Cookie.MaxAge != 300 {
					t.Fatal("invalid MFA challenge", err)
				}
				plain, err := s.crypto.Decrypt(db.continuation)
				var proof mfaPayload
				if err != nil || json.Unmarshal([]byte(plain), &proof) != nil || proof.IdentityID != db.ident || proof.UserID != db.user || !slices.Equal(proof.Methods, []string{"phone_otp"}) || !proof.KeepSignedIn {
					t.Fatal("lost phone first factor")
				}
			}
			if _, err = s.VerifyPhoneLogin(context.Background(), db.binding, input); !errors.Is(err, ErrNativeInvalidCode) {
				t.Fatal("phone proof replay", err)
			}
		})
	}
}

func TestPhoneLoginStaleReboundAndWrongOTP(t *testing.T) {
	for _, scenario := range []string{"stale", "reassigned", "wrong code"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, input := newPhoneLoginTest(t)
			want := ErrNativeInvalidCode
			switch scenario {
			case "stale":
				db.last = db.now.Add(-s.config.PhoneStaleAfter - time.Second)
				want = ErrPhoneStale
			case "reassigned":
				db.active = false
			case "wrong code":
				input.Code = "000000"
			}
			_, err := s.VerifyPhoneLogin(context.Background(), db.binding, input)
			if !errors.Is(err, want) || db.commits != 1 || db.session || db.used || db.continuation != nil {
				t.Fatal("phone login decision", err)
			}
			if scenario == "wrong code" {
				if db.completed || db.consumed || db.attempts != 1 {
					t.Fatal("bad guess must spend exactly one attempt")
				}
			} else if !db.completed || !db.consumed {
				t.Fatal("matched OTP wasn't consumed")
			}
		})
	}
}

func TestPhoneManagementBindingThresholdsAndRedaction(t *testing.T) {
	p := Principal{User: User{ID: uuid.New()}, SessionID: uuid.New(), ContextVersion: 2}
	proof := phonePayload{Purpose: "phone_verify", UserID: p.User.ID, SessionID: p.SessionID, ContextVersion: 2, Number: NormalizedPhoneNumber{E164: "+84912345678"}}
	if err := matchPhoneSession(proof, &p); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(matchPhoneSession(proof, nil), ErrInvalidCSRFToken) {
		t.Fatal("missing authenticated resend")
	}
	p.ContextVersion++
	if !errors.Is(matchPhoneSession(proof, &p), ErrNativeInvalidCode) {
		t.Fatal("stale generation")
	}
	now := time.Now()
	for _, window := range []time.Duration{60 * 24 * time.Hour, 90 * 24 * time.Hour} {
		if phonePastThreshold(now, now.Add(-window), window) || !phonePastThreshold(now, now.Add(-window-time.Nanosecond), window) {
			t.Fatal("threshold must be strict")
		}
	}
	input := PhoneStartInput{PhoneNumber: proof.Number.E164}
	for _, repr := range []string{proof.String(), proof.LogValue().String(), fmt.Sprint(input), input.LogValue().String()} {
		if strings.Contains(repr, "912345678") || !strings.Contains(repr, "redacted") {
			t.Fatal("phone input logged")
		}
	}
}

func TestPhoneRemainingMethodUsesMFAAndStalePolicy(t *testing.T) {
	now := time.Now()
	flags := loginMethodAvailability{mfa: true, passkeys: true, staleAfter: 60 * 24 * time.Hour}
	for _, tc := range []struct {
		name string
		s    phoneLoginState
		want bool
	}{
		{"fresh without enrollment", phoneLoginState{lastUse: now}, true},
		{"stale requires verified email", phoneLoginState{lastUse: now.Add(-61 * 24 * time.Hour), otherIdentity: true}, false},
		{"no alternate can still sign in", phoneLoginState{lastUse: now.Add(-61 * 24 * time.Hour)}, true},
		{"stale fails eventual MFA", phoneLoginState{lastUse: now.Add(-61 * 24 * time.Hour), totp: true}, false},
		{"only quarantined passkey", phoneLoginState{lastUse: now, passkey: true}, false},
		{"usable passkey", phoneLoginState{lastUse: now, passkey: true, usablePasskey: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.s.usableAfterRemoval(now, flags) != tc.want {
				t.Fatal("unexpected phone availability")
			}
		})
	}
	flags.mfa = false
	if (phoneLoginState{lastUse: now, totp: true}).usableAfterRemoval(now, flags) {
		t.Fatal("disabled MFA routes counted as an available login")
	}
}
