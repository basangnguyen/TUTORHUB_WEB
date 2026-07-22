package media

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func FuzzLiveKitWebhookIdentifiers(f *testing.F) {
	tenantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	classID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	validRoom := RoomName(tenantID, classID)
	for _, seed := range []struct {
		roomName string
		eventID  string
	}{
		{validRoom, "EV_test-1"},
		{strings.ToUpper(validRoom), "event_2"},
		{"", ""},
		{"th_invalid_room", "event with spaces"},
		{"th_11111111111141118111111111111111_33333333333343338333333333333333", "event"},
		{validRoom + "_extra", strings.Repeat("a", 129)},
		{strings.Repeat("x", 4096), "\u0000"},
	} {
		f.Add(seed.roomName, seed.eventID)
	}

	f.Fuzz(func(t *testing.T, roomName string, eventID string) {
		parsedTenant, parsedClass, ok := ParseRoomName(roomName)
		if ok {
			if parsedTenant == uuid.Nil || parsedClass == uuid.Nil ||
				!strings.EqualFold(roomName, RoomName(parsedTenant, parsedClass)) {
				t.Fatalf("accepted non-canonical room name %q", roomName)
			}
		}
		if safeWebhookEventIDPattern.MatchString(eventID) {
			if len(eventID) == 0 || len(eventID) > 128 {
				t.Fatalf("accepted out-of-range webhook event ID length %d", len(eventID))
			}
		}
	})
}
