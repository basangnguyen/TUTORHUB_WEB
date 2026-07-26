package calendarinvitation

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	productID         = "-//TutorHub//Calendar Invitation 1.0//EN"
	maxICalendarBytes = 512 * 1024
)

// CanonicalICalendar is the immutable renderer output and its content hash.
type CanonicalICalendar struct {
	Method Method
	Bytes  []byte
	SHA256 [sha256.Size]byte
}

// RenderICalendar emits CRLF-only, UTF-8 iCalendar bytes. Every logical
// content line is folded to at most 75 octets without splitting a rune.
func RenderICalendar(
	snapshot Snapshot,
	policy RenderPolicy,
) (CanonicalICalendar, error) {
	if err := snapshot.Validate(); err != nil {
		return CanonicalICalendar{}, err
	}
	if err := policy.validateSnapshot(snapshot); err != nil {
		return CanonicalICalendar{}, err
	}
	method, err := snapshot.ICalendarMethod()
	if err != nil {
		return CanonicalICalendar{}, err
	}
	location, err := time.LoadLocation(snapshot.TimeZone)
	if err != nil {
		return CanonicalICalendar{}, fmt.Errorf("%w: load time zone: %v", ErrInvalidCalendar, err)
	}

	logicalLines := []string{
		"BEGIN:VCALENDAR",
		"PRODID:" + escapeText(productID),
		"VERSION:2.0",
		"CALSCALE:GREGORIAN",
		"METHOD:" + string(method),
	}
	timeZoneLines, err := renderVTimeZone(snapshot, location)
	if err != nil {
		return CanonicalICalendar{}, err
	}
	logicalLines = append(logicalLines, timeZoneLines...)
	logicalLines = append(logicalLines,
		"BEGIN:VEVENT",
		"UID:"+escapeText(snapshot.UID),
		"DTSTAMP:"+formatUTC(snapshot.DTStamp),
		fmt.Sprintf("SEQUENCE:%d", snapshot.Sequence),
		"DTSTART;TZID="+escapeParameter(snapshot.TimeZone)+":"+
			formatLocal(snapshot.StartsAt, location),
		"DTEND;TZID="+escapeParameter(snapshot.TimeZone)+":"+
			formatLocal(snapshot.EndsAt, location),
	)
	if snapshot.RecurrenceRule != "" {
		logicalLines = append(logicalLines, "RRULE:"+snapshot.RecurrenceRule)
	}
	if snapshot.RecurrenceID != nil {
		logicalLines = append(
			logicalLines,
			"RECURRENCE-ID;TZID="+escapeParameter(snapshot.TimeZone)+":"+
				formatLocal(*snapshot.RecurrenceID, location),
		)
	}
	if len(snapshot.ExDates) != 0 {
		exDates := append([]time.Time(nil), snapshot.ExDates...)
		sort.Slice(exDates, func(left int, right int) bool {
			return exDates[left].Before(exDates[right])
		})
		values := make([]string, len(exDates))
		for index, exDate := range exDates {
			values[index] = formatLocal(exDate, location)
		}
		logicalLines = append(
			logicalLines,
			"EXDATE;TZID="+escapeParameter(snapshot.TimeZone)+":"+strings.Join(values, ","),
		)
	}
	if snapshot.Effect == EffectCancel {
		logicalLines = append(logicalLines, "STATUS:CANCELLED")
	}
	logicalLines = append(logicalLines,
		"SUMMARY:"+escapeText(snapshot.Title),
		"DESCRIPTION:"+escapeText(calendarDescription(snapshot)),
		"LOCATION:"+escapeText(snapshot.Location),
		"URL;VALUE=URI:"+snapshot.DeepLink,
		renderOrganizer(snapshot.Organizer),
		renderAttendee(snapshot.Recipient.Attendee),
	)
	if snapshot.IncludeGuestList {
		visible := append([]Attendee(nil), snapshot.VisibleAttendees...)
		sort.Slice(visible, func(left int, right int) bool {
			if visible[left].RecipientID == visible[right].RecipientID {
				return visible[left].Email < visible[right].Email
			}
			return visible[left].RecipientID < visible[right].RecipientID
		})
		for _, attendee := range visible {
			logicalLines = append(logicalLines, renderAttendee(attendee))
		}
	}
	logicalLines = append(logicalLines, "END:VEVENT", "END:VCALENDAR")

	var output bytes.Buffer
	for _, logicalLine := range logicalLines {
		if strings.ContainsAny(logicalLine, "\r\n") {
			return CanonicalICalendar{}, fmt.Errorf(
				"%w: unescaped line break",
				ErrInvalidCalendar,
			)
		}
		output.WriteString(foldContentLine(logicalLine))
		output.WriteString("\r\n")
		if output.Len() > maxICalendarBytes {
			return CanonicalICalendar{}, fmt.Errorf(
				"%w: iCalendar exceeds %d bytes",
				ErrCanonicalTooLarge,
				maxICalendarBytes,
			)
		}
	}
	rendered := append([]byte(nil), output.Bytes()...)
	return CanonicalICalendar{
		Method: method,
		Bytes:  rendered,
		SHA256: sha256.Sum256(rendered),
	}, nil
}

func calendarDescription(snapshot Snapshot) string {
	if snapshot.Description == "" {
		return snapshot.Recipient.CTALabel + ": " + snapshot.DeepLink
	}
	return snapshot.Description + "\n\n" + snapshot.Recipient.CTALabel + ": " + snapshot.DeepLink
}

func renderOrganizer(organizer Organizer) string {
	return "ORGANIZER;CN=" + quoteParameter(organizer.DisplayName) +
		":mailto:" + organizer.Email
}

func renderAttendee(attendee Attendee) string {
	role := "REQ-PARTICIPANT"
	if attendee.Role == RoleOptional {
		role = "OPT-PARTICIPANT"
	}
	partStat := map[RSVPState]string{
		RSVPNeedsAction: "NEEDS-ACTION",
		RSVPAccepted:    "ACCEPTED",
		RSVPTentative:   "TENTATIVE",
		RSVPDeclined:    "DECLINED",
	}[attendee.RSVP]
	return "ATTENDEE;CN=" + quoteParameter(attendee.DisplayName) +
		";ROLE=" + role +
		";PARTSTAT=" + partStat +
		";RSVP=FALSE:mailto:" + attendee.Email
}

func escapeText(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		";", "\\;",
		",", "\\,",
		"\r\n", "\\n",
		"\n", "\\n",
		"\r", "\\n",
	)
	return replacer.Replace(value)
}

// quoteParameter uses RFC 6868 caret encoding for characters that cannot be
// represented safely inside an RFC 5545 quoted-string.
func quoteParameter(value string) string {
	value = strings.ReplaceAll(value, "^", "^^")
	value = strings.ReplaceAll(value, "\"", "^'")
	value = strings.ReplaceAll(value, "\r\n", "^n")
	value = strings.ReplaceAll(value, "\n", "^n")
	value = strings.ReplaceAll(value, "\r", "^n")
	return "\"" + value + "\""
}

func escapeParameter(value string) string {
	return strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,").Replace(value)
}

func foldContentLine(line string) string {
	if len(line) <= 75 {
		return line
	}
	var output strings.Builder
	remaining := line
	limit := 75
	for len(remaining) > limit {
		cut := utf8SafePrefix(remaining, limit)
		output.WriteString(remaining[:cut])
		output.WriteString("\r\n ")
		remaining = remaining[cut:]
		limit = 74
	}
	output.WriteString(remaining)
	return output.String()
}

func utf8SafePrefix(value string, maximumBytes int) int {
	if len(value) <= maximumBytes {
		return len(value)
	}
	cut := maximumBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	if cut == 0 {
		_, size := utf8.DecodeRuneInString(value)
		return size
	}
	return cut
}

func formatUTC(value time.Time) string {
	return value.UTC().Format("20060102T150405Z")
}

func formatLocal(value time.Time, location *time.Location) string {
	return value.In(location).Format("20060102T150405")
}

type zoneTransition struct {
	At         time.Time
	NameBefore string
	NameAfter  string
	From       int
	To         int
	Daylight   bool
}

func renderVTimeZone(snapshot Snapshot, location *time.Location) ([]string, error) {
	startYear := snapshot.StartsAt.In(location).Year() - 1
	endAt := snapshot.EndsAt
	if !snapshot.TimeZoneHorizonAt.IsZero() {
		endAt = snapshot.TimeZoneHorizonAt
	}
	endYear := endAt.In(location).Year() + 1
	if endYear-startYear > 4 {
		return nil, fmt.Errorf("%w: time zone expansion is not bounded", ErrInvalidCalendar)
	}

	rangeStart := time.Date(startYear, time.January, 1, 0, 0, 0, 0, time.UTC)
	rangeEnd := time.Date(endYear+1, time.January, 1, 0, 0, 0, 0, time.UTC)
	transitions := findZoneTransitions(location, rangeStart, rangeEnd)
	lines := []string{
		"BEGIN:VTIMEZONE",
		"TZID:" + escapeText(snapshot.TimeZone),
		"X-LIC-LOCATION:" + escapeText(snapshot.TimeZone),
	}
	if len(transitions) == 0 {
		local := rangeStart.In(location)
		name, offset := local.Zone()
		lines = append(lines,
			"BEGIN:STANDARD",
			"DTSTART:"+local.Format("20060102T150405"),
			"TZOFFSETFROM:"+formatUTCOffset(offset),
			"TZOFFSETTO:"+formatUTCOffset(offset),
			"TZNAME:"+escapeText(name),
			"END:STANDARD",
		)
	} else {
		for _, transition := range transitions {
			component := "STANDARD"
			if transition.Daylight {
				component = "DAYLIGHT"
			}
			lines = append(lines,
				"BEGIN:"+component,
				"DTSTART:"+formatTransitionLocal(transition.At, transition.From),
				"TZOFFSETFROM:"+formatUTCOffset(transition.From),
				"TZOFFSETTO:"+formatUTCOffset(transition.To),
				"TZNAME:"+escapeText(transition.NameAfter),
				"END:"+component,
			)
		}
	}
	lines = append(lines, "END:VTIMEZONE")
	return lines, nil
}

func findZoneTransitions(
	location *time.Location,
	start time.Time,
	end time.Time,
) []zoneTransition {
	const scanStep = 6 * time.Hour
	previous := start
	_, previousOffset := previous.In(location).Zone()
	var transitions []zoneTransition
	for cursor := start.Add(scanStep); cursor.Before(end); cursor = cursor.Add(scanStep) {
		_, currentOffset := cursor.In(location).Zone()
		if currentOffset != previousOffset {
			at := locateZoneTransition(location, previous, cursor, previousOffset)
			beforeName, from := at.Add(-time.Second).In(location).Zone()
			afterLocal := at.In(location)
			afterName, to := afterLocal.Zone()
			transitions = append(transitions, zoneTransition{
				At:         at,
				NameBefore: beforeName,
				NameAfter:  afterName,
				From:       from,
				To:         to,
				Daylight:   afterLocal.IsDST(),
			})
			currentOffset = to
		}
		previous = cursor
		previousOffset = currentOffset
	}
	return transitions
}

func formatTransitionLocal(at time.Time, offsetFromSeconds int) string {
	// RFC 5545 observance DTSTART is expressed in local wall time using
	// TZOFFSETFROM, not the post-transition wall time returned by time.Location.
	return at.UTC().
		Add(time.Duration(offsetFromSeconds) * time.Second).
		Format("20060102T150405")
}

func locateZoneTransition(
	location *time.Location,
	low time.Time,
	high time.Time,
	oldOffset int,
) time.Time {
	lowUnix := low.Unix()
	highUnix := high.Unix()
	for highUnix-lowUnix > 1 {
		middle := lowUnix + (highUnix-lowUnix)/2
		_, offset := time.Unix(middle, 0).In(location).Zone()
		if offset == oldOffset {
			lowUnix = middle
		} else {
			highUnix = middle
		}
	}
	return time.Unix(highUnix, 0).UTC()
}

func formatUTCOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	seconds := offsetSeconds % 60
	if seconds == 0 {
		return fmt.Sprintf("%s%02d%02d", sign, hours, minutes)
	}
	return fmt.Sprintf("%s%02d%02d%02d", sign, hours, minutes, seconds)
}
