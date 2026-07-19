package audit

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditCursorRoundTripIsBoundToTenantAndFilters(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	from := time.Date(2026, 7, 1, 1, 2, 3, 4, time.FixedZone("ICT", 7*60*60))
	to := from.Add(48 * time.Hour)
	filter := Filter{
		OccurredFrom: &from,
		OccurredTo:   &to,
		Action:       ActionClassEnrollmentUpdateRole,
		ResourceType: "class_enrollment",
		ResourceID:   uuid.New(),
		Outcome:      OutcomeSucceeded,
		Limit:        25,
	}
	event := Event{
		ID:         uuid.New(),
		OccurredAt: from.Add(time.Hour),
	}

	cursor, err := encodeCursor(tenantID, filter, event)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	filter.Cursor = cursor
	decoded, err := decodeCursor(tenantID, filter)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded.ID != event.ID || !decoded.OccurredAt.Equal(event.OccurredAt) {
		t.Fatalf("unexpected cursor payload: %#v", decoded)
	}
	if decoded.OccurredAt.Location() != time.UTC {
		t.Fatalf("cursor timestamp must be normalized to UTC: %s", decoded.OccurredAt)
	}

	differentFrom := from.Add(time.Second)
	differentTo := to.Add(time.Second)
	tests := []struct {
		name     string
		tenantID uuid.UUID
		mutate   func(*Filter)
	}{
		{name: "tenant", tenantID: uuid.New()},
		{name: "occurred from", tenantID: tenantID, mutate: func(value *Filter) { value.OccurredFrom = &differentFrom }},
		{name: "occurred to", tenantID: tenantID, mutate: func(value *Filter) { value.OccurredTo = &differentTo }},
		{name: "action", tenantID: tenantID, mutate: func(value *Filter) { value.Action = ActionClassEnrollmentSuspend }},
		{name: "resource type", tenantID: tenantID, mutate: func(value *Filter) { value.ResourceType = "class" }},
		{name: "resource id", tenantID: tenantID, mutate: func(value *Filter) { value.ResourceID = uuid.New() }},
		{name: "outcome", tenantID: tenantID, mutate: func(value *Filter) { value.Outcome = OutcomeDenied }},
		{name: "limit", tenantID: tenantID, mutate: func(value *Filter) { value.Limit = 10 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changed := filter
			if test.mutate != nil {
				test.mutate(&changed)
			}
			if _, err := decodeCursor(test.tenantID, changed); !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("expected scope mismatch, got %v", err)
			}
		})
	}
}

func TestAuditCursorContainsOnlyStablePaginationAnchor(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	resourceID := uuid.New()
	requestID := "request-with-private-context"
	displayName := "Private Student"
	event := Event{
		ID:       uuid.New(),
		TenantID: tenantID,
		Actor: Actor{
			Type:        ActorTypeUser,
			UserID:      &actorID,
			DisplayName: &displayName,
		},
		Action:    ActionClassEnrollmentUpdateRole,
		Resource:  Resource{Type: "class_enrollment", ID: &resourceID},
		Outcome:   OutcomeSucceeded,
		RequestID: requestID,
		Metadata:  Metadata{"effect": "updated"},
		OccurredAt: time.Date(
			2026, 7, 19, 12, 30, 0, 0, time.UTC,
		),
	}
	cursor, err := encodeCursor(tenantID, Filter{}, event)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	contents, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatalf("decode cursor bytes: %v", err)
	}
	for _, privateValue := range []string{
		tenantID.String(), actorID.String(), resourceID.String(), displayName, requestID, "effect",
	} {
		if strings.Contains(string(contents), privateValue) {
			t.Fatalf("cursor leaked %q: %s", privateValue, contents)
		}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(contents, &payload); err != nil {
		t.Fatalf("decode cursor JSON: %v", err)
	}
	if len(payload) != 4 || payload["v"] == nil || payload["occurred_at"] == nil ||
		payload["id"] == nil || payload["scope_hash"] == nil {
		t.Fatalf("unexpected cursor fields: %v", payload)
	}
}

func TestDecodeAuditCursorRejectsMalformedPayloads(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	filter := Filter{}
	validPayload := cursorPayload{
		Version:    cursorVersion,
		OccurredAt: time.Now().UTC(),
		ID:         uuid.New(),
		ScopeHash:  filterScopeHash(tenantID, filter),
	}
	encode := func(value any) string {
		contents, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal cursor fixture: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(contents)
	}

	tests := []struct {
		name   string
		cursor string
	}{
		{name: "invalid base64", cursor: "%%%"},
		{name: "unknown field", cursor: encode(struct {
			cursorPayload
			Unexpected bool `json:"unexpected"`
		}{cursorPayload: validPayload, Unexpected: true})},
		{name: "wrong version", cursor: func() string {
			value := validPayload
			value.Version++
			return encode(value)
		}()},
		{name: "missing id", cursor: func() string {
			value := validPayload
			value.ID = uuid.Nil
			return encode(value)
		}()},
		{name: "missing timestamp", cursor: func() string {
			value := validPayload
			value.OccurredAt = time.Time{}
			return encode(value)
		}()},
		{name: "wrong scope", cursor: func() string {
			value := validPayload
			value.ScopeHash = strings.Repeat("0", 64)
			return encode(value)
		}()},
		{name: "oversized", cursor: strings.Repeat("a", 1025)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeCursor(tenantID, Filter{Cursor: test.cursor}); !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("expected invalid cursor, got %v", err)
			}
		})
	}
}

func TestNormalizeAuditFilter(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 7, 1, 9, 0, 0, 0, time.FixedZone("ICT", 7*60*60))
	to := from.Add(time.Hour)
	normalized, err := normalizeFilter(Filter{
		OccurredFrom: &from,
		OccurredTo:   &to,
		Action:       ActionTenantUpdate,
		ResourceType: "  tenant  ",
		ResourceID:   uuid.New(),
		Outcome:      OutcomeDenied,
	})
	if err != nil {
		t.Fatalf("normalize filter: %v", err)
	}
	if normalized.Limit != defaultPageLimit || normalized.ResourceType != "tenant" {
		t.Fatalf("unexpected normalized filter: %#v", normalized)
	}
	if normalized.OccurredFrom.Location() != time.UTC || normalized.OccurredTo.Location() != time.UTC {
		t.Fatalf("filter times must be UTC: %#v", normalized)
	}

	tests := []struct {
		name   string
		filter Filter
	}{
		{name: "limit below range", filter: Filter{Limit: -1}},
		{name: "limit above range", filter: Filter{Limit: maximumPageLimit + 1}},
		{name: "empty interval", filter: Filter{OccurredFrom: &from, OccurredTo: &from}},
		{name: "reversed interval", filter: Filter{OccurredFrom: &to, OccurredTo: &from}},
		{name: "unknown action", filter: Filter{Action: Action("tenant.destroy")}},
		{name: "resource id without type", filter: Filter{ResourceID: uuid.New()}},
		{name: "unknown outcome", filter: Filter{Outcome: Outcome("unknown")}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := normalizeFilter(test.filter); !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("expected invalid filter, got %v", err)
			}
		})
	}
}

func TestAuditMetadataRedactionAndDefensiveCopy(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"token", "access_token", "secret_value", "password_hint", "cookie_value",
		"session_id", "student_email", "display_name", "description", "raw_payload",
		"request_body", "sql_query", "error_message", "stack_trace", "token_hash",
	} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			if err := validateMetadata(Metadata{key: "must-not-persist"}); !errors.Is(err, ErrInvalidFilter) {
				t.Fatalf("forbidden metadata key %q was accepted: %v", key, err)
			}
		})
	}
	if err := validateMetadata(Metadata{
		"effect":        "updated",
		"class_role":    "co_teacher",
		"reason_code":   "resource_unavailable",
		"changed_field": "timezone",
	}); err != nil {
		t.Fatalf("safe metadata rejected: %v", err)
	}
	if err := validateMetadata(Metadata{"effect": strings.Repeat("x", 257)}); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("oversized metadata value was accepted: %v", err)
	}
	tooMany := make(Metadata, 33)
	for index := 0; index < 33; index++ {
		tooMany["field_"+string(rune('a'+index%26))+string(rune('a'+index/26))] = "safe"
	}
	if err := validateMetadata(tooMany); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("oversized metadata map was accepted: %v", err)
	}

	original := Metadata{"effect": "updated"}
	copied := copyMetadata(original)
	copied["effect"] = "unchanged"
	copied["reason_code"] = "conflict"
	if original["effect"] != "updated" || len(original) != 1 {
		t.Fatalf("metadata copy aliases caller map: original=%v copy=%v", original, copied)
	}
}

func TestAuditActionCatalogIsSortedUniqueAndMapsAllDomainEvents(t *testing.T) {
	t.Parallel()

	actions := Actions()
	if len(actions) != len(actionCatalog) {
		t.Fatalf("catalog size mismatch: actions=%d catalog=%d", len(actions), len(actionCatalog))
	}
	for index, action := range actions {
		if index > 0 && actions[index-1] >= action {
			t.Fatalf("actions are not strictly sorted: %v", actions)
		}
		if _, ok := actionCatalog[action]; !ok {
			t.Fatalf("unknown action returned from catalog: %q", action)
		}
	}
	for eventType, action := range domainEventActions {
		mapped, ok := ActionForDomainEvent(eventType)
		if !ok || mapped != action {
			t.Fatalf("domain event %q mapped to %q, %t; want %q", eventType, mapped, ok, action)
		}
		if _, ok := actionCatalog[action]; !ok {
			t.Fatalf("domain event %q maps outside action catalog: %q", eventType, action)
		}
	}
	if _, ok := ActionForDomainEvent("class.deleted"); ok {
		t.Fatal("unknown domain event unexpectedly mapped")
	}
}
