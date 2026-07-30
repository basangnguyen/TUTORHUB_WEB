package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/database"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	stagingEnvironment                 = "staging"
	confirmationPhrase                 = "ISSUE-P3-02C-STAGING-CAPABILITIES"
	fixtureAppName                     = "tutorhub-p3-02c-rsvp-fixture"
	fixtureEnableKey                   = "P3_02C_STAGING_FIXTURE_ENABLED"
	maximumTokenLength                 = 512
	minimumFixtureIdempotencyKeyLength = 16
	maximumFixtureIdempotencyKeyLength = 128

	operationIssueCapabilities = "issue-capabilities"
	operationCancelSession     = "cancel-session"
	operationTransferOrganizer = "transfer-organizer"
)

type environmentLookup func(string) (string, bool)

type capabilityIssueRequest struct {
	TenantID              uuid.UUID
	ActorID               uuid.UUID
	ClassID               uuid.UUID
	InvitationRevisionID  uuid.UUID
	InvitationRecipientID uuid.UUID
}

type capabilityIssueResult struct {
	ResolveToken string
	RespondToken string
}

type capabilityIssuer func(
	context.Context,
	capabilityIssueRequest,
) (capabilityIssueResult, error)

type fixtureScope struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	ClassID  uuid.UUID
}

type cancelSessionRequest struct {
	fixtureScope
	SessionID       uuid.UUID
	ExpectedVersion int64
}

type organizerTransferRequest struct {
	fixtureScope
	SessionID             uuid.UUID
	NewOrganizerUserID    uuid.UUID
	ExpectedSourceVersion int64
	IdempotencyKey        string
}

type sessionCanceller func(context.Context, cancelSessionRequest) error

type organizerTransferrer func(context.Context, organizerTransferRequest) error

type fixtureActions struct {
	issueCapabilities capabilityIssuer
	cancelSession     sessionCanceller
	transferOrganizer organizerTransferrer
}

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	os.Exit(run(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		os.LookupEnv,
		fixtureActions{
			issueCapabilities: issueCapabilities,
			cancelSession:     cancelStagingSession,
			transferOrganizer: transferStagingOrganizer,
		},
	))
}

func run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	lookup environmentLookup,
	actions fixtureActions,
) int {
	flags := flag.NewFlagSet("calendar-rsvp-fixture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	operation := flags.String(
		"operation",
		operationIssueCapabilities,
		"staging fixture operation",
	)
	tenantID := flags.String("tenant-id", "", "staging tenant UUID")
	actorID := flags.String("actor-id", "", "authorized staging actor UUID")
	classID := flags.String("class-id", "", "staging class UUID")
	sessionID := flags.String("session-id", "", "staging one-time session UUID")
	expectedVersion := flags.Int64(
		"expected-version",
		0,
		"current session version for cancellation",
	)
	newOrganizerUserID := flags.String(
		"new-organizer-user-id",
		"",
		"eligible replacement organizer UUID",
	)
	expectedSourceVersion := flags.Int64(
		"expected-source-version",
		0,
		"current participation source version",
	)
	idempotencyKey := flags.String(
		"idempotency-key",
		"",
		"single-use lifecycle idempotency key",
	)
	revisionID := flags.String(
		"invitation-revision-id",
		"",
		"immutable invitation revision UUID",
	)
	recipientID := flags.String(
		"invitation-recipient-id",
		"",
		"external invitation recipient UUID",
	)
	confirmation := flags.String(
		"confirm",
		"",
		"exact staging acceptance confirmation phrase",
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "positional arguments are not supported")
		return 2
	}

	environment := ""
	if lookup != nil {
		environment, _ = lookup("APP_ENV")
	}
	environment = strings.ToLower(strings.TrimSpace(environment))
	if environment != stagingEnvironment {
		fmt.Fprintln(stderr, "calendar RSVP fixture is restricted to APP_ENV=staging")
		return 1
	}
	fixtureEnabled := ""
	if lookup != nil {
		fixtureEnabled, _ = lookup(fixtureEnableKey)
	}
	if !strings.EqualFold(strings.TrimSpace(fixtureEnabled), "true") {
		fmt.Fprintf(stderr, "calendar RSVP fixture requires temporary %s=true\n", fixtureEnableKey)
		return 1
	}
	if *confirmation != confirmationPhrase {
		fmt.Fprintf(
			stderr,
			"refusing fixture operation without --confirm %s\n",
			confirmationPhrase,
		)
		return 2
	}

	normalizedOperation := strings.ToLower(strings.TrimSpace(*operation))
	switch normalizedOperation {
	case operationIssueCapabilities:
		return runCapabilityIssuance(
			ctx,
			stdout,
			stderr,
			lookup,
			actions.issueCapabilities,
			*tenantID,
			*actorID,
			*classID,
			*revisionID,
			*recipientID,
		)
	case operationCancelSession:
		request, err := parseCancelSessionRequest(
			*tenantID,
			*actorID,
			*classID,
			*sessionID,
			*expectedVersion,
		)
		if err != nil {
			fmt.Fprintln(
				stderr,
				"tenant, actor, class, session, and a positive expected version are required",
			)
			return 2
		}
		if actions.cancelSession == nil {
			fmt.Fprintln(stderr, "session cancellation fixture is unavailable")
			return 1
		}
		if err := actions.cancelSession(ctx, request); err != nil {
			fmt.Fprintln(stderr, "staging session cancellation failed")
			return 1
		}
		fmt.Fprintln(stdout, "staging session cancellation completed")
		return 0
	case operationTransferOrganizer:
		request, err := parseOrganizerTransferRequest(
			*tenantID,
			*actorID,
			*classID,
			*sessionID,
			*newOrganizerUserID,
			*expectedSourceVersion,
			*idempotencyKey,
		)
		if err != nil {
			fmt.Fprintln(
				stderr,
				"tenant, actor, class, session, organizer, source version, and idempotency key are required",
			)
			return 2
		}
		if actions.transferOrganizer == nil {
			fmt.Fprintln(stderr, "organizer transfer fixture is unavailable")
			return 1
		}
		if err := actions.transferOrganizer(ctx, request); err != nil {
			fmt.Fprintln(stderr, "staging organizer transfer failed")
			return 1
		}
		fmt.Fprintln(stdout, "staging organizer transfer completed")
		return 0
	default:
		fmt.Fprintln(stderr, "unsupported staging fixture operation")
		return 2
	}
}

func runCapabilityIssuance(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	lookup environmentLookup,
	issuer capabilityIssuer,
	tenantID string,
	actorID string,
	classID string,
	revisionID string,
	recipientID string,
) int {
	request, err := parseIssueRequest(
		tenantID,
		actorID,
		classID,
		revisionID,
		recipientID,
	)
	if err != nil {
		fmt.Fprintln(stderr, "all tenant/source identifiers must be valid non-zero UUIDs")
		return 2
	}
	if issuer == nil {
		fmt.Fprintln(stderr, "capability issuer is unavailable")
		return 1
	}
	webOrigin := ""
	if lookup != nil {
		webOrigin, _ = lookup("PUBLIC_WEB_ORIGIN")
	}
	responseBase, err := publicRSVPBase(webOrigin)
	if err != nil {
		fmt.Fprintln(stderr, "configured public web origin is unavailable for staging RSVP")
		return 1
	}

	result, err := issuer(ctx, request)
	if err != nil {
		// Do not print dependency errors: driver/configuration errors can contain
		// deployment details, and a partially issued raw token must never escape.
		fmt.Fprintln(stderr, "capability issuance failed; no response link was emitted")
		return 1
	}
	responseLink, err := publicRSVPLink(
		responseBase,
		result.ResolveToken,
		result.RespondToken,
	)
	if err != nil {
		fmt.Fprintln(stderr, "capability issuance did not produce a safe response link")
		return 1
	}

	// This is the only successful stdout write. The operator must copy the link
	// directly to the controlled recipient; it is intentionally unrecoverable.
	fmt.Fprintln(stdout, responseLink)
	return 0
}

func parseIssueRequest(
	tenantID string,
	actorID string,
	classID string,
	revisionID string,
	recipientID string,
) (capabilityIssueRequest, error) {
	values := [5]uuid.UUID{}
	raw := []string{tenantID, actorID, classID, revisionID, recipientID}
	for index := range raw {
		parsed, err := uuid.Parse(strings.TrimSpace(raw[index]))
		if err != nil || parsed == uuid.Nil {
			return capabilityIssueRequest{}, fmt.Errorf("invalid fixture identifier")
		}
		values[index] = parsed
	}
	return capabilityIssueRequest{
		TenantID:              values[0],
		ActorID:               values[1],
		ClassID:               values[2],
		InvitationRevisionID:  values[3],
		InvitationRecipientID: values[4],
	}, nil
}

func parseFixtureScope(
	tenantID string,
	actorID string,
	classID string,
) (fixtureScope, error) {
	values := [3]uuid.UUID{}
	raw := []string{tenantID, actorID, classID}
	for index := range raw {
		parsed, err := uuid.Parse(strings.TrimSpace(raw[index]))
		if err != nil || parsed == uuid.Nil {
			return fixtureScope{}, fmt.Errorf("invalid fixture scope")
		}
		values[index] = parsed
	}
	return fixtureScope{
		TenantID: values[0],
		ActorID:  values[1],
		ClassID:  values[2],
	}, nil
}

func parseCancelSessionRequest(
	tenantID string,
	actorID string,
	classID string,
	sessionID string,
	expectedVersion int64,
) (cancelSessionRequest, error) {
	scope, err := parseFixtureScope(tenantID, actorID, classID)
	if err != nil {
		return cancelSessionRequest{}, err
	}
	session, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil || session == uuid.Nil || expectedVersion < 1 {
		return cancelSessionRequest{}, fmt.Errorf("invalid cancellation target")
	}
	return cancelSessionRequest{
		fixtureScope:    scope,
		SessionID:       session,
		ExpectedVersion: expectedVersion,
	}, nil
}

func parseOrganizerTransferRequest(
	tenantID string,
	actorID string,
	classID string,
	sessionID string,
	newOrganizerUserID string,
	expectedSourceVersion int64,
	idempotencyKey string,
) (organizerTransferRequest, error) {
	scope, err := parseFixtureScope(tenantID, actorID, classID)
	if err != nil {
		return organizerTransferRequest{}, err
	}
	session, err := uuid.Parse(strings.TrimSpace(sessionID))
	if err != nil || session == uuid.Nil {
		return organizerTransferRequest{}, fmt.Errorf("invalid organizer transfer session")
	}
	organizer, err := uuid.Parse(strings.TrimSpace(newOrganizerUserID))
	if err != nil || organizer == uuid.Nil || expectedSourceVersion < 1 {
		return organizerTransferRequest{}, fmt.Errorf("invalid organizer transfer target")
	}
	key := strings.TrimSpace(idempotencyKey)
	if key != idempotencyKey ||
		len(key) < minimumFixtureIdempotencyKeyLength ||
		len(key) > maximumFixtureIdempotencyKeyLength {
		return organizerTransferRequest{}, fmt.Errorf("invalid organizer transfer idempotency key")
	}
	return organizerTransferRequest{
		fixtureScope:          scope,
		SessionID:             session,
		NewOrganizerUserID:    organizer,
		ExpectedSourceVersion: expectedSourceVersion,
		IdempotencyKey:        key,
	}, nil
}

func publicRSVPBase(rawOrigin string) (*url.URL, error) {
	origin, err := url.Parse(strings.TrimSpace(rawOrigin))
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil ||
		origin.ForceQuery || origin.RawQuery != "" || origin.Fragment != "" ||
		(origin.Path != "" && origin.Path != "/") {
		return nil, fmt.Errorf("invalid public RSVP origin")
	}
	origin.Path = "/calendar/respond"
	origin.RawPath = ""
	return origin, nil
}

func publicRSVPLink(
	base *url.URL,
	resolveToken string,
	respondToken string,
) (string, error) {
	if base == nil || !validCapabilityToken(resolveToken) ||
		!validCapabilityToken(respondToken) {
		return "", fmt.Errorf("invalid public RSVP link input")
	}
	origin := *base
	fragment := url.Values{}
	fragment.Set("resolve_token", resolveToken)
	fragment.Set("respond_token", respondToken)
	origin.Fragment = fragment.Encode()
	return origin.String(), nil
}

func validCapabilityToken(token string) bool {
	return token != "" && token == strings.TrimSpace(token) && len(token) <= maximumTokenLength
}

func issueCapabilities(
	ctx context.Context,
	request capabilityIssueRequest,
) (capabilityIssueResult, error) {
	var result capabilityIssueResult
	err := withStagingFixtureServices(
		ctx,
		fixtureScope{
			TenantID: request.TenantID,
			ActorID:  request.ActorID,
			ClassID:  request.ClassID,
		},
		func(services stagingFixtureServices) error {
			service, err := classroom.NewExternalRSVPService(
				services.repository,
				services.classAuthorizer,
				time.Now,
			)
			if err != nil {
				return fmt.Errorf("initialize RSVP authority")
			}
			issue := classroom.ExternalRSVPCapabilityIssue{
				InvitationRevisionID:  request.InvitationRevisionID,
				InvitationRecipientID: request.InvitationRecipientID,
				Purpose:               classroom.ExternalRSVPCapabilityResolve,
			}
			resolve, err := service.IssueCapability(
				ctx,
				services.access,
				request.ClassID,
				issue,
			)
			if err != nil {
				return err
			}
			issue.Purpose = classroom.ExternalRSVPCapabilityRespond
			respond, err := service.IssueCapability(
				ctx,
				services.access,
				request.ClassID,
				issue,
			)
			if err != nil {
				return err
			}
			result = capabilityIssueResult{
				ResolveToken: resolve.Raw,
				RespondToken: respond.Raw,
			}
			return nil
		},
	)
	if err != nil {
		return capabilityIssueResult{}, err
	}
	return result, nil
}

func cancelStagingSession(
	ctx context.Context,
	request cancelSessionRequest,
) error {
	return withStagingFixtureServices(
		ctx,
		request.fixtureScope,
		func(services stagingFixtureServices) error {
			service, err := classroom.NewSessionService(
				services.repository,
				services.classAuthorizer,
			)
			if err != nil {
				return fmt.Errorf("initialize session authority")
			}
			session, err := service.CancelSession(
				ctx,
				services.access,
				request.ClassID,
				request.SessionID,
				request.ExpectedVersion,
			)
			if err != nil {
				return err
			}
			return validateCancelledSession(request, session)
		},
	)
}

func transferStagingOrganizer(
	ctx context.Context,
	request organizerTransferRequest,
) error {
	return withStagingFixtureServices(
		ctx,
		request.fixtureScope,
		func(services stagingFixtureServices) error {
			service, err := classroom.NewSessionParticipationService(
				services.repository,
				services.classAuthorizer,
			)
			if err != nil {
				return fmt.Errorf("initialize participation authority")
			}
			result, err := service.TransferOrganizer(
				ctx,
				services.access,
				request.ClassID,
				classroom.SessionParticipationSource(request.SessionID),
				classroom.TransferOrganizerInput{
					NewOrganizerUserID:    request.NewOrganizerUserID,
					ExpectedSourceVersion: request.ExpectedSourceVersion,
					IdempotencyKey:        request.IdempotencyKey,
				},
			)
			if err != nil {
				return err
			}
			return validateOrganizerTransfer(request, result)
		},
	)
}

func validateCancelledSession(
	request cancelSessionRequest,
	session classroom.ClassSession,
) error {
	if session.ID != request.SessionID ||
		session.ClassID != request.ClassID ||
		session.Status != classroom.SessionStatusCancelled ||
		session.CancelledAt == nil ||
		session.Version <= request.ExpectedVersion ||
		session.Version-request.ExpectedVersion != 1 {
		return fmt.Errorf("session cancellation result is inconsistent")
	}
	return nil
}

func validateOrganizerTransfer(
	request organizerTransferRequest,
	result classroom.OrganizerTransferResult,
) error {
	if result.OrganizerUserID != request.NewOrganizerUserID ||
		result.SourceVersion <= request.ExpectedSourceVersion ||
		result.SourceVersion-request.ExpectedSourceVersion != 1 ||
		result.Audience.Source.Kind != classroom.ParticipationSourceSession ||
		result.Audience.Source.SessionID != request.SessionID {
		return fmt.Errorf("organizer transfer result is inconsistent")
	}
	return nil
}

type stagingFixtureServices struct {
	repository      *classroom.PostgresRepository
	classAuthorizer *classroom.Service
	access          classroom.AccessContext
}

func withStagingFixtureServices(
	ctx context.Context,
	scope fixtureScope,
	action func(stagingFixtureServices) error,
) error {
	if scope.TenantID == uuid.Nil || scope.ActorID == uuid.Nil ||
		scope.ClassID == uuid.Nil || action == nil {
		return fmt.Errorf("invalid staging fixture scope")
	}
	cfg, err := config.Load()
	if err != nil || cfg.Environment != stagingEnvironment || cfg.Database.PoolURL == "" ||
		!cfg.CalendarProtectedData.Enabled {
		return fmt.Errorf("staging fixture runtime is not configured")
	}

	pool, err := database.OpenNamed(ctx, cfg.Database, fixtureAppName)
	if err != nil {
		return fmt.Errorf("open fixture database")
	}
	defer pool.Close()

	protector, err := protecteddata.New(protecteddata.Config{
		Key:        cfg.CalendarProtectedData.Key,
		KeyVersion: cfg.CalendarProtectedData.KeyVersion,
	})
	if err != nil {
		return fmt.Errorf("initialize calendar protection")
	}
	authorizer := policy.NewEngine()
	forcedOff := map[featurecontrol.FeatureKey]bool{}
	if cfg.FeatureControls.DisableClassSessionScheduling {
		forcedOff[featurecontrol.FeatureClassSessionScheduling] = true
	}
	catalog, err := featurecontrol.NewCatalog(featurecontrol.Guardrails{
		ForcedOffFeatures: forcedOff,
	})
	if err != nil {
		return fmt.Errorf("initialize feature controls")
	}
	controls, err := featurecontrol.NewPostgresRepository(
		pool,
		cfg.Database.QueryTimeout,
		authorizer,
		catalog,
	)
	if err != nil {
		return fmt.Errorf("initialize feature repository")
	}
	repository := classroom.NewPostgresRepository(
		pool,
		cfg.Database.QueryTimeout,
		authorizer,
		controls,
	).WithCalendarProtectedData(protector)
	classAuthorizer, err := classroom.NewService(repository, authorizer)
	if err != nil {
		return fmt.Errorf("initialize class authority")
	}
	roleContext, cancelRoleLookup := context.WithTimeout(ctx, cfg.Database.QueryTimeout)
	defer cancelRoleLookup()
	var organizationRole policy.OrganizationRole
	if err := pool.QueryRow(
		roleContext,
		`SELECT membership.role
FROM tutorhub.memberships AS membership
JOIN tutorhub.tenants AS tenant ON tenant.id = membership.tenant_id
WHERE membership.tenant_id = $1
  AND membership.user_id = $2
  AND membership.status = 'active'
  AND tenant.status = 'active'`,
		scope.TenantID,
		scope.ActorID,
	).Scan(&organizationRole); err != nil {
		return fmt.Errorf("resolve active staging actor")
	}
	access := classroom.AccessContext{
		TenantID:          scope.TenantID,
		ActorID:           scope.ActorID,
		MembershipActive:  true,
		OrganizationRoles: []policy.OrganizationRole{organizationRole},
	}
	return action(stagingFixtureServices{
		repository:      repository,
		classAuthorizer: classAuthorizer,
		access:          access,
	})
}
