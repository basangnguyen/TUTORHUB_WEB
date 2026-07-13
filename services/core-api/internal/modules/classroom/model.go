package classroom

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

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
		return CreateClassParams{}, fmt.Errorf("class owner is required")
	}
	if len(params.Code) < 3 || len(params.Code) > 32 {
		return CreateClassParams{}, fmt.Errorf("class code must contain between 3 and 32 characters")
	}
	if len(params.Title) < 1 || len(params.Title) > 200 {
		return CreateClassParams{}, fmt.Errorf("class title must contain between 1 and 200 characters")
	}

	return params, nil
}
