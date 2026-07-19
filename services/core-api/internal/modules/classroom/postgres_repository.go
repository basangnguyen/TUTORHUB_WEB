package classroom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type DBTX interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct {
	database     DBTX
	queryTimeout time.Duration
	authorizer   policy.Authorizer
}

func NewPostgresRepository(
	database DBTX,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
) *PostgresRepository {
	return &PostgresRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer,
	}
}

func (repository *PostgresRepository) Create(
	ctx context.Context,
	tenantContext tenancy.Context,
	params CreateClassParams,
) (Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return Class{}, ErrClassAccessDenied
	}
	params, err := params.normalized()
	if err != nil {
		return Class{}, err
	}
	if params.OwnerUserID != tenantContext.ActorID {
		return Class{}, ErrClassOwnerUnavailable
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Class{}, fmt.Errorf("begin class creation: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	tenantTimezone, err := lockActiveClassTenant(
		queryContext,
		transaction,
		tenantContext.TenantID,
	)
	if err != nil {
		return Class{}, err
	}
	membership, found, err := lockClassMembership(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
	)
	if err != nil {
		return Class{}, err
	}
	if !found || !membership.Active {
		return Class{}, ErrClassAccessDenied
	}
	if err := repository.authorizeLockedClassCreation(
		tenantContext.ActorID,
		tenantContext.TenantID,
		membership.Role,
	); err != nil {
		return Class{}, err
	}

	timezone := tenantTimezone
	if params.Timezone != nil {
		timezone = *params.Timezone
	}
	class, err := scanClass(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.classes (
    tenant_id,
    owner_user_id,
    code,
    title,
    description,
    timezone
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, tenant_id, owner_user_id, code, title, description, timezone,
          status, version, created_at, updated_at, archived_at`,
		tenantContext.TenantID,
		params.OwnerUserID,
		params.Code,
		params.Title,
		params.Description,
		timezone,
	))
	if err != nil {
		return Class{}, mapClassPostgresError("create class", err)
	}
	if err := insertClassEvent(
		queryContext,
		transaction,
		class,
		tenantContext.ActorID,
		"class.created",
		classEventDetails{OwnerUserID: class.OwnerUserID},
		class.CreatedAt,
	); err != nil {
		return Class{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Class{}, fmt.Errorf("commit class creation: %w", err)
	}
	return class, nil
}

func (repository *PostgresRepository) Get(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
) (Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return Class{}, ErrClassNotFound
	}
	if classID == uuid.Nil {
		return Class{}, ErrClassNotFound
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	class, err := scanClass(repository.database.QueryRow(
		queryContext,
		`SELECT id, tenant_id, owner_user_id, code, title, description, timezone,
       status, version, created_at, updated_at, archived_at
FROM tutorhub.classes
WHERE tenant_id = $1 AND id = $2`,
		tenantContext.TenantID,
		classID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Class{}, ErrClassNotFound
	}
	if err != nil {
		return Class{}, fmt.Errorf("get class: %w", err)
	}
	return class, nil
}

func (repository *PostgresRepository) List(
	ctx context.Context,
	tenantContext tenancy.Context,
	params ListClassesParams,
) (ListClassesResult, error) {
	if err := tenantContext.Validate(); err != nil {
		return ListClassesResult{}, ErrClassAccessDenied
	}
	if params.Limit < 1 || params.Limit > maximumListLimit {
		return ListClassesResult{}, fmt.Errorf(
			"%w: class list limit must be between 1 and %d",
			ErrInvalidListLimit,
			maximumListLimit,
		)
	}
	if params.Status != nil {
		if err := validateClassStatus(*params.Status); err != nil {
			return ListClassesResult{}, err
		}
	}
	if params.After != nil &&
		(params.After.CreatedAt.IsZero() || params.After.ID == uuid.Nil) {
		return ListClassesResult{}, ErrInvalidClassCursor
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	organizationRole, err := repository.loadActiveOrganizationRole(
		queryContext,
		tenantContext,
	)
	if err != nil {
		return ListClassesResult{}, err
	}
	listAccess := AccessContext{
		TenantID:          tenantContext.TenantID,
		ActorID:           tenantContext.ActorID,
		MembershipActive:  true,
		OrganizationRoles: []policy.OrganizationRole{organizationRole},
	}
	includeAll := repository.authorizer != nil && repository.authorizer.Authorize(policy.Input{
		Subject: viewerSubject(listAccess),
		Action:  policy.ActionClassView,
		Resource: policy.Resource{
			TenantID: tenantContext.TenantID,
			State:    policy.ResourceStateUnknown,
		},
	}).Allowed

	var status any
	if params.Status != nil {
		status = string(*params.Status)
	}
	var afterCreatedAt any
	var afterID any
	if params.After != nil {
		afterCreatedAt = params.After.CreatedAt.UTC()
		afterID = params.After.ID
	}
	rows, err := repository.database.Query(
		queryContext,
		`SELECT id, tenant_id, owner_user_id, code, title, description, timezone,
       status, version, created_at, updated_at, archived_at
FROM tutorhub.classes
WHERE tenant_id = $1
  AND ($2::text IS NULL OR status = $2)
  AND (
      $3::timestamptz IS NULL
      OR (created_at, id) < ($3::timestamptz, $4::uuid)
  )
  AND (
      $5::boolean
      OR owner_user_id = $6
      OR EXISTS (
          SELECT 1
          FROM tutorhub.class_enrollments AS enrollment
          WHERE enrollment.tenant_id = tutorhub.classes.tenant_id
            AND enrollment.class_id = tutorhub.classes.id
            AND enrollment.user_id = $6
            AND enrollment.status = $7
      )
  )
ORDER BY created_at DESC, id DESC
LIMIT $8`,
		tenantContext.TenantID,
		status,
		afterCreatedAt,
		afterID,
		includeAll,
		tenantContext.ActorID,
		EnrollmentStatusActive,
		params.Limit+1,
	)
	if err != nil {
		return ListClassesResult{}, fmt.Errorf("list classes: %w", err)
	}
	defer rows.Close()

	classes := make([]Class, 0, params.Limit+1)
	for rows.Next() {
		class, err := scanClass(rows)
		if err != nil {
			return ListClassesResult{}, fmt.Errorf("scan class list: %w", err)
		}
		classes = append(classes, class)
	}
	if err := rows.Err(); err != nil {
		return ListClassesResult{}, fmt.Errorf("iterate class list: %w", err)
	}
	rows.Close()

	result := ListClassesResult{Items: classes}
	if len(classes) > params.Limit {
		result.HasMore = true
		result.Items = classes[:params.Limit]
	}
	classIDs := make([]uuid.UUID, 0, len(result.Items))
	for _, class := range result.Items {
		classIDs = append(classIDs, class.ID)
	}
	enrollments, err := repository.ListActorEnrollments(
		queryContext,
		tenantContext,
		classIDs,
	)
	if err != nil {
		return ListClassesResult{}, fmt.Errorf("list actor class enrollments: %w", err)
	}
	enrollmentByClassID := make(map[uuid.UUID]*Enrollment, len(enrollments))
	for index := range enrollments {
		enrollment := &enrollments[index]
		enrollmentByClassID[enrollment.ClassID] = enrollment
	}
	for index := range result.Items {
		result.Items[index], _ = projectClassViewerAccess(
			repository.authorizer,
			listAccess,
			result.Items[index],
			enrollmentByClassID[result.Items[index].ID],
		)
	}
	return result, nil
}

func (repository *PostgresRepository) loadActiveOrganizationRole(
	ctx context.Context,
	tenantContext tenancy.Context,
) (policy.OrganizationRole, error) {
	if repository.authorizer == nil {
		return "", ErrClassAccessDenied
	}
	var role policy.OrganizationRole
	err := repository.database.QueryRow(
		ctx,
		`SELECT membership.role
FROM tutorhub.memberships AS membership
JOIN tutorhub.tenants AS tenant ON tenant.id = membership.tenant_id
WHERE membership.tenant_id = $1
  AND membership.user_id = $2
  AND membership.status = 'active'
  AND tenant.status = 'active'`,
		tenantContext.TenantID,
		tenantContext.ActorID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrClassAccessDenied
	}
	if err != nil {
		return "", fmt.Errorf("load active class-list membership: %w", err)
	}
	return role, nil
}

func (repository *PostgresRepository) Update(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params UpdateClassParams,
	updatedAt time.Time,
) (Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return Class{}, ErrClassAccessDenied
	}
	params, err := params.normalized()
	if err != nil {
		return Class{}, err
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Class{}, fmt.Errorf("begin class update: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	locked, membership, err := repository.lockClassMutation(
		queryContext,
		transaction,
		tenantContext,
		classID,
	)
	if err != nil {
		return Class{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext,
		membership,
		locked.Class,
		policy.ActionClassUpdate,
	); err != nil {
		return Class{}, err
	}
	if locked.Class.Version != params.ExpectedVersion {
		return Class{}, ErrClassVersionConflict
	}
	if params.Status != nil {
		if err := validateDirectStatusTransition(locked.Class.Status, *params.Status); err != nil {
			return Class{}, err
		}
	}

	class, err := scanClass(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.classes
SET code = COALESCE($3, code),
    title = COALESCE($4, title),
    description = COALESCE($5, description),
    timezone = COALESCE($6, timezone),
    status = COALESCE($7, status),
    version = version + 1,
    updated_at = $8
WHERE tenant_id = $1 AND id = $2 AND version = $9
RETURNING id, tenant_id, owner_user_id, code, title, description, timezone,
          status, version, created_at, updated_at, archived_at`,
		tenantContext.TenantID,
		classID,
		nullableClassString(params.Code),
		nullableClassString(params.Title),
		nullableClassString(params.Description),
		nullableClassString(params.Timezone),
		nullableClassStatus(params.Status),
		updatedAt,
		params.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Class{}, ErrClassVersionConflict
	}
	if err != nil {
		return Class{}, mapClassPostgresError("update class", err)
	}
	if err := insertClassEvent(
		queryContext,
		transaction,
		class,
		tenantContext.ActorID,
		"class.updated",
		classEventDetails{},
		updatedAt,
	); err != nil {
		return Class{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Class{}, fmt.Errorf("commit class update: %w", err)
	}
	return class, nil
}

func (repository *PostgresRepository) Archive(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	expectedVersion int64,
	archivedAt time.Time,
) (Class, error) {
	return repository.changeArchiveState(
		ctx,
		tenantContext,
		classID,
		expectedVersion,
		archivedAt,
		true,
	)
}

func (repository *PostgresRepository) Restore(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	expectedVersion int64,
	restoredAt time.Time,
) (Class, error) {
	return repository.changeArchiveState(
		ctx,
		tenantContext,
		classID,
		expectedVersion,
		restoredAt,
		false,
	)
}

func (repository *PostgresRepository) changeArchiveState(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	expectedVersion int64,
	changedAt time.Time,
	archive bool,
) (Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return Class{}, ErrClassAccessDenied
	}
	if expectedVersion < 1 {
		return Class{}, fmt.Errorf("%w: expected version is required", ErrInvalidClassInput)
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Class{}, fmt.Errorf("begin class lifecycle mutation: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	locked, membership, err := repository.lockClassMutation(
		queryContext,
		transaction,
		tenantContext,
		classID,
	)
	if err != nil {
		return Class{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext,
		membership,
		locked.Class,
		policy.ActionClassArchive,
	); err != nil {
		return Class{}, err
	}
	if locked.Class.Version != expectedVersion {
		return Class{}, ErrClassVersionConflict
	}

	var class Class
	var eventType string
	var fromStatus ClassStatus
	var toStatus ClassStatus
	if archive {
		if locked.Class.Status == ClassStatusArchived {
			return Class{}, ErrInvalidClassTransition
		}
		fromStatus = locked.Class.Status
		toStatus = ClassStatusArchived
		eventType = "class.archived"
		class, err = scanClass(transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.classes
SET status = 'archived',
    archived_from_status = status,
    archived_at = $4,
    version = version + 1,
    updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, tenant_id, owner_user_id, code, title, description, timezone,
          status, version, created_at, updated_at, archived_at`,
			tenantContext.TenantID,
			classID,
			expectedVersion,
			changedAt,
		))
	} else {
		if locked.Class.Status != ClassStatusArchived ||
			locked.ArchivedFromStatus == nil ||
			(*locked.ArchivedFromStatus != ClassStatusDraft &&
				*locked.ArchivedFromStatus != ClassStatusActive) {
			return Class{}, ErrInvalidClassTransition
		}
		fromStatus = ClassStatusArchived
		toStatus = *locked.ArchivedFromStatus
		eventType = "class.restored"
		class, err = scanClass(transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.classes
SET status = archived_from_status,
    archived_from_status = NULL,
    archived_at = NULL,
    version = version + 1,
    updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, tenant_id, owner_user_id, code, title, description, timezone,
          status, version, created_at, updated_at, archived_at`,
			tenantContext.TenantID,
			classID,
			expectedVersion,
			changedAt,
		))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Class{}, ErrClassVersionConflict
	}
	if err != nil {
		return Class{}, mapClassPostgresError("change class archive state", err)
	}
	if err := insertClassEvent(
		queryContext,
		transaction,
		class,
		tenantContext.ActorID,
		eventType,
		classEventDetails{FromStatus: fromStatus, ToStatus: toStatus},
		changedAt,
	); err != nil {
		return Class{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Class{}, fmt.Errorf("commit class lifecycle mutation: %w", err)
	}
	return class, nil
}

func (repository *PostgresRepository) TransferOwnership(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params TransferClassOwnershipParams,
	transferredAt time.Time,
) (Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return Class{}, ErrClassAccessDenied
	}
	params, err := params.normalized()
	if err != nil {
		return Class{}, err
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Class{}, fmt.Errorf("begin class ownership transfer: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if _, err := lockActiveClassTenant(
		queryContext,
		transaction,
		tenantContext.TenantID,
	); err != nil {
		return Class{}, err
	}
	locked, err := lockTenantClass(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
	)
	if err != nil {
		return Class{}, err
	}
	memberships, err := lockClassMemberships(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
		params.NewOwnerUserID,
	)
	if err != nil {
		return Class{}, err
	}
	actorMembership, actorFound := memberships[tenantContext.ActorID]
	if !actorFound || !actorMembership.Active {
		return Class{}, ErrClassAccessDenied
	}
	actorMembership.ClassRole, err = lockActiveClassEnrollmentRole(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
	)
	if err != nil {
		return Class{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext,
		actorMembership,
		locked.Class,
		policy.ActionClassTransferOwnership,
	); err != nil {
		return Class{}, err
	}
	if locked.Class.Version != params.ExpectedVersion {
		return Class{}, ErrClassVersionConflict
	}
	if locked.Class.OwnerUserID == params.NewOwnerUserID {
		if err := transaction.Commit(queryContext); err != nil {
			return Class{}, fmt.Errorf("commit unchanged class ownership: %w", err)
		}
		return locked.Class, nil
	}
	targetMembership, targetFound := memberships[params.NewOwnerUserID]
	if !targetFound || !targetMembership.Active ||
		!repository.canOwnClass(
			params.NewOwnerUserID,
			tenantContext.TenantID,
			targetMembership.Role,
		) {
		return Class{}, ErrClassOwnerUnavailable
	}

	previousOwnerID := locked.Class.OwnerUserID
	class, err := scanClass(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.classes
SET owner_user_id = $4,
    version = version + 1,
    updated_at = $5
WHERE tenant_id = $1 AND id = $2 AND version = $3
RETURNING id, tenant_id, owner_user_id, code, title, description, timezone,
          status, version, created_at, updated_at, archived_at`,
		tenantContext.TenantID,
		classID,
		params.ExpectedVersion,
		params.NewOwnerUserID,
		transferredAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Class{}, ErrClassVersionConflict
	}
	if err != nil {
		return Class{}, mapClassPostgresError("transfer class ownership", err)
	}
	if err := insertClassEvent(
		queryContext,
		transaction,
		class,
		tenantContext.ActorID,
		"class.ownership_transferred",
		classEventDetails{
			PreviousOwnerUserID: previousOwnerID,
			OwnerUserID:         class.OwnerUserID,
		},
		transferredAt,
	); err != nil {
		return Class{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Class{}, fmt.Errorf("commit class ownership transfer: %w", err)
	}
	return class, nil
}

type lockedClass struct {
	Class
	ArchivedFromStatus *ClassStatus
}

type lockedClassMembership struct {
	Role      string
	Active    bool
	ClassRole *policy.ClassRole
}

func (repository *PostgresRepository) lockClassMutation(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
) (lockedClass, lockedClassMembership, error) {
	if _, err := lockActiveClassTenant(
		ctx,
		transaction,
		tenantContext.TenantID,
	); err != nil {
		return lockedClass{}, lockedClassMembership{}, err
	}
	class, err := lockTenantClass(ctx, transaction, tenantContext.TenantID, classID)
	if err != nil {
		return lockedClass{}, lockedClassMembership{}, err
	}
	membership, found, err := lockClassMembership(
		ctx,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
	)
	if err != nil {
		return lockedClass{}, lockedClassMembership{}, err
	}
	if !found || !membership.Active {
		return lockedClass{}, lockedClassMembership{}, ErrClassAccessDenied
	}
	membership.ClassRole, err = lockActiveClassEnrollmentRole(
		ctx,
		transaction,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
	)
	if err != nil {
		return lockedClass{}, lockedClassMembership{}, err
	}
	return class, membership, nil
}

func lockActiveClassEnrollmentRole(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	userID uuid.UUID,
) (*policy.ClassRole, error) {
	var role policy.ClassRole
	var status EnrollmentStatus
	err := transaction.QueryRow(
		ctx,
		`SELECT class_role, status
FROM tutorhub.class_enrollments
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
FOR SHARE`,
		tenantID,
		classID,
		userID,
	).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock class enrollment role: %w", err)
	}
	if status != EnrollmentStatusActive {
		return nil, nil
	}
	return &role, nil
}

func lockActiveClassTenant(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
) (string, error) {
	var timezone string
	if err := transaction.QueryRow(
		ctx,
		`SELECT timezone
FROM tutorhub.tenants
WHERE id = $1 AND status = 'active'
FOR SHARE`,
		tenantID,
	).Scan(&timezone); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrClassNotFound
		}
		return "", fmt.Errorf("lock class tenant: %w", err)
	}
	return timezone, nil
}

func lockTenantClass(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
) (lockedClass, error) {
	var class lockedClass
	var archivedFromStatus *string
	err := transaction.QueryRow(
		ctx,
		`SELECT id, tenant_id, owner_user_id, code, title, description, timezone,
       status, version, created_at, updated_at, archived_at, archived_from_status
FROM tutorhub.classes
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`,
		tenantID,
		classID,
	).Scan(
		&class.ID,
		&class.TenantID,
		&class.OwnerUserID,
		&class.Code,
		&class.Title,
		&class.Description,
		&class.Timezone,
		&class.Status,
		&class.Version,
		&class.CreatedAt,
		&class.UpdatedAt,
		&class.ArchivedAt,
		&archivedFromStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedClass{}, ErrClassNotFound
	}
	if err != nil {
		return lockedClass{}, fmt.Errorf("lock class: %w", err)
	}
	if archivedFromStatus != nil {
		status := ClassStatus(*archivedFromStatus)
		class.ArchivedFromStatus = &status
	}
	return class, nil
}

func lockClassMembership(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	userID uuid.UUID,
) (lockedClassMembership, bool, error) {
	var membership lockedClassMembership
	var status string
	err := transaction.QueryRow(
		ctx,
		`SELECT role, status
FROM tutorhub.memberships
WHERE tenant_id = $1 AND user_id = $2
FOR SHARE`,
		tenantID,
		userID,
	).Scan(&membership.Role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedClassMembership{}, false, nil
	}
	if err != nil {
		return lockedClassMembership{}, false, fmt.Errorf("lock class membership: %w", err)
	}
	membership.Active = status == "active"
	return membership, true, nil
}

func lockClassMemberships(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	firstUserID uuid.UUID,
	secondUserID uuid.UUID,
) (map[uuid.UUID]lockedClassMembership, error) {
	userIDs := []uuid.UUID{firstUserID}
	if secondUserID != firstUserID {
		userIDs = append(userIDs, secondUserID)
	}
	if len(userIDs) == 2 && userIDs[1].String() < userIDs[0].String() {
		userIDs[0], userIDs[1] = userIDs[1], userIDs[0]
	}

	memberships := make(map[uuid.UUID]lockedClassMembership, len(userIDs))
	for _, userID := range userIDs {
		membership, found, err := lockClassMembership(ctx, transaction, tenantID, userID)
		if err != nil {
			return nil, err
		}
		if found {
			memberships[userID] = membership
		}
	}
	return memberships, nil
}

func (repository *PostgresRepository) authorizeLockedClassCreation(
	actorID uuid.UUID,
	tenantID uuid.UUID,
	role string,
) error {
	if repository.authorizer == nil {
		return ErrClassAccessDenied
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID:          actorID,
			ActiveTenantID:   tenantID,
			MembershipActive: true,
			OrganizationRoles: []policy.OrganizationRole{
				policy.OrganizationRole(role),
			},
		},
		Action: policy.ActionClassCreate,
		Resource: policy.Resource{
			TenantID: tenantID,
			State:    policy.ResourceStateActive,
		},
	})
	if !decision.Allowed {
		return ErrClassAccessDenied
	}
	return nil
}

func (repository *PostgresRepository) authorizeLockedClass(
	tenantContext tenancy.Context,
	membership lockedClassMembership,
	class Class,
	action policy.Action,
) error {
	if repository.authorizer == nil {
		return ErrClassAccessDenied
	}
	classRoles := []policy.ClassRole{}
	if class.OwnerUserID == tenantContext.ActorID {
		classRoles = append(classRoles, policy.ClassRoleOwner)
	}
	if membership.ClassRole != nil {
		classRoles = append(classRoles, *membership.ClassRole)
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID:          tenantContext.ActorID,
			ActiveTenantID:   tenantContext.TenantID,
			MembershipActive: true,
			OrganizationRoles: []policy.OrganizationRole{
				policy.OrganizationRole(membership.Role),
			},
			ClassRoles: classRoles,
		},
		Action: action,
		Resource: policy.Resource{
			TenantID: class.TenantID,
			ClassID:  class.ID,
			State:    policy.ResourceState(class.Status),
		},
	})
	if !decision.Allowed {
		if decision.ConcealResource {
			return ErrClassNotFound
		}
		if decision.Reason == policy.DenialResourceState {
			return ErrInvalidClassTransition
		}
		return ErrClassAccessDenied
	}
	return nil
}

func (repository *PostgresRepository) canOwnClass(
	userID uuid.UUID,
	tenantID uuid.UUID,
	role string,
) bool {
	if repository.authorizer == nil {
		return false
	}
	return repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID:          userID,
			ActiveTenantID:   tenantID,
			MembershipActive: true,
			OrganizationRoles: []policy.OrganizationRole{
				policy.OrganizationRole(role),
			},
		},
		Action: policy.ActionClassCreate,
		Resource: policy.Resource{
			TenantID: tenantID,
			State:    policy.ResourceStateActive,
		},
	}).Allowed
}

type rowScanner interface {
	Scan(...any) error
}

func scanClass(row rowScanner) (Class, error) {
	var class Class
	if err := row.Scan(
		&class.ID,
		&class.TenantID,
		&class.OwnerUserID,
		&class.Code,
		&class.Title,
		&class.Description,
		&class.Timezone,
		&class.Status,
		&class.Version,
		&class.CreatedAt,
		&class.UpdatedAt,
		&class.ArchivedAt,
	); err != nil {
		return Class{}, err
	}
	return class, nil
}

type classEventDetails struct {
	PreviousOwnerUserID uuid.UUID
	OwnerUserID         uuid.UUID
	FromStatus          ClassStatus
	ToStatus            ClassStatus
}

func insertClassEvent(
	ctx context.Context,
	transaction pgx.Tx,
	class Class,
	actorID uuid.UUID,
	eventType string,
	details classEventDetails,
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
    'class',
    $2,
    $3,
    jsonb_strip_nulls(jsonb_build_object(
        'class_id', $2::uuid,
        'actor_user_id', $4::uuid,
        'version', $5::bigint,
        'status', $6::text,
        'previous_owner_user_id', $7::uuid,
        'owner_user_id', $8::uuid,
        'from_status', $9::text,
        'to_status', $10::text
    )),
    $11,
    $11
)`,
		class.TenantID,
		class.ID,
		eventType,
		actorID,
		class.Version,
		string(class.Status),
		nullableClassUUID(details.PreviousOwnerUserID),
		nullableClassUUID(details.OwnerUserID),
		nullableClassStatusValue(details.FromStatus),
		nullableClassStatusValue(details.ToStatus),
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}
	return nil
}

func nullableClassString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableClassStatus(value *ClassStatus) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func nullableClassStatusValue(value ClassStatus) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func nullableClassUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func (repository *PostgresRepository) contextWithTimeout(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if repository.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, repository.queryTimeout)
}

func mapClassPostgresError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}

	switch postgresError.ConstraintName {
	case "classes_tenant_code_unique":
		return ErrDuplicateClassCode
	case "classes_owner_membership_fk":
		return ErrClassOwnerUnavailable
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func rollbackClassTransaction(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}
