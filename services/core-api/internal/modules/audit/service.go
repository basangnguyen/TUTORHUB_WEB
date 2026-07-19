package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	defaultPageLimit    = 25
	maximumPageLimit    = 100
	defaultQueryTimeout = 10 * time.Second
)

type Database interface {
	Begin(context.Context) (pgx.Tx, error)
}

type ServiceAPI interface {
	List(context.Context, tenancy.Context, uuid.UUID, Filter) (Page, error)
	Record(context.Context, Draft) error
	RecordFallback(context.Context, Draft) error
	RecordItemFallback(context.Context, uuid.UUID, Draft) error
}

type Service struct {
	database     Database
	queryTimeout time.Duration
	authorizer   policy.Authorizer
	clock        func() time.Time
}

func NewService(
	database Database,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	clock func() time.Time,
) (*Service, error) {
	if database == nil || authorizer == nil {
		return nil, fmt.Errorf("audit service dependencies must be configured")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer, clock: clock,
	}, nil
}

func (service *Service) Record(ctx context.Context, draft Draft) error {
	if service == nil {
		return fmt.Errorf("record audit event: service is unavailable")
	}
	draft, err := service.normalizeRequestDraft(ctx, draft)
	if err != nil {
		return err
	}
	queryContext, cancel := context.WithTimeout(ctx, service.queryTimeout)
	defer cancel()
	transaction, err := service.database.Begin(queryContext)
	if err != nil {
		return fmt.Errorf("begin audit record: %w", err)
	}
	defer rollback(transaction)
	if err := AppendTransaction(queryContext, transaction, draft); err != nil {
		return err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return fmt.Errorf("commit audit record: %w", err)
	}
	return nil
}

// RecordFallback appends the HTTP-level no-op/failure result only when no
// transactional or item-level audit row already exists for the same server-side
// request instance and action.
func (service *Service) RecordFallback(ctx context.Context, draft Draft) error {
	if service == nil {
		return fmt.Errorf("record audit fallback: service is unavailable")
	}
	return service.recordFallback(ctx, draft, uuid.Nil)
}

// RecordItemFallback appends an item-level HTTP result only when the same
// request/action/target has no transactional audit row. The target metadata is
// overwritten from the server-owned argument so callers cannot alter the
// deduplication identity.
func (service *Service) RecordItemFallback(
	ctx context.Context,
	targetUserID uuid.UUID,
	draft Draft,
) error {
	if service == nil {
		return fmt.Errorf("record audit item fallback: service is unavailable")
	}
	if targetUserID == uuid.Nil {
		return fmt.Errorf("record audit item fallback: %w", ErrInvalidFilter)
	}
	draft.Metadata = copyMetadata(draft.Metadata)
	draft.Metadata[MetadataKeyTargetUserID] = targetUserID.String()
	return service.recordFallback(ctx, draft, targetUserID)
}

func (service *Service) recordFallback(
	ctx context.Context,
	draft Draft,
	targetUserID uuid.UUID,
) error {
	draft, err := service.normalizeRequestDraft(ctx, draft)
	if err != nil {
		return err
	}
	request := requestmeta.SnapshotFromContext(ctx)
	queryContext, cancel := context.WithTimeout(ctx, service.queryTimeout)
	defer cancel()
	transaction, err := service.database.Begin(queryContext)
	if err != nil {
		return fmt.Errorf("begin audit fallback: %w", err)
	}
	defer rollback(transaction)

	var alreadyRecorded bool
	query := `SELECT EXISTS (
    SELECT 1
    FROM tutorhub.audit_events
    WHERE request_instance_id = $1 AND action = $2
)`
	queryArguments := []any{request.RequestInstance, draft.Action}
	if targetUserID != uuid.Nil {
		query = `SELECT EXISTS (
    SELECT 1
    FROM tutorhub.audit_events
    WHERE tenant_id = $1
      AND request_instance_id = $2
      AND action = $3
      AND metadata ->> 'target_user_id' = $4
)`
		queryArguments = []any{
			draft.TenantID,
			request.RequestInstance,
			draft.Action,
			targetUserID.String(),
		}
	}
	if err := transaction.QueryRow(queryContext, query, queryArguments...).Scan(&alreadyRecorded); err != nil {
		return fmt.Errorf("check existing audit request result: %w", err)
	}
	if alreadyRecorded {
		return transaction.Commit(queryContext)
	}
	if err := AppendTransaction(queryContext, transaction, draft); err != nil {
		return err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return fmt.Errorf("commit audit fallback: %w", err)
	}
	return nil
}

func (service *Service) List(
	ctx context.Context,
	tenantContext tenancy.Context,
	tenantID uuid.UUID,
	filter Filter,
) (Page, error) {
	if service == nil {
		return Page{}, fmt.Errorf("list audit events: service is unavailable")
	}
	if err := tenantContext.Validate(); err != nil || tenantID == uuid.Nil || tenantID != tenantContext.TenantID {
		return Page{}, ErrNotFound
	}
	filter, err := normalizeFilter(filter)
	if err != nil {
		return Page{}, err
	}
	cursor, err := decodeCursor(tenantID, filter)
	if err != nil {
		return Page{}, err
	}

	queryContext, cancel := context.WithTimeout(ctx, service.queryTimeout)
	defer cancel()
	transaction, err := service.database.Begin(queryContext)
	if err != nil {
		return Page{}, fmt.Errorf("begin audit query: %w", err)
	}
	defer rollback(transaction)
	if err := service.authorizeView(queryContext, transaction, tenantContext); err != nil {
		return Page{}, err
	}
	page, err := queryEvents(queryContext, transaction, tenantID, filter, cursor)
	if err != nil {
		return Page{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Page{}, fmt.Errorf("commit audit query: %w", err)
	}
	return page, nil
}

func (service *Service) normalizeRequestDraft(ctx context.Context, draft Draft) (Draft, error) {
	request := requestmeta.SnapshotFromContext(ctx)
	tenantID := request.TenantID
	if request.AuditTenantResolved {
		tenantID = request.AuditTenantID
	}
	if tenantID == uuid.Nil || request.ActorID == uuid.Nil ||
		(draft.TenantID != uuid.Nil && draft.TenantID != tenantID) ||
		(draft.ActorID != uuid.Nil && draft.ActorID != request.ActorID) {
		return Draft{}, ErrAccessDenied
	}
	draft.TenantID = tenantID
	draft.ActorID = request.ActorID
	if draft.OccurredAt.IsZero() {
		draft.OccurredAt = service.clock().UTC()
	}
	if draft.Metadata == nil {
		draft.Metadata = Metadata{}
	}
	if err := validateDraft(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func (service *Service) authorizeView(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
) error {
	var tenantStatus string
	var membershipRole string
	var membershipStatus string
	err := transaction.QueryRow(
		ctx,
		`SELECT t.status, m.role, m.status
FROM tutorhub.tenants AS t
JOIN tutorhub.memberships AS m
  ON m.tenant_id = t.id AND m.user_id = $2
WHERE t.id = $1
FOR SHARE OF t, m`,
		tenantContext.TenantID,
		tenantContext.ActorID,
	).Scan(&tenantStatus, &membershipRole, &membershipStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load audit viewer authorization: %w", err)
	}
	if tenantStatus != "active" || membershipStatus != "active" {
		return ErrAccessDenied
	}
	decision := service.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID:          tenantContext.ActorID,
			ActiveTenantID:   tenantContext.TenantID,
			MembershipActive: true,
			OrganizationRoles: []policy.OrganizationRole{
				policy.OrganizationRole(membershipRole),
			},
		},
		Action: policy.ActionAuditView,
		Resource: policy.Resource{
			TenantID: tenantContext.TenantID,
			State:    policy.ResourceStateActive,
		},
	})
	if !decision.Allowed {
		return ErrAccessDenied
	}
	return nil
}

func normalizeFilter(filter Filter) (Filter, error) {
	if filter.Limit == 0 {
		filter.Limit = defaultPageLimit
	}
	if filter.Limit < 1 || filter.Limit > maximumPageLimit {
		return Filter{}, ErrInvalidFilter
	}
	if filter.OccurredFrom != nil {
		value := filter.OccurredFrom.UTC()
		filter.OccurredFrom = &value
	}
	if filter.OccurredTo != nil {
		value := filter.OccurredTo.UTC()
		filter.OccurredTo = &value
	}
	if filter.OccurredFrom != nil && filter.OccurredTo != nil &&
		!filter.OccurredFrom.Before(*filter.OccurredTo) {
		return Filter{}, ErrInvalidFilter
	}
	if filter.Action != "" {
		if _, ok := actionCatalog[filter.Action]; !ok {
			return Filter{}, ErrInvalidFilter
		}
	}
	filter.ResourceType = strings.TrimSpace(filter.ResourceType)
	if filter.ResourceType != "" && (len(filter.ResourceType) > 80 ||
		!resourceTypePattern.MatchString(filter.ResourceType)) {
		return Filter{}, ErrInvalidFilter
	}
	if filter.ResourceID != uuid.Nil && filter.ResourceType == "" {
		return Filter{}, ErrInvalidFilter
	}
	if filter.Outcome != "" {
		switch filter.Outcome {
		case OutcomeSucceeded, OutcomeDenied, OutcomeFailed:
		default:
			return Filter{}, ErrInvalidFilter
		}
	}
	return filter, nil
}

func queryEvents(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	filter Filter,
	cursor cursorPayload,
) (Page, error) {
	arguments := []any{tenantID}
	var query strings.Builder
	query.WriteString(`SELECT
    ae.id,
    ae.tenant_id,
    ae.actor_type,
    ae.actor_user_id,
    u.display_name,
    ae.action,
    ae.resource_type,
    ae.resource_id,
    ae.outcome,
    ae.request_id,
    ae.metadata,
    ae.occurred_at
FROM tutorhub.audit_events AS ae
LEFT JOIN tutorhub.users AS u ON u.id = ae.actor_user_id
WHERE ae.tenant_id = $1`)
	appendFilter := func(clause string, value any) {
		arguments = append(arguments, value)
		query.WriteString(fmt.Sprintf(clause, len(arguments)))
	}
	if filter.OccurredFrom != nil {
		appendFilter(" AND ae.occurred_at >= $%d", *filter.OccurredFrom)
	}
	if filter.OccurredTo != nil {
		appendFilter(" AND ae.occurred_at < $%d", *filter.OccurredTo)
	}
	if filter.Action != "" {
		appendFilter(" AND ae.action = $%d", filter.Action)
	}
	if filter.ResourceType != "" {
		appendFilter(" AND ae.resource_type = $%d", filter.ResourceType)
	}
	if filter.ResourceID != uuid.Nil {
		appendFilter(" AND ae.resource_id = $%d", filter.ResourceID)
	}
	if filter.Outcome != "" {
		appendFilter(" AND ae.outcome = $%d", filter.Outcome)
	}
	if cursor.ID != uuid.Nil {
		arguments = append(arguments, cursor.OccurredAt, cursor.ID)
		query.WriteString(fmt.Sprintf(
			" AND (ae.occurred_at, ae.id) < ($%d, $%d)",
			len(arguments)-1,
			len(arguments),
		))
	}
	arguments = append(arguments, filter.Limit+1)
	query.WriteString(fmt.Sprintf(" ORDER BY ae.occurred_at DESC, ae.id DESC LIMIT $%d", len(arguments)))

	rows, err := transaction.Query(ctx, query.String(), arguments...)
	if err != nil {
		return Page{}, fmt.Errorf("query audit events: %w", err)
	}
	defer rows.Close()
	items := make([]Event, 0, filter.Limit+1)
	for rows.Next() {
		var event Event
		var actorID uuid.NullUUID
		var actorName *string
		var resourceID uuid.NullUUID
		var metadataBytes []byte
		if err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.Actor.Type,
			&actorID,
			&actorName,
			&event.Action,
			&event.Resource.Type,
			&resourceID,
			&event.Outcome,
			&event.RequestID,
			&metadataBytes,
			&event.OccurredAt,
		); err != nil {
			return Page{}, fmt.Errorf("scan audit event: %w", err)
		}
		if actorID.Valid {
			value := actorID.UUID
			event.Actor.UserID = &value
		}
		event.Actor.DisplayName = actorName
		if resourceID.Valid {
			value := resourceID.UUID
			event.Resource.ID = &value
		}
		if err := json.Unmarshal(metadataBytes, &event.Metadata); err != nil {
			return Page{}, fmt.Errorf("decode audit metadata: %w", err)
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate audit events: %w", err)
	}

	page := Page{Items: items}
	if len(page.Items) > filter.Limit {
		page.Items = page.Items[:filter.Limit]
		nextCursor, err := encodeCursor(tenantID, filter, page.Items[len(page.Items)-1])
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = nextCursor
	}
	return page, nil
}

func rollback(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}
