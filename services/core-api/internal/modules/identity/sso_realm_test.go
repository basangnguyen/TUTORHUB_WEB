package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type enforcedSchoolFixture struct {
	pgx.Tx
	t *testing.T
}

func (f enforcedSchoolFixture) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f enforcedSchoolFixture) Rollback(context.Context) error        { return nil }
func (f enforcedSchoolFixture) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	f.t.Helper()
	if !strings.Contains(query, "pg_advisory_xact_lock_shared") {
		f.t.Fatalf("unexpected statement before SSO check: %s", query)
	}
	return pgconn.CommandTag{}, nil
}
func (f enforcedSchoolFixture) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	f.t.Helper()
	if !strings.Contains(query, "FROM tutorhub.auth_sso_domains") || strings.Contains(query, "tutorhub.users") || len(args) != 3 || args[0] != "campus.test" {
		f.t.Fatalf("auth attempted account lookup before enforcing domain policy")
	}
	return foundationTestRow{values: []any{uuid.New(), uuid.New(), true, []string{"campus.test"}, nil, "google"}}
}

func TestSSOEnforcedDomainRejectsLocalStartBeforeAccountLookup(t *testing.T) {
	s, _, passwords := nativeUnitFixture(t)
	s.config.SSO = SSORealmConfig{Enabled: true, GoogleEnabled: true}
	s.database = enforcedSchoolFixture{t: t}
	for _, email := range []string{"existing@CAMPUS.TEST", "missing@campus.test"} {
		input := SignupStartInput{Email: email}
		for _, start := range []func(context.Context, SignupStartInput) (SignupChallengeResult, error){s.StartEmailSignup, s.StartEmailCodeLogin, s.StartPasswordRecovery} {
			if _, err := start(context.Background(), input); !errors.Is(err, ErrSSORequired) {
				t.Fatal("local challenge start did not return SSO requirement", err)
			}
		}
		if _, err := s.PasswordLogin(context.Background(), PasswordLoginInput{Identifier: email}); !errors.Is(err, ErrSSORequired) {
			t.Fatal("local password lookup did not return SSO requirement", err)
		}
	}
	if passwords.dummies+passwords.verifies+passwords.hashes != 0 {
		t.Fatal("account-specific password work occurred before SSO requirement")
	}
}
