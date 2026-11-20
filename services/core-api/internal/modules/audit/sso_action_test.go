package audit

import (
	"slices"
	"testing"
)

func TestTenantSSOActions(t *testing.T) {
	for event, want := range map[string]Action{
		"tenant.sso_connection.created": "tenant.sso_connection.create",
		"tenant.sso_connection.updated": "tenant.sso_connection.update",
		"tenant.sso_domain.created":     "tenant.sso_domain.create",
		"tenant.sso_domain.rotated":     "tenant.sso_domain.rotate",
		"tenant.sso_domain.verified":    "tenant.sso_domain.verify",
		"tenant.sso_domain.disabled":    "tenant.sso_domain.disable",
	} {
		action, ok := ActionForDomainEvent(event)
		if !ok || action != want || !slices.Contains(Actions(), action) {
			t.Errorf("%s mapped to %q, want cataloged action %q", event, action, want)
		}
	}
}
