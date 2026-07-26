package calendarinvitation

import (
	"bufio"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type canonicalGolden struct {
	ICalendarSHA256 string
	MIMESHA256      string
}

func TestCanonicalInvitationGoldenHashes(t *testing.T) {
	t.Parallel()

	expected := readCanonicalGoldenHashes(t)
	scenarios := canonicalGoldenScenarios(t)
	if len(expected) != len(scenarios) {
		t.Fatalf("golden case count = %d, want %d", len(expected), len(scenarios))
	}
	for name, snapshot := range scenarios {
		name := name
		snapshot := snapshot
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			email, err := RenderCanonicalEmail(
				snapshot,
				deterministicEnvelope(),
				deterministicRenderPolicy(t),
			)
			if err != nil {
				t.Fatalf("RenderCanonicalEmail(): %v", err)
			}
			got := canonicalGolden{
				ICalendarSHA256: hex.EncodeToString(email.ICalendar.SHA256[:]),
				MIMESHA256:      hex.EncodeToString(email.SHA256[:]),
			}
			want, exists := expected[name]
			if !exists {
				t.Fatalf(
					"missing golden case; add:\n%s %s %s",
					name,
					got.ICalendarSHA256,
					got.MIMESHA256,
				)
			}
			if got != want {
				t.Fatalf(
					"canonical bytes changed; review then update:\n%s %s %s\nwant ICS=%s MIME=%s",
					name,
					got.ICalendarSHA256,
					got.MIMESHA256,
					want.ICalendarSHA256,
					want.MIMESHA256,
				)
			}
		})
	}
}

func canonicalGoldenScenarios(t *testing.T) map[string]Snapshot {
	t.Helper()

	create := deterministicSnapshot(t, EffectInvite, 7)

	update := deterministicSnapshot(t, EffectUpdate, 8)
	update.Description = "Nội dung cập nhật đã được organizer xác nhận."

	reschedule := deterministicSnapshot(t, EffectUpdate, 9)
	reschedule.StartsAt = reschedule.StartsAt.Add(2 * time.Hour)
	reschedule.EndsAt = reschedule.EndsAt.Add(2 * time.Hour)
	reschedule.TimeZoneHorizonAt = reschedule.StartsAt.In(
		reschedule.StartsAt.Location(),
	).AddDate(0, 0, 730)
	for index := range reschedule.ExDates {
		reschedule.ExDates[index] = reschedule.ExDates[index].Add(2 * time.Hour)
	}

	role := deterministicSnapshot(t, EffectUpdate, 10)
	role.Recipient.Role = RoleOptional
	role.Recipient.RSVP = RSVPNeedsAction
	role.Recipient.RSVPSource = RSVPSourceNone

	override := deterministicSnapshot(t, EffectUpdate, 11)
	recurrenceID := override.StartsAt.AddDate(0, 0, 7)
	override.RecurrenceRule = ""
	override.OverlapPolicy = ""
	override.TimeZoneHorizonAt = time.Time{}
	override.ExDates = nil
	override.RecurrenceID = &recurrenceID
	override.StartsAt = recurrenceID.Add(time.Hour)
	override.EndsAt = override.StartsAt.Add(90 * time.Minute)

	split := deterministicSnapshot(t, EffectInvite, 0)
	split.InvitationID = "invitation-split-02"
	split.UID = "urn:uuid:36de746c-f50a-4dcc-8f9d-648ae38cd5a8"
	split.DTStamp = split.DTStamp.Add(time.Minute)
	split.StartsAt = split.StartsAt.AddDate(0, 1, 0)
	split.EndsAt = split.EndsAt.AddDate(0, 1, 0)
	split.TimeZoneHorizonAt = split.StartsAt.In(
		split.StartsAt.Location(),
	).AddDate(0, 0, 730)
	split.ExDates = nil
	split.DeepLink = "https://tutorhub.example/calendar/invitations/invitation-split-02"

	cancel := deterministicSnapshot(t, EffectCancel, 13)

	return map[string]Snapshot{
		"create":     create,
		"update":     update,
		"reschedule": reschedule,
		"role":       role,
		"override":   override,
		"split":      split,
		"cancel":     cancel,
	}
}

func readCanonicalGoldenHashes(t *testing.T) map[string]canonicalGolden {
	t.Helper()
	path := filepath.Join("testdata", "canonical_hashes.txt")
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open canonical golden hashes: %v", err)
	}
	defer file.Close()

	result := make(map[string]canonicalGolden)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("invalid canonical golden line %q", line)
		}
		if _, duplicate := result[fields[0]]; duplicate {
			t.Fatalf("duplicate canonical golden case %q", fields[0])
		}
		for _, value := range fields[1:] {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != 32 {
				t.Fatalf("invalid SHA-256 in canonical golden line %q", line)
			}
		}
		result[fields[0]] = canonicalGolden{
			ICalendarSHA256: fields[1],
			MIMESHA256:      fields[2],
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read canonical golden hashes: %v", err)
	}
	return result
}
