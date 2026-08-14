package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const (
	maximumMediaRosterParticipants  = 50
	mediaReactionTTL                = 10 * time.Second
	mediaReactionGroupingWindow     = 750 * time.Millisecond
	mediaSignalReceiptRetention     = 24 * time.Hour
	maximumReactionSnapshotClusters = 50
	maximumReactionClusterCount     = 100
)

type MediaSignalKind string

const (
	MediaSignalHandRaise    MediaSignalKind = "hand_raise"
	MediaSignalHandLower    MediaSignalKind = "hand_lower"
	MediaSignalHandLowerOne MediaSignalKind = "hand_lower_one"
	MediaSignalHandLowerAll MediaSignalKind = "hand_lower_all"
	MediaSignalReaction     MediaSignalKind = "reaction"
)

type MediaReaction string

const (
	MediaReactionThumbsUp  MediaReaction = "thumbs_up"
	MediaReactionClap      MediaReaction = "clap"
	MediaReactionHeart     MediaReaction = "heart"
	MediaReactionCelebrate MediaReaction = "celebrate"
	MediaReactionLaugh     MediaReaction = "laugh"
	MediaReactionSurprised MediaReaction = "surprised"
)

type ParticipantConnectionState string

const (
	ParticipantConnectionJoining      ParticipantConnectionState = "joining"
	ParticipantConnectionConnected    ParticipantConnectionState = "connected"
	ParticipantConnectionReconnecting ParticipantConnectionState = "reconnecting"
)

var (
	ErrMediaSignalUnavailable       = errors.New("media participant signal unavailable")
	ErrInvalidMediaSignalRequest    = errors.New("invalid media participant signal request")
	ErrMediaSignalNotFound          = errors.New("media participant signal resource not found")
	ErrMediaSignalVersionConflict   = errors.New("media participant signal version conflict")
	ErrMediaSignalIdempotency       = errors.New("media participant signal idempotency conflict")
	ErrMediaSignalTargetUnavailable = errors.New("media participant signal target unavailable")
	mediaSignalIdempotencyPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
)

type MediaSignalRateLimitError struct {
	RetryAfter time.Duration
}

func (err *MediaSignalRateLimitError) Error() string {
	return "media participant signal rate limit exceeded"
}

// MediaParticipant is deliberately safe for every currently connected room
// participant. It excludes tenant/user/session/join-attempt/provider IDs and
// every email address. ParticipantKey is an independent opaque correlation key.
type MediaParticipant struct {
	ParticipantKey       uuid.UUID                     `json:"participant_key"`
	RosterSequence       int64                         `json:"roster_sequence"`
	DisplayName          string                        `json:"display_name"`
	InstanceRole         InstanceRole                  `json:"instance_role"`
	Connection           ParticipantConnectionState    `json:"connection_state"`
	ModerationOperations MediaParticipantModerationOps `json:"moderation_operations"`
}

type MediaParticipantModerationOps struct {
	CanPromoteCoHost bool `json:"can_promote_co_host"`
	CanDemoteCoHost  bool `json:"can_demote_co_host"`
	CanRemoteMute    bool `json:"can_remote_mute"`
	CanRemove        bool `json:"can_remove"`
}

type RaisedHand struct {
	ParticipantKey uuid.UUID `json:"participant_key"`
	SignalSequence int64     `json:"signal_sequence"`
	RaisedAt       time.Time `json:"raised_at"`
}

type ReactionCluster struct {
	Reaction            MediaReaction `json:"reaction"`
	Count               int           `json:"count"`
	FirstSignalSequence int64         `json:"first_signal_sequence"`
	LastSignalSequence  int64         `json:"last_signal_sequence"`
	AcceptedAt          time.Time     `json:"accepted_at"`
	ExpiresAt           time.Time     `json:"expires_at"`
}

type MediaSignalViewerOperations struct {
	CanRaiseHand     bool `json:"can_raise_hand"`
	CanSendReaction  bool `json:"can_send_reaction"`
	CanModerateHands bool `json:"can_moderate_hands"`
	CanLockRoom      bool `json:"can_lock_room"`
	CanEndRoom       bool `json:"can_end_room"`
}

type MediaParticipantSnapshot struct {
	RoomInstanceID     uuid.UUID                   `json:"room_instance_id"`
	ProjectionVersion  int64                       `json:"projection_version"`
	LastSignalSequence int64                       `json:"last_signal_sequence"`
	RoomLocked         bool                        `json:"room_locked"`
	SelfParticipantKey uuid.UUID                   `json:"self_participant_key"`
	ViewerOperations   MediaSignalViewerOperations `json:"viewer_operations"`
	Participants       []MediaParticipant          `json:"participants"`
	RaisedHands        []RaisedHand                `json:"raised_hands"`
	ReactionClusters   []ReactionCluster           `json:"reaction_clusters"`
	ServerTime         time.Time                   `json:"server_time"`
}

type GetMediaParticipantSnapshotInput struct {
	ExpectedSpaceVersion        int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
}

type SendMediaSignalInput struct {
	ExpectedSpaceVersion        int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
	ExpectedProjectionVersion   int64
	IdempotencyKey              string
	Kind                        MediaSignalKind
	TargetParticipantKey        uuid.UUID
	Reaction                    MediaReaction
}

type SendMediaSignalCommand struct {
	SpaceID                     uuid.UUID
	ExpectedSpaceVersion        int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
	ExpectedProjectionVersion   int64
	IdempotencyKey              string
	Kind                        MediaSignalKind
	TargetParticipantKey        uuid.UUID
	Reaction                    MediaReaction
	Fingerprint                 []byte
}

type mediaReactionEvent struct {
	Reaction       MediaReaction
	SignalSequence int64
	AcceptedAt     time.Time
	ExpiresAt      time.Time
}

type MediaSignalRepository interface {
	GetParticipantSnapshot(
		context.Context,
		AccessContext,
		uuid.UUID,
		GetMediaParticipantSnapshotInput,
	) (MediaParticipantSnapshot, error)
	SendSignal(
		context.Context,
		AccessContext,
		SendMediaSignalCommand,
	) (MediaParticipantSnapshot, error)
}

type MediaSignalServiceAPI interface {
	GetParticipantSnapshot(
		context.Context,
		AccessContext,
		uuid.UUID,
		GetMediaParticipantSnapshotInput,
	) (MediaParticipantSnapshot, error)
	SendSignal(
		context.Context,
		AccessContext,
		uuid.UUID,
		SendMediaSignalInput,
	) (MediaParticipantSnapshot, error)
}

type MediaSignalService struct {
	repository MediaSignalRepository
}

func NewMediaSignalService(
	repository MediaSignalRepository,
) (*MediaSignalService, error) {
	if repository == nil {
		return nil, fmt.Errorf("media signal repository is required")
	}
	return &MediaSignalService{repository: repository}, nil
}

func (service *MediaSignalService) GetParticipantSnapshot(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input GetMediaParticipantSnapshotInput,
) (MediaParticipantSnapshot, error) {
	if service == nil || service.repository == nil {
		return MediaParticipantSnapshot{}, ErrMediaSignalUnavailable
	}
	if !validMediaSignalAccess(access) || spaceID == uuid.Nil ||
		input.ExpectedSpaceVersion < 1 || input.ExpectedRoomInstanceID == uuid.Nil ||
		input.ExpectedRoomInstanceVersion < 1 {
		return MediaParticipantSnapshot{}, ErrInvalidMediaSignalRequest
	}
	snapshot, err := service.repository.GetParticipantSnapshot(
		ctx, access, spaceID, input,
	)
	if err != nil {
		return MediaParticipantSnapshot{}, normalizeMediaSignalError(err)
	}
	if !validParticipantSnapshot(snapshot, input.ExpectedRoomInstanceID) {
		return MediaParticipantSnapshot{}, ErrMediaSignalUnavailable
	}
	return snapshot, nil
}

func (service *MediaSignalService) SendSignal(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input SendMediaSignalInput,
) (MediaParticipantSnapshot, error) {
	if service == nil || service.repository == nil {
		return MediaParticipantSnapshot{}, ErrMediaSignalUnavailable
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !validMediaSignalAccess(access) || !validSendMediaSignalInput(spaceID, input) {
		return MediaParticipantSnapshot{}, ErrInvalidMediaSignalRequest
	}
	fingerprint := mediaSignalRequestFingerprint(spaceID, input)
	snapshot, err := service.repository.SendSignal(ctx, access, SendMediaSignalCommand{
		SpaceID: spaceID, ExpectedSpaceVersion: input.ExpectedSpaceVersion,
		ExpectedRoomInstanceID:      input.ExpectedRoomInstanceID,
		ExpectedRoomInstanceVersion: input.ExpectedRoomInstanceVersion,
		ExpectedProjectionVersion:   input.ExpectedProjectionVersion,
		IdempotencyKey:              input.IdempotencyKey, Kind: input.Kind,
		TargetParticipantKey: input.TargetParticipantKey, Reaction: input.Reaction,
		Fingerprint: fingerprint[:],
	})
	if err != nil {
		return MediaParticipantSnapshot{}, normalizeMediaSignalError(err)
	}
	if !validParticipantSnapshot(snapshot, input.ExpectedRoomInstanceID) {
		return MediaParticipantSnapshot{}, ErrMediaSignalUnavailable
	}
	return snapshot, nil
}

func validMediaSignalAccess(access AccessContext) bool {
	return access.TenantID != uuid.Nil && access.ActorID != uuid.Nil && access.SessionID != uuid.Nil
}

func validSendMediaSignalInput(spaceID uuid.UUID, input SendMediaSignalInput) bool {
	if spaceID == uuid.Nil || input.ExpectedSpaceVersion < 1 ||
		input.ExpectedRoomInstanceID == uuid.Nil || input.ExpectedRoomInstanceVersion < 1 ||
		input.ExpectedProjectionVersion < 1 || len(input.IdempotencyKey) < 16 ||
		len(input.IdempotencyKey) > 128 || !mediaSignalIdempotencyPattern.MatchString(input.IdempotencyKey) {
		return false
	}
	switch input.Kind {
	case MediaSignalHandRaise, MediaSignalHandLower, MediaSignalHandLowerAll:
		return input.TargetParticipantKey == uuid.Nil && input.Reaction == ""
	case MediaSignalHandLowerOne:
		return input.TargetParticipantKey != uuid.Nil && input.Reaction == ""
	case MediaSignalReaction:
		return input.TargetParticipantKey == uuid.Nil && validMediaReaction(input.Reaction)
	default:
		return false
	}
}

func validMediaReaction(reaction MediaReaction) bool {
	switch reaction {
	case MediaReactionThumbsUp, MediaReactionClap, MediaReactionHeart,
		MediaReactionCelebrate, MediaReactionLaugh, MediaReactionSurprised:
		return true
	default:
		return false
	}
}

func mediaSignalRequestFingerprint(spaceID uuid.UUID, input SendMediaSignalInput) [32]byte {
	return sha256.Sum256([]byte(strings.Join([]string{
		"media-signal-v1", spaceID.String(), input.ExpectedRoomInstanceID.String(),
		fmt.Sprintf("%d", input.ExpectedSpaceVersion),
		fmt.Sprintf("%d", input.ExpectedRoomInstanceVersion),
		fmt.Sprintf("%d", input.ExpectedProjectionVersion), string(input.Kind),
		input.TargetParticipantKey.String(), string(input.Reaction),
	}, "\x00")))
}

func validParticipantSnapshot(snapshot MediaParticipantSnapshot, expectedRoomID uuid.UUID) bool {
	if snapshot.RoomInstanceID != expectedRoomID || snapshot.ProjectionVersion < 1 ||
		snapshot.LastSignalSequence < 0 || snapshot.SelfParticipantKey == uuid.Nil ||
		snapshot.ServerTime.IsZero() || len(snapshot.Participants) > maximumMediaRosterParticipants ||
		len(snapshot.RaisedHands) > maximumMediaRosterParticipants ||
		len(snapshot.ReactionClusters) > maximumReactionSnapshotClusters {
		return false
	}
	participantKeys := make(map[uuid.UUID]struct{}, len(snapshot.Participants))
	lastSequence := int64(0)
	lastKey := uuid.Nil
	for _, participant := range snapshot.Participants {
		if participant.ParticipantKey == uuid.Nil || participant.RosterSequence < 1 ||
			strings.TrimSpace(participant.DisplayName) == "" ||
			len([]rune(participant.DisplayName)) > 200 || !validInstanceRole(participant.InstanceRole) ||
			!validParticipantConnection(participant.Connection) {
			return false
		}
		if participant.RosterSequence < lastSequence ||
			(participant.RosterSequence == lastSequence &&
				lastKey != uuid.Nil && participant.ParticipantKey.String() <= lastKey.String()) {
			return false
		}
		if _, duplicate := participantKeys[participant.ParticipantKey]; duplicate {
			return false
		}
		participantKeys[participant.ParticipantKey] = struct{}{}
		lastSequence, lastKey = participant.RosterSequence, participant.ParticipantKey
	}
	if _, found := participantKeys[snapshot.SelfParticipantKey]; !found {
		return false
	}
	lastHandSequence := int64(0)
	raisedParticipantKeys := make(map[uuid.UUID]struct{}, len(snapshot.RaisedHands))
	for _, hand := range snapshot.RaisedHands {
		if hand.ParticipantKey == uuid.Nil || hand.SignalSequence < 1 || hand.RaisedAt.IsZero() ||
			hand.SignalSequence < lastHandSequence {
			return false
		}
		if _, found := participantKeys[hand.ParticipantKey]; !found {
			return false
		}
		if _, duplicate := raisedParticipantKeys[hand.ParticipantKey]; duplicate {
			return false
		}
		raisedParticipantKeys[hand.ParticipantKey] = struct{}{}
		lastHandSequence = hand.SignalSequence
	}
	for _, cluster := range snapshot.ReactionClusters {
		if !validMediaReaction(cluster.Reaction) || cluster.Count < 1 ||
			cluster.Count > maximumReactionClusterCount || cluster.FirstSignalSequence < 1 ||
			cluster.LastSignalSequence < cluster.FirstSignalSequence ||
			cluster.AcceptedAt.IsZero() || !cluster.ExpiresAt.After(cluster.AcceptedAt) ||
			cluster.ExpiresAt.Sub(cluster.AcceptedAt) > mediaReactionTTL+mediaReactionGroupingWindow {
			return false
		}
	}
	return true
}

func validParticipantConnection(state ParticipantConnectionState) bool {
	switch state {
	case ParticipantConnectionJoining, ParticipantConnectionConnected,
		ParticipantConnectionReconnecting:
		return true
	default:
		return false
	}
}

func groupMediaReactionEvents(events []mediaReactionEvent) []ReactionCluster {
	sort.SliceStable(events, func(left, right int) bool {
		return events[left].SignalSequence < events[right].SignalSequence
	})
	clusters := make([]ReactionCluster, 0, maximumReactionSnapshotClusters)
	for _, event := range events {
		if !validMediaReaction(event.Reaction) || event.SignalSequence < 1 ||
			event.AcceptedAt.IsZero() || !event.ExpiresAt.After(event.AcceptedAt) {
			continue
		}
		grouped := false
		for index := len(clusters) - 1; index >= 0; index-- {
			cluster := &clusters[index]
			delta := event.AcceptedAt.Sub(cluster.AcceptedAt)
			if cluster.Reaction != event.Reaction ||
				delta < 0 || delta > mediaReactionGroupingWindow {
				continue
			}
			if cluster.Count < maximumReactionClusterCount {
				cluster.Count++
			}
			cluster.LastSignalSequence = event.SignalSequence
			if event.ExpiresAt.After(cluster.ExpiresAt) {
				cluster.ExpiresAt = event.ExpiresAt
			}
			grouped = true
			break
		}
		if grouped {
			continue
		}
		clusters = append(clusters, ReactionCluster{
			Reaction: event.Reaction, Count: 1,
			FirstSignalSequence: event.SignalSequence,
			LastSignalSequence:  event.SignalSequence,
			AcceptedAt:          event.AcceptedAt.UTC(), ExpiresAt: event.ExpiresAt.UTC(),
		})
		if len(clusters) > maximumReactionSnapshotClusters {
			clusters = append(
				[]ReactionCluster(nil),
				clusters[len(clusters)-maximumReactionSnapshotClusters:]...,
			)
		}
	}
	return clusters
}

func normalizeMediaSignalError(err error) error {
	if err == nil {
		return nil
	}
	known := []error{
		ErrMediaSignalUnavailable, ErrInvalidMediaSignalRequest, ErrMediaSignalNotFound,
		ErrMediaSignalVersionConflict, ErrMediaSignalIdempotency,
		ErrMediaSignalTargetUnavailable, ErrSpaceAccessDenied, ErrSpaceNotFound,
		ErrSourceUnavailable, ErrRoomNotOpen, ErrRoomLocked,
		featurecontrol.ErrInvalidControl, featurecontrol.ErrAccessDenied,
		featurecontrol.ErrTenantNotFound, featurecontrol.ErrFeatureDisabled,
		featurecontrol.ErrQuotaExceeded, featurecontrol.ErrUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return err
		}
	}
	var rateLimit *MediaSignalRateLimitError
	if errors.As(err, &rateLimit) {
		return err
	}
	return ErrMediaSignalUnavailable
}
