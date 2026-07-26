package calendarinvitation

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func deterministicEnvelope() MailEnvelope {
	return MailEnvelope{
		FromEmail: "calendar@example.edu",
		FromName:  "TutorHub Calendar",
		ReplyTo:   "support@example.edu",
	}
}

func TestRenderCanonicalEmailIsDeterministicAndHasCanonicalHash(t *testing.T) {
	t.Parallel()

	snapshot := deterministicSnapshot(t, EffectUpdate, 8)
	first, err := RenderCanonicalEmail(
		snapshot,
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("first RenderCanonicalEmail(): %v", err)
	}
	second, err := RenderCanonicalEmail(
		snapshot,
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("second RenderCanonicalEmail(): %v", err)
	}

	if !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatal("same immutable snapshot produced different MIME bytes")
	}
	if first.SHA256 != second.SHA256 {
		t.Fatal("same immutable snapshot produced different canonical hashes")
	}
	if got := sha256.Sum256(first.Bytes); got != first.SHA256 {
		t.Fatal("canonical MIME hash does not match raw bytes")
	}
	if first.ICalendar.SHA256 != sha256.Sum256(first.ICalendar.Bytes) {
		t.Fatal("embedded iCalendar hash does not match canonical iCalendar bytes")
	}

	changed := snapshot
	changed.Sequence++
	third, err := RenderCanonicalEmail(
		changed,
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("changed RenderCanonicalEmail(): %v", err)
	}
	if bytes.Equal(first.Bytes, third.Bytes) || first.SHA256 == third.SHA256 {
		t.Fatal("a new invitation sequence reused the prior canonical MIME")
	}
}

func TestRenderCanonicalEmailHasExactlyThreeBase64AlternativeParts(t *testing.T) {
	t.Parallel()

	rendered, err := RenderCanonicalEmail(
		deterministicSnapshot(t, EffectCancel, 9),
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail(): %v", err)
	}

	message, err := mail.ReadMessage(bytes.NewReader(rendered.Bytes))
	if err != nil {
		t.Fatalf("parse MIME message: %v", err)
	}
	if message.Header.Get("Cc") != "" || message.Header.Get("Bcc") != "" {
		t.Fatalf("recipient-isolated MIME unexpectedly has Cc/Bcc: %#v", message.Header)
	}
	if got := message.Header.Get("To"); strings.Count(got, "@") != 1 {
		t.Fatalf("To header = %q, want exactly one address", got)
	}

	mediaType, parameters, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse root Content-Type: %v", err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("root media type = %q, want multipart/alternative", mediaType)
	}
	reader := multipart.NewReader(message.Body, parameters["boundary"])

	wantMediaTypes := []string{"text/plain", "text/html", "text/calendar"}
	var calendarParts int
	for index, wantMediaType := range wantMediaTypes {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("read MIME part %d: %v", index, err)
		}
		partMediaType, partParameters, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse MIME part %d Content-Type: %v", index, err)
		}
		if partMediaType != wantMediaType {
			t.Fatalf("MIME part %d media type = %q, want %q", index, partMediaType, wantMediaType)
		}
		if !strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
			t.Fatalf("MIME part %d is not base64 encoded", index)
		}
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatalf("decode MIME part %d: %v", index, err)
		}
		if len(decoded) == 0 {
			t.Fatalf("decoded MIME part %d is empty", index)
		}
		if partMediaType == "text/calendar" {
			calendarParts++
			if got := partParameters["method"]; got != string(rendered.Method) {
				t.Fatalf("calendar MIME method = %q, want %q", got, rendered.Method)
			}
			if !bytes.Equal(decoded, rendered.ICalendar.Bytes) {
				t.Fatal("calendar MIME part differs from canonical iCalendar bytes")
			}
			if disposition := part.Header.Get("Content-Disposition"); !strings.Contains(
				disposition,
				`inline; filename="invite.ics"`,
			) {
				t.Fatalf("calendar Content-Disposition = %q", disposition)
			}
		}
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("MIME has more than three parts: %v", err)
	}
	if calendarParts != 1 {
		t.Fatalf("calendar part count = %d, want 1", calendarParts)
	}

	unfolded := unfoldICalendar(string(rendered.ICalendar.Bytes))
	assertCalendarLine(t, unfolded, "METHOD:"+string(rendered.Method))
	if rendered.Method != MethodCancel {
		t.Fatalf("rendered method = %q, want CANCEL", rendered.Method)
	}
}

func TestRenderCanonicalEmailRejectsEnvelopeHeaderInjection(t *testing.T) {
	t.Parallel()

	snapshot := deterministicSnapshot(t, EffectInvite, 7)
	tests := []struct {
		name     string
		envelope MailEnvelope
	}{
		{
			name: "from name",
			envelope: MailEnvelope{
				FromEmail: "calendar@example.edu",
				FromName:  "TutorHub\r\nBcc: attacker@example.com",
			},
		},
		{
			name: "from email",
			envelope: MailEnvelope{
				FromEmail: "calendar@example.edu\r\nBcc:attacker@example.com",
				FromName:  "TutorHub",
			},
		},
		{
			name: "reply-to",
			envelope: MailEnvelope{
				FromEmail: "calendar@example.edu",
				FromName:  "TutorHub",
				ReplyTo:   "reply@example.edu\r\nBcc:attacker@example.com",
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := RenderCanonicalEmail(
				snapshot,
				test.envelope,
				deterministicRenderPolicy(t),
			); err == nil {
				t.Fatal("RenderCanonicalEmail() error = nil, want validation failure")
			}
		})
	}
}

func TestCanonicalMIMEUsesCRLFAndWrappedBase64(t *testing.T) {
	t.Parallel()

	rendered, err := RenderCanonicalEmail(
		deterministicSnapshot(t, EffectInvite, 7),
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail(): %v", err)
	}

	withoutCRLF := bytes.ReplaceAll(rendered.Bytes, []byte("\r\n"), nil)
	if bytes.ContainsAny(withoutCRLF, "\r\n") {
		t.Fatal("canonical MIME contains a lone CR or LF")
	}
	inBody := false
	for index, line := range bytes.Split(rendered.Bytes, []byte("\r\n")) {
		if bytes.Equal(line, []byte("Content-Transfer-Encoding: base64")) {
			inBody = false
			continue
		}
		if len(line) == 0 {
			inBody = true
			continue
		}
		if bytes.HasPrefix(line, []byte("--")) {
			inBody = false
			continue
		}
		if inBody && len(line) > 76 {
			t.Fatalf("base64 line %d has %d characters, want <= 76", index+1, len(line))
		}
	}
}

func TestCanonicalEmailUsesRecipientLocaleViewerZoneAndResponsePolicy(t *testing.T) {
	t.Parallel()

	english := deterministicSnapshot(t, EffectInvite, 7)
	english.Recipient.Locale = LocaleEnglish
	english.Recipient.ViewerTimeZone = "Asia/Ho_Chi_Minh"
	english.Recipient.ResponseRequested = true
	rendered, err := RenderCanonicalEmail(
		english,
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail() English: %v", err)
	}
	subject := decodedSubject(t, rendered.Bytes)
	if !strings.HasPrefix(subject, "Invitation: ") {
		t.Fatalf("English subject = %q", subject)
	}
	plain := string(decodedMIMEPart(t, rendered.Bytes, "text/plain"))
	if !strings.Contains(plain, "Time: 2026-03-10 20:00 +07") {
		t.Fatalf("plain body did not use recipient viewer time zone:\n%s", plain)
	}
	if !strings.Contains(
		plain,
		"Respond in TutorHub; email-client RSVP is not synchronized.",
	) {
		t.Fatalf("response-requested guidance is missing:\n%s", plain)
	}

	noResponse := english
	noResponse.Sequence++
	noResponse.Recipient.ResponseRequested = false
	noResponseRendered, err := RenderCanonicalEmail(
		noResponse,
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail() no response: %v", err)
	}
	noResponsePlain := string(decodedMIMEPart(t, noResponseRendered.Bytes, "text/plain"))
	if strings.Contains(noResponsePlain, "Respond in TutorHub") {
		t.Fatalf("non-response invitation contains RSVP guidance:\n%s", noResponsePlain)
	}

	vietnamese := english
	vietnamese.Sequence += 2
	vietnamese.Recipient.Locale = LocaleVietnamese
	vietnamese.Recipient.ViewerTimeZone = "America/New_York"
	vietnameseRendered, err := RenderCanonicalEmail(
		vietnamese,
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail() Vietnamese: %v", err)
	}
	vietnameseSubject := decodedSubject(t, vietnameseRendered.Bytes)
	if !strings.HasPrefix(vietnameseSubject, "Lời mời: ") {
		t.Fatalf("Vietnamese subject = %q", vietnameseSubject)
	}
	vietnamesePlain := string(
		decodedMIMEPart(t, vietnameseRendered.Bytes, "text/plain"),
	)
	if !strings.Contains(vietnamesePlain, "Thời gian: 2026-03-10 09:00 EDT") {
		t.Fatalf("Vietnamese body did not use viewer zone/locale:\n%s", vietnamesePlain)
	}
}

func TestCanonicalMIMEPhysicalLinesRespectRFC5322HardLimit(t *testing.T) {
	t.Parallel()

	snapshot := deterministicSnapshot(t, EffectInvite, 7)
	snapshot.Title = strings.Repeat("Lịch-học-rất-dài-", 20)
	snapshot.Recipient.DisplayName = strings.Repeat("Nguyễn-Bình-", 15)
	envelope := deterministicEnvelope()
	envelope.FromName = strings.Repeat("TutorHub-Calendar-", 12)

	rendered, err := RenderCanonicalEmail(
		snapshot,
		envelope,
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail(): %v", err)
	}
	for index, line := range bytes.Split(rendered.Bytes, []byte("\r\n")) {
		if len(line) > 998 {
			t.Fatalf(
				"MIME physical line %d has %d octets, RFC 5322 hard limit is 998",
				index+1,
				len(line),
			)
		}
	}
}

func decodedSubject(t *testing.T, rawMIME []byte) string {
	t.Helper()
	message, err := mail.ReadMessage(bytes.NewReader(rawMIME))
	if err != nil {
		t.Fatalf("parse MIME message: %v", err)
	}
	decoded, err := (&mime.WordDecoder{}).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode Subject: %v", err)
	}
	return decoded
}

func decodedMIMEPart(t *testing.T, rawMIME []byte, wantMediaType string) []byte {
	t.Helper()
	message, err := mail.ReadMessage(bytes.NewReader(rawMIME))
	if err != nil {
		t.Fatalf("parse MIME message: %v", err)
	}
	mediaType, parameters, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse root Content-Type: %v", err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("root media type = %q", mediaType)
	}
	reader := multipart.NewReader(message.Body, parameters["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			t.Fatalf("MIME part %q was not found", wantMediaType)
		}
		if err != nil {
			t.Fatalf("read MIME part: %v", err)
		}
		partMediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse MIME part Content-Type: %v", err)
		}
		if partMediaType != wantMediaType {
			continue
		}
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatalf("decode MIME part %q: %v", wantMediaType, err)
		}
		return decoded
	}
}
