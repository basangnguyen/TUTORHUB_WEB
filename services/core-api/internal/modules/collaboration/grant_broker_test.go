package collaboration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemoryGrantBrokerOneTimeExchangeAndExactLeaseValidation(t *testing.T) {
	t.Parallel()
	broker, access, authority, now := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{})
	credential := issueTestGrant(t, broker, access, authority)
	resolution, err := broker.Consume(context.Background(), GrantConsumeInput{
		Credential: credential.Credential, Origin: "https://app.example.test",
		ProviderDocumentName: authority.ProviderDocumentName,
	})
	if err != nil {
		t.Fatalf("consume grant: %v", err)
	}
	if resolution.Scope.AuthorityLease == credential.Credential ||
		resolution.Scope.ActorID != access.ActorID || resolution.Scope.TenantID != access.TenantID ||
		resolution.Scope.SessionID != access.SessionID || resolution.Scope.Capability != CapabilityEdit ||
		resolution.Scope.DocumentID != authority.Document.ID ||
		resolution.Scope.Generation != authority.Document.CurrentGeneration ||
		resolution.Scope.WriterFence != authority.WriterFence {
		t.Fatalf("unexpected exact scope: %+v", resolution.Scope)
	}
	if _, err := broker.Consume(context.Background(), GrantConsumeInput{
		Credential: credential.Credential, Origin: "https://app.example.test",
		ProviderDocumentName: authority.ProviderDocumentName,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("replay error = %v, want grant denied", err)
	}
	if _, err := broker.Validate(context.Background(), GrantValidationInput{
		Origin: "https://app.example.test", Scope: resolution.Scope,
	}); err != nil {
		t.Fatalf("validate current lease: %v", err)
	}

	*now = now.Add(3 * time.Minute)
	if _, err := broker.Validate(context.Background(), GrantValidationInput{
		Origin: "https://app.example.test", Scope: resolution.Scope,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("expired lease error = %v, want grant denied", err)
	}
}

func TestMemoryGrantBrokerRejectsWrongBindingAndConsumesAttempt(t *testing.T) {
	t.Parallel()
	broker, access, authority, _ := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{})
	credential := issueTestGrant(t, broker, access, authority)
	if _, err := broker.Consume(context.Background(), GrantConsumeInput{
		Credential: credential.Credential, Origin: "https://evil.example.test",
		ProviderDocumentName: authority.ProviderDocumentName,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("wrong-origin error = %v, want grant denied", err)
	}
	if _, err := broker.Consume(context.Background(), GrantConsumeInput{
		Credential: credential.Credential, Origin: "https://app.example.test",
		ProviderDocumentName: authority.ProviderDocumentName,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("retry after wrong binding = %v, want consumed grant", err)
	}
}

func TestMemoryGrantBrokerRejectsEveryForgedLeaseBinding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*GrantValidationInput)
	}{
		{name: "origin", mutate: func(input *GrantValidationInput) {
			input.Origin = "https://evil.example.test"
		}},
		{name: "actor", mutate: func(input *GrantValidationInput) {
			input.Scope.ActorID = uuid.New()
		}},
		{name: "tenant", mutate: func(input *GrantValidationInput) {
			input.Scope.TenantID = uuid.New()
		}},
		{name: "session", mutate: func(input *GrantValidationInput) {
			input.Scope.SessionID = uuid.New()
		}},
		{name: "document", mutate: func(input *GrantValidationInput) {
			input.Scope.DocumentID = uuid.New()
		}},
		{name: "provider document", mutate: func(input *GrantValidationInput) {
			input.Scope.ProviderDocumentName = "wb_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{name: "generation", mutate: func(input *GrantValidationInput) {
			input.Scope.Generation++
		}},
		{name: "writer fence", mutate: func(input *GrantValidationInput) {
			input.Scope.WriterFence++
		}},
		{name: "capability", mutate: func(input *GrantValidationInput) {
			input.Scope.Capability = CapabilityPresent
		}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			broker, access, authority, _ := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{})
			credential := issueTestGrant(t, broker, access, authority)
			resolution, err := broker.Consume(context.Background(), GrantConsumeInput{
				Credential: credential.Credential, Origin: "https://app.example.test",
				ProviderDocumentName: authority.ProviderDocumentName,
			})
			if err != nil {
				t.Fatalf("consume grant: %v", err)
			}
			input := GrantValidationInput{
				Origin: "https://app.example.test", Scope: resolution.Scope,
			}
			test.mutate(&input)
			if _, err := broker.Validate(context.Background(), input); !errors.Is(err, ErrGrantDenied) {
				t.Fatalf("forged binding error = %v, want grant denied", err)
			}
		})
	}
}

func TestMemoryGrantBrokerRejectsExpiredGrantAndBoundsRate(t *testing.T) {
	t.Parallel()
	broker, access, authority, now := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{
		GrantTTL: time.Second, RateLimit: 2,
	})
	credential := issueTestGrant(t, broker, access, authority)
	*now = now.Add(time.Second)
	if _, err := broker.Consume(context.Background(), GrantConsumeInput{
		Credential: credential.Credential, Origin: "https://app.example.test",
		ProviderDocumentName: authority.ProviderDocumentName,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("expired grant error = %v, want grant denied", err)
	}
	*now = now.Add(time.Second)
	issueTestGrant(t, broker, access, authority)
	if _, err := broker.Issue(context.Background(), access, authority, CapabilityEdit, GrantExchangeInput{
		Origin: "https://app.example.test",
	}); !errors.Is(err, ErrGrantRateLimited) {
		t.Fatalf("rate-limit error = %v, want rate limited", err)
	}
}

func TestMemoryGrantBrokerConcurrentConsumeHasSingleWinner(t *testing.T) {
	t.Parallel()
	broker, access, authority, _ := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{})
	credential := issueTestGrant(t, broker, access, authority)
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := broker.Consume(context.Background(), GrantConsumeInput{
				Credential: credential.Credential, Origin: "https://app.example.test",
				ProviderDocumentName: authority.ProviderDocumentName,
			})
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrGrantDenied) {
				t.Errorf("unexpected consume error: %v", err)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful consumes = %d, want 1", successes.Load())
	}
}

func TestMemoryGrantBrokerRevokePurgesPendingAndActiveAuthority(t *testing.T) {
	t.Parallel()
	broker, access, authority, _ := newGrantBrokerFixture(t, MemoryGrantBrokerConfig{})
	pending := issueTestGrant(t, broker, access, authority)
	active := issueTestGrant(t, broker, access, authority)
	resolution, err := broker.Consume(context.Background(), GrantConsumeInput{
		Credential: active.Credential, Origin: "https://app.example.test",
		ProviderDocumentName: authority.ProviderDocumentName,
	})
	if err != nil {
		t.Fatalf("consume active grant: %v", err)
	}
	broker.Revoke(authority.Document.ID)
	if _, err := broker.Consume(context.Background(), GrantConsumeInput{
		Credential: pending.Credential, Origin: "https://app.example.test",
		ProviderDocumentName: authority.ProviderDocumentName,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("revoked pending grant error = %v", err)
	}
	if _, err := broker.Validate(context.Background(), GrantValidationInput{
		Origin: "https://app.example.test", Scope: resolution.Scope,
	}); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("revoked active lease error = %v", err)
	}
}

func newGrantBrokerFixture(
	t *testing.T,
	config MemoryGrantBrokerConfig,
) (*MemoryGrantBroker, AccessContext, GrantAuthority, *time.Time) {
	t.Helper()
	now := collaborationTestTime
	config.Clock = func() time.Time { return now }
	config.ProviderURL = "wss://whiteboard.example.test"
	broker, err := NewMemoryGrantBroker(config)
	if err != nil {
		t.Fatalf("new grant broker: %v", err)
	}
	access := testAccess()
	documentID, spaceID := uuid.New(), uuid.New()
	authority := GrantAuthority{
		Document: Document{
			ID: documentID, MediaSpaceID: spaceID, Status: DocumentOpen,
			CurrentGeneration: 3, RevokeGeneration: 5,
		},
		ProviderDocumentName: opaqueProviderDocumentName(documentID),
		WriterFence:          5,
	}
	return broker, access, authority, &now
}

func issueTestGrant(
	t *testing.T,
	broker *MemoryGrantBroker,
	access AccessContext,
	authority GrantAuthority,
) GrantCredential {
	t.Helper()
	credential, err := broker.Issue(
		context.Background(), access, authority, CapabilityEdit,
		GrantExchangeInput{Origin: "https://app.example.test"},
	)
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}
	return credential
}
