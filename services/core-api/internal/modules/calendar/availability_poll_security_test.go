package calendar

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
)

func TestPollCapabilityTokenRoundTripUsesVersionedOpaqueSecret(t *testing.T) {
	t.Parallel()

	protector, err := protecteddata.New(protecteddata.Config{
		Key:        bytes.Repeat([]byte{0x52}, 32),
		KeyVersion: 7,
	})
	if err != nil {
		t.Fatalf("create protected-data helper: %v", err)
	}
	token, err := newPollCapabilityToken(
		protector,
		bytes.NewReader(bytes.Repeat([]byte{0xa6}, pollCapabilitySecretBytes)),
	)
	if err != nil {
		t.Fatalf("create poll capability: %v", err)
	}
	if !strings.HasPrefix(token.Raw, "v7.") {
		t.Fatalf("capability is not versioned: %q", token.Raw)
	}
	if strings.Contains(token.Raw, "=") {
		t.Fatalf("capability must use canonical unpadded base64url: %q", token.Raw)
	}

	version, digest, err := digestPollCapabilityToken(protector, token.Raw)
	if err != nil {
		t.Fatalf("digest generated capability: %v", err)
	}
	if version != token.Version || !bytes.Equal(digest[:], token.Digest[:]) {
		t.Fatal("generated capability did not round-trip to its persisted digest")
	}
	if bytes.Contains(token.Digest[:], bytes.Repeat([]byte{0xa6}, pollCapabilitySecretBytes)) {
		t.Fatal("persisted digest must not contain the raw capability secret")
	}
}

func TestPollCapabilityTokenRejectsNonCanonicalOrWrongVersionInput(t *testing.T) {
	t.Parallel()

	protector, err := protecteddata.New(protecteddata.Config{
		Key:        bytes.Repeat([]byte{0x39}, 32),
		KeyVersion: 3,
	})
	if err != nil {
		t.Fatalf("create protected-data helper: %v", err)
	}
	valid, err := newPollCapabilityToken(
		protector,
		bytes.NewReader(bytes.Repeat([]byte{0x41}, pollCapabilitySecretBytes)),
	)
	if err != nil {
		t.Fatalf("create valid poll capability: %v", err)
	}
	for name, raw := range map[string]string{
		"blank":         "",
		"outer-space":   " " + valid.Raw,
		"wrong-version": strings.Replace(valid.Raw, "v3.", "v4.", 1),
		"missing-dot":   strings.Replace(valid.Raw, ".", "", 1),
		"extra-segment": valid.Raw + ".extra",
		"padded":        valid.Raw + "=",
		"short-secret":  "v3.YQ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := digestPollCapabilityToken(protector, raw); !errors.Is(
				err, ErrAvailabilityPollCapabilityUnavailable,
			) {
				t.Fatalf("expected uniform capability-unavailable error, got %v", err)
			}
		})
	}
}

func TestRankAvailabilityPollSlotsIsDeterministicAndPrivacyBounded(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 3, 8, 0, 0, 0, time.UTC)
	lowID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	mediumID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	highID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	slots := []AvailabilityPollSlot{
		{ID: lowID, StartsAt: base, EndsAt: base.Add(time.Hour), Ordinal: 0},
		{ID: highID, StartsAt: base.Add(2 * time.Hour), EndsAt: base.Add(3 * time.Hour), Ordinal: 2},
		{ID: mediumID, StartsAt: base.Add(time.Hour), EndsAt: base.Add(2 * time.Hour), Ordinal: 1},
	}
	counts := map[uuidKey]pollSlotCounts{
		uuidKey(lowID):    {Preferred: 1, Unavailable: 3},
		uuidKey(mediumID): {Preferred: 1, Available: 1, Unavailable: 2},
		uuidKey(highID):   {Preferred: 1, Available: 2, Unavailable: 1},
	}

	belowCohort := rankAvailabilityPollSlots(slots, counts, 2, false, true)
	for _, ranked := range belowCohort {
		if ranked.CohortSatisfied || ranked.AggregateBucket != nil ||
			ranked.PreferredCount != nil || ranked.AvailableCount != nil ||
			ranked.UnavailableCount != nil {
			t.Fatalf("below-cohort public ranking leaked aggregate detail: %+v", ranked)
		}
	}
	preResponsePublic := rankAvailabilityPollSlots(slots, counts, 4, false, false)
	for _, slot := range preResponsePublic {
		if slot.CohortSatisfied || slot.AggregateBucket != nil ||
			slot.PreferredCount != nil || slot.AvailableCount != nil ||
			slot.UnavailableCount != nil {
			t.Fatalf("pre-response public projection leaked cohort state: %+v", slot)
		}
	}

	ranked := rankAvailabilityPollSlots(slots, counts, 4, true, true)
	if len(ranked) != 3 {
		t.Fatalf("expected three ranked slots, got %d", len(ranked))
	}
	for index, expectedID := range []uuid.UUID{highID, mediumID, lowID} {
		if ranked[index].Slot.ID != expectedID || ranked[index].Rank != index+1 {
			t.Fatalf("unexpected deterministic rank at %d: %+v", index, ranked[index])
		}
	}
	for index, expectedBucket := range []string{"high", "medium", "low"} {
		if ranked[index].AggregateBucket == nil || *ranked[index].AggregateBucket != expectedBucket {
			t.Fatalf("rank %d: expected %q bucket, got %+v", index+1, expectedBucket, ranked[index].AggregateBucket)
		}
		if ranked[index].PreferredCount == nil || ranked[index].AvailableCount == nil ||
			ranked[index].UnavailableCount == nil {
			t.Fatalf("organizer exact projection is missing bounded counts: %+v", ranked[index])
		}
	}
}

func TestIndividualResponseVisibilityRequiresExplicitManagementCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		authorization      pollAuthorization
		pollHasClass       bool
		canScheduleSession bool
		want               bool
	}{
		{name: "owner", authorization: pollAuthorization{Owner: true}, want: true},
		{name: "safety admin", authorization: pollAuthorization{SafetyAdmin: true}, want: true},
		{
			name:          "visible class teacher with scheduling capability",
			authorization: pollAuthorization{ClassMember: true},
			pollHasClass:  true, canScheduleSession: true, want: true,
		},
		{
			name:          "ordinary class participant",
			authorization: pollAuthorization{ClassMember: true},
			pollHasClass:  true,
		},
		{
			name:               "scheduler outside a class poll",
			authorization:      pollAuthorization{ClassMember: true},
			canScheduleSession: true,
		},
		{name: "ordinary invited participant", authorization: pollAuthorization{}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := canViewPollIndividualResponses(
				test.authorization, test.pollHasClass, test.canScheduleSession,
			); got != test.want {
				t.Fatalf("visibility = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOrdinaryPollAndPublicProjectionCannotSerializeIndividualResponses(t *testing.T) {
	t.Parallel()

	for name, projection := range map[string]any{
		"authenticated poll detail": AvailabilityPoll{
			Slots: []AvailabilityPollSlot{}, Participants: []AvailabilityPollParticipant{},
		},
		"public poll": PublicAvailabilityPoll{
			Slots: []AvailabilityPollSlot{}, RankedSlots: []AvailabilityPollRankedSlot{},
		},
	} {
		name, projection := name, projection
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			contents, err := json.Marshal(projection)
			if err != nil {
				t.Fatalf("marshal projection: %v", err)
			}
			if bytes.Contains(contents, []byte(`"individual_responses":`)) {
				t.Fatalf("projection leaked individual responses: %s", contents)
			}
		})
	}
}

func TestResponseSessionHardCapReusesRevokedUnusedRowsBeforeInsert(t *testing.T) {
	t.Parallel()

	if maximumPollResponseSessions != 500 || maximumPollResponseSessionHistory != 1000 {
		t.Fatalf(
			"response-session bounds = active:%d history:%d",
			maximumPollResponseSessions,
			maximumPollResponseSessionHistory,
		)
	}
	contents, err := os.ReadFile("postgres_availability_poll_repository.go")
	if err != nil {
		t.Fatalf("read repository source: %v", err)
	}
	source := string(contents)
	for _, fragment := range []string{
		"count(*) FILTER (WHERE revoked_at IS NULL AND expires_at > $3)",
		"totalResponseSessions >= maximumPollResponseSessionHistory",
		"candidate.revoked_at IS NOT NULL OR candidate.expires_at <= $9",
		"response.response_capability_id = candidate.id",
		"FOR UPDATE SKIP LOCKED",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("response-session hard-cap path is missing %q", fragment)
		}
	}
}
