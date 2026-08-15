//go:build integration

package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/livekit"
	protologger "github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
)

const (
	p411ProviderConfirmation = "I_UNDERSTAND_P4_11_ISOLATED_LIVEKIT_LOAD"
	p411ProviderTimeout      = 210 * time.Second
	p411CoreAPIWarmupTimeout = 30 * time.Second
	p411TTMTarget            = 10 * time.Second
	p411SustainDuration      = 120 * time.Second
)

var p411ProviderConflictingEnvironment = []string{
	"DATABASE_MIGRATION_URL",
	"DATABASE_POOL_URL",
	"DATABASE_POLL_MAINTENANCE_URL",
	"P4_10_DISPOSABLE_CONFIRM",
	"P4_10_OWNER_PREFLIGHT",
	"P4_10_ACL_PROVISION_CONFIRM",
	"P4_10_SHARED_CONFIRM",
	"P4_10_SHARED_ACL_PROVISION_CONFIRM",
	"P4_10_SHARED_SNAPSHOT_CONFIRM",
}

func TestP411UnreachableProviderFailsClosed(t *testing.T) {
	provider, err := NewLiveKitRoomProvider(
		"wss://127.0.0.1:1",
		"p411-local-unreachable-key",
		"p411-local-unreachable-secret",
	)
	if err != nil {
		t.Fatal("initialize deterministic unreachable P4-11 provider")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	startedAt := time.Now()
	room, err := provider.EnsureRoom(ctx, opaqueProviderRoomName(uuid.New()))
	if err == nil || room.SID != "" {
		t.Fatal("unreachable P4-11 provider did not fail closed")
	}
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatal("unreachable P4-11 provider did not return the typed unavailable error")
	}
	if time.Since(startedAt) > 5*time.Second {
		t.Fatal("unreachable P4-11 provider exceeded the bounded failure window")
	}
}

func TestLiveKitP411TenCycleJoinLeaveCleanup(t *testing.T) {
	serverURL, apiKey, apiSecret := requireP411IsolatedProvider(t)
	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 cleanup provider")
	}
	issuer, err := NewLiveKitTokenIssuer(apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 cleanup token issuer")
	}
	inspector := lksdk.NewRoomServiceClient(serverURL, apiKey, apiSecret)

	baselineGoroutines := runtime.NumGoroutine()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for cycle := 1; cycle <= 10; cycle++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		roomName := opaqueProviderRoomName(uuid.New())
		created, createErr := provider.EnsureRoom(ctx, roomName)
		if createErr != nil || created.SID == "" {
			cancel()
			t.Fatalf("create isolated P4-11 cleanup room at cycle %d", cycle)
		}
		token, issueErr := issuer.Issue(TokenGrant{
			RoomName:            roomName,
			ParticipantIdentity: opaqueParticipantIdentity(uuid.New()),
			ParticipantName:     "P4-11 cleanup participant",
			Role:                "student",
			ClassRole:           "student",
			CanSubscribe:        true,
			ValidFor:            2 * time.Minute,
		})
		if issueErr != nil {
			_ = provider.DeleteRoom(ctx, roomName)
			cancel()
			t.Fatalf("issue P4-11 cleanup credential at cycle %d", cycle)
		}
		room, connectErr := lksdk.ConnectToRoomWithToken(
			serverURL,
			token,
			lksdk.NewRoomCallback(),
			lksdk.WithConnectTimeout(15*time.Second),
			lksdk.WithAutoSubscribe(true),
			lksdk.WithICETransportPolicy(webrtc.ICETransportPolicyRelay),
			lksdk.WithLogger(protologger.GetDiscardLogger()),
		)
		if connectErr != nil {
			_ = provider.DeleteRoom(ctx, roomName)
			cancel()
			t.Fatalf("join P4-11 cleanup room at cycle %d", cycle)
		}
		if !waitForP405ProviderParticipantCount(ctx, inspector, roomName, 1) {
			room.Disconnect()
			_ = provider.DeleteRoom(ctx, roomName)
			cancel()
			t.Fatalf("observe P4-11 cleanup participant at cycle %d", cycle)
		}
		room.Disconnect()
		if deleteErr := provider.DeleteRoom(ctx, roomName); deleteErr != nil ||
			!waitForP402ProviderRoomCount(ctx, provider, roomName, 0) {
			cancel()
			t.Fatalf("clean P4-11 room to zero at cycle %d", cycle)
		}
		cancel()
	}

	postCleanupGoroutines := waitForP411GoroutineCleanup(baselineGoroutines, 10)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	t.Logf(
		"P4_11_TEN_CYCLE cycles=10 cleanup_zero=true post_cleanup_heap_delta_bytes=%d post_cleanup_goroutine_delta=%d",
		int64(after.HeapAlloc)-int64(before.HeapAlloc),
		postCleanupGoroutines-baselineGoroutines,
	)
}

func TestLiveKitP411InvalidCredentialFailsClosedAndPreservesExistingRoom(t *testing.T) {
	serverURL, apiKey, apiSecret := requireP411IsolatedProvider(t)
	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 valid provider")
	}
	issuer, err := NewLiveKitTokenIssuer(apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 valid token issuer")
	}
	inspector := lksdk.NewRoomServiceClient(serverURL, apiKey, apiSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	existingRoomName := opaqueProviderRoomName(uuid.New())
	created, err := provider.EnsureRoom(ctx, existingRoomName)
	if err != nil || created.SID == "" {
		t.Fatal("create existing P4-11 room")
	}
	defer func() {
		_ = provider.DeleteRoom(context.Background(), existingRoomName)
	}()
	token, err := issuer.Issue(TokenGrant{
		RoomName:            existingRoomName,
		ParticipantIdentity: opaqueParticipantIdentity(uuid.New()),
		ParticipantName:     "P4-11 existing-room participant",
		Role:                "student",
		ClassRole:           "student",
		CanSubscribe:        true,
		ValidFor:            2 * time.Minute,
	})
	if err != nil {
		t.Fatal("issue existing-room credential")
	}
	room, err := lksdk.ConnectToRoomWithToken(
		serverURL,
		token,
		lksdk.NewRoomCallback(),
		lksdk.WithConnectTimeout(15*time.Second),
		lksdk.WithAutoSubscribe(true),
		lksdk.WithICETransportPolicy(webrtc.ICETransportPolicyRelay),
		lksdk.WithLogger(protologger.GetDiscardLogger()),
	)
	if err != nil {
		t.Fatal("join existing P4-11 room")
	}
	defer room.Disconnect()
	if !waitForP405ProviderParticipantCount(ctx, inspector, existingRoomName, 1) {
		t.Fatal("existing P4-11 room did not become healthy")
	}

	invalidProvider, err := NewLiveKitRoomProvider(
		serverURL,
		apiKey+"-invalid-p411-probe",
		apiSecret,
	)
	if err != nil {
		t.Fatal("initialize invalid P4-11 credential probe")
	}
	invalidRoom, invalidErr := invalidProvider.EnsureRoom(ctx, opaqueProviderRoomName(uuid.New()))
	if invalidErr == nil || invalidRoom.SID != "" || !errors.Is(invalidErr, ErrProviderUnavailable) {
		t.Fatal("invalid P4-11 credential did not fail closed with typed unavailable")
	}
	if !waitForP405ProviderParticipantCount(ctx, inspector, existingRoomName, 1) {
		t.Fatal("invalid credential probe disturbed the existing P4-11 room")
	}

	recoveryRoomName := opaqueProviderRoomName(uuid.New())
	recoveryRoom, err := provider.EnsureRoom(ctx, recoveryRoomName)
	if err != nil || recoveryRoom.SID == "" {
		t.Fatal("valid P4-11 provider did not recover new-room creation")
	}
	if err := provider.DeleteRoom(ctx, recoveryRoomName); err != nil ||
		!waitForP402ProviderRoomCount(ctx, provider, recoveryRoomName, 0) {
		t.Fatal("cleanup recovered P4-11 room")
	}
	t.Log("P4_11_INVALID_CREDENTIAL typed_unavailable=true existing_room_preserved=true recovery_create=true cleanup_zero=true")
}

func TestLiveKitP411RevokedCredentialFailsClosed(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_11_ROTATION_CONFIRM")) !=
		"I_REVOKED_THE_P4_11_ISOLATED_TEMPORARY_KEY" {
		t.Skip("P4_11 revoked-key confirmation is not set")
	}
	serverURL, apiKey, apiSecret := requireP411IsolatedProvider(t)
	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 revoked credential probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	room, err := provider.EnsureRoom(ctx, opaqueProviderRoomName(uuid.New()))
	if err == nil || room.SID != "" || !errors.Is(err, ErrProviderUnavailable) {
		t.Fatal("revoked P4-11 credential did not fail closed with typed unavailable")
	}
	t.Log("P4_11_REVOKED_CREDENTIAL typed_unavailable=true room_created=false")
}

func TestLiveKitP411PostRotationCredentialCreatesAndCleansRoom(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_11_ROTATION_CONFIRM")) !=
		"I_INSTALLED_THE_NEW_P4_11_ISOLATED_KEY" {
		t.Skip("P4_11 new-key confirmation is not set")
	}
	serverURL, apiKey, apiSecret := requireP411IsolatedProvider(t)
	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 post-rotation provider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	roomName := opaqueProviderRoomName(uuid.New())
	created, err := provider.EnsureRoom(ctx, roomName)
	if err != nil || created.SID == "" {
		t.Fatal("new P4-11 credential did not create the post-rotation room")
	}
	if err := provider.DeleteRoom(ctx, roomName); err != nil ||
		!waitForP402ProviderRoomCount(ctx, provider, roomName, 0) {
		t.Fatal("post-rotation P4-11 room cleanup did not converge to zero")
	}
	t.Log("P4_11_POST_ROTATION create=true cleanup_zero=true")
}

func TestLiveKitP411JoinStormAndMediaDeliveryProfile(t *testing.T) {
	confirmation := strings.TrimSpace(os.Getenv("P4_11_PROVIDER_CONFIRM"))
	if confirmation == "" {
		t.Skip("P4_11_PROVIDER_CONFIRM is not set; provider load remains opt-in")
	}
	if confirmation != p411ProviderConfirmation {
		t.Fatal("P4_11_PROVIDER_CONFIRM does not match the exact isolated-load confirmation")
	}
	// Apply the discard logger before any Room construction so acceptance output
	// cannot contain ICE candidates, network addresses, SDP, or token-adjacent data.
	lksdk.SetLogger(protologger.GetDiscardLogger())
	rejectP411ProviderConflictingEnvironment(t)

	profileText := strings.TrimSpace(os.Getenv("P4_11_LOAD_PROFILE"))
	profile, err := strconv.Atoi(profileText)
	if err != nil || (profile != 25 && profile != 50) {
		t.Fatal("P4_11_LOAD_PROFILE must be exactly 25 or 50")
	}
	expectedQuotaConfirmation := "I_CONFIRMED_P4_11_PROVIDER_QUOTA_FOR_" + profileText
	if strings.TrimSpace(os.Getenv("P4_11_PROVIDER_QUOTA_CONFIRM")) != expectedQuotaConfirmation {
		t.Fatal("P4_11_PROVIDER_QUOTA_CONFIRM does not match the exact selected profile")
	}
	if strings.TrimSpace(os.Getenv("P4_11_SUSTAIN_SECONDS")) != "120" {
		t.Fatal("P4_11_SUSTAIN_SECONDS must be exactly 120")
	}
	if strings.TrimSpace(os.Getenv("P4_11_CORE_API_HEALTH_CONFIRM")) !=
		"I_CONFIRMED_P4_11_READ_ONLY_CORE_API_HEALTH" {
		t.Fatal("P4_11_CORE_API_HEALTH_CONFIRM does not match the exact read-only confirmation")
	}

	serverURL := requireP411ProviderEnvironment(t, "LIVEKIT_URL")
	apiKey := requireP411ProviderEnvironment(t, "LIVEKIT_API_KEY")
	apiSecret := requireP411ProviderEnvironment(t, "LIVEKIT_API_SECRET")
	if !validP402ProviderSmokeURL(serverURL) {
		t.Fatal("P4-11 LiveKit provider URL is not a secure WebSocket origin")
	}
	coreAPIBaseURL := requireP411ProviderEnvironment(t, "P4_11_CORE_API_BASE_URL")
	if !validP411CoreAPIBaseURL(coreAPIBaseURL) {
		t.Fatal("P4-11 Core API base URL is not a credential-free HTTPS origin")
	}
	coreAPIBaseURL = strings.TrimSuffix(coreAPIBaseURL, "/")

	provider, err := NewLiveKitRoomProvider(serverURL, apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 LiveKit room provider")
	}
	issuer, err := NewLiveKitTokenIssuer(apiKey, apiSecret)
	if err != nil {
		t.Fatal("initialize P4-11 LiveKit token issuer")
	}
	inspector := lksdk.NewRoomServiceClient(serverURL, apiKey, apiSecret)

	ctx, cancel := context.WithTimeout(context.Background(), p411ProviderTimeout)
	defer cancel()
	roomName := opaqueProviderRoomName(uuid.New())
	created, err := provider.EnsureRoom(ctx, roomName)
	if err != nil || created.SID == "" {
		t.Fatal("create isolated P4-11 provider room")
	}
	run := &p411ProviderRun{provider: provider, roomName: roomName}
	t.Cleanup(func() { run.cleanup(t) })
	warmupContext, stopWarmup := context.WithTimeout(context.Background(), p411CoreAPIWarmupTimeout)
	coreAPIHealthy := waitForP411CoreAPIHealthy(warmupContext, coreAPIBaseURL)
	stopWarmup()
	if !coreAPIHealthy {
		t.Fatal("P4-11 Core API health did not converge before the load window")
	}

	tokens := make([]string, profile)
	for index := range profile {
		grant := TokenGrant{
			RoomName:            roomName,
			ParticipantIdentity: opaqueParticipantIdentity(uuid.New()),
			ParticipantName:     "P4-11 synthetic participant",
			Role:                "student",
			OrganizationRole:    "member",
			ClassRole:           "student",
			CanPublishData:      false,
			CanSubscribe:        index != 0,
			ValidFor:            5 * time.Minute,
		}
		if index == 0 {
			grant.Role = "teacher"
			grant.ClassRole = "teacher"
			grant.CanPublishCameraMicrophone = true
		}
		tokens[index], err = issuer.Issue(grant)
		if err != nil {
			t.Fatal("issue P4-11 synthetic participant credential")
		}
	}

	type joinResult struct {
		index     int
		room      *lksdk.Room
		startedAt time.Time
		duration  time.Duration
		delivery  <-chan time.Duration
	}
	track := newP405SyntheticTrack(t, p405MicrophoneTrack)
	run.tracks = append(run.tracks, track)
	results := make(chan joinResult, profile)
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	beforeGoroutines := runtime.NumGoroutine()
	loadStartedAt := time.Now()
	healthContext, stopHealthSampler := context.WithCancel(context.Background())
	healthResults := make(chan p411CoreAPIHealthResult, 1)
	go func() {
		healthResults <- sampleP411CoreAPIHealth(healthContext, coreAPIBaseURL)
	}()
	defer stopHealthSampler()
	for index := range profile {
		index := index
		go func() {
			callback := lksdk.NewRoomCallback()
			startedAt := time.Now()
			var delivered chan time.Duration
			var deliveredOnce sync.Once
			if index != 0 {
				delivered = make(chan time.Duration, 1)
				callback.OnTrackSubscribed = func(
					_ *webrtc.TrackRemote,
					publication *lksdk.RemoteTrackPublication,
					_ *lksdk.RemoteParticipant,
				) {
					if publication.Source() == livekit.TrackSource_MICROPHONE {
						deliveredOnce.Do(func() { delivered <- time.Since(startedAt) })
					}
				}
			}
			room, connectErr := lksdk.ConnectToRoomWithToken(
				serverURL,
				tokens[index],
				callback,
				lksdk.WithConnectTimeout(15*time.Second),
				lksdk.WithAutoSubscribe(index != 0),
				lksdk.WithICETransportPolicy(webrtc.ICETransportPolicyRelay),
				lksdk.WithLogger(protologger.GetDiscardLogger()),
			)
			if connectErr != nil {
				results <- joinResult{index: index, startedAt: startedAt}
				return
			}
			results <- joinResult{
				index:     index,
				room:      room,
				startedAt: startedAt,
				duration:  time.Since(startedAt),
				delivery:  delivered,
			}
		}()
	}

	joined := make([]joinResult, profile)
	durations := make([]time.Duration, 0, profile)
	joinedCount := 0
	publisherMediaReady := time.Duration(0)
	publisherPublishFailed := false
	for range profile {
		select {
		case result := <-results:
			joined[result.index] = result
			if result.room != nil {
				joinedCount++
				durations = append(durations, result.duration)
				run.rooms = append(run.rooms, result.room)
				if result.index == 0 {
					if _, publishErr := result.room.LocalParticipant.PublishTrack(
						track,
						&lksdk.TrackPublicationOptions{
							Name:   "p411-load-microphone",
							Source: livekit.TrackSource_MICROPHONE,
						},
					); publishErr != nil {
						publisherPublishFailed = true
					} else {
						publisherMediaReady = time.Since(result.startedAt)
					}
				}
			}
		case <-ctx.Done():
			t.Fatal("P4-11 provider join storm exceeded its bounded timeout")
		}
	}
	if joinedCount != profile {
		t.Fatalf("P4-11 provider join success was below target: profile=%d joined=%d", profile, joinedCount)
	}
	if publisherPublishFailed || publisherMediaReady == 0 {
		t.Fatal("publish P4-11 synthetic microphone")
	}
	if !waitForP405ProviderParticipantCount(ctx, inspector, roomName, profile) {
		t.Fatal("P4-11 provider participant count did not converge to the selected profile")
	}

	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	connectP95 := durations[(len(durations)*95+99)/100-1]

	if joined[0].room == nil {
		t.Fatal("P4-11 provider publisher was not connected")
	}

	timeToMedia := []time.Duration{publisherMediaReady}
	deliveredCount := 0
	for index := 1; index < profile; index++ {
		select {
		case deliveredAt := <-joined[index].delivery:
			deliveredCount++
			timeToMedia = append(timeToMedia, deliveredAt)
		case <-ctx.Done():
			t.Fatal("P4-11 synthetic microphone did not reach every subscriber")
		}
	}
	if deliveredCount != profile-1 {
		t.Fatal("P4-11 provider media delivery was incomplete")
	}
	sort.Slice(timeToMedia, func(left, right int) bool { return timeToMedia[left] < timeToMedia[right] })
	timeToMediaP95 := timeToMedia[(len(timeToMedia)*95+99)/100-1]
	if timeToMediaP95 >= p411TTMTarget {
		t.Fatalf(
			"P4-11 provider TTM p95 missed target: profile=%d p95_ms=%d",
			profile,
			timeToMediaP95.Milliseconds(),
		)
	}

	sustainDeadline := time.NewTimer(p411SustainDuration)
	sustainTicker := time.NewTicker(5 * time.Second)
	defer sustainDeadline.Stop()
	defer sustainTicker.Stop()
	sustained := false
	for !sustained {
		select {
		case <-sustainDeadline.C:
			sustained = true
		case <-sustainTicker.C:
			probeContext, probeCancel := context.WithTimeout(ctx, 4*time.Second)
			stable := waitForP405ProviderParticipantCount(
				probeContext,
				inspector,
				roomName,
				profile,
			)
			probeCancel()
			if !stable {
				t.Fatal("P4-11 sustained participant count left the selected profile")
			}
		case <-ctx.Done():
			t.Fatal("P4-11 sustained provider profile exceeded its bounded timeout")
		}
	}
	stopHealthSampler()
	healthResult := <-healthResults

	var active runtime.MemStats
	runtime.ReadMemStats(&active)
	activeHeapDelta := int64(active.HeapAlloc) - int64(before.HeapAlloc)
	activeGoroutineDelta := runtime.NumGoroutine() - beforeGoroutines
	run.cleanup(t)
	if !run.cleanupZero {
		t.Fatal("P4-11 provider cleanup did not return the exact room count to zero")
	}
	postCleanupGoroutines := waitForP411GoroutineCleanup(beforeGoroutines, profile)
	var afterCleanup runtime.MemStats
	runtime.ReadMemStats(&afterCleanup)
	postCleanupHeapDelta := int64(afterCleanup.HeapAlloc) - int64(before.HeapAlloc)
	postCleanupGoroutineDelta := postCleanupGoroutines - beforeGoroutines
	healthP95 := time.Duration(0)
	if len(healthResult.durations) > 0 {
		sort.Slice(healthResult.durations, func(left, right int) bool {
			return healthResult.durations[left] < healthResult.durations[right]
		})
		healthP95 = healthResult.durations[(len(healthResult.durations)*95+99)/100-1]
	}

	t.Logf(
		"P4_11_LOAD profile=%d joined=%d success_bp=%d connect_p95_ms=%d ttm_p95_ms=%d sustain_seconds=120 active_heap_delta_bytes=%d active_goroutine_delta=%d post_cleanup_heap_delta_bytes=%d post_cleanup_goroutine_delta=%d delivered=%d core_health_requests=%d core_health_failures=%d core_health_endpoint_failures=%d core_ready_endpoint_failures=%d core_transport_failures=%d core_status_failures=%d core_health_p95_ms=%d elapsed_ms=%d cleanup_zero=true",
		profile,
		joinedCount,
		joinedCount*10_000/profile,
		connectP95.Milliseconds(),
		timeToMediaP95.Milliseconds(),
		activeHeapDelta,
		activeGoroutineDelta,
		postCleanupHeapDelta,
		postCleanupGoroutineDelta,
		deliveredCount,
		healthResult.requests,
		healthResult.failures,
		healthResult.healthFailures,
		healthResult.readyFailures,
		healthResult.transportFailures,
		healthResult.statusFailures,
		healthP95.Milliseconds(),
		time.Since(loadStartedAt).Milliseconds(),
	)
	if healthResult.requests < 4 || healthResult.failures != 0 {
		t.Fatalf(
			"P4-11 Core API health was not stable during load: requests=%d failures=%d health_failures=%d ready_failures=%d transport_failures=%d status_failures=%d cleanup_zero=true",
			healthResult.requests,
			healthResult.failures,
			healthResult.healthFailures,
			healthResult.readyFailures,
			healthResult.transportFailures,
			healthResult.statusFailures,
		)
	}
}

func waitForP411GoroutineCleanup(baseline, profile int) int {
	deadline := time.Now().Add(10 * time.Second)
	for {
		runtime.GC()
		current := runtime.NumGoroutine()
		if current <= baseline+profile || time.Now().After(deadline) {
			return current
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func validP411CoreAPIBaseURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil &&
		(parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" && parsed.Fragment == ""
}

type p411CoreAPIHealthResult struct {
	requests          int
	failures          int
	healthFailures    int
	readyFailures     int
	transportFailures int
	statusFailures    int
	durations         []time.Duration
}

func waitForP411CoreAPIHealthy(ctx context.Context, baseURL string) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for {
		healthy := true
		for _, path := range []string{"/health", "/ready"} {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
			if err != nil {
				return false
			}
			response, err := client.Do(request)
			if err != nil {
				healthy = false
				continue
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4_096))
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				healthy = false
			}
		}
		if healthy {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
}

func sampleP411CoreAPIHealth(ctx context.Context, baseURL string) p411CoreAPIHealthResult {
	result := p411CoreAPIHealthResult{}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	probe := func(path string) {
		markFailure := func() {
			result.failures++
			if path == "/health" {
				result.healthFailures++
			} else {
				result.readyFailures++
			}
		}
		startedAt := time.Now()
		requestContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(requestContext, http.MethodGet, baseURL+path, nil)
		if err != nil {
			markFailure()
			result.transportFailures++
			return
		}
		response, err := client.Do(request)
		result.requests++
		result.durations = append(result.durations, time.Since(startedAt))
		if err != nil {
			markFailure()
			result.transportFailures++
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4_096))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			markFailure()
			result.statusFailures++
		}
	}
	probe("/health")
	probe("/ready")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return result
		case <-ticker.C:
			probe("/health")
			probe("/ready")
		}
	}
}

func rejectP411ProviderConflictingEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range p411ProviderConflictingEnvironment {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatal("P4-11 provider load refuses database or stale shared-stage confirmations")
		}
	}
}

func requireP411ProviderEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("P4-11 provider load requires %s after explicit opt-in", key)
	}
	return value
}

func requireP411IsolatedProvider(t *testing.T) (string, string, string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_11_PROVIDER_CONFIRM")) != p411ProviderConfirmation {
		t.Skip("P4_11 isolated provider confirmation is not set")
	}
	// Set the SDK-wide logger before Room construction so Pion cannot emit
	// candidate addresses during a privacy-sensitive acceptance run.
	lksdk.SetLogger(protologger.GetDiscardLogger())
	rejectP411ProviderConflictingEnvironment(t)
	serverURL := requireP411ProviderEnvironment(t, "LIVEKIT_URL")
	if !validP402ProviderSmokeURL(serverURL) {
		t.Fatal("P4-11 LiveKit provider URL is not a secure WebSocket origin")
	}
	return serverURL,
		requireP411ProviderEnvironment(t, "LIVEKIT_API_KEY"),
		requireP411ProviderEnvironment(t, "LIVEKIT_API_SECRET")
}

type p411ProviderRun struct {
	cleanupOnce sync.Once
	provider    *LiveKitRoomProvider
	roomName    string
	rooms       []*lksdk.Room
	tracks      []*lksdk.LocalTrack
	cleanupZero bool
}

func (run *p411ProviderRun) cleanup(t *testing.T) {
	t.Helper()
	run.cleanupOnce.Do(func() {
		for _, track := range run.tracks {
			if track != nil {
				if err := track.Close(); err != nil {
					t.Error("close P4-11 synthetic track")
				}
			}
		}
		for _, room := range run.rooms {
			if room != nil {
				room.Disconnect()
			}
		}

		cleanupContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := run.provider.DeleteRoom(cleanupContext, run.roomName); err != nil {
			t.Error("delete exact P4-11 provider room during cleanup")
			return
		}
		run.cleanupZero = waitForP402ProviderRoomCount(
			cleanupContext,
			run.provider,
			run.roomName,
			0,
		)
		if !run.cleanupZero {
			t.Error("P4-11 provider room count did not return to zero")
		} else {
			t.Log("P4_11_CLEANUP cleanup_zero=true")
		}
	})
}
