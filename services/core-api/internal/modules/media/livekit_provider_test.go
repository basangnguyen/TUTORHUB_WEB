package media

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/livekit/protocol/livekit"
	"github.com/twitchtv/twirp"
)

const testOpaqueProviderRoomName = "r_0123456789abcdef0123456789abcdef"

type fakeLiveKitRoomService struct {
	createRoom func(context.Context, *livekit.CreateRoomRequest) (*livekit.Room, error)
	listRooms  func(context.Context, *livekit.ListRoomsRequest) (*livekit.ListRoomsResponse, error)
	deleteRoom func(context.Context, *livekit.DeleteRoomRequest) (*livekit.DeleteRoomResponse, error)
}

func (fake *fakeLiveKitRoomService) CreateRoom(
	ctx context.Context,
	request *livekit.CreateRoomRequest,
) (*livekit.Room, error) {
	return fake.createRoom(ctx, request)
}

func (fake *fakeLiveKitRoomService) ListRooms(
	ctx context.Context,
	request *livekit.ListRoomsRequest,
) (*livekit.ListRoomsResponse, error) {
	return fake.listRooms(ctx, request)
}

func (fake *fakeLiveKitRoomService) DeleteRoom(
	ctx context.Context,
	request *livekit.DeleteRoomRequest,
) (*livekit.DeleteRoomResponse, error) {
	return fake.deleteRoom(ctx, request)
}

func TestLiveKitRoomProviderCreatesOpaqueRoom(t *testing.T) {
	t.Parallel()

	fake := &fakeLiveKitRoomService{
		createRoom: func(_ context.Context, request *livekit.CreateRoomRequest) (*livekit.Room, error) {
			if request.GetName() != testOpaqueProviderRoomName {
				t.Fatalf("unexpected room name: %q", request.GetName())
			}
			return &livekit.Room{Name: request.GetName(), Sid: "RM_safe_provider_sid"}, nil
		},
	}
	provider := &LiveKitRoomProvider{rooms: fake}

	room, err := provider.EnsureRoom(context.Background(), testOpaqueProviderRoomName)
	if err != nil {
		t.Fatalf("ensure room: %v", err)
	}
	if room.SID != "RM_safe_provider_sid" {
		t.Fatalf("unexpected provider room: %+v", room)
	}
}

func TestLiveKitRoomProviderRejectsNonOpaqueRoomName(t *testing.T) {
	t.Parallel()

	called := false
	provider := &LiveKitRoomProvider{rooms: &fakeLiveKitRoomService{
		createRoom: func(context.Context, *livekit.CreateRoomRequest) (*livekit.Room, error) {
			called = true
			return nil, nil
		},
	}}

	_, err := provider.EnsureRoom(context.Background(), "tenant-or-class-readable-name")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request, got %v", err)
	}
	if called {
		t.Fatal("provider must not be called for a non-opaque room name")
	}
}

func TestLiveKitRoomProviderRecoversExistingRoom(t *testing.T) {
	t.Parallel()

	fake := &fakeLiveKitRoomService{
		createRoom: func(context.Context, *livekit.CreateRoomRequest) (*livekit.Room, error) {
			return nil, twirp.NewError(twirp.AlreadyExists, "provider detail")
		},
		listRooms: func(_ context.Context, request *livekit.ListRoomsRequest) (*livekit.ListRoomsResponse, error) {
			if len(request.GetNames()) != 1 || request.GetNames()[0] != testOpaqueProviderRoomName {
				t.Fatalf("unexpected room lookup: %+v", request.GetNames())
			}
			return &livekit.ListRoomsResponse{Rooms: []*livekit.Room{{
				Name: testOpaqueProviderRoomName,
				Sid:  "RM_existing_provider_sid",
			}}}, nil
		},
	}
	provider := &LiveKitRoomProvider{rooms: fake}

	room, err := provider.EnsureRoom(context.Background(), testOpaqueProviderRoomName)
	if err != nil {
		t.Fatalf("ensure existing room: %v", err)
	}
	if room.SID != "RM_existing_provider_sid" {
		t.Fatalf("unexpected existing room: %+v", room)
	}
}

func TestLiveKitRoomProviderNormalizesProviderError(t *testing.T) {
	t.Parallel()

	const sensitiveDetail = "provider-sensitive-detail"
	provider := &LiveKitRoomProvider{rooms: &fakeLiveKitRoomService{
		createRoom: func(context.Context, *livekit.CreateRoomRequest) (*livekit.Room, error) {
			return nil, errors.New(sensitiveDetail)
		},
	}}

	_, err := provider.EnsureRoom(context.Background(), testOpaqueProviderRoomName)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected normalized provider error, got %v", err)
	}
	if strings.Contains(err.Error(), sensitiveDetail) {
		t.Fatalf("provider detail leaked in normalized error: %v", err)
	}
}

func TestLiveKitRoomProviderRejectsInvalidProviderBinding(t *testing.T) {
	t.Parallel()

	provider := &LiveKitRoomProvider{rooms: &fakeLiveKitRoomService{
		createRoom: func(context.Context, *livekit.CreateRoomRequest) (*livekit.Room, error) {
			return &livekit.Room{Name: testOpaqueProviderRoomName, Sid: "invalid sid"}, nil
		},
	}}

	_, err := provider.EnsureRoom(context.Background(), testOpaqueProviderRoomName)
	if !errors.Is(err, ErrInvalidProviderResponse) {
		t.Fatalf("expected invalid provider response, got %v", err)
	}
}

func TestLiveKitRoomProviderDeleteIsIdempotent(t *testing.T) {
	t.Parallel()

	provider := &LiveKitRoomProvider{rooms: &fakeLiveKitRoomService{
		deleteRoom: func(_ context.Context, request *livekit.DeleteRoomRequest) (*livekit.DeleteRoomResponse, error) {
			if request.GetRoom() != testOpaqueProviderRoomName {
				t.Fatalf("unexpected deleted room: %q", request.GetRoom())
			}
			return nil, twirp.NewError(twirp.NotFound, "provider detail")
		},
	}}

	if err := provider.DeleteRoom(context.Background(), testOpaqueProviderRoomName); err != nil {
		t.Fatalf("delete missing room: %v", err)
	}
}
