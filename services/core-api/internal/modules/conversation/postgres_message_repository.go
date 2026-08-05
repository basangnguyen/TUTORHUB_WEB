package conversation

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	actorMessageRateQuota  featurecontrol.QuotaKey = "actor_message_sends_per_minute"
	actorMessageRateWindow                         = time.Minute
)

func (repository *PostgresRepository) ListMessages(
	ctx context.Context,
	access AccessContext,
	conversationID uuid.UUID,
	input MessageListInput,
	cursor messageCursor,
) (messageRepositoryPage, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return messageRepositoryPage{}, fmt.Errorf("begin message list: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.authorizeMessageRead(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return messageRepositoryPage{}, err
	}
	rows, err := transaction.Query(
		queryContext,
		messageSelect+`
WHERE message.tenant_id = $1
  AND message.conversation_id = $2
  AND ($3::bigint = 0 OR message.sequence < $3)
ORDER BY message.sequence DESC
LIMIT $4`,
		access.TenantID,
		conversationID,
		cursor.BeforeSequence,
		input.Limit+1,
	)
	if err != nil {
		return messageRepositoryPage{}, fmt.Errorf("query message list: %w", err)
	}
	defer rows.Close()
	items := make([]Message, 0, input.Limit+1)
	for rows.Next() {
		item, scanErr := scanMessage(rows)
		if scanErr != nil {
			return messageRepositoryPage{}, safeMessageStoreError("scan message list", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return messageRepositoryPage{}, fmt.Errorf("iterate message list: %w", err)
	}
	hasMore := len(items) > input.Limit
	if hasMore {
		items = items[:input.Limit]
	}
	readState, err := loadMessageReadState(
		queryContext, transaction, access.TenantID, conversationID, access.ActorID,
	)
	if err != nil {
		return messageRepositoryPage{}, err
	}
	lastReadSequence := int64(0)
	if readState != nil {
		lastReadSequence = readState.LastReadSequence
	}
	var unread int
	if err := transaction.QueryRow(
		queryContext,
		`SELECT count(*)
FROM (
    SELECT 1
    FROM tutorhub.messages
    WHERE tenant_id = $1
      AND conversation_id = $2
      AND author_user_id <> $3
      AND state <> 'deleted'
      AND sequence > $4
    ORDER BY sequence
    LIMIT $5
) AS bounded_unread`,
		access.TenantID,
		conversationID,
		access.ActorID,
		lastReadSequence,
		maximumUnreadCount+1,
	).Scan(&unread); err != nil {
		return messageRepositoryPage{}, fmt.Errorf("count bounded unread messages: %w", err)
	}
	unreadCapped := unread > maximumUnreadCount
	if unreadCapped {
		unread = maximumUnreadCount
	}
	if err := transaction.Commit(queryContext); err != nil {
		return messageRepositoryPage{}, fmt.Errorf("commit message list: %w", err)
	}
	return messageRepositoryPage{
		Items:             items,
		HasMore:           hasMore,
		UnreadCount:       unread,
		UnreadCountCapped: unreadCapped,
		ReadState:         readState,
	}, nil
}

func (repository *PostgresRepository) SendMessage(
	ctx context.Context,
	access AccessContext,
	conversationID uuid.UUID,
	input SendMessageInput,
) (MessageMutationResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MessageMutationResult{}, fmt.Errorf("begin message send: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.authorizeMessageRead(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return MessageMutationResult{}, err
	}
	fingerprint := sha256.Sum256([]byte(input.Content))
	if existing, found, err := loadIdempotentMessage(
		queryContext,
		transaction,
		access.TenantID,
		access.ActorID,
		input.ClientMessageID,
	); err != nil {
		return MessageMutationResult{}, err
	} else if found {
		if existing.message.ConversationID != conversationID ||
			subtle.ConstantTimeCompare(existing.fingerprint, fingerprint[:]) != 1 {
			return MessageMutationResult{}, ErrIdempotencyConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return MessageMutationResult{}, fmt.Errorf("commit message replay: %w", err)
		}
		return MessageMutationResult{Message: existing.message}, nil
	}
	if err := repository.controls.RequireFeature(
		queryContext,
		transaction,
		access.TenantID,
		featurecontrol.FeatureConversations,
	); err != nil {
		return MessageMutationResult{}, err
	}
	access, err = repository.authorizeMessageWrite(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return MessageMutationResult{}, err
	}
	// RequireFeature above holds the tenant control advisory lock. Because the
	// idempotency key is tenant + author (not conversation), recheck after the
	// conversation authorization/lock so concurrent sends to different
	// conversations cannot race through to the unique constraint.
	if existing, found, err := loadIdempotentMessage(
		queryContext,
		transaction,
		access.TenantID,
		access.ActorID,
		input.ClientMessageID,
	); err != nil {
		return MessageMutationResult{}, err
	} else if found {
		if existing.message.ConversationID != conversationID ||
			subtle.ConstantTimeCompare(existing.fingerprint, fingerprint[:]) != 1 {
			return MessageMutationResult{}, ErrIdempotencyConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return MessageMutationResult{}, fmt.Errorf("commit concurrent message replay: %w", err)
		}
		return MessageMutationResult{Message: existing.message}, nil
	}
	var now time.Time
	if err := transaction.QueryRow(queryContext, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return MessageMutationResult{}, fmt.Errorf("read message server time: %w", err)
	}
	now = now.UTC()
	if err := repository.reserveMessageCapacity(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return MessageMutationResult{}, err
	}
	if _, err := repository.controls.ConsumeRateQuota(
		queryContext,
		transaction,
		access.TenantID,
		featurecontrol.QuotaMessageSendsPerHour,
		now,
	); err != nil {
		return MessageMutationResult{}, err
	}
	if err := enforceActorMessageRate(
		queryContext, transaction, access, now,
	); err != nil {
		return MessageMutationResult{}, err
	}
	var nextSequence int64
	if err := transaction.QueryRow(
		queryContext,
		`SELECT coalesce(max(sequence), 0) + 1
FROM tutorhub.messages
WHERE tenant_id = $1 AND conversation_id = $2`,
		access.TenantID,
		conversationID,
	).Scan(&nextSequence); err != nil {
		return MessageMutationResult{}, fmt.Errorf("allocate message sequence: %w", err)
	}
	messageID := uuid.New()
	item, err := scanMessage(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.messages (
    id, tenant_id, conversation_id, sequence, author_user_id,
    client_message_id, request_fingerprint, content, state, version,
    created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', 1, $9, $9)
RETURNING
    id,
    conversation_id,
    sequence,
    client_message_id,
    author_user_id,
    (SELECT display_name FROM tutorhub.users WHERE id = $5),
    content,
    state,
    version,
    created_at,
    updated_at,
    edited_at,
    deleted_at`,
		messageID,
		access.TenantID,
		conversationID,
		nextSequence,
		access.ActorID,
		input.ClientMessageID,
		fingerprint[:],
		input.Content,
		now,
	))
	if err != nil {
		return MessageMutationResult{}, safeMessageStoreError("insert message", err)
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.conversations
SET updated_at = greatest(updated_at, $3)
WHERE tenant_id = $1 AND id = $2`,
		access.TenantID,
		conversationID,
		item.CreatedAt,
	); err != nil {
		return MessageMutationResult{}, fmt.Errorf("advance conversation message activity: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MessageMutationResult{}, fmt.Errorf("commit message send: %w", err)
	}
	return MessageMutationResult{Message: item, Created: true}, nil
}

func (repository *PostgresRepository) EditMessage(
	ctx context.Context,
	access AccessContext,
	conversationID, messageID uuid.UUID,
	input EditMessageInput,
) (Message, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Message{}, fmt.Errorf("begin message edit: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.authorizeMessageRead(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return Message{}, err
	}
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureConversations,
	); err != nil {
		return Message{}, err
	}
	access, err = repository.authorizeMessageWrite(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return Message{}, err
	}
	item, err := loadMessageForUpdate(
		queryContext, transaction, access.TenantID, conversationID, messageID,
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.Author.UserID != access.ActorID) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, safeMessageStoreError("load message for edit", err)
	}
	if item.State != MessageStateActive || item.Version != input.ExpectedVersion {
		return Message{}, ErrVersionConflict
	}
	if item.Content != nil && *item.Content == input.Content {
		if err := transaction.Commit(queryContext); err != nil {
			return Message{}, fmt.Errorf("commit unchanged message edit: %w", err)
		}
		return item, nil
	}
	item, err = scanMessage(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.messages AS message
SET content = $4,
    version = message.version + 1,
    edited_at = statement_timestamp(),
    updated_at = statement_timestamp()
FROM tutorhub.users AS author
WHERE message.tenant_id = $1
  AND message.conversation_id = $2
  AND message.id = $3
  AND author.id = message.author_user_id
RETURNING
    message.id,
    message.conversation_id,
    message.sequence,
    message.client_message_id,
    message.author_user_id,
    author.display_name,
    message.content,
    message.state,
    message.version,
    message.created_at,
    message.updated_at,
    message.edited_at,
    message.deleted_at`,
		access.TenantID,
		conversationID,
		messageID,
		input.Content,
	))
	if err != nil {
		return Message{}, safeMessageStoreError("update message content", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Message{}, fmt.Errorf("commit message edit: %w", err)
	}
	return item, nil
}

func (repository *PostgresRepository) DeleteMessage(
	ctx context.Context,
	access AccessContext,
	conversationID, messageID uuid.UUID,
	input DeleteMessageInput,
) (Message, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Message{}, fmt.Errorf("begin message delete: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.authorizeMessageRead(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return Message{}, err
	}
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureConversations,
	); err != nil {
		return Message{}, err
	}
	access, err = repository.authorizeMessageWrite(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return Message{}, err
	}
	item, err := loadMessageForUpdate(
		queryContext, transaction, access.TenantID, conversationID, messageID,
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.Author.UserID != access.ActorID) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, safeMessageStoreError("load message for delete", err)
	}
	if item.State == MessageStateDeleted {
		if err := transaction.Commit(queryContext); err != nil {
			return Message{}, fmt.Errorf("commit repeated message delete: %w", err)
		}
		return item, nil
	}
	if item.Version != input.ExpectedVersion {
		return Message{}, ErrVersionConflict
	}
	item, err = scanMessage(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.messages AS message
SET content = NULL,
    state = 'deleted',
    version = message.version + 1,
    deleted_at = statement_timestamp(),
    updated_at = statement_timestamp()
FROM tutorhub.users AS author
WHERE message.tenant_id = $1
  AND message.conversation_id = $2
  AND message.id = $3
  AND author.id = message.author_user_id
RETURNING
    message.id,
    message.conversation_id,
    message.sequence,
    message.client_message_id,
    message.author_user_id,
    author.display_name,
    message.content,
    message.state,
    message.version,
    message.created_at,
    message.updated_at,
    message.edited_at,
    message.deleted_at`,
		access.TenantID,
		conversationID,
		messageID,
	))
	if err != nil {
		return Message{}, safeMessageStoreError("tombstone message", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Message{}, fmt.Errorf("commit message delete: %w", err)
	}
	return item, nil
}

func (repository *PostgresRepository) MarkRead(
	ctx context.Context,
	access AccessContext,
	conversationID, messageID uuid.UUID,
) (MessageReadState, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MessageReadState{}, fmt.Errorf("begin message read marker: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.authorizeMessageRead(
		queryContext, transaction, access, conversationID,
	)
	if err != nil {
		return MessageReadState{}, err
	}
	var sequence int64
	if err := transaction.QueryRow(
		queryContext,
		`SELECT sequence
FROM tutorhub.messages
WHERE tenant_id = $1 AND conversation_id = $2 AND id = $3`,
		access.TenantID,
		conversationID,
		messageID,
	).Scan(&sequence); errors.Is(err, pgx.ErrNoRows) {
		return MessageReadState{}, ErrNotFound
	} else if err != nil {
		return MessageReadState{}, fmt.Errorf("resolve read-through message: %w", err)
	}
	state := MessageReadState{}
	err = transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.message_receipts (
    tenant_id, conversation_id, user_id,
    last_read_message_id, last_read_sequence, updated_at
)
VALUES ($1, $2, $3, $4, $5, clock_timestamp())
ON CONFLICT (tenant_id, conversation_id, user_id)
DO UPDATE SET
    last_read_message_id = EXCLUDED.last_read_message_id,
    last_read_sequence = EXCLUDED.last_read_sequence,
    updated_at = EXCLUDED.updated_at
WHERE tutorhub.message_receipts.last_read_sequence < EXCLUDED.last_read_sequence
RETURNING last_read_message_id, last_read_sequence, updated_at`,
		access.TenantID,
		conversationID,
		access.ActorID,
		messageID,
		sequence,
	).Scan(&state.LastReadMessageID, &state.LastReadSequence, &state.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		current, loadErr := loadMessageReadState(
			queryContext, transaction, access.TenantID, conversationID, access.ActorID,
		)
		if loadErr != nil {
			return MessageReadState{}, loadErr
		}
		if current == nil {
			return MessageReadState{}, fmt.Errorf("read marker disappeared after monotonic upsert")
		}
		state = *current
	} else if err != nil {
		return MessageReadState{}, fmt.Errorf("advance message read marker: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MessageReadState{}, fmt.Errorf("commit message read marker: %w", err)
	}
	return state, nil
}

func (repository *PostgresRepository) authorizeMessageRead(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	conversationID uuid.UUID,
) (AccessContext, error) {
	access, err := repository.requireActiveScope(ctx, transaction, access)
	if err != nil {
		return AccessContext{}, err
	}
	row, err := loadConversationRow(
		ctx, transaction, access.TenantID, access.ActorID, conversationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, ErrNotFound
	}
	if err != nil {
		return AccessContext{}, fmt.Errorf("load message conversation: %w", err)
	}
	if _, err := repository.project(access, row, policy.ActionClassView); err != nil {
		return AccessContext{}, err
	}
	return access, nil
}

func (repository *PostgresRepository) authorizeMessageWrite(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	conversationID uuid.UUID,
) (AccessContext, error) {
	row, err := loadConversationRow(
		ctx, transaction, access.TenantID, access.ActorID, conversationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, ErrNotFound
	}
	if err != nil {
		return AccessContext{}, fmt.Errorf("load writable message conversation: %w", err)
	}
	switch row.Kind {
	case KindDirect:
		if !row.DirectLowID.Valid || !row.DirectHighID.Valid ||
			(access.ActorID != row.DirectLowID.UUID && access.ActorID != row.DirectHighID.UUID) {
			return AccessContext{}, ErrNotFound
		}
		members, err := lockDirectMembers(
			ctx,
			transaction,
			access.TenantID,
			row.DirectLowID.UUID,
			row.DirectHighID.UUID,
		)
		if err != nil {
			return AccessContext{}, err
		}
		actor, actorOK := members[access.ActorID]
		low, lowOK := members[row.DirectLowID.UUID]
		high, highOK := members[row.DirectHighID.UUID]
		if !actorOK || !actor.Active {
			return AccessContext{}, ErrAccessDenied
		}
		if !lowOK || !highOK || !low.Active || !high.Active {
			return AccessContext{}, ErrReadOnly
		}
		access.MembershipActive = true
		access.OrganizationRoles = []policy.OrganizationRole{actor.Role}
		if !repository.authorizer.Authorize(policy.Input{
			Subject: subject(access), Action: policy.ActionMessageWriteDirect,
			Resource: policy.Resource{
				TenantID: access.TenantID, State: policy.ResourceStateActive,
			},
		}).Allowed {
			return AccessContext{}, ErrAccessDenied
		}
	case KindClass:
		if !row.ClassID.Valid {
			return AccessContext{}, ErrNotFound
		}
		class, err := lockConversationClass(
			ctx, transaction, access.TenantID, row.ClassID.UUID,
		)
		if err != nil {
			return AccessContext{}, err
		}
		member, err := lockActorMember(
			ctx, transaction, access.TenantID, access.ActorID,
		)
		if err != nil {
			return AccessContext{}, err
		}
		classRole, err := lockActorClassRole(
			ctx, transaction, access.TenantID, row.ClassID.UUID, access.ActorID,
		)
		if err != nil {
			return AccessContext{}, err
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
				MembershipActive:  access.MembershipActive,
				OrganizationRoles: access.OrganizationRoles,
				ClassRoles:        classRoles,
			},
			Action: policy.ActionChatSend,
			Resource: policy.Resource{
				TenantID: access.TenantID,
				ClassID:  row.ClassID.UUID,
				State:    policy.ResourceState(class.Status),
			},
		})
		if !decision.Allowed {
			if decision.ConcealResource {
				return AccessContext{}, ErrNotFound
			}
			if decision.Reason == policy.DenialResourceState {
				return AccessContext{}, ErrReadOnly
			}
			return AccessContext{}, ErrAccessDenied
		}
	default:
		return AccessContext{}, ErrNotFound
	}
	var lockedID uuid.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT id
FROM tutorhub.conversations
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`,
		access.TenantID,
		conversationID,
	).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, ErrNotFound
	} else if err != nil {
		return AccessContext{}, fmt.Errorf("lock message conversation: %w", err)
	}
	return access, nil
}

func (repository *PostgresRepository) reserveMessageCapacity(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
) error {
	// A zero check acquires the shared tenant-control advisory lock for the
	// remainder of this transaction. The O(1) counter reservation and limit
	// check are therefore serialized with every governed mutation in the tenant.
	if err := repository.controls.RequireQuotaAtMost(
		ctx, transaction, tenantID, featurecontrol.QuotaMessagesPerTenant, 0,
	); err != nil {
		return err
	}
	var used int64
	if err := transaction.QueryRow(
		ctx,
		`SELECT COALESCE((
    SELECT message_count
    FROM tutorhub.tenant_message_usage
    WHERE tenant_id = $1
), 0)`,
		tenantID,
	).Scan(&used); err != nil {
		return fmt.Errorf("read tenant message usage: %w", err)
	}
	if err := repository.controls.RequireQuotaAtMost(
		ctx, transaction, tenantID, featurecontrol.QuotaMessagesPerTenant, used+1,
	); err != nil {
		return err
	}
	var reserved int64
	if err := transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.tenant_message_usage AS usage (
    tenant_id, message_count, updated_at
)
VALUES ($1, 1, clock_timestamp())
ON CONFLICT (tenant_id)
DO UPDATE SET
    message_count = usage.message_count + 1,
    updated_at = EXCLUDED.updated_at
RETURNING message_count`,
		tenantID,
	).Scan(&reserved); err != nil {
		return fmt.Errorf("reserve tenant message usage: %w", err)
	}
	if reserved != used+1 {
		return fmt.Errorf("reserve tenant message usage: counter changed outside the tenant lock")
	}
	return nil
}

func enforceActorMessageRate(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	now time.Time,
) error {
	var used int64
	var oldest sql.NullTime
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*), min(created_at)
FROM tutorhub.messages
WHERE tenant_id = $1
  AND author_user_id = $2
  AND created_at >= $3`,
		access.TenantID,
		access.ActorID,
		now.Add(-actorMessageRateWindow),
	).Scan(&used, &oldest); err != nil {
		return fmt.Errorf("inspect actor message rate: %w", err)
	}
	if used < actorMessageRateLimit {
		return nil
	}
	retryAfter := actorMessageRateWindow
	if oldest.Valid {
		retryAfter = oldest.Time.Add(actorMessageRateWindow).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
	}
	return &featurecontrol.QuotaExceededError{
		Quota:      actorMessageRateQuota,
		Limit:      actorMessageRateLimit,
		Used:       used,
		ResetAt:    now.Add(retryAfter),
		RetryAfter: retryAfter,
	}
}

type idempotentMessage struct {
	message     Message
	fingerprint []byte
}

func loadIdempotentMessage(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, authorID, clientMessageID uuid.UUID,
) (idempotentMessage, bool, error) {
	var result idempotentMessage
	row := transaction.QueryRow(
		ctx,
		messageIdempotencySelect+`
WHERE message.tenant_id = $1
  AND message.author_user_id = $2
  AND message.client_message_id = $3`,
		tenantID,
		authorID,
		clientMessageID,
	)
	item, err := scanMessageWithFingerprint(row, &result.fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return idempotentMessage{}, false, nil
	}
	if err != nil {
		return idempotentMessage{}, false, safeMessageStoreError("load idempotent message", err)
	}
	result.message = item
	return result, true, nil
}

func loadMessageForUpdate(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, conversationID, messageID uuid.UUID,
) (Message, error) {
	return scanMessage(transaction.QueryRow(
		ctx,
		messageSelect+`
WHERE message.tenant_id = $1
  AND message.conversation_id = $2
  AND message.id = $3
FOR UPDATE OF message`,
		tenantID,
		conversationID,
		messageID,
	))
}

func loadMessageReadState(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, conversationID, userID uuid.UUID,
) (*MessageReadState, error) {
	state := &MessageReadState{}
	if err := transaction.QueryRow(
		ctx,
		`SELECT last_read_message_id, last_read_sequence, updated_at
FROM tutorhub.message_receipts
WHERE tenant_id = $1 AND conversation_id = $2 AND user_id = $3`,
		tenantID,
		conversationID,
		userID,
	).Scan(
		&state.LastReadMessageID,
		&state.LastReadSequence,
		&state.UpdatedAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("load message read state: %w", err)
	}
	return state, nil
}

const messageProjection = `
    message.id,
    message.conversation_id,
    message.sequence,
    message.client_message_id,
    message.author_user_id,
    author.display_name,
    message.content,
    message.state,
    message.version,
    message.created_at,
    message.updated_at,
    message.edited_at,
    message.deleted_at`

const messageSource = `
FROM tutorhub.messages AS message
JOIN tutorhub.users AS author ON author.id = message.author_user_id`

const messageSelect = `SELECT` + messageProjection + messageSource

const messageIdempotencySelect = `SELECT` + messageProjection + `,
    message.request_fingerprint` + messageSource

func scanMessage(row rowScanner) (Message, error) {
	return scanMessageWithFingerprint(row, nil)
}

func scanMessageWithFingerprint(
	row rowScanner,
	fingerprint *[]byte,
) (Message, error) {
	var item Message
	var content sql.NullString
	var editedAt sql.NullTime
	var deletedAt sql.NullTime
	values := []any{
		&item.ID,
		&item.ConversationID,
		&item.Sequence,
		&item.ClientMessageID,
		&item.Author.UserID,
		&item.Author.DisplayName,
		&content,
		&item.State,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
		&editedAt,
		&deletedAt,
	}
	if fingerprint != nil {
		// The idempotency query appends the internal fingerprint after the public
		// projection. No caller can serialize this value.
		values = append(values, fingerprint)
	}
	if err := row.Scan(values...); err != nil {
		return Message{}, err
	}
	if content.Valid {
		value := content.String
		item.Content = &value
	}
	if editedAt.Valid {
		value := editedAt.Time
		item.EditedAt = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time
		item.DeletedAt = &value
	}
	return item, nil
}

// Database constraint and scan errors can include the rejected row or scanned
// value. Message text is protected data, so only a bounded SQLSTATE is allowed
// to cross the repository boundary.
func safeMessageStoreError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return fmt.Errorf("%s: PostgreSQL error %s", operation, postgresError.Code)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: message storage operation failed", operation)
}
