package audit

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func FuzzDecodeAuditCursor(f *testing.F) {
	tenantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	foreignTenantID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	filter := Filter{Limit: 25}
	event := Event{
		ID:         uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		OccurredAt: time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC),
	}
	valid, err := encodeCursor(tenantID, filter, event)
	if err != nil {
		f.Fatalf("encode valid audit cursor: %v", err)
	}
	unknownPayload, err := json.Marshal(struct {
		cursorPayload
		Unexpected bool `json:"unexpected"`
	}{cursorPayload: cursorPayload{
		Version:    cursorVersion,
		OccurredAt: event.OccurredAt,
		ID:         event.ID,
		ScopeHash:  filterScopeHash(tenantID, filter),
	}, Unexpected: true})
	if err != nil {
		f.Fatalf("marshal unknown audit cursor payload: %v", err)
	}
	unknown := base64.RawURLEncoding.EncodeToString(unknownPayload)
	foreignPayload, err := json.Marshal(cursorPayload{
		Version:    cursorVersion,
		OccurredAt: event.OccurredAt,
		ID:         event.ID,
		ScopeHash:  filterScopeHash(foreignTenantID, filter),
	})
	if err != nil {
		f.Fatalf("marshal foreign audit cursor payload: %v", err)
	}
	foreign := base64.RawURLEncoding.EncodeToString(foreignPayload)
	if _, err := decodeCursor(tenantID, Filter{Limit: 25, Cursor: foreign}); err == nil {
		f.Fatal("foreign-tenant audit cursor was accepted")
	}
	f.Add("")
	f.Add("%%%")
	f.Add(valid)
	f.Add(unknown)
	f.Add(foreign)
	f.Add(" " + valid)

	f.Fuzz(func(t *testing.T, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		decoded, err := decodeCursor(tenantID, Filter{Limit: 25, Cursor: value})
		if err != nil {
			return
		}
		if decoded.ID == uuid.Nil || decoded.OccurredAt.IsZero() ||
			decoded.ScopeHash != filterScopeHash(tenantID, filter) {
			t.Fatalf("accepted invalid audit cursor: value=%q payload=%+v", value, decoded)
		}
	})
}
