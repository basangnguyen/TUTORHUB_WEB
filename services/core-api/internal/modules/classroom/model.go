package classroom

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var classCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{2,31}$`)

type ClassStatus string

const (
	ClassStatusDraft    ClassStatus = "draft"
	ClassStatusActive   ClassStatus = "active"
	ClassStatusArchived ClassStatus = "archived"
)

type Class struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	OwnerUserID uuid.UUID
	Code        string
	Title       string
	Description string
	Status      ClassStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
}

type CreateClassParams struct {
	OwnerUserID uuid.UUID
	Code        string
	Title       string
	Description string
}

func (params CreateClassParams) normalized() (CreateClassParams, error) {
	params.Code = strings.ToUpper(strings.TrimSpace(params.Code))
	params.Title = strings.TrimSpace(params.Title)
	params.Description = strings.TrimSpace(params.Description)

	if params.OwnerUserID == uuid.Nil {
		return CreateClassParams{}, fmt.Errorf("%w: class owner is required", ErrInvalidClassInput)
	}
	if !classCodePattern.MatchString(params.Code) {
		return CreateClassParams{}, fmt.Errorf(
			"%w: class code must contain 3 to 32 uppercase letters, numbers, hyphens, or underscores",
			ErrInvalidClassInput,
		)
	}
	titleLength := utf8.RuneCountInString(params.Title)
	if titleLength < 1 || titleLength > 200 {
		return CreateClassParams{}, fmt.Errorf(
			"%w: class title must contain between 1 and 200 characters",
			ErrInvalidClassInput,
		)
	}
	if utf8.RuneCountInString(params.Description) > 4000 {
		return CreateClassParams{}, fmt.Errorf(
			"%w: class description cannot exceed 4000 characters",
			ErrInvalidClassInput,
		)
	}

	return params, nil
}
