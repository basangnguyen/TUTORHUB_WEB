package identity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPrincipalAssuranceRemainsInternal(t *testing.T) {
	principal := Principal{AuthenticatedAt: time.Now(), AssuranceLevel: 2, AuthMethods: []string{"pwd", "totp"}}
	encoded, err := json.Marshal(principal)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"AssuranceLevel", "AuthMethods", "AuthenticatedAt", "assurance_level", "auth_methods", "totp", "pwd"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("principal exposes internal authentication metadata")
		}
	}
}
