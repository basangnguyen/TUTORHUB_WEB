package recurrence

import (
	"errors"
	"testing"
)

func TestPreviewScopeRequiresExplicitFollowingPolicy(t *testing.T) {
	t.Parallel()
	occurrences := []Occurrence{
		{Key: "occ_1", OriginalLocal: "2026-07-20T09:00:00"},
		{Key: "occ_2", OriginalLocal: "2026-07-27T09:00:00"},
		{Key: "occ_3", OriginalLocal: "2026-08-03T09:00:00"},
	}
	exceptions := []Exception{
		{OccurrenceKey: "occ_1", Type: ExceptionOverride},
		{OccurrenceKey: "occ_3", Type: ExceptionCancel},
	}
	if _, err := PreviewScope(
		occurrences, exceptions, "occ_2", ScopeFollowing, "",
	); !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("expected explicit policy error, got %v", err)
	}
	preview, err := PreviewScope(
		occurrences, exceptions, "occ_2", ScopeFollowing, ExceptionDiscard,
	)
	if err != nil {
		t.Fatalf("preview following edit: %v", err)
	}
	if preview.AffectedOccurrenceCount != 2 ||
		preview.FutureExceptionCount != 1 ||
		preview.DiscardedExceptionCount != 1 ||
		preview.RetainedExceptionCount != 0 {
		t.Fatalf("unexpected following preview: %+v", preview)
	}
}

func TestPreviewScopeRetainsStableExceptionIdentity(t *testing.T) {
	t.Parallel()
	occurrences := []Occurrence{
		{Key: "occ_a", OriginalLocal: "2026-07-20T09:00:00"},
		{Key: "occ_b", OriginalLocal: "2026-07-27T09:00:00"},
	}
	exceptions := []Exception{{OccurrenceKey: "occ_b", Type: ExceptionOverride}}
	preview, err := PreviewScope(
		occurrences, exceptions, "occ_b", ScopeThisOccurrence, "",
	)
	if err != nil {
		t.Fatalf("preview occurrence edit: %v", err)
	}
	if preview.BoundaryOccurrenceKey != "occ_b" ||
		preview.BoundaryOriginalLocal != "2026-07-27T09:00:00" ||
		preview.RetainedExceptionCount != 1 {
		t.Fatalf("stable identity was not retained: %+v", preview)
	}
	if _, err := PreviewScope(
		occurrences, exceptions, "occ_missing", ScopeThisOccurrence, "",
	); !errors.Is(err, ErrOccurrenceNotInSeries) {
		t.Fatalf("expected occurrence membership error, got %v", err)
	}
}
