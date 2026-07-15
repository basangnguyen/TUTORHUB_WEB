package tenancy

import (
	"fmt"

	"github.com/google/uuid"
)

type Context struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
}

func New(tenantID uuid.UUID, actorID uuid.UUID) (Context, error) {
	context := Context{TenantID: tenantID, ActorID: actorID}
	if err := context.Validate(); err != nil {
		return Context{}, err
	}

	return context, nil
}

func (context Context) Validate() error {
	if context.TenantID == uuid.Nil {
		return fmt.Errorf("tenant context requires a tenant ID")
	}
	if context.ActorID == uuid.Nil {
		return fmt.Errorf("tenant context requires an actor ID")
	}

	return nil
}
