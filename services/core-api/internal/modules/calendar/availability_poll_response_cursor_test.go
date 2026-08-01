package calendar

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

func TestIndividualResponseCursorIsKeysetAndScopeBound(t *testing.T) {
	t.Parallel()

	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	pollID := uuid.New()
	response := AvailabilityPollIndividualResponse{
		ResponseID: uuid.New(),
		createdAt:  time.Date(2026, time.August, 1, 12, 30, 0, 123, time.UTC),
	}
	cursor, err := encodeIndividualResponseCursor(scope, pollID, 25, response)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	decoded, err := decodeIndividualResponseCursor(cursor, scope, pollID, 25)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded.ID != response.ResponseID || !decoded.CreatedAt.Equal(response.createdAt) {
		t.Fatalf("decoded cursor = %+v", decoded)
	}

	for name, decode := range map[string]func() error{
		"other actor": func() error {
			_, err := decodeIndividualResponseCursor(cursor, tenancy.Context{
				TenantID: scope.TenantID, ActorID: uuid.New(),
			}, pollID, 25)
			return err
		},
		"other poll": func() error {
			_, err := decodeIndividualResponseCursor(cursor, scope, uuid.New(), 25)
			return err
		},
		"other limit": func() error {
			_, err := decodeIndividualResponseCursor(cursor, scope, pollID, 10)
			return err
		},
		"malformed": func() error {
			_, err := decodeIndividualResponseCursor("not-a-cursor", scope, pollID, 25)
			return err
		},
		"oversized": func() error {
			_, err := decodeIndividualResponseCursor(
				availabilityPollResponseCursorPrefix+strings.Repeat("a", maximumPollResponseCursorLength),
				scope, pollID, 25,
			)
			return err
		},
	} {
		name, decode := name, decode
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := decode(); !errors.Is(err, ErrAvailabilityPollInvalid) {
				t.Fatalf("error = %v, want invalid poll request", err)
			}
		})
	}
}

func TestIndividualResponseListInputDefaultsAndCapsPageSize(t *testing.T) {
	t.Parallel()

	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	pollID := uuid.New()
	_, limit, err := normalizeIndividualResponseListInput(
		scope, pollID, ListAvailabilityPollResponsesInput{},
	)
	if err != nil || limit != defaultIndividualResponseListLimit {
		t.Fatalf("default limit = %d, error = %v", limit, err)
	}
	for _, limit := range []int{-1, maximumIndividualResponseListLimit + 1} {
		if _, _, err := normalizeIndividualResponseListInput(
			scope, pollID, ListAvailabilityPollResponsesInput{Limit: limit},
		); !errors.Is(err, ErrAvailabilityPollInvalid) {
			t.Fatalf("limit %d error = %v, want invalid poll request", limit, err)
		}
	}
}
