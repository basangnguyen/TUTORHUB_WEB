package featurecontrol

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidControl  = errors.New("invalid feature control")
	ErrAccessDenied    = errors.New("feature control access denied")
	ErrTenantNotFound  = errors.New("feature control tenant not found")
	ErrFeatureDisabled = errors.New("feature is disabled")
	ErrQuotaExceeded   = errors.New("quota is exceeded")
	ErrVersionConflict = errors.New("feature control version is stale")
	ErrUnavailable     = errors.New("feature controls are unavailable")
)

// NormalizeError preserves expected domain failures and classifies every
// unexpected repository/evaluator failure as unavailable. Mutation handlers
// can therefore fail closed without leaking storage details or returning an
// ambiguous generic error.
func NormalizeError(err error) error {
	if err == nil {
		return nil
	}
	known := []error{
		ErrInvalidControl,
		ErrAccessDenied,
		ErrTenantNotFound,
		ErrFeatureDisabled,
		ErrQuotaExceeded,
		ErrVersionConflict,
		ErrUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return err
		}
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

type EffectiveFeature struct {
	Key     FeatureKey  `json:"key"`
	Enabled bool        `json:"enabled"`
	Source  ValueSource `json:"source"`
}

type EffectiveQuota struct {
	Key    QuotaKey    `json:"key"`
	Limit  int64       `json:"limit"`
	Source ValueSource `json:"source"`
}

type FeatureCapability struct {
	EffectiveFeature
	ConfiguredEnabled bool `json:"configured_enabled"`
}

type QuotaCapability struct {
	EffectiveQuota
	ConfiguredLimit int64      `json:"configured_limit"`
	Used            int64      `json:"used"`
	Remaining       int64      `json:"remaining"`
	WindowStartedAt *time.Time `json:"window_started_at,omitempty"`
	ResetAt         *time.Time `json:"reset_at,omitempty"`
}

type AllowedActions struct {
	ManageControls bool `json:"manage_controls"`
}

type Capabilities struct {
	TenantID      uuid.UUID           `json:"tenant_id"`
	Version       int64               `json:"version"`
	Features      []FeatureCapability `json:"features"`
	Quotas        []QuotaCapability   `json:"quotas"`
	AllowedAction AllowedActions      `json:"allowed_actions"`
}

type FeatureOverride struct {
	Key     FeatureKey `json:"key"`
	Enabled bool       `json:"enabled"`
}

type QuotaOverride struct {
	Key   QuotaKey `json:"key"`
	Limit int64    `json:"limit"`
}

type PutOverridesInput struct {
	ExpectedVersion  int64             `json:"expected_version"`
	FeatureOverrides []FeatureOverride `json:"feature_overrides"`
	QuotaOverrides   []QuotaOverride   `json:"quota_overrides"`
}

type RateLimitResult struct {
	Limit      int64
	Used       int64
	Remaining  int64
	WindowFrom time.Time
	ResetAt    time.Time
	RetryAfter time.Duration
}

type FeatureDisabledError struct {
	Feature FeatureKey
}

func (failure *FeatureDisabledError) Error() string {
	return fmt.Sprintf("feature %q is disabled", failure.Feature)
}

func (failure *FeatureDisabledError) Unwrap() error {
	return ErrFeatureDisabled
}

type QuotaExceededError struct {
	Quota      QuotaKey
	Limit      int64
	Used       int64
	ResetAt    time.Time
	RetryAfter time.Duration
}

func (failure *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota %q is exhausted (%d/%d)", failure.Quota, failure.Used, failure.Limit)
}

func (failure *QuotaExceededError) Unwrap() error {
	return ErrQuotaExceeded
}

type VersionConflictError struct {
	Expected int64
	Current  int64
}

func (failure *VersionConflictError) Error() string {
	return fmt.Sprintf(
		"feature control version is stale: expected %d, current %d",
		failure.Expected,
		failure.Current,
	)
}

func (failure *VersionConflictError) Unwrap() error {
	return ErrVersionConflict
}
