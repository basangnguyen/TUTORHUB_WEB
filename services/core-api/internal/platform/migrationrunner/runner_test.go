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

func TestClassLifecycleMigrationHasConcurrencyAndRestoreInvariants(t *testing.T) {
	t.Parallel()

	up, err := migrations.Files.ReadFile("000009_class_lifecycle.up.sql")
	if err != nil {
		t.Fatalf("read class lifecycle up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("000009_class_lifecycle.down.sql")
	if err != nil {
		t.Fatalf("read class lifecycle down migration: %v", err)
	}

	upSQL := string(up)
	for _, required := range []string{
		"ADD COLUMN timezone text",
		"ADD COLUMN version bigint NOT NULL DEFAULT 1",
		"ADD COLUMN archived_from_status text",
		"SET timezone = tenant.timezone",
		"CHECK (version > 0)",
		"archived_from_status IN ('draft', 'active')",
		"classes_tenant_status_created_id_idx",
		"created_at DESC, id DESC",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("class lifecycle migration is missing %q", required)
		}
	}
	downSQL := string(down)
	for _, required := range []string{
		"DROP COLUMN IF EXISTS archived_from_status",
		"DROP COLUMN IF EXISTS version",
		"DROP COLUMN IF EXISTS timezone",
		"ADD CONSTRAINT classes_archive_state_valid",
	} {
		if !strings.Contains(downSQL, required) {
			t.Fatalf("class lifecycle rollback is missing %q", required)
		}
	}
}

func TestAuditEventsMigrationHasRedactionAppendOnlyAndRollbackInvariants(t *testing.T) {
	t.Parallel()

	up, err := migrations.Files.ReadFile("000011_audit_events.up.sql")
	if err != nil {
		t.Fatalf("read audit events up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("000011_audit_events.down.sql")
	if err != nil {
		t.Fatalf("read audit events down migration: %v", err)
	}

	upSQL := string(up)
	for _, required := range []string{
		"CREATE TABLE tutorhub.audit_events",
		"tenant_id uuid NOT NULL",
		"ON DELETE RESTRICT",
		"actor_user_id uuid",
		"action text NOT NULL",
		"resource_type text NOT NULL",
		"resource_id uuid",
		"outcome text NOT NULL",
		"request_id text NOT NULL",
		"request_instance_id uuid NOT NULL",
		"audit_events_source_ip_prefix_valid",
		"metadata jsonb NOT NULL",
		"audit_metadata_is_redacted",
		"octet_length(value::text) <= 8192",
		"token|secret|password|cookie|session|email|name|description|payload|request_body|sql|error|stack|hash",
		"audit_events_tenant_time_idx",
		"audit_events_tenant_action_time_idx",
		"audit_events_tenant_resource_time_idx",
		"BEFORE UPDATE OR DELETE ON tutorhub.audit_events",
		"BEFORE TRUNCATE ON tutorhub.audit_events",
		"ENABLE ALWAYS TRIGGER audit_events_immutable_rows",
		"ENABLE ALWAYS TRIGGER audit_events_immutable_truncate",
		"REVOKE UPDATE, DELETE, TRUNCATE, TRIGGER",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("audit events migration is missing %q", required)
		}
	}

	tableEnd := strings.Index(upSQL, "CREATE INDEX audit_events_tenant_time_idx")
	if tableEnd < 0 {
		t.Fatal("audit events migration must define the tenant/time index")
	}
	tableSQL := strings.ToLower(upSQL[:tableEnd])
	for _, forbiddenColumn := range []string{
		"raw_token ",
		"token_hash ",
		"session_id ",
		"email text",
		"password ",
		"request_body ",
	} {
		if strings.Contains(tableSQL, forbiddenColumn) {
			t.Fatalf("audit events schema must not persist forbidden column %q", forbiddenColumn)
		}
	}

	downSQL := string(down)
	for _, required := range []string{
		"DROP TABLE tutorhub.audit_events",
		"DROP FUNCTION tutorhub.reject_audit_event_mutation()",
		"DROP FUNCTION tutorhub.audit_metadata_is_redacted(jsonb)",
	} {
		if !strings.Contains(downSQL, required) {
			t.Fatalf("audit events rollback is missing %q", required)
		}
	}
}
