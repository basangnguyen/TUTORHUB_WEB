package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The browser only receives a binding after the same transaction that checked
// the provider identity and created the challenge commits.
func (repository *PostgresRepository) beginFederatedMFA(ctx context.Context, tx pgx.Tx, userID, identityID uuid.UUID, provider, returnTo string, school schoolLoginContext, realm *schoolRealm) (*MFAChallenge, error) {
	if repository.mfa == nil {
		return nil, nil
	}
	return repository.mfa.beginMFA(ctx, tx, mfaPayload{
		UserID: userID, IdentityID: identityID, Provider: provider, ReturnTo: returnTo, Methods: []string{"fed"}, School: school,
	}, realm != nil && realm.Required)
}

// A provider may have been disabled or the identity erased between callback
// and second-factor proof. The caller holds the user lock until session creation.
func (service *NativeAuthService) validateMFAFederatedIdentity(ctx context.Context, tx pgx.Tx, userID, identityID uuid.UUID, provider string) error {
	if provider == "oidc" && !service.config.OIDCDefaultEnabled {
		return ErrAuthTransactionUnavailable
	}
	entry, err := service.registrationProviders.lookup(provider)
	if err != nil {
		return ErrAuthTransactionUnavailable
	}
	var issuer, slug string
	err = tx.QueryRow(ctx, `SELECT provider,COALESCE(provider_slug,'oidc') FROM tutorhub.identities
		WHERE id=$1 AND user_id=$2 AND status='active' AND kind IN ('oidc','oauth2') FOR UPDATE`, identityID, userID).Scan(&issuer, &slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthTransactionUnavailable
	}
	if err != nil {
		return ErrAuthFoundationStorage
	}
	// The legacy OIDC route may resolve an existing Google identity at the
	// same issuer. resolveIdentity excludes Facebook and Microsoft on that route.
	if !entry.matchesIssuer(issuer) || (slug != provider && !(provider == "oidc" && slug == "google")) {
		return ErrAuthTransactionUnavailable
	}
	return nil
}
