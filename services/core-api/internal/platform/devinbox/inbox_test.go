package devinbox

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestInboxEnvironmentCapacityExpiryAndPrivacy(t *testing.T) {
	for _, environment := range []string{"staging", "production", "local", ""} {
		if inbox, err := New(environment, 2, time.Minute); err == nil || inbox != nil {
			t.Fatalf("unsafe environment accepted: %s", environment)
		}
	}
	inbox, err := New("test", 2, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	for _, id := range []string{"first", "second", "third"} {
		if err := inbox.Add(Message{ChallengeID: id, Destination: "private@example.test", Code: "420381", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	items := inbox.List()
	if len(items) != 2 || items[0].ChallengeID != "second" || items[1].ChallengeID != "third" {
		t.Fatal("capacity did not evict oldest message")
	}
	items[0].Code = "000000"
	if inbox.List()[0].Code != "420381" {
		t.Fatal("list exposes mutable storage")
	}
	var output bytes.Buffer
	slog.New(slog.NewJSONHandler(&output, nil)).Info("inbox", "message", items[1])
	if strings.Contains(output.String(), "private@example.test") || strings.Contains(output.String(), "420381") {
		t.Fatal("inbox message leaked through structured logger")
	}
	remaining := 2
	deadline := time.Now().Add(time.Second)
	for remaining > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		inbox.mu.Lock()
		remaining = len(inbox.messages)
		inbox.mu.Unlock()
	}
	if remaining != 0 {
		t.Fatal("automatic TTL cleanup did not remove expired messages")
	}
	inbox.Close()
	if err := inbox.Add(Message{ExpiresAt: time.Now().Add(time.Minute)}); err == nil {
		t.Fatal("closed inbox accepted a message")
	}
}
