package classroom

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeEnrollmentClassIDsBoundsAndDeduplicates(t *testing.T) {
	first := uuid.New()
	second := uuid.New()
	ids, err := normalizeEnrollmentClassIDs([]uuid.UUID{first, second, first})
	if err != nil {
		t.Fatalf("normalize enrollment class ids: %v", err)
	}
	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("unexpected normalized ids: %v", ids)
	}
	if _, err := normalizeEnrollmentClassIDs([]uuid.UUID{uuid.Nil}); !errors.Is(
		err,
		ErrInvalidEnrollmentInput,
	) {
		t.Fatalf("nil class id returned %v", err)
	}
	tooMany := make([]uuid.UUID, maximumListLimit+1)
	for index := range tooMany {
		tooMany[index] = uuid.New()
	}
	if _, err := normalizeEnrollmentClassIDs(tooMany); !errors.Is(
		err,
		ErrInvalidEnrollmentInput,
	) {
		t.Fatalf("oversized class id batch returned %v", err)
	}
}
