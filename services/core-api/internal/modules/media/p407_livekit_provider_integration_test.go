//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/livekit"
	protologger "github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

const (
	p407ProviderConfirmation = "I_UNDERSTAND_P4_07_TEST_PROVIDER_RESOURCE"
	p407ProviderTimeout      = 60 * time.Second
)

var p407ProviderConflictingEnvironment = []string{
	"DATABASE_MIGRATION_URL",
	"DATABASE_POOL_URL",
	"DATABASE_POLL_MAINTENANCE_URL",
	"P4_02_DISPOSABLE_CONFIRM",
	"P4_02_PROVIDER_SMOKE_CONFIRM",
	"P4_04_DISPOSABLE_CONFIRM",
	"P4_05_DISPOSABLE_CONFIRM",
	"P4_05_PROVIDER_CONFIRM",
	"P4_05_BROWSER_PROVIDER_CONFIRM",
	"P4_06_DISPOSABLE_CONFIRM",
	"P4_06_ACL_PROVISION_CONFIRM",
	"P4_07_DISPOSABLE_CONFIRM",
	"P4_07_ACL_PROVISION_CONFIRM",
	"P4_07_SHARED_CONFIRM",
	"P4_07_SHARED_OWNER_PREFLIGHT",
	"P4_07_SHARED_ACL_PROVISION_CONFIRM",
	"P4_07_SHARED_FINAL_SNAPSHOT_CONFIRM",
}

func TestLiveKitP407ModerationMatrix(t *testing.T) {
	confirmation := strings.TrimSpace(os.Getenv("P4_07_PROVIDER_CONFIRM"))
	if confirmation == "" {
		t.Skip("P4_07_PROVIDER_CONFIRM is not set; provider integration remains opt-in")
	}
	if confirmation != p407ProviderConfirmation {
		t.Fatal("P4_07_PROVIDER_CONFIRM does not match the exact provider-test confirmation")
	}
	rejectP407ProviderConflictingEnvironment(t)

	serverURL := requireP405ProviderEnvironment(t, "LIVEKIT_URL")
	apiKey := requireP405ProviderEnvironment(t, "LIVEKIT_API_KEY")
	apiSecret := requireP405ProviderEnvironment(t, "LIVEKIT_API_SECRET")
	if !validP402ProviderSmokeURL(serverURL) {
		t.Fatal("P4-07 LiveKit provider URL is not a secure WebSocket origin")
	}

	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-07 LiveKit provider")
	}
	issuer, err := NewLiveKitTokenIssuer(apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-07 LiveKit token issuer")
	}
	inspector := lksdk.NewRoomServiceClient(serverURL, apiKey, apiSecret)

	ctx, cancel := context.WithTimeout(context.Background(), p407ProviderTimeout)
	defer cancel()
	roomName := opaqueProviderRoomName(uuid.New())
	participantIdentity := opaqueParticipantIdentity(uuid.New())
	created, err := provider.EnsureRoom(ctx, roomName)
	if err != nil || created.SID == "" {
		t.Fatal("create isolated P4-07 LiveKit room")
	}
	run := &p405ProviderRun{provider: provider, roomName: roomName}
	t.Cleanup(func() { run.cleanup(t) })

	token, err := issuer.Issue(TokenGrant{
		RoomName:                   roomName,
		ParticipantIdentity:        participantIdentity,
		ParticipantName:            "P4-07 moderation target",
		Role:                       "student",
		OrganizationRole:           "member",
		ClassRole:                  "student",
		CanPublishCameraMicrophone: true,
		CanPublishData:             false,
		CanSubscribe:               false,
		ValidFor:                   5 * time.Minute,
	})
	if err != nil {
		t.Fatal("issue P4-07 participant token")
	}
	run.publisher, err = lksdk.ConnectToRoomWithToken(
		serverURL,
		token,
		lksdk.NewRoomCallback(),
		lksdk.WithConnectTimeout(15*time.Second),
		lksdk.WithAutoSubscribe(false),
		lksdk.WithLogger(protologger.GetDiscardLogger()),
	)
	if err != nil {
		t.Fatal("connect P4-07 moderation target")
	}

	microphone := newP405SyntheticTrack(t, p405MicrophoneTrack)
	run.tracks = append(run.tracks, microphone)
	if _, err := run.publisher.LocalParticipant.PublishTrack(
		microphone,
		&lksdk.TrackPublicationOptions{
			Name:   p405MicrophoneTrack.name,
			Source: livekit.TrackSource_MICROPHONE,
		},
	); err != nil {
		t.Fatal("publish P4-07 microphone track")
	}
	if !waitForP405ProviderParticipantCount(ctx, inspector, roomName, 1) ||
		!waitForP407MicrophoneState(ctx, inspector, roomName, participantIdentity, false) {
		t.Fatal("P4-07 provider did not expose the unmuted microphone target")
	}

	if err := provider.MuteParticipantMicrophone(ctx, roomName, participantIdentity); err != nil {
		t.Fatal("mute P4-07 participant microphone")
	}
	if !waitForP407MicrophoneState(ctx, inspector, roomName, participantIdentity, true) {
		t.Fatal("P4-07 microphone did not converge to muted")
	}
	if err := provider.MuteParticipantMicrophone(ctx, roomName, participantIdentity); err != nil {
		t.Fatal("repeat P4-07 mute was not idempotent")
	}

	if err := provider.RemoveParticipant(ctx, roomName, participantIdentity); err != nil {
		t.Fatal("remove P4-07 participant")
	}
	if !waitForP405ProviderParticipantCount(ctx, inspector, roomName, 0) {
		t.Fatal("P4-07 participant removal did not converge")
	}
	if err := provider.RemoveParticipant(ctx, roomName, participantIdentity); err != nil {
		t.Fatal("repeat P4-07 removal was not idempotent")
	}

	if err := provider.DeleteRoom(ctx, roomName); err != nil {
		t.Fatal("delete P4-07 provider room")
	}
	if !waitForP402ProviderRoomCount(ctx, provider, roomName, 0) {
		t.Fatal("P4-07 provider room did not return to zero")
	}
	if err := provider.DeleteRoom(ctx, roomName); err != nil {
		t.Fatal("repeat P4-07 room deletion was not idempotent")
	}
	run.cleanup(t)
}

func rejectP407ProviderConflictingEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range p407ProviderConflictingEnvironment {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatal("P4-07 provider gate refuses database, stale, shared, or provisioning confirmations")
		}
	}
}

func waitForP407MicrophoneState(
	ctx context.Context,
	inspector *lksdk.RoomServiceClient,
	roomName string,
	participantIdentity string,
	wantMuted bool,
) bool {
	return waitForP405ProviderCondition(ctx, func() bool {
		response, err := inspector.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: roomName})
		if err != nil || response == nil {
			return false
		}
		for _, participant := range response.GetParticipants() {
			if participant.GetIdentity() != participantIdentity {
				continue
			}
			for _, track := range participant.GetTracks() {
				if track.GetType() == livekit.TrackType_AUDIO &&
					track.GetSource() == livekit.TrackSource_MICROPHONE {
					return track.GetMuted() == wantMuted
				}
			}
		}
		return false
	})
}
