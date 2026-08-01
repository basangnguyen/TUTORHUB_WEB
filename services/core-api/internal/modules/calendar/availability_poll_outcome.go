package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var (
	ErrClassSessionOutcomeAccessDenied = errors.New("class session outcome access denied")
	ErrClassSessionOutcomeNotFound     = errors.New("class session outcome class not found")
	ErrClassSessionOutcomeConflict     = errors.New("class session outcome conflict")
	ErrClassSessionOutcomeUnavailable  = errors.New("class session outcome unavailable")
)

type ClassSessionOutcomeInput struct {
	Title       string
	Description string
	StartsAt    time.Time
	EndsAt      time.Time
	Timezone    string
	CreatedAt   time.Time
}

// ClassSessionOutcomeWriter is deliberately transaction-aware: poll finalize
// owns the transaction so a session cannot commit without the poll outcome
// link, idempotency receipt, audit record, and outbox fact.
type ClassSessionOutcomeWriter interface {
	CreatePollOutcomeInTransaction(
		context.Context,
		pgx.Tx,
		tenancy.Context,
		uuid.UUID,
		ClassSessionOutcomeInput,
	) (uuid.UUID, error)
}
