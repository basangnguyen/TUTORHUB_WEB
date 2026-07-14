package media

import (
	"fmt"
	"net/http"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
)

type LiveKitTokenIssuer struct {
	apiKey    string
	apiSecret string
}

func NewLiveKitTokenIssuer(apiKey string, apiSecret string) (*LiveKitTokenIssuer, error) {
	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("LiveKit API key and secret are required")
	}

	return &LiveKitTokenIssuer{apiKey: apiKey, apiSecret: apiSecret}, nil
}

func (issuer *LiveKitTokenIssuer) Issue(grant TokenGrant) (string, error) {
	videoGrant := &auth.VideoGrant{RoomJoin: true, Room: grant.RoomName}
	videoGrant.SetCanPublish(grant.CanPublish)
	videoGrant.SetCanSubscribe(grant.CanSubscribe)
	videoGrant.SetCanPublishData(grant.CanPublishData)
	videoGrant.SetCanUpdateOwnMetadata(false)
	if grant.CanPublish {
		videoGrant.SetCanPublishSources([]livekit.TrackSource{
			livekit.TrackSource_CAMERA,
			livekit.TrackSource_MICROPHONE,
			livekit.TrackSource_SCREEN_SHARE,
			livekit.TrackSource_SCREEN_SHARE_AUDIO,
		})
	}

	accessToken := auth.NewAccessToken(issuer.apiKey, issuer.apiSecret).
		SetIdentity(grant.ParticipantIdentity).
		SetName(grant.ParticipantName).
		SetAttributes(map[string]string{"tutorhub.role": grant.Role}).
		SetVideoGrant(videoGrant).
		SetValidFor(grant.ValidFor)
	token, err := accessToken.ToJWT()
	if err != nil {
		return "", fmt.Errorf("sign LiveKit access token: %w", err)
	}

	return token, nil
}

type WebhookVerifier interface {
	Receive(*http.Request) (WebhookEvent, error)
}

type LiveKitWebhookVerifier struct {
	keys auth.KeyProvider
}

func NewLiveKitWebhookVerifier(apiKey string, apiSecret string) (*LiveKitWebhookVerifier, error) {
	if apiKey == "" || apiSecret == "" {
		return nil, fmt.Errorf("LiveKit API key and secret are required")
	}

	return &LiveKitWebhookVerifier{keys: auth.NewSimpleKeyProvider(apiKey, apiSecret)}, nil
}

func (verifier *LiveKitWebhookVerifier) Receive(request *http.Request) (WebhookEvent, error) {
	event, err := webhook.ReceiveWebhookEvent(request, verifier.keys)
	if err != nil {
		return WebhookEvent{}, fmt.Errorf("verify LiveKit webhook: %w", err)
	}
	eventID := event.GetId()
	if !safeWebhookEventIDPattern.MatchString(eventID) {
		return WebhookEvent{}, ErrInvalidWebhook
	}
	roomName := ""
	if event.GetRoom() != nil {
		roomName = event.GetRoom().GetName()
	}
	participantIdentity := ""
	if event.GetParticipant() != nil {
		participantIdentity = event.GetParticipant().GetIdentity()
	}

	return WebhookEvent{
		ID:                  eventID,
		EventType:           event.GetEvent(),
		RoomName:            roomName,
		ParticipantIdentity: participantIdentity,
		OccurredAt:          time.Unix(event.GetCreatedAt(), 0).UTC(),
	}, nil
}
