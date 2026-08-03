package conversation

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const defaultQueryTimeout = 10 * time.Second

type transactionDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     transactionDatabase
	queryTimeout time.Duration
	authorizer   policy.Authorizer
	controls     featurecontrol.Enforcer
}

func NewPostgresRepository(
	database transactionDatabase,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	controls featurecontrol.Enforcer,
) (*PostgresRepository, error) {
	if database == nil || authorizer == nil || controls == nil {
		return nil, fmt.Errorf("conversation database, authorizer, and feature controls are required")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer, controls: controls,
	}, nil
}

func (repository *PostgresRepository) List(
	ctx context.Context,
	access AccessContext,
	input ListInput,
	cursor listCursor,
) ([]Conversation, bool, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, false, fmt.Errorf("begin conversation list: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return nil, false, err
	}
	includeAllClasses := repository.authorizer.Authorize(policy.Input{
		Subject: subject(access),
		Action:  policy.ActionClassView,
		Resource: policy.Resource{
			TenantID: access.TenantID,
			State:    policy.ResourceStateUnknown,
		},
	}).Allowed
	var kind any
	if input.Kind != nil {
		kind = string(*input.Kind)
	}
	var afterUpdatedAt any
	var afterID any
	if !cursor.UpdatedAt.IsZero() {
		afterUpdatedAt = cursor.UpdatedAt.UTC()
		afterID = cursor.ID
	}
	rows, err := transaction.Query(
		queryContext,
		conversationSelect+`
WHERE conversation.tenant_id = $1
  AND ($2::text IS NULL OR conversation.kind = $2)
  AND (
      $3::timestamptz IS NULL
      OR (conversation.updated_at, conversation.id) < ($3::timestamptz, $4::uuid)
  )
  AND (
      (
          conversation.kind = 'direct'
          AND $5 IN (conversation.direct_user_low_id, conversation.direct_user_high_id)
      )
      OR (
          conversation.kind = 'class'
          AND (
              $6::boolean
              OR class.owner_user_id = $5
              OR (
                  actor_enrollment.user_id = $5
                  AND actor_enrollment.status = 'active'
              )
          )
      )
  )
ORDER BY conversation.updated_at DESC, conversation.id DESC
LIMIT $7`,
		access.TenantID,
		kind,
		afterUpdatedAt,
		afterID,
		access.ActorID,
		includeAllClasses,
		input.Limit+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("query conversation list: %w", err)
	}
	defer rows.Close()
	items := make([]Conversation, 0, input.Limit+1)
	for rows.Next() {
		row, scanErr := scanConversationRow(rows)
		if scanErr != nil {
			return nil, false, fmt.Errorf("scan conversation list: %w", scanErr)
		}
		item, projectErr := repository.project(access, row, policy.ActionClassView)
		if projectErr != nil {
			return nil, false, fmt.Errorf("project authorized conversation list: %w", projectErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate conversation list: %w", err)
	}
	hasMore := len(items) > input.Limit
	if hasMore {
		items = items[:input.Limit]
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, false, fmt.Errorf("commit conversation list: %w", err)
	}
	return items, hasMore, nil
}

func (repository *PostgresRepository) Get(
	ctx context.Context,
	access AccessContext,
	conversationID uuid.UUID,
) (Conversation, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Conversation{}, fmt.Errorf("begin conversation query: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return Conversation{}, err
	}
	row, err := loadConversationRow(
		queryContext, transaction, access.TenantID, access.ActorID, conversationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("query conversation: %w", err)
	}
	item, err := repository.project(access, row, policy.ActionClassView)
	if err != nil {
		return Conversation{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Conversation{}, fmt.Errorf("commit conversation query: %w", err)
	}
	return item, nil
}

func (repository *PostgresRepository) CreateDirect(
	ctx context.Context,
	access AccessContext,
	targetMemberEmail string,
) (CreateResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return CreateResult{}, fmt.Errorf("begin direct conversation creation: %w", err)
	}
	defer rollback(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureConversations,
	); err != nil {
		return CreateResult{}, err
	}
	if err := lockActiveTenant(queryContext, transaction, access.TenantID); err != nil {
		return CreateResult{}, err
	}
	var targetUserID uuid.UUID
	if err := transaction.QueryRow(
		queryContext,
		`SELECT member.user_id
FROM tutorhub.memberships AS member
JOIN tutorhub.users AS target
  ON target.id = member.user_id
WHERE member.tenant_id = $1
  AND target.email = $2
  AND member.status = 'active'
  AND target.status = 'active'`,
		access.TenantID,
		targetMemberEmail,
	).Scan(&targetUserID); errors.Is(err, pgx.ErrNoRows) {
		return CreateResult{}, ErrUnavailable
	} else if err != nil {
		return CreateResult{}, fmt.Errorf("resolve direct conversation target: %w", err)
	}
	if targetUserID == access.ActorID {
		return CreateResult{}, ErrUnavailable
	}
	members, err := lockDirectMembers(
		queryContext, transaction, access.TenantID, access.ActorID, targetUserID,
	)
	if err != nil {
		return CreateResult{}, err
	}
	actor, actorOK := members[access.ActorID]
	target, targetOK := members[targetUserID]
	if !actorOK || !actor.Active {
		return CreateResult{}, ErrAccessDenied
	}
	if !targetOK || !target.Active {
		return CreateResult{}, ErrUnavailable
	}
	access.MembershipActive = true
	access.OrganizationRoles = []policy.OrganizationRole{actor.Role}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: subject(access),
		Action:  policy.ActionConversationCreateDirect,
		Resource: policy.Resource{
			TenantID: access.TenantID,
			State:    policy.ResourceStateActive,
		},
	})
	if !decision.Allowed {
		return CreateResult{}, ErrAccessDenied
	}
	lowID, highID := canonicalPair(access.ActorID, targetUserID)
	conversationID := uuid.New()
	createdAt := time.Now().UTC()
	var insertedID uuid.UUID
	err = transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.conversations (
    id, tenant_id, kind, direct_user_low_id, direct_user_high_id,
    created_by_user_id, created_at, updated_at
)
VALUES ($1, $2, 'direct', $3, $4, $5, $6, $6)
ON CONFLICT (tenant_id, direct_user_low_id, direct_user_high_id)
    WHERE kind = 'direct'
DO NOTHING
RETURNING id`,
		conversationID,
		access.TenantID,
		lowID,
		highID,
		access.ActorID,
		createdAt,
	).Scan(&insertedID)
	created := true
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		if err := transaction.QueryRow(
			queryContext,
			`SELECT id
FROM tutorhub.conversations
WHERE tenant_id = $1
  AND kind = 'direct'
  AND direct_user_low_id = $2
  AND direct_user_high_id = $3`,
			access.TenantID,
			lowID,
			highID,
		).Scan(&conversationID); err != nil {
			return CreateResult{}, fmt.Errorf("load canonical direct conversation: %w", err)
		}
	} else if err != nil {
		return CreateResult{}, fmt.Errorf("insert direct conversation: %w", err)
	} else {
		conversationID = insertedID
	}
	if created {
		if _, err := transaction.Exec(
			queryContext,
			`INSERT INTO tutorhub.conversation_members (
    tenant_id, conversation_id, user_id, joined_at
)
VALUES ($1, $2, $3, $5), ($1, $2, $4, $5)`,
			access.TenantID,
			conversationID,
			lowID,
			highID,
			createdAt,
		); err != nil {
			return CreateResult{}, fmt.Errorf("insert direct conversation members: %w", err)
		}
		if err := appendCreatedAudit(
			queryContext, transaction, access, conversationID, KindDirect, createdAt,
		); err != nil {
			return CreateResult{}, err
		}
	}
	row, err := loadConversationRow(
		queryContext, transaction, access.TenantID, access.ActorID, conversationID,
	)
	if err != nil {
		return CreateResult{}, fmt.Errorf("load created direct conversation: %w", err)
	}
	item, err := repository.project(access, row, policy.ActionClassView)
	if err != nil {
		return CreateResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return CreateResult{}, fmt.Errorf("commit direct conversation creation: %w", err)
	}
	return CreateResult{Conversation: item, Created: created}, nil
}

func (repository *PostgresRepository) CreateClass(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (CreateResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return CreateResult{}, fmt.Errorf("begin class conversation creation: %w", err)
	}
	defer rollback(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureConversations,
	); err != nil {
		return CreateResult{}, err
	}
	if err := lockActiveTenant(queryContext, transaction, access.TenantID); err != nil {
		return CreateResult{}, err
	}
	class, err := lockConversationClass(
		queryContext, transaction, access.TenantID, classID,
	)
	if err != nil {
		return CreateResult{}, err
	}
	member, err := lockActorMember(queryContext, transaction, access.TenantID, access.ActorID)
	if err != nil {
		return CreateResult{}, err
	}
	classRole, err := lockActorClassRole(
		queryContext, transaction, access.TenantID, classID, access.ActorID,
	)
	if err != nil {
		return CreateResult{}, err
	}
	access.MembershipActive = member.Active
	access.OrganizationRoles = []policy.OrganizationRole{member.Role}
	classRoles := make([]policy.ClassRole, 0, 2)
	if class.OwnerUserID == access.ActorID {
		classRoles = append(classRoles, policy.ClassRoleOwner)
	}
	if classRole != nil {
		classRoles = append(classRoles, *classRole)
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: access.ActorID, ActiveTenantID: access.TenantID,
			MembershipActive: access.MembershipActive, OrganizationRoles: access.OrganizationRoles,
			ClassRoles: classRoles,
		},
		Action: policy.ActionChatSend,
		Resource: policy.Resource{
			TenantID: access.TenantID, ClassID: classID,
			State: policy.ResourceState(class.Status),
		},
	})
	if !decision.Allowed {
		if decision.ConcealResource {
			return CreateResult{}, ErrNotFound
		}
		if decision.Reason == policy.DenialResourceState {
			return CreateResult{}, ErrReadOnly
		}
		return CreateResult{}, ErrAccessDenied
	}
	conversationID := uuid.New()
	createdAt := time.Now().UTC()
	var insertedID uuid.UUID
	err = transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.conversations (
    id, tenant_id, kind, class_id, created_by_user_id, created_at, updated_at
)
VALUES ($1, $2, 'class', $3, $4, $5, $5)
ON CONFLICT (tenant_id, class_id) WHERE kind = 'class'
DO NOTHING
RETURNING id`,
		conversationID,
		access.TenantID,
		classID,
		access.ActorID,
		createdAt,
	).Scan(&insertedID)
	created := true
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		if err := transaction.QueryRow(
			queryContext,
			`SELECT id
FROM tutorhub.conversations
WHERE tenant_id = $1 AND kind = 'class' AND class_id = $2`,
			access.TenantID,
			classID,
		).Scan(&conversationID); err != nil {
			return CreateResult{}, fmt.Errorf("load canonical class conversation: %w", err)
		}
	} else if err != nil {
		return CreateResult{}, fmt.Errorf("insert class conversation: %w", err)
	} else {
		conversationID = insertedID
	}
	if created {
		if err := appendCreatedAudit(
			queryContext, transaction, access, conversationID, KindClass, createdAt,
		); err != nil {
			return CreateResult{}, err
		}
	}
	row, err := loadConversationRow(
		queryContext, transaction, access.TenantID, access.ActorID, conversationID,
	)
	if err != nil {
		return CreateResult{}, fmt.Errorf("load created class conversation: %w", err)
	}
	item, err := repository.project(access, row, policy.ActionClassView)
	if err != nil {
		return CreateResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return CreateResult{}, fmt.Errorf("commit class conversation creation: %w", err)
	}
	return CreateResult{Conversation: item, Created: created}, nil
}

type lockedMember struct {
	Role   policy.OrganizationRole
	Active bool
}

type lockedClass struct {
	OwnerUserID uuid.UUID
	Status      string
}

func lockActiveTenant(ctx context.Context, transaction pgx.Tx, tenantID uuid.UUID) error {
	var found uuid.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT id FROM tutorhub.tenants
WHERE id = $1 AND status = 'active'
FOR SHARE`,
		tenantID,
	).Scan(&found); errors.Is(err, pgx.ErrNoRows) {
		return ErrAccessDenied
	} else if err != nil {
		return fmt.Errorf("lock active conversation tenant: %w", err)
	}
	return nil
}

func lockDirectMembers(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, actorID, targetID uuid.UUID,
) (map[uuid.UUID]lockedMember, error) {
	ids := []uuid.UUID{actorID, targetID}
	if bytes.Compare(ids[0][:], ids[1][:]) > 0 {
		ids[0], ids[1] = ids[1], ids[0]
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT member.user_id, member.role,
       member.status = 'active' AND tutor.status = 'active'
FROM tutorhub.memberships AS member
JOIN tutorhub.users AS tutor ON tutor.id = member.user_id
WHERE member.tenant_id = $1 AND member.user_id = ANY($2::uuid[])
ORDER BY member.user_id
FOR SHARE OF member, tutor`,
		tenantID,
		ids,
	)
	if err != nil {
		return nil, fmt.Errorf("lock direct conversation members: %w", err)
	}
	defer rows.Close()
	members := make(map[uuid.UUID]lockedMember, 2)
	for rows.Next() {
		var id uuid.UUID
		var member lockedMember
		if err := rows.Scan(&id, &member.Role, &member.Active); err != nil {
			return nil, fmt.Errorf("scan direct conversation member: %w", err)
		}
		members[id] = member
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate direct conversation members: %w", err)
	}
	return members, nil
}

func lockConversationClass(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, classID uuid.UUID,
) (lockedClass, error) {
	var class lockedClass
	if err := transaction.QueryRow(
		ctx,
		`SELECT owner_user_id, status
FROM tutorhub.classes
WHERE tenant_id = $1 AND id = $2
FOR SHARE`,
		tenantID,
		classID,
	).Scan(&class.OwnerUserID, &class.Status); errors.Is(err, pgx.ErrNoRows) {
		return lockedClass{}, ErrNotFound
	} else if err != nil {
		return lockedClass{}, fmt.Errorf("lock class conversation class: %w", err)
	}
	return class, nil
}

func lockActorMember(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, actorID uuid.UUID,
) (lockedMember, error) {
	var member lockedMember
	if err := transaction.QueryRow(
		ctx,
		`SELECT member.role,
       member.status = 'active' AND tutor.status = 'active'
FROM tutorhub.memberships AS member
JOIN tutorhub.users AS tutor ON tutor.id = member.user_id
WHERE member.tenant_id = $1 AND member.user_id = $2
FOR SHARE OF member, tutor`,
		tenantID,
		actorID,
	).Scan(&member.Role, &member.Active); errors.Is(err, pgx.ErrNoRows) {
		return lockedMember{}, ErrAccessDenied
	} else if err != nil {
		return lockedMember{}, fmt.Errorf("lock conversation actor membership: %w", err)
	}
	if !member.Active {
		return lockedMember{}, ErrAccessDenied
	}
	return member, nil
}

func lockActorClassRole(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, classID, actorID uuid.UUID,
) (*policy.ClassRole, error) {
	var role policy.ClassRole
	var status string
	err := transaction.QueryRow(
		ctx,
		`SELECT class_role, status
FROM tutorhub.class_enrollments
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
FOR SHARE`,
		tenantID,
		classID,
		actorID,
	).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock conversation actor enrollment: %w", err)
	}
	if status != "active" {
		return nil, nil
	}
	return &role, nil
}

func (repository *PostgresRepository) requireActiveScope(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
) (AccessContext, error) {
	if access.TenantID == uuid.Nil || access.ActorID == uuid.Nil {
		return AccessContext{}, ErrAccessDenied
	}
	var role policy.OrganizationRole
	if err := transaction.QueryRow(
		ctx,
		`SELECT member.role
FROM tutorhub.tenants AS tenant
JOIN tutorhub.memberships AS member
  ON member.tenant_id = tenant.id
JOIN tutorhub.users AS tutor
  ON tutor.id = member.user_id
WHERE tenant.id = $1
  AND member.user_id = $2
  AND tenant.status = 'active'
  AND member.status = 'active'
  AND tutor.status = 'active'`,
		access.TenantID,
		access.ActorID,
	).Scan(&role); errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, ErrAccessDenied
	} else if err != nil {
		return AccessContext{}, fmt.Errorf("authorize conversation scope: %w", err)
	}
	access.MembershipActive = true
	access.OrganizationRoles = []policy.OrganizationRole{role}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: subject(access),
		Action:  policy.ActionTenantView,
		Resource: policy.Resource{
			TenantID: access.TenantID,
			State:    policy.ResourceStateActive,
		},
	})
	if !decision.Allowed {
		return AccessContext{}, ErrAccessDenied
	}
	return access, nil
}

type conversationRow struct {
	ID                   uuid.UUID
	TenantID             uuid.UUID
	Kind                 Kind
	ClassID              uuid.NullUUID
	DirectLowID          uuid.NullUUID
	DirectHighID         uuid.NullUUID
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ClassTitle           sql.NullString
	ClassStatus          sql.NullString
	ClassOwnerID         uuid.NullUUID
	ActorEnrollmentRole  sql.NullString
	ActorEnrollmentState sql.NullString
	LowDisplayName       sql.NullString
	HighDisplayName      sql.NullString
	LowActive            bool
	HighActive           bool
}

const conversationSelect = `SELECT
    conversation.id,
    conversation.tenant_id,
    conversation.kind,
    conversation.class_id,
    conversation.direct_user_low_id,
    conversation.direct_user_high_id,
    conversation.created_at,
    conversation.updated_at,
    class.title,
    class.status,
    class.owner_user_id,
    actor_enrollment.class_role,
    actor_enrollment.status,
    low_user.display_name,
    high_user.display_name,
    COALESCE(low_member.status = 'active' AND low_user.status = 'active', false),
    COALESCE(high_member.status = 'active' AND high_user.status = 'active', false)
FROM tutorhub.conversations AS conversation
LEFT JOIN tutorhub.classes AS class
  ON class.tenant_id = conversation.tenant_id
 AND class.id = conversation.class_id
LEFT JOIN tutorhub.class_enrollments AS actor_enrollment
  ON actor_enrollment.tenant_id = conversation.tenant_id
 AND actor_enrollment.class_id = conversation.class_id
 AND actor_enrollment.user_id = $5
LEFT JOIN tutorhub.users AS low_user
  ON low_user.id = conversation.direct_user_low_id
LEFT JOIN tutorhub.memberships AS low_member
  ON low_member.tenant_id = conversation.tenant_id
 AND low_member.user_id = conversation.direct_user_low_id
LEFT JOIN tutorhub.users AS high_user
  ON high_user.id = conversation.direct_user_high_id
LEFT JOIN tutorhub.memberships AS high_member
  ON high_member.tenant_id = conversation.tenant_id
 AND high_member.user_id = conversation.direct_user_high_id`

const conversationGetSelect = `SELECT
    conversation.id,
    conversation.tenant_id,
    conversation.kind,
    conversation.class_id,
    conversation.direct_user_low_id,
    conversation.direct_user_high_id,
    conversation.created_at,
    conversation.updated_at,
    class.title,
    class.status,
    class.owner_user_id,
    actor_enrollment.class_role,
    actor_enrollment.status,
    low_user.display_name,
    high_user.display_name,
    COALESCE(low_member.status = 'active' AND low_user.status = 'active', false),
    COALESCE(high_member.status = 'active' AND high_user.status = 'active', false)
FROM tutorhub.conversations AS conversation
LEFT JOIN tutorhub.classes AS class
  ON class.tenant_id = conversation.tenant_id
 AND class.id = conversation.class_id
LEFT JOIN tutorhub.class_enrollments AS actor_enrollment
  ON actor_enrollment.tenant_id = conversation.tenant_id
 AND actor_enrollment.class_id = conversation.class_id
 AND actor_enrollment.user_id = $2
LEFT JOIN tutorhub.users AS low_user
  ON low_user.id = conversation.direct_user_low_id
LEFT JOIN tutorhub.memberships AS low_member
  ON low_member.tenant_id = conversation.tenant_id
 AND low_member.user_id = conversation.direct_user_low_id
LEFT JOIN tutorhub.users AS high_user
  ON high_user.id = conversation.direct_user_high_id
LEFT JOIN tutorhub.memberships AS high_member
  ON high_member.tenant_id = conversation.tenant_id
 AND high_member.user_id = conversation.direct_user_high_id
WHERE conversation.tenant_id = $1 AND conversation.id = $3`

func loadConversationRow(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, actorID, conversationID uuid.UUID,
) (conversationRow, error) {
	return scanConversationRow(transaction.QueryRow(
		ctx, conversationGetSelect, tenantID, actorID, conversationID,
	))
}

type rowScanner interface {
	Scan(...any) error
}

func scanConversationRow(row rowScanner) (conversationRow, error) {
	var item conversationRow
	err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.Kind,
		&item.ClassID,
		&item.DirectLowID,
		&item.DirectHighID,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.ClassTitle,
		&item.ClassStatus,
		&item.ClassOwnerID,
		&item.ActorEnrollmentRole,
		&item.ActorEnrollmentState,
		&item.LowDisplayName,
		&item.HighDisplayName,
		&item.LowActive,
		&item.HighActive,
	)
	return item, err
}

func (repository *PostgresRepository) project(
	access AccessContext,
	row conversationRow,
	classReadAction policy.Action,
) (Conversation, error) {
	item := Conversation{
		ID: row.ID, TenantID: row.TenantID, Kind: row.Kind,
		Participants: []Participant{}, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	switch row.Kind {
	case KindDirect:
		if !row.DirectLowID.Valid || !row.DirectHighID.Valid ||
			!row.LowDisplayName.Valid || !row.HighDisplayName.Valid {
			return Conversation{}, fmt.Errorf("direct conversation projection is incomplete")
		}
		if access.ActorID != row.DirectLowID.UUID && access.ActorID != row.DirectHighID.UUID {
			return Conversation{}, ErrNotFound
		}
		item.Participants = []Participant{
			{UserID: row.DirectLowID.UUID, DisplayName: row.LowDisplayName.String},
			{UserID: row.DirectHighID.UUID, DisplayName: row.HighDisplayName.String},
		}
		if access.ActorID == row.DirectLowID.UUID {
			item.Title = row.HighDisplayName.String
		} else {
			item.Title = row.LowDisplayName.String
		}
		item.ViewerAccess.CanPostMessages = row.LowActive && row.HighActive &&
			repository.authorizer.Authorize(policy.Input{
				Subject: subject(access), Action: policy.ActionConversationCreateDirect,
				Resource: policy.Resource{
					TenantID: access.TenantID, State: policy.ResourceStateActive,
				},
			}).Allowed
	case KindClass:
		if !row.ClassID.Valid || !row.ClassTitle.Valid || !row.ClassStatus.Valid ||
			!row.ClassOwnerID.Valid {
			return Conversation{}, fmt.Errorf("class conversation projection is incomplete")
		}
		classRoles := make([]policy.ClassRole, 0, 2)
		if row.ClassOwnerID.UUID == access.ActorID {
			classRoles = append(classRoles, policy.ClassRoleOwner)
		}
		if row.ActorEnrollmentRole.Valid && row.ActorEnrollmentState.String == "active" {
			classRoles = append(classRoles, policy.ClassRole(row.ActorEnrollmentRole.String))
		}
		resource := policy.Resource{
			TenantID: access.TenantID, ClassID: row.ClassID.UUID,
			State: policy.ResourceState(row.ClassStatus.String),
		}
		classSubject := subject(access)
		classSubject.ClassRoles = classRoles
		decision := repository.authorizer.Authorize(policy.Input{
			Subject: classSubject, Action: classReadAction, Resource: resource,
		})
		if !decision.Allowed {
			if decision.ConcealResource {
				return Conversation{}, ErrNotFound
			}
			return Conversation{}, ErrAccessDenied
		}
		classID := row.ClassID.UUID
		classStatus := row.ClassStatus.String
		item.ClassID = &classID
		item.ClassStatus = &classStatus
		item.Title = row.ClassTitle.String
		item.ViewerAccess.CanPostMessages = repository.authorizer.Authorize(policy.Input{
			Subject: classSubject, Action: policy.ActionChatSend, Resource: resource,
		}).Allowed
	default:
		return Conversation{}, fmt.Errorf("unsupported conversation kind")
	}
	return item, nil
}

func subject(access AccessContext) policy.Subject {
	return policy.Subject{
		ActorID: access.ActorID, ActiveTenantID: access.TenantID,
		MembershipActive:  access.MembershipActive,
		OrganizationRoles: append([]policy.OrganizationRole(nil), access.OrganizationRoles...),
	}
}

func canonicalPair(left, right uuid.UUID) (uuid.UUID, uuid.UUID) {
	if bytes.Compare(left[:], right[:]) <= 0 {
		return left, right
	}
	return right, left
}

func appendCreatedAudit(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	conversationID uuid.UUID,
	kind Kind,
	createdAt time.Time,
) error {
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: access.TenantID, ActorID: access.ActorID,
		EventType: "conversation.created.v1", AggregateType: "conversation",
		AggregateID: conversationID, Metadata: audit.Metadata{"kind": string(kind)},
		OccurredAt: createdAt,
	}); err != nil {
		return fmt.Errorf("append conversation creation audit: %w", err)
	}
	return nil
}

func rollback(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}
