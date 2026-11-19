package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type passkeyManagementDB struct {
	*securitySessionsDB
	id                                uuid.UUID
	password, federated, otherPasskey bool
	revoked, event                    bool
}

func (db *passkeyManagementDB) Begin(context.Context) (pgx.Tx, error) { return db, nil }

func (db *passkeyManagementDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.HasPrefix(sql, "UPDATE tutorhub.webauthn_credentials SET label="):
		if args[0] != db.id || args[1] != db.principal.User.ID {
			return foundationTestRow{err: pgx.ErrNoRows}
		}
		return foundationTestRow{values: []any{db.id, args[2], []string{"internal"}, true, false, db.now, (*time.Time)(nil)}}
	case strings.HasPrefix(sql, "SELECT EXISTS(SELECT 1 FROM tutorhub.webauthn_credentials"):
		return foundationTestRow{values: []any{args[0] == db.id && args[1] == db.principal.User.ID && !db.revoked}}
	case strings.HasPrefix(sql, "SELECT EXISTS (SELECT 1 FROM tutorhub.identities"):
		if args[0] != db.principal.User.ID || args[1] != nil || args[2] != db.id || !strings.Contains(sql, "id IS DISTINCT FROM $3") || !strings.Contains(sql, "NOT clone_warning") {
			return foundationTestRow{err: errors.New("incorrect alternate login query")}
		}
		return foundationTestRow{values: []any{db.password || db.federated || (args[3].(bool) && db.otherPasskey)}}
	default:
		return db.securitySessionsDB.QueryRow(ctx, sql, args...)
	}
}

func (db *passkeyManagementDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(sql, "UPDATE tutorhub.webauthn_credentials SET status='revoked'") && args[0] == db.id && args[1] == db.principal.User.ID {
		db.revoked = true
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	if strings.Contains(sql, "INSERT INTO tutorhub.outbox_events") && db.revoked && args[2] == "passkey_removed" && args[3] == db.id {
		db.event = true
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	return db.securitySessionsDB.Exec(ctx, sql, args...)
}

func passkeyManagementFixture() (*NativeAuthService, *passkeyManagementDB, Principal) {
	p := Principal{SessionID: uuid.New(), User: User{ID: uuid.New()}, ContextVersion: 1}
	db := &passkeyManagementDB{securitySessionsDB: &securitySessionsDB{principal: p, now: time.Now().UTC(), version: 1}, id: uuid.New()}
	s := &NativeAuthService{webauthn: &PasskeyProtocol{}, database: db, now: func() time.Time { return db.now }}
	return s, db, p
}

func TestPasskeyRenameOwnershipRecentAuthenticationAndLabel(t *testing.T) {
	service, db, p := passkeyManagementFixture()
	ctx := context.Background()
	for _, label := range []string{"", strings.Repeat("đ", 81)} {
		if _, err := service.RenameMyPasskey(ctx, p, db.id, label); !errors.Is(err, ErrAuthFoundationInput) {
			t.Fatal("invalid label accepted", err)
		}
	}
	if _, err := service.RenameMyPasskey(ctx, p, uuid.New(), "Foreign"); !errors.Is(err, ErrPasskeyNotFound) {
		t.Fatal("foreign passkey exposed", err)
	}
	label := strings.Repeat("đ", 80)
	got, err := service.RenameMyPasskey(ctx, p, db.id, label)
	if err != nil || got.Label != label || got.ID != db.id || !db.committed {
		t.Fatal("rename failed", err)
	}
	db.committed = false
	service.now = func() time.Time { return db.now.Add(11 * time.Minute) }
	if _, err := service.RenameMyPasskey(ctx, p, db.id, "Stale"); !errors.Is(err, ErrRecentAuthenticationRequired) || db.committed {
		t.Fatal("stale rename", err)
	}
	if err := service.RevokeMyPasskey(ctx, p, db.id); !errors.Is(err, ErrRecentAuthenticationRequired) || db.revoked {
		t.Fatal("stale revocation", err)
	}
}

func TestPasskeyRevokeLastMethodAndAvailableAlternatives(t *testing.T) {
	for _, method := range []string{"none", "password", "federated", "other passkey"} {
		t.Run(method, func(t *testing.T) {
			service, db, p := passkeyManagementFixture()
			db.password, db.federated, db.otherPasskey = method == "password", method == "federated", method == "other passkey"
			err := service.RevokeMyPasskey(context.Background(), p, db.id)
			if method == "none" {
				if !errors.Is(err, ErrLastIdentity) || db.revoked || db.event || db.committed {
					t.Fatal("last method was removed", err)
				}
				return
			}
			if err != nil || !db.revoked || !db.event || !db.committed {
				t.Fatal("revocation or outbox event failed", err)
			}
		})
	}
}
