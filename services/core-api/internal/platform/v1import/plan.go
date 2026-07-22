package v1import

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type EntityType string

const (
	EntityUser       EntityType = "user"
	EntityTenant     EntityType = "tenant"
	EntityMembership EntityType = "membership"
	EntityClass      EntityType = "class"
)

const (
	SkipDeletedUser           = "deleted_user_out_of_scope"
	SkipDependencyUnavailable = "dependency_unavailable"
)

type plannedRecord struct {
	Ordinal    int
	EntityType EntityType
	ExternalID string
	TargetID   uuid.UUID
	SourceHash [sha256.Size]byte
	SkipReason string
	Desired    any
}

type desiredUser struct {
	Email       string
	DisplayName string
	Locale      string
	Timezone    string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type desiredTenant struct {
	Slug       string
	Name       string
	Locale     string
	Timezone   string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt *time.Time
}

type desiredMembership struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	Role      string
	Status    string
	JoinedAt  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type desiredClass struct {
	TenantID           uuid.UUID
	OwnerUserID        uuid.UUID
	Code               string
	Title              string
	Description        string
	Timezone           string
	Status             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ArchivedAt         *time.Time
	ArchivedFromStatus *string
}

func buildPlan(fixture Fixture) ([]plannedRecord, error) {
	total := len(fixture.Users) + len(fixture.Tenants) + len(fixture.Memberships) + len(fixture.Classes)
	plan := make([]plannedRecord, 0, total)
	userTargets := make(map[string]uuid.UUID, len(fixture.Users))
	tenantTargets := make(map[string]uuid.UUID, len(fixture.Tenants))
	activeMemberships := make(map[string]struct{}, len(fixture.Memberships))

	appendRecord := func(entityType EntityType, externalID string, desired any, skipReason string) error {
		sourceJSON, err := json.Marshal(desired)
		if err != nil {
			return fmt.Errorf("marshal %s source record: %w", entityType, err)
		}
		plan = append(plan, plannedRecord{
			Ordinal:    len(plan),
			EntityType: entityType,
			ExternalID: externalID,
			TargetID:   deterministicTargetID(fixture.SourceSystem, entityType, externalID),
			SourceHash: sha256.Sum256(sourceJSON),
			SkipReason: skipReason,
			Desired:    desired,
		})
		return nil
	}

	for _, source := range fixture.Users {
		desired := desiredUser{
			Email:       source.Email,
			DisplayName: strings.TrimSpace(source.DisplayName),
			Locale:      source.Locale,
			Timezone:    source.Timezone,
			Status:      mapUserStatus(source.Status),
			CreatedAt:   source.CreatedAt,
			UpdatedAt:   source.UpdatedAt,
		}
		skipReason := ""
		if source.Status == "deleted" {
			skipReason = SkipDeletedUser
		} else {
			userTargets[source.ExternalID] = deterministicTargetID(fixture.SourceSystem, EntityUser, source.ExternalID)
		}
		if err := appendRecord(EntityUser, source.ExternalID, desired, skipReason); err != nil {
			return nil, err
		}
	}

	for _, source := range fixture.Tenants {
		desired := desiredTenant{
			Slug:       source.Slug,
			Name:       strings.TrimSpace(source.Name),
			Locale:     source.Locale,
			Timezone:   source.Timezone,
			Status:     mapTenantStatus(source.Status),
			CreatedAt:  source.CreatedAt,
			UpdatedAt:  source.UpdatedAt,
			ArchivedAt: source.ArchivedAt,
		}
		tenantTargets[source.ExternalID] = deterministicTargetID(fixture.SourceSystem, EntityTenant, source.ExternalID)
		if err := appendRecord(EntityTenant, source.ExternalID, desired, ""); err != nil {
			return nil, err
		}
	}

	for _, source := range fixture.Memberships {
		tenantID, tenantExists := tenantTargets[source.TenantExternalID]
		userID, userExists := userTargets[source.UserExternalID]
		skipReason := ""
		if !tenantExists || !userExists {
			skipReason = SkipDependencyUnavailable
		}
		desired := desiredMembership{
			TenantID:  tenantID,
			UserID:    userID,
			Role:      mapMembershipRole(source.Role),
			Status:    mapMembershipStatus(source.Status),
			JoinedAt:  source.JoinedAt,
			CreatedAt: source.CreatedAt,
			UpdatedAt: source.UpdatedAt,
		}
		if skipReason == "" && desired.Status == "active" {
			activeMemberships[source.TenantExternalID+"\x00"+source.UserExternalID] = struct{}{}
		}
		if err := appendRecord(EntityMembership, source.ExternalID, desired, skipReason); err != nil {
			return nil, err
		}
	}

	for _, source := range fixture.Classes {
		tenantID, tenantExists := tenantTargets[source.TenantExternalID]
		ownerID, ownerExists := userTargets[source.OwnerUserExternalID]
		_, ownerMembershipExists := activeMemberships[source.TenantExternalID+"\x00"+source.OwnerUserExternalID]
		skipReason := ""
		if !tenantExists || !ownerExists || !ownerMembershipExists {
			skipReason = SkipDependencyUnavailable
		}
		status := mapClassStatus(source.Status)
		var archivedFromStatus *string
		if status == "archived" {
			value := "active"
			archivedFromStatus = &value
		}
		desired := desiredClass{
			TenantID:           tenantID,
			OwnerUserID:        ownerID,
			Code:               source.Code,
			Title:              strings.TrimSpace(source.Title),
			Description:        source.Description,
			Timezone:           source.Timezone,
			Status:             status,
			CreatedAt:          source.CreatedAt,
			UpdatedAt:          source.UpdatedAt,
			ArchivedAt:         source.ArchivedAt,
			ArchivedFromStatus: archivedFromStatus,
		}
		if err := appendRecord(EntityClass, source.ExternalID, desired, skipReason); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

func mapUserStatus(status string) string {
	switch status {
	case "disabled":
		return "suspended"
	case "deleted":
		return "deleted"
	default:
		return "active"
	}
}

func mapTenantStatus(status string) string {
	switch status {
	case "disabled":
		return "suspended"
	case "archived":
		return "archived"
	default:
		return "active"
	}
}

func mapMembershipRole(role string) string {
	switch role {
	case "administrator":
		return strings.Join([]string{"org", "admin"}, "_")
	case "instructor":
		return strings.Join([]string{"teach", "er"}, "")
	case "observer":
		return strings.Join([]string{"gu", "est"}, "")
	default:
		return strings.Join([]string{"stu", "dent"}, "")
	}
}

func mapMembershipStatus(status string) string {
	switch status {
	case "blocked":
		return "suspended"
	case "removed":
		return "removed"
	default:
		return "active"
	}
}

func mapClassStatus(status string) string {
	switch status {
	case "draft":
		return "draft"
	case "closed":
		return "archived"
	default:
		return "active"
	}
}
