package collaboration

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestSnapshotCursorIsBoundToTenantPrincipalDocumentGenerationAndLimit(t *testing.T) {
	t.Parallel()
	access := testAccess()
	document := Document{ID: uuid.New(), CurrentGeneration: 7}
	want := SnapshotPageCursor{CreatedAt: collaborationTestTime, ID: uuid.New()}

	encoded, err := encodeSnapshotCursor(access, document, 25, want)
	if err != nil {
		t.Fatalf("encode snapshot cursor: %v", err)
	}
	decoded, err := decodeSnapshotCursor(access, document, 25, encoded)
	if err != nil || decoded.ID != want.ID || !decoded.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("decode snapshot cursor=%+v err=%v", decoded, err)
	}

	tests := []struct {
		name     string
		access   AccessContext
		document Document
		limit    int
	}{
		{name: "tenant", access: withSnapshotCursorTenant(access), document: document, limit: 25},
		{name: "principal", access: withSnapshotCursorActor(access), document: document, limit: 25},
		{name: "document", access: access, document: Document{ID: uuid.New(), CurrentGeneration: 7}, limit: 25},
		{name: "generation", access: access, document: Document{ID: document.ID, CurrentGeneration: 8}, limit: 25},
		{name: "limit", access: access, document: document, limit: 50},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeSnapshotCursor(test.access, test.document, test.limit, encoded); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("cross-scope cursor error=%v", err)
			}
		})
	}
}

func TestSnapshotCursorRejectsMalformedOrNonCanonicalPayload(t *testing.T) {
	t.Parallel()
	access := testAccess()
	document := Document{ID: uuid.New(), CurrentGeneration: 1}
	values := []string{
		"wrong-prefix",
		snapshotCursorPrefix + "not-base64!",
		snapshotCursorPrefix + "e30",
		snapshotCursorPrefix + "eyJjcmVhdGVkX2F0IjoiMjAyNi0wOC0yMVQwOTozMDowMFoiLCJpZCI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCIsInNjb3BlX2hhc2giOiJmb3JnZWQifQ",
	}
	for _, value := range values {
		if _, err := decodeSnapshotCursor(access, document, 50, value); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("malformed cursor %q error=%v", value, err)
		}
	}

	tooLong := snapshotCursorPrefix + string(make([]byte, maximumSnapshotCursorLength))
	if _, err := decodeSnapshotCursor(access, document, 50, tooLong); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("oversized cursor error=%v", err)
	}
}

func withSnapshotCursorTenant(access AccessContext) AccessContext {
	access.TenantID = uuid.New()
	return access
}

func withSnapshotCursorActor(access AccessContext) AccessContext {
	access.ActorID = uuid.New()
	return access
}
