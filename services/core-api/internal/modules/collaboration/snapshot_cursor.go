package collaboration

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
)

const (
	snapshotCursorPrefix        = "thwbsv1_"
	maximumSnapshotCursorLength = 1024
)

type snapshotCursorPayload struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
	ScopeHash string `json:"scope_hash"`
}

func encodeSnapshotCursor(
	access AccessContext,
	document Document,
	limit int,
	cursor SnapshotPageCursor,
) (string, error) {
	if !validAccess(access) || document.ID == uuid.Nil || document.CurrentGeneration < 1 ||
		limit < 1 || cursor.ID == uuid.Nil || cursor.CreatedAt.IsZero() {
		return "", ErrInvalidRequest
	}
	contents, err := json.Marshal(snapshotCursorPayload{
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        cursor.ID.String(),
		ScopeHash: snapshotCursorScopeHash(access, document, limit),
	})
	if err != nil {
		return "", ErrInvalidRequest
	}
	return snapshotCursorPrefix + base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeSnapshotCursor(
	access AccessContext,
	document Document,
	limit int,
	value string,
) (SnapshotPageCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return SnapshotPageCursor{}, nil
	}
	if !validAccess(access) || document.ID == uuid.Nil || document.CurrentGeneration < 1 ||
		limit < 1 || len(value) > maximumSnapshotCursorLength ||
		!strings.HasPrefix(value, snapshotCursorPrefix) {
		return SnapshotPageCursor{}, ErrInvalidRequest
	}
	contents, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, snapshotCursorPrefix))
	if err != nil {
		return SnapshotPageCursor{}, ErrInvalidRequest
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var payload snapshotCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return SnapshotPageCursor{}, ErrInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) ||
		payload.ScopeHash != snapshotCursorScopeHash(access, document, limit) {
		return SnapshotPageCursor{}, ErrInvalidRequest
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return SnapshotPageCursor{}, ErrInvalidRequest
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil || id == uuid.Nil {
		return SnapshotPageCursor{}, ErrInvalidRequest
	}
	return SnapshotPageCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}

func snapshotCursorScopeHash(access AccessContext, document Document, limit int) string {
	parts := []string{
		access.TenantID.String(),
		access.ActorID.String(),
		document.ID.String(),
		strconv.FormatInt(document.CurrentGeneration, 10),
		strconv.Itoa(limit),
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
