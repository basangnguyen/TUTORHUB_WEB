package classroom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var (
	ErrEnrollmentNotFound         = errors.New("class enrollment not found")
	ErrEnrollmentAccessDenied     = errors.New("class enrollment access denied")
	ErrInvalidEnrollmentInput     = errors.New("invalid class enrollment input")
	ErrEnrollmentConflict         = errors.New("class enrollment state conflict")
	ErrClassInviteCodeUnavailable = errors.New("class invite code unavailable")
	ErrClassInviteCodeConflict    = errors.New("class invite code conflict")
)

type EnrollmentStatus string

const (
	EnrollmentStatusInvited   EnrollmentStatus = "invited"
	EnrollmentStatusActive    EnrollmentStatus = "active"
	EnrollmentStatusSuspended EnrollmentStatus = "suspended"
	EnrollmentStatusLeft      EnrollmentStatus = "left"
	EnrollmentStatusRemoved   EnrollmentStatus = "removed"
)

type ClassInviteCodeStatus string

const (
	ClassInviteCodeStatusActive    ClassInviteCodeStatus = "active"
	ClassInviteCodeStatusExhausted ClassInviteCodeStatus = "exhausted"
	ClassInviteCodeStatusExpired   ClassInviteCodeStatus = "expired"
	ClassInviteCodeStatusRevoked   ClassInviteCodeStatus = "revoked"
)

type Enrollment struct {
	ID          uuid.UUID        `json:"id"`
	TenantID    uuid.UUID        `json:"tenant_id"`
	ClassID     uuid.UUID        `json:"class_id"`
	UserID      uuid.UUID        `json:"user_id"`
	ClassRole   policy.ClassRole `json:"class_role"`
	Status      EnrollmentStatus `json:"status"`
	EnrolledBy  uuid.UUID        `json:"enrolled_by"`
	JoinedAt    *time.Time       `json:"joined_at"`
	SuspendedAt *time.Time       `json:"suspended_at"`
	LeftAt      *time.Time       `json:"left_at"`
	RemovedAt   *time.Time       `json:"removed_at"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// ClassEnrollment keeps the public contract descriptive while Enrollment remains
// concise inside the classroom module.
type ClassEnrollment = Enrollment

type ClassInviteCode struct {
	ID         uuid.UUID             `json:"id"`
	TenantID   uuid.UUID             `json:"tenant_id"`
	ClassID    uuid.UUID             `json:"class_id"`
	Status     ClassInviteCodeStatus `json:"status"`
	ExpiresAt  time.Time             `json:"expires_at"`
	UsageLimit int                   `json:"usage_limit"`
	UsageCount int                   `json:"usage_count"`
	CreatedBy  uuid.UUID             `json:"created_by"`
	RevokedAt  *time.Time            `json:"revoked_at"`
	RevokedBy  *uuid.UUID            `json:"revoked_by"`
	CreatedAt  time.Time             `json:"created_at"`
	UpdatedAt  time.Time             `json:"updated_at"`
}

type DirectEnrollmentParams struct {
	MemberEmail string
	ChangedAt   time.Time
}

type CreateInviteCodeParams struct {
	CodeHash   []byte
	ExpiresAt  time.Time
	UsageLimit int
	CreatedAt  time.Time
}

type EnrollmentMutationResult struct {
	Enrollment Enrollment
	Changed    bool
}

type JoinClassInvitationResult struct {
	Class      Class
	Enrollment *Enrollment
	Joined     bool
}

// EnrollmentLookup is the authoritative class-role read boundary. The list
// method returns active enrollments only; callers must not restore terminal roles
// from a session snapshot.
type EnrollmentLookup interface {
	FindActorEnrollment(
		context.Context,
		tenancy.Context,
		uuid.UUID,
	) (*Enrollment, error)
	ListActorEnrollments(
		context.Context,
		tenancy.Context,
		[]uuid.UUID,
	) ([]Enrollment, error)
}

type EnrollmentRepository interface {
	EnrollmentLookup
	DirectEnroll(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		DirectEnrollmentParams,
	) (EnrollmentMutationResult, error)
	SuspendEnrollment(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) (EnrollmentMutationResult, error)
	RemoveEnrollment(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) (EnrollmentMutationResult, error)
	CreateInviteCode(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		CreateInviteCodeParams,
	) (ClassInviteCode, error)
	ListInviteCodes(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		time.Time,
	) ([]ClassInviteCode, error)
	RevokeInviteCode(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) (ClassInviteCode, error)
	JoinByInviteCode(
		context.Context,
		tenancy.Context,
		[]byte,
		time.Time,
	) (JoinClassInvitationResult, error)
	LeaveClass(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		time.Time,
	) (EnrollmentMutationResult, error)
}
