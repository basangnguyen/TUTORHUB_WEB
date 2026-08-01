package calendar

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
)

const (
	pollCapabilitySecretBytes = 32
	publicAggregateCohort     = 3
)

type pollCapabilityToken struct {
	Raw     string
	Version int16
	Digest  [sha256.Size]byte
}

type pollSlotCounts struct {
	Preferred   int
	Available   int
	Unavailable int
}

func generatePollCapabilityToken(
	protector *protecteddata.Protector,
) (pollCapabilityToken, error) {
	return newPollCapabilityToken(protector, rand.Reader)
}

func newPollCapabilityToken(
	protector *protecteddata.Protector,
	random io.Reader,
) (pollCapabilityToken, error) {
	if protector == nil || random == nil {
		return pollCapabilityToken{}, ErrAvailabilityPollCapabilityUnavailable
	}
	secret := make([]byte, pollCapabilitySecretBytes)
	if _, err := io.ReadFull(random, secret); err != nil {
		return pollCapabilityToken{}, fmt.Errorf("generate poll capability: %w", err)
	}
	digest, err := protector.PollCapabilityTokenDigest(secret)
	if err != nil {
		return pollCapabilityToken{}, ErrAvailabilityPollCapabilityUnavailable
	}
	version := protector.KeyVersion()
	return pollCapabilityToken{
		Raw: "v" + strconv.FormatInt(int64(version), 10) + "." +
			base64.RawURLEncoding.EncodeToString(secret),
		Version: version,
		Digest:  digest,
	}, nil
}

func digestPollCapabilityToken(
	protector *protecteddata.Protector,
	raw string,
) (int16, [sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if protector == nil || raw == "" || raw != strings.TrimSpace(raw) {
		return 0, empty, ErrAvailabilityPollCapabilityUnavailable
	}
	prefix, encoded, found := strings.Cut(raw, ".")
	if !found || !strings.HasPrefix(prefix, "v") || strings.Contains(encoded, ".") {
		return 0, empty, ErrAvailabilityPollCapabilityUnavailable
	}
	versionValue, err := strconv.ParseInt(strings.TrimPrefix(prefix, "v"), 10, 16)
	if err != nil || versionValue <= 0 || int16(versionValue) != protector.KeyVersion() {
		return 0, empty, ErrAvailabilityPollCapabilityUnavailable
	}
	secret, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(secret) != pollCapabilitySecretBytes ||
		base64.RawURLEncoding.EncodeToString(secret) != encoded {
		return 0, empty, ErrAvailabilityPollCapabilityUnavailable
	}
	digest, err := protector.PollCapabilityTokenDigest(secret)
	if err != nil {
		return 0, empty, ErrAvailabilityPollCapabilityUnavailable
	}
	return int16(versionValue), digest, nil
}

func rankAvailabilityPollSlots(
	slots []AvailabilityPollSlot,
	counts map[uuidKey]pollSlotCounts,
	responseCount int,
	exposeExact bool,
	exposeAggregate bool,
) []AvailabilityPollRankedSlot {
	type candidate struct {
		slot   AvailabilityPollSlot
		counts pollSlotCounts
	}
	candidates := make([]candidate, 0, len(slots))
	for _, slot := range slots {
		candidates = append(candidates, candidate{slot: slot, counts: counts[uuidKey(slot.ID)]})
	}
	cohortSatisfied := responseCount >= publicAggregateCohort
	aggregateVisible := exposeExact || (exposeAggregate && cohortSatisfied)
	sort.Slice(candidates, func(left, right int) bool {
		l, r := candidates[left], candidates[right]
		// The ordering itself is aggregate information. Before the privacy
		// cohort is satisfied, keep the public/responder projection in civil
		// time order so low-cardinality answers cannot be inferred from rank.
		if aggregateVisible {
			if l.counts.Unavailable != r.counts.Unavailable {
				return l.counts.Unavailable < r.counts.Unavailable
			}
			if l.counts.Preferred != r.counts.Preferred {
				return l.counts.Preferred > r.counts.Preferred
			}
			if l.counts.Available != r.counts.Available {
				return l.counts.Available > r.counts.Available
			}
		}
		if !l.slot.StartsAt.Equal(r.slot.StartsAt) {
			return l.slot.StartsAt.Before(r.slot.StartsAt)
		}
		return l.slot.ID.String() < r.slot.ID.String()
	})
	result := make([]AvailabilityPollRankedSlot, 0, len(candidates))
	for index, candidate := range candidates {
		ranked := AvailabilityPollRankedSlot{
			Slot: candidate.slot, Rank: index + 1,
			CohortSatisfied: (exposeExact || exposeAggregate) && cohortSatisfied,
		}
		if exposeExact {
			unavailable := candidate.counts.Unavailable
			preferred := candidate.counts.Preferred
			available := candidate.counts.Available
			ranked.UnavailableCount = &unavailable
			ranked.PreferredCount = &preferred
			ranked.AvailableCount = &available
		}
		if exposeAggregate && cohortSatisfied {
			bucket := aggregateBucket(candidate.counts, responseCount)
			ranked.AggregateBucket = &bucket
		}
		result = append(result, ranked)
	}
	return result
}

// uuidKey keeps the ranker independent from map key string formatting while
// remaining cheap to construct from database UUIDs.
type uuidKey [16]byte

func aggregateBucket(counts pollSlotCounts, responseCount int) string {
	positive := counts.Preferred + counts.Available
	if responseCount <= 0 || positive*3 < responseCount {
		return "low"
	}
	if positive*3 < responseCount*2 {
		return "medium"
	}
	return "high"
}
