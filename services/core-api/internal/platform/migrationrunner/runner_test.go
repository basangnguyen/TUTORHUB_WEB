package migrationrunner

import (
	"context"
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/migrations"
)

func TestUpRequiresDatabaseURL(t *testing.T) {
	t.Parallel()

	err := Up(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_MIGRATION_URL") {
		t.Fatalf("expected missing URL error, got %v", err)
	}
}

func TestDownRequiresPositiveStepCount(t *testing.T) {
	t.Parallel()

	err := Down(context.Background(), "postgresql://not-used", 0)
	if err == nil || !strings.Contains(err.Error(), "greater than zero") {
		t.Fatalf("expected invalid step error, got %v", err)
	}
}

func TestMembershipInvitationMigrationHasSecurityAndRollbackInvariants(t *testing.T) {
	t.Parallel()

	up, err := migrations.Files.ReadFile("000008_membership_invitations.up.sql")
	if err != nil {
		t.Fatalf("read membership invitation up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("000008_membership_invitations.down.sql")
	if err != nil {
		t.Fatalf("read membership invitation down migration: %v", err)
	}

	upSQL := string(up)
	for _, required := range []string{
		"token_hash bytea NOT NULL",
		"octet_length(token_hash) = 32",
		"UNIQUE (token_hash)",
		"status IN ('pending', 'accepted', 'revoked', 'expired')",
		"expires_at <= created_at + interval '30 days'",
		"membership_invitations_state_consistent",
		"updated_at >= expires_at",
		"accepted_at >= created_at",
		"revoked_at >= created_at",
		"intended_role IN ('teacher', 'student', 'guest')",
		"WHERE status = 'pending'",
		"FOREIGN KEY (tenant_id, invited_by)",
		"FOREIGN KEY (tenant_id, accepted_by)",
		"FOREIGN KEY (tenant_id, revoked_by)",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("membership invitation migration is missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(upSQL), "raw_token") {
		t.Fatal("membership invitation schema must not persist a raw token column")
	}
	if !strings.Contains(
		string(down),
		"DROP TABLE tutorhub.membership_invitations",
	) {
		t.Fatal("membership invitation migration must have an explicit rollback")
	}
}
