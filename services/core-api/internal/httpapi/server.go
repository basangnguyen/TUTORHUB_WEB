package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
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
	Media                 media.ServiceAPI
	LiveKitWebhook        media.WebhookVerifier
	InvitationRateLimiter InvitationRateLimiter
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
	mux.Handle(tenantsCollectionPath, http.HandlerFunc(auth.tenantCollection))
	mux.Handle(tenantsResourcePathPrefix, http.HandlerFunc(auth.tenantResource))
	mux.Handle(
		membershipInvitationsAdminCollectionPattern,
		membershipInvitationResponseHeaders(
			true,
			http.HandlerFunc(invitations.adminCollection),
		),
	)
	mux.Handle(
		membershipInvitationsAdminRevokePattern,
		membershipInvitationResponseHeaders(
			true,
			http.HandlerFunc(invitations.adminRevoke),
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
			requireMethod(http.MethodPost, http.HandlerFunc(invitations.accept)),
		),
	)
	mux.Handle(
		"/api/v1/session/active-tenant",
		requireMethod(http.MethodPut, http.HandlerFunc(auth.switchActiveTenant)),
	)
	classes := newClassHandlers(logger, auth, options.Classroom)
	mediaHandlers := newMediaHandlers(
		logger,
		auth,
		options.Media,
		options.LiveKitWebhook,
	)
	mux.Handle(classesCollectionPath, http.HandlerFunc(classes.collection))
	mux.Handle(classesResourcePathPrefix, http.HandlerFunc(classes.detail))
	mux.Handle(classArchivePathPattern, http.HandlerFunc(classes.detail))
	mux.Handle(classRestorePathPattern, http.HandlerFunc(classes.detail))
	mux.Handle(classTransferPathPattern, http.HandlerFunc(classes.detail))
	mux.Handle(mediaTokenPathPattern, http.HandlerFunc(mediaHandlers.issueJoinCredential))
	mux.Handle(mediaEventsPathPattern, http.HandlerFunc(mediaHandlers.recordClientEvent))
	mux.Handle(liveKitWebhookPath, http.HandlerFunc(mediaHandlers.receiveWebhook))
	mux.Handle("/metrics", requireMethod(http.MethodGet, options.Metrics.Handler()))
	mux.Handle("/", notFoundHandler())

	return middlewareStack(logger, options.Metrics, options.Tracer, mux)
}

func middlewareStack(
	logger *slog.Logger,
	metrics observability.HTTPMetrics,
	tracer observability.Tracer,
	next http.Handler,
) http.Handler {
	handler := recoverMiddleware(logger, metrics, next)
	handler = requestLogMiddleware(logger, metrics, tracer, handler)
	handler = requestIDMiddleware(handler)

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
