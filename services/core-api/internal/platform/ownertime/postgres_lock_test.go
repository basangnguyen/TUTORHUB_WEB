package ownertime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type advisoryLockCall struct {
	SQL string
	Key string
}

type recordingTransaction struct {
	pgx.Tx
	calls  []advisoryLockCall
	failAt int
}

func (transaction *recordingTransaction) Exec(
	_ context.Context,
	sql string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	if transaction.failAt > 0 && len(transaction.calls)+1 == transaction.failAt {
		return pgconn.CommandTag{}, errors.New("lock failed")
	}
	transaction.calls = append(transaction.calls, advisoryLockCall{
		SQL: sql,
		Key: arguments[0].(string),
	})
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func TestAcquireLocksUsesLegacyKeysInStableDeduplicatedOrder(t *testing.T) {
	t.Parallel()
	tenantID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	first := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	third := uuid.MustParse("20000000-0000-0000-0000-000000000003")
	transaction := &recordingTransaction{}

	err := AcquireLocks(
		context.Background(),
		transaction,
		tenantID,
		[]uuid.UUID{third, first, second, third, first},
	)
	if err != nil {
		t.Fatalf("acquire owner-time locks: %v", err)
	}
	want := []advisoryLockCall{
		{SQL: advisoryLockSQL, Key: "study-meeting-conflict:10000000-0000-0000-0000-000000000001:20000000-0000-0000-0000-000000000001"},
		{SQL: advisoryLockSQL, Key: "study-meeting-conflict:10000000-0000-0000-0000-000000000001:20000000-0000-0000-0000-000000000002"},
		{SQL: advisoryLockSQL, Key: "study-meeting-conflict:10000000-0000-0000-0000-000000000001:20000000-0000-0000-0000-000000000003"},
	}
	if !reflect.DeepEqual(transaction.calls, want) {
		t.Fatalf("advisory lock calls = %#v, want %#v", transaction.calls, want)
	}
}

func TestOwnerLockKeyIsTenantAndUserScoped(t *testing.T) {
	t.Parallel()
	firstTenant := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	secondTenant := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	firstUser := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	secondUser := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	key := ownerLockKey(firstTenant, firstUser)
	if key != "study-meeting-conflict:10000000-0000-0000-0000-000000000001:20000000-0000-0000-0000-000000000001" {
		t.Fatalf("owner-time lock key = %q", key)
	}
	if ownerLockKey(secondTenant, firstUser) == key {
		t.Fatal("owner-time lock key is not tenant scoped")
	}
	if ownerLockKey(firstTenant, secondUser) == key {
		t.Fatal("owner-time lock key is not user scoped")
	}
}

func TestAcquireLocksRejectsInvalidScopeWithoutExecutingSQL(t *testing.T) {
	t.Parallel()
	tenantID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	userID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	tests := []struct {
		name    string
		ctx     context.Context
		tx      pgx.Tx
		tenant  uuid.UUID
		userIDs []uuid.UUID
	}{
		{name: "nil context", tx: &recordingTransaction{}, tenant: tenantID, userIDs: []uuid.UUID{userID}},
		{name: "nil transaction", ctx: context.Background(), tenant: tenantID, userIDs: []uuid.UUID{userID}},
		{name: "nil tenant", ctx: context.Background(), tx: &recordingTransaction{}, userIDs: []uuid.UUID{userID}},
		{name: "empty users", ctx: context.Background(), tx: &recordingTransaction{}, tenant: tenantID},
		{name: "nil user", ctx: context.Background(), tx: &recordingTransaction{}, tenant: tenantID, userIDs: []uuid.UUID{userID, uuid.Nil}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := AcquireLocks(test.ctx, test.tx, test.tenant, test.userIDs); err == nil {
				t.Fatal("AcquireLocks accepted an invalid scope")
			}
			if transaction, ok := test.tx.(*recordingTransaction); ok && len(transaction.calls) != 0 {
				t.Fatalf("invalid scope executed SQL: %#v", transaction.calls)
			}
		})
	}
}

func TestAcquireLocksStopsAfterDatabaseError(t *testing.T) {
	t.Parallel()
	tenantID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	first := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	transaction := &recordingTransaction{failAt: 2}

	err := AcquireLocks(
		context.Background(), transaction, tenantID, []uuid.UUID{first, second},
	)
	if err == nil || len(transaction.calls) != 1 {
		t.Fatalf("database error = %v, completed calls = %#v", err, transaction.calls)
	}
}
