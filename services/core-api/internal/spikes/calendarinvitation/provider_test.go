package calendarinvitation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func deterministicSendRequest(t *testing.T) SendRequest {
	t.Helper()

	email, err := RenderCanonicalEmail(
		deterministicSnapshot(t, EffectInvite, 7),
		deterministicEnvelope(),
		deterministicRenderPolicy(t),
	)
	if err != nil {
		t.Fatalf("RenderCanonicalEmail(): %v", err)
	}
	return SendRequest{
		EffectIdentity: EffectIdentity{
			InvitationID: testInvitationID,
			RecipientID:  "student-01",
			Effect:       EffectInvite,
			Sequence:     7,
			Channel:      ChannelEmail,
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

func TestEffectIdentityKeyIsStableOpaqueAndRevisionSpecific(t *testing.T) {
	t.Parallel()

	request := deterministicSendRequest(t)
	first, err := request.EffectIdentity.Key()
	if err != nil {
		t.Fatalf("EffectIdentity.Key(): %v", err)
	}
	second, err := request.EffectIdentity.Key()
	if err != nil {
		t.Fatalf("EffectIdentity.Key() second call: %v", err)
	}
	if first != second {
		t.Fatalf("stable effect identity changed: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, "calfx_") ||
		strings.Contains(first, request.EffectIdentity.InvitationID) ||
		strings.Contains(first, request.EffectIdentity.RecipientID) {
		t.Fatalf("effect key is not opaque: %q", first)
	}

	changed := request.EffectIdentity
	changed.Sequence++
	changedKey, err := changed.Key()
	if err != nil {
		t.Fatalf("changed EffectIdentity.Key(): %v", err)
	}
	if changedKey == first {
		t.Fatal("new sequence reused the prior effect key")
	}
}

func TestSinkProviderCapturesOneRecipientAndDefensiveCopies(t *testing.T) {
	t.Parallel()

	request := deterministicSendRequest(t)
	var sink SinkProvider
	result, err := sink.Send(context.Background(), request)
	if err != nil {
		t.Fatalf("SinkProvider.Send(): %v", err)
	}
	if result.State != SubmissionAccepted {
		t.Fatalf("submission state = %q, want accepted", result.State)
	}
	if result.State == SubmissionState("delivered") {
		t.Fatal("provider acceptance was incorrectly reported as delivery")
	}
	if result.ProviderMessageID == "" {
		t.Fatal("synthetic provider message ID is empty")
	}

	request.RawMIME[0] ^= 0xff
	request.Tags["notification_type"] = "mutated"
	captured := sink.Requests()
	if len(captured) != 1 {
		t.Fatalf("captured request count = %d, want 1", len(captured))
	}
	if captured[0].RecipientEmail != "student@example.edu" {
		t.Fatalf("captured recipient = %q, want one recipient", captured[0].RecipientEmail)
	}
	if bytes.Equal(captured[0].RawMIME, request.RawMIME) {
		t.Fatal("sink retained caller-owned RawMIME storage")
	}
	if captured[0].Tags["notification_type"] == "mutated" {
		t.Fatal("sink retained caller-owned tag map")
	}
}

func TestSendRequestRejectsChangedCanonicalPayload(t *testing.T) {
	t.Parallel()

	request := deterministicSendRequest(t)
	request.RawMIME = append(request.RawMIME, byte('x'))
	if err := request.Validate(); err == nil {
		t.Fatal("SendRequest.Validate() error = nil for changed canonical bytes")
	}

	request = deterministicSendRequest(t)
	request.CanonicalSHA256 = sha256.Sum256([]byte("different"))
	if err := request.Validate(); err == nil {
		t.Fatal("SendRequest.Validate() error = nil for mismatched canonical hash")
	}
}

func TestSendRequestAllowsOnlyBoundedNotificationTypeTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tags map[string]string
	}{
		{
			name: "missing notification type",
			tags: map[string]string{},
		},
		{
			name: "reserved effect tag",
			tags: map[string]string{"tutorhub_effect": "forged"},
		},
		{
			name: "tenant identifier is not provider metadata",
			tags: map[string]string{"tenant": "tenant_01"},
		},
		{
			name: "invalid opaque value",
			tags: map[string]string{"notification_type": "calendar invitation"},
		},
		{
			name: "notification type does not match effect",
			tags: map[string]string{"notification_type": "calendar_update"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := deterministicSendRequest(t)
			request.Tags = test.tags
			if err := request.Validate(); err == nil {
				t.Fatal("SendRequest.Validate() error = nil for forbidden provider tags")
			}
		})
	}
}

func TestSinkProviderCanceledContextBeforeCaptureIsRetryable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (&SinkProvider{}).Send(ctx, deterministicSendRequest(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SinkProvider.Send() error = %v, want context.Canceled", err)
	}
	if result.State != SubmissionRejectedRetryable {
		t.Fatalf("submission state = %q, want rejected_retryable", result.State)
	}
}
