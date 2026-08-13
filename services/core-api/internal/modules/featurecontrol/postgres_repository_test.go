package featurecontrol

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

type featureControlLockCall struct {
	query     string
	arguments []any
}

type recordingFeatureControlTransaction struct {
	execCalls []featureControlLockCall
	events    []string
	rows      []pgx.Row
}

func (transaction *recordingFeatureControlTransaction) Exec(
	_ context.Context,
	query string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	transaction.execCalls = append(transaction.execCalls, featureControlLockCall{
		query: query, arguments: arguments,
	})
	transaction.events = append(transaction.events, "exec:"+query)
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (*recordingFeatureControlTransaction) Query(
	context.Context,
	string,
	...any,
) (pgx.Rows, error) {
	return nil, errors.New("unexpected feature-control row query")
}

func (transaction *recordingFeatureControlTransaction) QueryRow(
	_ context.Context,
	query string,
	_ ...any,
) pgx.Row {
	transaction.events = append(transaction.events, "row:"+query)
	if len(transaction.rows) == 0 {
		return featureControlValueRow{err: errors.New("unexpected feature-control scalar query")}
	}
	row := transaction.rows[0]
	transaction.rows = transaction.rows[1:]
	return row
}

type featureControlValueRow struct {
	value any
	err   error
}

func (row featureControlValueRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected feature-control scan arity")
	}
	switch destination := destinations[0].(type) {
	case *string:
		value, ok := row.value.(string)
		if !ok {
			return errors.New("unexpected feature-control string value")
		}
		*destination = value
	case *bool:
		value, ok := row.value.(bool)
		if !ok {
			return errors.New("unexpected feature-control boolean value")
		}
		*destination = value
	default:
		return errors.New("unexpected feature-control scan destination")
	}
	return nil
}

func TestIsRateQuotaIncludesEveryHourlyQuota(t *testing.T) {
	t.Parallel()

	hourlyQuotas := []QuotaKey{
		QuotaInviteCreationsPerHour,
		QuotaAvailabilityPollCreationsPerHour,
		QuotaAvailabilityPollCapabilityCreationsPerHour,
		QuotaStudyMeetingCreationsPerHour,
		QuotaMessageSendsPerHour,
		QuotaFileUploadIntentsPerHour,
		QuotaMediaSpaceStartsPerHour,
	}
	for _, quota := range hourlyQuotas {
		if !isRateQuota(quota) {
			t.Errorf("expected %q to be classified as a rate quota", quota)
		}
	}

	if isRateQuota(QuotaActiveMediaSpaces) {
		t.Error("expected active media spaces to remain a capacity quota")
	}
}

func TestTenantControlReadAndMutationLocksUseMatchingPostgresModes(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	readTransaction := &recordingFeatureControlTransaction{}
	if err := AcquireTenantControlReadLock(
		context.Background(), readTransaction, tenantID,
	); err != nil {
		t.Fatalf("acquire tenant control read lock: %v", err)
	}
	mutationTransaction := &recordingFeatureControlTransaction{}
	if err := AcquireTenantControlLock(
		context.Background(), mutationTransaction, tenantID,
	); err != nil {
		t.Fatalf("acquire tenant control mutation lock: %v", err)
	}

	if len(readTransaction.execCalls) != 1 ||
		!strings.Contains(readTransaction.execCalls[0].query, "pg_advisory_xact_lock_shared") {
		t.Fatalf("read lock SQL = %#v, want shared advisory transaction lock", readTransaction.execCalls)
	}
	if len(mutationTransaction.execCalls) != 1 ||
		strings.Contains(mutationTransaction.execCalls[0].query, "_shared") ||
		!strings.Contains(mutationTransaction.execCalls[0].query, "pg_advisory_xact_lock(") {
		t.Fatalf("mutation lock SQL = %#v, want exclusive advisory transaction lock", mutationTransaction.execCalls)
	}
	readKey := readTransaction.execCalls[0].arguments[0]
	mutationKey := mutationTransaction.execCalls[0].arguments[0]
	if readKey != mutationKey || readKey != tenantControlLockKey(tenantID) {
		t.Fatalf("lock keys = read:%v mutation:%v, want common tenant key", readKey, mutationKey)
	}
}

func TestRequireFeatureForReadLocksSharedBeforeControlReads(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	transaction := &recordingFeatureControlTransaction{rows: []pgx.Row{
		featureControlValueRow{value: "active"},
		featureControlValueRow{value: true},
	}}
	repository := &PostgresRepository{
		queryTimeout: time.Second,
		catalog:      NewDefaultCatalog(),
	}
	if err := repository.RequireFeatureForRead(
		context.Background(),
		transaction,
		tenantID,
		FeatureClassroomMediaRooms,
	); err != nil {
		t.Fatalf("require feature for read: %v", err)
	}

	if len(transaction.events) != 3 {
		t.Fatalf("read feature events = %#v, want lock plus two reads", transaction.events)
	}
	if !strings.Contains(transaction.events[0], "pg_advisory_xact_lock_shared") ||
		!strings.Contains(transaction.events[1], "FROM tutorhub.tenants") ||
		!strings.Contains(transaction.events[2], "FROM tutorhub.tenant_feature_overrides") {
		t.Fatalf("read feature event order = %#v", transaction.events)
	}
}
