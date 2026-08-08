package calendar

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type lockedStudyMeetingMediaSpace struct {
	ID     uuid.UUID
	Status string
}

func lockStudyMeetingMediaSpaces(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	meetingID uuid.UUID,
) ([]lockedStudyMeetingMediaSpace, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT id, status
FROM tutorhub.media_spaces
WHERE tenant_id = $1
  AND source_kind = 'study_meeting'
  AND source_study_meeting_id = $2
ORDER BY id
FOR UPDATE`,
		tenantID,
		meetingID,
	)
	if err != nil {
		return nil, fmt.Errorf("lock study meeting media spaces: %w", err)
	}
	defer rows.Close()

	spaces := make([]lockedStudyMeetingMediaSpace, 0)
	for rows.Next() {
		var space lockedStudyMeetingMediaSpace
		if err := rows.Scan(&space.ID, &space.Status); err != nil {
			return nil, fmt.Errorf("scan locked study meeting media space: %w", err)
		}
		spaces = append(spaces, space)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked study meeting media spaces: %w", err)
	}
	return spaces, nil
}

func hasOpenStudyMeetingMediaSpace(spaces []lockedStudyMeetingMediaSpace) bool {
	for _, space := range spaces {
		if space.Status == "open" {
			return true
		}
	}
	return false
}
