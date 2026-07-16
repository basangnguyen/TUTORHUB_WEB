package policytest

import (
	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func ActiveOrganizationSubject(
	actorID uuid.UUID,
	tenantID uuid.UUID,
	role policy.OrganizationRole,
) policy.Subject {
	return policy.Subject{
		ActorID: actorID, ActiveTenantID: tenantID, MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{role},
	}
}

func WithClassRoles(subject policy.Subject, roles ...policy.ClassRole) policy.Subject {
	subject.ClassRoles = append([]policy.ClassRole(nil), roles...)
	return subject
}
