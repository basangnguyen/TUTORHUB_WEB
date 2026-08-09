//go:build integration

package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	protologger "github.com/livekit/protocol/logger"
	"github.com/livekit/protocol/utils/protojson"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

const (
	p402ProviderSmokeConfirmation = "I_UNDERSTAND_P4_02_TEST_PROVIDER_RESOURCE"
	p402ProviderSmokeTimeout      = 60 * time.Second
)

func TestLiveKitProviderDisposableSmoke(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_02_DISPOSABLE_CONFIRM")) != p402DisposableConfirmation {
		t.Skip("P4_02_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_02_PROVIDER_SMOKE_CONFIRM")) !=
		p402ProviderSmokeConfirmation {
		t.Skip("P4_02_PROVIDER_SMOKE_CONFIRM is not set to the provider-test confirmation")
	}

	serverURL := requireP402ProviderSmokeEnvironment(t, "LIVEKIT_URL")
	apiKey := requireP402ProviderSmokeEnvironment(t, "LIVEKIT_API_KEY")
	apiSecret := requireP402ProviderSmokeEnvironment(t, "LIVEKIT_API_SECRET")
	if !validP402ProviderSmokeURL(serverURL) {
		t.Fatal("LiveKit provider smoke URL is invalid")
	}

	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize LiveKit provider smoke")
	}
	issuer, err := NewLiveKitTokenIssuer(apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize LiveKit token issuer smoke")
	}
	verifier, err := NewLiveKitWebhookVerifier(apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize LiveKit webhook verifier smoke")
	}

	roomName := opaqueProviderRoomName(uuid.New())
	participantIdentity := opaqueParticipantIdentity(uuid.New())
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := provider.DeleteRoom(cleanupContext, roomName); err != nil {
			t.Error("cleanup LiveKit provider smoke room")
			return
		}
		if !waitForP402ProviderRoomCount(cleanupContext, provider, roomName, 0) {
			t.Error("verify LiveKit provider smoke cleanup")
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), p402ProviderSmokeTimeout)
	defer cancel()
	created, err := provider.EnsureRoom(ctx, roomName)
	if err != nil {
		t.Fatal("create LiveKit provider smoke room")
	}
	reused, err := provider.EnsureRoom(ctx, roomName)
	if err != nil {
		t.Fatal("reuse LiveKit provider smoke room")
	}
	if created.SID == "" || reused.SID != created.SID {
		t.Fatal("LiveKit provider smoke room was not reused")
	}
	if !waitForP402ProviderRoomCount(ctx, provider, roomName, 1) {
		t.Fatal("LiveKit provider smoke room count is not exact")
	}

	const tokenTTL = 5 * time.Minute
	token, err := issuer.Issue(TokenGrant{
		RoomName:            roomName,
		ParticipantIdentity: participantIdentity,
		ParticipantName:     "P4-02 provider smoke",
		Role:                "student",
		OrganizationRole:    "member",
		ClassRole:           "student",
		CanPublishData:      false,
		CanSubscribe:        true,
		ValidFor:            tokenTTL,
	})
	if err != nil {
		t.Fatal("issue LiveKit provider smoke token")
	}
	assertP402ProviderSmokeToken(t, token, apiKey, apiSecret, roomName, participantIdentity, tokenTTL)
	assertP402ProviderSmokeWebhookSignature(
		t,
		verifier,
		apiKey,
		apiSecret,
		roomName,
		created.SID,
		participantIdentity,
	)

	connectedRoom, err := lksdk.ConnectToRoomWithToken(
		serverURL,
		token,
		nil,
		lksdk.WithConnectTimeout(15*time.Second),
		lksdk.WithAutoSubscribe(false),
		lksdk.WithLogger(protologger.GetDiscardLogger()),
	)
	if err != nil {
		t.Fatal("join LiveKit provider smoke room")
	}
	t.Cleanup(connectedRoom.Disconnect)
	if connectedRoom.Name() != roomName || connectedRoom.SID() != created.SID {
		t.Fatal("LiveKit provider smoke token joined an unexpected room")
	}
	connectedRoom.Disconnect()

	if err := provider.DeleteRoom(ctx, roomName); err != nil {
		t.Fatal("delete LiveKit provider smoke room")
	}
	if !waitForP402ProviderRoomCount(ctx, provider, roomName, 0) {
		t.Fatal("LiveKit provider smoke room still exists after cleanup")
	}
	if err := provider.DeleteRoom(ctx, roomName); err != nil {
		t.Fatal("repeat LiveKit provider smoke cleanup was not idempotent")
	}
}

func requireP402ProviderSmokeEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Skipf("%s is not configured; skipping LiveKit provider smoke", key)
	}
	return value
}

func validP402ProviderSmokeURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "wss" && parsed.Host != "" && parsed.User == nil &&
		parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func waitForP402ProviderRoomCount(
	ctx context.Context,
	provider *LiveKitRoomProvider,
	roomName string,
	want int,
) bool {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		count, ok := p402ProviderRoomCount(ctx, provider, roomName)
		if ok && count == want {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func p402ProviderRoomCount(
	ctx context.Context,
	provider *LiveKitRoomProvider,
	roomName string,
) (int, bool) {
	if provider == nil || provider.rooms == nil {
		return 0, false
	}
	response, err := provider.rooms.ListRooms(ctx, &livekit.ListRoomsRequest{
		Names: []string{roomName},
	})
	if err != nil || response == nil {
		return 0, false
	}
	count := 0
	for _, room := range response.GetRooms() {
		if room.GetName() == roomName {
			count++
		}
	}
	return count, true
}

func assertP402ProviderSmokeToken(
	t *testing.T,
	token string,
	apiKey string,
	apiSecret string,
	roomName string,
	participantIdentity string,
	wantTTL time.Duration,
) {
	t.Helper()
	parsed, err := auth.ParseAPIToken(token)
	if err != nil {
		t.Fatal("parse LiveKit provider smoke token")
	}
	registered, claims, err := parsed.Verify(apiSecret)
	if err != nil {
		t.Fatal("verify LiveKit provider smoke token")
	}
	grant := claims.Video
	if parsed.APIKey() != apiKey || parsed.Identity() != participantIdentity ||
		registered.IssuedAt == nil || registered.ExpiresAt == nil ||
		registered.ExpiresAt.Sub(registered.IssuedAt.Time) != wantTTL ||
		grant == nil || !grant.RoomJoin || grant.Room != roomName || grant.GetCanPublish() ||
		!grant.GetCanSubscribe() || grant.GetCanPublishData() || grant.GetCanUpdateOwnMetadata() ||
		len(grant.GetCanPublishSources()) != 0 {
		t.Fatal("LiveKit provider smoke token grant is invalid")
	}
}

func assertP402ProviderSmokeWebhookSignature(
	t *testing.T,
	verifier *LiveKitWebhookVerifier,
	apiKey string,
	apiSecret string,
	roomName string,
	roomSID string,
	participantIdentity string,
) {
	t.Helper()
	event := &livekit.WebhookEvent{
		Id:        "EV_" + strings.ReplaceAll(uuid.New().String(), "-", ""),
		Event:     "participant_joined",
		CreatedAt: time.Now().UTC().Unix(),
		Room:      &livekit.Room{Name: roomName, Sid: roomSID},
		Participant: &livekit.ParticipantInfo{
			Identity: participantIdentity,
			Sid:      "PA_" + strings.ReplaceAll(uuid.New().String(), "-", ""),
		},
	}
	body, authorization, err := p402SignedWebhook(event, apiKey, apiSecret)
	if err != nil {
		t.Fatal("sign LiveKit provider smoke webhook")
	}
	received, err := verifier.Receive(p402WebhookRequest(body, authorization))
	if err != nil || received.ID != event.GetId() || received.RoomName != roomName ||
		received.RoomSID != roomSID || received.ParticipantIdentity != participantIdentity {
		t.Fatal("verify LiveKit provider smoke webhook")
	}

	wrongSecret := "p4-02-provider-smoke-invalid-secret"
	if wrongSecret == apiSecret {
		wrongSecret += "-alternate"
	}
	_, wrongAuthorization, err := p402SignedWebhook(event, apiKey, wrongSecret)
	if err != nil {
		t.Fatal("sign negative LiveKit provider smoke webhook")
	}
	if _, err := verifier.Receive(p402WebhookRequest(body, wrongAuthorization)); err == nil {
		t.Fatal("LiveKit provider smoke accepted a wrong-key webhook")
	}
	tamperedBody := append(append([]byte(nil), body...), ' ')
	if _, err := verifier.Receive(p402WebhookRequest(tamperedBody, authorization)); err == nil {
		t.Fatal("LiveKit provider smoke accepted a tampered webhook")
	}
}

func p402SignedWebhook(
	event *livekit.WebhookEvent,
	apiKey string,
	apiSecret string,
) ([]byte, string, error) {
	body, err := protojson.Marshal(event)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(body)
	authorization, err := auth.NewAccessToken(apiKey, apiSecret).
		SetSha256(base64.StdEncoding.EncodeToString(digest[:])).
		ToJWT()
	if err != nil {
		return nil, "", err
	}
	return body, authorization, nil
}

func p402WebhookRequest(body []byte, authorization string) *http.Request {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/webhooks/livekit",
		bytes.NewReader(body),
	)
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/webhook+json")
	return request
}
