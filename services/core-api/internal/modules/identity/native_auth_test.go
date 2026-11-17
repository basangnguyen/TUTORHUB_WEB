package identity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

type nativePasswordSpy struct {
	db                        *nativeUnitDB
	hashes, dummies, verifies int
	matched                   bool
	busy                      bool
}

func (spy *nativePasswordSpy) ValidateAndHash(context.Context, string, string, string) (string, error) {
	spy.hashes++
	for _, tx := range spy.db.transactions {
		if !tx.closed {
			return "", errors.New("password hashing held database connection")
		}
	}
	if spy.busy {
		return "", ErrPasswordBusy
	}
	return dummyPasswordPHC, nil
}
func (spy *nativePasswordSpy) Hash(context.Context, string) (string, error) {
	return dummyPasswordPHC, nil
}
func (spy *nativePasswordSpy) Verify(context.Context, string, string) (bool, bool, error) {
	spy.verifies++
	return spy.matched, false, nil
}
func (spy *nativePasswordSpy) DummyVerify(context.Context, string) error {
	spy.dummies++
	if spy.busy {
		return ErrPasswordBusy
	}
	return nil
}

type nativeUnitDB struct {
	transactions                        []*nativeUnitTx
	crypto                              *Crypto
	ciphertext                          []byte
	id, challengeID, userID, identityID uuid.UUID
	now                                 time.Time
	failAt                              string
	existing                            bool
	locked                              bool
	changedHash                         bool
	wrongCode                           bool
}
type nativeUnitTx struct {
	pgx.Tx
	db                *nativeUnitDB
	closed, committed bool
	writes            []string
}

func (db *nativeUnitDB) Begin(context.Context) (pgx.Tx, error) {
	tx := &nativeUnitTx{db: db}
	db.transactions = append(db.transactions, tx)
	return tx, nil
}
func (tx *nativeUnitTx) Commit(context.Context) error {
	tx.closed = true
	if tx.db.failAt == "commit" {
		return errors.New("fixture commit failure")
	}
	tx.committed = true
	return nil
}
func (tx *nativeUnitTx) Rollback(context.Context) error { tx.closed = true; return nil }
func (tx *nativeUnitTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.writes = append(tx.writes, sql)
	if tx.db.failAt != "" && strings.Contains(sql, tx.db.failAt) {
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "23514", Detail: "private@example.test secret-password 420381"}
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *nativeUnitTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	db := tx.db
	switch {
	case strings.HasPrefix(sql, "SELECT id,kind,state"):
		return foundationTestRow{values: []any{db.id, "signup_email", "pending", db.now, db.now.Add(15 * time.Minute), (*time.Time)(nil)}}
	case strings.HasPrefix(sql, "SELECT payload_ciphertext"):
		return foundationTestRow{values: []any{db.ciphertext, db.crypto.Digest("auth-ip-v1", "127.0.0.0/24")}}
	case strings.HasPrefix(sql, "SELECT code_hash"):
		code := "420381"
		if db.wrongCode {
			code = "000000"
		}
		return foundationTestRow{values: []any{db.crypto.Digest("auth-otp-v1", db.challengeID.String()+":"+code), true, 0, 5, db.now.Add(10 * time.Minute), (*time.Time)(nil), (*time.Time)(nil)}}
	case strings.HasPrefix(sql, "SELECT m.tenant_id"):
		return foundationTestRow{err: pgx.ErrNoRows}
	case strings.HasPrefix(sql, "SELECT u.id,u.status"):
		if !db.existing {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		hash, status := dummyPasswordPHC, "active"
		var locked *time.Time
		if db.locked {
			until := db.now.Add(time.Hour)
			locked = &until
		}
		return foundationTestRow{values: []any{db.userID, "active", &db.now, &hash, &status, locked, uuid.NullUUID{UUID: db.identityID, Valid: true}}}
	case strings.HasPrefix(sql, "SELECT status,email_verified_at"):
		return foundationTestRow{values: []any{"active", &db.now}}
	case strings.HasPrefix(sql, "SELECT password_hash,status"):
		hash := dummyPasswordPHC
		if db.changedHash {
			hash += "changed"
		}
		return foundationTestRow{values: []any{&hash, "active", 4, (*time.Time)(nil)}}
	case strings.HasPrefix(sql, "SELECT id FROM tutorhub.identities"):
		return foundationTestRow{values: []any{db.identityID}}
	case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM tutorhub.memberships"):
		return foundationTestRow{values: []any{false}}
	default:
		return foundationTestRow{err: errors.New("unexpected native fixture query")}
	}
}

func nativeUnitFixture(t *testing.T) (*NativeAuthService, *nativeUnitDB, *nativePasswordSpy) {
	t.Helper()
	crypto, err := NewCrypto(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := crypto.Encrypt(`{"email":"private@example.test","existing":false}`)
	if err != nil {
		t.Fatal(err)
	}
	db := &nativeUnitDB{crypto: crypto, ciphertext: ciphertext, id: uuid.New(), challengeID: uuid.New(), userID: uuid.New(), identityID: uuid.New(), now: time.Now().UTC()}
	spy := &nativePasswordSpy{db: db, matched: true}
	service, err := NewNativeAuthService(db, crypto, spy, nil, NativeAuthConfig{CookieSecure: true, LegalTermsVersion: "2026-11-16-v2.0", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 48 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return service, db, spy
}

func nativeCompleteFixture(db *nativeUnitDB) SignupCompleteInput {
	return SignupCompleteInput{ChallengeID: db.challengeID, Code: "420381", DisplayName: "Test Person", Password: "secret-password", TermsVersion: "2026-11-16-v2.0", KeepSignedIn: true}
}

func TestNativeSignupCompletionHasOneAtomicWriteTransaction(t *testing.T) {
	service, db, spy := nativeUnitFixture(t)
	result, err := service.CompleteEmailSignup(context.Background(), strings.Repeat("a", 43), nativeCompleteFixture(db))
	if err != nil {
		t.Fatal(err)
	}
	if spy.hashes != 1 || len(db.transactions) != 2 || db.transactions[0].committed || !db.transactions[1].committed {
		t.Fatal("completion must hash outside locks and commit once")
	}
	writes := strings.Join(db.transactions[1].writes, "\n")
	for _, required := range []string{"UPDATE tutorhub.auth_challenges", "INSERT INTO tutorhub.users", "INSERT INTO tutorhub.identities", "INSERT INTO tutorhub.user_password_credentials", "INSERT INTO tutorhub.user_legal_acceptances", "INSERT INTO tutorhub.sessions", "UPDATE tutorhub.auth_transactions"} {
		if !strings.Contains(writes, required) {
			t.Fatalf("atomic completion missing %s", required)
		}
	}
	if !result.Persistent || result.SessionToken == result.CSRFToken || len(result.SessionToken) != 43 {
		t.Fatal("fresh opaque native session missing")
	}
}

func TestNativeSignupEveryWriteFailureRollsBack(t *testing.T) {
	for _, failure := range []string{"INSERT INTO tutorhub.users", "INSERT INTO tutorhub.identities", "INSERT INTO tutorhub.user_password_credentials", "INSERT INTO tutorhub.user_legal_acceptances", "INSERT INTO tutorhub.sessions", "UPDATE tutorhub.auth_transactions", "commit"} {
		t.Run(failure, func(t *testing.T) {
			service, db, _ := nativeUnitFixture(t)
			db.failAt = failure
			result, err := service.CompleteEmailSignup(context.Background(), strings.Repeat("a", 43), nativeCompleteFixture(db))
			if !errors.Is(err, ErrAuthFoundationStorage) || result.SessionToken != "" {
				t.Fatal("failed completion exposed success or DB error")
			}
			for _, tx := range db.transactions {
				if tx.committed {
					t.Fatal("partial completion committed")
				}
			}
		})
	}
	service, db, _ := nativeUnitFixture(t)
	db.wrongCode = true
	if _, err := service.CompleteEmailSignup(context.Background(), strings.Repeat("a", 43), nativeCompleteFixture(db)); !errors.Is(err, ErrNativeSignupUnavailable) {
		t.Fatal("wrong code accepted")
	}
	if len(db.transactions[1].writes) != 1 || !db.transactions[1].committed {
		t.Fatal("wrong code must persist only attempts")
	}
}

func TestNativePasswordDummyPathsAndConcurrentSnapshotChange(t *testing.T) {
	for _, locked := range []bool{false, true} {
		service, db, spy := nativeUnitFixture(t)
		db.existing = locked
		db.locked = locked
		if _, err := service.PasswordLogin(context.Background(), PasswordLoginInput{Identifier: "private@example.test", Password: "secret-password"}); !errors.Is(err, ErrNativeInvalidCredentials) {
			t.Fatal("unknown/locked login must be generic")
		}
		if spy.dummies != 1 || spy.verifies != 0 {
			t.Fatal("unknown/locked account did not use dummy Argon path")
		}
	}
	service, db, spy := nativeUnitFixture(t)
	db.existing = true
	db.changedHash = true
	if _, err := service.PasswordLogin(context.Background(), PasswordLoginInput{Identifier: "private@example.test", Password: "secret-password"}); !errors.Is(err, ErrNativeInvalidCredentials) {
		t.Fatal("password snapshot change did not invalidate login")
	}
	if spy.verifies != 1 || len(db.transactions) != 2 || db.transactions[1].committed {
		t.Fatal("password change race created a session")
	}
}

func TestNativeSessionAdminHardCapAndBoundedLockout(t *testing.T) {
	service, db, _ := nativeUnitFixture(t)
	admin := service.sessionExpiry(NativeSessionResult{}, db.now, true, true)
	nonAdmin := service.sessionExpiry(NativeSessionResult{}, db.now, true, false)
	if admin.Persistent || admin.AbsoluteExpiresAt.Sub(db.now) != 24*time.Hour {
		t.Fatal("administrator exceeded 24-hour absolute cap")
	}
	if !nonAdmin.Persistent || nonAdmin.AbsoluteExpiresAt.Sub(db.now) != 30*24*time.Hour {
		t.Fatal("non-admin persistent expiry must be 30 days")
	}
	for attempts := 0; attempts < 100; attempts++ {
		delay := nativePasswordLockDuration(attempts)
		if (attempts < 5 && delay != 0) || (attempts >= 5 && (delay < time.Minute || delay > 15*time.Minute)) {
			t.Fatal("lockout is not bounded")
		}
	}
}

func TestNativeInputAndErrorPrivacy(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	logger.Info("native auth", "start", SignupStartInput{Email: "private@example.test"}, "complete", SignupCompleteInput{Password: "secret-password", Code: "420381"}, "login", PasswordLoginInput{Password: "secret-password"}, "notice", AccountNotice{Destination: "private@example.test"}, "session", NativeSessionResult{SessionToken: "private-token"})
	for _, secret := range []string{"private@example.test", "secret-password", "420381", "private-token"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("native auth fields leaked in slog")
		}
	}
	if !errors.Is(nativeSignupWriteError(&pgconn.PgError{Code: "23505", Detail: "private@example.test"}), ErrNativeSignupUnavailable) {
		t.Fatal("unique conflict not concealed")
	}
	if _, err := normalizeSignupDisplayName("Person\x00"); !errors.Is(err, ErrInvalidProfile) {
		t.Fatal("display name control accepted")
	}
	if email, err := NormalizeSignupEmail("  PRIVATE@EXAMPLE.TEST "); err != nil || email != "private@example.test" {
		t.Fatal("email normalization mismatch")
	}
}

func TestNativeExistingAccountNoticeHasNoCode(t *testing.T) {
	inbox, err := devinbox.New("test", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	sender := NewDevinboxSender(inbox, DeliveryEmail)
	if _, err := sender.SendNotice(context.Background(), AccountNotice{ChallengeID: uuid.New(), Kind: "account_exists", Destination: "private@example.test", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	items := inbox.List()
	if len(items) != 1 || items[0].Kind != "notice" || items[0].NoticeKind != "account_exists" || items[0].Code != "" {
		t.Fatal("notice must be distinguishable without any OTP")
	}
}
