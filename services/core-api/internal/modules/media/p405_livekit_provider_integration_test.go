//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	protologger "github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
)

const (
	p405ProviderConfirmation        = "I_UNDERSTAND_P4_05_TEST_PROVIDER_RESOURCE"
	p405BrowserProviderConfirmation = "I_UNDERSTAND_P4_05_BROWSER_PROVIDER_RESOURCE"
	p405ProviderTimeout             = 60 * time.Second
	p405ProviderQuietWindow         = 2 * time.Second
)

var p405ProviderConflictingEnvironment = []string{
	"DATABASE_MIGRATION_URL",
	"DATABASE_POOL_URL",
	"P4_02_DISPOSABLE_CONFIRM",
	"P4_02_OWNER_PREFLIGHT",
	"P4_02_ACL_PROVISION_CONFIRM",
	"P4_02_PROVIDER_SMOKE_CONFIRM",
	"P4_02_SHARED_CONFIRM",
	"P4_02_SHARED_ACL_PROVISION_CONFIRM",
	"P4_04_DISPOSABLE_CONFIRM",
	"P4_04_OWNER_PREFLIGHT",
	"P4_04_ACL_PROVISION_CONFIRM",
	"P4_04_SHARED_CONFIRM",
	"P4_04_SHARED_ACL_PROVISION_CONFIRM",
	"P4_04_SHARED_SNAPSHOT_CONFIRM",
	"P4_05_DISPOSABLE_CONFIRM",
	"P4_05_ACL_PROVISION_CONFIRM",
	"P4_05_SHARED_CONFIRM",
	"P4_05_SHARED_ACL_PROVISION_CONFIRM",
	"P4_05_SHARED_SNAPSHOT_CONFIRM",
}

type p405ProviderTrackSpec struct {
	codec   webrtc.RTPCodecCapability
	name    string
	source  livekit.TrackSource
	bitrate uint32
}

var (
	p405CameraTrack = p405ProviderTrackSpec{
		codec: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90_000,
		},
		name:    "p405-camera",
		source:  livekit.TrackSource_CAMERA,
		bitrate: 160_000,
	}
	p405MicrophoneTrack = p405ProviderTrackSpec{
		codec: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48_000,
			Channels:  2,
		},
		name:    "p405-microphone",
		source:  livekit.TrackSource_MICROPHONE,
		bitrate: 32_000,
	}
	p405ScreenTrack = p405ProviderTrackSpec{
		codec: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90_000,
		},
		name:    "p405-screen",
		source:  livekit.TrackSource_SCREEN_SHARE,
		bitrate: 160_000,
	}
)

func TestLiveKitP405TwoParticipantGrantMatrix(t *testing.T) {
	confirmation := strings.TrimSpace(os.Getenv("P4_05_PROVIDER_CONFIRM"))
	if confirmation == "" {
		t.Skip("P4_05_PROVIDER_CONFIRM is not set; provider integration remains opt-in")
	}
	if confirmation != p405ProviderConfirmation {
		t.Fatal("P4_05_PROVIDER_CONFIRM does not match the exact provider-test confirmation")
	}
	rejectP405ProviderConflictingEnvironment(t)
	rejectP405ProviderMatrixOnlyEnvironment(t)

	serverURL := requireP405ProviderEnvironment(t, "LIVEKIT_URL")
	apiKey := requireP405ProviderEnvironment(t, "LIVEKIT_API_KEY")
	apiSecret := requireP405ProviderEnvironment(t, "LIVEKIT_API_SECRET")
	if !validP402ProviderSmokeURL(serverURL) {
		t.Fatal("P4-05 LiveKit provider URL is not a secure WebSocket origin")
	}

	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-05 LiveKit room provider")
	}
	issuer, err := NewLiveKitTokenIssuer(apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-05 LiveKit token issuer")
	}
	inspector := lksdk.NewRoomServiceClient(serverURL, apiKey, apiSecret)

	matrices := []struct {
		name            string
		publisherGrant  TokenGrant
		allowedTracks   []p405ProviderTrackSpec
		deniedTracks    []p405ProviderTrackSpec
		wantCameraMic   bool
		wantScreenShare bool
	}{
		{
			name: "camera_microphone_publisher",
			publisherGrant: TokenGrant{
				Role:                       "teacher",
				OrganizationRole:           "member",
				ClassRole:                  "teacher",
				CanPublishCameraMicrophone: true,
				CanPublishData:             false,
				CanSubscribe:               false,
				ValidFor:                   5 * time.Minute,
			},
			allowedTracks:   []p405ProviderTrackSpec{p405CameraTrack, p405MicrophoneTrack},
			deniedTracks:    []p405ProviderTrackSpec{p405ScreenTrack},
			wantCameraMic:   true,
			wantScreenShare: false,
		},
		{
			name: "screen_share_publisher",
			publisherGrant: TokenGrant{
				Role:             "teacher",
				OrganizationRole: "member",
				ClassRole:        "teacher",
				CanShareScreen:   true,
				CanPublishData:   false,
				CanSubscribe:     false,
				ValidFor:         5 * time.Minute,
			},
			allowedTracks:   []p405ProviderTrackSpec{p405ScreenTrack},
			deniedTracks:    []p405ProviderTrackSpec{p405CameraTrack, p405MicrophoneTrack},
			wantCameraMic:   false,
			wantScreenShare: true,
		},
	}

	for _, matrix := range matrices {
		matrix := matrix
		t.Run(matrix.name, func(t *testing.T) {
			runP405ProviderMatrix(
				t,
				provider,
				inspector,
				issuer,
				serverURL,
				apiKey,
				apiSecret,
				matrix.publisherGrant,
				matrix.allowedTracks,
				matrix.deniedTracks,
				matrix.wantCameraMic,
				matrix.wantScreenShare,
			)
		})
	}
}

func TestLiveKitP405BrowserRoomCleanup(t *testing.T) {
	providerConfirmation := strings.TrimSpace(os.Getenv("P4_05_PROVIDER_CONFIRM"))
	if providerConfirmation == "" {
		t.Skip("P4_05_PROVIDER_CONFIRM is not set to the provider-test confirmation")
	}
	if providerConfirmation != p405ProviderConfirmation {
		t.Fatal("P4_05_PROVIDER_CONFIRM does not match the exact provider-test confirmation")
	}
	browserConfirmation := strings.TrimSpace(os.Getenv("P4_05_BROWSER_PROVIDER_CONFIRM"))
	if browserConfirmation == "" {
		t.Skip("P4_05_BROWSER_PROVIDER_CONFIRM is not set to the browser-provider confirmation")
	}
	if browserConfirmation != p405BrowserProviderConfirmation {
		t.Fatal("P4_05_BROWSER_PROVIDER_CONFIRM does not match the exact browser-provider confirmation")
	}
	rejectP405ProviderConflictingEnvironment(t)

	serverURL := requireP405ProviderEnvironment(t, "LIVEKIT_URL")
	apiKey := requireP405ProviderEnvironment(t, "LIVEKIT_API_KEY")
	apiSecret := requireP405ProviderEnvironment(t, "LIVEKIT_API_SECRET")
	roomName := requireP405ProviderEnvironment(t, "P4_05_PROVIDER_ROOM_NAME")
	if !validP402ProviderSmokeURL(serverURL) {
		t.Fatal("P4-05 LiveKit provider URL is not a secure WebSocket origin")
	}
	if !opaqueProviderRoomNamePattern.MatchString(roomName) {
		t.Fatal("P4-05 browser provider room name is not an exact opaque room name")
	}

	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-05 browser cleanup provider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := provider.DeleteRoom(ctx, roomName); err != nil {
		t.Fatal("delete exact P4-05 browser provider room")
	}
	if !waitForP402ProviderRoomCount(ctx, provider, roomName, 0) {
		t.Fatal("P4-05 browser provider room count did not return to zero")
	}
}

func rejectP405ProviderConflictingEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range p405ProviderConflictingEnvironment {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatal("P4-05 provider gate refuses database, stale, shared, or provisioning confirmations")
		}
	}
}

func rejectP405ProviderMatrixOnlyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"P4_05_BROWSER_PROVIDER_CONFIRM",
		"P4_05_PROVIDER_ROOM_NAME",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatal("P4-05 provider matrix refuses browser-cleanup state")
		}
	}
}

func requireP405ProviderEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("P4-05 provider gate requires %s after explicit opt-in", key)
	}
	return value
}

func runP405ProviderMatrix(
	t *testing.T,
	provider *LiveKitRoomProvider,
	inspector *lksdk.RoomServiceClient,
	issuer *LiveKitTokenIssuer,
	serverURL string,
	apiKey string,
	apiSecret string,
	publisherGrant TokenGrant,
	allowedTracks []p405ProviderTrackSpec,
	deniedTracks []p405ProviderTrackSpec,
	wantCameraMicrophone bool,
	wantScreenShare bool,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), p405ProviderTimeout)
	defer cancel()

	roomName := opaqueProviderRoomName(uuid.New())
	subscriberIdentity := opaqueParticipantIdentity(uuid.New())
	publisherIdentity := opaqueParticipantIdentity(uuid.New())

	created, err := provider.EnsureRoom(ctx, roomName)
	if err != nil || created.SID == "" {
		t.Fatal("create isolated P4-05 LiveKit room")
	}
	run := &p405ProviderRun{provider: provider, roomName: roomName}
	t.Cleanup(func() { run.cleanup(t) })

	subscriberToken, err := issuer.Issue(TokenGrant{
		RoomName:            roomName,
		ParticipantIdentity: subscriberIdentity,
		ParticipantName:     "P4-05 subscribe-only",
		Role:                "student",
		OrganizationRole:    "member",
		ClassRole:           "student",
		CanPublishData:      false,
		CanSubscribe:        true,
		ValidFor:            5 * time.Minute,
	})
	if err != nil {
		t.Fatal("issue P4-05 subscribe-only token")
	}
	assertP405ProviderTokenGrant(
		t,
		subscriberToken,
		apiKey,
		apiSecret,
		roomName,
		subscriberIdentity,
		true,
		false,
		false,
	)

	publisherGrant.RoomName = roomName
	publisherGrant.ParticipantIdentity = publisherIdentity
	publisherGrant.ParticipantName = "P4-05 publisher"
	publisherToken, err := issuer.Issue(publisherGrant)
	if err != nil {
		t.Fatal("issue P4-05 publisher token")
	}
	assertP405ProviderTokenGrant(
		t,
		publisherToken,
		apiKey,
		apiSecret,
		roomName,
		publisherIdentity,
		false,
		wantCameraMicrophone,
		wantScreenShare,
	)

	probe := newP405ProviderDeliveryProbe()
	subscriberCallback := lksdk.NewRoomCallback()
	subscriberCallback.OnTrackSubscribed = func(
		_ *webrtc.TrackRemote,
		publication *lksdk.RemoteTrackPublication,
		_ *lksdk.RemoteParticipant,
	) {
		probe.recordTrack(publication.Source())
	}
	subscriberCallback.OnDataPacket = func(lksdk.DataPacket, lksdk.DataReceiveParams) {
		probe.dataDelivered.Store(true)
	}

	run.subscriber, err = lksdk.ConnectToRoomWithToken(
		serverURL,
		subscriberToken,
		subscriberCallback,
		lksdk.WithConnectTimeout(15*time.Second),
		lksdk.WithAutoSubscribe(true),
		lksdk.WithLogger(protologger.GetDiscardLogger()),
	)
	if err != nil {
		t.Fatal("connect P4-05 subscribe-only participant")
	}
	run.publisher, err = lksdk.ConnectToRoomWithToken(
		serverURL,
		publisherToken,
		lksdk.NewRoomCallback(),
		lksdk.WithConnectTimeout(15*time.Second),
		lksdk.WithAutoSubscribe(false),
		lksdk.WithLogger(protologger.GetDiscardLogger()),
	)
	if err != nil {
		t.Fatal("connect P4-05 publishing participant")
	}

	if !waitForP405ProviderParticipantCount(ctx, inspector, roomName, 2) ||
		!waitForP405RemoteParticipantCount(ctx, run.subscriber, 1) ||
		!waitForP405RemoteParticipantCount(ctx, run.publisher, 1) {
		t.Fatal("P4-05 provider room did not converge to exactly two participants")
	}

	for _, spec := range allowedTracks {
		track := newP405SyntheticTrack(t, spec)
		run.tracks = append(run.tracks, track)
		if _, err := run.publisher.LocalParticipant.PublishTrack(
			track,
			&lksdk.TrackPublicationOptions{Name: spec.name, Source: spec.source},
		); err != nil {
			t.Fatal("P4-05 provider rejected an allowed synthetic track source")
		}
	}
	if !waitForP405DeliveredSources(ctx, probe, allowedTracks) ||
		!waitForP405ProviderSources(ctx, inspector, roomName, allowedTracks) {
		t.Fatal("P4-05 subscriber did not receive the exact allowed track sources")
	}

	_ = run.publisher.LocalParticipant.PublishDataPacket(
		lksdk.UserData([]byte("p405-denied-data")),
		lksdk.WithDataPublishReliable(true),
	)
	if !p405ProviderRemainsQuiet(probe.dataDelivered.Load) {
		t.Fatal("P4-05 provider delivered data despite CanPublishData=false")
	}

	for _, spec := range deniedTracks {
		track := newP405SyntheticTrack(t, spec)
		run.tracks = append(run.tracks, track)
		if _, err := run.publisher.LocalParticipant.PublishTrack(
			track,
			&lksdk.TrackPublicationOptions{Name: spec.name, Source: spec.source},
		); err == nil {
			t.Fatal("P4-05 provider accepted a denied synthetic track source")
		}
	}
	if !p405ProviderRemainsQuiet(func() bool {
		return probe.hasAnySource(deniedTracks)
	}) {
		t.Fatal("P4-05 subscriber received a denied track source")
	}
	if hasDenied, ok := p405ProviderHasAnySource(ctx, inspector, roomName, deniedTracks); !ok || hasDenied {
		t.Fatal("P4-05 provider retained a denied track source")
	}

	run.cleanup(t)
}

type p405ProviderRun struct {
	cleanupOnce sync.Once
	provider    *LiveKitRoomProvider
	roomName    string
	publisher   *lksdk.Room
	subscriber  *lksdk.Room
	tracks      []*lksdk.LocalTrack
}

func (run *p405ProviderRun) cleanup(t *testing.T) {
	t.Helper()
	run.cleanupOnce.Do(func() {
		for _, track := range run.tracks {
			if track != nil {
				if err := track.Close(); err != nil {
					t.Error("close P4-05 synthetic track")
				}
			}
		}
		if run.publisher != nil {
			run.publisher.Disconnect()
		}
		if run.subscriber != nil {
			run.subscriber.Disconnect()
		}

		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := run.provider.DeleteRoom(cleanupContext, run.roomName); err != nil {
			t.Error("delete exact P4-05 provider room during cleanup")
			return
		}
		if !waitForP402ProviderRoomCount(cleanupContext, run.provider, run.roomName, 0) {
			t.Error("P4-05 provider room count did not return to zero")
		}
	})
}

type p405ProviderDeliveryProbe struct {
	mu            sync.Mutex
	trackSources  map[livekit.TrackSource]int
	dataDelivered atomic.Bool
}

func newP405ProviderDeliveryProbe() *p405ProviderDeliveryProbe {
	return &p405ProviderDeliveryProbe{trackSources: make(map[livekit.TrackSource]int)}
}

func (probe *p405ProviderDeliveryProbe) recordTrack(source livekit.TrackSource) {
	probe.mu.Lock()
	probe.trackSources[source]++
	probe.mu.Unlock()
}

func (probe *p405ProviderDeliveryProbe) hasExactSources(specs []p405ProviderTrackSpec) bool {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.trackSources) != len(specs) {
		return false
	}
	for _, spec := range specs {
		if probe.trackSources[spec.source] != 1 {
			return false
		}
	}
	return true
}

func (probe *p405ProviderDeliveryProbe) hasAnySource(specs []p405ProviderTrackSpec) bool {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	for _, spec := range specs {
		if probe.trackSources[spec.source] > 0 {
			return true
		}
	}
	return false
}

func newP405SyntheticTrack(t *testing.T, spec p405ProviderTrackSpec) *lksdk.LocalTrack {
	t.Helper()
	track, err := lksdk.NewLocalTrack(spec.codec)
	if err != nil {
		t.Fatal("create P4-05 synthetic track")
	}
	track.SetLogger(protologger.GetDiscardLogger())
	if err := track.StartWrite(lksdk.NewNullSampleProvider(spec.bitrate), func() {}); err != nil {
		_ = track.Close()
		t.Fatal("start P4-05 synthetic track")
	}
	return track
}

func assertP405ProviderTokenGrant(
	t *testing.T,
	token string,
	apiKey string,
	apiSecret string,
	roomName string,
	participantIdentity string,
	wantSubscribe bool,
	wantCameraMicrophone bool,
	wantScreenShare bool,
) {
	t.Helper()
	parsed, err := auth.ParseAPIToken(token)
	if err != nil {
		t.Fatal("parse P4-05 provider token")
	}
	registered, claims, err := parsed.Verify(apiSecret)
	if err != nil {
		t.Fatal("verify P4-05 provider token")
	}
	grant := claims.Video
	wantPublish := wantCameraMicrophone || wantScreenShare
	wantSourceCount := 0
	if wantCameraMicrophone {
		wantSourceCount += 2
	}
	if wantScreenShare {
		wantSourceCount += 2
	}
	if parsed.APIKey() != apiKey || parsed.Identity() != participantIdentity ||
		registered.IssuedAt == nil || registered.ExpiresAt == nil ||
		registered.ExpiresAt.Sub(registered.IssuedAt.Time) != 5*time.Minute ||
		grant == nil || !grant.RoomJoin || grant.Room != roomName ||
		grant.GetCanSubscribe() != wantSubscribe || grant.GetCanPublish() != wantPublish ||
		grant.GetCanPublishData() || grant.GetCanUpdateOwnMetadata() ||
		len(grant.GetCanPublishSources()) != wantSourceCount ||
		grant.GetCanPublishSource(livekit.TrackSource_CAMERA) != wantCameraMicrophone ||
		grant.GetCanPublishSource(livekit.TrackSource_MICROPHONE) != wantCameraMicrophone ||
		grant.GetCanPublishSource(livekit.TrackSource_SCREEN_SHARE) != wantScreenShare ||
		grant.GetCanPublishSource(livekit.TrackSource_SCREEN_SHARE_AUDIO) != wantScreenShare {
		t.Fatal("P4-05 provider token grant is not exact")
	}
}

func waitForP405ProviderParticipantCount(
	ctx context.Context,
	inspector *lksdk.RoomServiceClient,
	roomName string,
	want int,
) bool {
	return waitForP405ProviderCondition(ctx, func() bool {
		if inspector == nil {
			return false
		}
		response, err := inspector.ListParticipants(ctx, &livekit.ListParticipantsRequest{
			Room: roomName,
		})
		return err == nil && response != nil && len(response.GetParticipants()) == want
	})
}

func waitForP405RemoteParticipantCount(ctx context.Context, room *lksdk.Room, want int) bool {
	return waitForP405ProviderCondition(ctx, func() bool {
		return room != nil && len(room.GetRemoteParticipants()) == want
	})
}

func waitForP405DeliveredSources(
	ctx context.Context,
	probe *p405ProviderDeliveryProbe,
	want []p405ProviderTrackSpec,
) bool {
	return waitForP405ProviderCondition(ctx, func() bool { return probe.hasExactSources(want) })
}

func waitForP405ProviderSources(
	ctx context.Context,
	inspector *lksdk.RoomServiceClient,
	roomName string,
	want []p405ProviderTrackSpec,
) bool {
	return waitForP405ProviderCondition(ctx, func() bool {
		sources, ok := p405ProviderSources(ctx, inspector, roomName)
		if !ok || len(sources) != len(want) {
			return false
		}
		for _, spec := range want {
			if sources[spec.source] != 1 {
				return false
			}
		}
		return true
	})
}

func p405ProviderHasAnySource(
	ctx context.Context,
	inspector *lksdk.RoomServiceClient,
	roomName string,
	specs []p405ProviderTrackSpec,
) (bool, bool) {
	sources, ok := p405ProviderSources(ctx, inspector, roomName)
	if !ok {
		return false, false
	}
	for _, spec := range specs {
		if sources[spec.source] > 0 {
			return true, true
		}
	}
	return false, true
}

func p405ProviderSources(
	ctx context.Context,
	inspector *lksdk.RoomServiceClient,
	roomName string,
) (map[livekit.TrackSource]int, bool) {
	if inspector == nil {
		return nil, false
	}
	response, err := inspector.ListParticipants(ctx, &livekit.ListParticipantsRequest{
		Room: roomName,
	})
	if err != nil || response == nil {
		return nil, false
	}
	sources := make(map[livekit.TrackSource]int)
	for _, participant := range response.GetParticipants() {
		for _, track := range participant.GetTracks() {
			sources[track.GetSource()]++
		}
	}
	return sources, true
}

func waitForP405ProviderCondition(ctx context.Context, condition func() bool) bool {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func p405ProviderRemainsQuiet(observed func() bool) bool {
	timer := time.NewTimer(p405ProviderQuietWindow)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if observed() {
			return false
		}
		select {
		case <-timer.C:
			return !observed()
		case <-ticker.C:
		}
	}
}
