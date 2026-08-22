package collaboration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestP511GrantTTLIsHardBoundedAtSixtySeconds(t *testing.T) {
	t.Parallel()

	for _, ttl := range []time.Duration{time.Nanosecond, 61 * time.Second} {
		if _, err := NewMemoryGrantBroker(MemoryGrantBrokerConfig{
			GrantTTL: ttl, ProviderURL: "wss://whiteboard.example.test",
		}); err == nil {
			t.Fatalf("GrantTTL %s accepted outside the supported boundary", ttl)
		}
	}

	broker, access, authority, now := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{
		GrantTTL: 60 * time.Second,
	})
	credential := issueTestGrant(t, broker, access, authority)
	if got := credential.ExpiresAt.Sub(*now); got != 60*time.Second {
		t.Fatalf("credential TTL = %s, want 60s", got)
	}
}

func TestP511ConsumeRevokeRaceNeverLeavesReusableAuthority(t *testing.T) {
	t.Parallel()

	for iteration := 0; iteration < 64; iteration++ {
		broker, access, authority, _ := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{})
		credential := issueTestGrant(t, broker, access, authority)
		var resolution GrantResolution
		var consumeErr error
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			resolution, consumeErr = broker.Consume(context.Background(), GrantConsumeInput{
				Credential:           credential.Credential,
				Origin:               "https://app.example.test",
				ProviderDocumentName: authority.ProviderDocumentName,
			})
		}()
		go func() {
			defer wait.Done()
			<-start
			broker.Revoke(authority.Document.ID)
		}()
		close(start)
		wait.Wait()

		// Establish the final revoked authority regardless of which goroutine won.
		broker.Revoke(authority.Document.ID)
		if consumeErr != nil && !errors.Is(consumeErr, ErrGrantDenied) {
			t.Fatalf("iteration %d: consume error = %v", iteration, consumeErr)
		}
		if consumeErr == nil {
			if _, err := broker.Validate(context.Background(), GrantValidationInput{
				Origin: "https://app.example.test", Scope: resolution.Scope,
			}); !errors.Is(err, ErrGrantDenied) {
				t.Fatalf("iteration %d: raced lease survived revoke: %v", iteration, err)
			}
		}
		if _, err := broker.Consume(context.Background(), GrantConsumeInput{
			Credential:           credential.Credential,
			Origin:               "https://app.example.test",
			ProviderDocumentName: authority.ProviderDocumentName,
		}); !errors.Is(err, ErrGrantDenied) {
			t.Fatalf("iteration %d: credential replay error = %v", iteration, err)
		}
	}
}

func TestP511MalformedCredentialCorpusFailsWithBoundedDenial(t *testing.T) {
	t.Parallel()

	broker, _, authority, _ := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{})
	corpus := []string{
		"",
		"short",
		strings.Repeat("a", 19),
		strings.Repeat("b", 1_025),
		"not-base64-but-long-enough",
		"credential-with-control-\x00-byte",
		"credential-with-unicode-雪-雪-雪",
	}
	for index, credential := range corpus {
		_, err := broker.Consume(context.Background(), GrantConsumeInput{
			Credential:           credential,
			Origin:               "https://app.example.test",
			ProviderDocumentName: authority.ProviderDocumentName,
		})
		if !errors.Is(err, ErrGrantDenied) {
			t.Fatalf("corpus %d: error = %v, want bounded grant denial", index, err)
		}
		if credential != "" && strings.Contains(err.Error(), credential) {
			t.Fatalf("corpus %d: denial echoed credential", index)
		}
	}
}
