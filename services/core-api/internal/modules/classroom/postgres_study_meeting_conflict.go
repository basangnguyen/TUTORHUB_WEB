package classroom

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/ownertime"
)

type studyMeetingBusyWindow struct {
	UserID   uuid.UUID
	StartsAt time.Time
	EndsAt   time.Time
}

type studyMeetingBusyWindowKey struct {
	UserID   uuid.UUID
	StartsAt time.Time
	EndsAt   time.Time
}

func requireNoStudyMeetingConflicts(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	windows []studyMeetingBusyWindow,
) error {
	if transaction == nil || tenantID == uuid.Nil {
		return ErrInvalidSessionInput
	}
	normalized, userIDs, err := normalizeStudyMeetingBusyWindows(windows)
	if err != nil {
		return err
	}
	if err := ownertime.AcquireLocks(ctx, transaction, tenantID, userIDs); err != nil {
		return fmt.Errorf("lock class schedule busy users: %w", err)
	}

	windowUserIDs := make([]uuid.UUID, 0, len(normalized))
	startsAt := make([]time.Time, 0, len(normalized))
	endsAt := make([]time.Time, 0, len(normalized))
	for _, window := range normalized {
		windowUserIDs = append(windowUserIDs, window.UserID)
		startsAt = append(startsAt, window.StartsAt)
		endsAt = append(endsAt, window.EndsAt)
	}

	var conflict bool
	if err := transaction.QueryRow(
		ctx,
		`WITH proposed_busy_windows AS (
    SELECT proposed.user_id, proposed.starts_at, proposed.ends_at
    FROM unnest($2::uuid[], $3::timestamptz[], $4::timestamptz[])
        AS proposed(user_id, starts_at, ends_at)
)
SELECT EXISTS (
    SELECT 1
    FROM proposed_busy_windows AS proposed
    JOIN tutorhub.study_meetings AS meeting
      ON meeting.tenant_id = $1
     AND meeting.owner_user_id = proposed.user_id
     AND meeting.status = 'scheduled'
     AND meeting.starts_at < proposed.ends_at
     AND meeting.ends_at > proposed.starts_at
)`,
		tenantID,
		windowUserIDs,
		startsAt,
		endsAt,
	).Scan(&conflict); err != nil {
		return fmt.Errorf("check class schedule study meeting conflicts: %w", err)
	}
	if conflict {
		return ErrSessionScheduleConflict
	}
	return nil
}

func normalizeStudyMeetingBusyWindows(
	windows []studyMeetingBusyWindow,
) ([]studyMeetingBusyWindow, []uuid.UUID, error) {
	if len(windows) == 0 {
		return nil, nil, ErrInvalidSessionInput
	}
	deduplicated := make(map[studyMeetingBusyWindowKey]studyMeetingBusyWindow, len(windows))
	for _, window := range windows {
		window.StartsAt = window.StartsAt.Round(0).UTC()
		window.EndsAt = window.EndsAt.Round(0).UTC()
		if window.UserID == uuid.Nil || window.StartsAt.IsZero() ||
			window.EndsAt.IsZero() || !window.EndsAt.After(window.StartsAt) {
			return nil, nil, ErrInvalidSessionInput
		}
		key := studyMeetingBusyWindowKey(window)
		deduplicated[key] = window
	}
	normalized := make([]studyMeetingBusyWindow, 0, len(deduplicated))
	for _, window := range deduplicated {
		normalized = append(normalized, window)
	}
	sort.Slice(normalized, func(left, right int) bool {
		if normalized[left].UserID != normalized[right].UserID {
			return normalized[left].UserID.String() < normalized[right].UserID.String()
		}
		if !normalized[left].StartsAt.Equal(normalized[right].StartsAt) {
			return normalized[left].StartsAt.Before(normalized[right].StartsAt)
		}
		return normalized[left].EndsAt.Before(normalized[right].EndsAt)
	})
	userIDs := make([]uuid.UUID, 0, len(normalized))
	for _, window := range normalized {
		if len(userIDs) == 0 || userIDs[len(userIDs)-1] != window.UserID {
			userIDs = append(userIDs, window.UserID)
		}
	}
	return normalized, userIDs, nil
}

func loadOneTimeSessionBusyUserIDs(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
) ([]uuid.UUID, error) {
	if transaction == nil || tenantID == uuid.Nil || classID == uuid.Nil ||
		sessionID == uuid.Nil {
		return nil, ErrInvalidSessionInput
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT busy_user.user_id
FROM (
    SELECT session.organizer_user_id AS user_id
    FROM tutorhub.class_sessions AS session
    WHERE session.tenant_id = $1
      AND session.class_id = $2
      AND session.id = $3
    UNION
    SELECT attendee.internal_user_id AS user_id
    FROM tutorhub.class_session_attendees AS attendee
    WHERE attendee.tenant_id = $1
      AND attendee.class_id = $2
      AND attendee.session_id = $3
      AND attendee.internal_user_id IS NOT NULL
      AND attendee.status = 'active'
) AS busy_user
ORDER BY busy_user.user_id`,
		tenantID,
		classID,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("load one-time class session busy users: %w", err)
	}
	defer rows.Close()
	userIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var userID uuid.UUID
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("scan one-time class session busy user: %w", err)
		}
		if userID == uuid.Nil {
			return nil, ErrInvalidSessionInput
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate one-time class session busy users: %w", err)
	}
	if len(userIDs) == 0 {
		return nil, ErrSessionNotFound
	}
	return userIDs, nil
}

func oneTimeBusyWindows(
	userIDs []uuid.UUID,
	startsAt time.Time,
	endsAt time.Time,
) []studyMeetingBusyWindow {
	windows := make([]studyMeetingBusyWindow, 0, len(userIDs))
	for _, userID := range userIDs {
		windows = append(windows, studyMeetingBusyWindow{
			UserID: userID, StartsAt: startsAt, EndsAt: endsAt,
		})
	}
	return windows
}

func newlyActiveOneTimeAudienceBusyWindows(
	organizerID uuid.UUID,
	current []persistedSessionAttendee,
	desired []resolvedAudienceMember,
	startsAt time.Time,
	endsAt time.Time,
) []studyMeetingBusyWindow {
	currentUserIDs := make(map[uuid.UUID]struct{}, len(current)+1)
	currentUserIDs[organizerID] = struct{}{}
	for _, attendee := range current {
		currentUserIDs[attendee.UserID] = struct{}{}
	}
	windows := make([]studyMeetingBusyWindow, 0, len(desired))
	for _, attendee := range desired {
		if _, exists := currentUserIDs[attendee.UserID]; exists {
			continue
		}
		currentUserIDs[attendee.UserID] = struct{}{}
		windows = append(windows, studyMeetingBusyWindow{
			UserID: attendee.UserID, StartsAt: startsAt, EndsAt: endsAt,
		})
	}
	return windows
}
