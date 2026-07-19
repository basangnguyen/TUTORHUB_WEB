package audit

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const cursorVersion = 1

type cursorPayload struct {
	Version    int       `json:"v"`
	OccurredAt time.Time `json:"occurred_at"`
	ID         uuid.UUID `json:"id"`
	ScopeHash  string    `json:"scope_hash"`
}

func encodeCursor(tenantID uuid.UUID, filter Filter, event Event) (string, error) {
	payload, err := json.Marshal(cursorPayload{
		Version:    cursorVersion,
		OccurredAt: event.OccurredAt.UTC(),
		ID:         event.ID,
		ScopeHash:  filterScopeHash(tenantID, filter),
	})
	if err != nil {
		return "", fmt.Errorf("encode audit cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(tenantID uuid.UUID, filter Filter) (cursorPayload, error) {
	if strings.TrimSpace(filter.Cursor) == "" {
		return cursorPayload{}, nil
	}
	if len(filter.Cursor) > 1024 {
		return cursorPayload{}, ErrInvalidFilter
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
	if err != nil {
		return cursorPayload{}, ErrInvalidFilter
	}
	decoder := json.NewDecoder(strings.NewReader(string(payloadBytes)))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return cursorPayload{}, ErrInvalidFilter
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return cursorPayload{}, ErrInvalidFilter
	}
	if payload.Version != cursorVersion || payload.ID == uuid.Nil || payload.OccurredAt.IsZero() ||
		payload.ScopeHash != filterScopeHash(tenantID, filter) {
		return cursorPayload{}, ErrInvalidFilter
	}
	return payload, nil
}

func filterScopeHash(tenantID uuid.UUID, filter Filter) string {
	parts := []string{
		tenantID.String(),
		formatFilterTime(filter.OccurredFrom),
		formatFilterTime(filter.OccurredTo),
		string(filter.Action),
		filter.ResourceType,
		filter.ResourceID.String(),
		string(filter.Outcome),
		strconv.Itoa(filter.Limit),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func formatFilterTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
