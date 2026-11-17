package httpapi

import (
	"errors"
	"time"
)

// Auth purposes share the atomic PostgreSQL limiter with invitations, but never
// share a bucket or quota with another purpose.
const (
	AuthRateLimitDiscoveryIP        InvitationRateLimitAction = "auth.discovery.ip"
	AuthRateLimitOTPSendDestination InvitationRateLimitAction = "auth.otp.send.destination"
	AuthRateLimitOTPSendIP          InvitationRateLimitAction = "auth.otp.send.ip"
	AuthRateLimitOTPVerifyIP        InvitationRateLimitAction = "auth.otp.verify.ip"
	AuthRateLimitDeliveryDailyEmail InvitationRateLimitAction = "auth.delivery.daily.email"
	AuthRateLimitDeliveryDailyPhone InvitationRateLimitAction = "auth.delivery.daily.phone"
)

func authRateLimitPolicies(emailQuota, phoneQuota int) map[InvitationRateLimitAction]invitationRateLimitPolicy {
	return map[InvitationRateLimitAction]invitationRateLimitPolicy{
		AuthRateLimitDiscoveryIP:        {Limit: 30, Window: time.Minute},
		AuthRateLimitOTPSendDestination: {Limit: 5, Window: time.Hour},
		AuthRateLimitOTPSendIP:          {Limit: 20, Window: time.Hour},
		AuthRateLimitOTPVerifyIP:        {Limit: 50, Window: 15 * time.Minute},
		AuthRateLimitDeliveryDailyEmail: {Limit: emailQuota, Window: 24 * time.Hour},
		AuthRateLimitDeliveryDailyPhone: {Limit: phoneQuota, Window: 24 * time.Hour},
	}
}

func NewPostgresAuthRateLimiter(database invitationRateLimitDatabase, queryTimeout time.Duration, emailQuota, phoneQuota int) (InvitationRateLimiter, error) {
	if database == nil || queryTimeout <= 0 || emailQuota < 0 || phoneQuota < 0 {
		return nil, errors.New("auth rate limiter requires a database, timeout and nonnegative quotas")
	}
	return &postgresInvitationRateLimiter{database: database, queryTimeout: queryTimeout, policies: authRateLimitPolicies(emailQuota, phoneQuota)}, nil
}
