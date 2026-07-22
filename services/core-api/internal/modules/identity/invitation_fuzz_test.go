package identity

import (
	"encoding/base64"
	"strings"
	"testing"
)

func FuzzNormalizeMembershipInvitationToken(f *testing.F) {
	valid := membershipInvitationTokenPrefix +
		base64.RawURLEncoding.EncodeToString(make([]byte, randomTokenBytes))
	f.Add("")
	f.Add("wrong_" + valid)
	f.Add(membershipInvitationTokenPrefix)
	f.Add(membershipInvitationTokenPrefix + "short")
	f.Add(valid + "=")
	f.Add("  " + valid + "  ")

	f.Fuzz(func(t *testing.T, value string) {
		normalized, err := normalizeMembershipInvitationToken(value)
		if err != nil {
			return
		}
		if normalized != strings.TrimSpace(value) ||
			!strings.HasPrefix(normalized, membershipInvitationTokenPrefix) {
			t.Fatalf("accepted non-canonical membership token: input=%q normalized=%q", value, normalized)
		}
	})
}
