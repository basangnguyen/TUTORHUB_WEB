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

func TestAppendExpiredMediaAdmissionSupportsSystemActorWithoutRequestMetadata(t *testing.T) {
	t.Parallel()

	transaction := &recordingTransaction{}
	err := AppendDomainEvent(context.Background(), transaction, DomainEvent{
		TenantID:      uuid.New(),
		EventType:     "media_admission.expired.v1",
		AggregateType: "media_admission",
		AggregateID:   uuid.New(),
		Metadata: Metadata{
			MetadataKeyTargetUserID: uuid.NewString(),
			"status":                "expired",
			"reason_code":           "provider_unavailable",
		},
		OccurredAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("append system admission expiry: %v", err)
	}
	if transaction.args[1] != ActorTypeSystem || transaction.args[2] != nil {
		t.Fatalf(
			"system admission expiry actor = type:%#v id:%#v",
			transaction.args[1], transaction.args[2],
		)
	}
	if transaction.args[3] != ActionMediaAdmissionExpire {
		t.Fatalf("system admission expiry action = %#v", transaction.args[3])
	}
	if transaction.args[9] != nil || transaction.args[10] != nil {
		t.Fatal("system admission expiry retained request source metadata")
	}
}

func TestAppendTransactionRejectsSecretShapedMetadata(t *testing.T) {
	for _, key := range []string{
		"session_id",
		"email",
		"provider_room_name",
		"provider_participant_identity",
		"join_attempt_id",
	} {
		t.Run(key, func(t *testing.T) {
			transaction := &recordingTransaction{}
			err := AppendTransaction(context.Background(), transaction, Draft{
				TenantID:     uuid.New(),
				ActorID:      uuid.New(),
				Action:       ActionMediaAdmissionAdmit,
				ResourceType: "media_admission",
				ResourceID:   uuid.New(),
				Outcome:      OutcomeSucceeded,
				Metadata:     Metadata{key: uuid.NewString()},
			})
			if !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("expected invalid metadata key %q, got %v", key, err)
			}
			if transaction.query != "" {
				t.Fatalf("invalid metadata key %q reached the database", key)
			}
		})
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
