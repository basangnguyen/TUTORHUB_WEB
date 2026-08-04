package conversation

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSafeMessageStoreErrorNeverExposesPostgresRowDetail(t *testing.T) {
	t.Parallel()

	const protectedContent = "private lesson message"
	err := safeMessageStoreError("insert message", &pgconn.PgError{
		Code:    "23514",
		Message: "check constraint failed",
		Detail:  "Failing row contains (" + protectedContent + ")",
	})

	if strings.Contains(err.Error(), protectedContent) ||
		strings.Contains(err.Error(), "Failing row") {
		t.Fatalf("protected message content escaped the repository boundary: %v", err)
	}
	if !strings.Contains(err.Error(), "23514") {
		t.Fatalf("safe database error lost its bounded SQLSTATE: %v", err)
	}
}
