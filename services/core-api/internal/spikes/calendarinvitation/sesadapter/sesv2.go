// Package sesadapter contains the isolated AWS SES v2 infrastructure adapter
// for the P3-CAL-02 spike. The parent calendarinvitation package has no AWS SDK
// dependency and remains the provider/domain boundary.
package sesadapter

import (
	"context"
	"errors"
	"net"
	"net/mail"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	"github.com/tutorhub-v2/core-api/internal/spikes/calendarinvitation"
)

// sesV2API is the narrow AWS SDK seam used by the isolated adapter.
type sesV2API interface {
	SendEmail(
		context.Context,
		*sesv2.SendEmailInput,
		...func(*sesv2.Options),
	) (*sesv2.SendEmailOutput, error)
}

// Provider submits persisted canonical MIME through SES v2 Content.Raw.
// Constructing this adapter does not load credentials and does not wire it to
// any runtime worker.
type Provider struct {
	client           sesV2API
	region           string
	fromEmail        string
	configurationSet string
}

// SubmissionError is safe to surface to bounded logs. It intentionally omits
// the provider's free-form message because that text can include an address.
type SubmissionError struct {
	State calendarinvitation.SubmissionState
	Code  string
}

func (err *SubmissionError) Error() string {
	return "calendar invitation SES v2 submission failed: " + err.Code
}

// New requires an already configured, region-pinned SDK client and pins the
// verified sender plus tracking-off configuration set at the adapter trust
// boundary. Business/outbox data cannot select either value.
func New(
	client sesV2API,
	region string,
	fromEmail string,
	configurationSet string,
) (*Provider, error) {
	if client == nil {
		return nil, errors.New("calendar invitation SES v2: client is required")
	}
	if !validOpaqueValue(region, 64) {
		return nil, errors.New("calendar invitation SES v2: invalid region")
	}
	if !validBareEmail(fromEmail) {
		return nil, errors.New("calendar invitation SES v2: invalid pinned sender")
	}
	if !validOpaqueValue(configurationSet, 64) {
		return nil, errors.New("calendar invitation SES v2: invalid pinned configuration set")
	}
	return &Provider{
		client:           client,
		region:           region,
		fromEmail:        fromEmail,
		configurationSet: configurationSet,
	}, nil
}

// Region returns the explicitly pinned SES region for diagnostics that do not
// expose credentials or recipient data.
func (provider *Provider) Region() string {
	if provider == nil {
		return ""
	}
	return provider.region
}

// Send maps one recipient and the exact Raw MIME bytes to SES v2 SendEmail.
func (provider *Provider) Send(
	ctx context.Context,
	request calendarinvitation.SendRequest,
) (calendarinvitation.SendResult, error) {
	if provider == nil || provider.client == nil {
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionRejectedPermanent,
		}, errors.New("calendar invitation SES v2: provider is not configured")
	}
	if ctx == nil {
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionRejectedPermanent,
		}, errors.New("calendar invitation SES v2: nil context")
	}
	if err := ctx.Err(); err != nil {
		// No provider call has started, so the application may retry safely.
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionRejectedRetryable,
		}, err
	}
	if err := request.Validate(); err != nil {
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionRejectedPermanent,
		}, err
	}
	if request.FromEmail != provider.fromEmail {
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionRejectedPermanent,
		}, errors.New("calendar invitation SES v2: sender does not match pinned configuration")
	}
	if request.ConfigurationSet != provider.configurationSet {
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionRejectedPermanent,
		}, errors.New("calendar invitation SES v2: configuration set does not match pinned configuration")
	}

	input := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(request.FromEmail),
		Destination: &sestypes.Destination{
			ToAddresses: []string{request.RecipientEmail},
		},
		Content: &sestypes.EmailContent{
			Raw: &sestypes.RawMessage{
				Data: append([]byte(nil), request.RawMIME...),
			},
		},
		ConfigurationSetName: aws.String(request.ConfigurationSet),
		EmailTags:            orderedSESTags(request),
	}
	output, err := provider.client.SendEmail(ctx, input, func(options *sesv2.Options) {
		// SendEmail has no caller idempotency token. Automatic SDK retries can
		// duplicate a message after an ambiguous transport failure, so the
		// application effect ledger owns every retry decision.
		options.Retryer = aws.NopRetryer{}
		options.RetryMaxAttempts = 1
	})
	if err != nil {
		state, code := classifyFailure(ctx, err)
		return calendarinvitation.SendResult{State: state}, &SubmissionError{
			State: state,
			Code:  code,
		}
	}
	if output == nil || output.MessageId == nil || *output.MessageId == "" {
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionOutcomeUnknown,
		}, errors.New("calendar invitation SES v2: accepted response did not include message id")
	}
	if !validOpaqueValue(*output.MessageId, 256) {
		return calendarinvitation.SendResult{
			State: calendarinvitation.SubmissionOutcomeUnknown,
		}, errors.New("calendar invitation SES v2: invalid message id")
	}
	return calendarinvitation.SendResult{
		State:             calendarinvitation.SubmissionAccepted,
		ProviderMessageID: *output.MessageId,
	}, nil
}

func orderedSESTags(
	request calendarinvitation.SendRequest,
) []sestypes.MessageTag {
	tags := make(map[string]string, len(request.Tags)+1)
	for key, value := range request.Tags {
		tags[key] = value
	}
	effectKey, _ := request.EffectIdentity.Key()
	tags["tutorhub_effect"] = effectKey
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]sestypes.MessageTag, 0, len(keys))
	for _, key := range keys {
		result = append(result, sestypes.MessageTag{
			Name:  aws.String(key),
			Value: aws.String(tags[key]),
		})
	}
	return result
}

func classifyFailure(
	ctx context.Context,
	err error,
) (calendarinvitation.SubmissionState, string) {
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		errors.Is(ctx.Err(), context.Canceled) {
		return calendarinvitation.SubmissionOutcomeUnknown, "timeout"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return calendarinvitation.SubmissionOutcomeUnknown, "network_timeout"
	}

	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "MessageRejected",
			"MailFromDomainNotVerifiedException",
			"NotFoundException",
			"BadRequestException",
			"AccountSuspendedException",
			"SendingPausedException":
			return calendarinvitation.SubmissionRejectedPermanent, apiError.ErrorCode()
		case "TooManyRequestsException",
			"LimitExceededException",
			"InternalServiceError",
			"ServiceUnavailableException":
			return calendarinvitation.SubmissionRejectedRetryable, apiError.ErrorCode()
		}
		return calendarinvitation.SubmissionRejectedRetryable,
			sanitizeProviderCode(apiError.ErrorCode())
	}
	// A non-API transport failure does not prove the service rejected the
	// request. Preserve it for grace/reconcile instead of blind retry.
	return calendarinvitation.SubmissionOutcomeUnknown, "transport_outcome_unknown"
}

func sanitizeProviderCode(value string) string {
	if !validOpaqueValue(value, 64) {
		return "provider_error"
	}
	return value
}

func validOpaqueValue(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune(
			"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.",
			character,
		) {
			return false
		}
	}
	return true
}

func validBareEmail(value string) bool {
	if value == "" || len(value) > 320 || !isASCII(value) ||
		strings.ContainsAny(value, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && address.Name == ""
}

func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > 0x7f {
			return false
		}
	}
	return true
}
