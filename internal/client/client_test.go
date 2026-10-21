// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPathValidationAndEscaping(t *testing.T) {
	t.Parallel()

	p, err := Path("rbac_groups", "rbac_group_01AbC", "members", "user_01XyZ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/v1/organizations/rbac_groups/rbac_group_01AbC/members/user_01XyZ"; p != want {
		t.Fatalf("got %q, want %q", p, want)
	}

	// Reject traversal, slashes, and empty segments
	badInputs := [][]string{
		{""},
		{"rbac_groups", ""},
		{"rbac_groups", ".."},
		{"rbac_groups", "."},
		{"rbac_groups", "../users/user_victim"},
		{"rbac_groups", "group_1/../../users"},
		{"rbac_groups", `group_1\..\users`},
	}
	for _, segs := range badInputs {
		if got, err := Path(segs...); err == nil {
			t.Errorf("Path(%v) = %q, expected error for unsafe segment", segs, got)
		}
	}

	// Query characters inside a segment are percent-encoded so they cannot inject query params
	escaped, err := Path("rbac_groups", "group?admin=true#frag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/v1/organizations/rbac_groups/group%3Fadmin=true%23frag"; escaped != want {
		t.Fatalf("got %q, want %q", escaped, want)
	}
}

func TestAllowedInferenceGeosJSON(t *testing.T) {
	t.Parallel()

	// 1. String "unrestricted"
	var ws1 Workspace
	if err := json.Unmarshal([]byte(`{"id":"wrkspc_1","data_residency":{"allowed_inference_geos":"unrestricted"}}`), &ws1); err != nil {
		t.Fatalf("unmarshal unrestricted failed: %v", err)
	}
	if ws1.DataResidency == nil || ws1.DataResidency.AllowedInferenceGeos == nil || !ws1.DataResidency.AllowedInferenceGeos.Unrestricted {
		t.Fatalf("expected Unrestricted=true, got %+v", ws1.DataResidency)
	}
	b1, err := json.Marshal(ws1.DataResidency.AllowedInferenceGeos)
	if err != nil || string(b1) != `"unrestricted"` {
		t.Fatalf("marshal unrestricted got %s, err %v", string(b1), err)
	}

	// 2. Slice ["us", "global"]
	var ws2 Workspace
	if err := json.Unmarshal([]byte(`{"id":"wrkspc_2","data_residency":{"allowed_inference_geos":["us","global"]}}`), &ws2); err != nil {
		t.Fatalf("unmarshal slice failed: %v", err)
	}
	if ws2.DataResidency == nil || ws2.DataResidency.AllowedInferenceGeos == nil || ws2.DataResidency.AllowedInferenceGeos.Unrestricted {
		t.Fatalf("expected Unrestricted=false, got %+v", ws2.DataResidency)
	}
	if len(ws2.DataResidency.AllowedInferenceGeos.Geos) != 2 || ws2.DataResidency.AllowedInferenceGeos.Geos[0] != "us" {
		t.Fatalf("unexpected geos: %+v", ws2.DataResidency.AllowedInferenceGeos.Geos)
	}
}

func TestDualCursorPaginationAndCycleGuard(t *testing.T) {
	t.Parallel()

	// 1. Opaque next_page pagination (?page=...)
	var pageCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pageCalls, 1)
		if r.Header.Get("anthropic-beta") != BetaCEUserManagement {
			t.Errorf("missing anthropic-beta header, got %q", r.Header.Get("anthropic-beta"))
		}
		if r.URL.Query().Get("beta") != "true" {
			t.Errorf("missing beta=true query param, got %q", r.URL.RawQuery)
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"id":"rbac_role_1","name":"Role 1"}],"has_more":true,"next_page":"tok_2"}`))
		case "tok_2":
			_, _ = w.Write([]byte(`{"data":[{"id":"rbac_role_2","name":"Role 2"}],"has_more":false,"next_page":null}`))
		default:
			t.Fatalf("unexpected page token %q", r.URL.Query().Get("page"))
		}
	}))
	defer srv.Close()

	c, err := New(Config{APIKey: "sk-ant-admin-test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	roles, err := c.ListRBACRoles(context.Background())
	if err != nil {
		t.Fatalf("ListRBACRoles error: %v", err)
	}
	if len(roles) != 2 || roles[0].ID != "rbac_role_1" || roles[1].ID != "rbac_role_2" {
		t.Fatalf("unexpected roles: %+v", roles)
	}

	// 2. Cycle detection on repeating cursor
	loopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"user_1"}],"has_more":true,"last_id":"user_1"}`))
	}))
	defer loopSrv.Close()

	loopClient, err := New(Config{APIKey: "sk-ant-admin-test", BaseURL: loopSrv.URL})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if _, err := loopClient.ListUsers(context.Background(), ""); err == nil {
		t.Fatalf("expected error on repeating cursor loop, got nil")
	}
}

func TestRetryOn429And529(t *testing.T) {
	t.Parallel()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
			return
		}
		if n == 2 {
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"org_123","name":"Example Org","type":"organization"}`))
	}))
	defer srv.Close()

	c, err := New(Config{
		APIKey:      "sk-ant-admin-test",
		BaseURL:     srv.URL,
		MaxRetries:  3,
		BaseBackoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	org, err := c.GetOrganization(context.Background())
	if err != nil {
		t.Fatalf("GetOrganization error: %v", err)
	}
	if org.ID != "org_123" || atomic.LoadInt32(&attempts) != 3 {
		t.Fatalf("expected 3 attempts and org_123, got %d attempts and %+v", attempts, org)
	}

	// Verify POST requests do NOT retry on 500/529 (preventing duplicate creation),
	// but DO retry on 429 Too Many Requests.
	var post500Attempts int32
	post500Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&post500Attempts, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"internal error"}}`))
	}))
	defer post500Srv.Close()

	postClient, err := New(Config{
		APIKey:      "sk-ant-admin-test",
		BaseURL:     post500Srv.URL,
		MaxRetries:  3,
		BaseBackoff: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if _, err := postClient.CreateWorkspace(context.Background(), CreateWorkspaceRequest{Name: "test"}); err == nil {
		t.Fatalf("expected error on 500 POST")
	}
	if got := atomic.LoadInt32(&post500Attempts); got != 1 {
		t.Fatalf("expected POST on 500 not to be retried (1 attempt), got %d", got)
	}
}

func TestRBACGroupDualRolesFieldAndMemberCache(t *testing.T) {
	t.Parallel()

	var memberListCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/organizations/rbac_groups/rbac_group_1":
			// Returns "roles" (documented REST field) without "role_ids"
			_, _ = w.Write([]byte(`{"id":"rbac_group_1","name":"Eng","source_type":"custom","roles":["rbac_role_a"],"type":"rbac_group"}`))
		case "/v1/organizations/rbac_groups/rbac_group_1/members":
			atomic.AddInt32(&memberListCalls, 1)
			_, _ = w.Write([]byte(`{"data":[{"user_id":"user_1","group_id":"rbac_group_1","email":"a@example.com","name":"Alice"}],"has_more":false}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := New(Config{
		APIKey:         "sk-ant-admin-test",
		BaseURL:        srv.URL,
		MemberCacheTTL: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	g, err := c.GetRBACGroup(context.Background(), "rbac_group_1")
	if err != nil {
		t.Fatalf("GetRBACGroup error: %v", err)
	}
	roles, degraded := g.EffectiveRoleIDs()
	if degraded || len(roles) != 1 || roles[0] != "rbac_role_a" {
		t.Fatalf("expected roles=[rbac_role_a], degraded=false; got roles=%v, degraded=%v", roles, degraded)
	}

	// Multiple FindRBACGroupMember calls hit the cache
	for i := 0; i < 5; i++ {
		m, err := c.FindRBACGroupMember(context.Background(), "rbac_group_1", "user_1")
		if err != nil || m == nil || m.EffectiveGroupID("") != "rbac_group_1" {
			t.Fatalf("FindRBACGroupMember failed: m=%+v, err=%v", m, err)
		}
	}
	if got := atomic.LoadInt32(&memberListCalls); got != 1 {
		t.Fatalf("expected 1 HTTP call due to member cache, got %d", got)
	}
}

func TestWIFTokenExchangeAndEndpoints(t *testing.T) {
	t.Parallel()

	var tokenExchangeCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// 1. OAuth token exchange endpoint called by anthropic-sdk-go WithFederationTokenProvider
		if r.Method == http.MethodPost && r.URL.Path == "/v1/oauth/token" {
			atomic.AddInt32(&tokenExchangeCalls, 1)
			var body struct {
				Assertion        string `json:"assertion"`
				FederationRuleID string `json:"federation_rule_id"`
				OrganizationID   string `json:"organization_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("failed to decode token exchange body: %v", err)
			}
			if body.Assertion != "oidc-jwt-token-123" {
				t.Errorf("expected assertion=oidc-jwt-token-123, got %q", body.Assertion)
			}
			if body.FederationRuleID != "fdrl_01JTEST" {
				t.Errorf("expected federation_rule_id=fdrl_01JTEST, got %q", body.FederationRuleID)
			}
			if body.OrganizationID != "org_01JTEST" {
				t.Errorf("expected organization_id=org_01JTEST, got %q", body.OrganizationID)
			}
			_, _ = w.Write([]byte(`{"access_token":"short-lived-wif-bearer","token_type":"Bearer","expires_in":3600}`))
			return
		}

		// 2. Verify WIF-exchanged bearer token is sent on Admin API requests without x-api-key
		if got := r.Header.Get("Authorization"); got != "Bearer short-lived-wif-bearer" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"missing or invalid Authorization bearer header"}}`))
			return
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("expected empty x-api-key header in federation mode, got %q", got)
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/service_accounts":
			_, _ = w.Write([]byte(`{"id":"svac_01","name":"ci-runner","organization_role":"developer","description":"CI service account","created_at":"2026-02-01T00:00:00Z","type":"service_account"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/service_accounts/svac_01/workspaces":
			_, _ = w.Write([]byte(`{"service_account_id":"svac_01","workspace_id":"wrkspc_01","workspace_role":"workspace_developer","implicit":false,"type":"service_account_workspace_member"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/federation_issuers":
			_, _ = w.Write([]byte(`{"id":"fdis_01","name":"github-actions","issuer_url":"https://token.actions.githubusercontent.com","jwks":{"type":"discovery"},"created_at":"2026-02-01T00:00:00Z","updated_at":"2026-02-01T00:00:00Z","type":"federation_issuer"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/organizations/federation_rules":
			_, _ = w.Write([]byte(`{"id":"fdrl_01","name":"github-main","issuer_id":"fdis_01","target":{"type":"service_account","service_account_id":"svac_01"},"oauth_scope":"workspace:developer","token_lifetime_seconds":3600,"applies_to_all_workspaces":true,"workspace_ids":[],"match":{"subject_prefix":"repo:example/app:ref:refs/heads/main"},"created_at":"2026-02-01T00:00:00Z","updated_at":"2026-02-01T00:00:00Z","type":"federation_rule"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// Static APIKey client must reject WIF endpoints before making network calls
	staticClient, err := New(Config{
		APIKey:  "sk-ant-admin-static",
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("New staticClient error: %v", err)
	}
	if _, err := staticClient.ListServiceAccounts(context.Background(), false); err == nil {
		t.Fatalf("expected static API key client to fail on WIF/ServiceAccount endpoint")
	}

	// Federation client exchanges OIDC token via SDK and calls WIF endpoints
	wifClient, err := New(Config{
		BaseURL: srv.URL,
		Federation: &FederationConfig{
			FederationRuleID: "fdrl_01JTEST",
			OrganizationID:   "org_01JTEST",
			IdentityToken:    "oidc-jwt-token-123",
		},
	})
	if err != nil {
		t.Fatalf("New wifClient error: %v", err)
	}

	desc := "CI service account"
	sa, err := wifClient.CreateServiceAccount(context.Background(), CreateServiceAccountRequest{
		Name:             "ci-runner",
		OrganizationRole: "developer",
		Description:      &desc,
	})
	if err != nil || sa.ID != "svac_01" {
		t.Fatalf("CreateServiceAccount failed: sa=%+v, err=%v", sa, err)
	}

	saw, err := wifClient.AddServiceAccountToWorkspace(context.Background(), sa.ID, AddServiceAccountToWorkspaceRequest{
		WorkspaceID:   "wrkspc_01",
		WorkspaceRole: "workspace_developer",
	})
	if err != nil || saw.WorkspaceID != "wrkspc_01" {
		t.Fatalf("AddServiceAccountToWorkspace failed: saw=%+v, err=%v", saw, err)
	}

	iss, err := wifClient.CreateFederationIssuer(context.Background(), CreateFederationIssuerRequest{
		Name:      "github-actions",
		IssuerURL: "https://token.actions.githubusercontent.com",
		JWKS: FederationIssuerJWKS{
			Type: "discovery",
		},
	})
	if err != nil || iss.ID != "fdis_01" {
		t.Fatalf("CreateFederationIssuer failed: iss=%+v, err=%v", iss, err)
	}

	subj := "repo:example/app:ref:refs/heads/main"
	rule, err := wifClient.CreateFederationRule(context.Background(), CreateFederationRuleRequest{
		Name:                   "github-main",
		IssuerID:               iss.ID,
		Target:                 ServiceAccountTarget(sa.ID),
		OAuthScope:             "workspace:developer",
		AppliesToAllWorkspaces: true,
		Match: FederationRuleMatch{
			SubjectPrefix: &subj,
		},
	})
	if err != nil || rule.ID != "fdrl_01" {
		t.Fatalf("CreateFederationRule failed: rule=%+v, err=%v", rule, err)
	}

	if got := atomic.LoadInt32(&tokenExchangeCalls); got != 1 {
		t.Fatalf("expected 1 OAuth token exchange call (cached across subsequent requests), got %d", got)
	}
}

func TestComputeBackoffCapsRetryAfter(t *testing.T) {
	c := &Client{baseBackoff: time.Millisecond}
	for _, v := range []string{"3600", time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)} {
		if got := c.computeBackoff(0, http.Header{"Retry-After": {v}}); got > MaxRetryAfter {
			t.Errorf("Retry-After %q: backoff %v exceeds cap %v", v, got, MaxRetryAfter)
		}
	}
}
