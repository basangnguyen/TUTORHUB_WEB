package httpapi

import (
	"errors"
	"strings"
	"time"
)

// Auth purposes share the atomic PostgreSQL limiter with invitations, but never
// share a bucket or quota with another purpose.
const (
	AuthRateLimitMFAUser            InvitationRateLimitAction = "auth.mfa.user"
	AuthRateLimitMFAIP              InvitationRateLimitAction = "auth.mfa.ip"
	AuthRateLimitPasskeyOptionsIP   InvitationRateLimitAction = "auth.passkey.options.ip"
	AuthRateLimitPasskeyVerifyIP    InvitationRateLimitAction = "auth.passkey.verify.ip"
	AuthRateLimitDiscoveryIP        InvitationRateLimitAction = "auth.discovery.ip"
	AuthRateLimitSignupIP           InvitationRateLimitAction = "auth.signup.ip"
	AuthRateLimitPasswordFailIP     InvitationRateLimitAction = "auth.password.fail.ip"
	AuthRateLimitOTPSendDestination InvitationRateLimitAction = "auth.otp.send.destination"
	AuthRateLimitOTPSendIP          InvitationRateLimitAction = "auth.otp.send.ip"
	AuthRateLimitOTPSendCountry     InvitationRateLimitAction = "auth.otp.send.country"
	AuthRateLimitOTPVerifyIP        InvitationRateLimitAction = "auth.otp.verify.ip"
	AuthRateLimitDeliveryDailyEmail InvitationRateLimitAction = "auth.delivery.daily.email"
	AuthRateLimitDeliveryDailyPhone InvitationRateLimitAction = "auth.delivery.daily.phone"
	AuthRateLimitReauthUser         InvitationRateLimitAction = "auth.reauth.user"
	AuthRateLimitReauthIP           InvitationRateLimitAction = "auth.reauth.ip"
	AuthRateLimitReauthSendUser     InvitationRateLimitAction = "auth.reauth.send.user"
	AuthRateLimitReauthSendIP       InvitationRateLimitAction = "auth.reauth.send.ip"
	AuthRateLimitSessionRevokeUser  InvitationRateLimitAction = "auth.session.revoke.user"
	AuthRateLimitSessionRevokeIP    InvitationRateLimitAction = "auth.session.revoke.ip"
	AuthRateLimitOAuthStartIP       InvitationRateLimitAction = "auth.oauth.start.ip"
	AuthRateLimitOAuthCallbackIP    InvitationRateLimitAction = "auth.oauth.callback.ip"
	AuthRateLimitLinkIntentIP       InvitationRateLimitAction = "auth.link_intent.ip"
	AuthRateLimitLinkIntentUser     InvitationRateLimitAction = "auth.link_intent.user"
)

func authRateLimitPolicies(emailQuota, phoneQuota int) map[InvitationRateLimitAction]invitationRateLimitPolicy {
	return scaledAuthRateLimitPolicies(emailQuota, phoneQuota, 1)
}

// scaledAuthRateLimitPolicies multiplies only per-IP buckets. Destination,
// user and delivery quotas stay unchanged. Config limits multipliers other
// than 1 to APP_ENV=test.
func scaledAuthRateLimitPolicies(emailQuota, phoneQuota, ipMultiplier int) map[InvitationRateLimitAction]invitationRateLimitPolicy {
	policies := map[InvitationRateLimitAction]invitationRateLimitPolicy{
		AuthRateLimitMFAUser:            {Limit: 5, Window: 5 * time.Minute},
		AuthRateLimitMFAIP:              {Limit: 20, Window: 5 * time.Minute},
		AuthRateLimitPasskeyOptionsIP:   {Limit: 30, Window: time.Minute},
		AuthRateLimitPasskeyVerifyIP:    {Limit: 30, Window: 15 * time.Minute},
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
		AuthRateLimitOAuthStartIP:       {Limit: 30, Window: 15 * time.Minute},
		AuthRateLimitOAuthCallbackIP:    {Limit: 60, Window: 15 * time.Minute},
		AuthRateLimitLinkIntentIP:       {Limit: 30, Window: 15 * time.Minute},
		AuthRateLimitLinkIntentUser:     {Limit: 10, Window: 15 * time.Minute},
	}
	if ipMultiplier > 1 {
		for action, policy := range policies {
			if strings.HasSuffix(string(action), ".ip") {
				policy.Limit *= ipMultiplier
				policies[action] = policy
			}
		}
	}
	return policies
}

func NewPostgresAuthRateLimiter(database invitationRateLimitDatabase, queryTimeout time.Duration, emailQuota, phoneQuota int) (InvitationRateLimiter, error) {
	return NewScaledPostgresAuthRateLimiter(database, queryTimeout, emailQuota, phoneQuota, 1)
}

// NewScaledPostgresAuthRateLimiter applies AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER.
func NewScaledPostgresAuthRateLimiter(database invitationRateLimitDatabase, queryTimeout time.Duration, emailQuota, phoneQuota, ipMultiplier int) (InvitationRateLimiter, error) {
	if database == nil || queryTimeout <= 0 || emailQuota < 0 || phoneQuota < 0 || ipMultiplier < 1 {
		return nil, errors.New("auth rate limiter requires a database, timeout, nonnegative quotas and a positive IP multiplier")
	}
	return &postgresInvitationRateLimiter{database: database, queryTimeout: queryTimeout, policies: scaledAuthRateLimitPolicies(emailQuota, phoneQuota, ipMultiplier)}, nil
}
