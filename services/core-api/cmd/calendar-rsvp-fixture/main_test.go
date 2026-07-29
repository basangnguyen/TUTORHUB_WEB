package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
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
		issuer,
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
				func(context.Context, capabilityIssueRequest) (capabilityIssueResult, error) {
					called = true
					return capabilityIssueResult{}, nil
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
		func(context.Context, capabilityIssueRequest) (capabilityIssueResult, error) {
			called = true
			return capabilityIssueResult{}, nil
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
				func(context.Context, capabilityIssueRequest) (capabilityIssueResult, error) {
					called = true
					return capabilityIssueResult{}, nil
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
		func(context.Context, capabilityIssueRequest) (capabilityIssueResult, error) {
			return capabilityIssueResult{ResolveToken: partialToken}, errors.New("respond issue failed")
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
		func(context.Context, capabilityIssueRequest) (capabilityIssueResult, error) {
			called = true
			return capabilityIssueResult{}, nil
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
