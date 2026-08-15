package media

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const defaultLifecycleQueryTimeout = 10 * time.Second

type LifecycleDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type ClassSourceAuthority interface {
	AuthorizeMediaClass(
		context.Context,
		pgx.Tx,
		tenancy.Context,
		uuid.UUID,
		policy.Action,
	) (classroom.ClassStatus, error)
	ResolveMediaSource(
		context.Context,
		pgx.Tx,
		tenancy.Context,
		classroom.MediaSourceReference,
		policy.Action,
	) (classroom.MediaSourceSnapshot, error)
	TransitionMediaSession(
		context.Context,
		pgx.Tx,
		tenancy.Context,
		uuid.UUID,
		classroom.SessionStatus,
		classroom.SessionStatus,
		time.Time,
	) error
}

type PostgresLifecycleRepository struct {
	database     LifecycleDatabase
	queryTimeout time.Duration
	authorizer   policy.Authorizer
	controls     featurecontrol.Enforcer
	classSources ClassSourceAuthority
}

func NewPostgresLifecycleRepository(
	database LifecycleDatabase,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	controls featurecontrol.Enforcer,
	classSources ClassSourceAuthority,
) (*PostgresLifecycleRepository, error) {
	if database == nil || authorizer == nil || controls == nil || classSources == nil {
		return nil, fmt.Errorf(
			"media lifecycle database, authorizer, controls, and class sources are required",
		)
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultLifecycleQueryTimeout
	}
	return &PostgresLifecycleRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer,
		controls: controls, classSources: classSources,
	}, nil
}

func (repository *PostgresLifecycleRepository) CreateSpace(
	ctx context.Context,
	access AccessContext,
	command CreateSpaceCommand,
) (CreateSpaceResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return CreateSpaceResult{}, repository.unavailable("begin media space creation", err)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return CreateSpaceResult{}, err
	}
	access, scope, err := repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return CreateSpaceResult{}, err
	}
	if err := acquireLifecycleTransactionLock(
		queryContext,
		transaction,
		"media-space-create:"+scope.TenantID.String()+":"+scope.ActorID.String()+":"+command.IdempotencyKey,
	); err != nil {
		return CreateSpaceResult{}, repository.unavailable("lock media space creation", err)
	}
	replayed, found, err := repository.loadCreateReplay(
		queryContext, transaction, access, command,
	)
	if err != nil {
		return CreateSpaceResult{}, err
	}
	if found {
		if err := transaction.Commit(queryContext); err != nil {
			return CreateSpaceResult{}, repository.unavailable("commit media space replay", err)
		}
		return CreateSpaceResult{Space: replayed, Created: false}, nil
	}
	if err := repository.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return CreateSpaceResult{}, err
	}
	if command.Source.Kind == SourceInstant {
		if err := repository.controls.RequireFeature(
			queryContext, transaction, scope.TenantID, featurecontrol.FeatureInstantStudyRooms,
		); err != nil {
			return CreateSpaceResult{}, err
		}
	}

	var source SourceReference
	var classID uuid.UUID
	if command.Source.Kind == SourceInstant {
		if err := repository.authorizeInstant(access); err != nil {
			return CreateSpaceResult{}, err
		}
		if err := repository.requireStudyMeetingCapacity(
			queryContext, transaction, scope.TenantID, command.CreatedAt,
		); err != nil {
			return CreateSpaceResult{}, err
		}
		if _, err := repository.controls.ConsumeRateQuota(
			queryContext, transaction, scope.TenantID,
			featurecontrol.QuotaStudyMeetingCreationsPerHour, command.CreatedAt,
		); err != nil {
			return CreateSpaceResult{}, err
		}
		if err := repository.insertInstantStudyMeeting(
			queryContext, transaction, scope, command,
		); err != nil {
			return CreateSpaceResult{}, err
		}
		meetingID := command.InstantMeetingID
		source = SourceReference{Kind: SourceStudyMeeting, StudyMeetingID: &meetingID}
	} else {
		source, classID, err = repository.resolveCreateSource(
			queryContext, transaction, scope, command.Source,
		)
		if err != nil {
			return CreateSpaceResult{}, err
		}
		existing, exists, err := findSpaceBySource(
			queryContext, transaction, scope.TenantID, source,
		)
		if err != nil {
			return CreateSpaceResult{}, repository.unavailable("find media space source", err)
		}
		if exists {
			projected, err := repository.projectAuthorizedSpace(
				queryContext, transaction, access, scope, existing,
			)
			if err != nil {
				return CreateSpaceResult{}, err
			}
			if err := transaction.Commit(queryContext); err != nil {
				return CreateSpaceResult{}, repository.unavailable("commit existing media space", err)
			}
			return CreateSpaceResult{Space: projected, Created: false}, nil
		}
	}
	if err := repository.requireActiveSpaceCapacity(
		queryContext, transaction, scope.TenantID, 1,
	); err != nil {
		return CreateSpaceResult{}, err
	}
	created, err := insertSpace(
		queryContext, transaction, scope, command, source, classID,
	)
	if err != nil {
		return CreateSpaceResult{}, repository.unavailable("insert media space", err)
	}
	if err := appendMediaSpaceEvent(
		queryContext, transaction, scope, created, "media_space.created.v1",
		mediaEventDetails{}, command.CreatedAt,
	); err != nil {
		return CreateSpaceResult{}, err
	}
	projected, err := repository.projectAuthorizedSpace(
		queryContext, transaction, access, scope, created,
	)
	if err != nil {
		return CreateSpaceResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return CreateSpaceResult{}, repository.unavailable("commit media space creation", err)
	}
	return CreateSpaceResult{Space: projected, Created: true}, nil
}

func (repository *PostgresLifecycleRepository) GetSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
) (MediaSpace, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MediaSpace{}, repository.unavailable("begin media space read", err)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return MediaSpace{}, err
	}
	access, scope, err := repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return MediaSpace{}, err
	}
	row, err := loadSpace(queryContext, transaction, scope.TenantID, spaceID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaSpace{}, ErrSpaceNotFound
	}
	if err != nil {
		return MediaSpace{}, repository.unavailable("load media space", err)
	}
	space, err := repository.projectAuthorizedSpace(
		queryContext, transaction, access, scope, row,
	)
	if err != nil {
		return MediaSpace{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MediaSpace{}, repository.unavailable("commit media space read", err)
	}
	return space, nil
}

func (repository *PostgresLifecycleRepository) TransitionSpace(
	ctx context.Context,
	access AccessContext,
	command TransitionCommand,
) (MediaSpace, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MediaSpace{}, repository.unavailable("begin media space transition", err)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return MediaSpace{}, err
	}
	access, scope, err := repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return MediaSpace{}, err
	}
	if err := acquireLifecycleTransactionLock(
		queryContext,
		transaction,
		"media-space-mutation:"+scope.TenantID.String()+":"+scope.ActorID.String()+":"+
			command.IdempotencyKey,
	); err != nil {
		return MediaSpace{}, repository.unavailable("lock media space transition", err)
	}
	if replay, found, err := repository.loadTransitionReplay(
		queryContext, transaction, access, scope, command,
	); err != nil {
		return MediaSpace{}, err
	} else if found {
		if command.Operation == "recover" {
			if err := repository.reauthorizeRecoveryReplay(
				queryContext, transaction, access, scope, command.SpaceID,
			); err != nil {
				return MediaSpace{}, err
			}
		}
		if err := transaction.Commit(queryContext); err != nil {
			return MediaSpace{}, repository.unavailable("commit media transition replay", err)
		}
		return replay, nil
	}
	row, err := loadSpace(queryContext, transaction, scope.TenantID, command.SpaceID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaSpace{}, ErrSpaceNotFound
	}
	if err != nil {
		return MediaSpace{}, repository.unavailable("lock media space", err)
	}
	if err := repository.authorizeTransition(
		queryContext, transaction, access, scope, row, command.Operation,
	); err != nil {
		return MediaSpace{}, err
	}
	if row.Version != command.ExpectedVersion {
		return MediaSpace{}, ErrSpaceVersionConflict
	}

	var transitioned spaceRow
	var resultInstanceID uuid.UUID
	switch command.Operation {
	case "start":
		transitioned, resultInstanceID, err = repository.startSpace(
			queryContext, transaction, access, scope, row, command,
		)
	case "end":
		transitioned, resultInstanceID, err = repository.endSpace(
			queryContext, transaction, access, scope, row, command,
		)
	case "cancel":
		transitioned, err = repository.cancelSpace(
			queryContext, transaction, access, scope, row, command,
		)
	case "recover":
		transitioned, resultInstanceID, err = repository.recoverSpace(
			queryContext, transaction, access, scope, row, command,
		)
	default:
		err = ErrInvalidSpaceRequest
	}
	if err != nil {
		return MediaSpace{}, err
	}
	if err := insertTransitionReceipt(
		queryContext, transaction, scope, command, transitioned.Version, resultInstanceID,
	); err != nil {
		return MediaSpace{}, repository.unavailable("record media transition receipt", err)
	}
	projected, err := repository.projectAuthorizedSpace(
		queryContext, transaction, access, scope, transitioned,
	)
	if err != nil {
		return MediaSpace{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MediaSpace{}, repository.unavailable("commit media space transition", err)
	}
	return projected, nil
}

func (repository *PostgresLifecycleRepository) authorizeTransition(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	row spaceRow,
	operation string,
) error {
	var action policy.Action
	allowSafetyEnd := false
	switch operation {
	case "start":
		action = policy.ActionSessionStart
	case "end":
		action = policy.ActionSessionEnd
		allowSafetyEnd = true
	case "cancel":
		action = policy.ActionSessionEnd
	case "recover":
		action = policy.ActionSessionStart
	default:
		return ErrInvalidSpaceRequest
	}
	_, err := repository.authorizeSource(
		ctx, transaction, access, scope, row, action, true, allowSafetyEnd,
	)
	return err
}

func (repository *PostgresLifecycleRepository) startSpace(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	row spaceRow,
	command TransitionCommand,
) (spaceRow, uuid.UUID, error) {
	if row.Status != SpaceStatusScheduled || command.RoomInstanceID == uuid.Nil ||
		command.ProviderRoomName == "" {
		return spaceRow{}, uuid.Nil, ErrSpaceTransition
	}
	if err := repository.controls.RequireFeature(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	source, err := repository.authorizeSource(
		ctx, transaction, access, scope, row, policy.ActionSessionStart, true, false,
	)
	if err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	if source.Status != "scheduled" {
		return spaceRow{}, uuid.Nil, ErrSourceUnavailable
	}
	if err := repository.requireActiveSpaceCapacity(ctx, transaction, scope.TenantID, 0); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	if _, err := repository.controls.ConsumeRateQuota(
		ctx, transaction, scope.TenantID,
		featurecontrol.QuotaMediaSpaceStartsPerHour, command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.media_room_instances (
    id, tenant_id, space_id, attempt_number, provider_room_name,
    created_by, updated_by, created_at, updated_at
)
VALUES ($1, $2, $3, 1, $4, $5, $5, $6, $6)`,
		command.RoomInstanceID, scope.TenantID, row.ID,
		command.ProviderRoomName, scope.ActorID, command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("insert room instance intent", err)
	}
	updated, err := scanSpace(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.media_spaces
SET status = 'open', version = version + 1, updated_by = $3,
    opened_at = $4, opened_by = $3, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $5 AND status = 'scheduled'
RETURNING `+spaceReturning,
		scope.TenantID, row.ID, scope.ActorID, command.OccurredAt, command.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return spaceRow{}, uuid.Nil, ErrSpaceVersionConflict
	}
	if err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("open media space", err)
	}
	if source.Kind == SourceClassSession {
		if err := repository.classSources.TransitionMediaSession(
			ctx, transaction, scope, source.ClassSessionID,
			classroom.SessionStatusScheduled, classroom.SessionStatusLive, command.OccurredAt,
		); err != nil {
			return spaceRow{}, uuid.Nil, mapClassSourceError(err, false)
		}
	}
	if err := appendMediaSpaceEvent(
		ctx, transaction, scope, updated, "media_space.started.v1",
		mediaEventDetails{ReasonCode: command.ReasonCode}, command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	return updated, command.RoomInstanceID, nil
}

func (repository *PostgresLifecycleRepository) endSpace(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	row spaceRow,
	command TransitionCommand,
) (spaceRow, uuid.UUID, error) {
	if row.Status != SpaceStatusOpen {
		return spaceRow{}, uuid.Nil, ErrSpaceTransition
	}
	source, err := repository.authorizeSource(
		ctx, transaction, access, scope, row, policy.ActionSessionEnd, true, true,
	)
	if err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	safetyEnd := source.Kind == SourceStudyMeeting && source.SafetyAdmin && !source.Owner
	if safetyEnd && command.ReasonCode == "" {
		return spaceRow{}, uuid.Nil, ErrInvalidSpaceRequest
	}
	instance, err := loadRoomForTermination(ctx, transaction, scope.TenantID, row.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return spaceRow{}, uuid.Nil, ErrSpaceTransition
	}
	if err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("lock active room instance", err)
	}
	if err := terminateRoomIntent(ctx, transaction, scope, instance, command.OccurredAt); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	if err := expireOutstandingLobbyAdmissions(
		ctx,
		transaction,
		scope.TenantID,
		row.ID,
		instance.ID,
		&scope.ActorID,
		"meeting_ended",
		command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("expire media lobby admissions", err)
	}
	if err := terminateRoomParticipants(
		ctx,
		transaction,
		scope.TenantID,
		row.ID,
		instance.ID,
		command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("terminate media room participants", err)
	}
	updated, err := scanSpace(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.media_spaces
SET status = 'ended', version = version + 1, locked = false, updated_by = $3,
    ended_at = $4, ended_by = $3, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $5 AND status = 'open'
RETURNING `+spaceReturning,
		scope.TenantID, row.ID, scope.ActorID, command.OccurredAt, command.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return spaceRow{}, uuid.Nil, ErrSpaceVersionConflict
	}
	if err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("end media space", err)
	}
	if source.Kind == SourceClassSession && source.Status == "live" {
		if err := repository.classSources.TransitionMediaSession(
			ctx, transaction, scope, source.ClassSessionID,
			classroom.SessionStatusLive, classroom.SessionStatusEnded, command.OccurredAt,
		); err != nil {
			return spaceRow{}, uuid.Nil, mapClassSourceError(err, false)
		}
	}
	if err := appendMediaSpaceEvent(
		ctx, transaction, scope, updated, "media_space.ended.v1",
		mediaEventDetails{ReasonCode: command.ReasonCode, SafetyAction: safetyEnd},
		command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	return updated, instance.ID, nil
}

func (repository *PostgresLifecycleRepository) cancelSpace(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	row spaceRow,
	command TransitionCommand,
) (spaceRow, error) {
	if row.Status != SpaceStatusScheduled {
		return spaceRow{}, ErrSpaceTransition
	}
	if _, err := repository.authorizeSource(
		ctx, transaction, access, scope, row, policy.ActionSessionEnd, true, false,
	); err != nil {
		return spaceRow{}, err
	}
	updated, err := scanSpace(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.media_spaces
SET status = 'cancelled', version = version + 1, updated_by = $3,
    cancelled_at = $4, cancelled_by = $3, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $5 AND status = 'scheduled'
RETURNING `+spaceReturning,
		scope.TenantID, row.ID, scope.ActorID, command.OccurredAt, command.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return spaceRow{}, ErrSpaceVersionConflict
	}
	if err != nil {
		return spaceRow{}, repository.unavailable("cancel media space", err)
	}
	if err := appendMediaSpaceEvent(
		ctx, transaction, scope, updated, "media_space.cancelled.v1",
		mediaEventDetails{ReasonCode: command.ReasonCode}, command.OccurredAt,
	); err != nil {
		return spaceRow{}, err
	}
	return updated, nil
}

func (repository *PostgresLifecycleRepository) recoverSpace(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	row spaceRow,
	command TransitionCommand,
) (spaceRow, uuid.UUID, error) {
	if row.Status != SpaceStatusOpen || row.Locked || command.RoomInstanceID == uuid.Nil ||
		command.ProviderRoomName == "" || command.ExpectedRoomInstanceID == uuid.Nil ||
		command.ExpectedRoomInstanceVersion < 1 {
		return spaceRow{}, uuid.Nil, ErrSpaceTransition
	}
	if err := repository.controls.RequireFeature(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	source, err := repository.authorizeSource(
		ctx, transaction, access, scope, row, policy.ActionSessionStart, true, false,
	)
	if err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	if !recoverySourceStatusAllowed(source) {
		return spaceRow{}, uuid.Nil, ErrSourceUnavailable
	}
	previous, err := loadLatestRoom(ctx, transaction, scope.TenantID, row.ID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return spaceRow{}, uuid.Nil, ErrSpaceTransition
	}
	if err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("lock failed room instance", err)
	}
	if previous.ID != command.ExpectedRoomInstanceID ||
		previous.Version != command.ExpectedRoomInstanceVersion {
		return spaceRow{}, uuid.Nil, ErrSpaceVersionConflict
	}
	if previous.Status != RoomInstanceFailed {
		return spaceRow{}, uuid.Nil, ErrSpaceTransition
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.media_room_instances (
    id, tenant_id, space_id, attempt_number, provider_room_name,
    created_by, updated_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $7)`,
		command.RoomInstanceID, scope.TenantID, row.ID, previous.AttemptNumber+1,
		command.ProviderRoomName, scope.ActorID, command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("insert recovery room instance", err)
	}
	updated, err := scanSpace(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.media_spaces
SET version = version + 1, updated_by = $3, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND version = $5 AND status = 'open'
RETURNING `+spaceReturning,
		scope.TenantID, row.ID, scope.ActorID, command.OccurredAt, command.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return spaceRow{}, uuid.Nil, ErrSpaceVersionConflict
	}
	if err != nil {
		return spaceRow{}, uuid.Nil, repository.unavailable("advance recovered media space", err)
	}
	if err := appendMediaSpaceEvent(
		ctx, transaction, scope, updated, "media_space.recovered.v1",
		mediaEventDetails{}, command.OccurredAt,
	); err != nil {
		return spaceRow{}, uuid.Nil, err
	}
	return updated, command.RoomInstanceID, nil
}

func (repository *PostgresLifecycleRepository) reauthorizeRecoveryReplay(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	spaceID uuid.UUID,
) error {
	row, err := loadSpace(ctx, transaction, scope.TenantID, spaceID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSpaceNotFound
	}
	if err != nil {
		return repository.unavailable("lock replayed recovery space", err)
	}
	if row.Status != SpaceStatusOpen || row.Locked {
		return ErrSpaceTransition
	}
	if err := repository.controls.RequireFeature(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return err
	}
	source, err := repository.authorizeSource(
		ctx, transaction, access, scope, row, policy.ActionSessionStart, true, false,
	)
	if err != nil {
		return err
	}
	if !recoverySourceStatusAllowed(source) {
		return ErrSourceUnavailable
	}
	return nil
}

func recoverySourceStatusAllowed(source authorizedSource) bool {
	switch source.Kind {
	case SourceClassSession:
		return source.Status == string(classroom.SessionStatusLive)
	case SourceClassSessionOccurrence, SourceStudyMeeting:
		return source.Status == "scheduled" || source.Status == "live"
	default:
		return false
	}
}

type authorizedSource struct {
	Kind                       SourceKind
	ClassSessionID             uuid.UUID
	Status                     string
	Owner                      bool
	SafetyAdmin                bool
	CanJoin                    bool
	InstanceRole               InstanceRole
	CanPublishCameraMicrophone bool
	CanShareScreen             bool
}

func (repository *PostgresLifecycleRepository) authorizeSource(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	row spaceRow,
	action policy.Action,
	mutating bool,
	allowSafetyEnd bool,
) (authorizedSource, error) {
	switch row.Source.Kind {
	case SourceClassSession, SourceClassSessionOccurrence:
		reference := classroom.MediaSourceReference{
			Kind:          classroom.MediaSourceKind(row.Source.Kind),
			OccurrenceKey: row.Source.OccurrenceKey,
		}
		if row.Source.ClassSessionID != nil {
			reference.ClassSessionID = *row.Source.ClassSessionID
		}
		if row.Source.SeriesID != nil {
			reference.SeriesID = *row.Source.SeriesID
		}
		// A mutation denial must not reveal that a same-tenant class source
		// exists to an actor who cannot view that class. Establish visibility
		// first, then evaluate the stronger lifecycle action so visible students
		// still receive an ordinary permission denial.
		if mutating && action != policy.ActionClassView {
			if _, err := repository.classSources.ResolveMediaSource(
				ctx, transaction, scope, reference, policy.ActionClassView,
			); err != nil {
				return authorizedSource{}, mapClassSourceError(err, true)
			}
		}
		snapshot, err := repository.classSources.ResolveMediaSource(
			ctx, transaction, scope, reference, action,
		)
		if err != nil {
			return authorizedSource{}, mapClassSourceError(err, !mutating)
		}
		return authorizedSource{
			Kind: row.Source.Kind, ClassSessionID: snapshot.ClassSessionID,
			Status:                     snapshot.Status,
			SafetyAdmin:                classSafetyAdmin(snapshot.ClassRole, access.OrganizationRoles),
			CanJoin:                    true,
			InstanceRole:               instanceRoleForClass(snapshot.ClassRole, access.OrganizationRoles),
			CanPublishCameraMicrophone: snapshot.CanPublishMedia,
			CanShareScreen:             snapshot.CanShareScreen,
		}, nil
	case SourceStudyMeeting:
		meeting, err := loadStudyMeeting(
			ctx, transaction, scope.TenantID, *row.Source.StudyMeetingID, mutating,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return authorizedSource{}, ErrSourceUnavailable
		}
		if err != nil {
			return authorizedSource{}, repository.unavailable("load media study meeting", err)
		}
		owner := meeting.OwnerUserID == scope.ActorID
		safetyAdmin := hasOrganizationRole(access.OrganizationRoles, policy.OrganizationRoleAdmin)
		explicitMember := false
		if !owner {
			if err := transaction.QueryRow(
				ctx,
				`SELECT EXISTS (
    SELECT 1 FROM tutorhub.media_space_members
    WHERE tenant_id = $1 AND space_id = $2 AND user_id = $3 AND status = 'active'
)`,
				scope.TenantID, row.ID, scope.ActorID,
			).Scan(&explicitMember); err != nil {
				return authorizedSource{}, repository.unavailable("authorize media space member", err)
			}
		}
		visible := owner || safetyAdmin || explicitMember
		if !visible {
			return authorizedSource{}, ErrSpaceNotFound
		}
		if mutating && !owner && !(allowSafetyEnd && safetyAdmin) {
			return authorizedSource{}, ErrSpaceAccessDenied
		}
		if meeting.ClassID != nil {
			classAction := policy.ActionClassView
			if action == policy.ActionSessionStart {
				classAction = policy.ActionSessionStart
			} else if action == policy.ActionSessionJoin {
				classAction = policy.ActionSessionJoin
			}
			if _, err := repository.classSources.AuthorizeMediaClass(
				ctx, transaction, scope, *meeting.ClassID, classAction,
			); err != nil {
				return authorizedSource{}, mapClassSourceError(err, !mutating)
			}
		}
		instanceRole := InstanceRoleAttendee
		if owner {
			instanceRole = InstanceRoleHost
		}
		return authorizedSource{
			Kind: SourceStudyMeeting, Status: meeting.Status,
			Owner: owner, SafetyAdmin: safetyAdmin && !owner, CanJoin: owner || explicitMember,
			InstanceRole:               instanceRole,
			CanPublishCameraMicrophone: true,
			CanShareScreen:             owner,
		}, nil
	default:
		return authorizedSource{}, ErrSourceUnavailable
	}
}

func instanceRoleForClass(
	classRole *policy.ClassRole,
	organizationRoles []policy.OrganizationRole,
) InstanceRole {
	if classRole != nil {
		switch *classRole {
		case policy.ClassRoleOwner:
			return InstanceRoleHost
		case policy.ClassRoleCoTeacher:
			return InstanceRoleCoHost
		case policy.ClassRoleTeachingAssistant:
			return InstanceRoleTeachingAssistant
		}
	}
	if hasOrganizationRole(organizationRoles, policy.OrganizationRoleTeacher) {
		return InstanceRoleCoHost
	}
	return InstanceRoleAttendee
}

func classSafetyAdmin(
	classRole *policy.ClassRole,
	organizationRoles []policy.OrganizationRole,
) bool {
	if !hasOrganizationRole(organizationRoles, policy.OrganizationRoleAdmin) ||
		hasOrganizationRole(organizationRoles, policy.OrganizationRoleTeacher) {
		return false
	}
	if classRole == nil {
		return true
	}
	return *classRole != policy.ClassRoleOwner && *classRole != policy.ClassRoleCoTeacher
}

func (repository *PostgresLifecycleRepository) projectAuthorizedSpace(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	row spaceRow,
) (MediaSpace, error) {
	viewerSource, err := repository.authorizeSource(
		ctx, transaction, access, scope, row, policy.ActionClassView, false, false,
	)
	if err != nil {
		if errors.Is(err, ErrSpaceAccessDenied) {
			return MediaSpace{}, ErrSpaceNotFound
		}
		return MediaSpace{}, err
	}
	operations := ViewerOperations{}
	featureEnabled := repository.controls.RequireFeature(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	) == nil
	if row.Status == SpaceStatusScheduled {
		if featureEnabled {
			if _, err := repository.authorizeSource(
				ctx, transaction, access, scope, row, policy.ActionSessionStart, true, false,
			); err == nil {
				operations.CanStart = true
			}
		}
		if _, err := repository.authorizeSource(
			ctx, transaction, access, scope, row, policy.ActionSessionEnd, true, false,
		); err == nil {
			operations.CanCancel = true
		}
	}
	if row.Status == SpaceStatusOpen {
		if _, err := repository.authorizeSource(
			ctx, transaction, access, scope, row, policy.ActionSessionEnd, true, true,
		); err == nil {
			operations.CanEnd = true
		}
		if featureEnabled {
			if _, err := repository.authorizeSource(
				ctx, transaction, access, scope, row, policy.ActionParticipantAdmit, true, false,
			); err == nil {
				operations.CanManageAdmissions = true
			}
		}
	}
	operations.CanManageInvites = featureEnabled &&
		(row.Status == SpaceStatusScheduled || row.Status == SpaceStatusOpen) &&
		row.Source.Kind == SourceStudyMeeting && viewerSource.Owner
	space := row.project(operations)
	instance, err := loadActiveRoom(ctx, transaction, scope.TenantID, row.ID, false)
	if err == nil {
		value := instance.project()
		space.ActiveRoomInstance = &value
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return MediaSpace{}, repository.unavailable("load active room instance", err)
	} else {
		latest, latestErr := loadLatestRoom(ctx, transaction, scope.TenantID, row.ID, false)
		if latestErr == nil && latest.Status == RoomInstanceFailed {
			value := latest.project()
			space.RecoveryRoomInstance = &value
			if row.Status == SpaceStatusOpen && !row.Locked && featureEnabled {
				if source, authorizeErr := repository.authorizeSource(
					ctx, transaction, access, scope, row,
					policy.ActionSessionStart, true, false,
				); authorizeErr == nil && recoverySourceStatusAllowed(source) {
					space.ViewerOperations.CanRecover = true
				}
			}
		} else if latestErr != nil && !errors.Is(latestErr, pgx.ErrNoRows) {
			return MediaSpace{}, repository.unavailable("load latest room instance", latestErr)
		}
	}
	return space, nil
}

func (repository *PostgresLifecycleRepository) resolveCreateSource(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	input CreateSourceInput,
) (SourceReference, uuid.UUID, error) {
	switch input.Kind {
	case SourceClassSession, SourceClassSessionOccurrence:
		reference := classroom.MediaSourceReference{
			Kind: classroom.MediaSourceKind(input.Kind), ClassSessionID: input.ClassSessionID,
			SeriesID: input.SeriesID, OccurrenceKey: input.OccurrenceKey,
		}
		snapshot, err := repository.classSources.ResolveMediaSource(
			ctx, transaction, scope, reference, policy.ActionSessionStart,
		)
		if err != nil {
			return SourceReference{}, uuid.Nil, mapClassSourceError(err, false)
		}
		if snapshot.Status != "scheduled" {
			return SourceReference{}, uuid.Nil, ErrSourceUnavailable
		}
		if input.Kind == SourceClassSession {
			id := input.ClassSessionID
			return SourceReference{Kind: input.Kind, ClassSessionID: &id}, snapshot.ClassID, nil
		}
		id := input.SeriesID
		return SourceReference{
			Kind: input.Kind, SeriesID: &id, OccurrenceKey: input.OccurrenceKey,
		}, snapshot.ClassID, nil
	case SourceStudyMeeting:
		meeting, err := loadStudyMeeting(ctx, transaction, scope.TenantID, input.StudyMeetingID, true)
		if errors.Is(err, pgx.ErrNoRows) {
			return SourceReference{}, uuid.Nil, ErrSourceUnavailable
		}
		if err != nil {
			return SourceReference{}, uuid.Nil, repository.unavailable("resolve study meeting", err)
		}
		if meeting.OwnerUserID != scope.ActorID {
			return SourceReference{}, uuid.Nil, ErrSpaceAccessDenied
		}
		if meeting.Status != "scheduled" {
			return SourceReference{}, uuid.Nil, ErrSourceUnavailable
		}
		if meeting.ClassID != nil {
			status, err := repository.classSources.AuthorizeMediaClass(
				ctx, transaction, scope, *meeting.ClassID, policy.ActionSessionStart,
			)
			if err != nil {
				return SourceReference{}, uuid.Nil, mapClassSourceError(err, false)
			}
			if status != classroom.ClassStatusActive {
				return SourceReference{}, uuid.Nil, ErrSourceUnavailable
			}
		}
		id := input.StudyMeetingID
		return SourceReference{Kind: SourceStudyMeeting, StudyMeetingID: &id}, uuid.Nil, nil
	default:
		return SourceReference{}, uuid.Nil, ErrInvalidSpaceRequest
	}
}

func (repository *PostgresLifecycleRepository) authorizeInstant(access AccessContext) error {
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: access.ActorID, ActiveTenantID: access.TenantID,
			MembershipActive: true, OrganizationRoles: access.OrganizationRoles,
		},
		Action:   policy.ActionRoomCreateInstant,
		Resource: policy.Resource{TenantID: access.TenantID, State: policy.ResourceStateActive},
	})
	if !decision.Allowed {
		return ErrSpaceAccessDenied
	}
	return nil
}

func (repository *PostgresLifecycleRepository) requireActiveScope(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
) (AccessContext, tenancy.Context, error) {
	if access.TenantID == uuid.Nil || access.ActorID == uuid.Nil {
		return AccessContext{}, tenancy.Context{}, ErrSpaceAccessDenied
	}
	var role policy.OrganizationRole
	err := transaction.QueryRow(
		ctx,
		`SELECT membership.role
FROM tutorhub.memberships AS membership
JOIN tutorhub.tenants AS tenant ON tenant.id = membership.tenant_id
WHERE membership.tenant_id = $1 AND membership.user_id = $2
  AND membership.status = 'active' AND tenant.status = 'active'
FOR SHARE OF membership, tenant`,
		access.TenantID, access.ActorID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, tenancy.Context{}, ErrSpaceAccessDenied
	}
	if err != nil {
		return AccessContext{}, tenancy.Context{}, repository.unavailable("authorize media membership", err)
	}
	scope, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, ErrSpaceAccessDenied
	}
	access.MembershipActive = true
	access.OrganizationRoles = []policy.OrganizationRole{role}
	access.ClassRoles = nil
	access.Role = string(role)
	return access, scope, nil
}

func (repository *PostgresLifecycleRepository) acquireTenantControlLock(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
) error {
	if tenantID == uuid.Nil {
		return nil
	}
	return featurecontrol.AcquireTenantControlLock(ctx, transaction, tenantID)
}

func (repository *PostgresLifecycleRepository) requireActiveSpaceCapacity(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	additional int64,
) error {
	var count int64
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND status IN ('scheduled', 'open')`,
		tenantID,
	).Scan(&count); err != nil {
		return repository.unavailable("count active media spaces", err)
	}
	return repository.controls.RequireQuotaAtMost(
		ctx, transaction, tenantID, featurecontrol.QuotaActiveMediaSpaces, count+additional,
	)
}

func (repository *PostgresLifecycleRepository) requireStudyMeetingCapacity(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	now time.Time,
) error {
	var count int64
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND status = 'scheduled' AND ends_at > $2`,
		tenantID, now,
	).Scan(&count); err != nil {
		return repository.unavailable("count active study meetings", err)
	}
	return repository.controls.RequireQuotaAtMost(
		ctx, transaction, tenantID, featurecontrol.QuotaActiveStudyMeetings, count+1,
	)
}

func (repository *PostgresLifecycleRepository) insertInstantStudyMeeting(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	command CreateSpaceCommand,
) error {
	endsAt := command.CreatedAt.Add(
		time.Duration(command.Source.Instant.DurationMinutes) * time.Minute,
	)
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.study_meetings (
    id, tenant_id, owner_user_id, title, starts_at, ends_at, timezone,
    create_idempotency_key, create_request_fingerprint, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $5, $5)`,
		command.InstantMeetingID, scope.TenantID, scope.ActorID,
		command.Source.Instant.Title, command.CreatedAt, endsAt,
		command.Source.Instant.Timezone, command.IdempotencyKey, command.Fingerprint,
	); err != nil {
		return repository.unavailable("insert instant study meeting", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'study_meeting', $2, 'study_meeting.scheduled.v1',
    jsonb_build_object(
        'meeting_id', $2::uuid, 'actor_user_id', $3::uuid,
        'status', 'scheduled', 'version', 1::bigint
    ),
    $4, $4
)`,
		scope.TenantID, command.InstantMeetingID, scope.ActorID, command.CreatedAt,
	); err != nil {
		return repository.unavailable("append instant study meeting outbox", err)
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: scope.TenantID, ActorID: scope.ActorID,
		EventType: "study_meeting.scheduled.v1", AggregateType: "study_meeting",
		AggregateID: command.InstantMeetingID,
		Metadata:    audit.Metadata{"status": "scheduled", "version": "1"},
		OccurredAt:  command.CreatedAt,
	}); err != nil {
		return fmt.Errorf("append instant study meeting audit: %w", err)
	}
	return nil
}

func (repository *PostgresLifecycleRepository) loadCreateReplay(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	command CreateSpaceCommand,
) (MediaSpace, bool, error) {
	row, persistedFingerprint, err := loadSpaceByCreateKey(
		ctx, transaction, access.TenantID, access.ActorID, command.IdempotencyKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaSpace{}, false, nil
	}
	if err != nil {
		return MediaSpace{}, false, repository.unavailable("load media create receipt", err)
	}
	if !bytes.Equal(persistedFingerprint, command.Fingerprint) {
		return MediaSpace{}, false, ErrSpaceIdempotency
	}
	scope, _ := tenancy.New(access.TenantID, access.ActorID)
	space, err := repository.projectAuthorizedSpace(ctx, transaction, access, scope, row)
	return space, true, err
}

func (repository *PostgresLifecycleRepository) loadTransitionReplay(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	command TransitionCommand,
) (MediaSpace, bool, error) {
	var fingerprint []byte
	var operation string
	var spaceID uuid.UUID
	var actorID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		`SELECT request_fingerprint, operation, space_id, actor_user_id
FROM tutorhub.media_space_mutation_receipts
WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3`,
		scope.TenantID, scope.ActorID, command.IdempotencyKey,
	).Scan(&fingerprint, &operation, &spaceID, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaSpace{}, false, nil
	}
	if err != nil {
		return MediaSpace{}, false, repository.unavailable("load media transition receipt", err)
	}
	if operation != command.Operation || spaceID != command.SpaceID || actorID != scope.ActorID ||
		!bytes.Equal(fingerprint, command.Fingerprint) {
		return MediaSpace{}, false, ErrSpaceIdempotency
	}
	row, err := loadSpace(ctx, transaction, scope.TenantID, spaceID, false)
	if err != nil {
		return MediaSpace{}, false, repository.unavailable("load replayed media space", err)
	}
	space, err := repository.projectAuthorizedSpace(ctx, transaction, access, scope, row)
	return space, true, err
}

type studyMeetingRow struct {
	OwnerUserID uuid.UUID
	ClassID     *uuid.UUID
	Status      string
}

func loadStudyMeeting(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	meetingID uuid.UUID,
	lock bool,
) (studyMeetingRow, error) {
	query := `SELECT owner_user_id, class_id, status FROM tutorhub.study_meetings
WHERE tenant_id = $1 AND id = $2`
	if lock {
		query += " FOR UPDATE"
	}
	var meeting studyMeetingRow
	var classID uuid.NullUUID
	err := transaction.QueryRow(ctx, query, tenantID, meetingID).Scan(
		&meeting.OwnerUserID, &classID, &meeting.Status,
	)
	if classID.Valid {
		value := classID.UUID
		meeting.ClassID = &value
	}
	return meeting, err
}

type spaceRow struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Source       SourceReference
	Status       SpaceStatus
	Version      int64
	LobbyEnabled bool
	Locked       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const spaceReturning = `id, tenant_id, source_kind, class_id,
       source_class_session_id, source_series_id, source_occurrence_key,
       source_study_meeting_id, status, version, lobby_enabled, locked,
       created_at, updated_at`

func loadSpace(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	lock bool,
) (spaceRow, error) {
	query := `SELECT ` + spaceReturning + ` FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND id = $2`
	if lock {
		query += " FOR UPDATE"
	}
	return scanSpace(transaction.QueryRow(ctx, query, tenantID, spaceID))
}

func loadSpaceByCreateKey(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	key string,
) (spaceRow, []byte, error) {
	var row spaceRow
	var sourceKind SourceKind
	var classID, classSessionID, seriesID, studyMeetingID uuid.NullUUID
	var occurrence sql.NullString
	var fingerprint []byte
	err := transaction.QueryRow(
		ctx,
		`SELECT `+spaceReturning+`, create_request_fingerprint
FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND created_by = $2 AND create_idempotency_key = $3`,
		tenantID, actorID, key,
	).Scan(
		&row.ID, &row.TenantID, &sourceKind, &classID, &classSessionID,
		&seriesID, &occurrence, &studyMeetingID, &row.Status, &row.Version,
		&row.LobbyEnabled, &row.Locked, &row.CreatedAt, &row.UpdatedAt, &fingerprint,
	)
	if err != nil {
		return spaceRow{}, nil, err
	}
	row.Source = sourceFromSQL(sourceKind, classSessionID, seriesID, occurrence, studyMeetingID)
	return row, fingerprint, nil
}

func scanSpace(row interface{ Scan(...any) error }) (spaceRow, error) {
	var value spaceRow
	var kind SourceKind
	var classID, classSessionID, seriesID, studyMeetingID uuid.NullUUID
	var occurrence sql.NullString
	err := row.Scan(
		&value.ID, &value.TenantID, &kind, &classID, &classSessionID,
		&seriesID, &occurrence, &studyMeetingID, &value.Status, &value.Version,
		&value.LobbyEnabled, &value.Locked, &value.CreatedAt, &value.UpdatedAt,
	)
	if err != nil {
		return spaceRow{}, err
	}
	value.Source = sourceFromSQL(kind, classSessionID, seriesID, occurrence, studyMeetingID)
	value.CreatedAt, value.UpdatedAt = value.CreatedAt.UTC(), value.UpdatedAt.UTC()
	return value, nil
}

func sourceFromSQL(
	kind SourceKind,
	classSessionID uuid.NullUUID,
	seriesID uuid.NullUUID,
	occurrence sql.NullString,
	studyMeetingID uuid.NullUUID,
) SourceReference {
	source := SourceReference{Kind: kind}
	if classSessionID.Valid {
		value := classSessionID.UUID
		source.ClassSessionID = &value
	}
	if seriesID.Valid {
		value := seriesID.UUID
		source.SeriesID = &value
	}
	if occurrence.Valid {
		source.OccurrenceKey = occurrence.String
	}
	if studyMeetingID.Valid {
		value := studyMeetingID.UUID
		source.StudyMeetingID = &value
	}
	return source
}

func (row spaceRow) project(operations ViewerOperations) MediaSpace {
	return MediaSpace{
		ID: row.ID, Source: row.Source, Status: row.Status, Version: row.Version,
		ViewerOperations: operations, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func insertSpace(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	command CreateSpaceCommand,
	source SourceReference,
	classID uuid.UUID,
) (spaceRow, error) {
	return scanSpace(transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.media_spaces (
    id, tenant_id, source_kind, class_id, source_class_session_id,
    source_series_id, source_occurrence_key, source_study_meeting_id,
    create_idempotency_key, create_request_fingerprint,
    created_by, updated_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11, $12, $12)
RETURNING `+spaceReturning,
		command.SpaceID, scope.TenantID, source.Kind, nullableLifecycleUUID(classID),
		nullableLifecycleUUIDPointer(source.ClassSessionID),
		nullableLifecycleUUIDPointer(source.SeriesID),
		nullableLifecycleString(source.OccurrenceKey),
		nullableLifecycleUUIDPointer(source.StudyMeetingID),
		command.IdempotencyKey, command.Fingerprint, scope.ActorID, command.CreatedAt,
	))
}

func findSpaceBySource(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	source SourceReference,
) (spaceRow, bool, error) {
	query := ""
	arguments := []any{tenantID}
	switch source.Kind {
	case SourceClassSession:
		query = `SELECT ` + spaceReturning + ` FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND source_kind = 'class_session' AND source_class_session_id = $2`
		arguments = append(arguments, *source.ClassSessionID)
	case SourceClassSessionOccurrence:
		query = `SELECT ` + spaceReturning + ` FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND source_kind = 'class_session_occurrence'
  AND source_series_id = $2 AND source_occurrence_key = $3`
		arguments = append(arguments, *source.SeriesID, source.OccurrenceKey)
	case SourceStudyMeeting:
		query = `SELECT ` + spaceReturning + ` FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND source_kind = 'study_meeting' AND source_study_meeting_id = $2`
		arguments = append(arguments, *source.StudyMeetingID)
	default:
		return spaceRow{}, false, ErrInvalidSpaceRequest
	}
	row, err := scanSpace(transaction.QueryRow(ctx, query, arguments...))
	if errors.Is(err, pgx.ErrNoRows) {
		return spaceRow{}, false, nil
	}
	return row, err == nil, err
}

type roomRow struct {
	ID               uuid.UUID
	AttemptNumber    int
	Status           RoomInstanceStatus
	Version          int64
	ProviderRoomName string
	ProviderRoomSID  sql.NullString
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func loadActiveRoom(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	lock bool,
) (roomRow, error) {
	query := `SELECT id, attempt_number, status, version, provider_room_name, provider_room_sid,
       created_at, updated_at
FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2
  AND status IN ('provisioning', 'active', 'closing')`
	if lock {
		query += " FOR UPDATE"
	}
	var room roomRow
	err := transaction.QueryRow(ctx, query, tenantID, spaceID).Scan(
		&room.ID, &room.AttemptNumber, &room.Status, &room.Version, &room.ProviderRoomName,
		&room.ProviderRoomSID, &room.CreatedAt, &room.UpdatedAt,
	)
	room.CreatedAt, room.UpdatedAt = room.CreatedAt.UTC(), room.UpdatedAt.UTC()
	return room, err
}

func loadRoomForTermination(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
) (roomRow, error) {
	var room roomRow
	err := transaction.QueryRow(
		ctx,
		`SELECT id, attempt_number, status, version, provider_room_name, provider_room_sid,
       created_at, updated_at
FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2
  AND status IN ('provisioning', 'active', 'closing', 'failed')
ORDER BY attempt_number DESC
LIMIT 1
FOR UPDATE`,
		tenantID,
		spaceID,
	).Scan(
		&room.ID,
		&room.AttemptNumber,
		&room.Status,
		&room.Version,
		&room.ProviderRoomName,
		&room.ProviderRoomSID,
		&room.CreatedAt,
		&room.UpdatedAt,
	)
	room.CreatedAt, room.UpdatedAt = room.CreatedAt.UTC(), room.UpdatedAt.UTC()
	return room, err
}

func loadLatestRoom(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	lock bool,
) (roomRow, error) {
	query := `SELECT id, attempt_number, status, version, provider_room_name, provider_room_sid,
       created_at, updated_at
FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2
ORDER BY attempt_number DESC
LIMIT 1`
	if lock {
		query += " FOR UPDATE"
	}
	var room roomRow
	err := transaction.QueryRow(ctx, query, tenantID, spaceID).Scan(
		&room.ID, &room.AttemptNumber, &room.Status, &room.Version,
		&room.ProviderRoomName, &room.ProviderRoomSID, &room.CreatedAt, &room.UpdatedAt,
	)
	room.CreatedAt, room.UpdatedAt = room.CreatedAt.UTC(), room.UpdatedAt.UTC()
	return room, err
}

func (room roomRow) project() RoomInstance {
	return RoomInstance{
		ID: room.ID, Status: room.Status, Version: room.Version,
		CreatedAt: room.CreatedAt, UpdatedAt: room.UpdatedAt,
		ProviderRoomName: room.ProviderRoomName,
		ProviderRoomSID:  room.ProviderRoomSID.String,
	}
}

func terminateRoomIntent(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	room roomRow,
	now time.Time,
) error {
	if room.Status == RoomInstanceFailed {
		return nil
	}
	if room.Status == RoomInstanceProvisioning {
		commandTag, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.media_room_instances
SET status = 'failed', version = version + 1, updated_by = $3,
    failed_at = $4, failure_code = 'ended_before_activation', updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND status = 'provisioning'`,
			scope.TenantID, room.ID, scope.ActorID, now,
		)
		if err != nil {
			return fmt.Errorf("fail unactivated room instance: %w", err)
		}
		if commandTag.RowsAffected() != 1 {
			return ErrSpaceTransition
		}
		return nil
	}
	commandTag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_room_instances
SET status = 'ended', version = version + 1, updated_by = $3,
    closing_at = COALESCE(closing_at, $4), ended_at = $4, updated_at = $4
WHERE tenant_id = $1 AND id = $2 AND status IN ('active', 'closing')`,
		scope.TenantID, room.ID, scope.ActorID, now,
	)
	if err != nil {
		return fmt.Errorf("end active room instance: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return ErrSpaceTransition
	}
	return nil
}

func terminateRoomParticipants(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
	now time.Time,
) error {
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET status = 'left', version = version + 1, capacity_reserved = false,
    terminal_at = $4, reconnecting_at = NULL, updated_at = $4
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')`,
		tenantID,
		spaceID,
		roomInstanceID,
		now.UTC(),
	)
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_hand_states
SET is_raised = false
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND is_raised`,
		tenantID, spaceID, roomInstanceID,
	); err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return advanceMediaRosterProjection(
		ctx, transaction, tenantID, spaceID, roomInstanceID,
	)
}

func failRoomParticipants(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
	failureCode string,
	now time.Time,
) error {
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET status = 'failed', version = version + 1, capacity_reserved = false,
    terminal_at = $4, reconnecting_at = NULL, failure_code = $5, updated_at = $4
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')`,
		tenantID, spaceID, roomInstanceID, now.UTC(), failureCode,
	)
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_hand_states
SET is_raised = false
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND is_raised`,
		tenantID, spaceID, roomInstanceID,
	); err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return advanceMediaRosterProjection(
		ctx, transaction, tenantID, spaceID, roomInstanceID,
	)
}

func insertTransitionReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	command TransitionCommand,
	resultVersion int64,
	instanceID uuid.UUID,
) error {
	providerRequired := command.Operation == "end"
	status := ProviderEffectNone
	var providerUpdatedAt any
	if providerRequired {
		status = ProviderEffectPending
		providerUpdatedAt = command.OccurredAt
	}
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.media_space_mutation_receipts (
    tenant_id, idempotency_key, request_fingerprint, operation, space_id,
    result_space_version, result_room_instance_id, actor_user_id, created_at,
    provider_effect_required, provider_effect_status, provider_effect_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		scope.TenantID, command.IdempotencyKey, command.Fingerprint, command.Operation,
		command.SpaceID, resultVersion, nullableLifecycleUUID(instanceID),
		scope.ActorID, command.OccurredAt, providerRequired, status, providerUpdatedAt,
	)
	return err
}

type mediaEventDetails struct {
	ReasonCode   string
	SafetyAction bool
}

func appendMediaSpaceEvent(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	space spaceRow,
	eventType string,
	details mediaEventDetails,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'media_space', $2, $3,
    jsonb_strip_nulls(jsonb_build_object(
        'space_id', $2::uuid, 'actor_user_id', $4::uuid,
        'source_kind', $5::text, 'status', $6::text, 'version', $7::bigint,
        'reason_code', NULLIF($8::text, ''),
        'safety_action', CASE WHEN $9::boolean THEN true ELSE NULL END
    )),
    $10, $10
)`,
		scope.TenantID, space.ID, eventType, scope.ActorID,
		space.Source.Kind, space.Status, space.Version,
		details.ReasonCode, details.SafetyAction, now,
	); err != nil {
		return fmt.Errorf("append %s outbox event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		"source_kind": string(space.Source.Kind), "status": string(space.Status),
		"version": strconv.FormatInt(space.Version, 10),
	}
	if details.ReasonCode != "" {
		metadata["reason_code"] = details.ReasonCode
	}
	if details.SafetyAction {
		metadata["safety_action"] = "true"
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: scope.TenantID, ActorID: scope.ActorID, EventType: eventType,
		AggregateType: "media_space", AggregateID: space.ID,
		Metadata:   metadata,
		OccurredAt: now,
	}); err != nil {
		return fmt.Errorf("append %s audit event: %w", eventType, err)
	}
	return nil
}

func acquireLifecycleTransactionLock(
	ctx context.Context,
	transaction pgx.Tx,
	key string,
) error {
	_, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`,
		key,
	)
	return err
}

func hasOrganizationRole(roles []policy.OrganizationRole, expected policy.OrganizationRole) bool {
	for _, role := range roles {
		if role == expected {
			return true
		}
	}
	return false
}

func mapClassSourceError(err error, conceal bool) error {
	switch {
	case errors.Is(err, classroom.ErrSessionNotFound),
		errors.Is(err, classroom.ErrSeriesNotFound),
		errors.Is(err, classroom.ErrClassNotFound):
		return ErrSourceUnavailable
	case errors.Is(err, classroom.ErrClassAccessDenied),
		errors.Is(err, classroom.ErrSessionAccessDenied):
		if conceal {
			return ErrSpaceNotFound
		}
		return ErrSpaceAccessDenied
	case errors.Is(err, classroom.ErrInvalidClassTransition),
		errors.Is(err, classroom.ErrInvalidSessionTransition):
		return ErrSourceUnavailable
	default:
		return fmt.Errorf("resolve media class source: %w", err)
	}
}

func nullableLifecycleUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func nullableLifecycleUUIDPointer(value *uuid.UUID) any {
	if value == nil || *value == uuid.Nil {
		return nil
	}
	return *value
}

func nullableLifecycleString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (repository *PostgresLifecycleRepository) unavailable(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, ErrLifecycleUnavailable)
}

func rollbackLifecycle(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}
