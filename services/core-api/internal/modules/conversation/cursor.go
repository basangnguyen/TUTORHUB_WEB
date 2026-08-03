package conversation

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

const cursorPrefix = "thcv1_"

type listCursor struct {
	UpdatedAt time.Time
	ID        uuid.UUID
}

type cursorPayload struct {
	UpdatedAt string `json:"updated_at"`
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	ScopeHash string `json:"scope_hash"`
}

func encodeCursor(tenantID uuid.UUID, input ListInput, cursor listCursor) (string, error) {
	if tenantID == uuid.Nil || cursor.ID == uuid.Nil || cursor.UpdatedAt.IsZero() {
		return "", ErrInvalidInput
	}
	kind := cursorKind(input.Kind)
	contents, err := json.Marshal(cursorPayload{
		UpdatedAt: cursor.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ID:        cursor.ID.String(),
		Kind:      kind,
		ScopeHash: cursorScopeHash(tenantID, kind),
	})
	if err != nil {
		return "", err
	}
	return cursorPrefix + base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeCursor(tenantID uuid.UUID, input ListInput) (listCursor, error) {
	value := strings.TrimSpace(input.Cursor)
	if value == "" {
		return listCursor{}, nil
	}
	if tenantID == uuid.Nil || len(value) > maximumCursorLength ||
		!strings.HasPrefix(value, cursorPrefix) {
		return listCursor{}, ErrInvalidInput
	}
	contents, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, cursorPrefix))
	if err != nil {
		return listCursor{}, ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return listCursor{}, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return listCursor{}, ErrInvalidInput
	}
	kind := cursorKind(input.Kind)
	if payload.Kind != kind || payload.ScopeHash != cursorScopeHash(tenantID, kind) {
		return listCursor{}, ErrInvalidInput
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, payload.UpdatedAt)
	if err != nil || updatedAt.IsZero() {
		return listCursor{}, ErrInvalidInput
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil || id == uuid.Nil {
		return listCursor{}, ErrInvalidInput
	}
	return listCursor{UpdatedAt: updatedAt.UTC(), ID: id}, nil
}

func cursorKind(kind *Kind) string {
	if kind == nil {
		return ""
	}
	return string(*kind)
}

func cursorScopeHash(tenantID uuid.UUID, kind string) string {
	digest := sha256.Sum256([]byte(tenantID.String() + "\x00" + kind))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
