package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

// Method-specific handlers are added in AUTH-02+. This composition keeps all
// ephemeral state under the API lifetime and shares delivery quotas across replicas.
type authFoundationRuntime struct {
	transactions *identity.TransactionService
	challenges   *identity.ChallengeService
	dispatcher   *identity.DeliveryDispatcher
	inbox        *devinbox.Inbox
}

func newAuthFoundation(cfg config.Config, pool *pgxpool.Pool, limiter httpapi.InvitationRateLimiter) (*authFoundationRuntime, error) {
	result := &authFoundationRuntime{}
	auth := cfg.Authentication
	for _, sender := range []string{auth.EmailSender, auth.PhoneSender} {
		if sender != "" && sender != "disabled" && sender != "devinbox" && sender != "smtp" {
			return nil, errors.New("selected authentication delivery adapter is not implemented in AUTH-01")
		}
	}
	deliveryEnabled := (auth.EmailSender != "" && auth.EmailSender != "disabled") || (auth.PhoneSender != "" && auth.PhoneSender != "disabled")
	if pool == nil || len(auth.SessionKey) < 32 {
		if deliveryEnabled {
			return nil, errors.New("authentication delivery requires database and session cryptography")
		}
		return result, nil
	}
	crypto, err := identity.NewCrypto(auth.SessionKey)
	if err != nil {
		return nil, errors.New("initialize authentication foundation cryptography")
	}
	var email identity.EmailSender = identity.DisabledSender{}
	var phone identity.PhoneChallengeSender = identity.DisabledSender{}
	if auth.EmailSender == "devinbox" || auth.PhoneSender == "devinbox" {
		result.inbox, err = devinbox.New(cfg.Environment, 200, 10*time.Minute)
		if err != nil {
			return nil, err
		}
		if auth.EmailSender == "devinbox" {
			email = identity.NewDevinboxSender(result.inbox, identity.DeliveryEmail)
		}
		if auth.PhoneSender == "devinbox" {
			phone = identity.NewDevinboxSender(result.inbox, identity.DeliverySMS)
		}
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
	return result, nil
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
