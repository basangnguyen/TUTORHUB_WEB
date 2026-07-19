package classroom

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var classCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{2,31}$`)

type ClassStatus string

const (
	ClassStatusDraft    ClassStatus = "draft"
	ClassStatusActive   ClassStatus = "active"
	ClassStatusArchived ClassStatus = "archived"
)

type Class struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	OwnerUserID  uuid.UUID
	Code         string
	Title        string
	Description  string
	Timezone     string
	Status       ClassStatus
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ArchivedAt   *time.Time
	ViewerAccess ViewerAccess
}

// ViewerAccess is a server-derived projection for the authenticated actor. It
// is never accepted as mutation input and must be rebuilt from authoritative
// membership, ownership, and enrollment state for every request.
type ViewerAccess struct {
	ClassRole            *policy.ClassRole
	EnrollmentStatus     *EnrollmentStatus
	CanManageEnrollments bool
	CanJoinRoom          bool
	CanPublishMedia      bool
	CanLeave             bool
}

type CreateClassParams struct {
	OwnerUserID uuid.UUID
	Code        string
	Title       string
	Description string
	Timezone    *string
}

func (params CreateClassParams) normalized() (CreateClassParams, error) {
	params.Code = normalizeClassCode(params.Code)
	params.Title = strings.TrimSpace(params.Title)
	params.Description = strings.TrimSpace(params.Description)

	if params.OwnerUserID == uuid.Nil {
		return CreateClassParams{}, fmt.Errorf("%w: class owner is required", ErrInvalidClassInput)
	}
	if err := validateClassCode(params.Code); err != nil {
		return CreateClassParams{}, err
	}
	if err := validateClassTitle(params.Title); err != nil {
		return CreateClassParams{}, err
	}
	if err := validateClassDescription(params.Description); err != nil {
		return CreateClassParams{}, err
	}
	if params.Timezone != nil {
		timezone, err := normalizeClassTimezone(*params.Timezone)
		if err != nil {
			return CreateClassParams{}, err
		}
		params.Timezone = &timezone
	}

	return params, nil
}

type UpdateClassParams struct {
	Code            *string
	Title           *string
	Description     *string
	Timezone        *string
	Status          *ClassStatus
	ExpectedVersion int64
}

func (params UpdateClassParams) normalized() (UpdateClassParams, error) {
	if params.ExpectedVersion < 1 ||
		(params.Code == nil &&
			params.Title == nil &&
			params.Description == nil &&
			params.Timezone == nil &&
			params.Status == nil) {
		return UpdateClassParams{}, fmt.Errorf(
			"%w: expected version and at least one mutable field are required",
			ErrInvalidClassInput,
		)
	}
	if params.Code != nil {
		code := normalizeClassCode(*params.Code)
		if err := validateClassCode(code); err != nil {
			return UpdateClassParams{}, err
		}
		params.Code = &code
	}
	if params.Title != nil {
		title := strings.TrimSpace(*params.Title)
		if err := validateClassTitle(title); err != nil {
			return UpdateClassParams{}, err
		}
		params.Title = &title
	}
	if params.Description != nil {
		description := strings.TrimSpace(*params.Description)
		if err := validateClassDescription(description); err != nil {
			return UpdateClassParams{}, err
		}
		params.Description = &description
	}
	if params.Timezone != nil {
		timezone, err := normalizeClassTimezone(*params.Timezone)
		if err != nil {
			return UpdateClassParams{}, err
		}
		params.Timezone = &timezone
	}
	if params.Status != nil {
		status := ClassStatus(strings.ToLower(strings.TrimSpace(string(*params.Status))))
		if status != ClassStatusDraft && status != ClassStatusActive {
			return UpdateClassParams{}, fmt.Errorf(
				"%w: class status must be draft or active",
				ErrInvalidClassInput,
			)
		}
		params.Status = &status
	}

	return params, nil
}

type TransferClassOwnershipParams struct {
	NewOwnerUserID  uuid.UUID
	ExpectedVersion int64
}

func (params TransferClassOwnershipParams) normalized() (TransferClassOwnershipParams, error) {
	if params.ExpectedVersion < 1 {
		return TransferClassOwnershipParams{}, fmt.Errorf(
			"%w: expected version is required",
			ErrInvalidClassInput,
		)
	}
	if params.NewOwnerUserID == uuid.Nil {
		return TransferClassOwnershipParams{}, ErrClassOwnerUnavailable
	}
	return params, nil
}

type ClassCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type ListClassesParams struct {
	Status *ClassStatus
	Limit  int
	After  *ClassCursor
}

type ListClassesResult struct {
	Items   []Class
	HasMore bool
}

func normalizeClassCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func validateClassCode(value string) error {
	if !classCodePattern.MatchString(value) {
		return fmt.Errorf(
			"%w: class code must contain 3 to 32 uppercase letters, numbers, hyphens, or underscores",
			ErrInvalidClassInput,
		)
	}
	return nil
}

func validateClassTitle(value string) error {
	titleLength := utf8.RuneCountInString(value)
	if titleLength < 1 || titleLength > 200 {
		return fmt.Errorf(
			"%w: class title must contain between 1 and 200 characters",
			ErrInvalidClassInput,
		)
	}
	return nil
}

func validateClassDescription(value string) error {
	if utf8.RuneCountInString(value) > 4000 {
		return fmt.Errorf(
			"%w: class description cannot exceed 4000 characters",
			ErrInvalidClassInput,
		)
	}
	return nil
}

func normalizeClassTimezone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 100 || strings.EqualFold(value, "local") {
		return "", fmt.Errorf("%w: invalid class timezone", ErrInvalidClassInput)
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", fmt.Errorf("%w: invalid class timezone", ErrInvalidClassInput)
	}
	return value, nil
}

func validateClassStatus(status ClassStatus) error {
	switch status {
	case ClassStatusDraft, ClassStatusActive, ClassStatusArchived:
		return nil
	default:
		return fmt.Errorf("%w: invalid class status", ErrInvalidClassInput)
	}
}

func validateDirectStatusTransition(current ClassStatus, requested ClassStatus) error {
	if current == ClassStatusArchived {
		return ErrInvalidClassTransition
	}
	if requested == current || (current == ClassStatusDraft && requested == ClassStatusActive) {
		return nil
	}
	return ErrInvalidClassTransition
}
