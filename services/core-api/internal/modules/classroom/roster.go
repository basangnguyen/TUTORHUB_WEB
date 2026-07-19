package classroom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var ErrInvalidRosterCursor = errors.New("invalid class roster cursor")

type RosterMemberActions struct {
	AssignableRoles []policy.ClassRole
	CanSuspend      bool
	CanRemove       bool
}

type RosterUser struct {
	ID          uuid.UUID
	DisplayName string
	Email       string
}

// RosterOwner is pinned outside enrollment pagination because class ownership
// is sourced exclusively from classes.owner_user_id.
type RosterOwner struct {
	User RosterUser
}

type RosterMember struct {
	User       RosterUser
	Enrollment Enrollment
	Actions    RosterMemberActions
}

type RosterCursor struct {
	UserID uuid.UUID
}

type ListRosterParams struct {
	Search string
	Status *EnrollmentStatus
	Limit  int
	After  *RosterCursor
}

type ListRosterResult struct {
	Owner   RosterOwner
	Items   []RosterMember
	HasMore bool
}

type UpdateRosterRoleParams struct {
	ClassRole policy.ClassRole
	ChangedAt time.Time
	Source    string
}

type RosterRepository interface {
	ListRoster(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		ListRosterParams,
	) (ListRosterResult, error)
	UpdateRosterRole(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		UpdateRosterRoleParams,
	) (EnrollmentMutationResult, error)
}

type ListRosterInput struct {
	Search string
	Status *EnrollmentStatus
	Limit  int
	Cursor string
}

type RosterPage struct {
	Owner      RosterOwner
	Items      []RosterMember
	NextCursor string
}

type UpdateRosterRoleInput struct {
	ClassRole policy.ClassRole
}

type RosterBulkAction string

const (
	RosterBulkActionUpdateRole RosterBulkAction = "update_role"
	RosterBulkActionSuspend    RosterBulkAction = "suspend"
	RosterBulkActionRemove     RosterBulkAction = "remove"
)

type BulkRosterInput struct {
	Action    RosterBulkAction
	ClassRole *policy.ClassRole
	UserIDs   []uuid.UUID
}

type RosterBulkFailureCode string

const (
	RosterBulkFailureInvalid      RosterBulkFailureCode = "invalid"
	RosterBulkFailureAccessDenied RosterBulkFailureCode = "access_denied"
	RosterBulkFailureNotFound     RosterBulkFailureCode = "not_found"
	RosterBulkFailureConflict     RosterBulkFailureCode = "conflict"
)

type RosterBulkFailure struct {
	Code  RosterBulkFailureCode
	Cause error
}

type RosterBulkItemResult struct {
	UserID     uuid.UUID
	Enrollment *Enrollment
	Failure    *RosterBulkFailure
	Changed    bool
}

type BulkRosterResult struct {
	Action         RosterBulkAction
	Items          []RosterBulkItemResult
	SucceededCount int
	UnchangedCount int
	FailedCount    int
}
