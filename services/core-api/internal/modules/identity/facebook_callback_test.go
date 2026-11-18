package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func signFacebookFixture(raw, key string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(raw))
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) + "." + payload
}

func TestFacebookSignedRequestVerification(t *testing.T) {
	const valid = `{"algorithm":"HMAC-SHA256","user_id":"987654321","issued_at":1700000000}`
	const key = "test-fixture-signing-key"
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, body   string
		fresh, valid bool
	}{
		{"fresh", valid, true, true}, {"no timestamp", `{"user_id":"123","algorithm":"HMAC-SHA256"}`, true, true},
		{"expired", strings.Replace(valid, "1700000000", "1600000000", 1), false, true},
		{"expired claim", strings.Replace(valid, "}", `,"expires":1699999999}`, 1), false, true},
		{"future", strings.Replace(valid, "1700000000", "1800000000", 1), false, false},
		{"wrong algorithm", strings.Replace(valid, "SHA256", "SHA1", 1), false, false},
		{"missing algorithm", `{"user_id":"123"}`, false, false}, {"missing subject", `{"algorithm":"HMAC-SHA256"}`, false, false},
		{"empty subject", strings.Replace(valid, "987654321", "", 1), false, false},
		{"duplicate field", strings.Replace(valid, "}", `,"user_id":"555"}`, 1), false, false},
		{"invalid timestamp", strings.Replace(valid, "1700000000", "null", 1), false, false},
		{"bad json", valid + "{}", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signed := signFacebookFixture(tc.body, key)
			subject, payload, fresh, err := verifyFacebookSignedRequest(signed, key, now)
			if (err == nil) != tc.valid || fresh != tc.fresh {
				t.Fatalf("accepted=%v, fresh=%v", err == nil, fresh)
			}
			if tc.valid && (subject == "" || !strings.HasSuffix(signed, "."+payload)) {
				t.Fatal("event payload or subject lost")
			}
		})
	}
	for _, signed := range []string{"", signFacebookFixture(valid, "another-key"), signFacebookFixture(valid, key) + ".", strings.Repeat("x", 65537)} {
		if _, _, _, err := verifyFacebookSignedRequest(signed, key, now); err != ErrFacebookSignedRequest {
			t.Fatal("invalid request accepted")
		}
	}
}
