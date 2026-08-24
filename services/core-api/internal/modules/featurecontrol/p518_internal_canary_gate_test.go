package featurecontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestRequireFeatureRejectsNonAllowlistedTenantPersistedTrueOverride(t *testing.T) {
	allowedTenantID := uuid.MustParse("10000000-0000-4000-8000-000000000018")
	deniedTenantID := uuid.MustParse("10000000-0000-4000-8000-000000000019")
	catalog, err := NewCatalog(Guardrails{
		TenantAllowlists: map[FeatureKey][]uuid.UUID{
			FeatureClassroomWhiteboards: {allowedTenantID},
		},
	})
	if err != nil {
		t.Fatalf("create tenant-allowlisted catalog: %v", err)
	}
	transaction := &recordingFeatureControlTransaction{rows: []pgx.Row{
		featureControlValueRow{value: "active"},
		featureControlValueRow{value: true},
	}}
	repository := &PostgresRepository{queryTimeout: time.Second, catalog: catalog}

	err = repository.RequireFeature(
		context.Background(),
		transaction,
		deniedTenantID,
		FeatureClassroomWhiteboards,
	)
	if !errors.Is(err, ErrFeatureDisabled) {
		t.Fatalf("non-allowlisted persisted true override error=%v, want ErrFeatureDisabled", err)
	}
	if len(transaction.events) != 3 {
		t.Fatalf("feature preflight events=%#v, want lock plus two control reads", transaction.events)
	}
	if !strings.Contains(transaction.events[0], "pg_advisory_xact_lock(") ||
		strings.Contains(transaction.events[0], "_shared") ||
		!strings.Contains(transaction.events[1], "FROM tutorhub.tenants") ||
		!strings.Contains(transaction.events[2], "FROM tutorhub.tenant_feature_overrides") {
		t.Fatalf("feature preflight event order=%#v", transaction.events)
	}
	if len(transaction.execCalls) != 1 {
		t.Fatalf("feature preflight performed unexpected PostgreSQL writes: %#v", transaction.execCalls)
	}
}
