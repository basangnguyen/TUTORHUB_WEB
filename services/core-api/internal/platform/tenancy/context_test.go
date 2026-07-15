package tenancy

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewRequiresTenantAndActor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		tenantID uuid.UUID
		actorID  uuid.UUID
	}{
		{name: "missing tenant", actorID: uuid.New()},
		{name: "missing actor", tenantID: uuid.New()},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(testCase.tenantID, testCase.actorID); err == nil {
				t.Fatal("expected invalid tenant context")
			}
		})
	}
}

func TestNewAcceptsValidContext(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	context, err := New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create tenant context: %v", err)
	}
	if context.TenantID != tenantID || context.ActorID != actorID {
		t.Fatalf("unexpected tenant context: %+v", context)
	}
}
