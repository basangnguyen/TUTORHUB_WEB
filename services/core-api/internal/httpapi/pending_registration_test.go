package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type registrationHTTPFixture struct {
	fakeNativeAuth
	linked bool
	input  identity.RegistrationCompleteInput
}

func (service *registrationHTTPFixture) PreviewRegistration(_ context.Context, binding string) (identity.PendingRegistrationView, error) {
	service.bound = binding
	service.calls++
	return identity.PendingRegistrationView{Provider: "facebook", MaskedEmail: "s***@example.test", ExpiresInSeconds: 860}, service.err
}
func (service *registrationHTTPFixture) StartRegistration(_ context.Context, binding string, input identity.SignupStartInput) (identity.SignupChallengeResult, error) {
	service.bound = binding
	service.calls++
	service.startInput = input
	return identity.SignupChallengeResult{ChallengeID: nativeTestChallengeID, CooldownSeconds: 60, ExpiresInSeconds: 600}, service.err
}
func (service *registrationHTTPFixture) ResendRegistration(ctx context.Context, binding string, allow func(context.Context, string) (bool, error)) (identity.SignupChallengeResult, error) {
	allowed, err := allow(ctx, "stored@example.test")
	if err != nil {
		return identity.SignupChallengeResult{}, err
	}
	return service.StartRegistration(ctx, binding, identity.SignupStartInput{Email: "stored@example.test", DestinationAllowed: allowed})
}
func (service *registrationHTTPFixture) CompleteRegistration(_ context.Context, binding string, input identity.RegistrationCompleteInput) (identity.RegistrationResult, error) {
	service.bound = binding
	service.calls++
	service.input = input
	if service.linked {
		return identity.RegistrationResult{LinkBinding: nativeTestBinding, LinkExpiresAt: fixedTime.Add(15 * time.Minute), ReturnTo: "/account-link"}, service.err
	}
	return identity.RegistrationResult{Session: service.session(), ReturnTo: "/app/home"}, service.err
}
func (service *registrationHTTPFixture) CancelRegistration(_ context.Context, binding string) error {
	service.bound = binding
	service.calls++
	return service.err
}

func registrationHandlerForTest(service *registrationHTTPFixture, limiter *nativeTestLimiter, signupEnabled bool) http.Handler {
	cfg := nativeTestConfig()
	cfg.Authentication.SignupEnabled = signupEnabled
	cfg.Authentication.ProviderFacebookEnabled = true
	cfg.Authentication.FacebookClientID = "test-id"
	cfg.Authentication.FacebookClientSecret = "test-secret"
	return nativeTestHandler(cfg, service, limiter, nil)
}

func registrationRequestForTest(suffix, body string, validBinding bool) *http.Request {
	const prefix = "/api/v1/auth/pending-registration"
	r := nativeRequest(prefix+suffix, body, false)
	if suffix == "" {
		r.Method = http.MethodGet
	}
	if validBinding {
		r.AddCookie(&http.Cookie{Name: "tutorhub_registration_tx", Value: nativeTestBinding})
	}
	return r
}

func TestPendingRegistrationPublicContracts(t *testing.T) {
	service := &registrationHTTPFixture{}
	router := registrationHandlerForTest(service, &nativeTestLimiter{}, true)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, registrationRequestForTest("", "", true))
	var view identity.PendingRegistrationView
	decodeJSON(t, recorder, &view)
	if recorder.Code != 200 || view.Provider != "facebook" || view.MaskedEmail != "s***@example.test" || view.ExpiresInSeconds <= 0 || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid registration preview")
	}

	var expected string
	for _, email := range []string{"New@example.test", "Existing@example.test"} {
		for _, denied := range []InvitationRateLimitAction{"", AuthRateLimitOTPSendDestination} {
			service := &registrationHTTPFixture{}
			router := registrationHandlerForTest(service, &nativeTestLimiter{deny: denied}, false)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, registrationRequestForTest("/start", `{"email":"`+email+`"}`, true))
			if recorder.Code != 202 || service.startInput.Email != strings.ToLower(email) || service.startInput.DestinationAllowed != (denied == "") ||
				len(recorder.Result().Cookies()) != 0 || service.bound != nativeTestBinding {
				t.Fatal("neutral start or isolated binding failed")
			}
			if expected == "" {
				expected = recorder.Body.String()
			} else if recorder.Body.String() != expected {
				t.Fatal("start leaking destination state")
			}
		}
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, registrationRequestForTest("/resend", `{}`, true))
	if recorder.Code != 202 || recorder.Body.String() != expected || service.startInput.Email != "stored@example.test" {
		t.Fatal("resend did not use stored destination")
	}
	for _, linked := range []bool{false, true} {
		service := &registrationHTTPFixture{linked: linked}
		router := registrationHandlerForTest(service, &nativeTestLimiter{}, !linked)
		recorder := httptest.NewRecorder()
		body := `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456","terms_version":"v1","keep_signed_in":true}`
		router.ServeHTTP(recorder, registrationRequestForTest("/complete", body, true))
		if recorder.Code != 200 || service.input.SignupEnabled == linked || !service.input.KeepSignedIn || service.bound != nativeTestBinding {
			t.Fatal("invalid pending completion")
		}
		cookies := recorder.Result().Cookies()
		if findCookie(t, cookies, "tutorhub_registration_tx").MaxAge != -1 {
			t.Fatal("completion must expire registration binding")
		}
		if linked {
			if len(cookies) != 2 || findCookie(t, cookies, "tutorhub_link_tx").Value == "" || !strings.Contains(recorder.Body.String(), `"status":"link_pending"`) {
				t.Fatal("collision may only issue link intent")
			}
		} else {
			if len(cookies) != 3 || findCookie(t, cookies, "tutorhub_session").Value == "" || findCookie(t, cookies, "tutorhub_csrf").Value == "" {
				t.Fatal("missing authenticated session")
			}
		}
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, registrationRequestForTest("/cancel", `{}`, true))
	if recorder.Code != 204 || findCookie(t, recorder.Result().Cookies(), "tutorhub_registration_tx").MaxAge != -1 {
		t.Fatal("cancel failed")
	}
}

func TestPendingRegistrationRejectsInvalidBindingsOrRequests(t *testing.T) {
	for _, suffix := range []string{"", "/start", "/resend", "/complete", "/cancel"} {
		for _, scenario := range []string{"missing", "duplicate", "cross-origin", "disabled"} {
			if scenario == "cross-origin" && suffix == "" {
				continue
			}
			service := &registrationHTTPFixture{}
			router := registrationHandlerForTest(service, &nativeTestLimiter{}, true)
			r := registrationRequestForTest(suffix, `{}`, scenario != "missing")
			switch scenario {
			case "missing":
				r.AddCookie(&http.Cookie{Name: "tutorhub_link_tx", Value: nativeTestBinding})
			case "duplicate":
				r.AddCookie(&http.Cookie{Name: "tutorhub_registration_tx", Value: nativeTestBinding})
			case "cross-origin":
				r.Header.Set("Origin", "https://other.example")
			case "disabled":
				router = nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, r)
			if recorder.Code < 400 || service.calls != 0 {
				t.Fatalf("%s %s reached registration service", suffix, scenario)
			}
		}
	}
	for _, tc := range []struct {
		suffix, body string
		err          error
		want         int
	}{
		{"/complete", `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456","terms_version":"v"}`, identity.ErrNativeInvalidCode, 401},
		{"/complete", `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456","terms_version":"v"}`, identity.ErrFederationSignupDisabled, 403},
		{"/resend", `{}`, identity.ErrAuthChallengeThrottled, 429},
		{"/resend", `{"email":"other@example.test"}`, nil, 400},
		{"", "", identity.ErrAuthTransactionUnavailable, 404},
	} {
		service := &registrationHTTPFixture{}
		service.err = tc.err
		recorder := httptest.NewRecorder()
		registrationHandlerForTest(service, &nativeTestLimiter{}, true).ServeHTTP(recorder, registrationRequestForTest(tc.suffix, tc.body, true))
		if recorder.Code != tc.want || len(recorder.Result().Cookies()) != 0 {
			t.Fatalf("%s error=%v got %d", tc.suffix, tc.err, recorder.Code)
		}
	}
}
