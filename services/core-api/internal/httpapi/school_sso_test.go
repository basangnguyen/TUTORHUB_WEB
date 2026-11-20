package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSchoolSSOAdminRoutesAreOffByDefault(t *testing.T) {
	handler := NewHandlerWithOptions(authFoundationConfig(), nil, Options{})
	const prefix = "/api/v1/tenants/0e6de15c-cb56-49ae-9899-b7f1b5f7259e/"
	for _, route := range []struct{ method, path string }{
		{"GET", "sso-connections"}, {"POST", "sso-connections"}, {"PATCH", "sso-connections/any"},
		{"GET", "sso-domains"}, {"POST", "sso-domains"}, {"POST", "sso-domains/any/rotate"},
		{"POST", "sso-domains/any/verify"}, {"POST", "sso-domains/any/disable"},
	} {
		r := httptest.NewRequest(route.method, prefix+route.path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s returned %d, Cache-Control %q", route.method, route.path, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}

func TestSchoolSSOPatchNullableRole(t *testing.T) {
	for _, tc := range []struct {
		body           string
		valid, touched bool
	}{
		{`{"status":"disabled"}`, true, false},
		{`{"auto_join_role":null}`, true, true},
		{`{"auto_join_role":"student"}`, true, true},
		{`{"enforce_sso":null}`, false, false},
		{`{"allowed_org_ids":null}`, false, false},
		{`{"status":"enabled","STATUS":"disabled"}`, false, false},
		{`{"tenant_id":"foreign"}`, false, false},
		{"{}", false, false},
	} {
		req := httptest.NewRequest("PATCH", "/", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		patch, err := decodeSSOConnectionPatch(httptest.NewRecorder(), req)
		if (err == nil) != tc.valid || (tc.valid && patch.SetAutoJoinRole != tc.touched) {
			t.Errorf("%s gave %+v, %v", tc.body, patch, err)
		}
	}
}
