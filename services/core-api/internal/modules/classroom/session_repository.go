package classroom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var (
	ErrSessionNotFound               = errors.New("class session not found")
	ErrInvalidSessionInput           = errors.New("invalid class session input")
	ErrInvalidSessionTimezone        = errors.New("invalid class session timezone")
	ErrSessionDSTGap                 = errors.New("class session time is in a timezone gap")
	ErrSessionTimezoneOffsetMismatch = errors.New("class session timezone offset mismatch")
	ErrInvalidSessionRange           = errors.New("invalid class session range")
	ErrInvalidSessionListLimit       = errors.New("invalid class session list limit")
	ErrInvalidSessionCursor          = errors.New("invalid class session cursor")
	ErrSessionAccessDenied           = errors.New("class session access denied")
	ErrSessionVersionConflict        = errors.New("class session version is stale")
	ErrInvalidSessionTransition      = errors.New("invalid class session state transition")
)

type SessionRepository interface {
	CreateSession(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		CreateSessionParams,
		time.Time,
	) (ClassSession, error)
	GetSession(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
	) (ClassSession, error)
	ListSessions(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		ListSessionsParams,
	) (ListSessionsResult, error)
	UpdateSession(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		UpdateSessionParams,
		time.Time,
	) (ClassSession, error)
	CancelSession(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		CancelSessionParams,
		time.Time,
	) (ClassSession, error)
}
