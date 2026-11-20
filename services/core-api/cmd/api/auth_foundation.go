package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nyaruka/phonenumbers"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

// This composition keeps all
// ephemeral state under the API lifetime and shares delivery quotas across replicas.
type authFoundationRuntime struct {
	transactions *identity.TransactionService
	challenges   *identity.ChallengeService
	dispatcher   *identity.DeliveryDispatcher
	inbox        *devinbox.Inbox
	native       interface {
		identity.NativeAuthAPI
		ConfigureRegistrationProviders(*identity.ProviderRegistry)
	}
}

func newAuthFoundation(cfg config.Config, pool *pgxpool.Pool, limiter httpapi.InvitationRateLimiter) (*authFoundationRuntime, error) {
	result := &authFoundationRuntime{}
	auth := cfg.Authentication
	if err := auth.ValidatePhoneRouting(cfg.Environment); err != nil {
		return nil, err
	}
	if auth.EmailSender != "" && auth.EmailSender != "disabled" && auth.EmailSender != "devinbox" && auth.EmailSender != "smtp" {
		return nil, errors.New("selected email delivery adapter is unavailable")
	}
	deliveryEnabled := (auth.EmailSender != "" && auth.EmailSender != "disabled") || (auth.PhoneSender != "" && auth.PhoneSender != "disabled")
	if pool == nil || len(auth.SessionKey) < 32 {
		if deliveryEnabled || auth.SignupEnabled || auth.PasswordEnabled || auth.EmailCodeEnabled || auth.PhoneEnabled || auth.DeviceManagementEnabled || auth.PasskeysEnabled || auth.MFATOTPEnabled {
			return nil, errors.New("native authentication and delivery require database and session cryptography")
		}
		return result, nil
	}
	crypto, err := identity.NewCrypto(auth.SessionKey)
	if err != nil {
		return nil, errors.New("initialize authentication foundation cryptography")
	}
	var email identity.EmailSender = identity.DisabledSender{}
	if auth.EmailSender == "devinbox" || auth.PhoneSender == "devinbox" {
		result.inbox, err = devinbox.New(cfg.Environment, 200, 10*time.Minute)
		if err != nil {
			return nil, err
		}
		if auth.EmailSender == "devinbox" {
			email = identity.NewDevinboxSender(result.inbox, identity.DeliveryEmail)
		}
	}
	phone, err := configuredPhoneSender(cfg.Environment, auth, result.inbox)
	if err != nil {
		result.close()
		return nil, err
	}
	if auth.EmailSender == "smtp" {
		email, err = identity.NewSMTPSender(cfg.Environment, auth.SMTPAddr)
		if err != nil {
			result.close()
			return nil, err
		}
	}
	result.dispatcher, err = identity.NewDeliveryDispatcher(pool, crypto, email, phone, 200, authDeliveryBudget{limiter: limiter})
	if err != nil {
		result.close()
		return nil, err
	}
	result.transactions = identity.NewTransactionService(pool, crypto, auth.CookieSecure)
	result.challenges = identity.NewChallengeService(pool, crypto, result.dispatcher)
	// Password status remains available to authenticated OIDC users even while
	// native sign-in methods are disabled; HTTP gates still control mutations.
	if auth.Enabled || auth.SignupEnabled || auth.PasswordEnabled || auth.EmailCodeEnabled || auth.PhoneEnabled || auth.DeviceManagementEnabled || auth.PasskeysEnabled || auth.MFATOTPEnabled {
		var protocol *identity.PasskeyProtocol
		if auth.PasskeysEnabled {
			if !auth.PasskeysAvailable() {
				result.close()
				return nil, errors.New("invalid passkey relying party configuration")
			}
			protocol, err = identity.NewPasskeyProtocol(auth.WebAuthnRPID, auth.WebAuthnRPName, auth.WebAuthnRPOrigins)
			if err != nil {
				result.close()
				return nil, errors.New("initialize passkey relying party")
			}
		}
		passwords, passwordErr := identity.NewPasswordService(identity.PasswordOptions{
			MinimumLength: auth.PasswordMinLength, MaxConcurrency: auth.PasswordHashMaxConcurrency,
			BreachMode: auth.PasswordBreachCheck, Logger: slog.Default(),
		})
		if passwordErr != nil {
			result.close()
			return nil, passwordErr
		}
		result.native, err = identity.NewNativeAuthService(pool, crypto, passwords, result.dispatcher, identity.NativeAuthConfig{
			PhoneEnabled:          auth.PhoneEnabled,
			PhoneLoginUnavailable: !phoneLoginAvailable(auth),
			PhoneAllowedCountries: auth.PhoneAllowedCountries,
			PhoneStaleAfter:       auth.PhoneStaleAfter,
			PhoneReclaimAfter:     auth.PhoneReclaimAfter,
			PasswordEnabled:       auth.PasswordEnabled,
			EmailCodeLoginEnabled: auth.EmailCodeEnabled,
			MFATOTPEnabled:        auth.MFATOTPEnabled, RequireAdminMFA: auth.RequireAdminMFA,
			OIDCDefaultEnabled: auth.OIDCEnabled && auth.ProviderOIDCDefaultEnabled,
			WebAuthn:           protocol,
			CookieSecure:       auth.CookieSecure, LegalTermsVersion: auth.LegalTermsVersion,
			SessionTTL: auth.SessionTTL, SessionAbsoluteTTL: auth.SessionAbsoluteTTL,
			SessionPersistentAbsoluteTTL: auth.SessionPersistentAbsoluteTTL,
			QueryTimeout:                 cfg.Database.QueryTimeout,
			EmailCodeAvailable:           auth.EmailSender != "" && auth.EmailSender != "disabled",
		})
		if err != nil {
			result.close()
			return nil, err
		}
	}
	return result, nil
}

func configuredPhoneSender(environment string, auth config.AuthenticationConfig, inbox *devinbox.Inbox) (identity.PhoneChallengeSender, error) {
	switch auth.PhoneSender {
	case "", "disabled":
		return identity.DisabledSender{}, nil
	case "devinbox":
		return identity.NewDevinboxPhoneRouter(inbox, auth.PhoneAllowedCountries)
	}

	var candidates []identity.PhoneRoute
	for _, provider := range []string{auth.PhoneSender, auth.PhoneFallbackSender} {
		var sender identity.PhoneChallengeSender
		var err error
		switch provider {
		case "", "disabled":
			continue
		case "zns":
			sender, err = identity.NewZaloZNSSender(environment, auth.ZNS)
		case "sms_brandname":
			sender, err = identity.NewSMSBrandnameSender(environment, auth.SMSBrandname)
		case "twilio":
			sender, err = identity.NewTwilioSMSSender(environment, auth.Twilio, auth.PhoneAllowedCountries)
		default:
			return nil, errors.New("unsupported phone delivery provider")
		}
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, identity.PhoneRoute{Provider: provider, Sender: sender})
	}
	routes := make(map[int][]identity.PhoneRoute)
	for _, country := range auth.PhoneAllowedCountries {
		code := phonenumbers.GetCountryCodeForRegion(country)
		if _, exists := routes[code]; exists {
			continue
		}
		var chain []identity.PhoneRoute
		for _, route := range candidates {
			if route.Sender.Supports(code) {
				chain = append(chain, route)
			}
		}
		routes[code] = chain
	}
	return identity.NewPhoneSenderRouter(routes)
}

func phoneLoginAvailable(auth config.AuthenticationConfig) bool {
	return auth.PhoneEnabled && auth.PhoneSender != "" && auth.PhoneSender != "disabled" && auth.PhoneOTPDailyQuota > 0
}

func configuredIdentityProvider(ctx context.Context, auth config.AuthenticationConfig) (identity.Provider, error) {
	if !auth.OIDCEnabled {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return identity.NewOIDCProvider(ctx, identity.OIDCProviderConfig{
		IssuerURL: auth.IssuerURL, ClientID: auth.ClientID, ClientSecret: auth.ClientSecret,
		CallbackURL: auth.CallbackURL, PostLogoutURL: auth.PostLogoutURL, Scopes: auth.Scopes,
		HTTPTimeout: 10 * time.Second,
	})
}

func (runtime *authFoundationRuntime) close() {
	if runtime.dispatcher != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		_ = runtime.dispatcher.Close(ctx)
	}
	if runtime.inbox != nil {
		runtime.inbox.Close()
	}
}

type authDeliveryBudget struct{ limiter httpapi.InvitationRateLimiter }

func (budget authDeliveryBudget) AllowDelivery(ctx context.Context, channel identity.DeliveryChannel) bool {
	if budget.limiter == nil {
		return false
	}
	purpose := httpapi.AuthRateLimitDeliveryDailyPhone
	if channel == identity.DeliveryEmail {
		purpose = httpapi.AuthRateLimitDeliveryDailyEmail
	}
	decision := budget.limiter.Allow(ctx, purpose, "global", time.Now().UTC())
	return decision.Err == nil && decision.Allowed
}
