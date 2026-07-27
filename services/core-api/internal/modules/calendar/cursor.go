package calendar

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type cursorPayload struct {
	StartsAt      string `json:"starts_at"`
	SourceType    string `json:"source_type"`
	OccurrenceKey string `json:"occurrence_key"`
	Scope         string `json:"scope"`
}

func encodeCursor(scope tenancy.Context, params listParams, item Item) (string, error) {
	payload, err := json.Marshal(cursorPayload{
		StartsAt:      item.StartsAt.UTC().Format(time.RFC3339Nano),
		SourceType:    item.SourceType,
		OccurrenceKey: item.OccurrenceKey,
		Scope:         cursorScope(scope, params),
	})
	if err != nil {
		return "", err
	}
	return cursorPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(
	value string,
	scope tenancy.Context,
	params listParams,
) (listCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return listCursor{}, nil
	}
	if len(value) > maximumCursorLength || !strings.HasPrefix(value, cursorPrefix) {
		return listCursor{}, ErrInvalidCursor
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, cursorPrefix))
	if err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return listCursor{}, ErrInvalidCursor
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return listCursor{}, ErrInvalidCursor
	}
	if payload.Scope != cursorScope(scope, params) || payload.SourceType != SourceClassSession {
		return listCursor{}, ErrInvalidCursor
	}
	startsAt, err := time.Parse(time.RFC3339Nano, payload.StartsAt)
	if err != nil || startsAt.IsZero() {
		return listCursor{}, ErrInvalidCursor
	}
	occurrenceKey := strings.TrimSpace(payload.OccurrenceKey)
	if len(occurrenceKey) < 8 || len(occurrenceKey) > 128 {
		return listCursor{}, ErrInvalidCursor
	}
	return listCursor{
		StartsAt:      startsAt.UTC(),
		SourceType:    payload.SourceType,
		OccurrenceKey: occurrenceKey,
	}, nil
}

func cursorScope(scope tenancy.Context, params listParams) string {
	classIDs := make([]string, 0, len(params.ClassIDs))
	for _, classID := range params.ClassIDs {
		classIDs = append(classIDs, classID.String())
	}
	value := strings.Join([]string{
		scope.TenantID.String(),
		scope.ActorID.String(),
		params.From.UTC().Format(time.RFC3339Nano),
		params.To.UTC().Format(time.RFC3339Nano),
		strings.Join(params.Types, ","),
		strings.Join(classIDs, ","),
		strings.Join(params.Statuses, ","),
		params.Search,
		params.ViewerTimezone,
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
