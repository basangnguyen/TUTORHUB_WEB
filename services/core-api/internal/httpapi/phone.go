package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type phoneStartRequest struct {
	PhoneNumber  string `json:"phone_number"`
	CountryCode  string `json:"country_code,omitempty"`
	KeepSignedIn bool   `json:"keep_signed_in,omitempty"`
}

func (phoneStartRequest) String() string         { return "[redacted phone input]" }
func (p phoneStartRequest) LogValue() slog.Value { return slog.StringValue(p.String()) }

type phoneVerifyRequest struct {
	ChallengeID  uuid.UUID `json:"challenge_id"`
	Code         string    `json:"code"`
	KeepSignedIn bool      `json:"keep_signed_in,omitempty"`
}

func (phoneVerifyRequest) String() string         { return "[redacted phone verification]" }
func (p phoneVerifyRequest) LogValue() slog.Value { return slog.StringValue(p.String()) }

type myPhoneResponse struct {
	Phone *identity.PhoneSummary `json:"phone"`
}

func (h nativeAuthHandlers) phoneService(w http.ResponseWriter, r *http.Request, enabled bool) (identity.NativePhoneAPI, bool) {
	if !h.available(w, r, enabled) {
		return nil, false
	}
	s, ok := h.service.(identity.NativePhoneAPI)
	if !ok {
		h.writeError(w, r, identity.ErrNativeMethodDisabled)
	}
	return s, ok
}

func (h nativeAuthHandlers) startPhoneLogin(w http.ResponseWriter, r *http.Request) {
	h.startPhone(w, r, false)
}

func (h nativeAuthHandlers) startMyPhone(w http.ResponseWriter, r *http.Request) {
	h.startPhone(w, r, true)
}

func (h nativeAuthHandlers) startPhone(w http.ResponseWriter, r *http.Request, manage bool) {
	auth := h.auth.config.Authentication
	s, ok := h.phoneService(w, r, auth.PhoneEnabled)
	if !ok {
		return
	}
	var p identity.Principal
	if manage {
		_, p, ok = h.securityPrincipalEnabled(w, r, true, auth.PhoneEnabled)
		if !ok {
			return
		}
	}
	if !h.allowIP(w, r, AuthRateLimitOTPSendIP) {
		return
	}
	var req phoneStartRequest
	if err := decodeJSONRequest(w, r, &req, 2048); err != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	input := identity.PhoneStartInput{PhoneNumber: req.PhoneNumber, CountryCode: req.CountryCode, KeepSignedIn: req.KeepSignedIn, RemoteAddress: r.RemoteAddr, Guard: h.phoneSendGuard}
	var result identity.SignupChallengeResult
	var err error
	if manage {
		result, err = s.StartMyPhone(r.Context(), p, input)
	} else {
		result, err = s.StartPhoneLogin(r.Context(), input)
	}
	if err != nil {
		h.writePhoneError(w, r, err)
		return
	}
	h.writeChallenge(w, result)
}

// An email transaction is handled by the existing email flow after this check.
func (h nativeAuthHandlers) tryPhoneResend(w http.ResponseWriter, r *http.Request, binding string) bool {
	s, ok := h.service.(identity.NativePhoneAPI)
	if !ok {
		return false
	}
	var p *identity.Principal
	if len(r.Header.Values("X-CSRF-Token")) > 0 {
		_, principal, ok := h.securityPrincipalEnabled(w, r, true, true)
		if !ok {
			return true
		}
		p = &principal
	}
	result, err := s.ResendPhoneChallenge(r.Context(), binding, p, h.phoneSendGuard)
	if errors.Is(err, identity.ErrAuthTransactionUnavailable) {
		auth := h.auth.config.Authentication
		if auth.SignupEnabled || auth.EmailCodeEnabled {
			return false
		}
		h.writeError(w, r, err)
		return true
	}
	if err != nil {
		h.writePhoneError(w, r, err)
		return true
	}
	h.writeChallenge(w, result)
	return true
}

func (h nativeAuthHandlers) verifyPhoneLogin(w http.ResponseWriter, r *http.Request) {
	h.verifyPhone(w, r, false)
}

func (h nativeAuthHandlers) verifyMyPhone(w http.ResponseWriter, r *http.Request) {
	h.verifyPhone(w, r, true)
}

func (h nativeAuthHandlers) verifyPhone(w http.ResponseWriter, r *http.Request, manage bool) {
	s, ok := h.phoneService(w, r, h.auth.config.Authentication.PhoneEnabled)
	if !ok {
		return
	}
	var p identity.Principal
	if manage {
		_, p, ok = h.securityPrincipalEnabled(w, r, true, true)
		if !ok {
			return
		}
	}
	if !h.allowIP(w, r, AuthRateLimitOTPVerifyIP) {
		return
	}
	var req phoneVerifyRequest
	if err := decodeJSONRequest(w, r, &req, 2048); err != nil || req.ChallengeID == uuid.Nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	binding, ok := h.accountTransactionBinding(w, r)
	if !ok {
		return
	}
	input := identity.EmailCodeVerifyInput{ChallengeID: req.ChallengeID, Code: req.Code, KeepSignedIn: req.KeepSignedIn, UserAgent: r.UserAgent(), RemoteAddress: r.RemoteAddr}
	if manage {
		result, err := s.VerifyMyPhone(r.Context(), p, binding, input)
		if err != nil {
			h.writePhoneError(w, r, err)
			return
		}
		h.clearTransactionCookie(w)
		writeJSON(h.auth.logger, w, http.StatusOK, myPhoneResponse{Phone: result})
		return
	}
	result, err := s.VerifyPhoneLogin(r.Context(), binding, input)
	if err != nil {
		h.writePhoneError(w, r, err)
		return
	}
	if h.writeMFAChallenge(w, result.MFA) {
		return
	}
	h.setSession(w, result)
	h.clearTransactionCookie(w)
	writeJSON(h.auth.logger, w, http.StatusOK, nativeAuthenticationResponse{Status: "authenticated"})
}

func (h nativeAuthHandlers) getMyPhone(w http.ResponseWriter, r *http.Request) {
	auth := h.auth.config.Authentication
	s, ok := h.phoneService(w, r, auth.PhoneEnabled)
	if !ok {
		return
	}
	_, p, ok := h.securityPrincipalEnabled(w, r, false, true)
	if !ok {
		return
	}
	result, err := s.GetMyPhone(r.Context(), p)
	if err != nil {
		h.writePhoneError(w, r, err)
		return
	}
	writeJSON(h.auth.logger, w, http.StatusOK, myPhoneResponse{Phone: result})
}

func (h nativeAuthHandlers) removeMyPhone(w http.ResponseWriter, r *http.Request) {
	s, ok := h.phoneService(w, r, h.auth.config.Authentication.PhoneEnabled)
	if !ok {
		return
	}
	_, p, ok := h.securityPrincipalEnabled(w, r, true, true)
	if !ok {
		return
	}
	result, err := s.RemoveMyPhone(r.Context(), p)
	if err != nil {
		h.writePhoneError(w, r, err)
		return
	}
	h.auth.setCookie(w, h.auth.cookieNames.session, result.SessionToken, result.CookieExpiresAt)
	h.auth.setCookie(w, h.auth.cookieNames.csrf, result.CSRFToken, result.CookieExpiresAt)
	w.WriteHeader(http.StatusNoContent)
}

func (h nativeAuthHandlers) writePhoneError(w http.ResponseWriter, r *http.Request, err error) {
	var limited *phoneDestinationLimitError
	switch {
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", retryAfterSeconds(limited.RetryAfter))
		writeProblem(w, r, 429, "Too many requests", "Please wait before requesting another code.")
	case errors.Is(err, identity.ErrPhoneStale):
		h.clearTransactionCookie(w)
		writeCodedProblem(w, r, 403, "auth_phone_stale", "Phone verification required", "Sign in another way and verify this number again in Settings.")
	case errors.Is(err, identity.ErrPhoneSecondFactorRequired):
		h.clearTransactionCookie(w)
		writeCodedProblem(w, r, 403, "auth_phone_second_factor_required", "Additional verification required", "Sign in with email, a password, a passkey or a linked provider.")
	case errors.Is(err, identity.ErrPhoneInUse):
		h.clearTransactionCookie(w)
		writeCodedProblem(w, r, 409, "auth_phone_in_use", "Phone number unavailable", "This number cannot be added to your account.")
	case errors.Is(err, identity.ErrPhoneDeliveryUnavailable):
		writeCodedProblem(w, r, 503, "auth_phone_delivery_unavailable", "Phone delivery unavailable", "Try signing in with email, a passkey or a linked provider.")
	case errors.Is(err, identity.ErrInvalidPhoneNumber):
		writeCodedProblem(w, r, 400, "auth_invalid_phone_number", "Invalid phone number", "Enter a supported mobile number.")
	case errors.Is(err, identity.ErrLastIdentity):
		writeCodedProblem(w, r, 409, "auth_last_identity", "Phone number cannot be removed", "Keep at least one sign-in method on your account.")
	case errors.Is(err, identity.ErrRecentAuthenticationRequired):
		writeCodedProblem(w, r, 403, "auth_recent_authentication_required", "Recent authentication required", "Verify your identity again before changing your phone number.")
	default:
		h.writeAccountCodeError(w, r, err)
	}
}
