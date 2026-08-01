package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type availabilityPollPublicServiceStub struct {
	calendar.AvailabilityPollServiceAPI
	resolveCalls     int
	publicID         uuid.UUID
	rawToken         string
	individualCalls  int
	individualScope  tenancy.Context
	individualPollID uuid.UUID
	individualInput  calendar.ListAvailabilityPollResponsesInput
	individualPage   calendar.AvailabilityPollIndividualResponsePage
	individualError  error
}

func (stub *availabilityPollPublicServiceStub) ResolvePublic(
	_ context.Context,
	publicID uuid.UUID,
	rawToken string,
) (calendar.PublicAvailabilityPollExchange, error) {
	stub.resolveCalls++
	stub.publicID = publicID
	stub.rawToken = rawToken
	now := time.Date(2030, time.August, 1, 0, 0, 0, 0, time.UTC)
	return calendar.PublicAvailabilityPollExchange{
		Poll: calendar.PublicAvailabilityPoll{
			PublicID:    publicID,
			Title:       "Study group",
			Timezone:    "UTC",
			Status:      calendar.PollStatusOpen,
			Slots:       []calendar.AvailabilityPollSlot{},
			RankedSlots: []calendar.AvailabilityPollRankedSlot{},
			DeadlineAt:  now.Add(time.Hour),
		},
		ResponseToken:          "v1.response-session-token",
		ResponseTokenExpiresAt: now.Add(30 * time.Minute),
	}, nil
}

func (stub *availabilityPollPublicServiceStub) ListIndividualResponses(
	_ context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input calendar.ListAvailabilityPollResponsesInput,
) (calendar.AvailabilityPollIndividualResponsePage, error) {
	stub.individualCalls++
	stub.individualScope = scope
	stub.individualPollID = pollID
	stub.individualInput = input
	return stub.individualPage, stub.individualError
}

type availabilityPollRateLimitCall struct {
	action InvitationRateLimitAction
	bucket string
}

type availabilityPollRateLimiterStub struct {
	calls  []availabilityPollRateLimitCall
	denyAt int
}

func (stub *availabilityPollRateLimiterStub) Allow(
	_ context.Context,
	action InvitationRateLimitAction,
	bucket string,
	_ time.Time,
) InvitationRateLimitDecision {
	stub.calls = append(stub.calls, availabilityPollRateLimitCall{
		action: action,
		bucket: bucket,
	})
	if stub.denyAt > 0 && len(stub.calls) == stub.denyAt {
		return InvitationRateLimitDecision{RetryAfter: 45 * time.Second}
	}
	return InvitationRateLimitDecision{Allowed: true}
}

func TestPublicAvailabilityPollResolveEnforcesOriginHeadersAndSafeRateBuckets(t *testing.T) {
	t.Parallel()

	publicID := uuid.MustParse("8818c018-b6c5-4f44-a844-7cbec84a986d")
	rawToken := "v1.raw-capability-that-must-not-be-a-rate-key"
	service := &availabilityPollPublicServiceStub{}
	limiter := &availabilityPollRateLimiterStub{}
	handlers := newAvailabilityPollHandlers(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		authHandlers{},
		service,
		limiter,
		func() time.Time { return time.Date(2030, time.August, 1, 0, 0, 0, 0, time.UTC) },
		"https://web.example.test",
	)
	handler := publicAvailabilityPollResponseHeaders(http.HandlerFunc(handlers.resolvePublic))

	request := httptest.NewRequest(
		http.MethodPost,
		availabilityPollResolvePath,
		strings.NewReader(`{"public_id":"`+publicID.String()+`","token":"`+rawToken+`"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://web.example.test")
	request.RemoteAddr = "203.0.113.42:49152"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	for name, expected := range map[string]string{
		"Cache-Control":                "no-store",
		"Pragma":                       "no-cache",
		"Referrer-Policy":              "no-referrer",
		"X-Robots-Tag":                 "noindex, nofollow",
		"Cross-Origin-Resource-Policy": "same-origin",
	} {
		if got := recorder.Header().Get(name); got != expected {
			t.Fatalf("%s = %q, want %q", name, got, expected)
		}
	}
	if !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("CSP = %q", recorder.Header().Get("Content-Security-Policy"))
	}
	if service.resolveCalls != 1 || service.publicID != publicID || service.rawToken != rawToken {
		t.Fatalf("resolve call = count:%d id:%s token:%q", service.resolveCalls, service.publicID, service.rawToken)
	}
	if len(limiter.calls) != 3 {
		t.Fatalf("rate-limit calls = %+v", limiter.calls)
	}
	for _, call := range limiter.calls {
		if strings.Contains(call.bucket, rawToken) {
			t.Fatalf("raw capability leaked into rate-limit bucket: %+v", call)
		}
	}
	if limiter.calls[0].action != InvitationRateLimitAvailabilityPollResolveIP ||
		limiter.calls[0].bucket != "ip:203.0.113.0/24" ||
		limiter.calls[1].action != InvitationRateLimitAvailabilityPollResolveTokenDigest ||
		!strings.HasPrefix(limiter.calls[1].bucket, "token:") ||
		limiter.calls[2].action != InvitationRateLimitAvailabilityPollResolvePublicID ||
		limiter.calls[2].bucket != "public_id:"+publicID.String() {
		t.Fatalf("unexpected rate-limit dimensions: %+v", limiter.calls)
	}
}

func TestPublicAvailabilityPollResolveFailsClosedBeforeCapabilityLookup(t *testing.T) {
	t.Parallel()

	publicID := uuid.MustParse("8818c018-b6c5-4f44-a844-7cbec84a986d")
	for _, test := range []struct {
		name       string
		origin     string
		denyAt     int
		wantStatus int
	}{
		{name: "foreign origin", origin: "https://attacker.example", wantStatus: http.StatusForbidden},
		{name: "rate limited", origin: "https://web.example.test", denyAt: 2, wantStatus: http.StatusTooManyRequests},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &availabilityPollPublicServiceStub{}
			limiter := &availabilityPollRateLimiterStub{denyAt: test.denyAt}
			handlers := newAvailabilityPollHandlers(
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				authHandlers{},
				service,
				limiter,
				time.Now,
				"https://web.example.test",
			)
			handler := publicAvailabilityPollResponseHeaders(http.HandlerFunc(handlers.resolvePublic))
			request := httptest.NewRequest(
				http.MethodPost,
				availabilityPollResolvePath,
				strings.NewReader(`{"public_id":"`+publicID.String()+`","token":"v1.secret"}`),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", test.origin)
			request.RemoteAddr = "203.0.113.42:49152"
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if service.resolveCalls != 0 {
				t.Fatalf("capability lookup ran on rejected request")
			}
			if test.denyAt == 0 && len(limiter.calls) != 0 {
				t.Fatalf("origin rejection reached limiter: %+v", limiter.calls)
			}
			if test.denyAt > 0 && recorder.Header().Get("Retry-After") != "45" {
				t.Fatalf("Retry-After = %q", recorder.Header().Get("Retry-After"))
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("rejected response is cacheable")
			}
		})
	}
}

func TestAvailabilityPollIndividualResponsesArePrivateBoundedAndTenantScoped(t *testing.T) {
	t.Parallel()

	tenantID, actorID, pollID := uuid.New(), uuid.New(), uuid.New()
	nextCursor := "thapir1_next-page"
	service := &availabilityPollPublicServiceStub{
		individualPage: calendar.AvailabilityPollIndividualResponsePage{
			Responses: []calendar.AvailabilityPollIndividualResponse{{
				ResponseID: uuid.New(), ActorType: "internal_member", Version: 1,
				Answers: []calendar.AvailabilityPollAnswer{}, SubmittedAt: calendarTestTime,
			}},
			NextCursor: &nextCursor,
		},
	}
	handler := NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{
			Clock:             func() time.Time { return calendarTestTime },
			Identity:          classIdentityService(tenantID, actorID, nil),
			AvailabilityPolls: service,
		},
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/calendar/availability-polls/"+pollID.String()+
			"/responses?cursor=thapir1_current&limit=17",
		nil,
	)
	addSessionCookie(request)
	request.Header.Set(calendarTenantHeader, tenantID.String())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("individual response page is cacheable: %q", recorder.Header().Get("Cache-Control"))
	}
	if service.individualCalls != 1 || service.individualScope.TenantID != tenantID ||
		service.individualScope.ActorID != actorID || service.individualPollID != pollID ||
		service.individualInput.Cursor != "thapir1_current" || service.individualInput.Limit != 17 {
		t.Fatalf("unexpected individual response call: %+v", service)
	}
	var page calendar.AvailabilityPollIndividualResponsePage
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Responses) != 1 || page.NextCursor == nil || *page.NextCursor != nextCursor {
		t.Fatalf("page = %+v", page)
	}
}
