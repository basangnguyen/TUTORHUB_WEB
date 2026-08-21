package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/collaboration"
	"github.com/tutorhub-v2/core-api/internal/modules/content"
	"github.com/tutorhub-v2/core-api/internal/modules/conversation"
	"github.com/tutorhub-v2/core-api/internal/modules/discovery"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/modules/notification"
	"github.com/tutorhub-v2/core-api/internal/platform/observability"
)

type ReadinessCheck interface {
	Name() string
	Check(context.Context) error
}

type Options struct {
	Metrics               *observability.Metrics
	Tracer                observability.Tracer
	Readiness             []ReadinessCheck
	Clock                 func() time.Time
	Identity              identity.ServiceAPI
	Classroom             classroom.ServiceAPI
	ClassSessions         classroom.SessionServiceAPI
	ClassSessionSeries    classroom.SessionSeriesServiceAPI
	SessionParticipation  classroom.SessionParticipationServiceAPI
	ExternalRSVP          classroom.ExternalRSVPServiceAPI
	Calendar              calendar.ServiceAPI
	CalendarScheduling    calendar.SchedulingServiceAPI
	AvailabilityPolls     calendar.AvailabilityPollServiceAPI
	Enrollment            classroom.EnrollmentServiceAPI
	Audit                 audit.ServiceAPI
	FeatureControls       featurecontrol.ServiceAPI
	Media                 media.ServiceAPI
	MediaSpaces           media.LifecycleServiceAPI
	MediaJoinAttempts     media.JoinAttemptServiceAPI
	MediaLobby            media.LobbyServiceAPI
	MediaSignals          media.MediaSignalServiceAPI
	MediaModeration       media.ModerationServiceAPI
	MediaDiagnostics      media.DiagnosticServiceAPI
	MediaCredentials      media.InstanceCredentialServiceAPI
	MediaWebhooks         media.WebhookProcessor
	Notifications         notification.ServiceAPI
	Conversations         conversation.ServiceAPI
	Content               content.ServiceAPI
	Discovery             discovery.ServiceAPI
	Collaboration         collaboration.ServiceAPI
	LiveKitWebhook        media.WebhookVerifier
	InvitationRateLimiter InvitationRateLimiter
	RemoteAddressResolver RemoteAddressResolver
}

func NewHandler(cfg config.Config, logger *slog.Logger) http.Handler {
	return NewHandlerWithOptions(cfg, logger, Options{})
}

func NewHandlerWithOptions(cfg config.Config, logger *slog.Logger, options Options) http.Handler {
	logger = normalizeLogger(logger)
	if options.Metrics == nil {
		options.Metrics = observability.NewMetrics()
	}
	if options.Tracer == nil {
		options.Tracer = observability.NoopTracer{}
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.InvitationRateLimiter == nil {
		options.InvitationRateLimiter = newDefaultInvitationRateLimiter()
	}

	mux := http.NewServeMux()
	mux.Handle("/health", requireMethod(http.MethodGet, healthHandler(cfg, logger, options.Clock)))
	mux.Handle("/live", requireMethod(http.MethodGet, livenessHandler(logger, options.Clock)))
	mux.Handle(
		"/ready",
		requireMethod(
			http.MethodGet,
			readinessHandler(logger, options.Clock, options.Readiness),
		),
	)
	mux.Handle(
		"/api/v1/status",
		requireMethod(http.MethodGet, apiStatusHandler(cfg, logger, options.Clock)),
	)
	auth := newAuthHandlers(cfg, logger, options.Identity, options.Clock)
	auditMutation := func(resolve auditMutationResolver, next http.Handler) http.Handler {
		return auditMutationMiddleware(logger, options.Audit, resolve, next)
	}
	auditResolvedTenantMutation := func(
		resolve auditMutationResolver,
		next http.Handler,
	) http.Handler {
		return auditResolvedTenantMutationMiddleware(logger, options.Audit, resolve, next)
	}
	invitations := newMembershipInvitationHandlers(
		cfg,
		logger,
		auth,
		options.Identity,
		options.InvitationRateLimiter,
		options.Clock,
	)
	externalRSVP := newExternalCalendarRSVPHandlers(
		logger,
		options.ExternalRSVP,
		options.InvitationRateLimiter,
		options.Clock,
		cfg.WebOrigin,
	)
	mux.Handle(
		calendarInvitationResolvePath,
		externalCalendarRSVPResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(externalRSVP.resolve)),
		),
	)
	mux.Handle(
		calendarInvitationRespondPath,
		externalCalendarRSVPResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(externalRSVP.respond)),
		),
	)
	mux.Handle("/api/v1/auth/login", requireMethod(http.MethodGet, http.HandlerFunc(auth.login)))
	mux.Handle("/api/v1/auth/callback", requireMethod(http.MethodGet, http.HandlerFunc(auth.callback)))
	mux.Handle("/api/v1/auth/csrf", requireMethod(http.MethodGet, http.HandlerFunc(auth.csrf)))
	mux.Handle("/api/v1/auth/logout", requireMethod(http.MethodPost, http.HandlerFunc(auth.logout)))
	mux.Handle("/api/v1/me", requireMethod(http.MethodGet, http.HandlerFunc(auth.me)))
	mux.Handle("/api/v1/me/profile", http.HandlerFunc(auth.profile))
	mux.Handle(
		"/api/v1/me/identities",
		requireMethod(http.MethodGet, http.HandlerFunc(auth.identities)),
	)
	mux.Handle(
		"/api/v1/me/identities/link",
		requireMethod(http.MethodPost, http.HandlerFunc(auth.beginIdentityLink)),
	)
	mux.Handle(identityResourcePathPrefix, http.HandlerFunc(auth.identityResource))
	mux.Handle(
		tenantsCollectionPath,
		auditMutation(
			staticAuditMutation(http.MethodPost, audit.ActionTenantCreate, "tenant", nil),
			http.HandlerFunc(auth.tenantCollection),
		),
	)
	mux.Handle(
		tenantsResourcePathPrefix,
		auditMutation(tenantResourceAuditMutation, http.HandlerFunc(auth.tenantResource)),
	)
	featureControls := newFeatureControlHandlers(logger, auth, options.FeatureControls)
	notifications := newNotificationHandlers(logger, auth, options.Notifications)
	conversations := newConversationHandlers(logger, auth, options.Conversations)
	files := newContentHandlers(logger, auth, options.Content)
	discovery := newDiscoveryHandlers(logger, auth, options.Discovery)
	whiteboards := newWhiteboardHandlers(logger, auth, options.Collaboration)
	calendarHandlers := newCalendarHandlers(logger, auth, options.Calendar)
	calendarScheduling := newCalendarSchedulingHandlers(
		logger,
		auth,
		options.CalendarScheduling,
	)
	availabilityPolls := newAvailabilityPollHandlers(
		logger,
		auth,
		options.AvailabilityPolls,
		options.InvitationRateLimiter,
		options.Clock,
		cfg.WebOrigin,
	)
	mux.Handle(
		tenantCapabilitiesPattern,
		featureControlResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(featureControls.capabilities)),
		),
	)
	mux.Handle(
		tenantFeatureControlsPattern,
		featureControlResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPut,
					audit.ActionTenantFeatureControlUpdate,
					"tenant_feature_control",
					pathValueAuditResource("tenant_id"),
				),
				requireMethod(http.MethodPut, http.HandlerFunc(featureControls.update)),
			),
		),
	)
	mux.Handle(
		notificationsCollectionPath,
		notificationResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(notifications.list)),
		),
	)
	mux.Handle(
		notificationUnreadCountPath,
		notificationResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(notifications.unreadCount)),
		),
	)
	mux.Handle(
		notificationReadPattern,
		notificationResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(notifications.markRead)),
		),
	)
	mux.Handle(
		notificationReadAllPath,
		notificationResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(notifications.markAllRead)),
		),
	)
	mux.Handle(
		notificationPreferencePath,
		notificationResponseHeaders(http.HandlerFunc(notifications.preference)),
	)
	mux.Handle(
		homeRecentFilesPath,
		discoveryResponseHeaders(http.HandlerFunc(discovery.recentFiles)),
	)
	mux.Handle(
		resourceSearchPath,
		discoveryResponseHeaders(http.HandlerFunc(discovery.search)),
	)
	mux.Handle(
		whiteboardsCollectionPath,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.collection)),
	)
	mux.Handle(
		whiteboardResourcePattern,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.resource)),
	)
	mux.Handle(
		whiteboardOpenPattern,
		whiteboardResponseHeaders(whiteboards.transition("open")),
	)
	mux.Handle(
		whiteboardSuspendPattern,
		whiteboardResponseHeaders(whiteboards.transition("suspend")),
	)
	mux.Handle(
		whiteboardResumePattern,
		whiteboardResponseHeaders(whiteboards.transition("resume")),
	)
	mux.Handle(
		whiteboardClosePattern,
		whiteboardResponseHeaders(whiteboards.transition("close")),
	)
	mux.Handle(
		whiteboardCapabilitiesPattern,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.capabilities)),
	)
	mux.Handle(
		whiteboardGrantExchangePattern,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.grantExchange)),
	)
	mux.Handle(
		whiteboardSnapshotsPattern,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.snapshots)),
	)
	mux.Handle(
		whiteboardExportsPattern,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.export)),
	)
	mux.Handle(
		whiteboardRestorePattern,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.restore)),
	)
	mux.Handle(
		whiteboardImportValidatePath,
		whiteboardResponseHeaders(http.HandlerFunc(whiteboards.validateImport)),
	)
	mux.Handle(
		classFilesPattern,
		fileResponseHeaders(http.HandlerFunc(files.list)),
	)
	mux.Handle(
		fileUploadIntentsPath,
		fileResponseHeaders(http.HandlerFunc(files.createIntent)),
	)
	mux.Handle(
		fileResourcePattern,
		fileResponseHeaders(http.HandlerFunc(files.resource)),
	)
	mux.Handle(
		fileFinalizePattern,
		fileResponseHeaders(http.HandlerFunc(files.finalize)),
	)
	mux.Handle(
		fileUploadCapabilityPattern,
		fileResponseHeaders(http.HandlerFunc(files.uploadCapability)),
	)
	mux.Handle(
		fileDownloadCapabilityPattern,
		fileResponseHeaders(http.HandlerFunc(files.downloadCapability)),
	)
	mux.Handle(
		fileMultipartCollectionPattern,
		fileResponseHeaders(http.HandlerFunc(files.multipartCollection)),
	)
	mux.Handle(
		fileMultipartPartPattern,
		fileResponseHeaders(http.HandlerFunc(files.multipartPartCapability)),
	)
	mux.Handle(
		fileMultipartCompletePattern,
		fileResponseHeaders(http.HandlerFunc(files.multipartComplete)),
	)
	mux.Handle(
		fileMultipartAbortPattern,
		fileResponseHeaders(http.HandlerFunc(files.multipartAbort)),
	)
	mux.Handle(
		conversationsCollectionPath,
		conversationResponseHeaders(http.HandlerFunc(conversations.collection)),
	)
	mux.Handle(
		conversationDirectPath,
		conversationResponseHeaders(http.HandlerFunc(conversations.createDirect)),
	)
	mux.Handle(
		conversationResourcePattern,
		conversationResponseHeaders(http.HandlerFunc(conversations.resource)),
	)
	mux.Handle(
		conversationMessagesPattern,
		conversationResponseHeaders(http.HandlerFunc(conversations.messages)),
	)
	mux.Handle(
		conversationMessagePattern,
		conversationResponseHeaders(http.HandlerFunc(conversations.message)),
	)
	mux.Handle(
		conversationReadPattern,
		conversationResponseHeaders(http.HandlerFunc(conversations.markRead)),
	)
	mux.Handle(
		classConversationPattern,
		conversationResponseHeaders(http.HandlerFunc(conversations.createClass)),
	)
	mux.Handle(
		mediaSpaceConversationPattern,
		conversationResponseHeaders(http.HandlerFunc(conversations.createRoom)),
	)
	mux.Handle(
		calendarItemsPath,
		calendarResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(calendarHandlers.listItems)),
		),
	)
	mux.Handle(
		calendarPreferencePath,
		calendarResponseHeaders(http.HandlerFunc(calendarHandlers.preference)),
	)
	mux.Handle(
		calendarWorkingSchedulePath,
		calendarScheduling.workingScheduleHandler(),
	)
	mux.Handle(
		calendarAvailabilityQueryPath,
		calendarScheduling.availabilityQueryHandler(),
	)
	mux.Handle(
		availabilityPollCollectionPath,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.collection)),
	)
	mux.Handle(
		availabilityPollResourcePattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.resource)),
	)
	mux.Handle(
		availabilityPollOpenPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.open)),
	)
	mux.Handle(
		availabilityPollClosePattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.close)),
	)
	mux.Handle(
		availabilityPollReopenPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.reopen)),
	)
	mux.Handle(
		availabilityPollCancelPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.cancel)),
	)
	mux.Handle(
		availabilityPollResponsePattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.respond)),
	)
	mux.Handle(
		availabilityPollIndividualResponsesPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.individualResponses)),
	)
	mux.Handle(
		availabilityPollSummaryPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.summary)),
	)
	mux.Handle(
		availabilityPollFinalizePattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.finalize)),
	)
	mux.Handle(
		availabilityPollCapabilitiesPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.capabilities)),
	)
	mux.Handle(
		availabilityPollCapabilityPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.revokeCapability)),
	)
	mux.Handle(
		availabilityPollResolvePath,
		publicAvailabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.resolvePublic)),
	)
	mux.Handle(
		availabilityPollPublicRespondPath,
		publicAvailabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.respondPublic)),
	)
	mux.Handle(
		studyMeetingCollectionPath,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.studyMeetingCollection)),
	)
	mux.Handle(
		studyMeetingResourcePattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.studyMeetingResource)),
	)
	mux.Handle(
		studyMeetingCancelPattern,
		availabilityPollResponseHeaders(http.HandlerFunc(availabilityPolls.cancelStudyMeeting)),
	)
	mux.Handle(
		membershipInvitationsAdminCollectionPattern,
		membershipInvitationResponseHeaders(
			true,
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionMembershipInvitationCreate,
					"membership_invitation",
					nil,
				),
				http.HandlerFunc(invitations.adminCollection),
			),
		),
	)
	mux.Handle(
		membershipInvitationsAdminRevokePattern,
		membershipInvitationResponseHeaders(
			true,
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionMembershipInvitationRevoke,
					"membership_invitation",
					pathValueAuditResource("invitationId"),
				),
				http.HandlerFunc(invitations.adminRevoke),
			),
		),
	)
	mux.Handle(
		membershipInvitationPreviewPath,
		membershipInvitationResponseHeaders(
			false,
			requireMethod(http.MethodPost, http.HandlerFunc(invitations.preview)),
		),
	)
	mux.Handle(
		membershipInvitationAcceptPath,
		membershipInvitationResponseHeaders(
			true,
			auditResolvedTenantMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionMembershipInvitationAccept,
					"membership_invitation",
					nil,
				),
				requireMethod(http.MethodPost, http.HandlerFunc(invitations.accept)),
			),
		),
	)
	mux.Handle(
		"/api/v1/session/active-tenant",
		auditMutation(
			staticAuditMutation(http.MethodPut, audit.ActionTenantSwitch, "tenant", nil),
			requireMethod(http.MethodPut, http.HandlerFunc(auth.switchActiveTenant)),
		),
	)
	classes := newClassHandlers(logger, auth, options.Classroom)
	classSessions := newClassSessionHandlers(logger, auth, options.ClassSessions)
	classSessionSeries := newClassSessionSeriesHandlers(
		auth, options.ClassSessionSeries, classSessions,
	)
	sessionParticipation := newClassSessionParticipationHandlers(
		logger,
		auth,
		options.SessionParticipation,
	)
	classEnrollments := newClassEnrollmentHandlers(
		cfg,
		logger,
		auth,
		options.Enrollment,
		options.InvitationRateLimiter,
		options.Clock,
		options.Audit,
	)
	mediaHandlers := newMediaHandlers(
		logger,
		auth,
		options.Media,
		options.MediaJoinAttempts,
		options.MediaCredentials,
		options.LiveKitWebhook,
		options.MediaWebhooks,
	)
	mediaSpaces := newMediaSpaceHandlers(logger, auth, options.MediaSpaces)
	mediaLobby := newMediaLobbyHandlers(logger, auth, options.MediaLobby)
	mediaSignals := newMediaSignalHandlers(logger, auth, options.MediaSignals)
	mediaModeration := newMediaModerationHandlers(logger, auth, options.MediaModeration)
	mediaDiagnostics := newMediaDiagnosticHandlers(logger, auth, options.MediaDiagnostics, options.Audit)
	mux.Handle(
		classesCollectionPath,
		auditMutation(
			staticAuditMutation(http.MethodPost, audit.ActionClassCreate, "class", nil),
			http.HandlerFunc(classes.collection),
		),
	)
	mux.Handle(
		classesResourcePathPrefix,
		auditMutation(classResourceUpdateAuditMutation, http.HandlerFunc(classes.detail)),
	)
	mux.Handle(
		classSessionsCollectionPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost,
				audit.ActionClassSessionCreate,
				"class_session",
				pathValueAuditResource("class_id"),
			),
			http.HandlerFunc(classSessions.collection),
		),
	)
	mux.Handle(
		classSessionResourcePattern,
		auditMutation(
			classSessionResourceAuditMutation,
			http.HandlerFunc(classSessions.resource),
		),
	)
	mux.Handle(
		classSessionCancelPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost,
				audit.ActionClassSessionCancel,
				"class_session",
				pathValueAuditResource("session_id"),
			),
			http.HandlerFunc(classSessions.cancel),
		),
	)
	mux.Handle(
		classSessionSeriesCollectionPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost,
				audit.ActionClassSessionCreate,
				"class_session_series",
				pathValueAuditResource("class_id"),
			),
			http.HandlerFunc(classSessionSeries.collection),
		),
	)
	mux.Handle(
		classSessionSeriesResourcePattern,
		http.HandlerFunc(classSessionSeries.resource),
	)
	mux.Handle(
		classSessionSeriesPreviewPattern,
		requireMethod(http.MethodPost, http.HandlerFunc(classSessionSeries.preview)),
	)
	mux.Handle(
		classSessionSeriesMutationPattern,
		auditMutation(
			classSessionSeriesResourceAuditMutation,
			http.HandlerFunc(classSessionSeries.update),
		),
	)
	mux.Handle(
		classSessionSeriesCancelPattern,
		auditMutation(
			classSessionSeriesResourceAuditMutation,
			http.HandlerFunc(classSessionSeries.cancel),
		),
	)
	mux.Handle(
		classSessionAttendeesPattern,
		sessionParticipation.attendeesHandler(),
	)
	mux.Handle(
		classSessionResponsesPattern,
		sessionParticipation.responsesHandler(),
	)
	mux.Handle(
		classSessionSeriesAttendeesPattern,
		sessionParticipation.attendeesHandler(),
	)
	mux.Handle(
		classSessionSeriesResponsesPattern,
		sessionParticipation.responsesHandler(),
	)
	mux.Handle(
		classSessionOccurrenceAttendeesPattern,
		sessionParticipation.attendeesHandler(),
	)
	mux.Handle(
		classSessionOccurrenceResponsesPattern,
		sessionParticipation.responsesHandler(),
	)
	mux.Handle(
		classSessionOrganizerPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost,
				audit.ActionClassSessionUpdate,
				"class_session",
				pathValueAuditResource("session_id"),
			),
			sessionParticipation.organizerHandler(),
		),
	)
	mux.Handle(
		classSessionSeriesOrganizerPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost,
				audit.ActionClassSessionUpdate,
				"class_session_series",
				pathValueAuditResource("series_id"),
			),
			sessionParticipation.organizerHandler(),
		),
	)
	mux.Handle(
		classArchivePathPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost, audit.ActionClassArchive, "class", pathValueAuditResource("class_id"),
			),
			http.HandlerFunc(classes.detail),
		),
	)
	mux.Handle(
		classRestorePathPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost, audit.ActionClassRestore, "class", pathValueAuditResource("class_id"),
			),
			http.HandlerFunc(classes.detail),
		),
	)
	mux.Handle(
		classTransferPathPattern,
		auditMutation(
			staticAuditMutation(
				http.MethodPost,
				audit.ActionClassTransferOwnership,
				"class",
				pathValueAuditResource("class_id"),
			),
			http.HandlerFunc(classes.detail),
		),
	)
	mux.Handle(
		classEnrollmentsPattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassEnrollmentEnroll,
					"class",
					pathValueAuditResource("class_id"),
				),
				http.HandlerFunc(classEnrollments.directEnrollment),
			),
		),
	)
	mux.Handle(
		classRosterPattern,
		classEnrollmentResponseHeaders(
			http.HandlerFunc(classEnrollments.rosterCollection),
		),
	)
	mux.Handle(
		classRosterBulkPattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassRosterBulk,
					"class",
					pathValueAuditResource("class_id"),
				),
				http.HandlerFunc(classEnrollments.rosterBulk),
			),
		),
	)
	mux.Handle(
		classRosterUserPattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPatch,
					audit.ActionClassEnrollmentUpdateRole,
					"class",
					pathValueAuditResource("class_id"),
				),
				http.HandlerFunc(classEnrollments.rosterUser),
			),
		),
	)
	mux.Handle(
		classEnrollmentSuspendPattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassEnrollmentSuspend,
					"class",
					pathValueAuditResource("class_id"),
				),
				classEnrollments.enrollmentStateMutation("suspend"),
			),
		),
	)
	mux.Handle(
		classEnrollmentRemovePattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassEnrollmentRemove,
					"class",
					pathValueAuditResource("class_id"),
				),
				classEnrollments.enrollmentStateMutation("remove"),
			),
		),
	)
	mux.Handle(
		classInviteCodesPattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassInviteCodeCreate,
					"class",
					pathValueAuditResource("class_id"),
				),
				http.HandlerFunc(classEnrollments.inviteCodeCollection),
			),
		),
	)
	mux.Handle(
		classInviteCodeRevokePattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassInviteCodeRevoke,
					"class_invite_code",
					pathValueAuditResource("code_id"),
				),
				http.HandlerFunc(classEnrollments.revokeInviteCode),
			),
		),
	)
	mux.Handle(
		classInvitationJoinPath,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassEnrollmentJoin,
					"class_enrollment",
					nil,
				),
				http.HandlerFunc(classEnrollments.joinByInviteCode),
			),
		),
	)
	mux.Handle(
		classLeavePattern,
		classEnrollmentResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost,
					audit.ActionClassEnrollmentLeave,
					"class",
					pathValueAuditResource("class_id"),
				),
				http.HandlerFunc(classEnrollments.leaveClass),
			),
		),
	)
	audits := newAuditHandlers(logger, auth, options.Audit)
	mux.Handle(
		auditEventsPattern,
		auditResponseHeaders(http.HandlerFunc(audits.list)),
	)
	mux.Handle(mediaTokenPathPattern, http.HandlerFunc(mediaHandlers.issueJoinCredential))
	mux.Handle(mediaEventsPathPattern, http.HandlerFunc(mediaHandlers.recordClientEvent))
	mux.Handle(
		mediaDiagnosticsPathPattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaDiagnostics.record)),
		),
	)
	mux.Handle(
		mediaDiagnosticsExportPath,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaDiagnostics.export)),
		),
	)
	mux.Handle(
		mediaParticipantsPattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(mediaSignals.participants)),
		),
	)
	mux.Handle(
		mediaSignalsPattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaSignals.signal)),
		),
	)
	mux.Handle(
		mediaSpaceLockPattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaModeration.lock)),
		),
	)
	mux.Handle(
		mediaParticipantRolePattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaModeration.role)),
		),
	)
	mux.Handle(
		mediaParticipantMutePattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaModeration.mute)),
		),
	)
	mux.Handle(
		mediaParticipantRemovePattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaModeration.remove)),
		),
	)
	mux.Handle(
		mediaSpaceJoinAttemptPathPattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaHandlers.createJoinAttempt)),
		),
	)
	mux.Handle(
		mediaJoinAttemptResourcePattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(mediaLobby.joinAttempt)),
		),
	)
	mux.Handle(
		mediaJoinAttemptCancelPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaAdmissionCancel, "media_admission", nil,
				),
				requireMethod(http.MethodPost, http.HandlerFunc(mediaLobby.cancelJoinAttempt)),
			),
		),
	)
	mux.Handle(
		mediaAdmissionsCollectionPath,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(mediaLobby.admissions)),
		),
	)
	mux.Handle(
		mediaAdmissionAdmitPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaAdmissionAdmit, "media_admission",
					pathValueAuditResource("admission_id"),
				),
				requireMethod(http.MethodPost, mediaLobby.mutateAdmission("admit")),
			),
		),
	)
	mux.Handle(
		mediaAdmissionDenyPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaAdmissionDeny, "media_admission",
					pathValueAuditResource("admission_id"),
				),
				requireMethod(http.MethodPost, mediaLobby.mutateAdmission("deny")),
			),
		),
	)
	mux.Handle(
		mediaAdmissionRestorePattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaAdmissionRestore, "media_admission",
					pathValueAuditResource("admission_id"),
				),
				requireMethod(http.MethodPost, mediaLobby.mutateAdmission("restore")),
			),
		),
	)
	mux.Handle(
		mediaSpaceMembersPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceMemberInvite, "media_space",
					pathValueAuditResource("space_id"),
				),
				http.HandlerFunc(mediaLobby.members),
			),
		),
	)
	mux.Handle(
		mediaSpaceMemberRevokePattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceMemberRevoke, "media_space_member",
					pathValueAuditResource("user_id"),
				),
				requireMethod(http.MethodPost, mediaLobby.mutateMember("revoke")),
			),
		),
	)
	mux.Handle(
		mediaSpaceMemberRestorePattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceMemberRestore, "media_space_member",
					pathValueAuditResource("user_id"),
				),
				requireMethod(http.MethodPost, mediaLobby.mutateMember("restore")),
			),
		),
	)
	mux.Handle(
		mediaSpaceCredentialPathPattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodPost, http.HandlerFunc(mediaHandlers.issueInstanceCredential)),
		),
	)
	mux.Handle(
		mediaSpacesCollectionPath,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceCreate, "media_space", nil,
				),
				requireMethod(http.MethodPost, http.HandlerFunc(mediaSpaces.create)),
			),
		),
	)
	mux.Handle(
		mediaSpaceResolvePath,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(mediaSpaces.resolve)),
		),
	)
	mux.Handle(
		mediaSpaceResourcePattern,
		mediaSpaceResponseHeaders(
			requireMethod(http.MethodGet, http.HandlerFunc(mediaSpaces.get)),
		),
	)
	mux.Handle(
		mediaSpaceStartPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceStart, "media_space",
					pathValueAuditResource("space_id"),
				),
				requireMethod(http.MethodPost, http.HandlerFunc(mediaSpaces.start)),
			),
		),
	)
	mux.Handle(
		mediaSpaceEndPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceEnd, "media_space",
					pathValueAuditResource("space_id"),
				),
				requireMethod(http.MethodPost, http.HandlerFunc(mediaSpaces.end)),
			),
		),
	)
	mux.Handle(
		mediaSpaceCancelPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceCancel, "media_space",
					pathValueAuditResource("space_id"),
				),
				requireMethod(http.MethodPost, http.HandlerFunc(mediaSpaces.cancel)),
			),
		),
	)
	mux.Handle(
		mediaSpaceRecoverPattern,
		mediaSpaceResponseHeaders(
			auditMutation(
				staticAuditMutation(
					http.MethodPost, audit.ActionMediaSpaceRecover, "media_space",
					pathValueAuditResource("space_id"),
				),
				requireMethod(http.MethodPost, http.HandlerFunc(mediaSpaces.recover)),
			),
		),
	)
	mux.Handle(liveKitWebhookPath, http.HandlerFunc(mediaHandlers.receiveWebhook))
	mux.Handle("/metrics", requireMethod(http.MethodGet, options.Metrics.Handler()))
	mux.Handle("/", notFoundHandler())

	return middlewareStack(
		logger,
		options.Metrics,
		options.Tracer,
		mux,
		options.RemoteAddressResolver,
	)
}

func middlewareStack(
	logger *slog.Logger,
	metrics observability.HTTPMetrics,
	tracer observability.Tracer,
	next http.Handler,
	resolvers ...RemoteAddressResolver,
) http.Handler {
	handler := recoverMiddleware(logger, metrics, next)
	handler = requestLogMiddleware(logger, metrics, tracer, handler)
	handler = requestIDMiddleware(handler, resolvers...)

	return handler
}

func normalizeLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func requireMethod(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == method || (method == http.MethodGet && r.Method == http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Allow", method)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"The requested resource does not support this HTTP method.",
		)
	})
}

func notFoundHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(
			w,
			r,
			http.StatusNotFound,
			"Resource not found",
			"The requested resource does not exist.",
		)
	})
}
