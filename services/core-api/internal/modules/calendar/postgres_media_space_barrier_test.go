package calendar

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestStudyMeetingMediaSpaceBarrierDetectsOpenBinding(t *testing.T) {
	t.Parallel()

	if hasOpenStudyMeetingMediaSpace([]lockedStudyMeetingMediaSpace{
		{ID: uuid.New(), Status: "scheduled"},
		{ID: uuid.New(), Status: "ended"},
	}) {
		t.Fatal("terminal or scheduled media spaces must not block StudyMeeting cancellation")
	}
	if !hasOpenStudyMeetingMediaSpace([]lockedStudyMeetingMediaSpace{
		{ID: uuid.New(), Status: "open"},
	}) {
		t.Fatal("open media space was not detected")
	}
}

func TestStudyMeetingCancellationLocksMediaSpaceBeforeSource(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("postgres_availability_poll_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	functionStart := strings.Index(
		source,
		"func (repository *PostgresAvailabilityPollRepository) CancelStudyMeeting(",
	)
	if functionStart < 0 {
		t.Fatal("StudyMeeting cancellation function was not found")
	}
	functionSource := source[functionStart:]
	mediaLock := strings.Index(functionSource, "lockStudyMeetingMediaSpaces(")
	membership := strings.Index(functionSource, "repository.requireActiveMembership(")
	sourceLock := strings.Index(functionSource, "repository.loadStudyMeeting(")
	if mediaLock < 0 || membership < 0 || sourceLock < 0 ||
		!(mediaLock < membership && membership < sourceLock) {
		t.Fatal("StudyMeeting cancellation must lock MediaSpace before authorization/source lock")
	}

	barrier, err := os.ReadFile("postgres_media_space_barrier.go")
	if err != nil {
		t.Fatal(err)
	}
	barrierSource := string(barrier)
	for _, fragment := range []string{
		"source_kind = 'study_meeting'",
		"ORDER BY id\nFOR UPDATE",
	} {
		if !strings.Contains(barrierSource, fragment) {
			t.Fatalf("StudyMeeting media barrier is missing %q", fragment)
		}
	}
}
