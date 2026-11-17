package identity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionServicePreservesCookieExpiryThroughRotation(t *testing.T) {
	now := time.Now().UTC()
	for _, lifetime := range []time.Duration{8 * time.Hour, 30 * 24 * time.Hour} {
		tenant := Tenant{ID: uuid.New(), Role: "student", Status: "active"}
		repository := &memoryRepository{principal: Principal{
			SessionID: uuid.New(), User: User{ID: uuid.New()}, Memberships: []Tenant{tenant},
			SessionExpiresAt: now.Add(8 * time.Hour), CookieExpiresAt: now.Add(lifetime),
		}, metadata: SessionMetadata{ExpiresAt: now.Add(8 * time.Hour)}}
		service := newTestService(t, repository, nil, now)
		repository.metadata.TokenHash = service.crypto.Digest(sessionPurpose, "session")
		csrf, err := service.RotateCSRF(context.Background(), "session")
		if err != nil || !csrf.CookieExpiresAt.Equal(now.Add(lifetime)) || !csrf.ExpiresAt.Equal(now.Add(8*time.Hour)) {
			t.Fatal("CSRF rotation must preserve the separate cookie expiry")
		}
		rotated, err := service.SwitchActiveTenant(context.Background(), repository.principal, tenant.ID)
		if err != nil || !rotated.CookieExpiresAt.Equal(now.Add(lifetime)) || !rotated.ExpiresAt.Equal(now.Add(8*time.Hour)) {
			t.Fatal("tenant rotation must preserve the separate cookie expiry")
		}
		payload, err := json.Marshal(rotated.Principal)
		if err != nil || strings.Contains(string(payload), "ExpiresAt") || strings.Contains(string(payload), "expires_at") {
			t.Fatal("internal session expiry metadata must not change principal JSON")
		}
	}
}
