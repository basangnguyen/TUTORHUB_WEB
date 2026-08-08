package classroom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSourceMediaSpaceBarrierDetectsOpenAndFollowingBindings(t *testing.T) {
	t.Parallel()

	spaces := []lockedSourceMediaSpace{
		{ID: uuid.New(), Status: "ended", OccurrenceKey: "occ_before"},
		{ID: uuid.New(), Status: "open", OccurrenceKey: "occ_after"},
	}
	if !hasOpenSourceMediaSpace(spaces) {
		t.Fatal("open source media space was not detected")
	}
	if !hasOpenFollowingMediaSpace(
		spaces,
		map[string]struct{}{"occ_before": {}, "occ_after": {}},
		map[string]struct{}{"occ_after": {}},
	) {
		t.Fatal("open following occurrence media space was not detected")
	}
	if hasOpenFollowingMediaSpace(
		spaces,
		map[string]struct{}{"occ_before": {}, "occ_after": {}},
		map[string]struct{}{"occ_future": {}},
	) {
		t.Fatal("open occurrence before the following boundary was incorrectly rejected")
	}
	if !hasOpenFollowingMediaSpace(
		[]lockedSourceMediaSpace{{Status: "open", OccurrenceKey: "occ_stale"}},
		map[string]struct{}{"occ_current": {}},
		map[string]struct{}{"occ_current": {}},
	) {
		t.Fatal("stale open occurrence binding must fail closed")
	}
}

func TestClassroomSourceMutationsLockMediaSpacesBeforeAuthorityRows(t *testing.T) {
	t.Parallel()

	assertSourceLockOrder(
		t,
		"postgres_session_repository.go",
		"func (repository *PostgresRepository) CancelSession(",
		[]string{
			"lockClassSessionMediaSpaces(",
			"repository.lockClassMutation(",
			"lockClassSession(",
		},
	)
	assertSourceLockOrder(
		t,
		"postgres_session_series_repository.go",
		"func (repository *PostgresRepository) mutateSeriesOccurrence(",
		[]string{
			"lockSeriesMediaSpaces(",
			"repository.lockClassMutation(",
			"lockClassSessionSeries(",
		},
	)
	assertSourceLockOrder(
		t,
		"postgres_repository.go",
		"func (repository *PostgresRepository) changeArchiveState(",
		[]string{
			"lockClassMediaSpaces(",
			"repository.lockClassMutation(",
		},
	)

	contents, err := os.ReadFile("postgres_media_space_barrier.go")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, fragment := range []string{
		"ORDER BY id\nFOR UPDATE",
		"ORDER BY space.id\nFOR UPDATE OF space",
		"source_kind = 'class_session'",
		"source_kind = 'class_session_occurrence'",
		"space.source_kind = 'study_meeting'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("media source barrier is missing %q", fragment)
		}
	}
}

func assertSourceLockOrder(
	t *testing.T,
	filename string,
	functionMarker string,
	orderedCalls []string,
) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Clean(filename))
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	functionStart := strings.Index(source, functionMarker)
	if functionStart < 0 {
		t.Fatalf("%s is missing %q", filename, functionMarker)
	}
	functionSource := source[functionStart:]
	lastPosition := -1
	for _, call := range orderedCalls {
		position := strings.Index(functionSource, call)
		if position < 0 {
			t.Fatalf("%s is missing %q after %q", filename, call, functionMarker)
		}
		if position <= lastPosition {
			t.Fatalf("%s does not lock in order %v", filename, orderedCalls)
		}
		lastPosition = position
	}
}
