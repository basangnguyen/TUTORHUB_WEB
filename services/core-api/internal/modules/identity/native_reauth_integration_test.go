//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

// These settings fixtures use a parent transaction and only release savepoints;
// even a future audited mutation remains rollback-only at fixture cleanup.
func TestNativeReauthPostgresPasswordRotationRecentAuthenticationAndAdminCap(t *testing.T) {
	if os.Getenv("AUTH_TEST_DATABASE_URL") == "" || os.Getenv("AUTH_TEST_DISPOSABLE") != "true" {
		t.Skip("requires AUTH_TEST_DATABASE_URL and AUTH_TEST_DISPOSABLE=true")
	}
	for _, admin := range []bool{false, true} {
		name := "persistent"
		if admin {
			name = "administrator"
		}
		t.Run(name, func(t *testing.T) {
			f := newSettingsIntegrationFixture(t, true)
			hash, err := f.passwords.Hash(f.ctx, nativeIntegrationPassword)
			if err != nil || upsertNativePassword(f.ctx, f.tx, f.principal.User.ID, hash, f.now) != nil {
				t.Fatal("seed reauthentication credential")
			}
			f.now = f.now.Add(time.Hour)
			if admin {
				tenantID := uuid.New()
				if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Reauth admin fixture')`, tenantID, "reauth-"+tenantID.String()); err != nil {
					t.Fatal("seed reauth admin tenant")
				}
				if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'org_admin','active')`, tenantID, f.principal.User.ID); err != nil {
					t.Fatal("seed reauth administrator")
				}
			}
			var beforeAbsolute, beforeIdle time.Time
			if err := f.tx.QueryRow(f.ctx, `SELECT absolute_expires_at,expires_at FROM tutorhub.sessions WHERE id=$1`, f.principal.SessionID).Scan(&beforeAbsolute, &beforeIdle); err != nil {
				t.Fatal("read original expiry")
			}
			result, err := f.service.Reauthenticate(f.ctx, f.principal, "", ReauthenticateInput{Method: "password", Password: nativeIntegrationPassword})
			if err != nil {
				t.Fatal(err)
			}
			var reauthenticated, absolute, idle, created time.Time
			var tokenHash, csrfHash []byte
			var version int64
			var persistent bool
			var methods []string
			if err := f.tx.QueryRow(f.ctx, `SELECT reauthenticated_at,token_hash,csrf_token_hash,context_version,persistent,absolute_expires_at,expires_at,created_at,auth_methods
                FROM tutorhub.sessions WHERE id=$1`, f.principal.SessionID).
				Scan(&reauthenticated, &tokenHash, &csrfHash, &version, &persistent, &absolute, &idle, &created, &methods); err != nil {
				t.Fatal("read reauthenticated session")
			}
			if !reauthenticated.Equal(f.now) || !result.RecentAuthExpiresAt.Equal(f.now.Add(10*time.Minute)) || version != 2 ||
				bytes.Equal(tokenHash, f.oldTokenHash) || bytes.Equal(csrfHash, f.oldCSRFHash) ||
				!bytes.Equal(tokenHash, f.service.crypto.Digest(sessionPurpose, result.SessionToken)) ||
				!bytes.Equal(csrfHash, f.service.crypto.Digest(csrfPurpose, result.CSRFToken)) || !slices.Contains(methods, "pwd") || !slices.Contains(methods, "fed") {
				t.Fatal("atomic reauth timestamp, methods, credential or context rotation failed")
			}
			if admin {
				if persistent || absolute.After(created.Add(24*time.Hour)) || !result.CookieExpiresAt.Equal(idle) {
					t.Fatal("admin lifetime cap failed")
				}
			} else if !persistent || !absolute.Equal(beforeAbsolute) || !idle.Equal(beforeIdle) || !result.CookieExpiresAt.Equal(beforeAbsolute) {
				t.Fatal("reauthentication extended or lost original cookie lifetime")
			}
			repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
			if _, err := repository.GetSession(f.ctx, f.oldTokenHash, f.now, 8*time.Hour); !errors.Is(err, ErrSessionNotFound) {
				t.Fatal("old session token replayed")
			}
			fresh, err := repository.loadPrincipal(f.ctx, f.tx, f.principal.SessionID, f.principal.User.ID, uuid.Nil)
			if err != nil || !fresh.AuthenticatedAt.Equal(f.now) || fresh.ContextVersion != version {
				t.Fatal("principal did not inherit GREATEST reauthenticated time")
			}
			identityService := &Service{clock: f.service.now, config: ServiceConfig{RecentAuthTTL: 10 * time.Minute}}
			if identityService.requireRecentAuthentication(fresh) != nil || nativePasswordRecentAuthentication(fresh.AuthenticatedAt, f.now) != nil {
				t.Fatal("shared sensitive-operation recent-auth gates did not accept step-up")
			}
			if _, err := f.service.Reauthenticate(f.ctx, f.principal, "", ReauthenticateInput{Method: "password", Password: nativeIntegrationPassword}); err == nil {
				t.Fatal("stale principal context replayed")
			}
		})
	}
}

func TestNativeReauthPostgresPasswordLockout(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	hash, err := f.passwords.Hash(f.ctx, nativeIntegrationPassword)
	if err != nil || upsertNativePassword(f.ctx, f.tx, f.principal.User.ID, hash, f.now) != nil {
		t.Fatal("seed reauthentication credential")
	}
	f.now = f.now.Add(time.Hour)
	for range 5 {
		if _, err := f.service.Reauthenticate(f.ctx, f.principal, "", ReauthenticateInput{Method: "password", Password: "Incorrect credential"}); !errors.Is(err, ErrNativeInvalidCurrentPassword) {
			t.Fatal("invalid password was not rejected uniformly")
		}
	}
	var attempts int
	var lockedUntil time.Time
	if err := f.tx.QueryRow(f.ctx, `SELECT failed_attempts,locked_until FROM tutorhub.user_password_credentials WHERE user_id=$1`, f.principal.User.ID).Scan(&attempts, &lockedUntil); err != nil || attempts != 5 || !lockedUntil.Equal(f.now.Add(time.Minute)) {
		t.Fatal("shared password lockout was not persisted")
	}
	if _, err := f.service.Reauthenticate(f.ctx, f.principal, "", ReauthenticateInput{Method: "password", Password: nativeIntegrationPassword}); !errors.Is(err, ErrNativeInvalidCurrentPassword) {
		t.Fatal("correct password bypassed active lockout")
	}
	f.now = f.now.Add(2 * time.Minute)
	if _, err := f.service.Reauthenticate(f.ctx, f.principal, "", ReauthenticateInput{Method: "password", Password: nativeIntegrationPassword}); err != nil {
		t.Fatal("password failed after lockout expired")
	}
}

func TestNativeReauthPostgresEmailSessionBindingCooldownAndConsumption(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	f.service.config.EmailCodeAvailable = true
	f.service.challenges.dispatcher = nil
	f.service.challenges.now = f.service.now
	f.service.challenges.code = func() (string, error) { return "420381", nil }
	allow := func(context.Context, string) (bool, error) { return true, nil }
	start, err := f.service.StartReauthEmailCode(f.ctx, f.principal, "", allow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartReauthEmailCode(f.ctx, f.principal, "", allow); !errors.Is(err, ErrAuthChallengeThrottled) {
		t.Fatal("dropping transaction cookie bypassed resend cooldown")
	}
	other := f.principal
	other.SessionID = f.otherSession
	input := ReauthenticateInput{Method: "email_code", ChallengeID: start.ChallengeID, Code: "420381"}
	if _, err := f.service.Reauthenticate(f.ctx, other, start.Cookie.Value, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("session A transaction was accepted in session B")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.auth_challenges SET purpose='login_code' WHERE id=$1`, start.ChallengeID); err != nil {
		t.Fatal("seed challenge purpose mismatch")
	}
	if _, err := f.service.Reauthenticate(f.ctx, f.principal, start.Cookie.Value, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("login challenge was accepted as reauthentication proof")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.auth_challenges SET purpose='reauth' WHERE id=$1`, start.ChallengeID); err != nil {
		t.Fatal("restore reauth challenge purpose")
	}
	input.Code = "000000"
	if _, err := f.service.Reauthenticate(f.ctx, f.principal, start.Cookie.Value, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("wrong verification code accepted")
	}
	var attempts int
	if err := f.tx.QueryRow(f.ctx, `SELECT attempts FROM tutorhub.auth_challenges WHERE id=$1`, start.ChallengeID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("failed code attempt did not commit")
	}
	input.Code = "420381"
	result, err := f.service.Reauthenticate(f.ctx, f.principal, start.Cookie.Value, input)
	if err != nil || !result.RecentAuthExpiresAt.Equal(f.now.Add(10*time.Minute)) {
		t.Fatal("valid email reauthentication failed")
	}
	var consumed, completed bool
	var methods []string
	var reauthenticated time.Time
	var tokenHash, csrfHash []byte
	if err := f.tx.QueryRow(f.ctx, `SELECT c.consumed_at IS NOT NULL,t.state='completed' AND t.completed_at IS NOT NULL
        FROM tutorhub.auth_challenges c JOIN tutorhub.auth_transactions t ON t.id=c.transaction_id WHERE c.id=$1`, start.ChallengeID).Scan(&consumed, &completed); err != nil || !consumed || !completed {
		t.Fatal("email proof was not consumed atomically")
	}
	if err := f.tx.QueryRow(f.ctx, `SELECT reauthenticated_at,token_hash,csrf_token_hash,auth_methods FROM tutorhub.sessions WHERE id=$1`, f.principal.SessionID).
		Scan(&reauthenticated, &tokenHash, &csrfHash, &methods); err != nil || !reauthenticated.Equal(f.now) || !slices.Contains(methods, "email_code") ||
		bytes.Equal(tokenHash, f.oldTokenHash) || bytes.Equal(csrfHash, f.oldCSRFHash) {
		t.Fatal("email step-up failed to rotate session and CSRF")
	}
	f.principal.ContextVersion++
	if _, err := f.service.Reauthenticate(f.ctx, f.principal, start.Cookie.Value, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("consumed email proof replayed with fresh context")
	}
}

func TestNativeReauthPostgresEmailRaceHasOneWinner(t *testing.T) {
	// This committed fixture writes only native auth rows, never audit_events.
	f := newNativeIntegrationFixture(t)
	f.service.config.EmailCodeAvailable = true
	email := "reauth-race-" + uuid.NewString() + "@example.test"
	signup := f.start(t, email)
	session, err := f.service.CompleteEmailSignup(f.ctx, signup.Cookie.Value, nativeIntegrationCompleteInput(signup.ChallengeID))
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRepository(f.pool, 20*time.Second, policy.NewEngine())
	record, err := repository.GetSession(f.ctx, f.service.crypto.Digest(sessionPurpose, session.SessionToken), time.Now().UTC(), 8*time.Hour)
	if err != nil {
		t.Fatal("load race session")
	}
	start, err := f.service.StartReauthEmailCode(f.ctx, record.Principal, "", func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	var transactionID uuid.UUID
	if err := f.pool.QueryRow(f.ctx, `SELECT transaction_id FROM tutorhub.auth_challenges WHERE id=$1`, start.ChallengeID).Scan(&transactionID); err != nil {
		t.Fatal("read race transaction")
	}
	f.transactions = append(f.transactions, transactionID)
	var wins atomic.Int32
	var failures atomic.Int32
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, err := f.service.Reauthenticate(f.ctx, record.Principal, start.Cookie.Value, ReauthenticateInput{Method: "email_code", ChallengeID: start.ChallengeID, Code: "420381"})
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, ErrSessionContextConflict) && !errors.Is(err, ErrNativeInvalidCode) {
				failures.Add(1)
			}
		}()
	}
	close(gate)
	wg.Wait()
	if wins.Load() != 1 || failures.Load() != 0 {
		t.Fatal("same email proof race did not have exactly one winner")
	}
}
