package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type passwordSettingsTestDB struct {
	now, authenticatedAt                time.Time
	active, verified, admin, persistent bool
	changedHash, changedSession         bool
	lockedUntil                         *time.Time
	transactions                        []*passwordSettingsTestTx
	failAt                              string
	attempts                            int
}

type passwordSettingsTestTx struct {
	pgx.Tx
	db                *passwordSettingsTestDB
	closed, committed bool
	writes            []string
	arguments         [][]any
}

func (db *passwordSettingsTestDB) Begin(context.Context) (pgx.Tx, error) {
	tx := &passwordSettingsTestTx{db: db}
	db.transactions = append(db.transactions, tx)
	return tx, nil
}
func (tx *passwordSettingsTestTx) Rollback(context.Context) error { tx.closed = true; return nil }
func (tx *passwordSettingsTestTx) Commit(context.Context) error {
	tx.closed = true
	if tx.db.failAt == "commit" {
		return errors.New("sensitive fixture error")
	}
	tx.committed = true
	return nil
}
func (tx *passwordSettingsTestTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "SELECT id FROM tutorhub.sessions") || strings.HasPrefix(query, "SELECT id FROM tutorhub.identities") {
		return pgconn.NewCommandTag("SELECT 2"), nil
	}
	tx.writes = append(tx.writes, query)
	tx.arguments = append(tx.arguments, args)
	if tx.db.failAt != "" && strings.Contains(query, tx.db.failAt) {
		return pgconn.CommandTag{}, errors.New("private@example.test raw-secret")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (tx *passwordSettingsTestTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	db := tx.db
	hash, status := dummyPasswordPHC, "active"
	switch {
	case strings.Contains(query, "FROM tutorhub.users u LEFT JOIN tutorhub.user_password_credentials"):
		var phc, credentialStatus *string
		var updated *time.Time
		if db.active {
			phc, credentialStatus, updated = &hash, &status, &db.now
		}
		return foundationTestRow{values: []any{"member@example.test", "Member", db.verified, phc, credentialStatus, updated, db.lockedUntil}}
	case strings.Contains(query, "FROM tutorhub.users u WHERE"):
		return foundationTestRow{values: []any{"member@example.test", "Member", db.verified}}
	case strings.Contains(query, "FROM tutorhub.sessions WHERE"):
		version := int64(1)
		if db.changedSession {
			version = 2
		}
		return foundationTestRow{values: []any{db.now, db.now.Add(8 * time.Hour), db.now.Add(30 * 24 * time.Hour), db.persistent, db.authenticatedAt, version}}
	case strings.Contains(query, "FROM tutorhub.user_password_credentials WHERE"):
		if !db.active {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		if db.changedHash {
			hash = "changed-password-hash"
		}
		return foundationTestRow{values: []any{&hash, status, db.attempts, db.lockedUntil}}
	case strings.HasPrefix(query, "INSERT INTO tutorhub.identities"):
		tx.writes = append(tx.writes, query)
		if db.failAt == "identity" {
			return foundationTestRow{err: errors.New("sensitive fixture error")}
		}
		return foundationTestRow{values: []any{uuid.New()}}
	case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM tutorhub.memberships"):
		return foundationTestRow{values: []any{db.admin}}
	default:
		return foundationTestRow{err: errors.New("unexpected settings query")}
	}
}

type passwordSettingsTestHasher struct {
	db                        *passwordSettingsTestDB
	matched                   bool
	verifies, hashes, dummies int
	policyErr                 error
}

func (hasher *passwordSettingsTestHasher) assertNoConnection() error {
	for _, tx := range hasher.db.transactions {
		if !tx.closed {
			return errors.New("password work holds database connection")
		}
	}
	return nil
}
func (hasher *passwordSettingsTestHasher) Verify(context.Context, string, string) (bool, bool, error) {
	hasher.verifies++
	return hasher.matched, false, hasher.assertNoConnection()
}
func (hasher *passwordSettingsTestHasher) Hash(context.Context, string) (string, error) {
	return dummyPasswordPHC, nil
}
func (hasher *passwordSettingsTestHasher) DummyVerify(context.Context, string) error {
	hasher.dummies++
	return hasher.assertNoConnection()
}
func (hasher *passwordSettingsTestHasher) ValidateAndHash(context.Context, string, string, string) (string, error) {
	hasher.hashes++
	if err := hasher.assertNoConnection(); err != nil {
		return "", err
	}
	return dummyPasswordPHC, hasher.policyErr
}

func passwordSettingsUnitFixture(t *testing.T) (*NativeAuthService, *passwordSettingsTestDB, *passwordSettingsTestHasher, Principal, UpdateMyPasswordInput) {
	t.Helper()
	now := time.Now().UTC()
	db := &passwordSettingsTestDB{now: now, authenticatedAt: now, active: true, verified: true, persistent: true}
	hasher := &passwordSettingsTestHasher{db: db, matched: true}
	crypto, err := NewCrypto(bytes.Repeat([]byte{31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewNativeAuthService(db, crypto, hasher, nil, NativeAuthConfig{LegalTermsVersion: "test", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	principal := Principal{User: User{ID: uuid.New()}, SessionID: uuid.New(), ContextVersion: 1, AuthenticatedAt: now}
	current := "Current password 42!"
	return service, db, hasher, principal, UpdateMyPasswordInput{CurrentPassword: &current, NewPassword: "New password 43!"}
}

func TestPasswordSettingsRequiresRecentAuthenticationAndVerifiedEnrollment(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*passwordSettingsTestDB, *Principal, *UpdateMyPasswordInput)
		want      error
	}{
		{"old principal", func(db *passwordSettingsTestDB, p *Principal, _ *UpdateMyPasswordInput) {
			p.AuthenticatedAt = db.now.Add(-11 * time.Minute)
		}, ErrRecentAuthenticationRequired},
		{"old authoritative session", func(db *passwordSettingsTestDB, _ *Principal, _ *UpdateMyPasswordInput) {
			db.authenticatedAt = db.now.Add(-11 * time.Minute)
		}, ErrRecentAuthenticationRequired},
		{"unverified first password", func(db *passwordSettingsTestDB, _ *Principal, input *UpdateMyPasswordInput) {
			db.active = false
			db.verified = false
			input.CurrentPassword = nil
		}, ErrNativeVerifiedEmailRequired},
		{"unexpected current password", func(db *passwordSettingsTestDB, _ *Principal, _ *UpdateMyPasswordInput) { db.active = false }, ErrNativeInvalidCurrentPassword},
		{"concurrent password change", func(db *passwordSettingsTestDB, _ *Principal, _ *UpdateMyPasswordInput) { db.changedHash = true }, ErrSessionContextConflict},
		{"concurrent session rotation", func(db *passwordSettingsTestDB, _ *Principal, _ *UpdateMyPasswordInput) { db.changedSession = true }, ErrSessionContextConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, db, _, principal, input := passwordSettingsUnitFixture(t)
			tc.configure(db, &principal, &input)
			if _, err := service.UpdateMyPassword(context.Background(), principal, input); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			for _, tx := range db.transactions {
				if tx.committed {
					t.Fatal("rejected update committed")
				}
			}
		})
	}
}

func TestPasswordSettingsRejectsEquivalentNFKCAndPolicyWithoutWrites(t *testing.T) {
	service, db, hasher, principal, input := passwordSettingsUnitFixture(t)
	current := "Ａbcdefgh 42!"
	input.CurrentPassword = &current
	input.NewPassword = "Abcdefgh 42!"
	if _, err := service.UpdateMyPassword(context.Background(), principal, input); !errors.Is(err, ErrNativePasswordUnchanged) {
		t.Fatal("NFKC equivalent password accepted")
	}
	if hasher.hashes != 0 || len(db.transactions) != 1 {
		t.Fatal("equivalent password acquired a mutation transaction")
	}
	input.NewPassword = "Different password 42!"
	hasher.policyErr = ErrPasswordPolicy
	if _, err := service.UpdateMyPassword(context.Background(), principal, input); !errors.Is(err, ErrPasswordPolicy) {
		t.Fatal("policy failure was ignored")
	}
	for _, tx := range db.transactions {
		if tx.committed || len(tx.writes) > 0 {
			t.Fatal("policy failure mutated credentials")
		}
	}
}

func TestPasswordSettingsWrongPasswordTracksBoundedLockout(t *testing.T) {
	service, db, hasher, principal, input := passwordSettingsUnitFixture(t)
	hasher.matched = false
	db.attempts = 4
	if _, err := service.UpdateMyPassword(context.Background(), principal, input); !errors.Is(err, ErrNativeInvalidCurrentPassword) {
		t.Fatal("wrong current password accepted")
	}
	if hasher.hashes != 0 || len(db.transactions) != 2 || !db.transactions[1].committed {
		t.Fatal("wrong password must only commit its failure counter")
	}
	tx := db.transactions[1]
	if len(tx.writes) != 1 || tx.arguments[0][1].(int) != 5 || !tx.arguments[0][3].(*time.Time).Equal(db.now.Add(time.Minute)) {
		t.Fatal("fifth failure did not apply one-minute lockout")
	}
	until := db.now.Add(time.Minute)
	db.lockedUntil = &until
	if _, err := service.UpdateMyPassword(context.Background(), principal, input); !errors.Is(err, ErrNativeInvalidCurrentPassword) || hasher.dummies != 1 {
		t.Fatal("locked account must do dummy work and remain locked")
	}
}

func TestPasswordSettingsCommitAndRollbackAreAtomic(t *testing.T) {
	for _, failure := range []string{"", "identity", "INSERT INTO tutorhub.user_password_credentials", "revoked_reason='password_changed'", "SET token_hash", "commit"} {
		t.Run(failure, func(t *testing.T) {
			service, db, hasher, principal, input := passwordSettingsUnitFixture(t)
			db.failAt = failure
			result, err := service.UpdateMyPassword(context.Background(), principal, input)
			if hasher.hashes != 1 || hasher.verifies != 1 {
				t.Fatal("password work was not performed outside mutation")
			}
			if failure != "" {
				if !errors.Is(err, ErrAuthFoundationStorage) {
					t.Fatal("failed write must return a sanitized storage error")
				}
				for _, tx := range db.transactions {
					if tx.committed {
						t.Fatal("partial password update committed")
					}
				}
				return
			}
			if err != nil || !result.HasPassword || result.SessionToken == "" || result.CSRFToken == "" || !result.CookieExpiresAt.Equal(db.now.Add(30*24*time.Hour)) {
				t.Fatal("successful password update did not preserve persistent session")
			}
			if len(db.transactions) != 2 || !db.transactions[1].committed {
				t.Fatal("password writes must have one atomic commit")
			}
		})
	}
}

func TestPasswordSettingsFirstPasswordAndAdminCookie(t *testing.T) {
	service, db, hasher, principal, input := passwordSettingsUnitFixture(t)
	db.active = false
	db.admin = true
	input.CurrentPassword = nil
	before, err := service.GetMyPasswordStatus(context.Background(), principal)
	if err != nil || before.HasPassword || before.UpdatedAt != nil {
		t.Fatal("missing credential status incorrect")
	}
	result, err := service.UpdateMyPassword(context.Background(), principal, input)
	if err != nil || hasher.verifies != 0 || !result.CookieExpiresAt.Equal(db.now.Add(8*time.Hour)) {
		t.Fatal("first password/admin cookie policy failed")
	}
}
