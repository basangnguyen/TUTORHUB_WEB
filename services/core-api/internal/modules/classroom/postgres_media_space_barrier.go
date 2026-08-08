package classroom

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type lockedSourceMediaSpace struct {
	ID            uuid.UUID
	Status        string
	OccurrenceKey string
}

func lockClassSessionMediaSpaces(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	sessionID uuid.UUID,
) ([]lockedSourceMediaSpace, error) {
	return queryAndLockSourceMediaSpaces(
		ctx,
		transaction,
		`SELECT id, status, COALESCE(source_occurrence_key, '')
FROM tutorhub.media_spaces
WHERE tenant_id = $1
  AND source_kind = 'class_session'
  AND source_class_session_id = $2
ORDER BY id
FOR UPDATE`,
		tenantID,
		sessionID,
	)
}

func lockSeriesMediaSpaces(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	seriesID uuid.UUID,
	occurrenceKey *string,
) ([]lockedSourceMediaSpace, error) {
	return queryAndLockSourceMediaSpaces(
		ctx,
		transaction,
		`SELECT id, status, COALESCE(source_occurrence_key, '')
FROM tutorhub.media_spaces
WHERE tenant_id = $1
  AND source_kind = 'class_session_occurrence'
  AND source_series_id = $2
  AND ($3::text IS NULL OR source_occurrence_key = $3)
ORDER BY id
FOR UPDATE`,
		tenantID,
		seriesID,
		occurrenceKey,
	)
}

func lockClassMediaSpaces(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
) ([]lockedSourceMediaSpace, error) {
	return queryAndLockSourceMediaSpaces(
		ctx,
		transaction,
		`SELECT space.id, space.status, COALESCE(space.source_occurrence_key, '')
FROM tutorhub.media_spaces AS space
LEFT JOIN tutorhub.study_meetings AS meeting
  ON meeting.tenant_id = space.tenant_id
 AND meeting.id = space.source_study_meeting_id
WHERE space.tenant_id = $1
  AND (
      space.class_id = $2
      OR (
          space.source_kind = 'study_meeting'
          AND meeting.class_id = $2
      )
  )
ORDER BY space.id
FOR UPDATE OF space`,
		tenantID,
		classID,
	)
}

func queryAndLockSourceMediaSpaces(
	ctx context.Context,
	transaction pgx.Tx,
	query string,
	arguments ...any,
) ([]lockedSourceMediaSpace, error) {
	rows, err := transaction.Query(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("lock source media spaces: %w", err)
	}
	defer rows.Close()

	spaces := make([]lockedSourceMediaSpace, 0)
	for rows.Next() {
		var space lockedSourceMediaSpace
		if err := rows.Scan(&space.ID, &space.Status, &space.OccurrenceKey); err != nil {
			return nil, fmt.Errorf("scan locked source media space: %w", err)
		}
		spaces = append(spaces, space)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked source media spaces: %w", err)
	}
	return spaces, nil
}

func hasOpenSourceMediaSpace(spaces []lockedSourceMediaSpace) bool {
	for _, space := range spaces {
		if space.Status == "open" {
			return true
		}
	}
	return false
}

func hasOpenFollowingMediaSpace(
	spaces []lockedSourceMediaSpace,
	allOccurrenceKeys map[string]struct{},
	followingOccurrenceKeys map[string]struct{},
) bool {
	for _, space := range spaces {
		if space.Status != "open" {
			continue
		}
		if _, follows := followingOccurrenceKeys[space.OccurrenceKey]; follows {
			return true
		}
		if _, stillExists := allOccurrenceKeys[space.OccurrenceKey]; !stillExists {
			// A stale open binding cannot be ordered safely relative to the
			// following boundary, so cancellation fails closed.
			return true
		}
	}
	return false
}
