package calendar

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	availabilityPollResponseCursorPrefix = "thapir1_"
	maximumPollResponseCursorLength      = 512
	defaultIndividualResponseListLimit   = 25
	maximumIndividualResponseListLimit   = 25
	individualResponseCursorVersion      = 1
)

type individualResponseCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type individualResponseCursorPayload struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
	ScopeHash string `json:"scope_hash"`
}

func normalizeIndividualResponseListInput(
	scope tenancy.Context,
	pollID uuid.UUID,
	input ListAvailabilityPollResponsesInput,
) (individualResponseCursor, int, error) {
	limit := input.Limit
	if limit == 0 {
		limit = defaultIndividualResponseListLimit
	}
	if limit < 1 || limit > maximumIndividualResponseListLimit {
		return individualResponseCursor{}, 0, ErrAvailabilityPollInvalid
	}
	cursor, err := decodeIndividualResponseCursor(
		strings.TrimSpace(input.Cursor), scope, pollID, limit,
	)
	if err != nil {
		return individualResponseCursor{}, 0, err
	}
	return cursor, limit, nil
}

func encodeIndividualResponseCursor(
	scope tenancy.Context,
	pollID uuid.UUID,
	limit int,
	response AvailabilityPollIndividualResponse,
) (string, error) {
	if response.ResponseID == uuid.Nil || response.createdAt.IsZero() {
		return "", ErrAvailabilityPollInvalid
	}
	payload, err := json.Marshal(individualResponseCursorPayload{
		Version:   individualResponseCursorVersion,
		CreatedAt: response.createdAt.UTC().Format(time.RFC3339Nano),
		ID:        response.ResponseID.String(),
		ScopeHash: individualResponseScopeHash(scope, pollID, limit),
	})
	if err != nil {
		return "", ErrAvailabilityPollInvalid
	}
	return availabilityPollResponseCursorPrefix +
		base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeIndividualResponseCursor(
	value string,
	scope tenancy.Context,
	pollID uuid.UUID,
	limit int,
) (individualResponseCursor, error) {
	if value == "" {
		return individualResponseCursor{}, nil
	}
	if len(value) > maximumPollResponseCursorLength ||
		!strings.HasPrefix(value, availabilityPollResponseCursorPrefix) {
		return individualResponseCursor{}, ErrAvailabilityPollInvalid
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(value, availabilityPollResponseCursorPrefix),
	)
	if err != nil {
		return individualResponseCursor{}, ErrAvailabilityPollInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(payloadBytes)))
	decoder.DisallowUnknownFields()
	var payload individualResponseCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return individualResponseCursor{}, ErrAvailabilityPollInvalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return individualResponseCursor{}, ErrAvailabilityPollInvalid
	}
	if payload.Version != individualResponseCursorVersion ||
		payload.ScopeHash != individualResponseScopeHash(scope, pollID, limit) {
		return individualResponseCursor{}, ErrAvailabilityPollInvalid
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return individualResponseCursor{}, ErrAvailabilityPollInvalid
	}
	responseID, err := uuid.Parse(payload.ID)
	if err != nil || responseID == uuid.Nil {
		return individualResponseCursor{}, ErrAvailabilityPollInvalid
	}
	return individualResponseCursor{CreatedAt: createdAt.UTC(), ID: responseID}, nil
}

func individualResponseScopeHash(
	scope tenancy.Context,
	pollID uuid.UUID,
	limit int,
) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		scope.TenantID.String(),
		scope.ActorID.String(),
		pollID.String(),
		strconv.Itoa(limit),
	}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
