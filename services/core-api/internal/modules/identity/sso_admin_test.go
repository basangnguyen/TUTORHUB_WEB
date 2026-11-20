package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type ssoResolverFunc func(context.Context, string) ([]string, error)

func (f ssoResolverFunc) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return f(ctx, name)
}
func TestSchoolSSODomainValidation(t *testing.T) {
	for input, want := range map[string]string{"SCHool.Example": "school.example", "bücher.example": "xn--bcher-kva.example", "xn--bcher-kva.example": "xn--bcher-kva.example"} {
		got, err := NormalizeSSODomain(input)
		if err != nil || got != want {
			t.Errorf("normalize %q: %q %v", input, got, err)
		}
	}
	for _, s := range []string{"gmail.com", "GOOGLEMAIL.com", "outlook.com", "hotmail.com", "live.com", "msn.com", "yahoo.com", "icloud.com", "*.edu.vn", ".school.test", "school.test.", "uni_.test", "127.0.0.1", "localhost", "co.uk", "[::1]", "-host.test", "bad-.test", "a..test", "school.test/", "school.test\n", "xn--a.test", strings.Repeat("a", 64) + ".test"} {
		if _, err := NormalizeSSODomain(s); !errors.Is(err, ErrSSOInvalid) {
			t.Errorf("accepted invalid domain %q", s)
		}
	}
	guid := uuid.NewString()
	for _, tc := range []struct {
		kind   string
		values []string
		ok     bool
	}{
		{"entra", []string{strings.ToUpper(guid)}, true}, {"entra", []string{guid, guid}, false}, {"entra", []string{"school.test"}, false},
		{"entra", []string{microsoftPersonalTenant}, false},
		{"google_workspace", []string{"school.example"}, true}, {"google_workspace", []string{"icloud.com"}, false},
		{"google_workspace", nil, false}, {"google_workspace", []string{"School.test", "school.test"}, false}, {"other", []string{"school.test"}, false},
	} {
		_, err := normalizeSSOOrgIDs(tc.kind, tc.values)
		if (err == nil) != tc.ok {
			t.Errorf("unexpected org validation for %s: %v", tc.kind, err)
		}
	}
}
func TestSchoolSSOTXTBoundsAndDeadline(t *testing.T) {
	crypto, _ := NewCrypto(bytes.Repeat([]byte{1}, 32))
	s := NewSchoolSSOService(&NativeAuthService{crypto: crypto}, nil, true, true)
	tenant, id := uuid.New(), uuid.New()
	challenge, hash, err := s.newChallenge(tenant, id, "school.example")
	if err != nil {
		t.Fatal(err)
	}
	state := ssoVerification{id: id, domain: "school.example", hash: hash}
	for _, tc := range []struct {
		records []string
		want    error
	}{
		{[]string{"ignored", challenge.TXTValue}, nil},
		{[]string{challenge.TXTValue + "x"}, ErrSSODNSMismatch},
		{append([]string{challenge.TXTValue}, make([]string, 32)...), ErrSSODNSMismatch},
		{[]string{challenge.TXTValue, strings.Repeat("a", 513)}, ErrSSODNSMismatch},
	} {
		s.resolver = ssoResolverFunc(func(ctx context.Context, name string) ([]string, error) {
			end, ok := ctx.Deadline()
			if !ok || time.Until(end) > 5*time.Second || name != "_tutorhub-challenge.school.example." {
				t.Error("DNS lookup target or deadline")
			}
			return tc.records, nil
		})
		if got := s.matchTXT(context.Background(), tenant, state); !errors.Is(got, tc.want) {
			t.Fatal("unexpected verification outcome", got)
		}
	}
	release := make(chan struct{})
	defer close(release)
	s.resolver = ssoResolverFunc(func(context.Context, string) ([]string, error) { <-release; return nil, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.matchTXT(ctx, tenant, state); !errors.Is(err, ErrSSODNSUnavailable) {
		t.Fatal(err)
	}
}
