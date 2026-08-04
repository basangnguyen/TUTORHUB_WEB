package conversation

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/google/uuid"
)

const messageCursorPrefix = "thmsgv1_"

type messageCursor struct {
	BeforeSequence int64
}

type messageCursorPayload struct {
	BeforeSequence int64  `json:"before_sequence"`
	ScopeHash      string `json:"scope_hash"`
}

func encodeMessageCursor(
	tenantID, conversationID uuid.UUID,
	cursor messageCursor,
) (string, error) {
	if tenantID == uuid.Nil || conversationID == uuid.Nil || cursor.BeforeSequence < 1 {
		return "", ErrInvalidInput
	}
	contents, err := json.Marshal(messageCursorPayload{
		BeforeSequence: cursor.BeforeSequence,
		ScopeHash:      messageCursorScopeHash(tenantID, conversationID),
	})
	if err != nil {
		return "", err
	}
	return messageCursorPrefix + base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeMessageCursor(
	tenantID, conversationID uuid.UUID,
	value string,
) (messageCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return messageCursor{}, nil
	}
	if tenantID == uuid.Nil || conversationID == uuid.Nil ||
		len(value) > maximumMessageCursorLength ||
		!strings.HasPrefix(value, messageCursorPrefix) {
		return messageCursor{}, ErrInvalidInput
	}
	contents, err := base64.RawURLEncoding.DecodeString(
		strings.TrimPrefix(value, messageCursorPrefix),
	)
	if err != nil {
		return messageCursor{}, ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var payload messageCursorPayload
	if err := decoder.Decode(&payload); err != nil {
		return messageCursor{}, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return messageCursor{}, ErrInvalidInput
	}
	if payload.BeforeSequence < 1 ||
		payload.ScopeHash != messageCursorScopeHash(tenantID, conversationID) {
		return messageCursor{}, ErrInvalidInput
	}
	return messageCursor{BeforeSequence: payload.BeforeSequence}, nil
}

func messageCursorScopeHash(tenantID, conversationID uuid.UUID) string {
	digest := sha256.Sum256([]byte(
		tenantID.String() + "\x00" + conversationID.String(),
	))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
