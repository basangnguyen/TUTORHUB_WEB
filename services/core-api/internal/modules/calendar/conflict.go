package calendar

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrClassScheduleConflict = errors.New("class schedule conflict")

// BusyInterval is the minimal class-scoped resource projection used by the
// authoritative conflict check. It intentionally contains no attendee data;
// teacher/student free-busy belongs to P3-02C.
type BusyInterval struct {
	ID       string
	ClassID  uuid.UUID
	StartsAt time.Time
	EndsAt   time.Time
	Source   string
}

type Conflict struct {
	CandidateID string
	ExistingID  string
	ClassID     uuid.UUID
	StartsAt    time.Time
	EndsAt      time.Time
}

// Overlaps implements the calendar contract's half-open interval semantics:
// [start,end) conflicts only when start < otherEnd and otherStart < end.
func Overlaps(leftStart, leftEnd, rightStart, rightEnd time.Time) bool {
	return leftStart.Before(rightEnd) && rightStart.Before(leftEnd)
}

// DetectClassConflicts returns conflicts in stable candidate / existing order.
// Cancelled or malformed intervals are ignored by the caller's projection and
// cannot create a false hard block here.
func DetectClassConflicts(candidates, existing []BusyInterval) []Conflict {
	conflicts := make([]Conflict, 0)
	for _, candidate := range candidates {
		if candidate.ClassID == uuid.Nil || !validInterval(candidate.StartsAt, candidate.EndsAt) {
			continue
		}
		for _, other := range existing {
			if other.ClassID != candidate.ClassID ||
				!validInterval(other.StartsAt, other.EndsAt) ||
				!Overlaps(candidate.StartsAt, candidate.EndsAt, other.StartsAt, other.EndsAt) {
				continue
			}
			conflicts = append(conflicts, Conflict{
				CandidateID: candidate.ID,
				ExistingID:  other.ID,
				ClassID:     candidate.ClassID,
				StartsAt:    later(candidate.StartsAt, other.StartsAt),
				EndsAt:      earlier(candidate.EndsAt, other.EndsAt),
			})
		}
	}
	sort.SliceStable(conflicts, func(left, right int) bool {
		if conflicts[left].CandidateID != conflicts[right].CandidateID {
			return conflicts[left].CandidateID < conflicts[right].CandidateID
		}
		if conflicts[left].StartsAt.Equal(conflicts[right].StartsAt) {
			return conflicts[left].ExistingID < conflicts[right].ExistingID
		}
		return conflicts[left].StartsAt.Before(conflicts[right].StartsAt)
	})
	return conflicts
}

// RequireNoClassConflict permits a hard-conflict override only when the caller
// has already established the dedicated capability and supplied an auditable
// reason. A reason by itself must never become authorization.
func RequireNoClassConflict(
	conflicts []Conflict,
	canOverride bool,
	overrideReason string,
) error {
	if len(conflicts) == 0 {
		return nil
	}
	if canOverride && strings.TrimSpace(overrideReason) != "" {
		return nil
	}
	return ErrClassScheduleConflict
}

func validInterval(start, end time.Time) bool {
	return !start.IsZero() && !end.IsZero() && start.Before(end)
}

func later(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func earlier(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
