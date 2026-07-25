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

func TestOutboxWorkerMigrationHasLeaseFencingAndFailClosedRollback(t *testing.T) {
	t.Parallel()

	up, err := migrations.Files.ReadFile("000015_outbox_worker.up.sql")
	if err != nil {
		t.Fatalf("read outbox worker up migration: %v", err)
	}
	down, err := migrations.Files.ReadFile("000015_outbox_worker.down.sql")
	if err != nil {
		t.Fatalf("read outbox worker down migration: %v", err)
	}

	upSQL := string(up)
	for _, required := range []string{
		"ADD COLUMN lease_owner uuid",
		"ADD COLUMN lease_token bigint NOT NULL DEFAULT 0",
		"ADD COLUMN leased_at timestamptz",
		"ADD COLUMN leased_until timestamptz",
		"ADD COLUMN dead_lettered_at timestamptz",
		"outbox_lease_token_non_negative",
		"outbox_lease_state_valid",
		"lease_token > 0",
		"leased_until > leased_at",
		"outbox_terminal_state_exclusive",
		"outbox_terminal_has_no_lease",
		"UPDATE tutorhub.outbox_events",
		"SET last_error = 'legacy_error_redacted'",
		"WHERE last_error IS NOT NULL",
		"outbox_last_error_code_valid",
		"length(last_error) BETWEEN 1 AND 100",
		"last_error ~ '^[a-z][a-z0-9._-]{0,99}$'",
		"outbox_dead_letter_state_valid",
		"attempts > 0",
		"CREATE INDEX outbox_ready_claim_idx",
		"event_type, available_at, occurred_at, id",
		"CREATE INDEX outbox_expired_lease_claim_idx",
		"event_type, leased_until, occurred_at, id",
		"CREATE INDEX outbox_pending_age_idx",
		"CREATE INDEX outbox_dead_lettered_idx",
		"DROP INDEX tutorhub.outbox_pending_idx",
		"REVOKE ALL ON tutorhub.outbox_events FROM PUBLIC",
	} {
		if !strings.Contains(upSQL, required) {
			t.Fatalf("outbox worker migration is missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(upSQL), "event_version") {
		t.Fatal("outbox event version must remain encoded in the exact event_type")
	}
	sanitizeAt := strings.Index(upSQL, "UPDATE tutorhub.outbox_events")
	errorConstraintAt := strings.Index(upSQL, "ADD CONSTRAINT outbox_last_error_code_valid")
	if sanitizeAt < 0 || errorConstraintAt < 0 || sanitizeAt > errorConstraintAt {
		t.Fatal("legacy last_error text must be redacted before the bounded-code constraint")
	}

	downSQL := string(down)
	for _, required := range []string{
		"LOCK TABLE tutorhub.outbox_events IN ACCESS EXCLUSIVE MODE",
		"lease_owner IS NOT NULL",
		"dead_lettered_at IS NOT NULL",
		"cannot roll back outbox worker schema while retained lease or dead-letter state exists",
		"CREATE INDEX outbox_pending_idx",
		"DROP COLUMN dead_lettered_at",
		"DROP COLUMN leased_until",
		"DROP COLUMN leased_at",
		"DROP COLUMN lease_token",
		"DROP COLUMN lease_owner",
	} {
		if !strings.Contains(downSQL, required) {
			t.Fatalf("outbox worker rollback is missing %q", required)
		}
	}
	if strings.Contains(strings.ToUpper(downSQL), "UPDATE TUTORHUB.OUTBOX_EVENTS") {
		t.Fatal("outbox worker rollback must not disguise dead-letter rows as published")
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
