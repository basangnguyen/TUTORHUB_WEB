package classroom

import (
	"encoding/base64"
	"strings"
	"testing"
)

func FuzzNormalizeClassInviteCodeToken(f *testing.F) {
	validRandom := base64.RawURLEncoding.EncodeToString(make([]byte, classInviteCodeTokenBytes))
	valid := classInviteCodeTokenPrefix + validRandom
	f.Add("")
	f.Add("wrong_" + valid)
	f.Add(classInviteCodeTokenPrefix)
	f.Add(classInviteCodeTokenPrefix + "short")
	f.Add(valid + "=")
	f.Add("  " + valid + "  ")

	f.Fuzz(func(t *testing.T, value string) {
		normalized, err := normalizeClassInviteCodeToken(value)
		if err != nil {
			return
		}
		if normalized != strings.TrimSpace(value) ||
			!strings.HasPrefix(normalized, classInviteCodeTokenPrefix) {
			t.Fatalf("accepted non-canonical class invite token: input=%q normalized=%q", value, normalized)
		}
	})
}
