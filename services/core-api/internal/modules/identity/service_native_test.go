package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionServiceWorksWithoutFederation(t *testing.T) {
	repository := &memoryRepository{}
	service := newTestService(t, repository, nil, time.Now().UTC())
	if _, err := service.BeginLogin(context.Background(), "/app/home"); !errors.Is(err, ErrAuthenticationDisabled) {
		t.Fatal("native-only session service must reject OIDC starts")
	}
	if _, err := service.CompleteLogin(context.Background(), CallbackInput{State: "state", BrowserBinding: "binding", Code: "code"}); !errors.Is(err, ErrAuthenticationDisabled) {
		t.Fatal("native-only session service must reject OIDC callbacks")
	}
	repository.metadata.TokenHash = service.crypto.Digest(sessionPurpose, "native-session")
	redirect, err := service.Logout(context.Background(), "native-session")
	if err != nil || redirect != "" || !repository.revoked {
		t.Fatal("native logout must revoke its session without provider access")
	}
}
