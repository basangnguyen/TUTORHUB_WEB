package audit

import "testing"

func TestConversationCreatedEventMapsToCatalogedCreateAction(t *testing.T) {
	t.Parallel()

	action, ok := ActionForDomainEvent("conversation.created.v1")
	if !ok || action != ActionConversationCreate {
		t.Fatalf("unexpected conversation action mapping: action=%q ok=%t", action, ok)
	}
	for _, cataloged := range Actions() {
		if cataloged == ActionConversationCreate {
			return
		}
	}
	t.Fatal("conversation create action is missing from audit catalog")
}
