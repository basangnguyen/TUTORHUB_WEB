package identity

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDeviceLabelUsesOnlyCoarseAllowlist(t *testing.T) {
	for _, test := range []struct{ ua, want string }{
		{"Mozilla/5.0 (Windows NT 10.0; Win64) Chrome/130.0 Safari/537.36", "Chrome · Windows"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Version/18.0 Mobile Safari/604.1", "Safari · iOS"},
		{"Mozilla/5.0 (Linux; Android 15) Chrome/130.0 Zalo/25.0", "Zalo in-app · Android"},
		{"Mozilla/5.0 (Windows NT) Chrome/130.0 Safari/537.36 Edg/130.0", "Edge · Windows"},
		{"Mozilla/5.0 (Macintosh) Firefox/131.0", "Firefox · macOS"},
		{"Mozilla/5.0 (X11; CrOS) Chrome/130.0 Safari/537.36", "Chrome · ChromeOS"},
		{"Mozilla/5.0 (iPhone) CriOS/130.0 Safari/604.1", "Chrome · iOS"},
		{"Mozilla/5.0 (Linux; Android) Chrome/130.0 [FBAN/FB4A;FBAV/1]", "Facebook in-app · Android"},
		{"", "Trình duyệt không xác định"},
		{"<script>private-device-id secret-token</script>", "Trình duyệt không xác định"},
	} {
		t.Run(test.want, func(t *testing.T) {
			got := DeviceLabel(test.ua)
			if got != test.want || utf8.RuneCountInString(got) > 80 {
				t.Fatalf("unexpected label %q", got)
			}
		})
	}
	if strings.Contains(DeviceLabel(strings.Repeat("private", 5000)+" Chrome/130.0"), "private") {
		t.Fatal("raw UA escaped allowlist")
	}
}

func TestOIDCCallbackCreatesCoarseDeviceMetadata(t *testing.T) {
	now := time.Now().UTC()
	repository := &memoryRepository{}
	provider := &fakeProvider{}
	service := newTestService(t, repository, provider, now)
	start, err := service.BeginLogin(context.Background(), "/app")
	if err != nil {
		t.Fatal(err)
	}
	provider.claims = validClaims(now, provider.nonce)
	_, err = service.CompleteLogin(context.Background(), CallbackInput{State: provider.state, BrowserBinding: start.BrowserBinding, Code: "code", UserAgent: "Mozilla/5.0 (Windows NT) Chrome/130.0 private-device-id"})
	if err != nil {
		t.Fatal(err)
	}
	if repository.metadata.DeviceLabel != "Chrome · Windows" || len(repository.metadata.UserAgentHash) != 32 {
		t.Fatal("OIDC callback did not persist coarse label and keyed UA hash")
	}
}
