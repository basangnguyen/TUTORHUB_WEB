package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type federationRepositoryTestDB struct {
	tx       *federationRepositoryTestTx
	err      error
	deadline time.Time
}

func (db *federationRepositoryTestDB) Begin(ctx context.Context) (pgx.Tx, error) {
	db.deadline, _ = ctx.Deadline()
	return db.tx, db.err
}

type federationRepositoryTestTx struct {
	pgx.Tx
	row pgx.Row
}

func (tx *federationRepositoryTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return tx.row
}

func TestFederationRepositoryRejectsUnsafeInputBeforeStorage(t *testing.T) {
	repository := NewPostgresRepository(nil, time.Second, nil)
	now := time.Now().UTC()
	if _, err := repository.CompleteFederatedLogin(context.Background(), FederationLoginParams{}); !errors.Is(err, ErrVerifiedEmailRequired) {
		t.Fatal("unverified identity accepted")
	}
	if _, err := repository.CompleteFederatedLogin(context.Background(), FederationLoginParams{Claims: ProviderClaims{EmailVerified: true}}); !errors.Is(err, ErrInvalidAuthFlow) {
		t.Fatal("missing provider identity accepted")
	}
	if _, err := repository.ConfirmLinkIntent(context.Background(), LinkIntentConfirmParams{Principal: Principal{User: User{ID: uuid.New()}, SessionID: uuid.New()}, Intent: StoredLinkIntent{UserID: uuid.New()}}); !errors.Is(err, ErrLinkIntentMismatch) {
		t.Fatal("wrong account reached database")
	}
	if _, err := repository.ReauthenticateFederated(context.Background(), FederationReauthParams{Flow: StoredFlow{Purpose: "reauth", CreatedAt: now}, Claims: ProviderClaims{EmailVerified: true, AuthTime: now.Add(-time.Hour)}, Rotation: SessionRotation{RotatedAt: now}}); !errors.Is(err, ErrFederationReauthRequired) {
		t.Fatal("stale provider proof reached database")
	}
}

func TestFederationRepositoryStorageFailuresAreRedacted(t *testing.T) {
	db := &federationRepositoryTestDB{err: errors.New("private@example.test authorization-code-private")}
	repository := NewPostgresRepository(db, time.Second, nil)
	now := time.Now().UTC()
	userID := uuid.New()
	calls := []func() error{
		func() error {
			_, err := repository.ReadLinkIntent(context.Background(), []byte("binding"), now)
			return err
		},
		func() error { return repository.CancelLinkIntent(context.Background(), []byte("binding"), now) },
		func() error {
			_, err := repository.CompleteFederatedLogin(context.Background(), FederationLoginParams{ProviderSlug: "google", Claims: ProviderClaims{Issuer: "issuer", Subject: "subject", EmailVerified: true}})
			return err
		},
		func() error {
			_, err := repository.ConfirmLinkIntent(context.Background(), LinkIntentConfirmParams{Principal: Principal{User: User{ID: userID}, SessionID: uuid.New()}, Intent: StoredLinkIntent{UserID: userID}, Payload: FederationIdentityPayload{EmailVerified: true}})
			return err
		},
		func() error {
			_, err := repository.ReauthenticateFederated(context.Background(), FederationReauthParams{Flow: StoredFlow{Purpose: "reauth", CreatedAt: now}, Claims: ProviderClaims{EmailVerified: true, AuthTime: now}, Rotation: SessionRotation{RotatedAt: now}})
			return err
		},
	}
	for _, call := range calls {
		if err := call(); !errors.Is(err, ErrAuthFoundationStorage) || strings.Contains(err.Error(), "private") {
			t.Fatal("storage error leaked provider data")
		}
		if db.deadline.IsZero() || time.Until(db.deadline) > time.Second {
			t.Fatal("configured repository timeout not used")
		}
	}
}

func TestFederationAccountAssuranceUsesStoredMaximum(t *testing.T) {
	tx := &federationRepositoryTestTx{row: foundationTestRow{values: []any{int16(2)}}}
	if err := requireFederationAccountAssurance(context.Background(), tx, uuid.New(), 1, time.Now()); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("weak session accepted for stronger account")
	}
	if err := requireFederationAccountAssurance(context.Background(), tx, uuid.New(), 2, time.Now()); err != nil {
		t.Fatal("sufficient assurance rejected")
	}
}
