package classroom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const participationOperationOrganizerTransfer = "organizer_transfer"

var ErrSessionOrganizerUnavailable = errors.New("class session organizer unavailable")

// TransferOrganizerInput is an explicit audited lifecycle command. An
// inactive organizer is never silently replaced by a worker or roster sync.
type TransferOrganizerInput struct {
	NewOrganizerUserID    uuid.UUID
	ExpectedSourceVersion int64
	IdempotencyKey        string
}

type OrganizerTransferParams struct {
	NewOrganizerUserID    uuid.UUID
	ExpectedSourceVersion int64
	IdempotencyKey        string
	Fingerprint           string
}

type OrganizerTransferResult struct {
	Audience        SessionAudience
	OrganizerUserID uuid.UUID
	SourceVersion   int64
	Replayed        bool
}

type ParticipationOrganizerRepository interface {
	TransferParticipationOrganizer(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		ParticipationSourceRef,
		OrganizerTransferParams,
		time.Time,
	) (OrganizerTransferResult, error)
}

type ParticipationOrganizerServiceAPI interface {
	TransferOrganizer(
		context.Context,
		AccessContext,
		uuid.UUID,
		ParticipationSourceRef,
		TransferOrganizerInput,
	) (OrganizerTransferResult, error)
}

func (input TransferOrganizerInput) normalized(
	source ParticipationSourceRef,
) (OrganizerTransferParams, error) {
	source, err := source.Normalized()
	if err != nil || source.Kind == ParticipationSourceOccurrence {
		return OrganizerTransferParams{}, fmt.Errorf(
			"%w: organizer transfer supports a session or series source",
			ErrInvalidParticipationInput,
		)
	}
	if input.NewOrganizerUserID == uuid.Nil || input.ExpectedSourceVersion < 1 {
		return OrganizerTransferParams{}, fmt.Errorf(
			"%w: organizer and expected source version are required",
			ErrInvalidParticipationInput,
		)
	}
	key, err := normalizeParticipationIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return OrganizerTransferParams{}, err
	}
	sourceFingerprint, err := source.Fingerprint()
	if err != nil {
		return OrganizerTransferParams{}, err
	}
	params := OrganizerTransferParams{
		NewOrganizerUserID:    input.NewOrganizerUserID,
		ExpectedSourceVersion: input.ExpectedSourceVersion,
		IdempotencyKey:        key,
	}
	params.Fingerprint, err = fingerprintParticipationInput(
		participationOperationOrganizerTransfer,
		struct {
			SourceFingerprint     string    `json:"source_fingerprint"`
			NewOrganizerUserID    uuid.UUID `json:"new_organizer_user_id"`
			ExpectedSourceVersion int64     `json:"expected_source_version"`
		}{
			SourceFingerprint:     sourceFingerprint,
			NewOrganizerUserID:    input.NewOrganizerUserID,
			ExpectedSourceVersion: input.ExpectedSourceVersion,
		},
	)
	if err != nil {
		return OrganizerTransferParams{}, err
	}
	return params, nil
}

func (service *SessionParticipationService) TransferOrganizer(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	source ParticipationSourceRef,
	input TransferOrganizerInput,
) (OrganizerTransferResult, error) {
	class, tenantContext, err := service.authorize(
		ctx,
		access,
		classID,
		policy.ActionSessionSchedule,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	if !canTransferParticipationOrganizer(access, class) {
		return OrganizerTransferResult{}, ErrSessionParticipationAccessDenied
	}
	params, err := input.normalized(source)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	repository, ok := service.repository.(ParticipationOrganizerRepository)
	if !ok {
		return OrganizerTransferResult{}, ErrSessionParticipationUnavailable
	}
	result, err := repository.TransferParticipationOrganizer(
		ctx,
		tenantContext,
		classID,
		source,
		params,
		service.clock().UTC(),
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	result.Audience = projectSessionAudience(result.Audience, class, access.ActorID)
	return result, nil
}

func (repository *PostgresRepository) TransferParticipationOrganizer(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	params OrganizerTransferParams,
	transferredAt time.Time,
) (OrganizerTransferResult, error) {
	source, err := source.Normalized()
	if err != nil || source.Kind == ParticipationSourceOccurrence ||
		tenantContext.Validate() != nil || classID == uuid.Nil {
		return OrganizerTransferResult{}, ErrSessionParticipationNotFound
	}
	if repository.calendarProtector == nil {
		return OrganizerTransferResult{}, ErrSessionParticipationUnavailable
	}
	normalized, err := (TransferOrganizerInput{
		NewOrganizerUserID:    params.NewOrganizerUserID,
		ExpectedSourceVersion: params.ExpectedSourceVersion,
		IdempotencyKey:        params.IdempotencyKey,
	}).normalized(source)
	if err != nil || normalized.Fingerprint != params.Fingerprint {
		return OrganizerTransferResult{}, ErrInvalidParticipationInput
	}
	fingerprint, err := decodeParticipationFingerprint(params.Fingerprint)
	if err != nil {
		return OrganizerTransferResult{}, err
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return OrganizerTransferResult{}, fmt.Errorf("begin organizer transfer: %w", err)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionSchedulingFeature(
		queryContext,
		transaction,
		tenantContext.TenantID,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	lockedClass, membership, err := repository.lockClassMutation(
		queryContext,
		transaction,
		tenantContext,
		classID,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext,
		membership,
		lockedClass.Class,
		policy.ActionSessionSchedule,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	if !canLockedActorTransferParticipationOrganizer(tenantContext, membership, lockedClass) {
		return OrganizerTransferResult{}, ErrSessionParticipationAccessDenied
	}
	if err := lockParticipationIdempotency(
		queryContext,
		transaction,
		tenantContext.TenantID,
		params.IdempotencyKey,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	replay, err := findTypedParticipationReplay(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationOrganizerTransfer,
		classID,
		source,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	locked, err := repository.lockTypedParticipationSource(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
		true,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	if replay {
		audience, readErr := repository.readTypedParticipationAudience(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			locked,
		)
		if readErr != nil {
			return OrganizerTransferResult{}, readErr
		}
		if err := transaction.Commit(queryContext); err != nil {
			return OrganizerTransferResult{}, fmt.Errorf("commit replayed organizer transfer: %w", err)
		}
		return OrganizerTransferResult{
			Audience: audience, OrganizerUserID: locked.OrganizerUserID,
			SourceVersion: locked.Version, Replayed: true,
		}, nil
	}
	if locked.Status != SessionStatusScheduled {
		return OrganizerTransferResult{}, ErrSessionParticipationUnavailable
	}
	if locked.Version != params.ExpectedSourceVersion {
		return OrganizerTransferResult{}, ErrSessionAudienceVersionConflict
	}
	if err := requireEligibleParticipationOrganizer(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		params.NewOrganizerUserID,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	if locked.OrganizerUserID == params.NewOrganizerUserID {
		if err := insertTypedParticipationReceipt(
			queryContext,
			transaction,
			tenantContext,
			params.IdempotencyKey,
			fingerprint,
			participationOperationOrganizerTransfer,
			classID,
			source,
			uuid.Nil,
			uuid.Nil,
			locked.Version,
		); err != nil {
			return OrganizerTransferResult{}, err
		}
		audience, readErr := repository.readTypedParticipationAudience(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			locked,
		)
		if readErr != nil {
			return OrganizerTransferResult{}, readErr
		}
		if err := transaction.Commit(queryContext); err != nil {
			return OrganizerTransferResult{}, fmt.Errorf("commit no-op organizer transfer: %w", err)
		}
		return OrganizerTransferResult{
			Audience: audience, OrganizerUserID: locked.OrganizerUserID,
			SourceVersion: locked.Version,
		}, nil
	}

	current, err := lockTypedParticipationAttendees(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	previousOrganizerRole, previousOrganizerSource, retainPreviousOrganizer, err :=
		resolveTransferredOrganizerAudienceRole(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			locked.OrganizerUserID,
		)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	desired := transferredOrganizerAudience(
		current,
		locked.OrganizerUserID,
		previousOrganizerRole,
		previousOrganizerSource,
		retainPreviousOrganizer,
		params.NewOrganizerUserID,
	)
	changedAt := transferredAt.UTC()
	if err := applyTypedAudienceReplacement(
		queryContext,
		transaction,
		tenantContext,
		classID,
		locked,
		AudienceReplacementParams{ResponseRequested: locked.ResponseRequested},
		current,
		desired,
		changedAt,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	if err := resetParticipationRSVPForOrganizerTransfer(
		queryContext,
		transaction,
		tenantContext,
		classID,
		source,
		locked.ResponseRequested,
		changedAt,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	if err := revokeParticipationCapabilities(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
		"organizer_transferred",
		changedAt,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	updatedVersion, updatedAudienceRevision, err := updateParticipationOrganizerSource(
		queryContext,
		transaction,
		tenantContext,
		classID,
		locked,
		params.NewOrganizerUserID,
		locked.Sequence+1,
		changedAt,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	locked.OrganizerUserID = params.NewOrganizerUserID
	locked.Version = updatedVersion
	locked.AudienceRevision = updatedAudienceRevision
	locked.Sequence++
	recipients, err := repository.readTypedInvitationSnapshotRecipients(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
		nil,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	revisionID := uuid.New()
	for index := range recipients {
		recipients[index].ID = uuid.New()
	}
	if err := repository.insertTypedInvitationSnapshot(
		queryContext,
		transaction,
		tenantContext,
		locked,
		updatedVersion,
		locked.AudienceRevision,
		locked.Sequence,
		locked.ResponseRequested,
		revisionID,
		recipients,
		changedAt,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	if err := insertTypedParticipationReceipt(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationOrganizerTransfer,
		classID,
		source,
		uuid.Nil,
		revisionID,
		updatedVersion,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	if err := insertTypedParticipationEvent(
		queryContext,
		transaction,
		tenantContext,
		classID,
		source,
		"class_session.organizer_transferred.v1",
		locked.AudienceRevision,
		locked.Sequence,
		len(recipients),
		"",
		changedAt,
	); err != nil {
		return OrganizerTransferResult{}, err
	}
	audience, err := repository.readTypedParticipationAudience(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		locked,
	)
	if err != nil {
		return OrganizerTransferResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return OrganizerTransferResult{}, fmt.Errorf("commit organizer transfer: %w", err)
	}
	return OrganizerTransferResult{
		Audience: audience, OrganizerUserID: params.NewOrganizerUserID,
		SourceVersion: updatedVersion,
	}, nil
}

func transferredOrganizerAudience(
	current []persistedSessionAttendee,
	previousOrganizerID uuid.UUID,
	previousOrganizerRole string,
	previousOrganizerSource string,
	retainPreviousOrganizer bool,
	newOrganizerID uuid.UUID,
) []resolvedAudienceMember {
	desired := make([]resolvedAudienceMember, 0, len(current)+1)
	newOrganizerPresent := false
	if retainPreviousOrganizer && len(current) >= maximumAudienceAttendees {
		retainPreviousOrganizer = false
	}
	for _, attendee := range current {
		if attendee.UserID == previousOrganizerID && !retainPreviousOrganizer {
			continue
		}
		member := resolvedAudienceMember{
			UserID:            attendee.UserID,
			ParticipationRole: attendee.ParticipationRole,
			BusinessRole:      attendee.BusinessRole,
			AudienceSource:    attendee.AudienceSource,
		}
		if attendee.UserID == previousOrganizerID && retainPreviousOrganizer {
			member.BusinessRole = previousOrganizerRole
			member.AudienceSource = previousOrganizerSource
		}
		if attendee.UserID == newOrganizerID {
			newOrganizerPresent = true
			member.BusinessRole = "organizer"
			member.AudienceSource = "manual"
		}
		desired = append(desired, member)
	}
	if !newOrganizerPresent {
		desired = append(desired, resolvedAudienceMember{
			UserID:            newOrganizerID,
			ParticipationRole: ParticipationRoleRequired,
			BusinessRole:      "organizer",
			AudienceSource:    "manual",
		})
	}
	return desired
}

func requireEligibleParticipationOrganizer(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	userID uuid.UUID,
) error {
	var eligible bool
	err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.memberships AS membership
    JOIN tutorhub.users AS app_user
      ON app_user.id = membership.user_id
     AND app_user.status = 'active'
    JOIN tutorhub.classes AS class
      ON class.tenant_id = membership.tenant_id
     AND class.id = $2
    LEFT JOIN tutorhub.class_enrollments AS enrollment
      ON enrollment.tenant_id = membership.tenant_id
     AND enrollment.class_id = class.id
     AND enrollment.user_id = membership.user_id
     AND enrollment.status = 'active'
    WHERE membership.tenant_id = $1
      AND membership.user_id = $3
      AND membership.status = 'active'
      AND (
          membership.role IN ('org_admin', 'teacher')
          OR class.owner_user_id = membership.user_id
          OR enrollment.class_role = 'co_teacher'
      )
)`,
		tenantID,
		classID,
		userID,
	).Scan(&eligible)
	if err != nil {
		return fmt.Errorf("resolve organizer eligibility: %w", err)
	}
	if !eligible {
		return ErrSessionOrganizerUnavailable
	}
	return nil
}

func canTransferParticipationOrganizer(access AccessContext, class Class) bool {
	if class.OwnerUserID == access.ActorID {
		return true
	}
	for _, role := range access.OrganizationRoles {
		if role == policy.OrganizationRoleAdmin {
			return true
		}
	}
	return false
}

func canLockedActorTransferParticipationOrganizer(
	tenantContext tenancy.Context,
	membership lockedClassMembership,
	class lockedClass,
) bool {
	if class.OwnerUserID == tenantContext.ActorID {
		return true
	}
	return membership.Role == string(policy.OrganizationRoleAdmin)
}

func resolveTransferredOrganizerAudienceRole(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	userID uuid.UUID,
) (string, string, bool, error) {
	var organizationRole string
	var classRole *string
	err := transaction.QueryRow(
		ctx,
		`SELECT membership.role, enrollment.class_role
FROM tutorhub.memberships AS membership
LEFT JOIN tutorhub.class_enrollments AS enrollment
  ON enrollment.tenant_id = membership.tenant_id
 AND enrollment.class_id = $2
 AND enrollment.user_id = membership.user_id
 AND enrollment.status = 'active'
WHERE membership.tenant_id = $1
  AND membership.user_id = $3
  AND membership.status = 'active'`,
		tenantID,
		classID,
		userID,
	).Scan(&organizationRole, &classRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("resolve previous organizer audience role: %w", err)
	}
	if classRole != nil && validAudienceBusinessRole(*classRole) {
		return *classRole, "roster", true, nil
	}
	switch organizationRole {
	case string(policy.OrganizationRoleAdmin), string(policy.OrganizationRoleTeacher):
		return string(policy.OrganizationRoleTeacher), "manual", true, nil
	case string(policy.OrganizationRoleStudent), string(policy.OrganizationRoleGuest):
		return string(policy.OrganizationRoleStudent), "manual", true, nil
	default:
		return "", "", false, nil
	}
}

func resetParticipationRSVPForOrganizerTransfer(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	responseRequested bool,
	changedAt time.Time,
) error {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	_, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.class_session_attendees
SET rsvp_state = 'needs_action',
    rsvp_source = 'none',
    responded_at = NULL,
    response_note = NULL,
    response_invitation_revision_id = NULL,
    response_sequence = NULL,
    response_closed_at = CASE WHEN $6 THEN NULL ELSE $8::timestamptz END,
    updated_by = $7,
    updated_at = $8,
    version = version + 1
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key IS NOT DISTINCT FROM $5::text
  AND status = 'active'`,
		tenantContext.TenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		responseRequested,
		tenantContext.ActorID,
		changedAt,
	)
	if err != nil {
		return mapParticipationPostgresError("reset RSVP after organizer transfer", err)
	}
	return nil
}

func revokeParticipationCapabilities(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	reason string,
	revokedAt time.Time,
) error {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	_, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.calendar_rsvp_capabilities AS capability
SET revoked_at = $7,
    revoked_reason = $6
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.class_id = recipient.class_id
 AND revision.id = recipient.invitation_revision_id
WHERE capability.tenant_id = $1
  AND capability.invitation_revision_id = recipient.invitation_revision_id
  AND capability.invitation_recipient_id = recipient.id
  AND revision.tenant_id = $1
  AND revision.class_id = $2
  AND revision.session_id IS NOT DISTINCT FROM $3::uuid
  AND revision.series_id IS NOT DISTINCT FROM $4::uuid
  AND revision.occurrence_key IS NOT DISTINCT FROM $5::text
  AND capability.revoked_at IS NULL`,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		reason,
		revokedAt,
	)
	if err != nil {
		return fmt.Errorf("revoke participation RSVP capabilities: %w", err)
	}
	return nil
}

func updateParticipationOrganizerSource(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source lockedTypedParticipationSource,
	newOrganizerID uuid.UUID,
	sequence int64,
	changedAt time.Time,
) (int64, int64, error) {
	table := "tutorhub.class_sessions"
	if source.Ref.Kind == ParticipationSourceSeries {
		table = "tutorhub.class_session_series"
	}
	query := fmt.Sprintf(
		`UPDATE %s
SET organizer_user_id = $4,
    sequence = $5,
    audience_revision = audience_revision + 1,
    version = version + 1,
    updated_by = $6,
    updated_at = $7
WHERE tenant_id = $1
  AND class_id = $2
  AND id = $3
  AND version = $8
RETURNING version, audience_revision`,
		table,
	)
	var version int64
	var audienceRevision int64
	err := transaction.QueryRow(
		ctx,
		query,
		tenantContext.TenantID,
		classID,
		source.ID,
		newOrganizerID,
		sequence,
		tenantContext.ActorID,
		changedAt,
		source.Version,
	).Scan(&version, &audienceRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrSessionAudienceVersionConflict
	}
	if err != nil {
		return 0, 0, mapParticipationPostgresError("update participation organizer", err)
	}
	return version, audienceRevision, nil
}
