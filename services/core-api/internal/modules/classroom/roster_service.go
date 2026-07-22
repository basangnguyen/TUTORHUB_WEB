package classroom

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
	"golang.org/x/text/unicode/norm"
)

const (
	defaultRosterLimit          = 25
	maximumRosterLimit          = 100
	maximumRosterSearchLength   = 200
	maximumRosterCursorLength   = 512
	rosterCursorPrefix          = "thro1_"
	maximumRosterBulkOperations = 50
)

type rosterCursorPayload struct {
	UserID     string `json:"user_id"`
	FilterHash string `json:"filter_hash"`
}

func (service *EnrollmentService) ListRoster(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input ListRosterInput,
) (RosterPage, error) {
	if _, err := service.authorizeManagedClass(ctx, access, classID); err != nil {
		return RosterPage{}, err
	}
	params, err := normalizeListRosterInput(input, access.TenantID, classID)
	if err != nil {
		return RosterPage{}, err
	}
	result, err := service.repository.ListRoster(
		ctx,
		tenantContextFromAccess(access),
		classID,
		params,
	)
	if err != nil {
		return RosterPage{}, err
	}

	page := RosterPage{Owner: result.Owner, Items: result.Items}
	if result.HasMore && len(result.Items) > 0 {
		last := result.Items[len(result.Items)-1]
		page.NextCursor, err = encodeRosterCursor(
			RosterCursor{UserID: last.User.ID},
			access.TenantID,
			classID,
			params.Search,
			params.Status,
		)
		if err != nil {
			return RosterPage{}, fmt.Errorf("encode next roster cursor: %w", err)
		}
	}
	return page, nil
}

func (service *EnrollmentService) UpdateRosterRole(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	userID uuid.UUID,
	input UpdateRosterRoleInput,
) (EnrollmentMutationResult, error) {
	if userID == uuid.Nil {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	role, err := normalizePersistedClassRole(input.ClassRole)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	class, err := service.authorizeManagedClass(ctx, access, classID)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	if class.Status != ClassStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	return service.repository.UpdateRosterRole(
		ctx,
		tenantContextFromAccess(access),
		classID,
		userID,
		UpdateRosterRoleParams{
			ClassRole: role,
			ChangedAt: service.clock().UTC(),
			Source:    "roster_single",
		},
	)
}

func (service *EnrollmentService) BulkMutateRoster(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input BulkRosterInput,
) (BulkRosterResult, error) {
	input, err := normalizeBulkRosterInput(input)
	if err != nil {
		return BulkRosterResult{}, err
	}
	class, err := service.authorizeManagedClass(ctx, access, classID)
	if err != nil {
		return BulkRosterResult{}, err
	}
	if class.Status != ClassStatusActive {
		return BulkRosterResult{}, ErrEnrollmentConflict
	}

	tenantContext := tenantContextFromAccess(access)
	changedAt := service.clock().UTC()
	result := BulkRosterResult{
		Action: input.Action,
		Items:  make([]RosterBulkItemResult, 0, len(input.UserIDs)),
	}
	for index, userID := range input.UserIDs {
		var mutation EnrollmentMutationResult
		switch input.Action {
		case RosterBulkActionUpdateRole:
			mutation, err = service.repository.UpdateRosterRole(
				ctx,
				tenantContext,
				classID,
				userID,
				UpdateRosterRoleParams{
					ClassRole: *input.ClassRole,
					ChangedAt: changedAt,
					Source:    "roster_bulk",
				},
			)
		case RosterBulkActionSuspend:
			mutation, err = service.repository.SuspendEnrollment(
				ctx, tenantContext, classID, userID, changedAt,
			)
		case RosterBulkActionRemove:
			mutation, err = service.repository.RemoveEnrollment(
				ctx, tenantContext, classID, userID, changedAt,
			)
		}

		item := RosterBulkItemResult{
			UserID: userID,
		}
		if err != nil {
			item.Failure = rosterBulkFailure(err)
			if item.Failure == nil {
				item.Failure = &RosterBulkFailure{
					Code:  RosterBulkFailureInternal,
					Cause: err,
				}
				result.Items = append(result.Items, item)
				for _, pendingUserID := range input.UserIDs[index+1:] {
					result.Items = append(result.Items, RosterBulkItemResult{
						UserID: pendingUserID,
						Failure: &RosterBulkFailure{
							Code: RosterBulkFailureNotAttempted,
						},
					})
				}
				result.FailedCount += len(input.UserIDs) - index
				return result, fmt.Errorf("apply class roster bulk mutation: %w", err)
			}
			result.FailedCount++
		} else {
			enrollment := mutation.Enrollment
			item.Enrollment = &enrollment
			item.Changed = mutation.Changed
			result.SucceededCount++
			if !mutation.Changed {
				result.UnchangedCount++
			}
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func normalizeListRosterInput(
	input ListRosterInput,
	tenantID uuid.UUID,
	classID uuid.UUID,
) (ListRosterParams, error) {
	search := strings.ToLower(norm.NFC.String(strings.Join(strings.Fields(input.Search), " ")))
	if len([]rune(search)) > maximumRosterSearchLength {
		return ListRosterParams{}, ErrInvalidEnrollmentInput
	}
	if input.Limit == 0 {
		input.Limit = defaultRosterLimit
	}
	if input.Limit < 1 || input.Limit > maximumRosterLimit {
		return ListRosterParams{}, ErrInvalidEnrollmentInput
	}

	var status *EnrollmentStatus
	if input.Status != nil {
		value := EnrollmentStatus(strings.ToLower(strings.TrimSpace(string(*input.Status))))
		if !validEnrollmentStatus(value) {
			return ListRosterParams{}, ErrInvalidEnrollmentInput
		}
		status = &value
	}
	after, err := decodeRosterCursor(
		strings.TrimSpace(input.Cursor), tenantID, classID, search, status,
	)
	if err != nil {
		return ListRosterParams{}, err
	}
	return ListRosterParams{
		Search: search,
		Status: status,
		Limit:  input.Limit,
		After:  after,
	}, nil
}

func encodeRosterCursor(
	cursor RosterCursor,
	tenantID uuid.UUID,
	classID uuid.UUID,
	search string,
	status *EnrollmentStatus,
) (string, error) {
	if !validRosterCursor(cursor) {
		return "", ErrInvalidRosterCursor
	}
	contents, err := json.Marshal(rosterCursorPayload{
		UserID:     cursor.UserID.String(),
		FilterHash: rosterFilterHash(tenantID, classID, search, status),
	})
	if err != nil {
		return "", err
	}
	return rosterCursorPrefix + base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeRosterCursor(
	value string,
	tenantID uuid.UUID,
	classID uuid.UUID,
	search string,
	status *EnrollmentStatus,
) (*RosterCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > maximumRosterCursorLength || !strings.HasPrefix(value, rosterCursorPrefix) {
		return nil, ErrInvalidRosterCursor
	}
	contents, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, rosterCursorPrefix))
	if err != nil {
		return nil, ErrInvalidRosterCursor
	}
	var payload rosterCursorPayload
	if err := decodeStrictCursorJSON(contents, &payload); err != nil ||
		payload.FilterHash != rosterFilterHash(tenantID, classID, search, status) {
		return nil, ErrInvalidRosterCursor
	}
	userID, err := uuid.Parse(payload.UserID)
	if err != nil {
		return nil, ErrInvalidRosterCursor
	}
	cursor := RosterCursor{UserID: userID}
	if !validRosterCursor(cursor) {
		return nil, ErrInvalidRosterCursor
	}
	return &cursor, nil
}

func rosterFilterHash(
	tenantID uuid.UUID,
	classID uuid.UUID,
	search string,
	status *EnrollmentStatus,
) string {
	statusValue := ""
	if status != nil {
		statusValue = string(*status)
	}
	digest := sha256.Sum256([]byte(
		tenantID.String() + "\x00" + classID.String() + "\x00" + search + "\x00" + statusValue,
	))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func validRosterCursor(cursor RosterCursor) bool {
	return cursor.UserID != uuid.Nil
}

func normalizeBulkRosterInput(input BulkRosterInput) (BulkRosterInput, error) {
	if len(input.UserIDs) < 1 || len(input.UserIDs) > maximumRosterBulkOperations {
		return BulkRosterInput{}, ErrInvalidEnrollmentInput
	}
	userIDs := make(map[uuid.UUID]struct{}, len(input.UserIDs))
	for _, userID := range input.UserIDs {
		if userID == uuid.Nil {
			return BulkRosterInput{}, ErrInvalidEnrollmentInput
		}
		if _, exists := userIDs[userID]; exists {
			return BulkRosterInput{}, ErrInvalidEnrollmentInput
		}
		userIDs[userID] = struct{}{}
	}

	switch input.Action {
	case RosterBulkActionUpdateRole:
		if input.ClassRole == nil {
			return BulkRosterInput{}, ErrInvalidEnrollmentInput
		}
		role, err := normalizePersistedClassRole(*input.ClassRole)
		if err != nil {
			return BulkRosterInput{}, err
		}
		input.ClassRole = &role
	case RosterBulkActionSuspend, RosterBulkActionRemove:
		if input.ClassRole != nil {
			return BulkRosterInput{}, ErrInvalidEnrollmentInput
		}
	default:
		return BulkRosterInput{}, ErrInvalidEnrollmentInput
	}
	input.UserIDs = append([]uuid.UUID(nil), input.UserIDs...)
	return input, nil
}

func normalizePersistedClassRole(value policy.ClassRole) (policy.ClassRole, error) {
	role := policy.ClassRole(strings.ToLower(strings.TrimSpace(string(value))))
	if !validPersistedClassRole(role) {
		return "", ErrInvalidEnrollmentInput
	}
	return role, nil
}

func validPersistedClassRole(role policy.ClassRole) bool {
	switch role {
	case policy.ClassRoleCoTeacher,
		policy.ClassRoleTeachingAssistant,
		policy.ClassRoleStudent:
		return true
	default:
		return false
	}
}

func validEnrollmentStatus(status EnrollmentStatus) bool {
	switch status {
	case EnrollmentStatusInvited,
		EnrollmentStatusActive,
		EnrollmentStatusSuspended,
		EnrollmentStatusLeft,
		EnrollmentStatusRemoved:
		return true
	default:
		return false
	}
}

func rosterBulkFailure(err error) *RosterBulkFailure {
	failure := &RosterBulkFailure{Cause: err}
	switch {
	case errors.Is(err, ErrInvalidEnrollmentInput), errors.Is(err, ErrInvalidRosterCursor):
		failure.Code = RosterBulkFailureInvalid
	case errors.Is(err, ErrEnrollmentAccessDenied), errors.Is(err, ErrClassAccessDenied):
		failure.Code = RosterBulkFailureAccessDenied
	case errors.Is(err, ErrEnrollmentNotFound), errors.Is(err, ErrClassNotFound):
		failure.Code = RosterBulkFailureNotFound
	case errors.Is(err, ErrEnrollmentConflict), errors.Is(err, ErrInvalidClassTransition):
		failure.Code = RosterBulkFailureConflict
	default:
		return nil
	}
	return failure
}
