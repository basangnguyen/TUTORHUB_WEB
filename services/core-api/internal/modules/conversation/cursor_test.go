package conversation

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTripAndTamperRejection(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	kind := KindDirect
	want := listCursor{
		UpdatedAt: time.Date(2026, time.August, 3, 1, 2, 3, 4, time.UTC),
		ID:        uuid.New(),
	}
	encoded, err := encodeCursor(tenantID, ListInput{Kind: &kind}, want)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	got, err := decodeCursor(tenantID, ListInput{Kind: &kind, Cursor: encoded})
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if got.ID != want.ID || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	tampered := encoded[:len(encoded)-1] + strings.ToUpper(encoded[len(encoded)-1:])
	if tampered == encoded {
		tampered = encoded + "x"
	}
	if _, err := decodeCursor(tenantID, ListInput{Kind: &kind, Cursor: tampered}); err == nil {
		t.Fatal("tampered cursor unexpectedly decoded")
	}
}
