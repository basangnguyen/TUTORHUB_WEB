package classroom

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseSessionTimestampValidatesZoneAndDST(t *testing.T) {
	t.Parallel()
	valid, err := parseSessionTimestamp(
		"2026-07-23T10:00:00+07:00", "Asia/Ho_Chi_Minh",
	)
	if err != nil || valid.UTC().Hour() != 3 {
		t.Fatalf("valid timestamp = %v, err = %v", valid, err)
	}
	_, err = parseSessionTimestamp(
		"2026-03-08T02:30:00-05:00", "America/New_York",
	)
	if !errors.Is(err, ErrSessionDSTGap) {
		t.Fatalf("DST gap error = %v, want %v", err, ErrSessionDSTGap)
	}
	for _, value := range []string{
		"2026-11-01T01:30:00-04:00",
		"2026-11-01T01:30:00-05:00",
	} {
		if _, err := parseSessionTimestamp(value, "America/New_York"); err != nil {
			t.Fatalf("valid overlap timestamp %q: %v", value, err)
		}
	}
	_, err = parseSessionTimestamp(
		"2026-07-23T10:00:00+08:00", "Asia/Ho_Chi_Minh",
	)
	if !errors.Is(err, ErrSessionTimezoneOffsetMismatch) {
		t.Fatalf("offset mismatch error = %v, want %v", err, ErrSessionTimezoneOffsetMismatch)
	}
}

func TestSessionParamsEnforceBoundsAndCompleteTimeTriplet(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 23, 3, 0, 0, 0, time.UTC)
	valid := CreateSessionParams{
		Title: "Math", StartsAt: now, EndsAt: now.Add(time.Hour),
		Timezone: "UTC", CreatedBy: uuid.New(),
	}
	if _, err := valid.normalized(); err != nil {
		t.Fatalf("valid params: %v", err)
	}
	tooLong := valid
	tooLong.EndsAt = now.Add(25 * time.Hour)
	if _, err := tooLong.normalized(); !errors.Is(err, ErrInvalidSessionRange) {
		t.Fatalf("too-long error = %v", err)
	}
	if _, err := (UpdateSessionParams{
		StartsAt: &now, ExpectedVersion: 1,
	}).normalized(); !errors.Is(err, ErrInvalidSessionInput) {
		t.Fatalf("partial time update error = %v", err)
	}
}
