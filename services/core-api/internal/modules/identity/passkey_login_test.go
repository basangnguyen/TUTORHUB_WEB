package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Model committed rows for the single-threaded service test. PostgreSQL's real
// transaction isolation, permissions, and concurrent consumption have their own
// runtime-role integration test.
type passkeyLoginState struct {
	binding, ciphertext     []byte
	kind                    string
	boundUser, boundSession *uuid.UUID
	handle                  []byte
	credential              *webauthn.Credential
	expiry                  time.Time
	used, cloned            bool
	count                   uint32
	sessions, aal           int
	methods                 []string
	event                   string
	revoked                 bool
	sessionHash, csrfHash   []byte
	newLifetime             NativeSessionResult
}

type passkeyLoginDB struct {
	passkeyLoginState
	owner  uuid.UUID
	admin  bool
	locked lockedSecuritySession
}

type passkeyLoginTx struct {
	pgx.Tx
	db *passkeyLoginDB
	passkeyLoginState
}

func (db *passkeyLoginDB) Begin(context.Context) (pgx.Tx, error) {
	return &passkeyLoginTx{db: db, passkeyLoginState: db.passkeyLoginState}, nil
}
func (tx *passkeyLoginTx) Commit(context.Context) error {
	tx.db.passkeyLoginState = tx.passkeyLoginState
	return nil
}
func (tx *passkeyLoginTx) Rollback(context.Context) error { return nil }

func (tx *passkeyLoginTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(query, "INSERT INTO tutorhub.auth_transactions"):
		tx.binding, tx.ciphertext, tx.expiry = args[0].([]byte), args[4].([]byte), args[6].(time.Time)
		tx.kind, tx.boundUser, tx.boundSession = args[1].(string), args[2].(*uuid.UUID), args[3].(*uuid.UUID)
		tx.used = false
	case strings.Contains(query, "UPDATE tutorhub.auth_transactions"):
		tx.used = true
	case strings.Contains(query, "SET sign_count="):
		tx.count = args[2].(uint32)
	case strings.Contains(query, "SET webauthn_user_handle="):
		tx.handle = args[1].([]byte)
	case strings.Contains(query, "SELECT id FROM tutorhub.sessions"):
		// User's sessions are locked before any credential change or step-up.
	case strings.Contains(query, "SELECT user_id,identity_id,active_tenant_id"):
		tx.sessionHash, tx.csrfHash = args[3].([]byte), args[4].([]byte)
		tx.newLifetime = NativeSessionResult{ExpiresAt: args[6].(time.Time), AbsoluteExpiresAt: args[7].(time.Time), Persistent: args[8].(bool)}
	case strings.Contains(query, "revoked_reason=$4") && args[3] == "passkey_reauthenticated":
		tx.revoked = true
	case strings.Contains(query, "INSERT INTO tutorhub.sessions"):
		tx.sessions++
		tx.methods, tx.aal = args[10].([]string), args[13].(int)
	case strings.Contains(query, "INSERT INTO tutorhub.outbox_events"):
		tx.event = args[2].(string)
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected write: %s", query)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *passkeyLoginTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	switch {
	case strings.Contains(query, "SELECT id,payload_ciphertext,expires_at FROM tutorhub.auth_transactions"):
		if !tx.used && bytes.Equal(tx.binding, args[0].([]byte)) && args[1] == tx.kind && reflect.DeepEqual(tx.boundUser, args[2]) &&
			reflect.DeepEqual(tx.boundSession, args[3]) && tx.expiry.After(args[4].(time.Time)) {
			return foundationTestRow{values: []any{tx.db.owner, tx.ciphertext, tx.expiry}}
		}
	case strings.Contains(query, "SELECT user_id FROM tutorhub.webauthn_credentials"):
		if tx.credential != nil && bytes.Equal(args[0].([]byte), tx.credential.ID) {
			return foundationTestRow{values: []any{tx.db.owner}}
		}
	case strings.Contains(query, "SELECT webauthn_user_handle"):
		return foundationTestRow{values: []any{tx.handle}}
	case strings.Contains(query, "SELECT id FROM tutorhub.users"):
		return foundationTestRow{values: []any{tx.db.owner}}
	case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM tutorhub.memberships"):
		return foundationTestRow{values: []any{tx.db.admin}}
	case strings.Contains(query, "SELECT u.email"):
		return foundationTestRow{values: []any{"fixture@example.test", true}}
	case strings.Contains(query, "SELECT created_at,expires_at,absolute_expires_at,persistent"):
		lock := tx.db.locked
		return foundationTestRow{values: []any{lock.CreatedAt, lock.ExpiresAt, lock.AbsoluteExpiresAt, lock.Persistent, lock.AuthenticatedAt, int64(1), lock.AssuranceLevel}}
	case strings.Contains(query, "INSERT INTO tutorhub.webauthn_credentials"):
		if tx.credential != nil && bytes.Equal(tx.credential.ID, args[1].([]byte)) {
			break
		}
		tx.credential = &webauthn.Credential{ID: args[1].([]byte), PublicKey: args[2].([]byte), AttestationType: args[3].(string)}
		tx.credential.Flags.BackupEligible = args[7].(bool)
		tx.credential.Flags.BackupState = args[8].(bool)
		tx.credential.Authenticator.AAGUID = args[4].([]byte)
		tx.count = args[5].(uint32)
		return foundationTestRow{values: []any{uuid.New(), args[9], args[6], args[7], args[8], args[10], (*time.Time)(nil)}}
	case strings.Contains(query, "UPDATE tutorhub.webauthn_credentials SET clone_warning"):
		if !tx.cloned {
			tx.cloned = true
			return foundationTestRow{values: []any{uuid.New()}}
		}
	case strings.Contains(query, "SELECT m.tenant_id"):
	default:
		return foundationTestRow{err: fmt.Errorf("unexpected query: %s", query)}
	}
	return foundationTestRow{err: pgx.ErrNoRows}
}

type credentialTestRows struct {
	pgx.Rows
	row  foundationTestRow
	read bool
}

func (rows *credentialTestRows) Next() bool {
	if rows.read {
		return false
	}
	rows.read = true
	return true
}
func (rows *credentialTestRows) Scan(dest ...any) error { return rows.row.Scan(dest...) }
func (rows *credentialTestRows) Close()                 {}
func (rows *credentialTestRows) Err() error             { return nil }

func (tx *passkeyLoginTx) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "FROM tutorhub.webauthn_credentials") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	c := tx.credential
	if c == nil {
		return &credentialTestRows{read: true}, nil
	}
	return &credentialTestRows{row: foundationTestRow{values: []any{c.ID, c.PublicKey, c.AttestationType,
		c.Authenticator.AAGUID, tx.count, tx.cloned, []string{"internal"}, c.Flags.BackupEligible, c.Flags.BackupState}}}, nil
}

func TestPasskeyDiscoverableSessionWithSignedProof(t *testing.T) {
	const origin = "https://class.tutorhub.test"
	ctx := context.Background()
	service, _, _ := nativeUnitFixture(t)
	rp, err := NewPasskeyProtocol("tutorhub.test", "TutorHub", []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	service.webauthn = rp
	user := PasskeyUser{Handle: make([]byte, 64), Name: "Fixture"}
	rand.Read(user.Handle)
	create, cs, err := rp.RegistrationOptions(user)
	if err != nil {
		t.Fatal(err)
	}
	a := newVirtualPasskey(t)
	c, err := rp.VerifyRegistration(user, *cs, a.Register(t, create, origin, true, true, true, 0))
	if err != nil {
		t.Fatal(err)
	}
	db := &passkeyLoginDB{owner: uuid.New(), admin: true, passkeyLoginState: passkeyLoginState{handle: user.Handle, credential: c}}
	service.database = db
	begin := func() PasskeyOptions {
		t.Helper()
		options, err := service.BeginPasskey(ctx, PasskeyLogin, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(db.ciphertext) == 0 || bytes.Contains(db.ciphertext, []byte("Challenge")) || bytes.Contains(db.ciphertext, []byte("challenge")) {
			t.Fatal("missing encrypted ceremony")
		}
		return options
	}
	for _, uv := range []bool{true, false} {
		options := begin()
		proof := a.Assert(t, options.Options, origin, uv, true, true, 0)
		session, err := service.FinishPasskeyLogin(ctx, options.Cookie.Value, proof, true, "Firefox", "127.0.0.1")
		expected := 1
		if uv {
			expected = 2
		}
		if err != nil || session.SessionToken == "" || session.CSRFToken == "" || db.aal != expected || !slices.Equal(db.methods, []string{"passkey"}) ||
			db.cloned || !db.used || session.Persistent || time.Until(session.AbsoluteExpiresAt) > 24*time.Hour {
			t.Fatalf("signed UV=%v did not produce expected admin session/assurance: %v", uv, err)
		}
		if _, err = service.FinishPasskeyLogin(ctx, options.Cookie.Value, proof, true, "", ""); !errors.Is(err, ErrInvalidPasskey) {
			t.Fatalf("replay: %v", err)
		}
	}
	missing := newVirtualPasskey(t)
	missing.Handle = user.Handle
	start := begin()
	missingProof := missing.Assert(t, start.Options, origin, true, true, true, 0)
	if _, err = service.FinishPasskeyLogin(ctx, start.Cookie.Value, missingProof, false, "", ""); !errors.Is(err, ErrPasskeyUnknown) || !db.used {
		t.Fatalf("absent credential: %v", err)
	}
	next := begin()
	if _, err = service.FinishPasskeyLogin(ctx, next.Cookie.Value, missingProof, false, "", ""); !errors.Is(err, ErrInvalidPasskey) || !db.used {
		t.Fatalf("wrong challenge for absent credential: %v", err)
	}
	start = begin()
	if _, err = service.FinishPasskeyLogin(ctx, start.Cookie.Value, a.Assert(t, start.Options, origin, true, true, true, 3), false, "", ""); err != nil || db.count != 3 {
		t.Fatalf("counter advance: %v", err)
	}
	before := db.sessions
	start = begin()
	if _, err = service.FinishPasskeyLogin(ctx, start.Cookie.Value, a.Assert(t, start.Options, origin, true, true, true, 3), false, "", ""); !errors.Is(err, ErrPasskeyClone) ||
		!db.cloned || !db.used || db.sessions != before || db.event != "passkey_clone_detected" {
		t.Fatalf("clone rejection/outbox: %v", err)
	}
}

func TestPasskeyRegisterAndStepUpWithSignedProof(t *testing.T) {
	const origin = "https://class.tutorhub.test"
	ctx := context.Background()
	now := time.Now().UTC()
	service, _, _ := nativeUnitFixture(t)
	service.now = func() time.Time { return now }
	rp, err := NewPasskeyProtocol("tutorhub.test", "TutorHub", []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	service.webauthn = rp
	db := &passkeyLoginDB{owner: uuid.New(), locked: lockedSecuritySession{
		CreatedAt: now.Add(-23 * time.Hour), AuthenticatedAt: now, ExpiresAt: now.Add(6 * time.Hour),
		AbsoluteExpiresAt: now.Add(7 * 24 * time.Hour), Persistent: true, AssuranceLevel: 1,
	}}
	service.database = db
	p := Principal{User: User{ID: db.owner, DisplayName: "TutorHub member"}, SessionID: uuid.New(), ContextVersion: 1, AuthenticatedAt: now}
	begin := func(purpose PasskeyPurpose) PasskeyOptions {
		t.Helper()
		options, err := service.BeginPasskey(ctx, purpose, &p)
		if err != nil {
			t.Fatal(err)
		}
		return options
	}
	if _, err := service.BeginPasskey(ctx, PasskeyReauth, &p); !errors.Is(err, ErrPasskeyNotFound) {
		t.Fatalf("empty credential set: %v", err)
	}
	device := newVirtualPasskey(t)
	start := begin(PasskeyRegistration)
	proof := device.Register(t, start.Options, origin, true, true, true, 0)
	other := p
	other.SessionID = uuid.New()
	if _, err := service.FinishPasskeyRegistration(ctx, other, start.Cookie.Value, proof, "Security key"); !errors.Is(err, ErrInvalidPasskey) || db.used {
		t.Fatalf("session binding: %v", err)
	}
	registered, err := service.FinishPasskeyRegistration(ctx, p, start.Cookie.Value, proof, "Security key")
	if err != nil || registered.Label != "Security key" || !bytes.Equal(db.handle, device.Handle) || len(db.handle) != 64 ||
		!db.used || db.event != "passkey_added" || !bytes.Equal(db.credential.ID, device.ID) {
		t.Fatalf("registration: %v", err)
	}
	if _, err = service.FinishPasskeyRegistration(ctx, p, start.Cookie.Value, proof, "Again"); !errors.Is(err, ErrInvalidPasskey) {
		t.Fatalf("registration replay: %v", err)
	}
	start = begin(PasskeyRegistration)
	proof = device.Register(t, start.Options, origin, true, true, true, 0)
	if _, err = service.FinishPasskeyRegistration(ctx, p, start.Cookie.Value, proof, "Duplicate"); !errors.Is(err, ErrPasskeyConflict) || !db.used {
		t.Fatalf("duplicate credential: %v", err)
	}
	start = begin(PasskeyReauth)
	if _, err = service.FinishPasskeyReauth(ctx, p, start.Cookie.Value, device.Assert(t, start.Options, origin, false, true, true, 1)); !errors.Is(err, ErrInvalidPasskey) || !db.used || db.revoked {
		t.Fatalf("step-up accepted without UV: %v", err)
	}
	for _, admin := range []bool{false, true} {
		db.admin = admin
		db.revoked = false
		p.SessionID = uuid.New()
		start = begin(PasskeyReauth)
		proof = device.Assert(t, start.Options, origin, true, true, true, db.count+1)
		rotated, err := service.FinishPasskeyReauth(ctx, p, start.Cookie.Value, proof)
		if err != nil || !db.used || !db.revoked || len(rotated.SessionToken) != 43 || len(rotated.CSRFToken) != 43 ||
			rotated.SessionToken == rotated.CSRFToken || !rotated.RecentAuthExpiresAt.Equal(now.Add(10*time.Minute)) ||
			!service.crypto.EqualDigest(db.sessionHash, sessionPurpose, rotated.SessionToken) ||
			!service.crypto.EqualDigest(db.csrfHash, csrfPurpose, rotated.CSRFToken) {
			t.Fatalf("step-up rotation admin=%v: %v", admin, err)
		}
		lifetime := db.newLifetime
		expectedCookie := db.locked.AbsoluteExpiresAt
		if admin {
			expectedCookie = db.locked.CreatedAt.Add(24 * time.Hour)
			if lifetime.Persistent || !lifetime.AbsoluteExpiresAt.Equal(expectedCookie) || !lifetime.ExpiresAt.Equal(expectedCookie) {
				t.Fatal("admin cap")
			}
		} else if !lifetime.Persistent || !lifetime.AbsoluteExpiresAt.Equal(db.locked.AbsoluteExpiresAt) || !lifetime.ExpiresAt.Equal(db.locked.ExpiresAt) {
			t.Fatal("persistent session lifetime changed")
		}
		if !rotated.CookieExpiresAt.Equal(expectedCookie) {
			t.Fatal("wrong cookie expiration")
		}
		if _, err = service.FinishPasskeyReauth(ctx, p, start.Cookie.Value, proof); !errors.Is(err, ErrInvalidPasskey) {
			t.Fatalf("step-up replay: %v", err)
		}
	}
}
