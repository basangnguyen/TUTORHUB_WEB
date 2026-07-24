package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
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
	Enrollment            classroom.EnrollmentServiceAPI
	Audit                 audit.ServiceAPI
	FeatureControls       featurecontrol.ServiceAPI
	Media                 media.ServiceAPI
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
		options.LiveKitWebhook,
	)
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
