//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestNativeSessionPersistenceSurvivesRotationAndClampsAdministratorElevation(t *testing.T) {
	ctx, pool, _ := authSchemaTestDatabase(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	repository := NewPostgresRepository(tx, 5*time.Second, policy.NewEngine())
	crypto, err := NewCrypto(bytes.Repeat([]byte{0x31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	seed := func(created, absolute time.Time) (uuid.UUID, uuid.UUID, []byte) {
		t.Helper()
		userID, sessionID := uuid.New(), uuid.New()
		hash := crypto.Digest(sessionPurpose, sessionID.String())
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Persistence fixture')`, userID, userID.String()+"@example.test"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.sessions(id,user_id,token_hash,csrf_token_hash,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time,persistent)
		VALUES($1,$2,$3,$3,$4,$5,$6,$7,$4,true)`, sessionID, userID, hash, created, now, now.Add(8*time.Hour), absolute); err != nil {
			t.Fatal(err)
		}
		return userID, sessionID, hash
	}
	student, studentSession, studentHash := seed(now, now.Add(30*24*time.Hour))
	tenantID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Persistence target')`, tenantID, "persist-"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status,joined_at) VALUES($1,$2,'student','active',$3)`, tenantID, student, now); err != nil {
		t.Fatal(err)
	}
	record, err := repository.GetSession(ctx, studentHash, now, 8*time.Hour)
	if err != nil || !record.Principal.CookieExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatal("persistent cookie lifetime was not read")
	}
	rotation := SessionRotation{TokenHash: crypto.Digest(sessionPurpose, "rotated-student"), CSRFHash: crypto.Digest(csrfPurpose, "rotated-student"), ExpectedContextVersion: record.Principal.ContextVersion, RotatedAt: now}
	rotated, err := repository.SwitchActiveTenant(ctx, studentSession, student, tenantID, rotation)
	if err != nil || !rotated.Principal.CookieExpiresAt.Equal(now.Add(30*24*time.Hour)) || !rotated.ExpiresAt.Equal(now.Add(8*time.Hour)) {
		t.Fatal("tenant rotation collapsed persistent cookie lifetime")
	}

	newUser, newSession, newHash := seed(now, now.Add(30*24*time.Hour))
	record, err = repository.GetSession(ctx, newHash, now, 8*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repository.CreateTenant(ctx, newSession, newUser, uuid.Nil, CreateTenantInput{Name: "New admin workspace", Slug: "admin-" + uuid.NewString()}, SessionRotation{
		TokenHash: crypto.Digest(sessionPurpose, "new-admin"), CSRFHash: crypto.Digest(csrfPurpose, "new-admin"), ExpectedContextVersion: record.Principal.ContextVersion, RotatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	var persistent bool
	var absolute, expires time.Time
	if err := tx.QueryRow(ctx, `SELECT persistent,absolute_expires_at,expires_at FROM tutorhub.sessions WHERE id=$1`, newSession).Scan(&persistent, &absolute, &expires); err != nil {
		t.Fatal(err)
	}
	if persistent || !absolute.Equal(now.Add(24*time.Hour)) || !created.Principal.CookieExpiresAt.Equal(expires) {
		t.Fatal("workspace creation retained an administrator's persistent session")
	}

	// A later promotion must reject an old session even if its idle timeout was
	// freshly extended. The cap is anchored to creation, never to the read time.
	oldCreated := now.Add(-48 * time.Hour)
	oldUser, oldSession, oldHash := seed(oldCreated, oldCreated.Add(30*24*time.Hour))
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status,joined_at) VALUES($1,$2,'org_admin','active',$3)`, tenantID, oldUser, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetSession(ctx, oldHash, now, 8*time.Hour); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("old administrator session survived its original24h cap")
	}
	if err := tx.QueryRow(ctx, `SELECT persistent,absolute_expires_at,expires_at FROM tutorhub.sessions WHERE id=$1`, oldSession).Scan(&persistent, &absolute, &expires); err != nil {
		t.Fatal(err)
	}
	if persistent || !absolute.Equal(oldCreated.Add(24*time.Hour)) || expires.After(absolute) {
		t.Fatal("administrator cap was not committed monotonically")
	}
}
