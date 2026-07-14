package identity

import "errors"

var (
	ErrAuthenticationDisabled = errors.New("authentication is not configured")
	ErrInvalidReturnTo        = errors.New("return_to must be an internal application path")
	ErrInvalidAuthFlow        = errors.New("authentication flow is invalid, expired, or already consumed")
	ErrProviderExchange       = errors.New("identity provider exchange failed")
	ErrVerifiedEmailRequired  = errors.New("a verified email claim is required")
	ErrSessionNotFound        = errors.New("session is missing, expired, or revoked")
	ErrInvalidCSRFToken       = errors.New("CSRF token is invalid")
	ErrInvalidTenant          = errors.New("tenant input is invalid")
	ErrTenantSlugTaken        = errors.New("tenant slug is already in use")
	ErrTenantCreationDenied   = errors.New("the current user cannot create another tenant")
	ErrTenantAccessDenied     = errors.New("the current user cannot access this tenant")
)
