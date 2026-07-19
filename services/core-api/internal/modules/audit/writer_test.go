package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
)

type recordingTransaction struct {
	query string
	args  []any
	err   error
}

func (transaction *recordingTransaction) Exec(
	_ context.Context,
	query string,
	args ...any,
) (pgconn.CommandTag, error) {
	transaction.query = query
	transaction.args = append([]any(nil), args...)
	return pgconn.CommandTag{}, transaction.err
}

func TestAppendDomainEventMapsSafeAuditProjection(t *testing.T) {
	actorID := uuid.New()
	tenantID := uuid.New()
	resourceID := uuid.New()
	ctx, _ := requestmeta.New(
		context.Background(),
		"audit-request-1",
		"203.0.113.44:443",
		"TutorHub Browser",
		time.Now(),
	)
	requestmeta.SetPrincipal(ctx, actorID, tenantID)
	transaction := &recordingTransaction{}

	err := AppendDomainEvent(ctx, transaction, DomainEvent{
		TenantID:      tenantID,
		ActorID:       actorID,
		EventType:     "class.enrollment.role_changed",
		AggregateType: "class_enrollment",
		AggregateID:   resourceID,
		Metadata:      Metadata{"effect": "updated", "class_role": "co_teacher"},
		OccurredAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("append domain event: %v", err)
	}
	if !strings.Contains(transaction.query, "INSERT INTO tutorhub.audit_events") {
		t.Fatalf("unexpected query: %s", transaction.query)
	}
	if transaction.args[3] != ActionClassEnrollmentUpdateRole {
		t.Fatalf("unexpected action: %#v", transaction.args[3])
	}
	if transaction.args[7] != "audit-request-1" {
		t.Fatalf("unexpected request id: %#v", transaction.args[7])
	}
	if transaction.args[9] != "203.0.113.0/24" {
		t.Fatalf("unexpected source prefix: %#v", transaction.args[9])
	}
	if hash, ok := transaction.args[10].([]byte); !ok || len(hash) != 32 {
		t.Fatalf("unexpected user-agent hash: %#v", transaction.args[10])
	}
	if metadata, ok := transaction.args[11].(string); !ok || strings.Contains(metadata, "TutorHub Browser") {
		t.Fatalf("unsafe metadata: %#v", transaction.args[11])
	}
}

func TestAppendTransactionRejectsSecretShapedMetadata(t *testing.T) {
	transaction := &recordingTransaction{}
	err := AppendTransaction(context.Background(), transaction, Draft{
		TenantID:     uuid.New(),
		ActorID:      uuid.New(),
		Action:       ActionTenantUpdate,
		ResourceType: "tenant",
		ResourceID:   uuid.New(),
		Outcome:      OutcomeSucceeded,
		Metadata:     Metadata{"session_id": uuid.NewString()},
	})
	if !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("expected invalid metadata, got %v", err)
	}
	if transaction.query != "" {
		t.Fatal("invalid metadata reached the database")
	}
}

func TestAppendTransactionPropagatesDatabaseFailure(t *testing.T) {
	databaseError := errors.New("database unavailable")
	transaction := &recordingTransaction{err: databaseError}
	err := AppendTransaction(context.Background(), transaction, Draft{
		TenantID:     uuid.New(),
		ActorID:      uuid.New(),
		Action:       ActionClassCreate,
		ResourceType: "class",
		ResourceID:   uuid.New(),
		Outcome:      OutcomeSucceeded,
	})
	if !errors.Is(err, databaseError) {
		t.Fatalf("expected database error, got %v", err)
	}
}
