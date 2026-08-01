package classroom

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
)

type recurringParticipationConflictSnapshot struct {
	OrganizerUserID       uuid.UUID
	BaseAssignments       map[uuid.UUID]bool
	OccurrenceAssignments map[string]map[uuid.UUID]bool
}

func (repository *PostgresRepository) lockRecurringParticipationConflictSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
) (recurringParticipationConflictSnapshot, error) {
	if transaction == nil || tenantID == uuid.Nil || classID == uuid.Nil ||
		seriesID == uuid.Nil {
		return recurringParticipationConflictSnapshot{}, ErrInvalidSessionInput
	}
	snapshot := recurringParticipationConflictSnapshot{
		BaseAssignments:       make(map[uuid.UUID]bool),
		OccurrenceAssignments: make(map[string]map[uuid.UUID]bool),
	}
	organizerUserID, err := loadRecurringSeriesOrganizerUserID(
		ctx, transaction, tenantID, classID, seriesID,
	)
	if err != nil {
		return recurringParticipationConflictSnapshot{}, err
	}
	snapshot.OrganizerUserID = organizerUserID

	// The authoritative series/source row is already locked by every caller, so
	// the plain latest-assignment read is stable and avoids locking history rows.
	assignmentRows, err := transaction.Query(
		ctx,
		`SELECT DISTINCT ON (
    attendee.occurrence_key, attendee.internal_user_id
)
    attendee.occurrence_key, attendee.internal_user_id, attendee.status
FROM tutorhub.class_session_attendees AS attendee
WHERE attendee.tenant_id = $1
  AND attendee.class_id = $2
  AND attendee.session_id IS NULL
  AND attendee.series_id = $3
  AND attendee.internal_user_id IS NOT NULL
ORDER BY attendee.occurrence_key NULLS FIRST, attendee.internal_user_id,
         attendee.updated_at DESC, attendee.version DESC, attendee.id DESC`,
		tenantID,
		classID,
		seriesID,
	)
	if err != nil {
		return recurringParticipationConflictSnapshot{}, fmt.Errorf(
			"load latest recurring participation assignments for study meeting conflict: %w",
			err,
		)
	}
	for assignmentRows.Next() {
		var occurrenceKey *string
		var userID uuid.UUID
		var status string
		if err := assignmentRows.Scan(&occurrenceKey, &userID, &status); err != nil {
			assignmentRows.Close()
			return recurringParticipationConflictSnapshot{}, fmt.Errorf(
				"scan latest recurring participation assignment for study meeting conflict: %w",
				err,
			)
		}
		if userID == uuid.Nil || (status != "active" && status != "removed") {
			assignmentRows.Close()
			return recurringParticipationConflictSnapshot{}, ErrInvalidSessionInput
		}
		active := status == "active"
		if occurrenceKey == nil {
			snapshot.BaseAssignments[userID] = active
			continue
		}
		assignments := snapshot.OccurrenceAssignments[*occurrenceKey]
		if assignments == nil {
			assignments = make(map[uuid.UUID]bool)
			snapshot.OccurrenceAssignments[*occurrenceKey] = assignments
		}
		assignments[userID] = active
	}
	assignmentRows.Close()
	if err := assignmentRows.Err(); err != nil {
		return recurringParticipationConflictSnapshot{}, fmt.Errorf(
			"iterate latest recurring participation assignments for study meeting conflict: %w",
			err,
		)
	}
	return snapshot, nil
}

func loadRecurringSeriesOrganizerUserID(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
) (uuid.UUID, error) {
	if transaction == nil || tenantID == uuid.Nil || classID == uuid.Nil ||
		seriesID == uuid.Nil {
		return uuid.Nil, ErrInvalidSessionInput
	}
	var organizerUserID uuid.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT organizer_user_id
FROM tutorhub.class_session_series
WHERE tenant_id = $1 AND class_id = $2 AND id = $3`,
		tenantID,
		classID,
		seriesID,
	).Scan(&organizerUserID); err != nil {
		return uuid.Nil, fmt.Errorf(
			"load recurring session organizer for study meeting conflict: %w",
			err,
		)
	}
	if organizerUserID == uuid.Nil {
		return uuid.Nil, ErrInvalidSessionInput
	}
	return organizerUserID, nil
}

func (repository *PostgresRepository) projectPersistedSeriesOccurrences(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
) ([]recurrence.Occurrence, error) {
	series, err := lockClassSessionSeries(ctx, transaction, tenantID, classID, seriesID)
	if err != nil {
		return nil, err
	}
	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(series))
	if err != nil {
		return nil, err
	}
	exceptions, err := listSeriesExceptions(ctx, transaction, tenantID, classID, seriesID)
	if err != nil {
		return nil, err
	}
	return applyPersistedExceptions(ctx, series, occurrences, exceptions)
}

func (repository *PostgresRepository) requireNoExistingSeriesStudyMeetingConflicts(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
	occurrences []recurrence.Occurrence,
) error {
	snapshot, err := repository.lockRecurringParticipationConflictSnapshot(
		ctx,
		transaction,
		tenantID,
		classID,
		seriesID,
	)
	if err != nil {
		return err
	}
	windows := recurringEffectiveAudienceBusyWindows(snapshot, occurrences)
	if len(windows) == 0 {
		return nil
	}
	return requireNoStudyMeetingConflicts(
		ctx,
		transaction,
		tenantID,
		windows,
	)
}

func recurringEffectiveAudienceBusyWindows(
	snapshot recurringParticipationConflictSnapshot,
	occurrences []recurrence.Occurrence,
) []studyMeetingBusyWindow {
	windows := make([]studyMeetingBusyWindow, 0, len(occurrences))
	for _, occurrence := range occurrences {
		users := make(map[uuid.UUID]struct{}, len(snapshot.BaseAssignments)+1)
		users[snapshot.OrganizerUserID] = struct{}{}
		for userID, active := range snapshot.BaseAssignments {
			if active || userID == snapshot.OrganizerUserID {
				users[userID] = struct{}{}
			}
		}
		for userID, active := range snapshot.OccurrenceAssignments[occurrence.Key] {
			if active || userID == snapshot.OrganizerUserID {
				users[userID] = struct{}{}
			} else {
				delete(users, userID)
			}
		}
		userIDs := make([]uuid.UUID, 0, len(users))
		for userID := range users {
			userIDs = append(userIDs, userID)
		}
		sort.Slice(userIDs, func(left, right int) bool {
			return userIDs[left].String() < userIDs[right].String()
		})
		for _, userID := range userIDs {
			windows = append(windows, studyMeetingBusyWindow{
				UserID:   userID,
				StartsAt: occurrence.StartsAt,
				EndsAt:   occurrence.EndsAt,
			})
		}
	}
	return windows
}

func recurringOrganizerBusyWindows(
	organizerID uuid.UUID,
	occurrences []recurrence.Occurrence,
) []studyMeetingBusyWindow {
	windows := make([]studyMeetingBusyWindow, 0, len(occurrences))
	for _, occurrence := range occurrences {
		windows = append(windows, studyMeetingBusyWindow{
			UserID:   organizerID,
			StartsAt: occurrence.StartsAt,
			EndsAt:   occurrence.EndsAt,
		})
	}
	return windows
}

func newlyActiveRecurringAudienceUserIDs(
	organizerID uuid.UUID,
	current []persistedSessionAttendee,
	desired []resolvedAudienceMember,
) []uuid.UUID {
	currentUserIDs := make(map[uuid.UUID]struct{}, len(current)+1)
	currentUserIDs[organizerID] = struct{}{}
	for _, attendee := range current {
		currentUserIDs[attendee.UserID] = struct{}{}
	}
	newUserIDs := make([]uuid.UUID, 0, len(desired))
	for _, attendee := range desired {
		if _, exists := currentUserIDs[attendee.UserID]; exists {
			continue
		}
		currentUserIDs[attendee.UserID] = struct{}{}
		newUserIDs = append(newUserIDs, attendee.UserID)
	}
	sort.Slice(newUserIDs, func(left, right int) bool {
		return newUserIDs[left].String() < newUserIDs[right].String()
	})
	return newUserIDs
}

func newlyActiveRecurringSeriesAudienceBusyWindows(
	userIDs []uuid.UUID,
	occurrences []recurrence.Occurrence,
	baseAssignments map[uuid.UUID]bool,
	occurrenceAssignments map[string]map[uuid.UUID]bool,
) []studyMeetingBusyWindow {
	windows := make([]studyMeetingBusyWindow, 0, len(userIDs)*len(occurrences))
	for _, occurrence := range occurrences {
		for _, userID := range userIDs {
			if _, exists := occurrenceAssignments[occurrence.Key][userID]; exists {
				// An occurrence-specific active edge already existed; a removed
				// edge remains removed and overrides the new base assignment.
				continue
			}
			if baseAssignments[userID] {
				// The latest base assignment was already active, so this is not
				// a newly-created busy edge for this user.
				continue
			}
			windows = append(windows, studyMeetingBusyWindow{
				UserID:   userID,
				StartsAt: occurrence.StartsAt,
				EndsAt:   occurrence.EndsAt,
			})
		}
	}
	return windows
}

func (repository *PostgresRepository) requireNoNewTypedAudienceStudyMeetingConflicts(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	locked lockedTypedParticipationSource,
	effectiveCurrent []persistedSessionAttendee,
	desired []resolvedAudienceMember,
) error {
	newUserIDs := newlyActiveRecurringAudienceUserIDs(
		locked.OrganizerUserID,
		effectiveCurrent,
		desired,
	)
	if len(newUserIDs) == 0 {
		return nil
	}
	if locked.Ref.Kind == ParticipationSourceOccurrence {
		windows := make([]studyMeetingBusyWindow, 0, len(newUserIDs))
		for _, userID := range newUserIDs {
			windows = append(windows, studyMeetingBusyWindow{
				UserID: userID, StartsAt: locked.StartsAt, EndsAt: locked.EndsAt,
			})
		}
		return requireNoStudyMeetingConflicts(ctx, transaction, tenantID, windows)
	}
	occurrences, err := repository.projectPersistedSeriesOccurrences(
		ctx,
		transaction,
		tenantID,
		classID,
		locked.Ref.SeriesID,
	)
	if err != nil {
		return err
	}
	snapshot, err := repository.lockRecurringParticipationConflictSnapshot(
		ctx,
		transaction,
		tenantID,
		classID,
		locked.Ref.SeriesID,
	)
	if err != nil {
		return err
	}
	windows := newlyActiveRecurringSeriesAudienceBusyWindows(
		newUserIDs,
		occurrences,
		snapshot.BaseAssignments,
		snapshot.OccurrenceAssignments,
	)
	if len(windows) == 0 {
		return nil
	}
	return requireNoStudyMeetingConflicts(ctx, transaction, tenantID, windows)
}

func (repository *PostgresRepository) requireNoOrganizerTransferStudyMeetingConflicts(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	locked lockedTypedParticipationSource,
	current []persistedSessionAttendee,
	newOrganizerID uuid.UUID,
) error {
	if locked.Ref.Kind == ParticipationSourceSession {
		for _, attendee := range current {
			if attendee.UserID == newOrganizerID {
				return nil
			}
		}
		return requireNoStudyMeetingConflicts(
			ctx,
			transaction,
			tenantID,
			[]studyMeetingBusyWindow{{
				UserID: newOrganizerID, StartsAt: locked.StartsAt, EndsAt: locked.EndsAt,
			}},
		)
	}
	occurrences, err := repository.projectPersistedSeriesOccurrences(
		ctx,
		transaction,
		tenantID,
		classID,
		locked.Ref.SeriesID,
	)
	if err != nil {
		return err
	}
	snapshot, err := repository.lockRecurringParticipationConflictSnapshot(
		ctx,
		transaction,
		tenantID,
		classID,
		locked.Ref.SeriesID,
	)
	if err != nil {
		return err
	}
	windows := make([]studyMeetingBusyWindow, 0, len(occurrences))
	for _, occurrence := range occurrences {
		if recurringParticipantPresent(snapshot, newOrganizerID, occurrence.Key) {
			continue
		}
		windows = append(windows, studyMeetingBusyWindow{
			UserID:   newOrganizerID,
			StartsAt: occurrence.StartsAt,
			EndsAt:   occurrence.EndsAt,
		})
	}
	if len(windows) == 0 {
		return nil
	}
	return requireNoStudyMeetingConflicts(ctx, transaction, tenantID, windows)
}

func recurringParticipantPresent(
	snapshot recurringParticipationConflictSnapshot,
	userID uuid.UUID,
	occurrenceKey string,
) bool {
	if userID == snapshot.OrganizerUserID {
		return true
	}
	present := snapshot.BaseAssignments[userID]
	if assignment, exists := snapshot.OccurrenceAssignments[occurrenceKey][userID]; exists {
		present = assignment
	}
	return present
}
