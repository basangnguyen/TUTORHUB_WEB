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
	stagingEnvironment = "staging"
	confirmationPhrase = "ISSUE-P3-02C-STAGING-CAPABILITIES"
	fixtureAppName     = "tutorhub-p3-02c-rsvp-fixture"
	fixtureEnableKey   = "P3_02C_STAGING_FIXTURE_ENABLED"
	maximumTokenLength = 512
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

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv, issueCapabilities))
}

func run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	lookup environmentLookup,
	issuer capabilityIssuer,
) int {
	flags := flag.NewFlagSet("calendar-rsvp-fixture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenantID := flags.String("tenant-id", "", "staging tenant UUID")
	actorID := flags.String("actor-id", "", "authorized staging actor UUID")
	classID := flags.String("class-id", "", "staging class UUID")
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
			"refusing issuance without --confirm %s\n",
			confirmationPhrase,
		)
		return 2
	}

	request, err := parseIssueRequest(
		*tenantID,
		*actorID,
		*classID,
		*revisionID,
		*recipientID,
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
	cfg, err := config.Load()
	if err != nil || cfg.Environment != stagingEnvironment || cfg.Database.PoolURL == "" ||
		!cfg.CalendarProtectedData.Enabled {
		return capabilityIssueResult{}, fmt.Errorf("staging fixture runtime is not configured")
	}

	pool, err := database.OpenNamed(ctx, cfg.Database, fixtureAppName)
	if err != nil {
		return capabilityIssueResult{}, fmt.Errorf("open fixture database")
	}
	defer pool.Close()

	protector, err := protecteddata.New(protecteddata.Config{
		Key:        cfg.CalendarProtectedData.Key,
		KeyVersion: cfg.CalendarProtectedData.KeyVersion,
	})
	if err != nil {
		return capabilityIssueResult{}, fmt.Errorf("initialize calendar protection")
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
		return capabilityIssueResult{}, fmt.Errorf("initialize feature controls")
	}
	controls, err := featurecontrol.NewPostgresRepository(
		pool,
		cfg.Database.QueryTimeout,
		authorizer,
		catalog,
	)
	if err != nil {
		return capabilityIssueResult{}, fmt.Errorf("initialize feature repository")
	}
	repository := classroom.NewPostgresRepository(
		pool,
		cfg.Database.QueryTimeout,
		authorizer,
		controls,
	).WithCalendarProtectedData(protector)
	classAuthorizer, err := classroom.NewService(repository, authorizer)
	if err != nil {
		return capabilityIssueResult{}, fmt.Errorf("initialize class authority")
	}
	service, err := classroom.NewExternalRSVPService(
		repository,
		classAuthorizer,
		time.Now,
	)
	if err != nil {
		return capabilityIssueResult{}, fmt.Errorf("initialize RSVP authority")
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
		request.TenantID,
		request.ActorID,
	).Scan(&organizationRole); err != nil {
		return capabilityIssueResult{}, fmt.Errorf("resolve active staging actor")
	}
	access := classroom.AccessContext{
		TenantID:          request.TenantID,
		ActorID:           request.ActorID,
		MembershipActive:  true,
		OrganizationRoles: []policy.OrganizationRole{organizationRole},
	}
	issue := classroom.ExternalRSVPCapabilityIssue{
		InvitationRevisionID:  request.InvitationRevisionID,
		InvitationRecipientID: request.InvitationRecipientID,
	}
	issue.Purpose = classroom.ExternalRSVPCapabilityResolve
	resolve, err := service.IssueCapability(ctx, access, request.ClassID, issue)
	if err != nil {
		return capabilityIssueResult{}, err
	}
	issue.Purpose = classroom.ExternalRSVPCapabilityRespond
	respond, err := service.IssueCapability(ctx, access, request.ClassID, issue)
	if err != nil {
		return capabilityIssueResult{}, err
	}
	return capabilityIssueResult{
		ResolveToken: resolve.Raw,
		RespondToken: respond.Raw,
	}, nil
}
