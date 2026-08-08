package discovery

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	ResultSession      ResultKind = "session"
	ResultConversation ResultKind = "conversation"
	ResultFile         ResultKind = "file"
)

var (
	ErrInvalidInput = errors.New("invalid discovery input")
	ErrAccessDenied = errors.New("discovery access denied")
)

type ResultKind string

type AccessContext struct {
	TenantID          uuid.UUID
	ActorID           uuid.UUID
	MembershipActive  bool
	OrganizationRoles []policy.OrganizationRole
}

type RecentFile struct {
	ID                uuid.UUID `json:"id"`
	ClassID           uuid.UUID `json:"class_id"`
	ClassTitle        string    `json:"class_title"`
	DisplayName       string    `json:"display_name"`
	DeclaredMediaType string    `json:"declared_media_type"`
	SizeBytes         int64     `json:"size_bytes"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SearchResult struct {
	Kind       ResultKind `json:"kind"`
	ID         uuid.UUID  `json:"id"`
	ClassID    *uuid.UUID `json:"class_id,omitempty"`
	Title      string     `json:"title"`
	Context    string     `json:"context"`
	OccurredAt time.Time  `json:"occurred_at"`
}

type SearchPage struct {
	Items []SearchResult `json:"items"`
}

type Repository interface {
	RecentFiles(context.Context, AccessContext, int) ([]RecentFile, error)
	Search(context.Context, AccessContext, string, int) ([]SearchResult, error)
}

type ServiceAPI interface {
	RecentFiles(context.Context, AccessContext, int) ([]RecentFile, error)
	Search(context.Context, AccessContext, string, int) (SearchPage, error)
}
