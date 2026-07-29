package classroom

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestParticipationIdempotencyAdvisoryLockKeyIsPostgresTextSafe(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	lockKey := participationIdempotencyAdvisoryLockKey(
		tenantID,
		"audience\x00replace",
	)

	if !utf8.ValidString(lockKey) {
		t.Fatal("lock key must be valid UTF-8 for a PostgreSQL text parameter")
	}
	if strings.ContainsRune(lockKey, '\x00') {
		t.Fatal("lock key must not contain a NUL byte")
	}
	if len(lockKey) != 2*sha256.Size {
		t.Fatalf("lock key length = %d, want %d", len(lockKey), 2*sha256.Size)
	}
	if _, err := hex.DecodeString(lockKey); err != nil {
		t.Fatalf("lock key must be hexadecimal: %v", err)
	}
}

func TestParticipationIdempotencyAdvisoryLockKeyIsStableAndTenantScoped(t *testing.T) {
	t.Parallel()

	firstTenant := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	secondTenant := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	first := participationIdempotencyAdvisoryLockKey(firstTenant, "request-1")

	if repeated := participationIdempotencyAdvisoryLockKey(firstTenant, "request-1"); repeated != first {
		t.Fatal("same tenant and idempotency key must produce the same lock key")
	}
	if otherTenant := participationIdempotencyAdvisoryLockKey(secondTenant, "request-1"); otherTenant == first {
		t.Fatal("different tenants must produce different lock keys")
	}
	if otherRequest := participationIdempotencyAdvisoryLockKey(firstTenant, "request-2"); otherRequest == first {
		t.Fatal("different idempotency keys must produce different lock keys")
	}
}
