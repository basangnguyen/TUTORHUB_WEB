package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type facebookDeletionResponse struct {
	URL              string `json:"url"`
	ConfirmationCode string `json:"confirmation_code"`
}
type facebookDeletionStatusResponse struct {
	Status string `json:"status"`
}

func (h federationHandlers) facebookCallbacks(w http.ResponseWriter, r *http.Request) (identity.FacebookCallbackAPI, bool) {
	service, ok := h.auth.identity.(identity.FacebookCallbackAPI)
	if !ok {
		writeFederationProviderUnavailable(w, r)
		return nil, false
	}
	// Separate signed callbacks/status reads from browser OAuth limits while reusing the existing budget.
	if !h.allow(w, r, AuthRateLimitOAuthCallbackIP, "facebook-meta:"+federationIP(r)) {
		return nil, false
	}
	return service, true
}

func (h federationHandlers) facebookSignedCallbacks(w http.ResponseWriter, r *http.Request) (identity.FacebookCallbackAPI, bool) {
	if h.auth.config.Authentication.FacebookClientSecret == "" {
		writeFederationProviderUnavailable(w, r)
		return nil, false
	}
	return h.facebookCallbacks(w, r)
}

func facebookSignedForm(w http.ResponseWriter, r *http.Request) (string, error) {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || typ != "application/x-www-form-urlencoded" {
		return "", identity.ErrFacebookSignedRequest
	}
	const size = 64 << 10
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, size))
	if err != nil {
		return "", identity.ErrFacebookSignedRequest
	}
	fields, err := url.ParseQuery(string(body))
	signed := fields["signed_request"]
	if err != nil || len(fields) != 1 || len(signed) != 1 || signed[0] == "" {
		return "", identity.ErrFacebookSignedRequest
	}
	return signed[0], nil
}

func (h federationHandlers) facebookCallbackProblem(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrFacebookSignedRequest):
		writeCodedProblem(w, r, http.StatusBadRequest, "auth_invalid_signed_request", "Invalid callback", "The callback could not be verified.")
	case errors.Is(err, identity.ErrDeletionRequestNotFound):
		writeCodedProblem(w, r, http.StatusNotFound, "auth_deletion_request_unavailable", "Request unavailable", "The deletion request is unavailable.")
	default:
		h.writeError(w, r, err, false)
	}
}

func (h federationHandlers) facebookDataDeletion(w http.ResponseWriter, r *http.Request) {
	service, ok := h.facebookSignedCallbacks(w, r)
	if !ok {
		return
	}
	signed, err := facebookSignedForm(w, r)
	if err != nil {
		h.facebookCallbackProblem(w, r, err)
		return
	}
	cfg := h.auth.config.Authentication
	code, err := service.FacebookDelete(r.Context(), signed, cfg.FacebookClientSecret, cfg.FacebookIssuerURL)
	if err != nil {
		h.facebookCallbackProblem(w, r, err)
		return
	}
	statusURL := strings.TrimRight(h.auth.config.WebOrigin, "/") + "/legal/data-deletion?code=" + url.QueryEscape(code)
	writeJSON(h.auth.logger, w, http.StatusOK, facebookDeletionResponse{URL: statusURL, ConfirmationCode: code})
}

func (h federationHandlers) facebookDeauthorize(w http.ResponseWriter, r *http.Request) {
	service, ok := h.facebookSignedCallbacks(w, r)
	if !ok {
		return
	}
	signed, err := facebookSignedForm(w, r)
	if err == nil {
		cfg := h.auth.config.Authentication
		err = service.FacebookDeauthorize(r.Context(), signed, cfg.FacebookClientSecret, cfg.FacebookIssuerURL)
	}
	if err != nil {
		h.facebookCallbackProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h federationHandlers) facebookDeletionStatus(w http.ResponseWriter, r *http.Request) {
	service, ok := h.facebookCallbacks(w, r)
	if !ok {
		return
	}
	status, err := service.FacebookDeletionStatus(r.Context(), r.PathValue("code"))
	if err != nil {
		h.facebookCallbackProblem(w, r, err)
		return
	}
	writeJSON(h.auth.logger, w, http.StatusOK, facebookDeletionStatusResponse{Status: status})
}
