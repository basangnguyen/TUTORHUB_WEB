package requestmeta

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMetadataCarriesCorrelationAndAuthoritativePrincipal(t *testing.T) {
	startedAt := time.Date(2026, 7, 19, 10, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	ctx, _ := New(
		context.Background(),
		"request-123",
		"192.0.2.129:443",
		"TutorHub test browser",
		startedAt,
	)
	actorID := uuid.New()
	tenantID := uuid.New()
	auditTenantID := uuid.New()
	SetPrincipal(ctx, actorID, tenantID)
	SetAuditTenant(ctx, auditTenantID)
	SetAuditTenant(ctx, uuid.New())

	snapshot := SnapshotFromContext(ctx)
	if snapshot.RequestID != "request-123" || snapshot.RequestInstance == uuid.Nil {
		t.Fatalf("unexpected correlation metadata: %#v", snapshot)
	}
	if snapshot.StartedAt != startedAt.UTC() {
		t.Fatalf("unexpected request start: %s", snapshot.StartedAt)
	}
	if snapshot.SourceIPPrefix != "192.0.2.0/24" {
		t.Fatalf("unexpected IP prefix: %s", snapshot.SourceIPPrefix)
	}
	if len(snapshot.UserAgentHash) != sha256Size {
		t.Fatalf("unexpected user-agent hash length: %d", len(snapshot.UserAgentHash))
	}
	if snapshot.ActorID != actorID || snapshot.TenantID != tenantID {
		t.Fatalf("unexpected principal scope: %#v", snapshot)
	}
	if !snapshot.AuditTenantResolved || snapshot.AuditTenantID != auditTenantID {
		t.Fatalf("unexpected resolved audit tenant: %#v", snapshot)
	}
}

func TestSetAuditTenantRequiresMetadataAndNonNilAuthoritativeTenant(t *testing.T) {
	t.Parallel()

	SetAuditTenant(context.Background(), uuid.New())
	ctx, _ := New(context.Background(), "audit-target", "", "", time.Now())
	SetAuditTenant(ctx, uuid.Nil)
	if snapshot := SnapshotFromContext(ctx); snapshot.AuditTenantResolved ||
		snapshot.AuditTenantID != uuid.Nil {
		t.Fatalf("nil audit tenant must remain unresolved: %#v", snapshot)
	}
}

func TestSnapshotWithoutMetadataUsesSafeInternalCorrelation(t *testing.T) {
	snapshot := SnapshotFromContext(context.Background())
	if snapshot.RequestID != internalRequestID || snapshot.RequestInstance == uuid.Nil {
		t.Fatalf("unexpected internal snapshot: %#v", snapshot)
	}
	if snapshot.SourceIPPrefix != "" || snapshot.UserAgentHash != nil {
		t.Fatalf("internal snapshot retained source metadata: %#v", snapshot)
	}
	if snapshot.AuditTenantResolved || snapshot.AuditTenantID != uuid.Nil {
		t.Fatalf("internal snapshot unexpectedly resolved an audit tenant: %#v", snapshot)
	}
}

func TestIPPrefixAndUserAgentHashMinimizeSource(t *testing.T) {
	if got := IPPrefix("[2001:db8:1234:5678::1]:443"); got != "2001:db8:1234:5600::/56" {
		t.Fatalf("unexpected IPv6 prefix: %s", got)
	}
	if got := IPPrefix("not-an-address"); got != "" {
		t.Fatalf("invalid remote address should be discarded: %s", got)
	}
	if got := UserAgentHash("  "); got != nil {
		t.Fatalf("blank user agent should be discarded: %x", got)
	}
}

const sha256Size = 32
