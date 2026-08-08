package classroom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type MediaSourceKind string

const (
	MediaSourceClassSession           MediaSourceKind = "class_session"
	MediaSourceClassSessionOccurrence MediaSourceKind = "class_session_occurrence"
)

type MediaSourceReference struct {
	Kind           MediaSourceKind
	ClassSessionID uuid.UUID
	SeriesID       uuid.UUID
	OccurrenceKey  string
}

type MediaSourceSnapshot struct {
	Kind           MediaSourceKind
	ClassID        uuid.UUID
	ClassSessionID uuid.UUID
	SeriesID       uuid.UUID
	OccurrenceKey  string
	Status         string
	StartsAt       time.Time
	EndsAt         time.Time
}

// AuthorizeMediaClass reloads the class, tenant membership, enrollment and
// class state inside the caller's transaction. Media lifecycle uses it for a
// StudyMeeting that is bound to a class so an owner/member projection cannot
// bypass current class visibility or mutate media for an archived class.
func (repository *PostgresRepository) AuthorizeMediaClass(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	classID uuid.UUID,
	action policy.Action,
) (ClassStatus, error) {
	if repository == nil || transaction == nil || scope.Validate() != nil || classID == uuid.Nil {
		return "", ErrClassAccessDenied
	}
	locked, membership, err := repository.lockClassMutation(ctx, transaction, scope, classID)
	if err != nil {
		return "", err
	}
	if err := repository.authorizeLockedClass(scope, membership, locked.Class, action); err != nil {
		return "", err
	}
	return locked.Class.Status, nil
}

// ResolveMediaSource reloads class membership, class state and the exact
// session/occurrence inside the caller's transaction. It deliberately accepts
// a shared transaction so media lifecycle commits cannot race source changes.
func (repository *PostgresRepository) ResolveMediaSource(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	reference MediaSourceReference,
	action policy.Action,
) (MediaSourceSnapshot, error) {
	if repository == nil || transaction == nil || scope.Validate() != nil {
		return MediaSourceSnapshot{}, ErrSessionAccessDenied
	}
	switch reference.Kind {
	case MediaSourceClassSession:
		return repository.resolveOneTimeMediaSource(ctx, transaction, scope, reference, action)
	case MediaSourceClassSessionOccurrence:
		return repository.resolveRecurringMediaSource(ctx, transaction, scope, reference, action)
	default:
		return MediaSourceSnapshot{}, ErrSessionNotFound
	}
}

func (repository *PostgresRepository) resolveOneTimeMediaSource(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	reference MediaSourceReference,
	action policy.Action,
) (MediaSourceSnapshot, error) {
	if reference.ClassSessionID == uuid.Nil || reference.SeriesID != uuid.Nil ||
		strings.TrimSpace(reference.OccurrenceKey) != "" {
		return MediaSourceSnapshot{}, ErrSessionNotFound
	}
	var classID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		`SELECT class_id FROM tutorhub.class_sessions
WHERE tenant_id = $1 AND id = $2 AND series_id IS NULL`,
		scope.TenantID,
		reference.ClassSessionID,
	).Scan(&classID)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaSourceSnapshot{}, ErrSessionNotFound
	}
	if err != nil {
		return MediaSourceSnapshot{}, fmt.Errorf("resolve media class session class: %w", err)
	}
	locked, membership, err := repository.lockClassMutation(ctx, transaction, scope, classID)
	if err != nil {
		return MediaSourceSnapshot{}, err
	}
	if err := repository.authorizeLockedClass(scope, membership, locked.Class, action); err != nil {
		return MediaSourceSnapshot{}, err
	}
	session, err := lockClassSession(
		ctx, transaction, scope.TenantID, classID, reference.ClassSessionID,
	)
	if err != nil {
		return MediaSourceSnapshot{}, err
	}
	if session.Status != SessionStatusScheduled && session.Status != SessionStatusLive &&
		session.Status != SessionStatusEnded && session.Status != SessionStatusCancelled {
		return MediaSourceSnapshot{}, ErrInvalidSessionTransition
	}
	return MediaSourceSnapshot{
		Kind: MediaSourceClassSession, ClassID: classID,
		ClassSessionID: session.ID, Status: string(session.Status),
		StartsAt: session.StartsAt.UTC(), EndsAt: session.EndsAt.UTC(),
	}, nil
}

func (repository *PostgresRepository) resolveRecurringMediaSource(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	reference MediaSourceReference,
	action policy.Action,
) (MediaSourceSnapshot, error) {
	reference.OccurrenceKey = strings.TrimSpace(reference.OccurrenceKey)
	if reference.SeriesID == uuid.Nil || reference.ClassSessionID != uuid.Nil ||
		len(reference.OccurrenceKey) < 8 || len(reference.OccurrenceKey) > 128 {
		return MediaSourceSnapshot{}, ErrSessionNotFound
	}
	var classID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		`SELECT class_id FROM tutorhub.class_session_series
WHERE tenant_id = $1 AND id = $2`,
		scope.TenantID,
		reference.SeriesID,
	).Scan(&classID)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaSourceSnapshot{}, ErrSeriesNotFound
	}
	if err != nil {
		return MediaSourceSnapshot{}, fmt.Errorf("resolve media session series class: %w", err)
	}
	locked, membership, err := repository.lockClassMutation(ctx, transaction, scope, classID)
	if err != nil {
		return MediaSourceSnapshot{}, err
	}
	if err := repository.authorizeLockedClass(scope, membership, locked.Class, action); err != nil {
		return MediaSourceSnapshot{}, err
	}
	series, err := lockClassSessionSeries(
		ctx, transaction, scope.TenantID, classID, reference.SeriesID,
	)
	if err != nil {
		return MediaSourceSnapshot{}, err
	}
	if series.Status != SeriesStatusScheduled {
		return MediaSourceSnapshot{}, ErrInvalidSessionTransition
	}
	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(series))
	if err != nil {
		return MediaSourceSnapshot{}, err
	}
	exceptions, err := listSeriesExceptions(
		ctx, transaction, scope.TenantID, classID, reference.SeriesID,
	)
	if err != nil {
		return MediaSourceSnapshot{}, err
	}
	occurrences, err = applyPersistedExceptions(ctx, series, occurrences, exceptions)
	if err != nil {
		return MediaSourceSnapshot{}, err
	}
	for _, occurrence := range occurrences {
		if occurrence.Key == reference.OccurrenceKey {
			return MediaSourceSnapshot{
				Kind: MediaSourceClassSessionOccurrence, ClassID: classID,
				SeriesID: reference.SeriesID, OccurrenceKey: reference.OccurrenceKey,
				Status: string(SessionStatusScheduled), StartsAt: occurrence.StartsAt.UTC(),
				EndsAt: occurrence.EndsAt.UTC(),
			}, nil
		}
	}
	return MediaSourceSnapshot{}, ErrSessionNotFound
}

// TransitionMediaSession is only valid for a one-time ClassSession already
// locked by ResolveMediaSource. Recurring occurrence lifecycle is projected by
// MediaSpace and never mutates the whole series.
func (repository *PostgresRepository) TransitionMediaSession(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	sessionID uuid.UUID,
	from SessionStatus,
	to SessionStatus,
	now time.Time,
) error {
	if repository == nil || transaction == nil || scope.Validate() != nil ||
		sessionID == uuid.Nil ||
		!((from == SessionStatusScheduled && to == SessionStatusLive) ||
			(from == SessionStatusLive && to == SessionStatusEnded)) {
		return ErrInvalidSessionTransition
	}
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.class_sessions
SET status = $4, version = version + 1, updated_by = $3, updated_at = $5
WHERE tenant_id = $1 AND id = $2 AND status = $6`,
		scope.TenantID,
		sessionID,
		scope.ActorID,
		to,
		now.UTC(),
		from,
	)
	if err != nil {
		return fmt.Errorf("transition media class session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrInvalidSessionTransition
	}
	return nil
}
