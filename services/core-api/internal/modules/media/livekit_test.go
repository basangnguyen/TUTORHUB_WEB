package media

import (
	"testing"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
)

func TestLiveKitTokenIssuerSignsExplicitLeastPrivilegeGrant(t *testing.T) {
	t.Parallel()

	issuer, err := NewLiveKitTokenIssuer("test-api-key", "test-api-secret-long-enough")
	if err != nil {
		t.Fatalf("create issuer: %v", err)
	}
	token, err := issuer.Issue(TokenGrant{
		RoomName: "room", ParticipantIdentity: "participant", ParticipantName: "Teacher",
		Role: "teacher", CanPublish: true, CanPublishData: false, CanSubscribe: true,
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
		claims.Name != "Teacher" || claims.Attributes["tutorhub.role"] != "teacher" {
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
