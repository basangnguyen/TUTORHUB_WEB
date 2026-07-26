package calendarinvitation

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tutorhub-v2/core-api/internal/spikes/calendarrecurrence"
)

const (
	testInvitationID  = "invitation-01"
	testUID           = "urn:uuid:94e9ef6a-47d9-4c7c-a8f0-f2329f1ed76f"
	testTZDataVersion = "go-tzdata-2026a"
)

func deterministicSnapshot(t *testing.T, effect EffectKind, sequence uint32) Snapshot {
	t.Helper()

	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load test time zone: %v", err)
	}
	startsAt := time.Date(2026, time.March, 10, 9, 0, 0, 0, location)

	return Snapshot{
		InvitationID: testInvitationID,
		UID:          testUID,
		Sequence:     sequence,
		Effect:       effect,
		DTStamp:      time.Date(2026, time.February, 14, 8, 30, 0, 0, time.UTC),

		StartsAt:            startsAt,
		EndsAt:              time.Date(2026, time.March, 10, 10, 30, 0, 0, location),
		TimeZone:            "America/New_York",
		TimeZoneDataVersion: testTZDataVersion,
		TimeZoneHorizonAt:   startsAt.AddDate(0, 0, calendarrecurrence.MaxSeriesHorizonDays),

		Title:       "Ôn tập Mật mã học, nhóm nâng cao",
		Description: "Chuẩn bị chương 3; mang theo câu hỏi.\nKhông chia sẻ liên kết.",
		Location:    "Phòng Lab 2, tầng 5",
		DeepLink:    "https://tutorhub.example/calendar/invitations/invitation-01",

		Organizer: Organizer{
			UserID:      "teacher-01",
			Email:       "teacher@example.edu",
			DisplayName: "Cô Nguyễn An",
		},
		Recipient: RecipientSnapshot{
			Attendee: Attendee{
				RecipientID:       "student-01",
				Email:             "student@example.edu",
				DisplayName:       "Nguyễn Bình",
				Role:              RoleRequired,
				Source:            AudienceSourceRoster,
				External:          false,
				ResponseRequested: true,
				RSVP:              RSVPAccepted,
				RSVPSource:        RSVPSourceAuthenticated,
				CanSeeGuestList:   false,
			},
			CTALabel:       "Mở lịch trong TutorHub",
			Locale:         LocaleVietnamese,
			ViewerTimeZone: "Asia/Ho_Chi_Minh",
		},

		RecurrenceRule: "FREQ=WEEKLY;COUNT=4;BYDAY=TU",
		OverlapPolicy:  calendarrecurrence.OverlapReject,
		ExDates: []time.Time{
			time.Date(2026, time.March, 31, 9, 0, 0, 0, location),
			time.Date(2026, time.March, 24, 9, 0, 0, 0, location),
		},
	}
}

func deterministicRenderPolicy(t *testing.T) RenderPolicy {
	t.Helper()
	policy, err := NewRenderPolicy(
		testTZDataVersion,
		"https://tutorhub.example",
	)
	if err != nil {
		t.Fatalf("NewRenderPolicy(): %v", err)
	}
	return policy
}

func TestRenderICalendarRequestUpdateCancel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		effect     EffectKind
		sequence   uint32
		method     Method
		isCanceled bool
	}{
		{name: "request", effect: EffectInvite, sequence: 7, method: MethodRequest},
		{name: "update", effect: EffectUpdate, sequence: 8, method: MethodRequest},
		{
			name:       "cancel",
			effect:     EffectCancel,
			sequence:   9,
			method:     MethodCancel,
			isCanceled: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := RenderICalendar(
				deterministicSnapshot(t, test.effect, test.sequence),
				deterministicRenderPolicy(t),
			)
			if err != nil {
				t.Fatalf("render iCalendar: %v", err)
			}
			text := string(rendered.Bytes)

			if rendered.Method != test.method {
				t.Fatalf("method = %q, want %q", rendered.Method, test.method)
			}
			assertCalendarLine(t, text, "METHOD:"+string(test.method))
			assertCalendarLine(t, text, "UID:"+testUID)
			assertCalendarLine(t, text, "SEQUENCE:"+uint32String(test.sequence))
			if got := hasCalendarLine(text, "STATUS:CANCELLED"); got != test.isCanceled {
				t.Fatalf("STATUS:CANCELLED presence = %t, want %t", got, test.isCanceled)
			}
		})
	}
}

func TestRenderICalendarRequiredFieldsAndRecurrence(t *testing.T) {
	t.Parallel()

	snapshot := deterministicSnapshot(t, EffectInvite, 7)
	rendered, err := RenderICalendar(snapshot, deterministicRenderPolicy(t))
	if err != nil {
		t.Fatalf("render iCalendar: %v", err)
	}
	text := string(rendered.Bytes)

	for _, line := range []string{
		"BEGIN:VCALENDAR",
		"PRODID:-//TutorHub//Calendar Invitation 1.0//EN",
		"VERSION:2.0",
		"CALSCALE:GREGORIAN",
		"METHOD:REQUEST",
		"BEGIN:VTIMEZONE",
		"TZID:America/New_York",
		"END:VTIMEZONE",
		"BEGIN:VEVENT",
		"UID:" + testUID,
		"DTSTAMP:20260214T083000Z",
		"SEQUENCE:7",
		"DTSTART;TZID=America/New_York:20260310T090000",
		"DTEND;TZID=America/New_York:20260310T103000",
		"RRULE:FREQ=WEEKLY;COUNT=4;BYDAY=TU",
		"EXDATE;TZID=America/New_York:20260324T090000,20260331T090000",
		"END:VEVENT",
		"END:VCALENDAR",
	} {
		assertCalendarLine(t, text, line)
	}
	if hasCalendarLine(text, "RECURRENCE-ID;TZID=America/New_York:20260317T090000") {
		t.Fatal("recurring master unexpectedly contains RECURRENCE-ID")
	}

	unfolded := unfoldICalendar(text)
	if !strings.Contains(
		unfolded,
		"ORGANIZER;CN=\"Cô Nguyễn An\":mailto:teacher@example.edu\r\n",
	) {
		t.Fatalf("organizer is missing or not recipient-safe:\n%s", unfolded)
	}
	if !strings.Contains(
		unfolded,
		"ATTENDEE;CN=\"Nguyễn Bình\";ROLE=REQ-PARTICIPANT;"+
			"PARTSTAT=ACCEPTED;RSVP=FALSE:mailto:student@example.edu\r\n",
	) {
		t.Fatalf("recipient attendee is missing or does not use CTA-only RSVP:\n%s", unfolded)
	}
	if got := strings.Count(unfolded, "\r\nATTENDEE;"); got != 1 {
		t.Fatalf("ATTENDEE count = %d, want exactly one recipient", got)
	}

	override := deterministicSnapshot(t, EffectUpdate, 8)
	recurrenceID := time.Date(
		2026,
		time.March,
		17,
		9,
		0,
		0,
		0,
		override.StartsAt.Location(),
	)
	override.RecurrenceRule = ""
	override.OverlapPolicy = ""
	override.TimeZoneHorizonAt = time.Time{}
	override.ExDates = nil
	override.RecurrenceID = &recurrenceID
	overrideRendered, err := RenderICalendar(override, deterministicRenderPolicy(t))
	if err != nil {
		t.Fatalf("render recurrence override: %v", err)
	}
	overrideText := string(overrideRendered.Bytes)
	assertCalendarLine(
		t,
		overrideText,
		"RECURRENCE-ID;TZID=America/New_York:20260317T090000",
	)
	if strings.Contains(unfoldICalendar(overrideText), "\r\nRRULE:") ||
		strings.Contains(unfoldICalendar(overrideText), "\r\nEXDATE") {
		t.Fatal("recurrence override unexpectedly contains RRULE or EXDATE")
	}
}

func TestRenderICalendarCanonicalCRLFFoldingAndUTF8(t *testing.T) {
	t.Parallel()

	snapshot := deterministicSnapshot(t, EffectInvite, 7)
	snapshot.Title = strings.Repeat("Buổi học tiếng Việt rất dài ", 10)
	rendered, err := RenderICalendar(snapshot, deterministicRenderPolicy(t))
	if err != nil {
		t.Fatalf("render iCalendar: %v", err)
	}

	if !utf8.Valid(rendered.Bytes) {
		t.Fatal("rendered iCalendar is not valid UTF-8")
	}
	withoutCRLF := bytes.ReplaceAll(rendered.Bytes, []byte("\r\n"), nil)
	if bytes.ContainsAny(withoutCRLF, "\r\n") {
		t.Fatal("rendered iCalendar contains a lone CR or LF")
	}
	for index, line := range bytes.Split(rendered.Bytes, []byte("\r\n")) {
		if len(line) > 75 {
			t.Fatalf("physical line %d has %d octets, want <= 75: %q", index+1, len(line), line)
		}
		if !utf8.Valid(line) {
			t.Fatalf("physical line %d splits an UTF-8 rune: %q", index+1, line)
		}
	}

	wantHash := sha256.Sum256(rendered.Bytes)
	if rendered.SHA256 != wantHash {
		t.Fatalf("canonical iCalendar hash mismatch")
	}
}

func TestRenderICalendarEscapesTextAndRejectsInjection(t *testing.T) {
	t.Parallel()

	snapshot := deterministicSnapshot(t, EffectInvite, 7)
	rendered, err := RenderICalendar(snapshot, deterministicRenderPolicy(t))
	if err != nil {
		t.Fatalf("render iCalendar: %v", err)
	}
	unfolded := unfoldICalendar(string(rendered.Bytes))
	if !strings.Contains(
		unfolded,
		"SUMMARY:Ôn tập Mật mã học\\, nhóm nâng cao\r\n",
	) {
		t.Fatalf("SUMMARY text was not escaped: %s", unfolded)
	}
	if !strings.Contains(
		unfolded,
		"DESCRIPTION:Chuẩn bị chương 3\\; mang theo câu hỏi.\\n"+
			"Không chia sẻ liên kết.\\n\\n",
	) {
		t.Fatalf("DESCRIPTION text was not escaped: %s", unfolded)
	}

	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{
			name: "title header injection",
			mutate: func(value *Snapshot) {
				value.Title = "Lịch hợp lệ\r\nATTENDEE:mailto:attacker@example.com"
			},
		},
		{
			name: "organizer display name injection",
			mutate: func(value *Snapshot) {
				value.Organizer.DisplayName = "Teacher\r\nBCC: attacker@example.com"
			},
		},
		{
			name: "recipient email injection",
			mutate: func(value *Snapshot) {
				value.Recipient.Email = "student@example.edu\r\nBcc:attacker@example.com"
			},
		},
		{
			name: "deep link injection",
			mutate: func(value *Snapshot) {
				value.DeepLink = "https://tutorhub.example/event\r\nATTACH:file:///secret"
			},
		},
		{
			name: "recurrence rule injection",
			mutate: func(value *Snapshot) {
				value.RecurrenceRule = "FREQ=WEEKLY;COUNT=4\r\nATTACH=FILE"
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			value := deterministicSnapshot(t, EffectInvite, 7)
			test.mutate(&value)
			if _, err := RenderICalendar(value, deterministicRenderPolicy(t)); err == nil {
				t.Fatal("RenderICalendar() error = nil, want validation failure")
			} else if !errors.Is(err, ErrInvalidSnapshot) &&
				!errors.Is(err, ErrInvalidAudience) &&
				!errors.Is(err, ErrInvalidCalendar) {
				t.Fatalf("RenderICalendar() error = %v, want a typed validation error", err)
			}
		})
	}
}

func TestRecurringSnapshotUsesCanonicalCivilHorizonAndRejectsSparseOverflow(t *testing.T) {
	t.Parallel()

	validSparse := deterministicSnapshot(t, EffectInvite, 7)
	validSparse.RecurrenceRule = "FREQ=YEARLY;COUNT=2;BYMONTH=12;BYMONTHDAY=31"
	validSparse.ExDates = nil
	if _, err := RenderICalendar(validSparse, deterministicRenderPolicy(t)); err != nil {
		t.Fatalf("bounded sparse recurrence was rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{
			name: "non canonical horizon",
			mutate: func(value *Snapshot) {
				value.TimeZoneHorizonAt = value.TimeZoneHorizonAt.Add(time.Hour)
			},
		},
		{
			name: "sparse count cannot finish inside horizon",
			mutate: func(value *Snapshot) {
				value.RecurrenceRule = "FREQ=YEARLY;COUNT=4;BYMONTH=2;BYMONTHDAY=29"
				value.ExDates = nil
			},
		},
		{
			name: "master mixes recurrence id",
			mutate: func(value *Snapshot) {
				recurrenceID := value.StartsAt.AddDate(0, 0, 7)
				value.RecurrenceID = &recurrenceID
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			snapshot := deterministicSnapshot(t, EffectInvite, 7)
			test.mutate(&snapshot)
			if _, err := RenderICalendar(snapshot, deterministicRenderPolicy(t)); err == nil {
				t.Fatal("RenderICalendar() error = nil for invalid recurrence snapshot")
			}
		})
	}
}

func assertCalendarLine(t *testing.T, calendar string, line string) {
	t.Helper()
	if !hasCalendarLine(calendar, line) {
		t.Fatalf("calendar does not contain logical line %q:\n%s", line, unfoldICalendar(calendar))
	}
}

func hasCalendarLine(calendar string, line string) bool {
	return strings.Contains(unfoldICalendar(calendar), line+"\r\n")
}

func unfoldICalendar(calendar string) string {
	return strings.ReplaceAll(calendar, "\r\n ", "")
}

func uint32String(value uint32) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	buffer := [10]byte{}
	cursor := len(buffer)
	for value > 0 {
		cursor--
		buffer[cursor] = digits[value%10]
		value /= 10
	}
	return string(buffer[cursor:])
}
