package featurecontrol

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const inviteCreationWindow = time.Hour

type DBTX interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     DBTX
	queryTimeout time.Duration
	authorizer   policy.Authorizer
	catalog      *Catalog
}

func NewPostgresRepository(
	database DBTX,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	catalog *Catalog,
) (*PostgresRepository, error) {
	if database == nil || queryTimeout <= 0 || authorizer == nil || catalog == nil {
		return nil, fmt.Errorf("feature control PostgreSQL dependencies are required")
	}
	return &PostgresRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer, catalog: catalog,
	}, nil
}

func (repository *PostgresRepository) GetCapabilities(
	ctx context.Context,
	tenantContext tenancy.Context,
	now time.Time,
) (Capabilities, error) {
	if err := tenantContext.Validate(); err != nil {
		return Capabilities{}, ErrAccessDenied
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Capabilities{}, fmt.Errorf("begin feature control read: %w", err)
	}
	defer rollbackFeatureControlTransaction(transaction)

	if err := acquireTenantControlLock(queryContext, transaction, tenantContext.TenantID); err != nil {
		return Capabilities{}, err
	}
	authorization, err := repository.authorizeLockedTenant(
		queryContext,
		transaction,
		tenantContext,
		policy.ActionTenantView,
	)
	if err != nil {
		return Capabilities{}, err
	}
	capabilities, err := repository.readCapabilities(
		queryContext,
		transaction,
		tenantContext.TenantID,
		authorization.canManageControls,
		now,
	)
	if err != nil {
		return Capabilities{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Capabilities{}, fmt.Errorf("commit feature control read: %w", err)
	}
	return capabilities, nil
}

func (repository *PostgresRepository) PutOverrides(
	ctx context.Context,
	tenantContext tenancy.Context,
	input PutOverridesInput,
	now time.Time,
) (Capabilities, error) {
	if err := tenantContext.Validate(); err != nil {
		return Capabilities{}, ErrAccessDenied
	}
	normalized, err := normalizeOverrides(repository.catalog, input)
	if err != nil {
		return Capabilities{}, err
	}
	now = now.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Capabilities{}, fmt.Errorf("begin feature control update: %w", err)
	}
	defer rollbackFeatureControlTransaction(transaction)

	if err := acquireTenantControlLock(queryContext, transaction, tenantContext.TenantID); err != nil {
		return Capabilities{}, err
	}
	if _, err := repository.authorizeLockedTenant(
		queryContext,
		transaction,
		tenantContext,
		policy.ActionTenantManageFeatures,
	); err != nil {
		return Capabilities{}, err
	}
	currentVersion, err := lockControlRevision(
		queryContext,
		transaction,
		tenantContext,
		now,
	)
	if err != nil {
		return Capabilities{}, err
	}
	if currentVersion != normalized.ExpectedVersion {
		return Capabilities{}, &VersionConflictError{
			Expected: normalized.ExpectedVersion, Current: currentVersion,
		}
	}
	if err := replaceFeatureOverrides(
		queryContext,
		transaction,
		tenantContext,
		normalized.FeatureOverrides,
		now,
	); err != nil {
		return Capabilities{}, err
	}
	if err := replaceQuotaOverrides(
		queryContext,
		transaction,
		tenantContext,
		normalized.QuotaOverrides,
		now,
	); err != nil {
		return Capabilities{}, err
	}
	newVersion := currentVersion + 1
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.tenant_feature_control_revisions
SET version = $2,
    updated_by = $3,
    updated_at = $4
WHERE tenant_id = $1`,
		tenantContext.TenantID,
		newVersion,
		tenantContext.ActorID,
		now,
	); err != nil {
		return Capabilities{}, fmt.Errorf("update feature control revision: %w", err)
	}
	if err := appendFeatureControlUpdate(
		queryContext,
		transaction,
		tenantContext,
		newVersion,
		len(normalized.FeatureOverrides),
		len(normalized.QuotaOverrides),
		now,
	); err != nil {
		return Capabilities{}, err
	}
	capabilities, err := repository.readCapabilities(
		queryContext,
		transaction,
		tenantContext.TenantID,
		true,
		now,
	)
	if err != nil {
		return Capabilities{}, err
	}
	if capabilities.Version != newVersion {
		return Capabilities{}, fmt.Errorf("feature control revision did not advance")
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Capabilities{}, fmt.Errorf("commit feature control update: %w", err)
	}
	return capabilities, nil
}

func (repository *PostgresRepository) RequireFeature(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
	key FeatureKey,
) (resultErr error) {
	defer func() { resultErr = NormalizeError(resultErr) }()
	if transaction == nil || tenantID == uuid.Nil {
		return ErrInvalidControl
	}
	if _, known := repository.catalog.FeatureDefinition(key); !known {
		return fmt.Errorf("require feature %q: %w", key, ErrInvalidControl)
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	if err := acquireTenantControlLock(queryContext, transaction, tenantID); err != nil {
		return err
	}
	if err := ensureActiveControlTenant(queryContext, transaction, tenantID); err != nil {
		return err
	}
	effective, err := repository.readEffectiveFeature(queryContext, transaction, tenantID, key)
	if err != nil {
		return err
	}
	if !effective.Enabled {
		return &FeatureDisabledError{Feature: key}
	}
	return nil
}

func (repository *PostgresRepository) RequireMemberCapacity(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
) error {
	return NormalizeError(repository.requireCapacity(
		ctx,
		transaction,
		tenantID,
		QuotaMembers,
		`SELECT count(*) FROM tutorhub.memberships WHERE tenant_id = $1 AND status = 'active'`,
	))
}

func (repository *PostgresRepository) RequireActiveClassCapacity(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
) error {
	return NormalizeError(repository.requireCapacity(
		ctx,
		transaction,
		tenantID,
		QuotaActiveClasses,
		`SELECT count(*) FROM tutorhub.classes WHERE tenant_id = $1 AND status = 'active'`,
	))
}

func (repository *PostgresRepository) ConsumeInviteCreation(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
	now time.Time,
) (result RateLimitResult, resultErr error) {
	defer func() { resultErr = NormalizeError(resultErr) }()
	if transaction == nil || tenantID == uuid.Nil || now.IsZero() {
		return RateLimitResult{}, ErrInvalidControl
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	if err := acquireTenantControlLock(queryContext, transaction, tenantID); err != nil {
		return RateLimitResult{}, err
	}
	if err := ensureActiveControlTenant(queryContext, transaction, tenantID); err != nil {
		return RateLimitResult{}, err
	}
	effective, err := repository.readEffectiveQuota(
		queryContext,
		transaction,
		tenantID,
		QuotaInviteCreationsPerHour,
	)
	if err != nil {
		return RateLimitResult{}, err
	}
	now = now.UTC()
	windowFrom := now.Truncate(inviteCreationWindow)
	resetAt := windowFrom.Add(inviteCreationWindow)
	var used int64
	err = transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.tenant_quota_windows (
    tenant_id,
    quota_key,
    window_started_at,
    window_ends_at,
    used_count,
    updated_at
)
VALUES ($1, $2, $3, $4, 1, $5)
ON CONFLICT (tenant_id, quota_key, window_started_at)
DO UPDATE SET
    used_count = tutorhub.tenant_quota_windows.used_count + 1,
    updated_at = EXCLUDED.updated_at
WHERE tutorhub.tenant_quota_windows.used_count < $6
RETURNING used_count`,
		tenantID,
		QuotaInviteCreationsPerHour,
		windowFrom,
		resetAt,
		now,
		effective.Limit,
	).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		if scanErr := transaction.QueryRow(
			queryContext,
			`SELECT used_count
FROM tutorhub.tenant_quota_windows
WHERE tenant_id = $1 AND quota_key = $2 AND window_started_at = $3`,
			tenantID,
			QuotaInviteCreationsPerHour,
			windowFrom,
		).Scan(&used); scanErr != nil {
			return RateLimitResult{}, fmt.Errorf("read exhausted invitation quota: %w", scanErr)
		}
		result := newRateLimitResult(effective.Limit, used, windowFrom, resetAt, now)
		return result, &QuotaExceededError{
			Quota: QuotaInviteCreationsPerHour, Limit: effective.Limit, Used: used,
			ResetAt: resetAt, RetryAfter: result.RetryAfter,
		}
	}
	if err != nil {
		return RateLimitResult{}, fmt.Errorf("consume invitation quota: %w", err)
	}
	return newRateLimitResult(effective.Limit, used, windowFrom, resetAt, now), nil
}

func (repository *PostgresRepository) requireCapacity(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
	key QuotaKey,
	countQuery string,
) error {
	if transaction == nil || tenantID == uuid.Nil {
		return ErrInvalidControl
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	if err := acquireTenantControlLock(queryContext, transaction, tenantID); err != nil {
		return err
	}
	if err := ensureActiveControlTenant(queryContext, transaction, tenantID); err != nil {
		return err
	}
	effective, err := repository.readEffectiveQuota(queryContext, transaction, tenantID, key)
	if err != nil {
		return err
	}
	var used int64
	if err := transaction.QueryRow(queryContext, countQuery, tenantID).Scan(&used); err != nil {
		return fmt.Errorf("count %s quota usage: %w", key, err)
	}
	if used >= effective.Limit {
		return &QuotaExceededError{Quota: key, Limit: effective.Limit, Used: used}
	}
	return nil
}

type tenantAuthorization struct {
	canManageControls bool
}

func (repository *PostgresRepository) authorizeLockedTenant(
	ctx context.Context,
	transaction Transaction,
	tenantContext tenancy.Context,
	action policy.Action,
) (tenantAuthorization, error) {
	var tenantStatus string
	if err := transaction.QueryRow(
		ctx,
		`SELECT status FROM tutorhub.tenants WHERE id = $1 FOR SHARE`,
		tenantContext.TenantID,
	).Scan(&tenantStatus); errors.Is(err, pgx.ErrNoRows) {
		return tenantAuthorization{}, ErrTenantNotFound
	} else if err != nil {
		return tenantAuthorization{}, fmt.Errorf("lock feature control tenant: %w", err)
	}
	if tenantStatus != "active" {
		return tenantAuthorization{}, ErrAccessDenied
	}
	var role string
	var membershipStatus string
	if err := transaction.QueryRow(
		ctx,
		`SELECT role, status
FROM tutorhub.memberships
WHERE tenant_id = $1 AND user_id = $2
FOR SHARE`,
		tenantContext.TenantID,
		tenantContext.ActorID,
	).Scan(&role, &membershipStatus); errors.Is(err, pgx.ErrNoRows) {
		return tenantAuthorization{}, ErrAccessDenied
	} else if err != nil {
		return tenantAuthorization{}, fmt.Errorf("lock feature control membership: %w", err)
	}
	membershipActive := membershipStatus == "active"
	subject := policy.Subject{
		ActorID:           tenantContext.ActorID,
		ActiveTenantID:    tenantContext.TenantID,
		MembershipActive:  membershipActive,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRole(role)},
	}
	resource := policy.Resource{
		TenantID: tenantContext.TenantID, State: policy.ResourceStateActive,
	}
	if !repository.authorizer.Authorize(policy.Input{
		Subject: subject, Action: action, Resource: resource,
	}).Allowed {
		return tenantAuthorization{}, ErrAccessDenied
	}
	return tenantAuthorization{
		canManageControls: repository.authorizer.Authorize(policy.Input{
			Subject: subject, Action: policy.ActionTenantManageFeatures, Resource: resource,
		}).Allowed,
	}, nil
}

func (repository *PostgresRepository) readCapabilities(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
	canManage bool,
	now time.Time,
) (Capabilities, error) {
	featureOverrides, err := loadFeatureOverrides(ctx, transaction, tenantID)
	if err != nil {
		return Capabilities{}, err
	}
	quotaOverrides, err := loadQuotaOverrides(ctx, transaction, tenantID)
	if err != nil {
		return Capabilities{}, err
	}
	version, err := readControlRevision(ctx, transaction, tenantID)
	if err != nil {
		return Capabilities{}, err
	}
	capabilities := Capabilities{
		TenantID: tenantID,
		Version:  version,
		AllowedAction: AllowedActions{
			ManageControls: canManage,
		},
	}
	for _, definition := range repository.catalog.Features() {
		override, overridden := featureOverrides[definition.Key]
		configured := definition.DefaultEnabled
		var overridePointer *bool
		if overridden {
			overridePointer = &override
			configured = override
		}
		effective, evaluateErr := repository.catalog.EvaluateFeature(
			definition.Key,
			overridePointer,
		)
		if evaluateErr != nil {
			return Capabilities{}, evaluateErr
		}
		capabilities.Features = append(
			capabilities.Features,
			FeatureCapability{
				EffectiveFeature:  effective,
				ConfiguredEnabled: configured,
			},
		)
	}
	now = now.UTC()
	for _, definition := range repository.catalog.Quotas() {
		override, overridden := quotaOverrides[definition.Key]
		configured := definition.DefaultLimit
		var overridePointer *int64
		if overridden {
			overridePointer = &override
			configured = override
		}
		effective, evaluateErr := repository.catalog.EvaluateQuota(
			definition.Key,
			overridePointer,
		)
		if evaluateErr != nil {
			return Capabilities{}, evaluateErr
		}
		usage, windowStart, resetAt, usageErr := readQuotaUsage(
			ctx,
			transaction,
			tenantID,
			definition.Key,
			now,
		)
		if usageErr != nil {
			return Capabilities{}, usageErr
		}
		capability := QuotaCapability{
			EffectiveQuota:  effective,
			ConfiguredLimit: configured,
			Used:            usage,
			Remaining:       nonNegative(effective.Limit - usage),
		}
		if !windowStart.IsZero() {
			capability.WindowStartedAt = &windowStart
			capability.ResetAt = &resetAt
		}
		capabilities.Quotas = append(capabilities.Quotas, capability)
	}
	return capabilities, nil
}

func (repository *PostgresRepository) readEffectiveFeature(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
	key FeatureKey,
) (EffectiveFeature, error) {
	var override bool
	err := transaction.QueryRow(
		ctx,
		`SELECT enabled
FROM tutorhub.tenant_feature_overrides
WHERE tenant_id = $1 AND feature_key = $2`,
		tenantID,
		key,
	).Scan(&override)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.catalog.EvaluateFeature(key, nil)
	}
	if err != nil {
		return EffectiveFeature{}, fmt.Errorf("read feature override %q: %w", key, err)
	}
	return repository.catalog.EvaluateFeature(key, &override)
}

func (repository *PostgresRepository) readEffectiveQuota(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
	key QuotaKey,
) (EffectiveQuota, error) {
	var override int64
	err := transaction.QueryRow(
		ctx,
		`SELECT limit_value
FROM tutorhub.tenant_quota_overrides
WHERE tenant_id = $1 AND quota_key = $2`,
		tenantID,
		key,
	).Scan(&override)
	if errors.Is(err, pgx.ErrNoRows) {
		return repository.catalog.EvaluateQuota(key, nil)
	}
	if err != nil {
		return EffectiveQuota{}, fmt.Errorf("read quota override %q: %w", key, err)
	}
	return repository.catalog.EvaluateQuota(key, &override)
}

func loadFeatureOverrides(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
) (map[FeatureKey]bool, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT feature_key, enabled
FROM tutorhub.tenant_feature_overrides
WHERE tenant_id = $1
ORDER BY feature_key`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list feature overrides: %w", err)
	}
	defer rows.Close()
	overrides := make(map[FeatureKey]bool)
	for rows.Next() {
		var key FeatureKey
		var enabled bool
		if err := rows.Scan(&key, &enabled); err != nil {
			return nil, fmt.Errorf("scan feature override: %w", err)
		}
		overrides[key] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature overrides: %w", err)
	}
	return overrides, nil
}

func loadQuotaOverrides(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
) (map[QuotaKey]int64, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT quota_key, limit_value
FROM tutorhub.tenant_quota_overrides
WHERE tenant_id = $1
ORDER BY quota_key`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list quota overrides: %w", err)
	}
	defer rows.Close()
	overrides := make(map[QuotaKey]int64)
	for rows.Next() {
		var key QuotaKey
		var limit int64
		if err := rows.Scan(&key, &limit); err != nil {
			return nil, fmt.Errorf("scan quota override: %w", err)
		}
		overrides[key] = limit
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate quota overrides: %w", err)
	}
	return overrides, nil
}

func readQuotaUsage(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
	key QuotaKey,
	now time.Time,
) (int64, time.Time, time.Time, error) {
	var query string
	switch key {
	case QuotaMembers:
		query = `SELECT count(*) FROM tutorhub.memberships WHERE tenant_id = $1 AND status = 'active'`
	case QuotaActiveClasses:
		query = `SELECT count(*) FROM tutorhub.classes WHERE tenant_id = $1 AND status = 'active'`
	case QuotaInviteCreationsPerHour:
		windowStart := now.UTC().Truncate(inviteCreationWindow)
		resetAt := windowStart.Add(inviteCreationWindow)
		var used int64
		if err := transaction.QueryRow(
			ctx,
			`SELECT coalesce(sum(used_count), 0)
FROM tutorhub.tenant_quota_windows
WHERE tenant_id = $1 AND quota_key = $2 AND window_started_at = $3`,
			tenantID,
			key,
			windowStart,
		).Scan(&used); err != nil {
			return 0, time.Time{}, time.Time{}, fmt.Errorf("read invitation quota usage: %w", err)
		}
		return used, windowStart, resetAt, nil
	default:
		return 0, time.Time{}, time.Time{}, ErrInvalidControl
	}
	var used int64
	if err := transaction.QueryRow(ctx, query, tenantID).Scan(&used); err != nil {
		return 0, time.Time{}, time.Time{}, fmt.Errorf("read %s quota usage: %w", key, err)
	}
	return used, time.Time{}, time.Time{}, nil
}

func readControlRevision(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
) (int64, error) {
	var version int64
	err := transaction.QueryRow(
		ctx,
		`SELECT version
FROM tutorhub.tenant_feature_control_revisions
WHERE tenant_id = $1`,
		tenantID,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read feature control revision: %w", err)
	}
	return version, nil
}

func lockControlRevision(
	ctx context.Context,
	transaction Transaction,
	tenantContext tenancy.Context,
	now time.Time,
) (int64, error) {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenant_feature_control_revisions (
    tenant_id,
    version,
    updated_by,
    updated_at
)
VALUES ($1, 0, $2, $3)
ON CONFLICT (tenant_id) DO NOTHING`,
		tenantContext.TenantID,
		tenantContext.ActorID,
		now,
	); err != nil {
		return 0, fmt.Errorf("initialize feature control revision: %w", err)
	}
	var version int64
	if err := transaction.QueryRow(
		ctx,
		`SELECT version
FROM tutorhub.tenant_feature_control_revisions
WHERE tenant_id = $1
FOR UPDATE`,
		tenantContext.TenantID,
	).Scan(&version); err != nil {
		return 0, fmt.Errorf("lock feature control revision: %w", err)
	}
	return version, nil
}

func replaceFeatureOverrides(
	ctx context.Context,
	transaction Transaction,
	tenantContext tenancy.Context,
	overrides []FeatureOverride,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`DELETE FROM tutorhub.tenant_feature_overrides WHERE tenant_id = $1`,
		tenantContext.TenantID,
	); err != nil {
		return fmt.Errorf("reset feature overrides: %w", err)
	}
	for _, override := range overrides {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.tenant_feature_overrides (
    tenant_id,
    feature_key,
    enabled,
    updated_by,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $5)`,
			tenantContext.TenantID,
			override.Key,
			override.Enabled,
			tenantContext.ActorID,
			now,
		); err != nil {
			return fmt.Errorf("store feature override %q: %w", override.Key, err)
		}
	}
	return nil
}

func replaceQuotaOverrides(
	ctx context.Context,
	transaction Transaction,
	tenantContext tenancy.Context,
	overrides []QuotaOverride,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`DELETE FROM tutorhub.tenant_quota_overrides WHERE tenant_id = $1`,
		tenantContext.TenantID,
	); err != nil {
		return fmt.Errorf("reset quota overrides: %w", err)
	}
	for _, override := range overrides {
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.tenant_quota_overrides (
    tenant_id,
    quota_key,
    limit_value,
    updated_by,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $5)`,
			tenantContext.TenantID,
			override.Key,
			override.Limit,
			tenantContext.ActorID,
			now,
		); err != nil {
			return fmt.Errorf("store quota override %q: %w", override.Key, err)
		}
	}
	return nil
}

func appendFeatureControlUpdate(
	ctx context.Context,
	transaction Transaction,
	tenantContext tenancy.Context,
	version int64,
	featureCount int,
	quotaCount int,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    occurred_at,
    available_at
)
VALUES (
    $1,
    'tenant_feature_control',
    $1,
    'tenant.feature_controls.updated',
    jsonb_build_object(
        'tenant_id', $1::uuid,
        'actor_user_id', $2::uuid,
        'version', $3::bigint,
        'feature_override_count', $4::integer,
        'quota_override_count', $5::integer
    ),
    $6,
    $6
)`,
		tenantContext.TenantID,
		tenantContext.ActorID,
		version,
		featureCount,
		quotaCount,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert feature control outbox event: %w", err)
	}
	request := requestmeta.SnapshotFromContext(ctx)
	if request.RequestInstance == uuid.Nil {
		request.RequestInstance = uuid.New()
	}
	var sourceIPPrefix any
	if request.SourceIPPrefix != "" {
		sourceIPPrefix = request.SourceIPPrefix
	}
	var userAgentHash any
	if len(request.UserAgentHash) > 0 {
		userAgentHash = request.UserAgentHash
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.audit_events (
    tenant_id,
    actor_type,
    actor_user_id,
    action,
    resource_type,
    resource_id,
    outcome,
    request_id,
    request_instance_id,
    source_ip_prefix,
    user_agent_hash,
    metadata,
    occurred_at
)
VALUES (
    $1,
    'user',
    $2,
    'tenant.feature_control.update',
    'tenant_feature_control',
    $1,
    'succeeded',
    $3,
    $4,
    $5::inet,
    $6,
    jsonb_build_object(
        'effect', 'replace',
        'version', $7::bigint::text,
        'feature_override_count', $8::integer::text,
        'quota_override_count', $9::integer::text
    ),
    $10
)`,
		tenantContext.TenantID,
		tenantContext.ActorID,
		request.RequestID,
		request.RequestInstance,
		sourceIPPrefix,
		userAgentHash,
		version,
		featureCount,
		quotaCount,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert feature control audit event: %w", err)
	}
	return nil
}

func acquireTenantControlLock(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
) error {
	// This advisory lock is the first lock acquired by feature-control reads,
	// override writes, and governed business mutations. Keeping that order avoids
	// a cycle with later tenant, membership, invitation, or class row locks.
	if transaction == nil || tenantID == uuid.Nil {
		return ErrInvalidControl
	}
	if _, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock($1::bigint)`,
		tenantControlLockKey(tenantID),
	); err != nil {
		return fmt.Errorf("acquire tenant feature control lock: %w", err)
	}
	return nil
}

func tenantControlLockKey(tenantID uuid.UUID) int64 {
	digest := sha256.Sum256(append(
		[]byte("tutorhub-feature-control-v1\x00"),
		tenantID[:]...,
	))
	return int64(binary.BigEndian.Uint64(digest[:8]))
}

func ensureActiveControlTenant(
	ctx context.Context,
	transaction Transaction,
	tenantID uuid.UUID,
) error {
	var status string
	err := transaction.QueryRow(
		ctx,
		`SELECT status FROM tutorhub.tenants WHERE id = $1`,
		tenantID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTenantNotFound
	}
	if err != nil {
		return fmt.Errorf("read feature control tenant: %w", err)
	}
	if status != "active" {
		return ErrAccessDenied
	}
	return nil
}

func newRateLimitResult(
	limit int64,
	used int64,
	windowFrom time.Time,
	resetAt time.Time,
	now time.Time,
) RateLimitResult {
	retryAfter := resetAt.Sub(now)
	if retryAfter < 0 {
		retryAfter = 0
	}
	return RateLimitResult{
		Limit: limit, Used: used, Remaining: nonNegative(limit - used),
		WindowFrom: windowFrom, ResetAt: resetAt, RetryAfter: retryAfter,
	}
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func (repository *PostgresRepository) contextWithTimeout(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, repository.queryTimeout)
}

func rollbackFeatureControlTransaction(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}

var _ Repository = (*PostgresRepository)(nil)
var _ Enforcer = (*PostgresRepository)(nil)
