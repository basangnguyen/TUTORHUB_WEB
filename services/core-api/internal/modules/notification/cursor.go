package notification

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const notificationCursorVersion = 1

type listCursor struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
	ScopeHash string    `json:"scope_hash"`
}

func encodeCursor(scope tenancy.Context, input ListInput, item Notification) (string, error) {
	payload, err := json.Marshal(listCursor{
		Version:   notificationCursorVersion,
		CreatedAt: item.CreatedAt.UTC(),
		ID:        item.ID,
		ScopeHash: cursorScopeHash(scope, input),
	})
	if err != nil {
		return "", ErrInvalidInput
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(scope tenancy.Context, input ListInput) (listCursor, error) {
	if strings.TrimSpace(input.Cursor) == "" {
		return listCursor{}, nil
	}
	if len(input.Cursor) > maximumCursorLength {
		return listCursor{}, ErrInvalidInput
	}
	contents, err := base64.RawURLEncoding.DecodeString(input.Cursor)
	if err != nil {
		return listCursor{}, ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var cursor listCursor
	if err := decoder.Decode(&cursor); err != nil {
		return listCursor{}, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return listCursor{}, ErrInvalidInput
	}
	if cursor.Version != notificationCursorVersion || cursor.ID == uuid.Nil ||
		cursor.CreatedAt.IsZero() || cursor.ScopeHash != cursorScopeHash(scope, input) {
		return listCursor{}, ErrInvalidInput
	}
	return cursor, nil
}

func cursorScopeHash(scope tenancy.Context, input ListInput) string {
	parts := []string{
		scope.TenantID.String(),
		scope.ActorID.String(),
		strconv.FormatBool(input.UnreadOnly),
		strconv.Itoa(input.Limit),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
