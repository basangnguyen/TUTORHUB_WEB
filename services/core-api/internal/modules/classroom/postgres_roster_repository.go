package classroom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const normalizedRosterDisplayNameSQL = `lower(
    regexp_replace(btrim(app_user.display_name), '[[:space:]]+', ' ', 'g')
) COLLATE "C"`

func (repository *PostgresRepository) ListRoster(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params ListRosterParams,
) (ListRosterResult, error) {
	if err := tenantContext.Validate(); err != nil {
		return ListRosterResult{}, ErrEnrollmentAccessDenied
	}
	if classID == uuid.Nil || params.Limit < 1 || params.Limit > maximumRosterLimit ||
		len([]rune(params.Search)) > maximumRosterSearchLength {
		return ListRosterResult{}, ErrInvalidEnrollmentInput
	}
	if params.Search != strings.ToLower(strings.TrimSpace(params.Search)) {
		return ListRosterResult{}, ErrInvalidEnrollmentInput
	}
	if params.Status != nil && !validEnrollmentStatus(*params.Status) {
		return ListRosterResult{}, ErrInvalidEnrollmentInput
	}
	if params.After != nil && !validRosterCursor(*params.After) {
		return ListRosterResult{}, ErrInvalidRosterCursor
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ListRosterResult{}, fmt.Errorf("begin class roster list: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	class, actorMembership, actorEnrollment, err := repository.lockEnrollmentManagerContext(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return ListRosterResult{}, err
	}
	managementGranted := repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID,
		tenantContext.TenantID,
		actorMembership,
		actorEnrollment,
		class,
	)
	if !managementGranted {
		return ListRosterResult{}, ErrEnrollmentAccessDenied
	}

	owner, err := readRosterOwner(
		queryContext, transaction, tenantContext.TenantID, class.OwnerUserID,
	)
	if err != nil {
		return ListRosterResult{}, err
	}
	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}
	afterSortName := ""
	afterUserID := uuid.Nil
	hasAfter := params.After != nil
	if hasAfter {
		afterUserID = params.After.UserID
		err := transaction.QueryRow(
			queryContext,
			`SELECT `+normalizedRosterDisplayNameSQL+`
FROM tutorhub.class_enrollments AS enrollment
JOIN tutorhub.classes AS class
  ON class.tenant_id = enrollment.tenant_id AND class.id = enrollment.class_id
JOIN tutorhub.users AS app_user ON app_user.id = enrollment.user_id
WHERE enrollment.tenant_id = $1
  AND enrollment.class_id = $2
  AND enrollment.user_id = $3
  AND enrollment.user_id <> class.owner_user_id
  AND ($4::text = '' OR enrollment.status = $4)
  AND (
      $5::text = ''
      OR strpos(`+normalizedRosterDisplayNameSQL+`, $5) > 0
      OR strpos(app_user.email, $5) > 0
  )`,
			tenantContext.TenantID,
			classID,
			afterUserID,
			status,
			params.Search,
		).Scan(&afterSortName)
		if errors.Is(err, pgx.ErrNoRows) {
			return ListRosterResult{}, ErrInvalidRosterCursor
		}
		if err != nil {
			return ListRosterResult{}, fmt.Errorf("resolve class roster cursor: %w", err)
		}
	}

	rows, err := transaction.Query(
		queryContext,
		`SELECT `+prefixedEnrollmentColumns("enrollment")+`,
       app_user.display_name,
       app_user.email,
       membership.role,
       membership.status
FROM tutorhub.class_enrollments AS enrollment
JOIN tutorhub.classes AS class
  ON class.tenant_id = enrollment.tenant_id AND class.id = enrollment.class_id
JOIN tutorhub.users AS app_user ON app_user.id = enrollment.user_id
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = enrollment.tenant_id
 AND membership.user_id = enrollment.user_id
WHERE enrollment.tenant_id = $1
  AND enrollment.class_id = $2
  AND enrollment.user_id <> class.owner_user_id
  AND ($3::text = '' OR enrollment.status = $3)
  AND (
      $4::text = ''
      OR strpos(`+normalizedRosterDisplayNameSQL+`, $4) > 0
      OR strpos(app_user.email, $4) > 0
  )
  AND (
      $5::boolean = false
      OR (`+normalizedRosterDisplayNameSQL+`, enrollment.user_id) >
         ($6::text COLLATE "C", $7::uuid)
  )
ORDER BY `+normalizedRosterDisplayNameSQL+` ASC, enrollment.user_id ASC
LIMIT $8`,
		tenantContext.TenantID,
		classID,
		status,
		params.Search,
		hasAfter,
		afterSortName,
		afterUserID,
		params.Limit+1,
	)
	if err != nil {
		return ListRosterResult{}, fmt.Errorf("list class roster: %w", err)
	}
	defer rows.Close()

	actorOrganizationRoles := []policy.OrganizationRole{
		policy.OrganizationRole(actorMembership.Role),
	}
	actorClassRoles := activeRosterClassRoles(
		tenantContext.ActorID, class, actorEnrollment,
	)
	items := make([]RosterMember, 0, params.Limit+1)
	for rows.Next() {
		member, targetMembership, err := scanRosterMember(rows)
		if err != nil {
			return ListRosterResult{}, fmt.Errorf("scan class roster: %w", err)
		}
		member.Actions = rosterMemberActions(
			class,
			tenantContext.ActorID,
			managementGranted,
			actorOrganizationRoles,
			actorClassRoles,
			targetMembership,
			member.Enrollment,
		)
		items = append(items, member)
	}
	if err := rows.Err(); err != nil {
		return ListRosterResult{}, fmt.Errorf("iterate class roster: %w", err)
	}
	rows.Close()

	hasMore := len(items) > params.Limit
	if hasMore {
		items = items[:params.Limit]
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ListRosterResult{}, fmt.Errorf("commit class roster list: %w", err)
	}
	return ListRosterResult{Owner: owner, Items: items, HasMore: hasMore}, nil
}

func (repository *PostgresRepository) UpdateRosterRole(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	userID uuid.UUID,
	params UpdateRosterRoleParams,
) (EnrollmentMutationResult, error) {
	if err := tenantContext.Validate(); err != nil {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if classID == uuid.Nil || userID == uuid.Nil || params.ChangedAt.IsZero() ||
		!validPersistedClassRole(params.ClassRole) ||
		(params.Source != "roster_single" && params.Source != "roster_bulk") {
		return EnrollmentMutationResult{}, ErrInvalidEnrollmentInput
	}
	changedAt := params.ChangedAt.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("begin class roster role change: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if _, err := lockActiveClassTenant(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	class, err := lockEnrollmentClassForShare(
		queryContext, transaction, tenantContext.TenantID, classID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	preflightMembership, found, err := lockClassMembership(
		queryContext, transaction, tenantContext.TenantID, tenantContext.ActorID,
	)
	if err != nil || !found || !preflightMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	preflightEnrollment, err := findEnrollmentWithoutLock(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID,
		tenantContext.TenantID,
		preflightMembership,
		preflightEnrollment,
		class,
	) {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if class.Status != ClassStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}

	memberships, err := lockClassMemberships(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
		userID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	actorMembership, actorFound := memberships[tenantContext.ActorID]
	targetMembership, targetFound := memberships[userID]
	if !actorFound || !actorMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if !targetFound {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	if !targetMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	enrollments, err := lockEnrollmentUsers(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
		userID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	actorEnrollment := enrollments[tenantContext.ActorID]
	targetEnrollment := enrollments[userID]
	if targetEnrollment == nil {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	if targetEnrollment.Status != EnrollmentStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	if err := repository.authorizeRosterTargetMutation(
		tenantContext,
		class,
		actorMembership,
		actorEnrollment,
		userID,
		targetMembership,
		targetEnrollment,
		policy.RosterMutationUpdateRole,
		params.ClassRole,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	if targetEnrollment.ClassRole == params.ClassRole {
		if err := transaction.Commit(queryContext); err != nil {
			return EnrollmentMutationResult{}, fmt.Errorf("commit unchanged roster role: %w", err)
		}
		return EnrollmentMutationResult{Enrollment: *targetEnrollment}, nil
	}

	previousRole := targetEnrollment.ClassRole
	updated, err := scanEnrollment(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.class_enrollments
SET class_role = $4, updated_at = $5
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3 AND status = 'active'
RETURNING `+enrollmentSelectColumns,
		tenantContext.TenantID,
		classID,
		userID,
		params.ClassRole,
		changedAt,
	))
	if err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("update class roster role: %w", err)
	}
	if err := insertEnrollmentRoleChangedEvent(
		queryContext,
		transaction,
		updated,
		previousRole,
		tenantContext.ActorID,
		params.Source,
		changedAt,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("commit class roster role change: %w", err)
	}
	return EnrollmentMutationResult{Enrollment: updated, Changed: true}, nil
}

type rosterTargetMembership struct {
	Role   policy.OrganizationRole
	Active bool
}

func readRosterOwner(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	ownerUserID uuid.UUID,
) (RosterOwner, error) {
	var owner RosterOwner
	if err := transaction.QueryRow(
		ctx,
		`SELECT app_user.id, app_user.display_name, app_user.email
FROM tutorhub.memberships AS membership
JOIN tutorhub.users AS app_user ON app_user.id = membership.user_id
WHERE membership.tenant_id = $1 AND membership.user_id = $2`,
		tenantID,
		ownerUserID,
	).Scan(&owner.User.ID, &owner.User.DisplayName, &owner.User.Email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RosterOwner{}, ErrClassNotFound
		}
		return RosterOwner{}, fmt.Errorf("read class roster owner: %w", err)
	}
	return owner, nil
}

func scanRosterMember(row enrollmentRowScanner) (RosterMember, rosterTargetMembership, error) {
	var member RosterMember
	var membershipStatus string
	var targetMembership rosterTargetMembership
	if err := row.Scan(
		&member.Enrollment.ID,
		&member.Enrollment.TenantID,
		&member.Enrollment.ClassID,
		&member.Enrollment.UserID,
		&member.Enrollment.ClassRole,
		&member.Enrollment.Status,
		&member.Enrollment.EnrolledBy,
		&member.Enrollment.JoinedAt,
		&member.Enrollment.SuspendedAt,
		&member.Enrollment.LeftAt,
		&member.Enrollment.RemovedAt,
		&member.Enrollment.CreatedAt,
		&member.Enrollment.UpdatedAt,
		&member.User.DisplayName,
		&member.User.Email,
		&targetMembership.Role,
		&membershipStatus,
	); err != nil {
		return RosterMember{}, rosterTargetMembership{}, err
	}
	member.User.ID = member.Enrollment.UserID
	targetMembership.Active = membershipStatus == "active"
	return member, targetMembership, nil
}

func activeRosterClassRoles(
	actorID uuid.UUID,
	class Class,
	enrollment *Enrollment,
) []policy.ClassRole {
	roles := make([]policy.ClassRole, 0, 2)
	if class.OwnerUserID == actorID {
		roles = append(roles, policy.ClassRoleOwner)
	}
	if enrollment != nil && enrollment.Status == EnrollmentStatusActive {
		roles = append(roles, enrollment.ClassRole)
	}
	return roles
}

func rosterMemberActions(
	class Class,
	actorID uuid.UUID,
	managementGranted bool,
	actorOrganizationRoles []policy.OrganizationRole,
	actorClassRoles []policy.ClassRole,
	targetMembership rosterTargetMembership,
	enrollment Enrollment,
) RosterMemberActions {
	actions := RosterMemberActions{AssignableRoles: []policy.ClassRole{}}
	if class.Status != ClassStatusActive {
		return actions
	}
	if enrollment.Status == EnrollmentStatusActive ||
		enrollment.Status == EnrollmentStatusSuspended ||
		enrollment.Status == EnrollmentStatusLeft {
		actions.CanRemove = policy.CanMutateRoster(policy.RosterMutationInput{
			ManagementGranted:       managementGranted,
			Action:                  policy.RosterMutationRemove,
			ActorID:                 actorID,
			TargetUserID:            enrollment.UserID,
			ActorOrganizationRoles:  actorOrganizationRoles,
			ActorClassRoles:         actorClassRoles,
			TargetOrganizationRoles: []policy.OrganizationRole{targetMembership.Role},
			TargetClassRole:         enrollment.ClassRole,
		}).Allowed
	}
	if !targetMembership.Active || enrollment.Status != EnrollmentStatusActive {
		return actions
	}
	for _, role := range []policy.ClassRole{
		policy.ClassRoleCoTeacher,
		policy.ClassRoleTeachingAssistant,
		policy.ClassRoleStudent,
	} {
		if role == enrollment.ClassRole {
			continue
		}
		if policy.CanMutateRoster(policy.RosterMutationInput{
			ManagementGranted:       managementGranted,
			Action:                  policy.RosterMutationUpdateRole,
			ActorID:                 actorID,
			TargetUserID:            enrollment.UserID,
			ActorOrganizationRoles:  actorOrganizationRoles,
			ActorClassRoles:         actorClassRoles,
			TargetOrganizationRoles: []policy.OrganizationRole{targetMembership.Role},
			TargetClassRole:         enrollment.ClassRole,
			DesiredClassRole:        role,
		}).Allowed {
			actions.AssignableRoles = append(actions.AssignableRoles, role)
		}
	}
	actions.CanSuspend = policy.CanMutateRoster(policy.RosterMutationInput{
		ManagementGranted:       managementGranted,
		Action:                  policy.RosterMutationSuspend,
		ActorID:                 actorID,
		TargetUserID:            enrollment.UserID,
		ActorOrganizationRoles:  actorOrganizationRoles,
		ActorClassRoles:         actorClassRoles,
		TargetOrganizationRoles: []policy.OrganizationRole{targetMembership.Role},
		TargetClassRole:         enrollment.ClassRole,
	}).Allowed
	return actions
}

func (repository *PostgresRepository) authorizeRosterTargetMutation(
	tenantContext tenancy.Context,
	class Class,
	actorMembership lockedClassMembership,
	actorEnrollment *Enrollment,
	targetUserID uuid.UUID,
	targetMembership lockedClassMembership,
	targetEnrollment *Enrollment,
	action policy.RosterMutationAction,
	desiredRole policy.ClassRole,
) error {
	managementGranted := repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID,
		tenantContext.TenantID,
		actorMembership,
		actorEnrollment,
		class,
	)
	if !managementGranted {
		return ErrEnrollmentAccessDenied
	}
	if targetEnrollment == nil {
		return ErrEnrollmentNotFound
	}
	decision := policy.CanMutateRoster(policy.RosterMutationInput{
		ManagementGranted:       managementGranted,
		Action:                  action,
		ActorID:                 tenantContext.ActorID,
		TargetUserID:            targetUserID,
		ActorOrganizationRoles:  []policy.OrganizationRole{policy.OrganizationRole(actorMembership.Role)},
		ActorClassRoles:         activeRosterClassRoles(tenantContext.ActorID, class, actorEnrollment),
		TargetOrganizationRoles: []policy.OrganizationRole{policy.OrganizationRole(targetMembership.Role)},
		TargetClassRole:         targetEnrollment.ClassRole,
		TargetIsOwner:           class.OwnerUserID == targetUserID,
		DesiredClassRole:        desiredRole,
	})
	if !decision.Allowed {
		return ErrEnrollmentConflict
	}
	return nil
}

func insertEnrollmentRoleChangedEvent(
	ctx context.Context,
	transaction pgx.Tx,
	enrollment Enrollment,
	previousRole policy.ClassRole,
	actorID uuid.UUID,
	source string,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'class_enrollment', $2, 'class.enrollment.role_changed',
    jsonb_build_object(
        'class_id', $3::uuid,
        'user_id', $4::uuid,
        'actor_user_id', $5::uuid,
        'previous_class_role', $6::text,
        'class_role', $7::text,
        'status', $8::text,
        'source', $9::text
    ),
    $10, $10
)`,
		enrollment.TenantID,
		enrollment.ID,
		enrollment.ClassID,
		enrollment.UserID,
		actorID,
		previousRole,
		enrollment.ClassRole,
		enrollment.Status,
		source,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert class enrollment role-changed event: %w", err)
	}
	return nil
}
