package calendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

type scoredSuggestion struct {
	SuggestedTime
	score [10]int
}

func buildAvailabilityResult(
	ctx context.Context,
	params availabilityParams,
	sources []availabilitySource,
) (AvailabilityResult, error) {
	byParticipant := make(map[string]availabilitySource, len(sources))
	for _, source := range sources {
		key := source.Participant.Reference.Kind + ":" + source.Participant.Reference.ID
		if _, duplicate := byParticipant[key]; duplicate {
			return AvailabilityResult{}, fmt.Errorf("duplicate availability source: %w", ErrSchedulingUnavailable)
		}
		byParticipant[key] = source
	}
	orderedSources := make([]availabilitySource, 0, len(params.Participants))
	participants := make([]ParticipantAvailability, 0, len(params.Participants))
	for _, participant := range params.Participants {
		key := participant.Reference.Kind + ":" + participant.Reference.ID
		source, ok := byParticipant[key]
		if !ok {
			return AvailabilityResult{}, fmt.Errorf("missing availability source: %w", ErrSchedulingUnavailable)
		}
		source.Participant = participant
		normalizeAvailabilitySource(&source, params)
		orderedSources = append(orderedSources, source)
		participants = append(participants, ParticipantAvailability{
			Participant:      participant.Reference,
			Role:             participant.Role,
			Intervals:        source.Intervals,
			WorkingIntervals: source.WorkingIntervals,
		})
	}

	starts, err := availabilityCivilStarts(ctx, params)
	if err != nil {
		return AvailabilityResult{}, err
	}
	scored := make([]scoredSuggestion, 0, len(starts))
	for _, start := range starts {
		if err := ctx.Err(); err != nil {
			return AvailabilityResult{}, err
		}
		end := start.Add(params.Duration)
		breakdown := scoreAvailabilitySlot(start, end, orderedSources)
		score := breakdownScore(breakdown)
		scored = append(scored, scoredSuggestion{
			SuggestedTime: SuggestedTime{
				StartsAt: start, EndsAt: end,
				StableSlotKey:   stableAvailabilitySlotKey(start, end, params.Timezone),
				ReasonBreakdown: breakdown,
			},
			score: score,
		})
	}
	sort.SliceStable(scored, func(left, right int) bool {
		for index := range scored[left].score {
			if scored[left].score[index] != scored[right].score[index] {
				return scored[left].score[index] < scored[right].score[index]
			}
		}
		if !scored[left].StartsAt.Equal(scored[right].StartsAt) {
			return scored[left].StartsAt.Before(scored[right].StartsAt)
		}
		return scored[left].StableSlotKey < scored[right].StableSlotKey
	})
	if len(scored) > params.MaxCandidates {
		scored = scored[:params.MaxCandidates]
	}
	suggestions := make([]SuggestedTime, len(scored))
	for index := range scored {
		suggestions[index] = scored[index].SuggestedTime
	}
	result := AvailabilityResult{
		Timezone:     params.Timezone,
		Participants: participants,
		Suggestions:  suggestions,
	}
	if len(result.Suggestions) == 0 {
		reason := "no_valid_civil_slots"
		result.EmptySuggestionsReason = &reason
	}
	return result, nil
}

func availabilityCivilStarts(
	ctx context.Context,
	params availabilityParams,
) ([]time.Time, error) {
	location, err := time.LoadLocation(params.Timezone)
	if err != nil {
		return nil, ErrInvalidInput
	}
	local := params.From.In(location)
	minute := local.Minute()
	stepMinutes := int(params.Step / time.Minute)
	nextMinute := ((minute + stepMinutes - 1) / stepMinutes) * stepMinutes
	civil := time.Date(
		local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, time.UTC,
	).Add(time.Duration(nextMinute) * time.Minute)
	starts := make([]time.Time, 0, minInt(MaximumAvailabilityStarts, 128))
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved := resolveCivilInstants(civil, location)
		for _, start := range resolved {
			if start.Before(params.From) || start.Add(params.Duration).After(params.To) {
				continue
			}
			if len(starts) >= MaximumAvailabilityStarts {
				return nil, ErrAvailabilityCapacity
			}
			starts = append(starts, start.UTC())
		}
		civil = civil.Add(params.Step)
		if civilDateAfterInstant(civil, params.To, location) {
			break
		}
	}
	sort.Slice(starts, func(left, right int) bool { return starts[left].Before(starts[right]) })
	return starts, nil
}

// resolveCivilInstants returns zero instants for a DST gap, one for an ordinary
// civil time and two for an overlap. It discovers the IANA offsets around the
// target date, then round-trips every candidate instead of trusting
// time.Date's gap normalization.
func resolveCivilInstants(civil time.Time, location *time.Location) []time.Time {
	offsets := make(map[int]struct{}, 2)
	for delta := -48 * time.Hour; delta <= 48*time.Hour; delta += 6 * time.Hour {
		_, offset := civil.Add(delta).In(location).Zone()
		offsets[offset] = struct{}{}
	}
	result := make([]time.Time, 0, len(offsets))
	for offset := range offsets {
		candidate := civil.Add(-time.Duration(offset) * time.Second).UTC()
		local := candidate.In(location)
		if local.Year() != civil.Year() || local.Month() != civil.Month() ||
			local.Day() != civil.Day() || local.Hour() != civil.Hour() ||
			local.Minute() != civil.Minute() || local.Second() != civil.Second() {
			continue
		}
		result = append(result, candidate)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Before(result[right]) })
	return result
}

func civilDateAfterInstant(civil time.Time, instant time.Time, location *time.Location) bool {
	limit := instant.In(location)
	civilDay := civil.Format("2006-01-02")
	limitDay := limit.Format("2006-01-02")
	return civilDay > limitDay || (civilDay == limitDay && civil.Format("15:04") > limit.Format("15:04"))
}

func normalizeAvailabilitySource(source *availabilitySource, params availabilityParams) {
	if source.Unknown {
		source.Intervals = []AvailabilityStatusInterval{{
			StartsAt: params.From, EndsAt: params.To, Status: "unknown",
		}}
		source.WorkingIntervals = []AvailabilityWorkingInterval{}
		return
	}
	intervals := source.Intervals[:0]
	for _, interval := range source.Intervals {
		if interval.EndsAt.After(interval.StartsAt) &&
			interval.EndsAt.After(params.From) && interval.StartsAt.Before(params.To) &&
			isAvailabilityStatus(interval.Status) {
			intervals = append(intervals, interval)
		}
	}
	source.Intervals = intervals
	if source.Intervals == nil {
		source.Intervals = []AvailabilityStatusInterval{}
	}
	if source.WorkingIntervals == nil {
		source.WorkingIntervals = []AvailabilityWorkingInterval{}
	}
}

func scoreAvailabilitySlot(
	start time.Time,
	end time.Time,
	sources []availabilitySource,
) SuggestedTimeReasonBreakdown {
	var result SuggestedTimeReasonBreakdown
	for _, source := range sources {
		status := worstAvailabilityStatus(start, end, source.Intervals)
		outsideWorking := !coveredByWorkingInterval(start, end, source.WorkingIntervals)
		if source.Participant.Role == "required" {
			switch status {
			case "out_of_office":
				result.RequiredOutOfOffice++
			case "busy":
				result.RequiredBusy++
			case "unknown":
				result.RequiredUnknown++
			case "tentative":
				result.RequiredTentative++
			}
			if outsideWorking {
				result.RequiredOutsideWorking++
			}
		} else {
			switch status {
			case "out_of_office":
				result.OptionalOutOfOffice++
			case "busy":
				result.OptionalBusy++
			case "unknown":
				result.OptionalUnknown++
			case "tentative":
				result.OptionalTentative++
			}
			if outsideWorking {
				result.OptionalOutsideWorking++
			}
		}
	}
	return result
}

func breakdownScore(value SuggestedTimeReasonBreakdown) [10]int {
	return [10]int{
		value.RequiredOutOfOffice, value.RequiredBusy, value.RequiredUnknown,
		value.RequiredTentative, value.RequiredOutsideWorking,
		value.OptionalOutOfOffice, value.OptionalBusy, value.OptionalUnknown,
		value.OptionalTentative, value.OptionalOutsideWorking,
	}
}

func worstAvailabilityStatus(
	start time.Time,
	end time.Time,
	intervals []AvailabilityStatusInterval,
) string {
	worst, rank := "free", 0
	for _, interval := range intervals {
		if !interval.StartsAt.Before(end) || !start.Before(interval.EndsAt) {
			continue
		}
		candidateRank := availabilityStatusRank(interval.Status)
		if candidateRank > rank {
			worst, rank = interval.Status, candidateRank
		}
	}
	return worst
}

func coveredByWorkingInterval(
	start time.Time,
	end time.Time,
	intervals []AvailabilityWorkingInterval,
) bool {
	for _, interval := range intervals {
		if !start.Before(interval.StartsAt) && !end.After(interval.EndsAt) {
			return true
		}
	}
	return false
}

func isAvailabilityStatus(status string) bool {
	return status == "free" || status == "tentative" || status == "unknown" ||
		status == "busy" || status == "out_of_office"
}

func availabilityStatusRank(status string) int {
	switch status {
	case "tentative":
		return 1
	case "unknown":
		return 2
	case "busy":
		return 3
	case "out_of_office":
		return 4
	default:
		return 0
	}
}

func stableAvailabilitySlotKey(start time.Time, end time.Time, timezone string) string {
	digest := sha256.Sum256([]byte(start.UTC().Format(time.RFC3339Nano) + "\x00" +
		end.UTC().Format(time.RFC3339Nano) + "\x00" + timezone))
	return "slot_" + hex.EncodeToString(digest[:12])
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
