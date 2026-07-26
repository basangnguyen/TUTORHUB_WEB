package calendarinvitation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"sync"
)

// SubmissionState separates provider acceptance from downstream delivery.
type SubmissionState string

const (
	SubmissionAccepted          SubmissionState = "accepted"
	SubmissionOutcomeUnknown    SubmissionState = "outcome_unknown"
	SubmissionRejectedRetryable SubmissionState = "rejected_retryable"
	SubmissionRejectedPermanent SubmissionState = "rejected_permanent"
)

// Channel identifies the application effect channel.
type Channel string

const ChannelEmail Channel = "email"

// EffectIdentity is the application-owned dedupe identity. The provider may
// not offer a caller idempotency token, so this identity must be committed in
// the later P3-05A effect ledger before any provider call.
type EffectIdentity struct {
	InvitationID string
	RecipientID  string
	Effect       EffectKind
	Sequence     uint32
	Channel      Channel
}

// Key returns a deterministic, non-PII effect key.
func (identity EffectIdentity) Key() (string, error) {
	if err := validateBoundedSingleLine(
		"effect invitation_id",
		identity.InvitationID,
		1,
		maxInvitationIDBytes,
	); err != nil {
		return "", err
	}
	if err := validateBoundedSingleLine(
		"effect recipient_id",
		identity.RecipientID,
		1,
		maxAttendeeIDBytes,
	); err != nil {
		return "", err
	}
	switch identity.Effect {
	case EffectInvite, EffectUpdate, EffectCancel:
	default:
		return "", fmt.Errorf("%w: invalid effect identity", ErrInvalidSnapshot)
	}
	if identity.Channel != ChannelEmail {
		return "", fmt.Errorf("%w: unsupported channel %q", ErrInvalidSnapshot, identity.Channel)
	}
	payload := fmt.Sprintf(
		"%s\x00%s\x00%s\x00%d\x00%s",
		identity.InvitationID,
		identity.RecipientID,
		identity.Effect,
		identity.Sequence,
		identity.Channel,
	)
	sum := sha256.Sum256([]byte(payload))
	return "calfx_" + hex.EncodeToString(sum[:18]), nil
}

// SendRequest is the provider boundary. RawMIME must be the exact persisted
// canonical bytes; adapters may not rebuild it as Simple or Template content.
type SendRequest struct {
	EffectIdentity   EffectIdentity
	FromEmail        string
	RecipientEmail   string
	RawMIME          []byte
	CanonicalSHA256  [sha256.Size]byte
	ConfigurationSet string
	Tags             map[string]string
}

// Validate rejects a changed payload or multi-recipient/header-injection data
// before it reaches a provider SDK.
func (request SendRequest) Validate() error {
	if _, err := request.EffectIdentity.Key(); err != nil {
		return err
	}
	if err := validateEmail(request.FromEmail); err != nil {
		return err
	}
	if err := validateEmail(request.RecipientEmail); err != nil {
		return err
	}
	if len(request.RawMIME) == 0 || len(request.RawMIME) > maxCanonicalEmailBytes {
		return fmt.Errorf("%w: invalid raw MIME size", ErrInvalidSnapshot)
	}
	if sha256.Sum256(request.RawMIME) != request.CanonicalSHA256 {
		return fmt.Errorf("%w: canonical MIME hash mismatch", ErrInvalidSnapshot)
	}
	message, err := mail.ReadMessage(bytes.NewReader(request.RawMIME))
	if err != nil {
		return fmt.Errorf("%w: raw MIME cannot be parsed", ErrInvalidSnapshot)
	}
	if err := validateSingleMailboxHeader(
		message.Header,
		"From",
		request.FromEmail,
	); err != nil {
		return err
	}
	if err := validateSingleMailboxHeader(
		message.Header,
		"To",
		request.RecipientEmail,
	); err != nil {
		return err
	}
	if len(message.Header["Cc"]) != 0 || len(message.Header["Bcc"]) != 0 {
		return fmt.Errorf("%w: CC/BCC is forbidden", ErrInvalidSnapshot)
	}
	if err := validateBoundedSingleLine(
		"configuration_set",
		request.ConfigurationSet,
		1,
		64,
	); err != nil {
		return err
	}
	for key, value := range request.Tags {
		if key == "tutorhub_effect" {
			return fmt.Errorf("%w: reserved provider tag", ErrInvalidSnapshot)
		}
		if key != "notification_type" {
			return fmt.Errorf("%w: provider tag is not allowlisted", ErrInvalidSnapshot)
		}
		if !validOpaqueTag(key) || !validOpaqueTag(value) {
			return fmt.Errorf("%w: invalid provider tag", ErrInvalidSnapshot)
		}
		expected, err := notificationTypeForEffect(request.EffectIdentity.Effect)
		if err != nil || value != expected {
			return fmt.Errorf(
				"%w: notification_type does not match effect",
				ErrInvalidSnapshot,
			)
		}
	}
	if len(request.Tags) != 1 {
		return fmt.Errorf(
			"%w: exactly one notification_type tag is required",
			ErrInvalidSnapshot,
		)
	}
	return nil
}

func notificationTypeForEffect(effect EffectKind) (string, error) {
	switch effect {
	case EffectInvite:
		return "calendar_invitation", nil
	case EffectUpdate:
		return "calendar_update", nil
	case EffectCancel:
		return "calendar_cancellation", nil
	default:
		return "", fmt.Errorf("%w: unsupported notification effect", ErrInvalidSnapshot)
	}
}

func validateSingleMailboxHeader(header mail.Header, name string, expected string) error {
	values := header[name]
	if len(values) != 1 {
		return fmt.Errorf(
			"%w: raw MIME requires exactly one %s header",
			ErrInvalidSnapshot,
			name,
		)
	}
	addresses, err := mail.ParseAddressList(values[0])
	if err != nil || len(addresses) != 1 || addresses[0].Address != expected {
		return fmt.Errorf(
			"%w: raw MIME %s does not match the provider envelope",
			ErrInvalidSnapshot,
			name,
		)
	}
	return nil
}

// SendResult only means the provider accepted the API call. Delivery events
// must travel through the durable provider-event path selected by ADR-0020.
type SendResult struct {
	State             SubmissionState
	ProviderMessageID string
}

// Provider is implemented by the isolated sink and AWS SES v2 adapter.
type Provider interface {
	Send(context.Context, SendRequest) (SendResult, error)
}

// SinkProvider captures canonical requests for deterministic, non-networked
// spike tests. It never sends email.
type SinkProvider struct {
	mu       sync.Mutex
	requests []SendRequest
}

// Send records a defensive copy and returns a deterministic synthetic
// acceptance identifier.
func (provider *SinkProvider) Send(
	ctx context.Context,
	request SendRequest,
) (SendResult, error) {
	if ctx == nil {
		return SendResult{}, errors.New("calendar invitation sink: nil context")
	}
	if err := ctx.Err(); err != nil {
		return SendResult{State: SubmissionRejectedRetryable}, err
	}
	if err := request.Validate(); err != nil {
		return SendResult{State: SubmissionRejectedPermanent}, err
	}
	copy := cloneSendRequest(request)
	provider.mu.Lock()
	provider.requests = append(provider.requests, copy)
	provider.mu.Unlock()
	key, _ := request.EffectIdentity.Key()
	return SendResult{
		State:             SubmissionAccepted,
		ProviderMessageID: "sink-" + strings.TrimPrefix(key, "calfx_"),
	}, nil
}

// Requests returns defensive copies in submission order.
func (provider *SinkProvider) Requests() []SendRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	result := make([]SendRequest, len(provider.requests))
	for index, request := range provider.requests {
		result[index] = cloneSendRequest(request)
	}
	return result
}

func cloneSendRequest(request SendRequest) SendRequest {
	copy := request
	copy.RawMIME = append([]byte(nil), request.RawMIME...)
	copy.Tags = make(map[string]string, len(request.Tags))
	keys := make([]string, 0, len(request.Tags))
	for key := range request.Tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		copy.Tags[key] = request.Tags[key]
	}
	return copy
}

func validOpaqueTag(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune(
			"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-",
			character,
		) {
			return false
		}
	}
	return true
}
