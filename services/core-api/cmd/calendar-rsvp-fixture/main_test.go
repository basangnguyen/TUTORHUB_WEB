package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
)

var fixtureIDs = []string{
	"11111111-1111-4111-8111-111111111111",
	"22222222-2222-4222-8222-222222222222",
	"33333333-3333-4333-8333-333333333333",
	"44444444-4444-4444-8444-444444444444",
	"55555555-5555-4555-8555-555555555555",
}

func TestRunEmitsExactlyOneFragmentOnlyLink(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := 0
	issuer := func(
		_ context.Context,
		request capabilityIssueRequest,
	) (capabilityIssueResult, error) {
		called++
		if request.TenantID != uuid.MustParse(fixtureIDs[0]) ||
			request.ActorID != uuid.MustParse(fixtureIDs[1]) ||
			request.ClassID != uuid.MustParse(fixtureIDs[2]) ||
			request.InvitationRevisionID != uuid.MustParse(fixtureIDs[3]) ||
			request.InvitationRecipientID != uuid.MustParse(fixtureIDs[4]) {
			t.Fatalf("unexpected issuance request: %+v", request)
		}
		return capabilityIssueResult{
			ResolveToken: "v1.resolve-once",
			RespondToken: "v1.respond-once",
		}, nil
	}
	exitCode := run(
		context.Background(),
		validArguments(),
		&stdout,
		&stderr,
		stagingEnvironmentLookup,
		fixtureActions{issueCapabilities: issuer},
	)
	if exitCode != 0 {
		t.Fatalf("run failed: exit=%d stderr=%q", exitCode, stderr.String())
	}
	if called != 1 || stderr.Len() != 0 {
		t.Fatalf("unexpected call/output: called=%d stderr=%q", called, stderr.String())
	}
	output := stdout.String()
	if strings.Count(output, "\n") != 1 ||
		strings.Count(output, "v1.resolve-once") != 1 ||
		strings.Count(output, "v1.respond-once") != 1 ||
		!strings.HasPrefix(output, "https://tutorhub-web.pages.dev/calendar/respond#") ||
		strings.Contains(strings.SplitN(output, "#", 2)[0], "token") {
		t.Fatalf("unexpected one-time output: %q", output)
	}
}

func TestRunRefusesEveryNonStagingEnvironmentBeforeIssuance(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"", "development", "test", "production"} {
		environment := environment
		t.Run(environment, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			called := false
			exitCode := run(
				context.Background(),
				validArguments(),
				&stdout,
				&stderr,
				func(key string) (string, bool) {
					if key == "APP_ENV" {
						return environment, environment != ""
					}
					return "", false
				},
				fixtureActions{
					issueCapabilities: func(
						context.Context,
						capabilityIssueRequest,
					) (capabilityIssueResult, error) {
						called = true
						return capabilityIssueResult{}, nil
					},
				},
			)
			if exitCode == 0 || called || stdout.Len() != 0 ||
				!strings.Contains(stderr.String(), "restricted to APP_ENV=staging") {
				t.Fatalf(
					"unsafe environment result: exit=%d called=%t stdout=%q stderr=%q",
					exitCode,
					called,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestRunRequiresTemporaryDeploymentOptInBeforeIssuance(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := false
	exitCode := run(
		context.Background(),
		validArguments(),
		&stdout,
		&stderr,
		func(key string) (string, bool) {
			if key == "APP_ENV" {
				return stagingEnvironment, true
			}
			return "", false
		},
		fixtureActions{
			issueCapabilities: func(
				context.Context,
				capabilityIssueRequest,
			) (capabilityIssueResult, error) {
				called = true
				return capabilityIssueResult{}, nil
			},
		},
	)
	if exitCode == 0 || called || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), fixtureEnableKey) {
		t.Fatalf(
			"missing deployment opt-in did not fail closed: exit=%d called=%t stdout=%q stderr=%q",
			exitCode,
			called,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunRequiresExplicitConfirmationAndEverySourceID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "confirmation", args: validArguments()[:10]},
		{name: "tenant", args: replaceArgument(validArguments(), "--tenant-id", "")},
		{name: "actor", args: replaceArgument(validArguments(), "--actor-id", "")},
		{name: "class", args: replaceArgument(validArguments(), "--class-id", "")},
		{name: "revision", args: replaceArgument(validArguments(), "--invitation-revision-id", "")},
		{name: "recipient", args: replaceArgument(validArguments(), "--invitation-recipient-id", "")},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			called := false
			exitCode := run(
				context.Background(),
				test.args,
				&stdout,
				&stderr,
				stagingEnvironmentLookup,
				fixtureActions{
					issueCapabilities: func(
						context.Context,
						capabilityIssueRequest,
					) (capabilityIssueResult, error) {
						called = true
						return capabilityIssueResult{}, nil
					},
				},
			)
			if exitCode == 0 || called || stdout.Len() != 0 {
				t.Fatalf(
					"guard did not fail closed: exit=%d called=%t stdout=%q stderr=%q",
					exitCode,
					called,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestRunNeverEmitsPartiallyIssuedTokenOnFailure(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	partialToken := "v1.partially-issued-must-stay-private"
	exitCode := run(
		context.Background(),
		validArguments(),
		&stdout,
		&stderr,
		stagingEnvironmentLookup,
		fixtureActions{
			issueCapabilities: func(
				context.Context,
				capabilityIssueRequest,
			) (capabilityIssueResult, error) {
				return capabilityIssueResult{ResolveToken: partialToken}, errors.New("respond issue failed")
			},
		},
	)
	if exitCode == 0 || stdout.Len() != 0 || strings.Contains(stderr.String(), partialToken) {
		t.Fatalf(
			"partial token escaped: exit=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunRejectsUnsafePublicOriginBeforeIssuance(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := false
	exitCode := run(
		context.Background(),
		validArguments(),
		&stdout,
		&stderr,
		func(key string) (string, bool) {
			switch key {
			case "APP_ENV":
				return stagingEnvironment, true
			case fixtureEnableKey:
				return "true", true
			case "PUBLIC_WEB_ORIGIN":
				return "http://unsafe.example", true
			default:
				return "", false
			}
		},
		fixtureActions{
			issueCapabilities: func(
				context.Context,
				capabilityIssueRequest,
			) (capabilityIssueResult, error) {
				called = true
				return capabilityIssueResult{}, nil
			},
		},
	)
	if exitCode == 0 || called || stdout.Len() != 0 ||
		!strings.Contains(stderr.String(), "public web origin") {
		t.Fatalf(
			"unsafe origin was not rejected before issuance: exit=%d called=%t stdout=%q stderr=%q",
			exitCode,
			called,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestRunCancelsSessionThroughInjectedBusinessActionWithoutLoggingIDs(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := 0
	exitCode := run(
		context.Background(),
		cancelSessionArguments(),
		&stdout,
		&stderr,
		stagingEnvironmentLookup,
		fixtureActions{
			cancelSession: func(
				_ context.Context,
				request cancelSessionRequest,
			) error {
				called++
				if request.TenantID != uuid.MustParse(fixtureIDs[0]) ||
					request.ActorID != uuid.MustParse(fixtureIDs[1]) ||
					request.ClassID != uuid.MustParse(fixtureIDs[2]) ||
					request.SessionID != uuid.MustParse(fixtureIDs[3]) ||
					request.ExpectedVersion != 7 {
					t.Fatalf("unexpected cancellation request: %+v", request)
				}
				return nil
			},
			issueCapabilities: func(
				context.Context,
				capabilityIssueRequest,
			) (capabilityIssueResult, error) {
				t.Fatal("capability issuance must not run for cancellation")
				return capabilityIssueResult{}, nil
			},
		},
	)
	if exitCode != 0 || called != 1 || stderr.Len() != 0 ||
		stdout.String() != "staging session cancellation completed\n" {
		t.Fatalf(
			"unexpected cancellation result: exit=%d called=%d stdout=%q stderr=%q",
			exitCode,
			called,
			stdout.String(),
			stderr.String(),
		)
	}
	for _, identifier := range fixtureIDs {
		if strings.Contains(stdout.String(), identifier) {
			t.Fatalf("fixture identifier escaped to stdout: %q", stdout.String())
		}
	}
}

func TestRunTransfersOrganizerThroughInjectedBusinessActionWithoutLoggingIDs(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := 0
	exitCode := run(
		context.Background(),
		organizerTransferArguments(),
		&stdout,
		&stderr,
		stagingEnvironmentLookup,
		fixtureActions{
			transferOrganizer: func(
				_ context.Context,
				request organizerTransferRequest,
			) error {
				called++
				if request.TenantID != uuid.MustParse(fixtureIDs[0]) ||
					request.ActorID != uuid.MustParse(fixtureIDs[1]) ||
					request.ClassID != uuid.MustParse(fixtureIDs[2]) ||
					request.SessionID != uuid.MustParse(fixtureIDs[3]) ||
					request.NewOrganizerUserID != uuid.MustParse(fixtureIDs[4]) ||
					request.ExpectedSourceVersion != 9 ||
					request.IdempotencyKey != "p3-02c-transfer-0001" {
					t.Fatalf("unexpected organizer transfer request: %+v", request)
				}
				return nil
			},
		},
	)
	if exitCode != 0 || called != 1 || stderr.Len() != 0 ||
		stdout.String() != "staging organizer transfer completed\n" {
		t.Fatalf(
			"unexpected transfer result: exit=%d called=%d stdout=%q stderr=%q",
			exitCode,
			called,
			stdout.String(),
			stderr.String(),
		)
	}
	for _, identifier := range fixtureIDs {
		if strings.Contains(stdout.String(), identifier) {
			t.Fatalf("fixture identifier escaped to stdout: %q", stdout.String())
		}
	}
}

func TestRunLifecycleOperationsFailClosedBeforeAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "cancel missing session",
			args: replaceArgument(cancelSessionArguments(), "--session-id", ""),
		},
		{
			name: "cancel missing expected version",
			args: replaceArgument(cancelSessionArguments(), "--expected-version", "0"),
		},
		{
			name: "transfer missing organizer",
			args: replaceArgument(
				organizerTransferArguments(),
				"--new-organizer-user-id",
				"",
			),
		},
		{
			name: "transfer short idempotency key",
			args: replaceArgument(
				organizerTransferArguments(),
				"--idempotency-key",
				"too-short",
			),
		},
		{
			name: "unsupported operation",
			args: replaceArgument(
				cancelSessionArguments(),
				"--operation",
				"drop-database",
			),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			called := false
			exitCode := run(
				context.Background(),
				test.args,
				&stdout,
				&stderr,
				stagingEnvironmentLookup,
				fixtureActions{
					cancelSession: func(context.Context, cancelSessionRequest) error {
						called = true
						return nil
					},
					transferOrganizer: func(
						context.Context,
						organizerTransferRequest,
					) error {
						called = true
						return nil
					},
				},
			)
			if exitCode == 0 || called || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf(
					"lifecycle guard did not fail closed: exit=%d called=%t stdout=%q stderr=%q",
					exitCode,
					called,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestRunLifecycleFailureDoesNotLeakDependencyDetails(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	sensitiveDetail := "postgres://secret@host/" + fixtureIDs[3]
	exitCode := run(
		context.Background(),
		cancelSessionArguments(),
		&stdout,
		&stderr,
		stagingEnvironmentLookup,
		fixtureActions{
			cancelSession: func(context.Context, cancelSessionRequest) error {
				return errors.New(sensitiveDetail)
			},
		},
	)
	if exitCode == 0 || stdout.Len() != 0 ||
		strings.Contains(stderr.String(), sensitiveDetail) ||
		!strings.Contains(stderr.String(), "staging session cancellation failed") {
		t.Fatalf(
			"lifecycle failure leaked details: exit=%d stdout=%q stderr=%q",
			exitCode,
			stdout.String(),
			stderr.String(),
		)
	}
}

func TestLifecyclePostconditionsRequireExactScopeAndVersionAdvance(t *testing.T) {
	t.Parallel()
	cancelRequest, err := parseCancelSessionRequest(
		fixtureIDs[0],
		fixtureIDs[1],
		fixtureIDs[2],
		fixtureIDs[3],
		7,
	)
	if err != nil {
		t.Fatalf("parse cancellation request: %v", err)
	}
	cancelledAt := time.Now().UTC()
	cancelled := classroom.ClassSession{
		ID:          cancelRequest.SessionID,
		ClassID:     cancelRequest.ClassID,
		Status:      classroom.SessionStatusCancelled,
		Version:     8,
		CancelledAt: &cancelledAt,
	}
	if err := validateCancelledSession(cancelRequest, cancelled); err != nil {
		t.Fatalf("valid cancellation result rejected: %v", err)
	}
	cancelled.Version = cancelRequest.ExpectedVersion
	if err := validateCancelledSession(cancelRequest, cancelled); err == nil {
		t.Fatal("cancellation without a version advance was accepted")
	}

	transferRequest, err := parseOrganizerTransferRequest(
		fixtureIDs[0],
		fixtureIDs[1],
		fixtureIDs[2],
		fixtureIDs[3],
		fixtureIDs[4],
		9,
		"p3-02c-transfer-0001",
	)
	if err != nil {
		t.Fatalf("parse transfer request: %v", err)
	}
	transferred := classroom.OrganizerTransferResult{
		Audience: classroom.SessionAudience{
			Source: classroom.SessionParticipationSource(transferRequest.SessionID),
		},
		OrganizerUserID: transferRequest.NewOrganizerUserID,
		SourceVersion:   10,
	}
	if err := validateOrganizerTransfer(transferRequest, transferred); err != nil {
		t.Fatalf("valid organizer transfer result rejected: %v", err)
	}
	transferred.SourceVersion = transferRequest.ExpectedSourceVersion
	if err := validateOrganizerTransfer(transferRequest, transferred); err == nil {
		t.Fatal("organizer transfer without a version advance was accepted")
	}
}

func TestPublicRSVPLinkRejectsUnsafeOriginAndMissingTokens(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{
		"http://tutorhub.example",
		"https://user@tutorhub.example",
		"https://tutorhub.example/path",
		"https://tutorhub.example?",
	} {
		if base, err := publicRSVPBase(origin); err == nil || base != nil {
			t.Fatalf("unsafe origin accepted: %q", origin)
		}
	}
	base, err := publicRSVPBase("https://tutorhub.example")
	if err != nil {
		t.Fatalf("valid origin rejected: %v", err)
	}
	for _, candidate := range []struct {
		resolve string
		respond string
	}{
		{resolve: "", respond: "respond"},
		{resolve: " resolve", respond: "respond"},
		{resolve: "resolve", respond: ""},
		{resolve: "resolve", respond: strings.Repeat("x", maximumTokenLength+1)},
	} {
		if link, err := publicRSVPLink(base, candidate.resolve, candidate.respond); err == nil || link != "" {
			t.Fatalf("unsafe token input accepted: %+v link=%q", candidate, link)
		}
	}
}

func validArguments() []string {
	return []string{
		"--tenant-id", fixtureIDs[0],
		"--actor-id", fixtureIDs[1],
		"--class-id", fixtureIDs[2],
		"--invitation-revision-id", fixtureIDs[3],
		"--invitation-recipient-id", fixtureIDs[4],
		"--confirm", confirmationPhrase,
	}
}

func cancelSessionArguments() []string {
	return []string{
		"--operation", operationCancelSession,
		"--tenant-id", fixtureIDs[0],
		"--actor-id", fixtureIDs[1],
		"--class-id", fixtureIDs[2],
		"--session-id", fixtureIDs[3],
		"--expected-version", "7",
		"--confirm", confirmationPhrase,
	}
}

func organizerTransferArguments() []string {
	return []string{
		"--operation", operationTransferOrganizer,
		"--tenant-id", fixtureIDs[0],
		"--actor-id", fixtureIDs[1],
		"--class-id", fixtureIDs[2],
		"--session-id", fixtureIDs[3],
		"--new-organizer-user-id", fixtureIDs[4],
		"--expected-source-version", "9",
		"--idempotency-key", "p3-02c-transfer-0001",
		"--confirm", confirmationPhrase,
	}
}

func replaceArgument(arguments []string, name string, value string) []string {
	copyOfArguments := append([]string(nil), arguments...)
	for index := range copyOfArguments {
		if copyOfArguments[index] == name && index+1 < len(copyOfArguments) {
			copyOfArguments[index+1] = value
			break
		}
	}
	return copyOfArguments
}

func stagingEnvironmentLookup(key string) (string, bool) {
	switch key {
	case "APP_ENV":
		return stagingEnvironment, true
	case fixtureEnableKey:
		return "true", true
	case "PUBLIC_WEB_ORIGIN":
		return "https://tutorhub-web.pages.dev", true
	default:
		return "", false
	}
}
