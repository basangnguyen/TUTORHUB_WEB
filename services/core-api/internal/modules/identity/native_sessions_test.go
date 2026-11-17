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

type securitySessionsDB struct {
	pgx.Tx
	now       time.Time
	principal Principal
	queries   []string
	version   int64
	affected  int
	valid     bool
	committed bool
	fail      string
}

func (db *securitySessionsDB) Begin(context.Context) (pgx.Tx, error) { return db, nil }
func (db *securitySessionsDB) Rollback(context.Context) error        { return nil }
func (db *securitySessionsDB) Commit(context.Context) error {
	if db.fail == "commit" {
		return errors.New("private storage detail")
	}
	db.committed = true
	return nil
}
func (db *securitySessionsDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.queries = append(db.queries, query)
	switch {
	case strings.Contains(query, "FROM tutorhub.users u WHERE"):
		if args[0] != db.principal.User.ID {
			return foundationTestRow{err: errors.New("incorrect owner")}
		}
		return foundationTestRow{values: []any{"owner@example.test", true}}
	case strings.Contains(query, "FROM tutorhub.sessions WHERE"):
		if args[0] != db.principal.SessionID || args[1] != db.principal.User.ID {
			return foundationTestRow{err: errors.New("incorrect current session")}
		}
		return foundationTestRow{values: []any{db.now, db.now.Add(time.Hour), db.now.Add(24 * time.Hour), false, db.now, db.version, 1}}
	case strings.Contains(query, "SELECT EXISTS (SELECT 1 FROM tutorhub.sessions"):
		return foundationTestRow{values: []any{db.valid}}
	default:
		return foundationTestRow{err: errors.New("unexpected query")}
	}
}
func (db *securitySessionsDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	db.queries = append(db.queries, query)
	if strings.HasPrefix(query, "SELECT id FROM tutorhub.sessions") {
		if !strings.Contains(query, "ORDER BY id FOR UPDATE") || args[0] != db.principal.User.ID {
			return pgconn.CommandTag{}, errors.New("unstable lock order")
		}
		return pgconn.NewCommandTag("SELECT 2"), nil
	}
	if db.fail == "update" {
		return pgconn.CommandTag{}, errors.New("secret storage detail")
	}
	if strings.Contains(query, "user_revoked_others") {
		if args[0] != db.principal.User.ID || args[1] != db.principal.SessionID {
			return pgconn.CommandTag{}, errors.New("revoke other scope")
		}
	} else if args[1] != db.principal.User.ID {
		return pgconn.CommandTag{}, errors.New("revoke single scope")
	}
	if db.affected == 0 {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return pgconn.NewCommandTag("UPDATE 2"), nil
}

func TestSecuritySessionRevocationAuthorizationAndAtomicity(t *testing.T) {
	for _, test := range []struct {
		name            string
		current, others bool
		version         int64
		affected        int
		fail            string
		want            error
	}{
		{"current session", true, false, 1, 1, "", ErrCannotRevokeCurrentSession},
		{"foreign or already revoked", false, false, 1, 0, "", ErrManagedSessionNotFound},
		{"stale context", false, true, 2, 1, "", ErrSessionContextConflict},
		{"revoke others", false, true, 1, 2, "", nil},
		{"repeat revoke others", false, true, 1, 0, "", nil},
		{"storage redacted", false, true, 1, 2, "update", ErrAuthFoundationStorage},
		{"commit failure", false, true, 1, 2, "commit", ErrAuthFoundationStorage},
	} {
		t.Run(test.name, func(t *testing.T) {
			principal := Principal{SessionID: uuid.New(), User: User{ID: uuid.New()}, ContextVersion: 1}
			db := &securitySessionsDB{now: time.Now().UTC(), principal: principal, version: test.version, affected: test.affected, fail: test.fail}
			service := &NativeAuthService{database: db, now: func() time.Time { return db.now }, config: NativeAuthConfig{QueryTimeout: time.Second}}
			target := uuid.New()
			if test.current {
				target = principal.SessionID
			}
			var err error
			if test.others {
				var count int
				count, err = service.RevokeOtherSessions(context.Background(), principal)
				if err == nil && count != test.affected {
					t.Fatal("incorrect revoked count")
				}
			} else {
				err = service.RevokeMySession(context.Background(), principal, target)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("unexpected failure: %v", err)
			}
			if test.want != nil && db.committed {
				t.Fatal("denied mutation committed")
			}
			if test.want == nil && !db.committed {
				t.Fatal("successful mutation not committed")
			}
			if len(db.queries) < 2 || !strings.Contains(db.queries[0], "FROM tutorhub.users") || !strings.Contains(db.queries[1], "ORDER BY id FOR UPDATE") {
				t.Fatal("user/session lock order not retained")
			}
		})
	}
}

func TestSecurityReadRejectsInvalidCurrentSession(t *testing.T) {
	principal := Principal{SessionID: uuid.New(), User: User{ID: uuid.New()}, ContextVersion: 1}
	db := &securitySessionsDB{now: time.Now().UTC(), principal: principal, valid: false}
	service := &NativeAuthService{database: db, now: func() time.Time { return db.now }}
	if _, err := service.ListMySessions(context.Background(), principal); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("session list bypassed current context")
	}
	if _, err := service.ListMyLoginMethods(context.Background(), principal); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("login methods bypassed current context")
	}
}
