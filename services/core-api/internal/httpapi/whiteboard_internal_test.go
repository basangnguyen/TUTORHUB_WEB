package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/collaboration"
)

const whiteboardInternalTestToken = "internal-runtime-token-32-characters"

func TestWhiteboardInternalGrantExchangeRequiresServiceAuthentication(t *testing.T) {
	t.Parallel()
	service := &fakeWhiteboardInternalService{}
	handlers := newWhiteboardInternalHandlers(slog.New(slog.NewTextHandler(io.Discard, nil)), service, whiteboardInternalTestToken)
	request := httptest.NewRequest(http.MethodPost, whiteboardInternalGrantExchangePath,
		strings.NewReader(`{"grant":"one-time-grant-that-is-long-enough","origin":"https://app.example.test","provider_document_name":"wb_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.exchangeGrant(response, request)

	if response.Code != http.StatusUnauthorized || service.consumeCalls != 0 {
		t.Fatalf("unauthorized exchange status=%d calls=%d", response.Code, service.consumeCalls)
	}
	assertWhiteboardInternalHeaders(t, response)
}

func TestWhiteboardInternalGrantExchangeIsStrictAndDoesNotLogCredentials(t *testing.T) {
	t.Parallel()
	lease := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scope := testWhiteboardInternalScope(lease)
	service := &fakeWhiteboardInternalService{scope: scope}
	var logs bytes.Buffer
	handlers := newWhiteboardInternalHandlers(slog.New(slog.NewJSONHandler(&logs, nil)), service, whiteboardInternalTestToken)
	grant := "one-time-grant-that-is-long-enough"
	body, err := json.Marshal(map[string]string{
		"grant": grant, "origin": "https://app.example.test",
		"provider_document_name": scope.ProviderDocumentName,
	})
	if err != nil {
		t.Fatalf("marshal exchange request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, whiteboardInternalGrantExchangePath, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+whiteboardInternalTestToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.exchangeGrant(response, request)

	if response.Code != http.StatusOK || service.consumeCalls != 1 ||
		service.consumeInput.Credential != grant ||
		service.consumeInput.ProviderDocumentName != scope.ProviderDocumentName {
		t.Fatalf("exchange status=%d calls=%d input=%+v", response.Code, service.consumeCalls, service.consumeInput)
	}
	if strings.Contains(logs.String(), grant) || strings.Contains(logs.String(), lease) {
		t.Fatal("grant or authority lease appeared in logs")
	}
	assertWhiteboardInternalHeaders(t, response)

	invalid := httptest.NewRequest(http.MethodPost, whiteboardInternalGrantExchangePath,
		strings.NewReader(`{"grant":"one-time-grant-that-is-long-enough","origin":"https://app.example.test","provider_document_name":"wb_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","actor_id":"forged"}`))
	invalid.Header.Set("Authorization", "Bearer "+whiteboardInternalTestToken)
	invalid.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	handlers.exchangeGrant(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || service.consumeCalls != 1 {
		t.Fatalf("unknown field status=%d calls=%d", invalidResponse.Code, service.consumeCalls)
	}
}

func TestWhiteboardInternalValidatesExactAuthorityLeases(t *testing.T) {
	t.Parallel()
	lease := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	scope := testWhiteboardInternalScope(lease)
	service := &fakeWhiteboardInternalService{valid: true}
	handlers := newWhiteboardInternalHandlers(slog.New(slog.NewTextHandler(io.Discard, nil)), service, whiteboardInternalTestToken)
	body, err := json.Marshal(whiteboardInternalValidationRequest{
		Scopes: []whiteboardInternalScopeRequest{{
			AuthorityLease: &scope.AuthorityLease, ActorID: &scope.ActorID,
			Capability: &scope.Capability, DocumentID: &scope.DocumentID,
			Generation: &scope.Generation, Origin: stringPointer("https://app.example.test"),
			ProviderDocumentName: &scope.ProviderDocumentName, SessionID: &scope.SessionID,
			TenantID: &scope.TenantID, WriterFence: &scope.WriterFence,
		}},
	})
	if err != nil {
		t.Fatalf("marshal validation request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, whiteboardInternalGrantValidatePath, bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+whiteboardInternalTestToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.validateGrants(response, request)

	if response.Code != http.StatusOK || service.validateCalls != 1 ||
		service.validateInput.Scope != scope {
		t.Fatalf("validate status=%d calls=%d input=%+v", response.Code, service.validateCalls, service.validateInput)
	}
	var payload whiteboardInternalValidationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil ||
		len(payload.ValidAuthorityLeases) != 1 || payload.ValidAuthorityLeases[0] != lease {
		t.Fatalf("validation response=%s err=%v", response.Body.String(), err)
	}
}

type fakeWhiteboardInternalService struct {
	consumeCalls  int
	consumeInput  collaboration.GrantConsumeInput
	scope         collaboration.GrantScope
	consumeError  error
	validateCalls int
	validateInput collaboration.GrantValidationInput
	valid         bool
	validateError error
}

func (service *fakeWhiteboardInternalService) ConsumeGrant(
	_ context.Context,
	input collaboration.GrantConsumeInput,
) (collaboration.GrantScope, error) {
	service.consumeCalls++
	service.consumeInput = input
	return service.scope, service.consumeError
}

func (service *fakeWhiteboardInternalService) ValidateGrant(
	_ context.Context,
	input collaboration.GrantValidationInput,
) (bool, error) {
	service.validateCalls++
	service.validateInput = input
	return service.valid, service.validateError
}

func testWhiteboardInternalScope(lease string) collaboration.GrantScope {
	return collaboration.GrantScope{
		AuthorityLease: lease, ActorID: uuid.New(), Capability: collaboration.CapabilityEdit,
		DocumentID: uuid.New(), Generation: 3,
		ProviderDocumentName: "wb_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SessionID:            uuid.New(), TenantID: uuid.New(), WriterFence: 5,
	}
}

func stringPointer(value string) *string {
	return &value
}

func assertWhiteboardInternalHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing privacy headers: %v", response.Header())
	}
}
