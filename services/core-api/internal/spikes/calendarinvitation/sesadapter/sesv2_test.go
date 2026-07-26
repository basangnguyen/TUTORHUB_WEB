package sesadapter

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/smithy-go"
	"github.com/tutorhub-v2/core-api/internal/spikes/calendarinvitation"
)

type fakeSESV2Client struct {
	input   *sesv2.SendEmailInput
	output  *sesv2.SendEmailOutput
	err     error
	options sesv2.Options
	calls   int
}

func (client *fakeSESV2Client) SendEmail(
	_ context.Context,
	input *sesv2.SendEmailInput,
	optionFunctions ...func(*sesv2.Options),
) (*sesv2.SendEmailOutput, error) {
	client.calls++
	client.input = input
	for _, apply := range optionFunctions {
		apply(&client.options)
	}
	return client.output, client.err
}

func TestProviderUsesOnlyRawContentAndOneRecipient(t *testing.T) {
	t.Parallel()

	messageID := "01020195-test-message-id"
	client := &fakeSESV2Client{
		output: &sesv2.SendEmailOutput{MessageId: &messageID},
	}
	provider := newTestProvider(t, client)
	request := deterministicSESRequest(t)
	result, err := provider.Send(context.Background(), request)
	if err != nil {
		t.Fatalf("Provider.Send(): %v", err)
	}
	if result.State != calendarinvitation.SubmissionAccepted ||
		result.ProviderMessageID != messageID {
		t.Fatalf("send result = %#v, want accepted with provider message ID", result)
	}
	if result.State == calendarinvitation.SubmissionState("delivered") {
		t.Fatal("SES API acceptance was incorrectly reported as delivery")
	}

	input := client.input
	if input == nil {
		t.Fatal("fake SES client did not receive SendEmailInput")
	}
	if input.Content == nil || input.Content.Raw == nil {
		t.Fatal("SES input does not use Content.Raw")
	}
	if input.Content.Simple != nil || input.Content.Template != nil {
		t.Fatal("SES input unexpectedly uses Simple or Template content")
	}
	if !bytes.Equal(input.Content.Raw.Data, request.RawMIME) {
		t.Fatal("SES RawMessage differs from persisted canonical MIME")
	}
	if input.Destination == nil ||
		len(input.Destination.ToAddresses) != 1 ||
		input.Destination.ToAddresses[0] != request.RecipientEmail {
		t.Fatalf("SES destination is not one recipient: %#v", input.Destination)
	}
	if len(input.Destination.CcAddresses) != 0 ||
		len(input.Destination.BccAddresses) != 0 {
		t.Fatalf("SES destination unexpectedly has Cc/Bcc: %#v", input.Destination)
	}
	if input.FromEmailAddress == nil || *input.FromEmailAddress != request.FromEmail {
		t.Fatalf("SES FromEmailAddress = %#v, want %q", input.FromEmailAddress, request.FromEmail)
	}
	if input.ConfigurationSetName == nil ||
		*input.ConfigurationSetName != request.ConfigurationSet {
		t.Fatalf(
			"SES ConfigurationSetName = %#v, want %q",
			input.ConfigurationSetName,
			request.ConfigurationSet,
		)
	}

	if len(input.EmailTags) != 2 {
		t.Fatalf("SES EmailTags count = %d, want 2", len(input.EmailTags))
	}
	if got := *input.EmailTags[0].Name; got != "notification_type" {
		t.Fatalf("first SES tag = %q, want sorted notification_type", got)
	}
	if got := *input.EmailTags[0].Value; got != "calendar_invitation" {
		t.Fatalf("notification type tag = %q", got)
	}
	if got := *input.EmailTags[1].Name; got != "tutorhub_effect" {
		t.Fatalf("second SES tag = %q, want sorted tutorhub_effect", got)
	}
	if got := *input.EmailTags[1].Value; !strings.HasPrefix(got, "calfx_") {
		t.Fatalf("effect tag = %q, want opaque effect identity", got)
	}
	if client.options.Retryer == nil || client.options.RetryMaxAttempts != 1 {
		t.Fatalf(
			"SES adapter did not disable ambiguous SDK retries: retryer=%T attempts=%d",
			client.options.Retryer,
			client.options.RetryMaxAttempts,
		)
	}
	if _, ok := client.options.Retryer.(aws.NopRetryer); !ok {
		t.Fatalf("SES retryer = %T, want aws.NopRetryer", client.options.Retryer)
	}

	request.RawMIME[0] ^= 0xff
	if bytes.Equal(input.Content.Raw.Data, request.RawMIME) {
		t.Fatal("SES adapter retained caller-owned RawMIME storage")
	}
}

func TestProviderTimeoutIsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	client := &fakeSESV2Client{err: context.DeadlineExceeded}
	provider := newTestProvider(t, client)
	result, err := provider.Send(context.Background(), deterministicSESRequest(t))
	if err == nil {
		t.Fatal("Provider.Send() error = nil")
	}
	if result.State != calendarinvitation.SubmissionOutcomeUnknown {
		t.Fatalf("submission state = %q, want outcome_unknown", result.State)
	}
	var submissionError *SubmissionError
	if !errors.As(err, &submissionError) ||
		submissionError.State != calendarinvitation.SubmissionOutcomeUnknown ||
		submissionError.Code != "timeout" {
		t.Fatalf("submission error = %#v, want sanitized timeout", err)
	}
}

func TestProviderCanceledBeforeCallIsRetryableAndSkipsSES(t *testing.T) {
	t.Parallel()

	client := &fakeSESV2Client{}
	provider := newTestProvider(t, client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := provider.Send(ctx, deterministicSESRequest(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Provider.Send() error = %v, want context.Canceled", err)
	}
	if result.State != calendarinvitation.SubmissionRejectedRetryable {
		t.Fatalf("submission state = %q, want rejected_retryable", result.State)
	}
	if client.calls != 0 || client.input != nil {
		t.Fatalf("canceled preflight called SES %d times", client.calls)
	}
}

func TestProviderRejectsRequestThatDoesNotMatchPinnedConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		pinnedFrom   string
		pinnedConfig string
	}{
		{
			name:         "sender mismatch",
			pinnedFrom:   "other-sender@example.edu",
			pinnedConfig: "tutorhub-calendar-spike",
		},
		{
			name:         "configuration set mismatch",
			pinnedFrom:   "calendar@example.edu",
			pinnedConfig: "other-tracking-off-config",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &fakeSESV2Client{}
			provider, err := New(
				client,
				"ap-southeast-1",
				test.pinnedFrom,
				test.pinnedConfig,
			)
			if err != nil {
				t.Fatalf("New(): %v", err)
			}
			result, err := provider.Send(
				context.Background(),
				deterministicSESRequest(t),
			)
			if err == nil {
				t.Fatal("Provider.Send() error = nil")
			}
			if result.State != calendarinvitation.SubmissionRejectedPermanent {
				t.Fatalf("submission state = %q, want rejected_permanent", result.State)
			}
			if client.calls != 0 {
				t.Fatalf("pinned configuration mismatch called SES %d times", client.calls)
			}
		})
	}
}

func TestProviderClassifiesAPIErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want calendarinvitation.SubmissionState
	}{
		{
			name: "message rejected",
			code: "MessageRejected",
			want: calendarinvitation.SubmissionRejectedPermanent,
		},
		{
			name: "mail from not verified",
			code: "MailFromDomainNotVerifiedException",
			want: calendarinvitation.SubmissionRejectedPermanent,
		},
		{
			name: "bad request",
			code: "BadRequestException",
			want: calendarinvitation.SubmissionRejectedPermanent,
		},
		{
			name: "rate limited",
			code: "TooManyRequestsException",
			want: calendarinvitation.SubmissionRejectedRetryable,
		},
		{
			name: "internal service",
			code: "InternalServiceError",
			want: calendarinvitation.SubmissionRejectedRetryable,
		},
		{
			name: "unknown is retryable",
			code: "FutureTransientFailure",
			want: calendarinvitation.SubmissionRejectedRetryable,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &fakeSESV2Client{
				err: &fakeSmithyAPIError{
					code:    test.code,
					message: "redacted provider failure",
				},
			}
			provider := newTestProvider(t, client)
			result, err := provider.Send(context.Background(), deterministicSESRequest(t))
			if err == nil {
				t.Fatal("Provider.Send() error = nil")
			}
			if result.State != test.want {
				t.Fatalf("submission state = %q, want %q", result.State, test.want)
			}
			var submissionError *SubmissionError
			if !errors.As(err, &submissionError) ||
				submissionError.State != test.want ||
				submissionError.Code != test.code {
				t.Fatalf(
					"submission error = %#v, want state=%q code=%q",
					err,
					test.want,
					test.code,
				)
			}
			if strings.Contains(err.Error(), "redacted provider failure") {
				t.Fatal("provider free-form message leaked through SubmissionError")
			}
		})
	}
}

func TestProviderUnknownTransportFailureIsOutcomeUnknownAndSanitized(t *testing.T) {
	t.Parallel()

	client := &fakeSESV2Client{
		err: errors.New("connection reset while sending to private-user@example.edu"),
	}
	provider := newTestProvider(t, client)
	result, err := provider.Send(context.Background(), deterministicSESRequest(t))
	if err == nil {
		t.Fatal("Provider.Send() error = nil")
	}
	if result.State != calendarinvitation.SubmissionOutcomeUnknown {
		t.Fatalf("submission state = %q, want outcome_unknown", result.State)
	}
	if strings.Contains(err.Error(), "private-user@example.edu") {
		t.Fatal("transport error leaked recipient data")
	}
	var submissionError *SubmissionError
	if !errors.As(err, &submissionError) ||
		submissionError.Code != "transport_outcome_unknown" {
		t.Fatalf("submission error = %#v, want sanitized transport outcome", err)
	}
}

func TestProviderMissingOrInvalidMessageIDIsOutcomeUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message *string
	}{
		{name: "missing"},
		{name: "empty", message: stringPointer("")},
		{name: "control", message: stringPointer("message\r\ninjected")},
		{name: "non ascii", message: stringPointer("message-đ")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &fakeSESV2Client{
				output: &sesv2.SendEmailOutput{MessageId: test.message},
			}
			provider := newTestProvider(t, client)
			result, err := provider.Send(context.Background(), deterministicSESRequest(t))
			if err == nil {
				t.Fatal("Provider.Send() error = nil")
			}
			if result.State != calendarinvitation.SubmissionOutcomeUnknown {
				t.Fatalf("submission state = %q, want outcome_unknown", result.State)
			}
		})
	}
}

func deterministicSESRequest(t *testing.T) calendarinvitation.SendRequest {
	t.Helper()

	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("load test time zone: %v", err)
	}
	snapshot := calendarinvitation.Snapshot{
		InvitationID:        "invitation-01",
		UID:                 "urn:uuid:94e9ef6a-47d9-4c7c-a8f0-f2329f1ed76f",
		Sequence:            7,
		Effect:              calendarinvitation.EffectInvite,
		DTStamp:             time.Date(2026, time.February, 14, 8, 30, 0, 0, time.UTC),
		StartsAt:            time.Date(2026, time.March, 10, 9, 0, 0, 0, location),
		EndsAt:              time.Date(2026, time.March, 10, 10, 30, 0, 0, location),
		TimeZone:            "Asia/Ho_Chi_Minh",
		TimeZoneDataVersion: "go-tzdata-2026a",
		Title:               "Ôn tập Mật mã học",
		Description:         "Chuẩn bị chương 3.",
		Location:            "Phòng Lab 2",
		DeepLink:            "https://tutorhub.example/calendar/invitations/invitation-01",
		Organizer: calendarinvitation.Organizer{
			UserID:      "teacher-01",
			Email:       "teacher@example.edu",
			DisplayName: "Cô Nguyễn An",
		},
		Recipient: calendarinvitation.RecipientSnapshot{
			Attendee: calendarinvitation.Attendee{
				RecipientID:       "student-01",
				Email:             "student@example.edu",
				DisplayName:       "Nguyễn Bình",
				Role:              calendarinvitation.RoleRequired,
				Source:            calendarinvitation.AudienceSourceRoster,
				ResponseRequested: true,
				RSVP:              calendarinvitation.RSVPNeedsAction,
				RSVPSource:        calendarinvitation.RSVPSourceNone,
			},
			CTALabel:       "Mở lịch trong TutorHub",
			Locale:         calendarinvitation.LocaleVietnamese,
			ViewerTimeZone: "Asia/Ho_Chi_Minh",
		},
	}
	policy, err := calendarinvitation.NewRenderPolicy(
		"go-tzdata-2026a",
		"https://tutorhub.example",
	)
	if err != nil {
		t.Fatalf("NewRenderPolicy(): %v", err)
	}
	email, err := calendarinvitation.RenderCanonicalEmail(
		snapshot,
		calendarinvitation.MailEnvelope{
			FromEmail: "calendar@example.edu",
			FromName:  "TutorHub Calendar",
			ReplyTo:   "support@example.edu",
		},
		policy,
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail(): %v", err)
	}
	return calendarinvitation.SendRequest{
		EffectIdentity: calendarinvitation.EffectIdentity{
			InvitationID: snapshot.InvitationID,
			RecipientID:  snapshot.Recipient.RecipientID,
			Effect:       snapshot.Effect,
			Sequence:     snapshot.Sequence,
			Channel:      calendarinvitation.ChannelEmail,
		},
		FromEmail:        email.FromEmail,
		RecipientEmail:   email.RecipientEmail,
		RawMIME:          append([]byte(nil), email.Bytes...),
		CanonicalSHA256:  email.SHA256,
		ConfigurationSet: "tutorhub-calendar-spike",
		Tags: map[string]string{
			"notification_type": "calendar_invitation",
		},
	}
}

func stringPointer(value string) *string {
	return &value
}

func newTestProvider(t *testing.T, client *fakeSESV2Client) *Provider {
	t.Helper()
	provider, err := New(
		client,
		"ap-southeast-1",
		"calendar@example.edu",
		"tutorhub-calendar-spike",
	)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return provider
}

type fakeSmithyAPIError struct {
	code    string
	message string
}

func (err *fakeSmithyAPIError) Error() string {
	return err.code + ": " + err.message
}

func (err *fakeSmithyAPIError) ErrorCode() string {
	return err.code
}

func (err *fakeSmithyAPIError) ErrorMessage() string {
	return err.message
}

func (err *fakeSmithyAPIError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultClient
}
