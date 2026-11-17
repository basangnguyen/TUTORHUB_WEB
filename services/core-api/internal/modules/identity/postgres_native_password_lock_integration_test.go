//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Observe the blocked backend rather than assuming a goroutine has reached its
// lock after a fixed sleep. While recovery owns the user row, an existing
// session mutation must wait without holding the session row recovery revokes.
func TestNativePasswordUserLockPrecedesExistingSessionMutationLocks(t *testing.T) {
	ctx, pool, _ := authSchemaTestDatabase(t)
	userID, sessionID := uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Lock fixture')`, userID, "lock-"+userID.String()+"@example.test"); err != nil {
		t.Fatal("seed password lock user")
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM tutorhub.users WHERE id=$1`, userID); err != nil {
			t.Error("clean password lock fixture")
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.sessions(id,user_id,token_hash,csrf_token_hash,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time)
        VALUES($1,$2,decode(md5($3),'hex'),decode(md5($3),'hex'),$4,$4,$5,$6,$4)`, sessionID, userID, sessionID.String(), now, now.Add(time.Hour), now.Add(24*time.Hour)); err != nil {
		t.Fatal("seed password lock session")
	}
	for _, scenario := range []struct {
		name string
		lock func(context.Context, pgx.Tx) error
	}{
		{"profile_and_identity", func(ctx context.Context, tx pgx.Tx) error {
			_, err := lockOwnedSession(ctx, tx, sessionID, userID, now)
			return err
		}},
		{"tenant_mutation", func(ctx context.Context, tx pgx.Tx) error {
			_, err := lockTenantMutationSession(ctx, tx, sessionID, userID, SessionRotation{ExpectedContextVersion: 1, RotatedAt: now})
			return err
		}},
		{"reauth_and_session_revocation", func(ctx context.Context, tx pgx.Tx) error {
			service := &NativeAuthService{now: func() time.Time { return now }}
			_, err := service.lockNativeSecuritySession(ctx, tx, Principal{User: User{ID: userID}, SessionID: sessionID, ContextVersion: 1})
			return err
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			owner, err := pool.Begin(bounded)
			if err != nil {
				t.Fatal("begin recovery user lock")
			}
			defer func() { _ = owner.Rollback(context.Background()) }()
			if err := lockActiveTenantUser(bounded, owner, userID); err != nil {
				t.Fatal("acquire recovery user lock")
			}
			waiter, err := pool.Acquire(bounded)
			if err != nil {
				t.Fatal("acquire session mutation connection")
			}
			defer waiter.Release()
			transaction, err := waiter.Begin(bounded)
			if err != nil {
				t.Fatal("begin session mutation")
			}
			var pid int32
			if err := transaction.QueryRow(bounded, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				_ = transaction.Rollback(context.Background())
				t.Fatal("read session mutation backend pid")
			}
			done := make(chan error, 1)
			go func() {
				err := scenario.lock(bounded, transaction)
				_ = transaction.Rollback(context.Background())
				done <- err
			}()
			finished := false
			defer func() {
				cancel()
				_ = owner.Rollback(context.Background())
				if !finished {
					<-done
				}
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var blocked bool
				if err := pool.QueryRow(bounded, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&blocked); err != nil {
					t.Fatal("observe session mutation backend")
				}
				if blocked {
					break
				}
				select {
				case <-done:
					finished = true
					t.Fatal("session mutation did not wait for the user's recovery lock")
				case <-bounded.Done():
					t.Fatal("session mutation did not enter its expected lock wait")
				case <-ticker.C:
				}
			}
			probe, err := pool.Begin(bounded)
			if err != nil {
				t.Fatal("begin session lock probe")
			}
			var probed uuid.UUID
			err = probe.QueryRow(bounded, `SELECT id FROM tutorhub.sessions WHERE id=$1 FOR UPDATE NOWAIT`, sessionID).Scan(&probed)
			_ = probe.Rollback(context.Background())
			if err != nil || probed != sessionID {
				t.Fatal("blocked session mutation held the session before obtaining the user lock")
			}
			if err := owner.Rollback(bounded); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Fatal("release recovery user lock")
			}
			err = <-done
			finished = true
			if err != nil {
				t.Fatal("session mutation failed after recovery released its user lock")
			}
		})
	}
}
