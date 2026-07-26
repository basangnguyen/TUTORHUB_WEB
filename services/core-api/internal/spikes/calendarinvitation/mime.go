package calendarinvitation

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxCanonicalEmailBytes = 2 * 1024 * 1024
	maxRFC5322LineBytes    = 998
	rfc2047ChunkBytes      = 42
)

// MailEnvelope contains the verified sender identity selected by the
// infrastructure layer. It is distinct from the iCalendar organizer.
type MailEnvelope struct {
	FromEmail string
	FromName  string
	ReplyTo   string
}

// CanonicalEmail is the exact Raw MIME payload submitted to a provider.
type CanonicalEmail struct {
	Method         Method
	RecipientEmail string
	FromEmail      string
	Bytes          []byte
	SHA256         [sha256.Size]byte
	ICalendar      CanonicalICalendar
}

// RenderCanonicalEmail creates a deterministic multipart/alternative message
// containing exactly one authoritative text/calendar part.
func RenderCanonicalEmail(
	snapshot Snapshot,
	envelope MailEnvelope,
	policy RenderPolicy,
) (CanonicalEmail, error) {
	if err := snapshot.Validate(); err != nil {
		return CanonicalEmail{}, err
	}
	if err := validateEmail(envelope.FromEmail); err != nil {
		return CanonicalEmail{}, fmt.Errorf("%w: from email: %v", ErrInvalidSnapshot, err)
	}
	if err := validateBoundedText(
		"from name",
		envelope.FromName,
		1,
		maxDisplayNameBytes,
		false,
	); err != nil {
		return CanonicalEmail{}, err
	}
	if envelope.ReplyTo != "" {
		if err := validateEmail(envelope.ReplyTo); err != nil {
			return CanonicalEmail{}, fmt.Errorf("%w: reply-to: %v", ErrInvalidSnapshot, err)
		}
	}

	calendar, err := RenderICalendar(snapshot, policy)
	if err != nil {
		return CanonicalEmail{}, err
	}
	subject := localizedSubject(snapshot)
	plainBody := renderPlainBody(snapshot)
	htmlBody := renderHTMLBody(snapshot)
	boundarySeed := sha256.Sum256(bytes.Join([][]byte{
		calendar.Bytes,
		[]byte(envelope.FromEmail),
		[]byte(snapshot.Recipient.Email),
		[]byte(subject),
	}, []byte{0}))
	boundary := fmt.Sprintf("=_TutorHub_%x", boundarySeed[:18])

	var output bytes.Buffer
	writeAddressHeader(&output, "From", envelope.FromName, envelope.FromEmail)
	writeAddressHeader(
		&output,
		"To",
		snapshot.Recipient.DisplayName,
		snapshot.Recipient.Email,
	)
	if envelope.ReplyTo != "" {
		writeHeader(&output, "Reply-To", envelope.ReplyTo)
	}
	writeEncodedHeader(&output, "Subject", subject)
	writeHeader(&output, "Date", snapshot.DTStamp.UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	writeHeader(&output, "MIME-Version", "1.0")
	writeHeader(
		&output,
		"Content-Type",
		fmt.Sprintf("multipart/alternative; boundary=%q", boundary),
	)
	output.WriteString("\r\n")

	writeMIMEPart(
		&output,
		boundary,
		"text/plain; charset=UTF-8",
		"",
		[]byte(plainBody),
	)
	writeMIMEPart(
		&output,
		boundary,
		"text/html; charset=UTF-8",
		"",
		[]byte(htmlBody),
	)
	writeMIMEPart(
		&output,
		boundary,
		"text/calendar; charset=UTF-8; method="+string(calendar.Method),
		`inline; filename="invite.ics"`,
		calendar.Bytes,
	)
	output.WriteString("--")
	output.WriteString(boundary)
	output.WriteString("--\r\n")

	if output.Len() > maxCanonicalEmailBytes {
		return CanonicalEmail{}, fmt.Errorf(
			"%w: MIME exceeds %d bytes",
			ErrCanonicalTooLarge,
			maxCanonicalEmailBytes,
		)
	}
	raw := append([]byte(nil), output.Bytes()...)
	if err := validatePhysicalLineLengths(raw); err != nil {
		return CanonicalEmail{}, err
	}
	return CanonicalEmail{
		Method:         calendar.Method,
		RecipientEmail: snapshot.Recipient.Email,
		FromEmail:      envelope.FromEmail,
		Bytes:          raw,
		SHA256:         sha256.Sum256(raw),
		ICalendar:      calendar,
	}, nil
}

func localizedSubject(snapshot Snapshot) string {
	if snapshot.Recipient.Locale == LocaleEnglish {
		prefix := "Invitation"
		switch snapshot.Effect {
		case EffectUpdate:
			prefix = "Calendar update"
		case EffectCancel:
			prefix = "Calendar canceled"
		}
		return prefix + ": " + snapshot.Title
	}
	prefix := "Lời mời"
	switch snapshot.Effect {
	case EffectUpdate:
		prefix = "Cập nhật lịch"
	case EffectCancel:
		prefix = "Lịch đã hủy"
	}
	return prefix + ": " + snapshot.Title
}

func renderPlainBody(snapshot Snapshot) string {
	var body strings.Builder
	location := mustSnapshotLocation(snapshot.Recipient.ViewerTimeZone)
	startsAt := snapshot.StartsAt.In(location)
	endsAt := snapshot.EndsAt.In(location)
	body.WriteString(snapshot.Title)
	body.WriteString("\r\n\r\n")
	if snapshot.Recipient.Locale == LocaleEnglish {
		body.WriteString("Time: ")
	} else {
		body.WriteString("Thời gian: ")
	}
	body.WriteString(startsAt.Format("2006-01-02 15:04 MST"))
	body.WriteString(" – ")
	body.WriteString(endsAt.Format("15:04 MST"))
	body.WriteString("\r\n")
	if snapshot.Location != "" {
		if snapshot.Recipient.Locale == LocaleEnglish {
			body.WriteString("Location: ")
		} else {
			body.WriteString("Địa điểm: ")
		}
		body.WriteString(snapshot.Location)
		body.WriteString("\r\n")
	}
	if snapshot.Description != "" {
		body.WriteString("\r\n")
		body.WriteString(normalizeCRLF(snapshot.Description))
		body.WriteString("\r\n")
	}
	body.WriteString("\r\n")
	body.WriteString(snapshot.Recipient.CTALabel)
	body.WriteString(": ")
	body.WriteString(snapshot.DeepLink)
	body.WriteString("\r\n")
	if snapshot.Recipient.ResponseRequested && snapshot.Effect != EffectCancel {
		if snapshot.Recipient.Locale == LocaleEnglish {
			body.WriteString("Respond in TutorHub; email-client RSVP is not synchronized.")
		} else {
			body.WriteString("Phản hồi trong TutorHub; RSVP của ứng dụng email không được đồng bộ.")
		}
		body.WriteString("\r\n")
	}
	return body.String()
}

func renderHTMLBody(snapshot Snapshot) string {
	var body strings.Builder
	location := mustSnapshotLocation(snapshot.Recipient.ViewerTimeZone)
	startsAt := snapshot.StartsAt.In(location)
	endsAt := snapshot.EndsAt.In(location)
	body.WriteString("<!doctype html><html><body>")
	body.WriteString("<h1>")
	body.WriteString(html.EscapeString(snapshot.Title))
	if snapshot.Recipient.Locale == LocaleEnglish {
		body.WriteString("</h1><p><strong>Time:</strong> ")
	} else {
		body.WriteString("</h1><p><strong>Thời gian:</strong> ")
	}
	body.WriteString(html.EscapeString(startsAt.Format("2006-01-02 15:04 MST")))
	body.WriteString(" – ")
	body.WriteString(html.EscapeString(endsAt.Format("15:04 MST")))
	body.WriteString("</p>")
	if snapshot.Location != "" {
		if snapshot.Recipient.Locale == LocaleEnglish {
			body.WriteString("<p><strong>Location:</strong> ")
		} else {
			body.WriteString("<p><strong>Địa điểm:</strong> ")
		}
		body.WriteString(html.EscapeString(snapshot.Location))
		body.WriteString("</p>")
	}
	if snapshot.Description != "" {
		body.WriteString("<p>")
		description := strings.ReplaceAll(snapshot.Description, "\r\n", "\n")
		description = strings.ReplaceAll(description, "\r", "\n")
		body.WriteString(strings.ReplaceAll(html.EscapeString(description), "\n", "<br>"))
		body.WriteString("</p>")
	}
	body.WriteString(`<p><a rel="noreferrer" href="`)
	body.WriteString(html.EscapeString(snapshot.DeepLink))
	body.WriteString(`">`)
	body.WriteString(html.EscapeString(snapshot.Recipient.CTALabel))
	body.WriteString("</a></p>")
	if snapshot.Recipient.ResponseRequested && snapshot.Effect != EffectCancel {
		if snapshot.Recipient.Locale == LocaleEnglish {
			body.WriteString("<p>Respond in TutorHub; email-client RSVP is not synchronized.</p>")
		} else {
			body.WriteString("<p>Phản hồi trong TutorHub; RSVP của ứng dụng email không được đồng bộ.</p>")
		}
	}
	body.WriteString("</body></html>\r\n")
	return body.String()
}

func normalizeCRLF(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func mustSnapshotLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		// Snapshot validation has already loaded this IANA zone. UTC keeps this
		// helper total if the host time-zone database changes between calls.
		return time.UTC
	}
	return location
}

func writeHeader(output *bytes.Buffer, name string, value string) {
	output.WriteString(name)
	output.WriteString(": ")
	output.WriteString(value)
	output.WriteString("\r\n")
}

func writeEncodedHeader(output *bytes.Buffer, name string, value string) {
	words := encodeRFC2047Words(value)
	output.WriteString(name)
	output.WriteString(": ")
	for index, word := range words {
		if index != 0 {
			output.WriteString("\r\n ")
		}
		output.WriteString(word)
	}
	output.WriteString("\r\n")
}

func writeAddressHeader(
	output *bytes.Buffer,
	name string,
	displayName string,
	address string,
) {
	writeEncodedHeader(output, name, displayName)
	output.Truncate(output.Len() - 2)
	output.WriteString("\r\n <")
	output.WriteString(address)
	output.WriteString(">\r\n")
}

func encodeRFC2047Words(value string) []string {
	remaining := value
	words := make([]string, 0, (len(value)/rfc2047ChunkBytes)+1)
	for remaining != "" {
		cut := utf8SafePrefix(remaining, rfc2047ChunkBytes)
		chunk := remaining[:cut]
		words = append(
			words,
			"=?UTF-8?B?"+base64.StdEncoding.EncodeToString([]byte(chunk))+"?=",
		)
		remaining = remaining[cut:]
	}
	return words
}

func validatePhysicalLineLengths(raw []byte) error {
	for _, line := range bytes.Split(raw, []byte("\r\n")) {
		if len(line) > maxRFC5322LineBytes {
			return fmt.Errorf(
				"%w: MIME physical line exceeds %d bytes",
				ErrCanonicalTooLarge,
				maxRFC5322LineBytes,
			)
		}
		if !utf8.Valid(line) {
			return fmt.Errorf("%w: MIME physical line is not UTF-8", ErrInvalidSnapshot)
		}
	}
	return nil
}

func writeMIMEPart(
	output *bytes.Buffer,
	boundary string,
	contentType string,
	disposition string,
	body []byte,
) {
	output.WriteString("--")
	output.WriteString(boundary)
	output.WriteString("\r\n")
	writeHeader(output, "Content-Type", contentType)
	writeHeader(output, "Content-Transfer-Encoding", "base64")
	if disposition != "" {
		writeHeader(output, "Content-Disposition", disposition)
	}
	output.WriteString("\r\n")
	writeWrappedBase64(output, body)
}

func writeWrappedBase64(output *bytes.Buffer, body []byte) {
	encoded := base64.StdEncoding.EncodeToString(body)
	for len(encoded) > 76 {
		output.WriteString(encoded[:76])
		output.WriteString("\r\n")
		encoded = encoded[76:]
	}
	output.WriteString(encoded)
	output.WriteString("\r\n")
}
