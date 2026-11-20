package identity

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

type schoolRouteFixture struct{}

func (schoolRouteFixture) DiscoverSSORealm(_ context.Context, domain string) (SSORoute, error) {
	if domain != "school.example" {
		return SSORoute{}, nil
	}
	return SSORoute{Available: true, Domain: domain, Provider: "google"}, nil
}

func TestSchoolSSOOAuthHintsAndBoundFlow(t *testing.T) {
	s, repo, _, google, now := federationServiceFixture(t)
	s.federation.SchoolDomains = schoolRouteFixture{}
	ctx := context.Background()
	for _, hints := range []OAuthStartHints{
		{LoginHint: "bad"}, {LoginHint: "Student <a@school.example>"}, {LoginHint: "a@school.example\n"},
		{LoginHint: strings.Repeat("a", 242) + "@school.example"}, {LoginHint: "a@elsewhere.example", Domain: "school.example"},
		{Domain: "unknown.example"}, {Domain: "gmail.com"},
	} {
		if _, err := s.BeginOAuth(ctx, "google", "/app", "login", Principal{}, hints); !errors.Is(err, ErrInvalidAuthFlow) {
			t.Fatalf("invalid school hint accepted: %v", err)
		}
	}
	start, err := s.BeginOAuth(ctx, "google", "/app", "login", Principal{}, OAuthStartHints{LoginHint: "a@school.example", Domain: "school.example"})
	if err != nil {
		t.Fatal(err)
	}
	location, err := url.Parse(start.AuthorizationURL)
	if err != nil || location.Query().Get("hd") != "school.example" || location.Query().Get("login_hint") != "a@school.example" {
		t.Fatal("missing school account hints", err)
	}
	callback := federationCallback(google, start, now)
	if _, err = s.CompleteOAuth(ctx, "google", callback); err != nil || repo.login.SchoolDomain != "school.example" || PKCEChallenge(google.verifier) != google.challenge {
		t.Fatal("school realm or PKCE verifier lost in callback", err)
	}
	start, err = s.BeginOAuth(ctx, "google", "/app", "login", Principal{}, OAuthStartHints{LoginHint: "person@gmail.com"})
	if err != nil {
		t.Fatal(err)
	}
	location, _ = url.Parse(start.AuthorizationURL)
	if location.Query().Has("hd") || location.Query().Get("login_hint") != "person@gmail.com" {
		t.Fatal("invalid consumer-account hint")
	}
}

func TestSchoolSSOProviderEvidence(t *testing.T) {
	const tid = "19be0457-72a8-47a7-a871-9785475d02d9"
	for _, tc := range []struct {
		slug, issuer, org   string
		verified, qualifies bool
	}{
		{"google", GoogleIssuerURL, "school.example", true, true}, {"google", GoogleIssuerURL, "", true, false},
		{"google", GoogleIssuerURL, "school.example", false, false}, {"facebook", "issuer", "school.example", true, false},
		{"microsoft", microsoftIssuer(tid), tid, true, true}, {"microsoft", microsoftIssuer(tid), tid, false, false},
		{"microsoft", microsoftIssuer(microsoftPersonalTenant), microsoftPersonalTenant, true, false},
		{"microsoft", microsoftIssuer(tid), "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", true, false},
	} {
		proof := schoolProof(tc.slug, ProviderClaims{Email: "member@school.example", ProviderOrgID: tc.org, Issuer: tc.issuer, EmailVerified: tc.verified})
		if proof.matches(tc.slug, "school.example", []string{tc.org}) != tc.qualifies || proof.matches(tc.slug, "other.example", []string{tc.org}) ||
			proof.matches(tc.slug, "school.example", []string{"unrelated.example"}) {
			t.Fatalf("incorrect %s school proof for %s", tc.slug, tc.org)
		}
	}
}
