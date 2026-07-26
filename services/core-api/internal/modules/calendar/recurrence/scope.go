package recurrence

import (
	"errors"
	"fmt"
)

var ErrOccurrenceNotInSeries = errors.New("occurrence is not in the expanded series")

type EditScope string

const (
	ScopeThisOccurrence EditScope = "this_occurrence"
	ScopeFollowing      EditScope = "this_and_following"
	ScopeEntireSeries   EditScope = "entire_series"
)

type FutureExceptionPolicy string

const (
	ExceptionCarry   FutureExceptionPolicy = "carry"
	ExceptionRebase  FutureExceptionPolicy = "rebase"
	ExceptionDiscard FutureExceptionPolicy = "discard"
)

type ExceptionType string

const (
	ExceptionCancel   ExceptionType = "cancel"
	ExceptionOverride ExceptionType = "override"
)

type Exception struct {
	OccurrenceKey string
	Type          ExceptionType
}

type ScopePreview struct {
	Scope                   EditScope
	BoundaryOccurrenceKey   string
	BoundaryOriginalLocal   string
	AffectedOccurrenceCount int
	FutureExceptionCount    int
	RetainedExceptionCount  int
	DiscardedExceptionCount int
	FutureExceptionPolicy   FutureExceptionPolicy
}

// PreviewScope is side-effect free. The mutation layer must persist the chosen
// policy and re-expand/re-check conflicts inside the write transaction.
func PreviewScope(
	occurrences []Occurrence,
	exceptions []Exception,
	boundaryKey string,
	scope EditScope,
	policy FutureExceptionPolicy,
) (ScopePreview, error) {
	boundary := -1
	for index, occurrence := range occurrences {
		if occurrence.Key == boundaryKey {
			boundary = index
			break
		}
	}
	if boundary < 0 {
		return ScopePreview{}, ErrOccurrenceNotInSeries
	}
	if scope != ScopeThisOccurrence && scope != ScopeFollowing && scope != ScopeEntireSeries {
		return ScopePreview{}, fmt.Errorf("%w: unsupported edit scope %q", ErrInvalidRule, scope)
	}
	if scope == ScopeFollowing && !validFutureExceptionPolicy(policy) {
		return ScopePreview{}, fmt.Errorf(
			"%w: following edits require carry, rebase, or discard",
			ErrInvalidRule,
		)
	}
	if scope != ScopeFollowing && policy != "" {
		return ScopePreview{}, fmt.Errorf(
			"%w: exception policy only applies to following edits",
			ErrInvalidRule,
		)
	}

	futureKeys := make(map[string]struct{}, len(occurrences)-boundary)
	for _, occurrence := range occurrences[boundary:] {
		futureKeys[occurrence.Key] = struct{}{}
	}
	futureExceptions := 0
	for _, exception := range exceptions {
		if _, future := futureKeys[exception.OccurrenceKey]; future {
			futureExceptions++
		}
	}

	preview := ScopePreview{
		Scope:                 scope,
		BoundaryOccurrenceKey: boundaryKey,
		BoundaryOriginalLocal: occurrences[boundary].OriginalLocal,
	}
	switch scope {
	case ScopeThisOccurrence:
		preview.AffectedOccurrenceCount = 1
		for _, exception := range exceptions {
			if exception.OccurrenceKey == boundaryKey {
				preview.RetainedExceptionCount = 1
				break
			}
		}
	case ScopeFollowing:
		preview.AffectedOccurrenceCount = len(occurrences) - boundary
		preview.FutureExceptionCount = futureExceptions
		preview.FutureExceptionPolicy = policy
		if policy == ExceptionDiscard {
			preview.DiscardedExceptionCount = futureExceptions
		} else {
			preview.RetainedExceptionCount = futureExceptions
		}
	case ScopeEntireSeries:
		preview.AffectedOccurrenceCount = len(occurrences)
		preview.FutureExceptionCount = futureExceptions
		preview.RetainedExceptionCount = len(exceptions)
	}
	return preview, nil
}

func validFutureExceptionPolicy(policy FutureExceptionPolicy) bool {
	return policy == ExceptionCarry || policy == ExceptionRebase || policy == ExceptionDiscard
}
