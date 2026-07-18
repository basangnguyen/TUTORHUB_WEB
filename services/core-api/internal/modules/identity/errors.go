package identity

import "errors"

var (
	ErrAuthenticationDisabled       = errors.New("authentication is not configured")
	ErrInvalidReturnTo              = errors.New("return_to must be an internal application path")
	ErrInvalidAuthFlow              = errors.New("authentication flow is invalid, expired, or already consumed")
	ErrProviderExchange             = errors.New("identity provider exchange failed")
	ErrVerifiedEmailRequired        = errors.New("a verified email claim is required")
	ErrSessionNotFound              = errors.New("session is missing, expired, or revoked")
	ErrInvalidCSRFToken             = errors.New("CSRF token is invalid")
	ErrInvalidTenant                = errors.New("tenant input is invalid")
	ErrTenantSlugTaken              = errors.New("tenant slug is already in use")
	ErrTenantCreationDenied         = errors.New("the current user cannot create another tenant")
	ErrTenantAccessDenied           = errors.New("the current user cannot access this tenant")
	ErrTenantNotFound               = errors.New("the tenant was not found in the active workspace")
	ErrTenantVersionConflict        = errors.New("the tenant metadata version is stale")
	ErrLastManagedTenant            = errors.New("the final managed tenant cannot be archived")
	ErrSessionContextConflict       = errors.New("the workspace session context changed concurrently")
	ErrInvalidProfile               = errors.New("profile input is invalid")
	ErrRecentAuthenticationRequired = errors.New("recent authentication is required")
	ErrIdentityConflict             = errors.New("the external identity is already linked to another user")
	ErrIdentityNotFound             = errors.New("the external identity was not found")
	ErrLastIdentity                 = errors.New("the final active identity cannot be unlinked")
	ErrIdentityLinkRequired         = errors.New("the external identity must be linked from an authenticated session")
	ErrIdentityInactive             = errors.New("the external identity is no longer active")
)
