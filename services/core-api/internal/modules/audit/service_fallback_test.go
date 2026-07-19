package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type fallbackDatabaseStub struct {
	transaction pgx.Tx
	err         error
}

func (database fallbackDatabaseStub) Begin(context.Context) (pgx.Tx, error) {
	return database.transaction, database.err
}

type fallbackTransactionStub struct {
	pgx.Tx
	alreadyRecorded bool
	queryError      error
	execError       error
	commitError     error
	query           string
	queryArgs       []any
	execQuery       string
	execArgs        []any
	commitCalls     int
	rollbackCalls   int
}

func (transaction *fallbackTransactionStub) QueryRow(
	_ context.Context,
	query string,
	args ...any,
) pgx.Row {
	transaction.query = query
	transaction.queryArgs = append([]any(nil), args...)
	return fallbackRowStub{value: transaction.alreadyRecorded, err: transaction.queryError}
}

func (transaction *fallbackTransactionStub) Exec(
	_ context.Context,
	query string,
	args ...any,
) (pgconn.CommandTag, error) {
	transaction.execQuery = query
	transaction.execArgs = append([]any(nil), args...)
	return pgconn.CommandTag{}, transaction.execError
}

func (transaction *fallbackTransactionStub) Commit(context.Context) error {
	transaction.commitCalls++
	return transaction.commitError
}

func (transaction *fallbackTransactionStub) Rollback(context.Context) error {
	transaction.rollbackCalls++
	return nil
}

type fallbackRowStub struct {
	value bool
	err   error
}

func (row fallbackRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected fallback row destinations")
	}
	value, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("fallback row destination is not *bool")
	}
	*value = row.value
	return nil
}

func TestRecordFallbackDeduplicatesTransactionalAuditResult(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	tenantID := uuid.New()
	requestContext, _ := requestmeta.New(
		context.Background(),
		"fallback-request",
		"203.0.113.99:443",
		"TutorHub test browser",
		time.Now(),
	)
	requestmeta.SetPrincipal(requestContext, actorID, tenantID)
	request := requestmeta.SnapshotFromContext(requestContext)

	transaction := &fallbackTransactionStub{alreadyRecorded: true}
	service, err := NewService(
		fallbackDatabaseStub{transaction: transaction},
		time.Second,
		policy.NewEngine(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	err = service.RecordFallback(requestContext, Draft{
		Action:       ActionClassUpdate,
		ResourceType: "class",
		ResourceID:   uuid.New(),
		Outcome:      OutcomeFailed,
		Metadata:     Metadata{"reason_code": "internal_failure"},
	})
	if err != nil {
		t.Fatalf("record fallback: %v", err)
	}
	if !strings.Contains(transaction.query, "request_instance_id = $1 AND action = $2") {
		t.Fatalf("unexpected dedupe query: %s", transaction.query)
	}
	if len(transaction.queryArgs) != 2 || transaction.queryArgs[0] != request.RequestInstance ||
		transaction.queryArgs[1] != ActionClassUpdate {
		t.Fatalf("unexpected dedupe arguments: %#v", transaction.queryArgs)
	}
	if transaction.execQuery != "" {
		t.Fatalf("duplicate fallback reached insert: %s", transaction.execQuery)
	}
	if transaction.commitCalls != 1 {
		t.Fatalf("deduplicated fallback did not commit read transaction: %d", transaction.commitCalls)
	}
}

func TestRecordItemFallbackDeduplicatesTransactionalTargetResult(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	tenantID := uuid.New()
	targetUserID := uuid.New()
	requestContext, _ := requestmeta.New(
		context.Background(),
		"item-fallback-request",
		"203.0.113.99:443",
		"TutorHub test browser",
		time.Now(),
	)
	requestmeta.SetPrincipal(requestContext, actorID, tenantID)
	request := requestmeta.SnapshotFromContext(requestContext)
	transaction := &fallbackTransactionStub{alreadyRecorded: true}
	service, err := NewService(
		fallbackDatabaseStub{transaction: transaction},
		time.Second,
		policy.NewEngine(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	err = service.RecordItemFallback(requestContext, targetUserID, Draft{
		Action:       ActionClassEnrollmentSuspend,
		ResourceType: "class_member",
		ResourceID:   targetUserID,
		Outcome:      OutcomeFailed,
		Metadata:     Metadata{"reason_code": "internal_failure"},
	})
	if err != nil {
		t.Fatalf("record item fallback: %v", err)
	}
	if !strings.Contains(transaction.query, "tenant_id = $1") ||
		!strings.Contains(transaction.query, "request_instance_id = $2") ||
		!strings.Contains(transaction.query, "action = $3") ||
		!strings.Contains(transaction.query, "metadata ->> 'target_user_id' = $4") {
		t.Fatalf("unexpected item dedupe query: %s", transaction.query)
	}
	if len(transaction.queryArgs) != 4 || transaction.queryArgs[0] != tenantID ||
		transaction.queryArgs[1] != request.RequestInstance ||
		transaction.queryArgs[2] != ActionClassEnrollmentSuspend ||
		transaction.queryArgs[3] != targetUserID.String() {
		t.Fatalf("unexpected item dedupe arguments: %#v", transaction.queryArgs)
	}
	if transaction.execQuery != "" || transaction.commitCalls != 1 {
		t.Fatalf(
			"deduplicated item fallback must only commit its read: exec=%q commits=%d",
			transaction.execQuery,
			transaction.commitCalls,
		)
	}
}

func TestRecordItemFallbackAppendsServerOwnedTargetMetadata(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	tenantID := uuid.New()
	targetUserID := uuid.New()
	callerTargetID := uuid.New()
	requestContext, _ := requestmeta.New(
		context.Background(),
		"item-fallback-new",
		"203.0.113.99:443",
		"TutorHub test browser",
		time.Now(),
	)
	requestmeta.SetPrincipal(requestContext, actorID, tenantID)
	transaction := &fallbackTransactionStub{}
	service, err := NewService(
		fallbackDatabaseStub{transaction: transaction},
		time.Second,
		policy.NewEngine(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	err = service.RecordItemFallback(requestContext, targetUserID, Draft{
		Action:       ActionClassEnrollmentRemove,
		ResourceType: "class_member",
		ResourceID:   targetUserID,
		Outcome:      OutcomeFailed,
		Metadata: Metadata{
			"reason_code":           "not_attempted",
			MetadataKeyTargetUserID: callerTargetID.String(),
		},
	})
	if err != nil {
		t.Fatalf("record item fallback: %v", err)
	}
	if !strings.Contains(transaction.execQuery, "INSERT INTO tutorhub.audit_events") ||
		len(transaction.execArgs) != 13 {
		t.Fatalf("item fallback did not append audit event: %s %#v", transaction.execQuery, transaction.execArgs)
	}
	metadata, ok := transaction.execArgs[11].(string)
	if !ok || !strings.Contains(metadata, targetUserID.String()) ||
		strings.Contains(metadata, callerTargetID.String()) {
		t.Fatalf("item fallback did not enforce server target metadata: %#v", transaction.execArgs[11])
	}
	if transaction.commitCalls != 1 {
		t.Fatalf("item fallback insert was not committed: %d", transaction.commitCalls)
	}
}

func TestRecordFallbackAppendsWhenRequestActionHasNoAuditResult(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	tenantID := uuid.New()
	requestContext, _ := requestmeta.New(
		context.Background(),
		"fallback-request-new",
		"203.0.113.99:443",
		"TutorHub test browser",
		time.Now(),
	)
	requestmeta.SetPrincipal(requestContext, actorID, tenantID)
	request := requestmeta.SnapshotFromContext(requestContext)
	transaction := &fallbackTransactionStub{}
	service, err := NewService(
		fallbackDatabaseStub{transaction: transaction},
		time.Second,
		policy.NewEngine(),
		func() time.Time { return time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	err = service.RecordFallback(requestContext, Draft{
		Action:       ActionTenantUpdate,
		ResourceType: "tenant",
		ResourceID:   tenantID,
		Outcome:      OutcomeDenied,
		Metadata:     Metadata{"reason_code": "resource_unavailable"},
	})
	if err != nil {
		t.Fatalf("record fallback: %v", err)
	}
	if !strings.Contains(transaction.execQuery, "INSERT INTO tutorhub.audit_events") {
		t.Fatalf("fallback did not append audit event: %s", transaction.execQuery)
	}
	if len(transaction.execArgs) != 13 {
		t.Fatalf("unexpected audit insert arguments: %#v", transaction.execArgs)
	}
	if transaction.execArgs[0] != tenantID || transaction.execArgs[2] != actorID ||
		transaction.execArgs[3] != ActionTenantUpdate || transaction.execArgs[7] != "fallback-request-new" ||
		transaction.execArgs[8] != request.RequestInstance {
		t.Fatalf("fallback lost authoritative request scope: %#v", transaction.execArgs)
	}
	if transaction.commitCalls != 1 {
		t.Fatalf("fallback insert was not committed: %d", transaction.commitCalls)
	}
}

func TestRecordFallbackPropagatesDedupeReadFailureWithoutInserting(t *testing.T) {
	t.Parallel()

	databaseError := errors.New("audit database unavailable")
	transaction := &fallbackTransactionStub{queryError: databaseError}
	service, err := NewService(
		fallbackDatabaseStub{transaction: transaction},
		time.Second,
		policy.NewEngine(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	ctx, _ := requestmeta.New(context.Background(), "fallback-error", "", "", time.Now())
	requestmeta.SetPrincipal(ctx, uuid.New(), uuid.New())
	err = service.RecordFallback(ctx, Draft{
		Action:       ActionTenantArchive,
		ResourceType: "tenant",
		Outcome:      OutcomeFailed,
	})
	if !errors.Is(err, databaseError) {
		t.Fatalf("expected dedupe read error, got %v", err)
	}
	if transaction.execQuery != "" || transaction.commitCalls != 0 {
		t.Fatalf("failed dedupe lookup must not append/commit: exec=%q commits=%d", transaction.execQuery, transaction.commitCalls)
	}
}

func TestRecordRejectsDraftScopeDifferentFromRequestPrincipal(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	tenantID := uuid.New()
	ctx, _ := requestmeta.New(context.Background(), "scope-mismatch", "", "", time.Now())
	requestmeta.SetPrincipal(ctx, actorID, tenantID)
	service, err := NewService(
		fallbackDatabaseStub{err: errors.New("database must not be reached")},
		time.Second,
		policy.NewEngine(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	for _, test := range []struct {
		name  string
		draft Draft
	}{
		{
			name: "tenant",
			draft: Draft{
				TenantID: uuid.New(), ActorID: actorID, Action: ActionTenantUpdate,
				ResourceType: "tenant", Outcome: OutcomeFailed,
			},
		},
		{
			name: "actor",
			draft: Draft{
				TenantID: tenantID, ActorID: uuid.New(), Action: ActionTenantUpdate,
				ResourceType: "tenant", Outcome: OutcomeFailed,
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := service.Record(ctx, test.draft); !errors.Is(err, ErrAccessDenied) {
				t.Fatalf("mismatched request scope must be denied, got %v", err)
			}
		})
	}
}

func TestNormalizeRequestDraftUsesResolvedAuditTenantAndPreservesActor(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	activeTenantID := uuid.New()
	targetTenantID := uuid.New()
	ctx, _ := requestmeta.New(context.Background(), "resolved-audit-scope", "", "", time.Now())
	requestmeta.SetPrincipal(ctx, actorID, activeTenantID)
	requestmeta.SetAuditTenant(ctx, targetTenantID)
	service := &Service{clock: time.Now}

	normalized, err := service.normalizeRequestDraft(ctx, Draft{
		TenantID: targetTenantID, ActorID: actorID,
		Action: ActionMembershipInvitationAccept, ResourceType: "membership_invitation",
		Outcome: OutcomeDenied,
	})
	if err != nil {
		t.Fatalf("normalize resolved audit tenant: %v", err)
	}
	if normalized.TenantID != targetTenantID || normalized.ActorID != actorID {
		t.Fatalf("resolved audit scope was not authoritative: %#v", normalized)
	}

	for _, test := range []struct {
		name  string
		draft Draft
	}{
		{
			name: "active tenant cannot replace resolved target",
			draft: Draft{
				TenantID: activeTenantID, ActorID: actorID,
				Action: ActionMembershipInvitationAccept, ResourceType: "membership_invitation",
				Outcome: OutcomeDenied,
			},
		},
		{
			name: "actor still comes from authenticated principal",
			draft: Draft{
				TenantID: targetTenantID, ActorID: uuid.New(),
				Action: ActionMembershipInvitationAccept, ResourceType: "membership_invitation",
				Outcome: OutcomeDenied,
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := service.normalizeRequestDraft(ctx, test.draft); !errors.Is(err, ErrAccessDenied) {
				t.Fatalf("mismatched resolved request scope must be denied, got %v", err)
			}
		})
	}
}

func TestNormalizeRequestDraftAllowsResolvedAuditTenantWithoutActiveWorkspace(t *testing.T) {
	t.Parallel()

	actorID := uuid.New()
	targetTenantID := uuid.New()
	ctx, _ := requestmeta.New(context.Background(), "resolved-audit-no-active", "", "", time.Now())
	requestmeta.SetPrincipal(ctx, actorID, uuid.Nil)
	requestmeta.SetAuditTenant(ctx, targetTenantID)
	service := &Service{clock: time.Now}

	normalized, err := service.normalizeRequestDraft(ctx, Draft{
		Action: ActionMembershipInvitationAccept, ResourceType: "membership_invitation",
		Outcome: OutcomeSucceeded,
	})
	if err != nil {
		t.Fatalf("normalize resolved audit tenant without active workspace: %v", err)
	}
	if normalized.TenantID != targetTenantID || normalized.ActorID != actorID {
		t.Fatalf("resolved audit target or authenticated actor was lost: %#v", normalized)
	}
}
