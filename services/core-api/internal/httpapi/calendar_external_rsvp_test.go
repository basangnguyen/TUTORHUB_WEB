package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
)

const (
	externalRSVPResolveToken = "v1.resolve-capability-for-http-test"
	externalRSVPRespondToken = "v1.respond-capability-for-http-test"
	externalRSVPWebOrigin    = "https://tutorhub-web.pages.dev"
)

var externalRSVPTestTime = time.Date(2026, time.July, 29, 8, 30, 0, 0, time.UTC)

func TestExternalCalendarRSVPResolveUsesSecurityHeadersAndBodyCapability(t *testing.T) {
	t.Parallel()

	service := &externalCalendarRSVPServiceStub{
		resolveProjection: externalRSVPTestProjection(),
	}
	limiter := &externalCalendarRSVPLimiterStub{}
	handler := newExternalCalendarRSVPTestHandler(service, limiter, false)
	request := newExternalCalendarRSVPRequest(
		http.MethodPost,
		calendarInvitationResolvePath,
		`{"token":"`+externalRSVPResolveToken+`"}`,
		"",
	)
	request.RemoteAddr = "203.0.113.27:4123"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if service.resolveToken != externalRSVPResolveToken {
		t.Fatalf("service received unexpected resolve capability: %q", service.resolveToken)
	}
	if request.URL.RawQuery != "" || strings.Contains(request.RequestURI, externalRSVPResolveToken) {
		t.Fatalf("capability must not appear in the request URL: %q", request.RequestURI)
	}
	if strings.Contains(response.Body.String(), externalRSVPResolveToken) {
		t.Fatal("capability must not be reflected in the response")
	}
	if len(limiter.calls) != 2 {
		t.Fatalf("expected IP and token limiter calls, got %+v", limiter.calls)
	}
	if limiter.calls[0].action != InvitationRateLimitCalendarRSVPResolveIP ||
		limiter.calls[0].bucket != "ip:203.0.113.0/24" {
		t.Fatalf("unexpected IP limiter call: %+v", limiter.calls[0])
	}
	expectedDigest := sha256.Sum256([]byte(externalRSVPResolveToken))
	expectedTokenBucket := "token:" + hex.EncodeToString(expectedDigest[:])
	if limiter.calls[1].action != InvitationRateLimitCalendarRSVPResolveToken ||
		limiter.calls[1].bucket != expectedTokenBucket ||
		strings.Contains(limiter.calls[1].bucket, externalRSVPResolveToken) {
		t.Fatalf("unexpected token limiter call: %+v", limiter.calls[1])
	}
	assertExternalCalendarRSVPSecurityHeaders(t, response)
}

func TestExternalCalendarRSVPHandlersArePostOnly(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		respond bool
		path    string
	}{
		{name: "resolve", path: calendarInvitationResolvePath},
		{name: "respond", path: calendarInvitationRespondPath, respond: true},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &externalCalendarRSVPServiceStub{}
			limiter := &externalCalendarRSVPLimiterStub{}
			request := newExternalCalendarRSVPRequest(
				http.MethodGet,
				testCase.path,
				"",
				externalRSVPWebOrigin,
			)
			response := httptest.NewRecorder()

			newExternalCalendarRSVPTestHandler(
				service,
				limiter,
				testCase.respond,
			).ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed ||
				response.Header().Get("Allow") != http.MethodPost {
				t.Fatalf(
					"unexpected method response: status=%d allow=%q body=%s",
					response.Code,
					response.Header().Get("Allow"),
					response.Body.String(),
				)
			}
			if service.resolveCalls != 0 || service.respondCalls != 0 ||
				len(limiter.calls) != 0 {
				t.Fatalf(
					"unsupported method reached dependencies: service=%+v limiter=%+v",
					service,
					limiter.calls,
				)
			}
			assertExternalCalendarRSVPSecurityHeaders(t, response)
		})
	}
}

func TestExternalCalendarRSVPRespondRequiresExactPublicOrigin(t *testing.T) {
	t.Parallel()

	body := `{"token":"` + externalRSVPRespondToken +
		`","state":"accepted","expected_attendee_version":4,` +
		`"idempotency_key":"external-rsvp-http-0001"}`
	for _, testCase := range []struct {
		name   string
		origin string
		status int
	}{
		{name: "missing", status: http.StatusForbidden},
		{
			name:   "lookalike host",
			origin: externalRSVPWebOrigin + ".attacker.test",
			status: http.StatusForbidden,
		},
		{
			name:   "path is not an origin",
			origin: externalRSVPWebOrigin + "/calendar/respond",
			status: http.StatusForbidden,
		},
		{name: "allowlisted", origin: externalRSVPWebOrigin, status: http.StatusOK},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &externalCalendarRSVPServiceStub{
				respondResult: classroom.ExternalRSVPMutationResult{
					Projection: externalRSVPTestProjection(),
				},
			}
			limiter := &externalCalendarRSVPLimiterStub{}
			request := newExternalCalendarRSVPRequest(
				http.MethodPost,
				calendarInvitationRespondPath,
				body,
				testCase.origin,
			)
			response := httptest.NewRecorder()

			newExternalCalendarRSVPTestHandler(
				service,
				limiter,
				true,
			).ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf(
					"expected status %d, got %d: %s",
					testCase.status,
					response.Code,
					response.Body.String(),
				)
			}
			if testCase.status == http.StatusForbidden {
				if service.respondCalls != 0 || len(limiter.calls) != 0 {
					t.Fatalf(
						"rejected origin reached dependencies: service=%+v limiter=%+v",
						service,
						limiter.calls,
					)
				}
				var problem Problem
				if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
					t.Fatalf("decode origin problem: %v", err)
				}
				if problem.Code != "calendar_invitation_response_forbidden" {
					t.Fatalf("unexpected origin problem: %+v", problem)
				}
			} else if service.respondCalls != 1 ||
				service.respondInput.RawToken != externalRSVPRespondToken ||
				len(limiter.calls) != 2 {
				t.Fatalf(
					"allowlisted origin did not reach bounded response flow: service=%+v limiter=%+v",
					service,
					limiter.calls,
				)
			}
			assertExternalCalendarRSVPSecurityHeaders(t, response)
		})
	}
}

func TestExternalCalendarRSVPRejectsQueryOnlyCapability(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		respond bool
		path    string
		origin  string
	}{
		{
			name: "resolve",
			path: calendarInvitationResolvePath + "?token=" + externalRSVPResolveToken,
		},
		{
			name:    "respond",
			path:    calendarInvitationRespondPath + "?token=" + externalRSVPRespondToken,
			origin:  externalRSVPWebOrigin,
			respond: true,
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &externalCalendarRSVPServiceStub{}
			limiter := &externalCalendarRSVPLimiterStub{}
			request := newExternalCalendarRSVPRequest(
				http.MethodPost,
				testCase.path,
				`{}`,
				testCase.origin,
			)
			response := httptest.NewRecorder()

			newExternalCalendarRSVPTestHandler(
				service,
				limiter,
				testCase.respond,
			).ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf(
					"query-only capability must be rejected, got %d: %s",
					response.Code,
					response.Body.String(),
				)
			}
			if service.resolveCalls != 0 || service.respondCalls != 0 ||
				len(limiter.calls) != 0 {
				t.Fatalf(
					"query-only capability reached dependencies: service=%+v limiter=%+v",
					service,
					limiter.calls,
				)
			}
			assertExternalCalendarRSVPSecurityHeaders(t, response)
		})
	}
}

func TestExternalCalendarRSVPRateLimitIsTwoDimensionalAndPrivacySafe(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name          string
		respond       bool
		path          string
		body          string
		origin        string
		decisions     []InvitationRateLimitDecision
		expectedCalls []InvitationRateLimitAction
	}{
		{
			name: "resolve IP limit",
			path: calendarInvitationResolvePath,
			body: `{"token":"` + externalRSVPResolveToken + `"}`,
			decisions: []InvitationRateLimitDecision{{
				RetryAfter: 1500 * time.Millisecond,
			}},
			expectedCalls: []InvitationRateLimitAction{
				InvitationRateLimitCalendarRSVPResolveIP,
			},
		},
		{
			name:    "respond token limit",
			respond: true,
			path:    calendarInvitationRespondPath,
			body: `{"token":"` + externalRSVPRespondToken +
				`","state":"declined","expected_attendee_version":4,` +
				`"idempotency_key":"external-rsvp-http-0002"}`,
			origin: externalRSVPWebOrigin,
			decisions: []InvitationRateLimitDecision{
				{Allowed: true},
				{RetryAfter: 17 * time.Second},
			},
			expectedCalls: []InvitationRateLimitAction{
				InvitationRateLimitCalendarRSVPRespondIP,
				InvitationRateLimitCalendarRSVPRespondToken,
			},
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &externalCalendarRSVPServiceStub{}
			limiter := &externalCalendarRSVPLimiterStub{
				decisions: append(
					[]InvitationRateLimitDecision(nil),
					testCase.decisions...,
				),
			}
			request := newExternalCalendarRSVPRequest(
				http.MethodPost,
				testCase.path,
				testCase.body,
				testCase.origin,
			)
			request.RemoteAddr = "198.51.100.45:8443"
			response := httptest.NewRecorder()

			newExternalCalendarRSVPTestHandler(
				service,
				limiter,
				testCase.respond,
			).ServeHTTP(response, request)

			if response.Code != http.StatusTooManyRequests {
				t.Fatalf(
					"expected status 429, got %d: %s",
					response.Code,
					response.Body.String(),
				)
			}
			expectedRetryAfter := "2"
			if testCase.respond {
				expectedRetryAfter = "17"
			}
			if response.Header().Get("Retry-After") != expectedRetryAfter {
				t.Fatalf(
					"unexpected Retry-After %q",
					response.Header().Get("Retry-After"),
				)
			}
			if len(limiter.calls) != len(testCase.expectedCalls) {
				t.Fatalf("unexpected limiter calls: %+v", limiter.calls)
			}
			for index, action := range testCase.expectedCalls {
				if limiter.calls[index].action != action {
					t.Fatalf("unexpected limiter action: %+v", limiter.calls[index])
				}
				if strings.Contains(limiter.calls[index].bucket, "capability-for-http-test") {
					t.Fatalf("raw capability leaked into limiter bucket: %+v", limiter.calls[index])
				}
			}
			if service.resolveCalls != 0 || service.respondCalls != 0 ||
				strings.Contains(response.Body.String(), "capability-for-http-test") {
				t.Fatalf(
					"rate-limited request leaked or reached service: service=%+v body=%s",
					service,
					response.Body.String(),
				)
			}
			assertExternalCalendarRSVPSecurityHeaders(t, response)
		})
	}
}

func TestExternalCalendarRSVPRateLimiterFailureIsGenericUnavailable(t *testing.T) {
	t.Parallel()

	service := &externalCalendarRSVPServiceStub{}
	limiter := &externalCalendarRSVPLimiterStub{
		decisions: []InvitationRateLimitDecision{{
			Err: errors.New("rate-limit storage unavailable"),
		}},
	}
	request := newExternalCalendarRSVPRequest(
		http.MethodPost,
		calendarInvitationResolvePath,
		`{"token":"`+externalRSVPResolveToken+`"}`,
		"",
	)
	response := httptest.NewRecorder()

	newExternalCalendarRSVPTestHandler(
		service,
		limiter,
		false,
	).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable ||
		response.Header().Get("Retry-After") != "" {
		t.Fatalf(
			"unexpected limiter failure response: status=%d retry-after=%q body=%s",
			response.Code,
			response.Header().Get("Retry-After"),
			response.Body.String(),
		)
	}
	var problem Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode limiter failure problem: %v", err)
	}
	if problem.Code != "calendar_invitation_service_unavailable" ||
		service.resolveCalls != 0 ||
		strings.Contains(response.Body.String(), externalRSVPResolveToken) {
		t.Fatalf(
			"limiter failure was distinguishable or reached service: problem=%+v service=%+v",
			problem,
			service,
		)
	}
	assertExternalCalendarRSVPSecurityHeaders(t, response)
}

func TestExternalCalendarRSVPMapsCapabilityErrorsWithoutEnumerationDetail(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name         string
		serviceError error
		status       int
		code         string
		title        string
	}{
		{
			name:         "unavailable",
			serviceError: classroom.ErrExternalRSVPCapabilityUnavailable,
			status:       http.StatusNotFound,
			code:         "calendar_invitation_unavailable",
			title:        "Calendar invitation unavailable",
		},
		{
			name:         "wrapped unavailable",
			serviceError: errors.Join(errors.New("expired"), classroom.ErrExternalRSVPCapabilityUnavailable),
			status:       http.StatusNotFound,
			code:         "calendar_invitation_unavailable",
			title:        "Calendar invitation unavailable",
		},
		{
			name:         "unexpected storage failure",
			serviceError: errors.New("storage exploded"),
			status:       http.StatusInternalServerError,
			code:         "calendar_invitation_failed",
			title:        "Calendar invitation request failed",
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &externalCalendarRSVPServiceStub{
				resolveError: testCase.serviceError,
			}
			request := newExternalCalendarRSVPRequest(
				http.MethodPost,
				calendarInvitationResolvePath,
				`{"token":"`+externalRSVPResolveToken+`"}`,
				"",
			)
			response := httptest.NewRecorder()

			newExternalCalendarRSVPTestHandler(
				service,
				&externalCalendarRSVPLimiterStub{},
				false,
			).ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf(
					"expected status %d, got %d: %s",
					testCase.status,
					response.Code,
					response.Body.String(),
				)
			}
			var problem Problem
			if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
				t.Fatalf("decode mapped problem: %v", err)
			}
			if problem.Code != testCase.code || problem.Title != testCase.title {
				t.Fatalf("unexpected mapped problem: %+v", problem)
			}
			if strings.Contains(response.Body.String(), externalRSVPResolveToken) ||
				strings.Contains(response.Body.String(), testCase.serviceError.Error()) {
				t.Fatalf("capability or internal error leaked: %s", response.Body.String())
			}
			assertExternalCalendarRSVPSecurityHeaders(t, response)
		})
	}
}

func newExternalCalendarRSVPTestHandler(
	service classroom.ExternalRSVPServiceAPI,
	limiter InvitationRateLimiter,
	respond bool,
) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handlers := newExternalCalendarRSVPHandlers(
		logger,
		service,
		limiter,
		func() time.Time { return externalRSVPTestTime },
		externalRSVPWebOrigin,
	)
	if respond {
		return externalCalendarRSVPResponseHeaders(http.HandlerFunc(handlers.respond))
	}
	return externalCalendarRSVPResponseHeaders(http.HandlerFunc(handlers.resolve))
}

func newExternalCalendarRSVPRequest(
	method string,
	path string,
	body string,
	origin string,
) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	return request
}

func assertExternalCalendarRSVPSecurityHeaders(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()

	expected := map[string]string{
		"Cache-Control":                "no-store",
		"Pragma":                       "no-cache",
		"Referrer-Policy":              "no-referrer",
		"X-Robots-Tag":                 "noindex, nofollow",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Content-Security-Policy":      "default-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
	}
	for name, expectedValue := range expected {
		if actual := response.Header().Get(name); actual != expectedValue {
			t.Fatalf(
				"unexpected %s header: got %q want %q",
				name,
				actual,
				expectedValue,
			)
		}
	}
}

func externalRSVPTestProjection() classroom.ExternalRSVPProjection {
	return classroom.ExternalRSVPProjection{
		Title:               "English review",
		StartsAt:            externalRSVPTestTime.Add(time.Hour),
		EndsAt:              externalRSVPTestTime.Add(2 * time.Hour),
		Timezone:            "Asia/Ho_Chi_Minh",
		RSVPState:           classroom.RSVPStateNeedsAction,
		ResponseRequested:   true,
		AttendeeVersion:     4,
		InvitationSequence:  2,
		CapabilityExpiresAt: externalRSVPTestTime.Add(7 * 24 * time.Hour),
	}
}

type externalCalendarRSVPServiceStub struct {
	resolveCalls      int
	resolveToken      string
	resolveProjection classroom.ExternalRSVPProjection
	resolveError      error
	respondCalls      int
	respondInput      classroom.ExternalRSVPResponseInput
	respondResult     classroom.ExternalRSVPMutationResult
	respondError      error
}

func (service *externalCalendarRSVPServiceStub) IssueCapability(
	context.Context,
	classroom.AccessContext,
	uuid.UUID,
	classroom.ExternalRSVPCapabilityIssue,
) (classroom.ExternalRSVPCapabilityToken, error) {
	return classroom.ExternalRSVPCapabilityToken{}, errors.New("unexpected capability issue")
}

func (service *externalCalendarRSVPServiceStub) ResolveCapability(
	_ context.Context,
	rawToken string,
) (classroom.ExternalRSVPProjection, error) {
	service.resolveCalls++
	service.resolveToken = rawToken
	return service.resolveProjection, service.resolveError
}

func (service *externalCalendarRSVPServiceStub) RespondWithCapability(
	_ context.Context,
	input classroom.ExternalRSVPResponseInput,
) (classroom.ExternalRSVPMutationResult, error) {
	service.respondCalls++
	service.respondInput = input
	return service.respondResult, service.respondError
}

type externalCalendarRSVPRateLimitCall struct {
	action InvitationRateLimitAction
	bucket string
	now    time.Time
}

type externalCalendarRSVPLimiterStub struct {
	decisions []InvitationRateLimitDecision
	calls     []externalCalendarRSVPRateLimitCall
}

func (limiter *externalCalendarRSVPLimiterStub) Allow(
	_ context.Context,
	action InvitationRateLimitAction,
	bucket string,
	now time.Time,
) InvitationRateLimitDecision {
	limiter.calls = append(limiter.calls, externalCalendarRSVPRateLimitCall{
		action: action,
		bucket: bucket,
		now:    now,
	})
	if len(limiter.decisions) == 0 {
		return InvitationRateLimitDecision{Allowed: true}
	}
	decision := limiter.decisions[0]
	limiter.decisions = limiter.decisions[1:]
	return decision
}
