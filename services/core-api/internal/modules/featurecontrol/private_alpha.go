package featurecontrol

import (
	"time"

	"github.com/google/uuid"
)

const (
	PrivateAlphaProgramClassroomWhiteboards = `classroom_whiteboards`
	PrivateAlphaNoticeVersion               = `p5-collab-19-v1`
)

type PrivateAlphaEnrollmentStatus string

const (
	PrivateAlphaEnrollmentActive      PrivateAlphaEnrollmentStatus = `active`
	PrivateAlphaEnrollmentNotEnrolled PrivateAlphaEnrollmentStatus = `not_enrolled`
	PrivateAlphaEnrollmentWithdrawn   PrivateAlphaEnrollmentStatus = `withdrawn`
)

type PrivateAlphaEnrollmentAllowedActions struct {
	ManageEnrollment bool
}

type PrivateAlphaEnrollment struct {
	TenantID       uuid.UUID
	Program        string
	Status         PrivateAlphaEnrollmentStatus
	Revision       int64
	NoticeVersion  string
	AcceptedAt     *time.Time
	WithdrawnAt    *time.Time
	AllowedActions PrivateAlphaEnrollmentAllowedActions
}

type UpdatePrivateAlphaEnrollmentInput struct {
	Enrolled         bool
	ExpectedRevision int64
	NoticeVersion    string
}
