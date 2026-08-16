package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/twitchtv/twirp"
)

const (
	defaultEnvFile = ".env.p4-11-livekit.local"
	listenAddress  = "127.0.0.1:4179"
	allowedOrigin  = "http://127.0.0.1:5173"
	confirmation   = "I_UNDERSTAND_P4_11_ISOLATED_LIVEKIT_LOAD"
)

var (
	teacherParticipantKey = uuid.MustParse("e4f2f4a2-1e4b-4d5d-a920-0e32256998ec")
	studentParticipantKey = uuid.MustParse("14ad5f16-7c78-46d3-bc10-1563afcb51c7")
)

type configuration struct {
	serverURL string
	apiKey    string
	apiSecret string
}

type roomService interface {
	ListRooms(context.Context, *livekit.ListRoomsRequest) (*livekit.ListRoomsResponse, error)
	ListParticipants(context.Context, *livekit.ListParticipantsRequest) (*livekit.ListParticipantsResponse, error)
	DeleteRoom(context.Context, *livekit.DeleteRoomRequest) (*livekit.DeleteRoomResponse, error)
}

type tokenIssuer interface {
	Issue(media.TokenGrant) (string, error)
}

type physicalHarness struct {
	serverURL      string
	roomName       string
	roomInstanceID uuid.UUID
	rooms          roomService
	recoveryRooms  roomService
	tokens         tokenIssuer
}

type credentialRequest struct {
	Role string `json:"role"`
}

type credentialProjection struct {
	AccessToken                string    `json:"access_token"`
	ServerURL                  string    `json:"server_url"`
	ParticipantSessionID       uuid.UUID `json:"participant_session_id"`
	RoomInstanceID             uuid.UUID `json:"room_instance_id"`
	JoinAttemptID              uuid.UUID `json:"join_attempt_id"`
	InstanceRole               string    `json:"instance_role"`
	CanPublishCameraMicrophone bool      `json:"can_publish_camera_microphone"`
	CanShareScreen             bool      `json:"can_share_screen"`
	CanSubscribe               bool      `json:"can_subscribe"`
	ExpiresAt                  time.Time `json:"expires_at"`
}

type harnessProjection struct {
	Role               string    `json:"role"`
	DisplayName        string    `json:"display_name"`
	SelfParticipantKey uuid.UUID `json:"self_participant_key"`
	PeerParticipantKey uuid.UUID `json:"peer_participant_key"`
	RoomInstanceID     uuid.UUID `json:"room_instance_id"`
}

type credentialResponse struct {
	Credential credentialProjection `json:"credential"`
	Harness    harnessProjection    `json:"harness"`
}

type statusResponse struct {
	RoomExists       bool `json:"room_exists"`
	ParticipantCount int  `json:"participant_count"`
	CleanupZero      bool `json:"cleanup_zero"`
}

func main() {
	envFile := flag.String("env-file", defaultEnvFile, "ignored local P4-11 LiveKit env file")
	recoveryEnvFile := flag.String("recovery-env-file", "", "ignored local P4-11 recovery env file")
	flag.Parse()

	cfg, err := loadConfiguration(*envFile)
	if err != nil {
		log.Fatal("P4-11 physical harness configuration is invalid")
	}
	issuer, err := media.NewLiveKitTokenIssuer(cfg.apiKey, cfg.apiSecret)
	if err != nil {
		log.Fatal("P4-11 physical harness credential issuer is unavailable")
	}
	var recoveryRooms roomService
	if path := strings.TrimSpace(*recoveryEnvFile); path != "" {
		recoveryCfg, recoveryErr := loadConfiguration(path)
		if recoveryErr != nil || recoveryCfg.serverURL != cfg.serverURL {
			log.Fatal("P4-11 physical harness recovery configuration is invalid")
		}
		recoveryRooms = lksdk.NewRoomServiceClient(
			recoveryCfg.serverURL,
			recoveryCfg.apiKey,
			recoveryCfg.apiSecret,
		)
	}
	roomName, err := newOpaqueRoomName()
	if err != nil {
		log.Fatal("P4-11 physical harness could not initialize")
	}
	harness := &physicalHarness{
		serverURL:      cfg.serverURL,
		roomName:       roomName,
		roomInstanceID: uuid.New(),
		rooms:          lksdk.NewRoomServiceClient(cfg.serverURL, cfg.apiKey, cfg.apiSecret),
		recoveryRooms:  recoveryRooms,
		tokens:         issuer,
	}

	httpServer := &http.Server{
		Addr:              listenAddress,
		Handler:           harness.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupZero := harness.cleanupRoom(ctx)
		log.Printf("P4-11 physical harness cleanup_zero=%t", cleanupZero)
		_ = httpServer.Shutdown(ctx)
	}()

	log.Printf("P4-11 physical credential boundary ready at http://%s", listenAddress)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("P4-11 physical harness stopped unexpectedly")
	}
}

func (h *physicalHarness) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/credential", h.credential)
	mux.HandleFunc("/v1/status", h.status)
	mux.HandleFunc("/v1/cleanup", h.cleanup)
	mux.HandleFunc("/v1/outage/terminate", h.terminateOutageRoom)
	return h.securityHeaders(mux)
}

func (h *physicalHarness) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Pragma", "no-cache")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		if request.Header.Get("Origin") != allowedOrigin {
			writeProblem(response, http.StatusForbidden, "origin_forbidden")
			return
		}
		response.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		response.Header().Set("Vary", "Origin")
		response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (h *physicalHarness) credential(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeProblem(response, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 256)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input credentialRequest
	if err := decoder.Decode(&input); err != nil {
		writeProblem(response, http.StatusBadRequest, "invalid_request")
		return
	}
	var extra any
	if decoder.Decode(&extra) == nil {
		writeProblem(response, http.StatusBadRequest, "invalid_request")
		return
	}

	role, displayName, instanceRole, selfKey, peerKey, canShare, ok := roleProjection(input.Role)
	if !ok {
		writeProblem(response, http.StatusBadRequest, "invalid_role")
		return
	}
	validFor := 10 * time.Minute
	expiresAt := time.Now().UTC().Add(validFor)
	participantSessionID := uuid.New()
	joinAttemptID := uuid.New()
	identity := "p_" + strings.ReplaceAll(participantSessionID.String(), "-", "")
	accessToken, err := h.tokens.Issue(media.TokenGrant{
		RoomName:                   h.roomName,
		ParticipantIdentity:        identity,
		ParticipantName:            displayName,
		ParticipantKey:             selfKey,
		Role:                       instanceRole,
		CanPublishCameraMicrophone: true,
		CanShareScreen:             canShare,
		CanPublishData:             false,
		CanSubscribe:               true,
		ValidFor:                   validFor,
	})
	if err != nil {
		writeProblem(response, http.StatusServiceUnavailable, "credential_unavailable")
		return
	}
	writeJSON(response, http.StatusOK, credentialResponse{
		Credential: credentialProjection{
			AccessToken:                accessToken,
			ServerURL:                  h.serverURL,
			ParticipantSessionID:       participantSessionID,
			RoomInstanceID:             h.roomInstanceID,
			JoinAttemptID:              joinAttemptID,
			InstanceRole:               instanceRole,
			CanPublishCameraMicrophone: true,
			CanShareScreen:             canShare,
			CanSubscribe:               true,
			ExpiresAt:                  expiresAt,
		},
		Harness: harnessProjection{
			Role:               role,
			DisplayName:        displayName,
			SelfParticipantKey: selfKey,
			PeerParticipantKey: peerKey,
			RoomInstanceID:     h.roomInstanceID,
		},
	})
}

func (h *physicalHarness) status(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeProblem(response, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	status, err := h.roomStatus(request.Context())
	if err != nil {
		writeProblem(response, http.StatusServiceUnavailable, "provider_unavailable")
		return
	}
	writeJSON(response, http.StatusOK, status)
}

func (h *physicalHarness) cleanup(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeProblem(response, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	status, err := h.roomStatus(request.Context())
	if err != nil {
		writeProblem(response, http.StatusServiceUnavailable, "provider_unavailable")
		return
	}
	if status.ParticipantCount != 0 {
		writeProblem(response, http.StatusConflict, "participants_active")
		return
	}
	if status.RoomExists {
		if _, err := h.rooms.DeleteRoom(request.Context(), &livekit.DeleteRoomRequest{Room: h.roomName}); err != nil && providerCode(err) != twirp.NotFound {
			writeProblem(response, http.StatusServiceUnavailable, "provider_unavailable")
			return
		}
	}
	verified, err := h.roomStatus(request.Context())
	if err != nil {
		writeProblem(response, http.StatusServiceUnavailable, "provider_unavailable")
		return
	}
	writeJSON(response, http.StatusOK, verified)
}

func (h *physicalHarness) terminateOutageRoom(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeProblem(response, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if h.recoveryRooms == nil {
		writeProblem(response, http.StatusServiceUnavailable, "recovery_unavailable")
		return
	}

	status, err := h.roomStatusWith(request.Context(), h.recoveryRooms)
	if err != nil {
		writeProblem(response, http.StatusServiceUnavailable, "provider_unavailable")
		return
	}
	if status.RoomExists {
		if _, err := h.recoveryRooms.DeleteRoom(
			request.Context(),
			&livekit.DeleteRoomRequest{Room: h.roomName},
		); err != nil && providerCode(err) != twirp.NotFound {
			writeProblem(response, http.StatusServiceUnavailable, "provider_unavailable")
			return
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		verified, verifyErr := h.roomStatusWith(request.Context(), h.recoveryRooms)
		if verifyErr != nil {
			writeProblem(response, http.StatusServiceUnavailable, "provider_unavailable")
			return
		}
		if verified.CleanupZero {
			writeJSON(response, http.StatusOK, verified)
			return
		}
		if time.Now().After(deadline) {
			writeProblem(response, http.StatusConflict, "cleanup_pending")
			return
		}
		select {
		case <-request.Context().Done():
			writeProblem(response, http.StatusRequestTimeout, "request_cancelled")
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (h *physicalHarness) roomStatus(ctx context.Context) (statusResponse, error) {
	return h.roomStatusWith(ctx, h.rooms)
}

func (h *physicalHarness) roomStatusWith(ctx context.Context, roomsClient roomService) (statusResponse, error) {
	rooms, err := roomsClient.ListRooms(ctx, &livekit.ListRoomsRequest{Names: []string{h.roomName}})
	if err != nil {
		return statusResponse{}, err
	}
	exists := false
	for _, room := range rooms.GetRooms() {
		if room.GetName() == h.roomName {
			exists = true
			break
		}
	}
	if !exists {
		return statusResponse{CleanupZero: true}, nil
	}
	participants, err := roomsClient.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: h.roomName})
	if err != nil {
		if providerCode(err) == twirp.NotFound {
			return statusResponse{CleanupZero: true}, nil
		}
		return statusResponse{}, err
	}
	count := len(participants.GetParticipants())
	return statusResponse{RoomExists: true, ParticipantCount: count, CleanupZero: false}, nil
}

func (h *physicalHarness) cleanupRoom(ctx context.Context) bool {
	status, err := h.roomStatus(ctx)
	if err != nil || status.ParticipantCount != 0 {
		return false
	}
	if status.RoomExists {
		if _, err := h.rooms.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: h.roomName}); err != nil && providerCode(err) != twirp.NotFound {
			return false
		}
	}
	status, err = h.roomStatus(ctx)
	return err == nil && status.CleanupZero
}

func loadConfiguration(path string) (configuration, error) {
	values, err := readEnvFile(path)
	if err != nil {
		return configuration{}, err
	}
	for name := range values {
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "DATABASE_") || strings.Contains(upper, "SHARED") {
			return configuration{}, fmt.Errorf("database or shared credentials are not allowed")
		}
	}
	if values["P4_11_PROVIDER_CONFIRM"] != confirmation {
		return configuration{}, fmt.Errorf("provider confirmation is missing")
	}
	serverURL := strings.TrimSpace(values["LIVEKIT_URL"])
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Scheme != "wss" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return configuration{}, fmt.Errorf("LIVEKIT_URL must be a credential-free wss origin")
	}
	apiKey := strings.TrimSpace(values["LIVEKIT_API_KEY"])
	apiSecret := strings.TrimSpace(values["LIVEKIT_API_SECRET"])
	if apiKey == "" || apiSecret == "" {
		return configuration{}, fmt.Errorf("LiveKit credentials are required")
	}
	return configuration{serverURL: serverURL, apiKey: apiKey, apiSecret: apiSecret}, nil
}

func readEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env assignment")
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[name] = value
	}
	return values, scanner.Err()
}

func roleProjection(role string) (string, string, string, uuid.UUID, uuid.UUID, bool, bool) {
	switch role {
	case "teacher":
		return role, "Giáo viên thử nghiệm", "host", teacherParticipantKey, studentParticipantKey, true, true
	case "student":
		return role, "Học viên thử nghiệm", "attendee", studentParticipantKey, teacherParticipantKey, false, true
	default:
		return "", "", "", uuid.Nil, uuid.Nil, false, false
	}
}

func newOpaqueRoomName() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "r_" + hex.EncodeToString(random), nil
}

func providerCode(err error) twirp.ErrorCode {
	var providerError twirp.Error
	if errors.As(err, &providerError) {
		return providerError.Code()
	}
	return twirp.Unknown
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(payload)
}

func writeProblem(response http.ResponseWriter, status int, code string) {
	writeJSON(response, status, map[string]string{"code": code})
}
