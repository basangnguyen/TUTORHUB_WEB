package media

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"
)

var (
	ErrProviderUnavailable     = ErrMediaProviderUnavailable
	ErrInvalidProviderResponse = errors.New("invalid media provider response")
)

var (
	opaqueProviderRoomNamePattern = regexp.MustCompile(`^r_[a-f0-9]{32}$`)
	providerSIDPattern            = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)
)

type ProviderRoom struct {
	SID string
}

type RoomProvider interface {
	EnsureRoom(context.Context, string) (ProviderRoom, error)
	DeleteRoom(context.Context, string) error
}

type liveKitRoomService interface {
	CreateRoom(context.Context, *livekit.CreateRoomRequest) (*livekit.Room, error)
	ListRooms(context.Context, *livekit.ListRoomsRequest) (*livekit.ListRoomsResponse, error)
	DeleteRoom(context.Context, *livekit.DeleteRoomRequest) (*livekit.DeleteRoomResponse, error)
}

type liveKitModerationService interface {
	ListParticipants(context.Context, *livekit.ListParticipantsRequest) (*livekit.ListParticipantsResponse, error)
	RemoveParticipant(context.Context, *livekit.RoomParticipantIdentity) (*livekit.RemoveParticipantResponse, error)
	MutePublishedTrack(context.Context, *livekit.MuteRoomTrackRequest) (*livekit.MuteRoomTrackResponse, error)
}

type LiveKitRoomProvider struct {
	rooms      liveKitRoomService
	moderation liveKitModerationService
}

func NewLiveKitRoomProvider(serverURL, apiKey, apiSecret string) (*LiveKitRoomProvider, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(apiSecret) == "" {
		return nil, fmt.Errorf("LiveKit server URL, API key, and secret are required")
	}

	client := lksdk.NewRoomServiceClient(serverURL, apiKey, apiSecret)
	return &LiveKitRoomProvider{rooms: client, moderation: client}, nil
}

func (provider *LiveKitRoomProvider) EnsureRoom(
	ctx context.Context,
	roomName string,
) (ProviderRoom, error) {
	if provider == nil || provider.rooms == nil {
		return ProviderRoom{}, ErrProviderUnavailable
	}
	if !opaqueProviderRoomNamePattern.MatchString(roomName) {
		return ProviderRoom{}, ErrInvalidRequest
	}

	room, err := provider.rooms.CreateRoom(ctx, &livekit.CreateRoomRequest{Name: roomName})
	if err != nil {
		if providerErrorCode(err) == twirp.AlreadyExists {
			return provider.findRoom(ctx, roomName)
		}
		return ProviderRoom{}, normalizeProviderOperationError("create room", err)
	}

	return validatedProviderRoom(room, roomName, "create room")
}

func (provider *LiveKitRoomProvider) findRoom(
	ctx context.Context,
	roomName string,
) (ProviderRoom, error) {
	response, err := provider.rooms.ListRooms(ctx, &livekit.ListRoomsRequest{
		Names: []string{roomName},
	})
	if err != nil {
		return ProviderRoom{}, normalizeProviderOperationError("list room", err)
	}
	if response == nil {
		return ProviderRoom{}, invalidProviderResponse("list room")
	}
	for _, room := range response.GetRooms() {
		if room.GetName() == roomName {
			return validatedProviderRoom(room, roomName, "list room")
		}
	}

	return ProviderRoom{}, invalidProviderResponse("list room")
}

func (provider *LiveKitRoomProvider) DeleteRoom(ctx context.Context, roomName string) error {
	if provider == nil || provider.rooms == nil {
		return ErrProviderUnavailable
	}
	if !opaqueProviderRoomNamePattern.MatchString(roomName) {
		return ErrInvalidRequest
	}

	response, err := provider.rooms.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: roomName})
	if err != nil {
		if providerErrorCode(err) == twirp.NotFound {
			return nil
		}
		return normalizeProviderOperationError("delete room", err)
	}
	if response == nil {
		return invalidProviderResponse("delete room")
	}

	return nil
}

// MuteParticipantMicrophone is intentionally one-way. TutorHub never sends a
// LiveKit request with Muted=false; unmute always requires participant consent.
func (provider *LiveKitRoomProvider) MuteParticipantMicrophone(
	ctx context.Context,
	roomName string,
	participantIdentity string,
) error {
	if provider == nil || provider.moderation == nil {
		return ErrProviderUnavailable
	}
	if !opaqueProviderRoomNamePattern.MatchString(roomName) ||
		!validProviderIdentifier(participantIdentity, 128) {
		return ErrInvalidRequest
	}
	participants, err := provider.moderation.ListParticipants(
		ctx, &livekit.ListParticipantsRequest{Room: roomName},
	)
	if err != nil {
		return normalizeProviderOperationError("list participant tracks", err)
	}
	if participants == nil {
		return invalidProviderResponse("list participant tracks")
	}
	foundParticipant := false
	for _, participant := range participants.GetParticipants() {
		if participant == nil || participant.GetIdentity() != participantIdentity {
			continue
		}
		foundParticipant = true
		for _, track := range participant.GetTracks() {
			if track == nil || track.GetType() != livekit.TrackType_AUDIO ||
				track.GetSource() != livekit.TrackSource_MICROPHONE || track.GetMuted() {
				continue
			}
			response, muteErr := provider.moderation.MutePublishedTrack(ctx, &livekit.MuteRoomTrackRequest{
				Room: roomName, Identity: participantIdentity, TrackSid: track.GetSid(), Muted: true,
			})
			if muteErr != nil {
				return normalizeProviderOperationError("mute microphone", muteErr)
			}
			if response == nil {
				return invalidProviderResponse("mute microphone")
			}
		}
		return nil
	}
	if !foundParticipant {
		// A participant already gone is the converged result of a mute request.
		return nil
	}
	return nil
}

func (provider *LiveKitRoomProvider) RemoveParticipant(
	ctx context.Context,
	roomName string,
	participantIdentity string,
) error {
	if provider == nil || provider.moderation == nil {
		return ErrProviderUnavailable
	}
	if !opaqueProviderRoomNamePattern.MatchString(roomName) ||
		!validProviderIdentifier(participantIdentity, 128) {
		return ErrInvalidRequest
	}
	response, err := provider.moderation.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{
		Room: roomName, Identity: participantIdentity,
	})
	if err != nil {
		if providerErrorCode(err) == twirp.NotFound {
			return nil
		}
		return normalizeProviderOperationError("remove participant", err)
	}
	if response == nil {
		return invalidProviderResponse("remove participant")
	}
	return nil
}

func validatedProviderRoom(
	room *livekit.Room,
	expectedName string,
	operation string,
) (ProviderRoom, error) {
	if room == nil || room.GetName() != expectedName || !providerSIDPattern.MatchString(room.GetSid()) {
		return ProviderRoom{}, invalidProviderResponse(operation)
	}

	return ProviderRoom{SID: room.GetSid()}, nil
}

func invalidProviderResponse(operation string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProviderResponse, operation)
}

func normalizeProviderOperationError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || providerErrorCode(err) == twirp.Canceled {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) || providerErrorCode(err) == twirp.DeadlineExceeded {
		return context.DeadlineExceeded
	}

	return fmt.Errorf("%w: %s", ErrProviderUnavailable, operation)
}

func providerErrorCode(err error) twirp.ErrorCode {
	var providerError twirp.Error
	if errors.As(err, &providerError) {
		return providerError.Code()
	}
	return twirp.Unknown
}
