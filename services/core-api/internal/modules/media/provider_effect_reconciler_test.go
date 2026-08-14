package media

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDurableProviderEffectReconcilerConcurrentClaimsHaveSingleWinner(t *testing.T) {
	t.Parallel()

	ref := DurableProviderEffectRef{
		TenantID: uuid.New(), OriginalActorID: uuid.New(), SpaceID: uuid.New(),
		IdempotencyKey: "p407-durable-remove-0001",
		Operation:      string(ModerationRemove), Attempt: 2,
	}
	repository := &fakeDurableProviderEffectRepository{effect: DurableProviderEffect{
		Ref: ref, RoomName: "r_0123456789abcdef0123456789abcdef",
		ParticipantIdentity: "p_0123456789abcdef0123456789abcdef",
	}}
	provider := &fakeDurableProvider{}
	reconciler, err := NewDurableProviderEffectReconciler(
		repository, provider, provider, func() time.Time { return mediaTestTime },
	)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	errorsSeen := make(chan error, 2)
	for range 2 {
		go func() {
			defer wait.Done()
			<-start
			_, reconcileErr := reconciler.ReconcileOnce(context.Background())
			errorsSeen <- reconcileErr
		}()
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	for reconcileErr := range errorsSeen {
		if reconcileErr != nil {
			t.Fatalf("concurrent reconcile: %v", reconcileErr)
		}
	}
	if provider.removeCalls != 1 || repository.claimWins != 1 ||
		repository.completeCalls != 1 {
		t.Fatalf("single-winner counts: provider=%d claim=%d complete=%d",
			provider.removeCalls, repository.claimWins, repository.completeCalls)
	}
	if repository.completedRef != ref || repository.completedStatus != ProviderEffectApplied {
		t.Fatalf("completion lost immutable original receipt attribution: %+v", repository)
	}
}

func TestDurableProviderEffectReconcilerPersistsRetryableFailureWithoutActorSession(t *testing.T) {
	t.Parallel()

	repository := &fakeDurableProviderEffectRepository{effect: DurableProviderEffect{
		Ref: DurableProviderEffectRef{
			TenantID: uuid.New(), OriginalActorID: uuid.New(), SpaceID: uuid.New(),
			IdempotencyKey: "p407-durable-mute-000001",
			Operation:      string(ModerationMute), Attempt: 3,
		},
		RoomName:            "r_0123456789abcdef0123456789abcdef",
		ParticipantIdentity: "p_0123456789abcdef0123456789abcdef",
	}}
	provider := &fakeDurableProvider{muteErr: errors.New("sensitive provider detail")}
	reconciler, err := NewDurableProviderEffectReconciler(
		repository, provider, provider, func() time.Time { return mediaTestTime },
	)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := reconciler.ReconcileOnce(context.Background())
	if err != nil || !claimed {
		t.Fatalf("reconcile retryable effect: claimed=%t err=%v", claimed, err)
	}
	if repository.completedStatus != ProviderEffectRetryableFailed ||
		repository.completedErrorCode != "provider_unavailable" {
		t.Fatalf("retryable completion=%s/%q", repository.completedStatus,
			repository.completedErrorCode)
	}
}

func TestPostgresDurableProviderEffectClaimIsTrustedBoundedAndSingleWinner(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("postgres_provider_effect_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	for _, required := range []string{
		"ORDER BY provider_effect_updated_at NULLS FIRST, created_at",
		"FOR UPDATE SKIP LOCKED", "LIMIT 1",
		"receipt.tenant_id = candidate.tenant_id",
		"receipt.actor_user_id = candidate.actor_user_id",
		"receipt.idempotency_key = candidate.idempotency_key",
		"provider_effect_status = 'applying'",
		"provider_effect_attempts = $6",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("durable claim/CAS is missing %q", required)
		}
	}
	claimAt := strings.Index(source, "func (repository *PostgresProviderEffectRepository) ClaimNextProviderEffect(")
	completeAt := strings.Index(source, "func (repository *PostgresProviderEffectRepository) CompleteProviderEffect(")
	if claimAt < 0 || completeAt < 0 || claimAt >= completeAt ||
		strings.Contains(source[claimAt:completeAt], "AccessContext") ||
		strings.Contains(source[claimAt:completeAt], "requireActiveScope") {
		t.Fatal("trusted durable claim still depends on an interactive actor scope")
	}
}

func TestDurableProviderCompletionErrorCodeAllowlistIsExact(t *testing.T) {
	t.Parallel()

	valid := []struct {
		status ProviderEffectStatus
		code   string
	}{
		{ProviderEffectApplied, ""},
		{ProviderEffectRetryableFailed, "provider_unavailable"},
		{ProviderEffectPermanentFailed, "provider_invalid_response"},
	}
	for _, value := range valid {
		if !validDurableProviderCompletion(value.status, value.code) {
			t.Errorf("valid durable completion rejected: %s/%q", value.status, value.code)
		}
	}
	for _, invalid := range []struct {
		status ProviderEffectStatus
		code   string
	}{
		{ProviderEffectApplied, "provider_unavailable"},
		{ProviderEffectRetryableFailed, "raw_sensitive_provider_error"},
		{ProviderEffectPermanentFailed, "provider_unavailable"},
		{ProviderEffectPending, ""},
	} {
		if validDurableProviderCompletion(invalid.status, invalid.code) {
			t.Errorf("invalid durable completion accepted: %s/%q", invalid.status, invalid.code)
		}
	}
}

type fakeDurableProviderEffectRepository struct {
	mu                 sync.Mutex
	effect             DurableProviderEffect
	claimed            bool
	claimWins          int
	completeCalls      int
	completedRef       DurableProviderEffectRef
	completedStatus    ProviderEffectStatus
	completedErrorCode string
}

func (repository *fakeDurableProviderEffectRepository) ClaimNextProviderEffect(
	context.Context,
	time.Time,
	time.Duration,
) (DurableProviderEffect, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.claimed {
		return DurableProviderEffect{}, false, nil
	}
	repository.claimed = true
	repository.claimWins++
	return repository.effect, true, nil
}

func (repository *fakeDurableProviderEffectRepository) CompleteProviderEffect(
	_ context.Context,
	ref DurableProviderEffectRef,
	status ProviderEffectStatus,
	errorCode string,
	_ time.Time,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.completeCalls++
	repository.completedRef = ref
	repository.completedStatus = status
	repository.completedErrorCode = errorCode
	return nil
}

type fakeDurableProvider struct {
	mu          sync.Mutex
	deleteCalls int
	muteCalls   int
	removeCalls int
	muteErr     error
}

func (provider *fakeDurableProvider) EnsureRoom(context.Context, string) (ProviderRoom, error) {
	return ProviderRoom{}, errors.New("unexpected ensure")
}

func (provider *fakeDurableProvider) DeleteRoom(context.Context, string) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.deleteCalls++
	return nil
}

func (provider *fakeDurableProvider) MuteParticipantMicrophone(context.Context, string, string) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.muteCalls++
	return provider.muteErr
}

func (provider *fakeDurableProvider) RemoveParticipant(context.Context, string, string) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.removeCalls++
	return nil
}
