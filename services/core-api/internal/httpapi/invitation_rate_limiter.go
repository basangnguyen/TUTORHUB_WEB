package httpapi

import (
	"context"
	"math"
	"strconv"
	"sync"
	"time"
)

type InvitationRateLimitAction string

const (
	InvitationRateLimitPreview   InvitationRateLimitAction = "preview"
	InvitationRateLimitAccept    InvitationRateLimitAction = "accept"
	InvitationRateLimitClassJoin InvitationRateLimitAction = "class_join"
)

type InvitationRateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
	Err        error
}

// InvitationRateLimiter deliberately receives only an action and an IP prefix.
// Raw invitation tokens must never be used as limiter keys or retained in memory.
type InvitationRateLimiter interface {
	Allow(
		ctx context.Context,
		action InvitationRateLimitAction,
		clientPrefix string,
		now time.Time,
	) InvitationRateLimitDecision
}

type invitationRateLimitPolicy struct {
	Limit  int
	Window time.Duration
}

type fixedWindowInvitationRateLimiter struct {
	mutex    sync.Mutex
	maximum  int
	policies map[InvitationRateLimitAction]invitationRateLimitPolicy
	entries  map[invitationRateLimitKey]fixedWindowInvitationRateLimitEntry
}

type invitationRateLimitKey struct {
	action       InvitationRateLimitAction
	clientPrefix string
}

type fixedWindowInvitationRateLimitEntry struct {
	windowStartedAt time.Time
	lastSeenAt      time.Time
	count           int
}

func newDefaultInvitationRateLimiter() InvitationRateLimiter {
	return newFixedWindowInvitationRateLimiter(
		4096,
		map[InvitationRateLimitAction]invitationRateLimitPolicy{
			InvitationRateLimitPreview:   {Limit: 30, Window: time.Minute},
			InvitationRateLimitAccept:    {Limit: 10, Window: time.Minute},
			InvitationRateLimitClassJoin: {Limit: 10, Window: time.Minute},
		},
	)
}

func newFixedWindowInvitationRateLimiter(
	maximumEntries int,
	policies map[InvitationRateLimitAction]invitationRateLimitPolicy,
) *fixedWindowInvitationRateLimiter {
	if maximumEntries < 1 {
		maximumEntries = 1
	}
	clonedPolicies := make(map[InvitationRateLimitAction]invitationRateLimitPolicy, len(policies))
	for action, policy := range policies {
		clonedPolicies[action] = policy
	}
	return &fixedWindowInvitationRateLimiter{
		maximum:  maximumEntries,
		policies: clonedPolicies,
		entries:  make(map[invitationRateLimitKey]fixedWindowInvitationRateLimitEntry),
	}
}

func (limiter *fixedWindowInvitationRateLimiter) Allow(
	_ context.Context,
	action InvitationRateLimitAction,
	clientPrefix string,
	now time.Time,
) InvitationRateLimitDecision {
	if limiter == nil {
		return InvitationRateLimitDecision{Allowed: true}
	}
	policy, configured := limiter.policies[action]
	if !configured || policy.Limit < 1 || policy.Window <= 0 {
		return InvitationRateLimitDecision{Allowed: true}
	}

	now = now.UTC()
	key := invitationRateLimitKey{action: action, clientPrefix: clientPrefix}
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	entry, exists := limiter.entries[key]
	if exists && (now.Before(entry.windowStartedAt) || !now.Before(entry.windowStartedAt.Add(policy.Window))) {
		entry = fixedWindowInvitationRateLimitEntry{}
		exists = false
	}
	if !exists {
		limiter.makeRoom(now)
		entry = fixedWindowInvitationRateLimitEntry{windowStartedAt: now}
	}
	entry.lastSeenAt = now
	if entry.count >= policy.Limit {
		limiter.entries[key] = entry
		retryAfter := entry.windowStartedAt.Add(policy.Window).Sub(now)
		if retryAfter <= 0 {
			retryAfter = time.Second
		}
		return InvitationRateLimitDecision{RetryAfter: retryAfter}
	}

	entry.count++
	limiter.entries[key] = entry
	return InvitationRateLimitDecision{Allowed: true}
}

func (limiter *fixedWindowInvitationRateLimiter) makeRoom(now time.Time) {
	if len(limiter.entries) < limiter.maximum {
		return
	}

	for key, entry := range limiter.entries {
		policy, configured := limiter.policies[key.action]
		if !configured || policy.Window <= 0 || !now.Before(entry.windowStartedAt.Add(policy.Window)) {
			delete(limiter.entries, key)
		}
	}
	if len(limiter.entries) < limiter.maximum {
		return
	}

	var oldestKey invitationRateLimitKey
	var oldestTime time.Time
	found := false
	for key, entry := range limiter.entries {
		if !found || entry.lastSeenAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.lastSeenAt
			found = true
		}
	}
	if found {
		delete(limiter.entries, oldestKey)
	}
}

func retryAfterSeconds(duration time.Duration) string {
	seconds := int64(math.Ceil(duration.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}
