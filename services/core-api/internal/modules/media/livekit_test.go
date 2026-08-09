package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/utils/protojson"
)

func TestLiveKitTokenIssuerSignsExplicitLeastPrivilegeGrant(t *testing.T) {
	t.Parallel()

	issuer, err := NewLiveKitTokenIssuer("test-api-key", "test-api-secret-long-enough")
	if err != nil {
		t.Fatalf("create issuer: %v", err)
	}
	token, err := issuer.Issue(TokenGrant{
		RoomName: "room", ParticipantIdentity: "participant", ParticipantName: "Teacher",
		Role: "co_teacher", OrganizationRole: "teacher", ClassRole: "co_teacher",
		CanPublish: true, CanPublishData: false, CanSubscribe: true,
		ValidFor: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	verifier, err := auth.ParseAPIToken(token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	registered, claims, err := verifier.Verify("test-api-secret-long-enough")
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if verifier.APIKey() != "test-api-key" || verifier.Identity() != "participant" ||
		claims.Name != "Teacher" || claims.Attributes["tutorhub.role"] != "co_teacher" ||
		claims.Attributes["tutorhub.organization_role"] != "teacher" ||
		claims.Attributes["tutorhub.class_role"] != "co_teacher" {
		t.Fatalf("unexpected token identity claims: %+v", claims)
	}
	if registered.ExpiresAt == nil || registered.IssuedAt == nil ||
		registered.ExpiresAt.Sub(registered.IssuedAt.Time) != 5*time.Minute {
		t.Fatalf("unexpected token lifetime: %+v", registered)
	}
	grant := claims.Video
	if grant == nil || !grant.RoomJoin || grant.Room != "room" ||
		!grant.GetCanPublish() || !grant.GetCanSubscribe() || grant.GetCanPublishData() ||
		grant.GetCanUpdateOwnMetadata() {
		t.Fatalf("unexpected video grant: %+v", grant)
	}
	for _, source := range []livekit.TrackSource{
		livekit.TrackSource_CAMERA,
		livekit.TrackSource_MICROPHONE,
		livekit.TrackSource_SCREEN_SHARE,
		livekit.TrackSource_SCREEN_SHARE_AUDIO,
	} {
		if !grant.GetCanPublishSource(source) {
			t.Fatalf("expected source %s to be publishable", source)
		}
	}
}

func TestLiveKitTokenIssuerSeparatesCameraMicrophoneAndScreenShare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		cameraMicrophone bool
		screenShare      bool
		wantCamera       bool
		wantScreen       bool
	}{
		{
			name: "camera and microphone only", cameraMicrophone: true,
			wantCamera: true,
		},
		{
			name: "screen share only", screenShare: true,
			wantScreen: true,
		},
		{name: "subscribe only"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			issuer, err := NewLiveKitTokenIssuer("test-api-key", "test-api-secret-long-enough")
			if err != nil {
				t.Fatalf("create issuer: %v", err)
			}
			token, err := issuer.Issue(TokenGrant{
				RoomName: "room", ParticipantIdentity: "participant",
				CanPublishCameraMicrophone: test.cameraMicrophone,
				CanShareScreen:             test.screenShare,
				CanSubscribe:               true,
				ValidFor:                   5 * time.Minute,
			})
			if err != nil {
				t.Fatalf("issue token: %v", err)
			}
			parsed, err := auth.ParseAPIToken(token)
			if err != nil {
				t.Fatalf("parse token: %v", err)
			}
			_, claims, err := parsed.Verify("test-api-secret-long-enough")
			if err != nil {
				t.Fatalf("verify token: %v", err)
			}
			grant := claims.Video
			if grant == nil || grant.GetCanPublish() != (test.wantCamera || test.wantScreen) {
				t.Fatalf("unexpected publish grant: %+v", grant)
			}
			if grant.GetCanPublishSource(livekit.TrackSource_CAMERA) != test.wantCamera ||
				grant.GetCanPublishSource(livekit.TrackSource_MICROPHONE) != test.wantCamera ||
				grant.GetCanPublishSource(livekit.TrackSource_SCREEN_SHARE) != test.wantScreen ||
				grant.GetCanPublishSource(livekit.TrackSource_SCREEN_SHARE_AUDIO) != test.wantScreen {
				t.Fatalf("unexpected publish sources: %+v", grant.GetCanPublishSources())
			}
		})
	}
}

func TestLiveKitWebhookVerifierExtractsProviderSIDs(t *testing.T) {
	t.Parallel()

	const (
		apiKey           = "test-api-key"
		apiSecret        = "test-api-secret-long-enough"
		roomName         = "r_0123456789abcdef0123456789abcdef"
		roomSID          = "RM_provider_room_sid"
		participantSID   = "PA_provider_participant_sid"
		participantID    = "p_0123456789abcdef0123456789abcdef"
		webhookEventID   = "EV_provider_binding_1"
		webhookCreatedAt = int64(1_800_000_000)
	)
	event := &livekit.WebhookEvent{
		Id: webhookEventID, Event: "participant_joined", CreatedAt: webhookCreatedAt,
		Room:        &livekit.Room{Sid: roomSID, Name: roomName},
		Participant: &livekit.ParticipantInfo{Sid: participantSID, Identity: participantID},
	}
	request := signedLiveKitWebhookRequest(t, apiKey, apiSecret, event)
	verifier, err := NewLiveKitWebhookVerifier(apiKey, apiSecret)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}

	received, err := verifier.Receive(request)
	if err != nil {
		t.Fatalf("receive webhook: %v", err)
	}
	if received.ID != webhookEventID || received.RoomName != roomName ||
		received.RoomSID != roomSID || received.ParticipantIdentity != participantID ||
		received.ParticipantSID != participantSID ||
		!received.OccurredAt.Equal(time.Unix(webhookCreatedAt, 0).UTC()) {
		t.Fatalf("unexpected webhook binding: %+v", received)
	}
}

func signedLiveKitWebhookRequest(
	t *testing.T,
	apiKey string,
	apiSecret string,
	event *livekit.WebhookEvent,
) *http.Request {
	t.Helper()
	body, err := protojson.Marshal(event)
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}
	digest := sha256.Sum256(body)
	token, err := auth.NewAccessToken(apiKey, apiSecret).
		SetSha256(base64.StdEncoding.EncodeToString(digest[:])).
		ToJWT()
	if err != nil {
		t.Fatalf("sign webhook: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/webhooks/livekit", bytes.NewReader(body))
	request.Header.Set("Authorization", token)
	request.Header.Set("Content-Type", "application/webhook+json")
	return request
}
