package httpapi

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type phoneDestinationLimitError struct{ RetryAfter time.Duration }

func (*phoneDestinationLimitError) Error() string { return "phone verification rate limit reached" }

func phoneConversionTooLow(sent, consumed int64, minimum, percentage int) bool {
	return sent >= int64(minimum) && consumed*100 < sent*int64(percentage)
}

// The guard runs inside challenge issuance. Serializing this short admission
// step includes queued sends in the daily ceiling even across API replicas.
// The dispatcher independently reserves its real delivery slot after commit.
func (h nativeAuthHandlers) phoneSendGuard(ctx context.Context, tx pgx.Tx, number identity.NormalizedPhoneNumber, manage bool) (bool, error) {
	cfg := h.auth.config.Authentication
	if cfg.PhoneSender == "" || cfg.PhoneSender == "disabled" || cfg.PhoneOTPDailyQuota == 0 {
		return false, identity.ErrPhoneDeliveryUnavailable
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tutorhub.auth.phone.send.v1',0))`); err != nil {
		return false, identity.ErrAuthFoundationStorage
	}
	now := h.auth.clock().UTC()
	day := now.Truncate(24 * time.Hour)
	bucket := invitationRateLimitBucketHash(string(AuthRateLimitDeliveryDailyPhone), "global")
	var reserved, dispatched, sent, consumed int64
	err := tx.QueryRow(ctx, `SELECT
	    (SELECT count(*) FROM tutorhub.auth_challenges WHERE deliverable AND phone_country_code IS NOT NULL AND created_at >= $1),
	    COALESCE((SELECT used_count FROM tutorhub.rate_limit_windows WHERE purpose=$2 AND bucket_hash=$3 AND window_started_at=$1),0),
	    count(*), count(consumed_at)
	    FROM tutorhub.auth_challenges WHERE deliverable AND phone_country_code=$4 AND created_at >= $5`,
		day, string(AuthRateLimitDeliveryDailyPhone), bucket[:], number.CountryCode, now.Add(-time.Hour)).Scan(&reserved, &dispatched, &sent, &consumed)
	if err != nil {
		return false, identity.ErrAuthFoundationStorage
	}
	if max(reserved, dispatched) >= int64(cfg.PhoneOTPDailyQuota) || phoneConversionTooLow(sent, consumed, cfg.PhoneBreakerMinSends, cfg.PhoneBreakerMinConversionPct) {
		return false, identity.ErrPhoneDeliveryUnavailable
	}
	limiter := postgresInvitationRateLimiter{database: tx, queryTimeout: h.auth.config.Database.QueryTimeout, policies: map[InvitationRateLimitAction]invitationRateLimitPolicy{
		AuthRateLimitOTPSendCountry:     {Limit: cfg.PhoneCountryHourlyQuota, Window: time.Hour},
		AuthRateLimitOTPSendDestination: {Limit: 5, Window: time.Hour},
	}}
	country := limiter.Allow(ctx, AuthRateLimitOTPSendCountry, number.CountryCode, now)
	if country.Err != nil {
		return false, identity.ErrAuthFoundationStorage
	}
	if !country.Allowed {
		return false, identity.ErrPhoneDeliveryUnavailable
	}
	destination := limiter.Allow(ctx, AuthRateLimitOTPSendDestination, number.E164, now)
	if destination.Err != nil {
		return false, identity.ErrAuthFoundationStorage
	}
	if !destination.Allowed && manage {
		return false, &phoneDestinationLimitError{RetryAfter: destination.RetryAfter}
	}
	return destination.Allowed, nil
}
