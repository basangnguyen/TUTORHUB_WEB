package classroom

import (
	"strings"
	"testing"
)

func TestLoadSeriesMutationReceiptSQLPreservesAppendOnlyPermissions(t *testing.T) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(loadSeriesMutationReceiptSQL), " "))

	if strings.Contains(normalized, " FOR UPDATE") {
		t.Fatal("receipt lookup must not require UPDATE privilege")
	}
	if !strings.Contains(normalized, "FROM TUTORHUB.CLASS_SESSION_MUTATION_RECEIPTS") {
		t.Fatal("receipt lookup must use the tenant-scoped mutation receipt table")
	}
	if !strings.Contains(normalized, "TENANT_ID = $1") ||
		!strings.Contains(normalized, "IDEMPOTENCY_KEY = $2") {
		t.Fatal("receipt lookup must remain scoped by tenant and idempotency key")
	}
}
