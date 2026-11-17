package httpapi

import (
	"errors"
	"time"
)

// Auth purposes share the atomic PostgreSQL limiter with invitations, but never
// share a bucket or quota with another purpose.
const (
	AuthRateLimitDiscoveryIP        InvitationRateLimitAction = "auth.discovery.ip"
	AuthRateLimitSignupIP           InvitationRateLimitAction = "auth.signup.ip"
	AuthRateLimitPasswordFailIP     InvitationRateLimitAction = "auth.password.fail.ip"
	AuthRateLimitOTPSendDestination InvitationRateLimitAction = "auth.otp.send.destination"
	AuthRateLimitOTPSendIP          InvitationRateLimitAction = "auth.otp.send.ip"
	AuthRateLimitOTPVerifyIP        InvitationRateLimitAction = "auth.otp.verify.ip"
	AuthRateLimitDeliveryDailyEmail InvitationRateLimitAction = "auth.delivery.daily.email"
	AuthRateLimitDeliveryDailyPhone InvitationRateLimitAction = "auth.delivery.daily.phone"
	AuthRateLimitReauthUser         InvitationRateLimitAction = "auth.reauth.user"
	AuthRateLimitReauthIP           InvitationRateLimitAction = "auth.reauth.ip"
	AuthRateLimitReauthSendUser     InvitationRateLimitAction = "auth.reauth.send.user"
	AuthRateLimitReauthSendIP       InvitationRateLimitAction = "auth.reauth.send.ip"
	AuthRateLimitSessionRevokeUser  InvitationRateLimitAction = "auth.session.revoke.user"
	AuthRateLimitSessionRevokeIP    InvitationRateLimitAction = "auth.session.revoke.ip"
)

func authRateLimitPolicies(emailQuota, phoneQuota int) map[InvitationRateLimitAction]invitationRateLimitPolicy {
	return map[InvitationRateLimitAction]invitationRateLimitPolicy{
		AuthRateLimitDiscoveryIP:        {Limit: 30, Window: time.Minute},
		AuthRateLimitSignupIP:           {Limit: 10, Window: 24 * time.Hour},
		AuthRateLimitPasswordFailIP:     {Limit: 30, Window: 15 * time.Minute},
		AuthRateLimitOTPSendDestination: {Limit: 5, Window: time.Hour},
		AuthRateLimitOTPSendIP:          {Limit: 20, Window: time.Hour},
		AuthRateLimitOTPVerifyIP:        {Limit: 50, Window: 15 * time.Minute},
		AuthRateLimitDeliveryDailyEmail: {Limit: emailQuota, Window: 24 * time.Hour},
		AuthRateLimitDeliveryDailyPhone: {Limit: phoneQuota, Window: 24 * time.Hour},
		AuthRateLimitReauthUser:         {Limit: 10, Window: 15 * time.Minute},
		AuthRateLimitReauthIP:           {Limit: 30, Window: 15 * time.Minute},
		AuthRateLimitReauthSendUser:     {Limit: 5, Window: time.Hour},
		AuthRateLimitReauthSendIP:       {Limit: 20, Window: time.Hour},
		AuthRateLimitSessionRevokeUser:  {Limit: 20, Window: time.Minute},
		AuthRateLimitSessionRevokeIP:    {Limit: 60, Window: time.Minute},
	}
}

func NewPostgresAuthRateLimiter(database invitationRateLimitDatabase, queryTimeout time.Duration, emailQuota, phoneQuota int) (InvitationRateLimiter, error) {
	if database == nil || queryTimeout <= 0 || emailQuota < 0 || phoneQuota < 0 {
		return nil, errors.New("auth rate limiter requires a database, timeout and nonnegative quotas")
	}
	return &postgresInvitationRateLimiter{database: database, queryTimeout: queryTimeout, policies: authRateLimitPolicies(emailQuota, phoneQuota)}, nil
}
