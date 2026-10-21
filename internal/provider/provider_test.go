// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"anthropic": providerserver.NewProtocol6WithError(New("test")()),
}

// mockAnthropicServer provides an in-memory mock of both Claude Console and Claude Enterprise
// Organization Admin APIs for deterministic provider unit/acceptance tests.
type mockAnthropicServer struct {
	mu                       sync.Mutex
	server                   *httptest.Server
	org                      client.Organization
	users                    map[string]client.User
	invites                  map[string]client.Invite
	workspaces               map[string]client.Workspace
	workspaceMember          map[string]map[string]client.WorkspaceMember
	apiKeys                  map[string]client.APIKey
	rbacRoles                []client.RBACRole
	rbacGroups               map[string]client.RBACGroup
	rbacMembers              map[string]map[string]client.RBACGroupMember
	spendLimits              map[string]client.SpendLimit
	serviceAccounts          map[string]client.ServiceAccount
	serviceAccountWorkspaces map[string]map[string]client.ServiceAccountWorkspaceMember
	federationIssuers        map[string]client.FederationIssuer
	federationRules          map[string]client.FederationRule

	// DegradedReadGroups simulates a backend degradation where GET /v1/organizations/rbac_groups/{id}
	// returns {"role_ids": null, "roles": null}.
	degradedReadGroups map[string]bool
	lastUpdateRawBody  map[string]any
}

func newMockAnthropicServer(t *testing.T) *mockAnthropicServer {
	t.Helper()

	descAdmin := "Full administrative access"
	descMember := "Standard organization member"
	roleList := []string{"role_member"}

	m := &mockAnthropicServer{
		org: client.Organization{
			ID:   "org_01JTESTORG",
			Name: "Example Org",
			Type: "organization",
		},
		users: map[string]client.User{
			"user_01JEXISTING": {
				ID:      "user_01JEXISTING",
				Email:   "alice@example.com",
				Name:    "Alice",
				Role:    "user",
				AddedAt: "2026-01-01T00:00:00Z",
				Type:    "user",
			},
		},
		invites:         make(map[string]client.Invite),
		workspaces:      make(map[string]client.Workspace),
		workspaceMember: make(map[string]map[string]client.WorkspaceMember),
		apiKeys: map[string]client.APIKey{
			"apikey_01JEXISTING": {
				ID:             "apikey_01JEXISTING",
				Name:           "prod-service-key",
				Status:         "active",
				PartialKeyHint: "sk-ant-api03-xyz...123",
				CreatedAt:      "2026-01-01T00:00:00Z",
				CreatedBy: &client.ActorRef{
					ID:   "user_01JEXISTING",
					Type: "user",
				},
				Type: "api_key",
			},
		},
		rbacRoles: []client.RBACRole{
			{
				ID:          "role_admin",
				Name:        "Admin",
				Description: &descAdmin,
				CreatedAt:   "2026-01-01T00:00:00Z",
				UpdatedAt:   "2026-01-01T00:00:00Z",
				Type:        "rbac_role",
			},
			{
				ID:          "role_member",
				Name:        "Member",
				Description: &descMember,
				CreatedAt:   "2026-01-01T00:00:00Z",
				UpdatedAt:   "2026-01-01T00:00:00Z",
				Type:        "rbac_role",
			},
		},
		rbacGroups: map[string]client.RBACGroup{
			"grp_scim_01": {
				ID:         "grp_scim_01",
				Name:       "Okta-Engineering",
				SourceType: "scim",
				RoleIDs:    &roleList,
				CreatedAt:  "2026-01-01T00:00:00Z",
				UpdatedAt:  "2026-01-01T00:00:00Z",
				Type:       "rbac_group",
			},
		},
		rbacMembers:              make(map[string]map[string]client.RBACGroupMember),
		spendLimits:              make(map[string]client.SpendLimit),
		serviceAccounts:          make(map[string]client.ServiceAccount),
		serviceAccountWorkspaces: make(map[string]map[string]client.ServiceAccountWorkspaceMember),
		federationIssuers:        make(map[string]client.FederationIssuer),
		federationRules:          make(map[string]client.FederationRule),
		degradedReadGroups:       make(map[string]bool),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/oauth/token", m.handleOAuthToken)
	mux.HandleFunc("/v1/organizations/", m.handleRequest)
	m.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		m.server.Close()
	})
	return m
}

func (m *mockAnthropicServer) providerConfig() string {
	return fmt.Sprintf(`
provider "anthropic" {
  api_key     = "sk-ant-admin01-test-key"
  base_url    = %q
  max_retries = 1
}
`, m.server.URL)
}

func (m *mockAnthropicServer) wifProviderConfig() string {
	return fmt.Sprintf(`
provider "anthropic" {
  base_url    = %q
  max_retries = 1
  federation = {
    federation_rule_id = "fdrl_bootstrap_01"
    organization_id    = "org_01JTESTORG"
    identity_token     = "oidc-jwt-token-test"
  }
}
`, m.server.URL)
}

func (m *mockAnthropicServer) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	var body struct {
		Assertion        string `json:"assertion"`
		FederationRuleID string `json:"federation_rule_id"`
		OrganizationID   string `json:"organization_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		body.Assertion == "" || body.FederationRuleID == "" || body.OrganizationID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "missing assertion, federation_rule_id or organization_id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": "wif-exchanged-oauth-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": message,
		},
	})
}

func (m *mockAnthropicServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	hasAPIKey := r.Header.Get("x-api-key") != ""
	hasBearer := strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
	if (!hasAPIKey && !hasBearer) || r.Header.Get("anthropic-version") == "" {
		writeAPIError(w, http.StatusUnauthorized, "authentication_error", "missing authentication or anthropic-version header")
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/v1/organizations/")
	segments := strings.Split(path, "/")

	switch segments[0] {
	case "me":
		if r.Method == http.MethodGet && len(segments) == 1 {
			writeJSON(w, http.StatusOK, m.org)
			return
		}

	case "users":
		m.handleUsers(w, r, segments[1:])
		return

	case "invites":
		m.handleInvites(w, r, segments[1:])
		return

	case "workspaces":
		m.handleWorkspaces(w, r, segments[1:])
		return

	case "api_keys":
		m.handleAPIKeys(w, r, segments[1:])
		return

	case "rbac_roles":
		if r.Header.Get("anthropic-beta") != client.BetaCEUserManagement || r.URL.Query().Get("beta") != "true" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "missing beta header or beta=true query param for rbac_roles")
			return
		}
		if r.Method == http.MethodGet && len(segments) == 1 {
			writeJSON(w, http.StatusOK, client.Page[client.RBACRole]{
				Data:    m.rbacRoles,
				HasMore: false,
			})
			return
		}

	case "rbac_groups":
		if r.Header.Get("anthropic-beta") != client.BetaCEUserManagement || r.URL.Query().Get("beta") != "true" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "missing beta header or beta=true query param for rbac_groups")
			return
		}
		m.handleRBACGroups(w, r, segments[1:])
		return

	case "spend_limits":
		if r.Header.Get("anthropic-beta") != client.BetaSpendLimitReads || r.URL.Query().Get("beta") != "true" {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "missing beta header or beta=true query param for spend_limits")
			return
		}
		m.handleSpendLimits(w, r, segments[1:])
		return

	case "service_accounts":
		if !hasBearer {
			writeAPIError(w, http.StatusUnauthorized, "authentication_error", "service_accounts requires Bearer token")
			return
		}
		m.handleServiceAccounts(w, r, segments[1:])
		return

	case "federation_issuers":
		if !hasBearer {
			writeAPIError(w, http.StatusUnauthorized, "authentication_error", "federation_issuers requires Bearer token")
			return
		}
		m.handleFederationIssuers(w, r, segments[1:])
		return

	case "federation_rules":
		if !hasBearer {
			writeAPIError(w, http.StatusUnauthorized, "authentication_error", "federation_rules requires Bearer token")
			return
		}
		m.handleFederationRules(w, r, segments[1:])
		return
	}

	writeAPIError(w, http.StatusNotFound, "not_found_error", fmt.Sprintf("unhandled route: %s %s", r.Method, r.URL.Path))
}

func (m *mockAnthropicServer) handleUsers(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		if r.Method == http.MethodGet {
			emailFilter := r.URL.Query().Get("email")
			var list []client.User
			for _, u := range m.users {
				if emailFilter == "" || strings.EqualFold(u.Email, emailFilter) {
					list = append(list, u)
				}
			}
			writeJSON(w, http.StatusOK, client.Page[client.User]{Data: list, HasMore: false})
			return
		}
	}
	if len(sub) == 1 {
		id := sub[0]
		u, ok := m.users[id]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "user not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, u)
		case http.MethodPost:
			var req client.UpdateUserRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			u.Role = req.Role
			m.users[id] = u
			writeJSON(w, http.StatusOK, u)
		case http.MethodDelete:
			delete(m.users, id)
			writeJSON(w, http.StatusOK, client.DeleteResponse{ID: id, Type: "user_deleted"})
		}
		return
	}
	writeAPIError(w, http.StatusNotFound, "not_found_error", "unknown users route")
}

func (m *mockAnthropicServer) handleInvites(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		switch r.Method {
		case http.MethodGet:
			var list []client.Invite
			for _, inv := range m.invites {
				list = append(list, inv)
			}
			writeJSON(w, http.StatusOK, client.Page[client.Invite]{Data: list, HasMore: false})
		case http.MethodPost:
			var req client.CreateInviteRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			id := fmt.Sprintf("inv_%d", len(m.invites)+1)
			inv := client.Invite{
				ID:        id,
				Email:     req.Email,
				Role:      req.Role,
				Status:    "pending",
				InvitedAt: "2026-02-01T00:00:00Z",
				ExpiresAt: "2026-02-22T00:00:00Z",
				Type:      "invite",
			}
			m.invites[id] = inv
			writeJSON(w, http.StatusCreated, inv)
		}
		return
	}
	if len(sub) == 1 {
		id := sub[0]
		inv, ok := m.invites[id]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "invite not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, inv)
		case http.MethodDelete:
			delete(m.invites, id)
			writeJSON(w, http.StatusOK, client.DeleteResponse{ID: id, Type: "invite_deleted"})
		}
		return
	}
}

func (m *mockAnthropicServer) handleWorkspaces(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		switch r.Method {
		case http.MethodGet:
			var list []client.Workspace
			for _, ws := range m.workspaces {
				list = append(list, ws)
			}
			writeJSON(w, http.StatusOK, client.Page[client.Workspace]{Data: list, HasMore: false})
		case http.MethodPost:
			var req client.CreateWorkspaceRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			id := fmt.Sprintf("wrkspc_%d", len(m.workspaces)+1)
			dr := req.DataResidency
			if dr != nil && dr.WorkspaceGeo == "" {
				dr.WorkspaceGeo = "us"
			}
			ws := client.Workspace{
				ID:            id,
				Name:          req.Name,
				DisplayColor:  "#6B5CE7",
				CreatedAt:     "2026-02-01T00:00:00Z",
				DataResidency: dr,
				Type:          "workspace",
			}
			m.workspaces[id] = ws
			writeJSON(w, http.StatusCreated, ws)
		}
		return
	}

	wsID := sub[0]
	ws, ok := m.workspaces[wsID]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "workspace not found")
		return
	}

	if len(sub) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, ws)
		case http.MethodPost:
			var req client.UpdateWorkspaceRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			ws.Name = req.Name
			if req.DataResidency != nil {
				if ws.DataResidency == nil {
					ws.DataResidency = req.DataResidency
				} else {
					if req.DataResidency.DefaultInferenceGeo != "" {
						ws.DataResidency.DefaultInferenceGeo = req.DataResidency.DefaultInferenceGeo
					}
					if req.DataResidency.AllowedInferenceGeos != nil {
						ws.DataResidency.AllowedInferenceGeos = req.DataResidency.AllowedInferenceGeos
					}
				}
			}
			m.workspaces[wsID] = ws
			writeJSON(w, http.StatusOK, ws)
		}
		return
	}

	if len(sub) == 2 && sub[1] == "archive" && r.Method == http.MethodPost {
		archived := "2026-02-02T00:00:00Z"
		ws.ArchivedAt = &archived
		delete(m.workspaces, wsID)
		writeJSON(w, http.StatusOK, ws)
		return
	}

	if sub[1] == "members" {
		if m.workspaceMember[wsID] == nil {
			m.workspaceMember[wsID] = make(map[string]client.WorkspaceMember)
		}
		if len(sub) == 2 {
			switch r.Method {
			case http.MethodGet:
				var list []client.WorkspaceMember
				for _, mem := range m.workspaceMember[wsID] {
					list = append(list, mem)
				}
				writeJSON(w, http.StatusOK, client.Page[client.WorkspaceMember]{Data: list, HasMore: false})
			case http.MethodPost:
				var req client.CreateWorkspaceMemberRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				mem := client.WorkspaceMember{
					WorkspaceID:   wsID,
					UserID:        req.UserID,
					WorkspaceRole: req.WorkspaceRole,
					Type:          "workspace_member",
				}
				m.workspaceMember[wsID][req.UserID] = mem
				writeJSON(w, http.StatusCreated, mem)
			}
			return
		}
		if len(sub) == 3 {
			userID := sub[2]
			mem, exists := m.workspaceMember[wsID][userID]
			if !exists {
				writeAPIError(w, http.StatusNotFound, "not_found_error", "workspace member not found")
				return
			}
			switch r.Method {
			case http.MethodGet:
				writeJSON(w, http.StatusOK, mem)
			case http.MethodPost:
				var req client.UpdateWorkspaceMemberRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				mem.WorkspaceRole = req.WorkspaceRole
				m.workspaceMember[wsID][userID] = mem
				writeJSON(w, http.StatusOK, mem)
			case http.MethodDelete:
				delete(m.workspaceMember[wsID], userID)
				writeJSON(w, http.StatusOK, client.DeleteResponse{ID: userID, Type: "workspace_member_deleted"})
			}
			return
		}
	}

	if len(sub) == 3 && sub[1] == "service_accounts" && r.Method == http.MethodPost {
		saID := sub[2]
		if m.serviceAccountWorkspaces[saID] == nil {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "service account workspace membership not found")
			return
		}
		mem, exists := m.serviceAccountWorkspaces[saID][wsID]
		if !exists {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "service account workspace membership not found")
			return
		}
		var req client.UpdateServiceAccountWorkspaceRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		mem.WorkspaceRole = req.WorkspaceRole
		m.serviceAccountWorkspaces[saID][wsID] = mem
		writeJSON(w, http.StatusOK, mem)
		return
	}
}

func (m *mockAnthropicServer) handleAPIKeys(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 && r.Method == http.MethodGet {
		var list []client.APIKey
		for _, k := range m.apiKeys {
			list = append(list, k)
		}
		writeJSON(w, http.StatusOK, client.Page[client.APIKey]{Data: list, HasMore: false})
		return
	}
	if len(sub) == 1 {
		id := sub[0]
		key, ok := m.apiKeys[id]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "api key not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, key)
		case http.MethodPost:
			var req client.UpdateAPIKeyRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "" {
				key.Name = req.Name
			}
			if req.Status != "" {
				key.Status = req.Status
			}
			m.apiKeys[id] = key
			writeJSON(w, http.StatusOK, key)
		}
		return
	}
}

func (m *mockAnthropicServer) handleRBACGroups(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		switch r.Method {
		case http.MethodGet:
			var list []client.RBACGroup
			for _, g := range m.rbacGroups {
				list = append(list, g)
			}
			writeJSON(w, http.StatusOK, client.Page[client.RBACGroup]{Data: list, HasMore: false})
		case http.MethodPost:
			var req client.CreateRBACGroupRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			id := fmt.Sprintf("grp_api_%d", len(m.rbacGroups)+1)
			roles := []string{}
			g := client.RBACGroup{
				ID:         id,
				Name:       req.Name,
				SourceType: "api",
				RoleIDs:    &roles,
				CreatedAt:  "2026-02-01T00:00:00Z",
				UpdatedAt:  "2026-02-01T00:00:00Z",
				Type:       "rbac_group",
			}
			m.rbacGroups[id] = g
			writeJSON(w, http.StatusCreated, g)
		}
		return
	}

	grpID := sub[0]
	g, ok := m.rbacGroups[grpID]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "rbac group not found")
		return
	}

	if len(sub) == 1 {
		switch r.Method {
		case http.MethodGet:
			if m.degradedReadGroups[grpID] {
				degraded := g
				degraded.RoleIDs = nil
				degraded.Roles = nil
				writeJSON(w, http.StatusOK, degraded)
				return
			}
			writeJSON(w, http.StatusOK, g)

		case http.MethodPost:
			rawBytes, _ := io.ReadAll(r.Body)
			var rawMap map[string]any
			_ = json.Unmarshal(rawBytes, &rawMap)
			m.lastUpdateRawBody = rawMap

			if g.SourceType == "scim" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "cannot update name on SCIM-managed group")
				return
			}

			var req client.UpdateRBACGroupRequest
			_ = json.Unmarshal(rawBytes, &req)
			if req.Name != "" {
				g.Name = req.Name
			}
			g.UpdatedAt = "2026-02-02T00:00:00Z"
			m.rbacGroups[grpID] = g
			writeJSON(w, http.StatusOK, g)

		case http.MethodDelete:
			if g.SourceType == "scim" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "cannot delete SCIM-managed group")
				return
			}
			delete(m.rbacGroups, grpID)
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}

	if sub[1] == "members" {
		if m.rbacMembers[grpID] == nil {
			m.rbacMembers[grpID] = make(map[string]client.RBACGroupMember)
		}
		if len(sub) == 2 {
			switch r.Method {
			case http.MethodGet:
				var list []client.RBACGroupMember
				for _, mem := range m.rbacMembers[grpID] {
					list = append(list, mem)
				}
				writeJSON(w, http.StatusOK, client.Page[client.RBACGroupMember]{Data: list, HasMore: false})
			case http.MethodPost:
				if g.SourceType == "scim" {
					writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "cannot modify members of SCIM group")
					return
				}
				var req client.AddRBACGroupMemberRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				u := m.users[req.UserID]
				mem := client.RBACGroupMember{
					UserID:    req.UserID,
					GroupID:   grpID,
					Email:     u.Email,
					Name:      u.Name,
					CreatedAt: "2026-02-01T00:00:00Z",
					Type:      "rbac_group_member",
				}
				m.rbacMembers[grpID][req.UserID] = mem
				writeJSON(w, http.StatusCreated, mem)
			}
			return
		}
		if len(sub) == 3 && r.Method == http.MethodDelete {
			userID := sub[2]
			if _, exists := m.rbacMembers[grpID][userID]; !exists {
				writeAPIError(w, http.StatusNotFound, "not_found_error", "member not found")
				return
			}
			delete(m.rbacMembers[grpID], userID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
}

func (m *mockAnthropicServer) handleSpendLimits(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 && r.Method == http.MethodPost {
		var req client.UpsertSpendLimitRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Upsert semantics: reuse existing ID for same (user_id, period) if present.
		var id string
		for k, existing := range m.spendLimits {
			if existing.Scope.UserID == req.UserID && existing.Period == req.Period {
				id = k
				break
			}
		}
		if id == "" {
			id = fmt.Sprintf("spl_%d", len(m.spendLimits)+1)
		}

		sl := client.SpendLimit{
			ID:       id,
			Amount:   req.Amount,
			Currency: "USD",
			Period:   req.Period,
			Scope: client.SpendLimitScope{
				Type:   "user",
				UserID: req.UserID,
			},
			CreatedAt: "2026-02-01T00:00:00Z",
			UpdatedAt: "2026-02-01T00:00:00Z",
			Type:      "spend_limit",
		}
		m.spendLimits[id] = sl
		writeJSON(w, http.StatusOK, sl)
		return
	}

	if len(sub) == 1 {
		id := sub[0]
		sl, ok := m.spendLimits[id]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "spend limit not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, sl)
		case http.MethodDelete:
			delete(m.spendLimits, id)
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}
}

func (m *mockAnthropicServer) handleServiceAccounts(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		switch r.Method {
		case http.MethodGet:
			var list []client.ServiceAccount
			for _, sa := range m.serviceAccounts {
				list = append(list, sa)
			}
			writeJSON(w, http.StatusOK, client.Page[client.ServiceAccount]{Data: list, HasMore: false})
		case http.MethodPost:
			var req client.CreateServiceAccountRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			id := fmt.Sprintf("svac_%d", len(m.serviceAccounts)+1)
			sa := client.ServiceAccount{
				ID:               id,
				Name:             req.Name,
				OrganizationRole: req.OrganizationRole,
				Description:      req.Description,
				CreatedAt:        "2026-02-01T00:00:00Z",
				Type:             "service_account",
			}
			m.serviceAccounts[id] = sa
			writeJSON(w, http.StatusCreated, sa)
		}
		return
	}

	id := sub[0]
	sa, ok := m.serviceAccounts[id]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "service account not found")
		return
	}

	if len(sub) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, sa)
		case http.MethodPost:
			var req client.UpdateServiceAccountRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.OrganizationRole != "" {
				sa.OrganizationRole = req.OrganizationRole
			}
			if req.Description != nil {
				if *req.Description == "" {
					sa.Description = nil
				} else {
					sa.Description = req.Description
				}
			} else {
				sa.Description = nil
			}
			m.serviceAccounts[id] = sa
			writeJSON(w, http.StatusOK, sa)
		}
		return
	}

	if len(sub) == 2 && sub[1] == "archive" && r.Method == http.MethodPost {
		archived := "2026-02-02T00:00:00Z"
		sa.ArchivedAt = &archived
		delete(m.serviceAccounts, id)
		writeJSON(w, http.StatusOK, sa)
		return
	}

	if sub[1] == "workspaces" {
		if m.serviceAccountWorkspaces[id] == nil {
			m.serviceAccountWorkspaces[id] = make(map[string]client.ServiceAccountWorkspaceMember)
		}
		if len(sub) == 2 {
			switch r.Method {
			case http.MethodGet:
				var list []client.ServiceAccountWorkspaceMember
				for _, mem := range m.serviceAccountWorkspaces[id] {
					list = append(list, mem)
				}
				writeJSON(w, http.StatusOK, client.Page[client.ServiceAccountWorkspaceMember]{Data: list, HasMore: false})
			case http.MethodPost:
				var req client.AddServiceAccountToWorkspaceRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				actorID := "user_01JEXISTING"
				mem := client.ServiceAccountWorkspaceMember{
					ServiceAccountID: id,
					WorkspaceID:      req.WorkspaceID,
					WorkspaceRole:    req.WorkspaceRole,
					CreatedByActorID: &actorID,
					Implicit:         false,
					Type:             "service_account_workspace_member",
				}
				m.serviceAccountWorkspaces[id][req.WorkspaceID] = mem
				writeJSON(w, http.StatusOK, mem)
			}
			return
		}
		if len(sub) == 3 && r.Method == http.MethodDelete {
			wsID := sub[2]
			if _, exists := m.serviceAccountWorkspaces[id][wsID]; !exists {
				writeAPIError(w, http.StatusNotFound, "not_found_error", "service account workspace membership not found")
				return
			}
			delete(m.serviceAccountWorkspaces[id], wsID)
			w.WriteHeader(http.StatusOK)
			return
		}
	}
}

func (m *mockAnthropicServer) handleFederationIssuers(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		switch r.Method {
		case http.MethodGet:
			var list []client.FederationIssuer
			for _, iss := range m.federationIssuers {
				list = append(list, iss)
			}
			writeJSON(w, http.StatusOK, client.Page[client.FederationIssuer]{Data: list, HasMore: false})
		case http.MethodPost:
			var req client.CreateFederationIssuerRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			id := fmt.Sprintf("fdis_%d", len(m.federationIssuers)+1)
			checkJTI := false
			if req.CheckJTI != nil {
				checkJTI = *req.CheckJTI
			}
			maxLifetime := int64(3600)
			if req.MaxJWTLifetimeSeconds != nil {
				maxLifetime = *req.MaxJWTLifetimeSeconds
			}
			iss := client.FederationIssuer{
				ID:                    id,
				Name:                  req.Name,
				IssuerURL:             req.IssuerURL,
				CheckJTI:              &checkJTI,
				MaxJWTLifetimeSeconds: &maxLifetime,
				JWKS:                  req.JWKS,
				CreatedAt:             "2026-02-01T00:00:00Z",
				UpdatedAt:             "2026-02-01T00:00:00Z",
				Type:                  "federation_issuer",
			}
			m.federationIssuers[id] = iss
			writeJSON(w, http.StatusCreated, iss)
		}
		return
	}

	id := sub[0]
	iss, ok := m.federationIssuers[id]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "federation issuer not found")
		return
	}

	if len(sub) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, iss)
		case http.MethodPost:
			var req client.UpdateFederationIssuerRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "" {
				iss.Name = req.Name
			}
			if req.IssuerURL != "" {
				iss.IssuerURL = req.IssuerURL
			}
			if req.CheckJTI != nil {
				iss.CheckJTI = req.CheckJTI
			}
			if req.MaxJWTLifetimeSeconds != nil {
				iss.MaxJWTLifetimeSeconds = req.MaxJWTLifetimeSeconds
			}
			if req.JWKS != nil {
				iss.JWKS = *req.JWKS
			}
			iss.UpdatedAt = "2026-02-02T00:00:00Z"
			m.federationIssuers[id] = iss
			writeJSON(w, http.StatusOK, iss)
		}
		return
	}

	if len(sub) == 2 && sub[1] == "archive" && r.Method == http.MethodPost {
		archived := "2026-02-02T00:00:00Z"
		iss.ArchivedAt = &archived
		delete(m.federationIssuers, id)
		writeJSON(w, http.StatusOK, iss)
		return
	}
}

func (m *mockAnthropicServer) handleFederationRules(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		switch r.Method {
		case http.MethodGet:
			var list []client.FederationRule
			for _, rule := range m.federationRules {
				list = append(list, rule)
			}
			writeJSON(w, http.StatusOK, client.Page[client.FederationRule]{Data: list, HasMore: false})
		case http.MethodPost:
			var req client.CreateFederationRuleRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			id := fmt.Sprintf("fdrl_%d", len(m.federationRules)+1)
			lifetime := int64(3600)
			if req.TokenLifetimeSeconds != nil {
				lifetime = *req.TokenLifetimeSeconds
			}
			if req.Target.Type != "service_account" || req.Target.ServiceAccountID == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "target.type must be service_account with service_account_id")
				return
			}
			if !req.AppliesToAllWorkspaces && req.WorkspaceID == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "rule must be enabled for at least one workspace")
				return
			}
			wsIDs := []string{}
			if req.WorkspaceID != "" {
				wsIDs = append(wsIDs, req.WorkspaceID)
			}
			description := ""
			if req.Description != nil {
				description = *req.Description
			}
			rule := client.FederationRule{
				ID:                     id,
				Name:                   req.Name,
				Description:            description,
				IssuerID:               req.IssuerID,
				Target:                 req.Target,
				OAuthScope:             req.OAuthScope,
				TokenLifetimeSeconds:   lifetime,
				AppliesToAllWorkspaces: req.AppliesToAllWorkspaces,
				WorkspaceIDs:           wsIDs,
				Match:                  req.Match,
				CreatedAt:              "2026-02-01T00:00:00Z",
				UpdatedAt:              "2026-02-01T00:00:00Z",
				Type:                   "federation_rule",
			}
			m.federationRules[id] = rule
			writeJSON(w, http.StatusCreated, rule)
		}
		return
	}

	id := sub[0]
	rule, ok := m.federationRules[id]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "federation rule not found")
		return
	}

	if len(sub) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, rule)
		case http.MethodPost:
			var req client.UpdateFederationRuleRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Name != "" {
				rule.Name = req.Name
			}
			if req.Target != nil {
				rule.Target = *req.Target
			}
			rule.Description = ""
			if req.Description != nil {
				rule.Description = *req.Description
			}
			if req.OAuthScope != "" {
				rule.OAuthScope = req.OAuthScope
			}
			if req.TokenLifetimeSeconds != nil {
				rule.TokenLifetimeSeconds = *req.TokenLifetimeSeconds
			}
			if req.AppliesToAllWorkspaces != nil {
				if !*req.AppliesToAllWorkspaces && len(rule.WorkspaceIDs) == 0 {
					writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "no workspace would remain enabled")
					return
				}
				rule.AppliesToAllWorkspaces = *req.AppliesToAllWorkspaces
			}
			if req.Match != nil {
				rule.Match = *req.Match
			}
			rule.UpdatedAt = "2026-02-02T00:00:00Z"
			m.federationRules[id] = rule
			writeJSON(w, http.StatusOK, rule)
		}
		return
	}

	if len(sub) == 2 && sub[1] == "archive" && r.Method == http.MethodPost {
		archived := "2026-02-02T00:00:00Z"
		rule.ArchivedAt = &archived
		delete(m.federationRules, id)
		writeJSON(w, http.StatusOK, rule)
		return
	}

	if len(sub) == 2 && sub[1] == "workspaces" && r.Method == http.MethodPost {
		var req client.AddFederationRuleWorkspaceRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !slices.Contains(rule.WorkspaceIDs, req.WorkspaceID) {
			rule.WorkspaceIDs = append(rule.WorkspaceIDs, req.WorkspaceID)
		}
		m.federationRules[id] = rule
		writeJSON(w, http.StatusOK, map[string]any{"federation_rule_id": id, "workspace_id": req.WorkspaceID})
		return
	}

	if len(sub) == 3 && sub[1] == "workspaces" && r.Method == http.MethodDelete {
		if !rule.AppliesToAllWorkspaces && len(rule.WorkspaceIDs) == 1 && rule.WorkspaceIDs[0] == sub[2] {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "no workspace would remain enabled")
			return
		}
		rule.WorkspaceIDs = slices.DeleteFunc(rule.WorkspaceIDs, func(v string) bool { return v == sub[2] })
		m.federationRules[id] = rule
		writeJSON(w, http.StatusOK, map[string]any{"federation_rule_id": id, "workspace_id": sub[2]})
		return
	}
}

func TestConsoleResourcesAndDataSources(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
data "anthropic_organization" "current" {}

resource "anthropic_invite" "bob" {
  email = "bob@example.com"
  role  = "developer"
}

resource "anthropic_workspace" "eng" {
  name = "engineering-platform"
  data_residency = {
    default_inference_geo  = "us"
    allowed_inference_geos = ["unrestricted"]
  }
}

resource "anthropic_workspace_member" "alice_eng" {
  workspace_id   = anthropic_workspace.eng.id
  user_id        = "user_01JEXISTING"
  workspace_role = "workspace_developer"
}

data "anthropic_users" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.anthropic_organization.current", "id", "org_01JTESTORG"),
					resource.TestCheckResourceAttr("data.anthropic_organization.current", "name", "Example Org"),
					resource.TestCheckResourceAttr("anthropic_invite.bob", "email", "bob@example.com"),
					resource.TestCheckResourceAttr("anthropic_invite.bob", "role", "developer"),
					resource.TestCheckResourceAttr("anthropic_invite.bob", "status", "pending"),
					resource.TestCheckResourceAttr("anthropic_workspace.eng", "name", "engineering-platform"),
					resource.TestCheckResourceAttr("anthropic_workspace.eng", "data_residency.default_inference_geo", "us"),
					resource.TestCheckResourceAttr("anthropic_workspace.eng", "data_residency.allowed_inference_geos.#", "1"),
					resource.TestCheckResourceAttr("anthropic_workspace_member.alice_eng", "workspace_role", "workspace_developer"),
					resource.TestCheckResourceAttr("data.anthropic_users.all", "users.#", "1"),
				),
			},
			// Update workspace name, inference geos list, and workspace member role
			{
				Config: mock.providerConfig() + `
resource "anthropic_invite" "bob" {
  email = "bob@example.com"
  role  = "developer"
}

resource "anthropic_workspace" "eng" {
  name = "engineering-platform-v2"
  data_residency = {
    default_inference_geo  = "us"
    allowed_inference_geos = ["us", "global"]
  }
}

resource "anthropic_workspace_member" "alice_eng" {
  workspace_id   = anthropic_workspace.eng.id
  user_id        = "user_01JEXISTING"
  workspace_role = "workspace_admin"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_workspace.eng", "name", "engineering-platform-v2"),
					resource.TestCheckResourceAttr("anthropic_workspace.eng", "data_residency.allowed_inference_geos.#", "2"),
					resource.TestCheckResourceAttr("anthropic_workspace_member.alice_eng", "workspace_role", "workspace_admin"),
				),
			},
			// Import workspace_member by composite ID
			{
				ResourceName:      "anthropic_workspace_member.alice_eng",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestEnterpriseRBACAndSpendLimit(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
data "anthropic_rbac_roles" "all" {}

resource "anthropic_rbac_group" "security" {
  name = "Security-Team"
}

resource "anthropic_rbac_group_member" "alice_sec" {
  group_id = anthropic_rbac_group.security.id
  user_id  = "user_01JEXISTING"
}

resource "anthropic_spend_limit" "alice_cap" {
  user_id = "user_01JEXISTING"
  period  = "monthly"
  amount  = "25000"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.anthropic_rbac_roles.all", "roles.#", "2"),
					resource.TestCheckResourceAttr("anthropic_rbac_group.security", "name", "Security-Team"),
					resource.TestCheckResourceAttr("anthropic_rbac_group.security", "source_type", "api"),
					resource.TestCheckResourceAttr("anthropic_rbac_group.security", "role_ids.#", "0"),
					resource.TestCheckResourceAttr("anthropic_rbac_group_member.alice_sec", "email", "alice@example.com"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.alice_cap", "amount", "25000"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.alice_cap", "unlimited", "false"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.alice_cap", "currency", "USD"),
				),
			},
			// Rename custom RBAC group via POST /v1/organizations/rbac_groups/{id}?beta=true
			// and switch spend limit from capped ("25000") to explicit unlimited (unlimited = true -> "amount": null)
			{
				Config: mock.providerConfig() + `
resource "anthropic_rbac_group" "security" {
  name = "Security-Team-Renamed"
}

resource "anthropic_rbac_group_member" "alice_sec" {
  group_id = anthropic_rbac_group.security.id
  user_id  = "user_01JEXISTING"
}

resource "anthropic_spend_limit" "alice_cap" {
  user_id   = "user_01JEXISTING"
  period    = "monthly"
  unlimited = true
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_rbac_group.security", "name", "Security-Team-Renamed"),
					resource.TestCheckResourceAttr("anthropic_rbac_group.security", "role_ids.#", "0"),
					resource.TestCheckNoResourceAttr("anthropic_spend_limit.alice_cap", "amount"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.alice_cap", "unlimited", "true"),
				),
			},
			// Import RBAC group member via composite ID <group_id>/<user_id>
			{
				ResourceName:      "anthropic_rbac_group_member.alice_sec",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestEnterpriseSCIMGroupImportAndProtection(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Import existing SCIM group grp_scim_01 (which has read-only role_ids = ["role_member"])
			{
				Config: mock.providerConfig() + `
resource "anthropic_rbac_group" "okta_eng" {
  name = "Okta-Engineering"
}
`,
				ResourceName:       "anthropic_rbac_group.okta_eng",
				ImportState:        true,
				ImportStateId:      "grp_scim_01",
				ImportStatePersist: true,
			},
			// Step 2: Simulate degraded API read (role_ids: null) and verify prior role_ids state is preserved
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					mock.degradedReadGroups["grp_scim_01"] = true
				},
				Config: mock.providerConfig() + `
resource "anthropic_rbac_group" "okta_eng" {
  name = "Okta-Engineering"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_rbac_group.okta_eng", "source_type", "scim"),
					resource.TestCheckResourceAttr("anthropic_rbac_group.okta_eng", "role_ids.#", "1"),
				),
			},
			// Step 3: Attempting to rename a SCIM group returns a clear diagnostic error.
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					delete(mock.degradedReadGroups, "grp_scim_01")
				},
				Config: mock.providerConfig() + `
resource "anthropic_rbac_group" "okta_eng" {
  name = "Attempt-Rename-SCIM"
}
`,
				ExpectError: regexp.MustCompile(`Cannot Rename SCIM-Managed RBAC Group`),
			},
			// Convert source_type before test cleanup destroys the resource.
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					g := mock.rbacGroups["grp_scim_01"]
					g.SourceType = "api"
					mock.rbacGroups["grp_scim_01"] = g
				},
				Config: mock.providerConfig() + `
resource "anthropic_rbac_group" "okta_eng" {
  name = "Okta-Engineering"
}
`,
			},
		},
	})
}

func TestSpendLimitValidationGuardrails(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Reject when neither amount nor unlimited is specified
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "invalid_missing" {
  user_id = "user_01JEXISTING"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			// Reject when both amount and unlimited = true are specified
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "invalid_both" {
  user_id   = "user_01JEXISTING"
  amount    = "10000"
  unlimited = true
}
`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			// Reject decimal dollar string instead of integer cents
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "invalid_decimal" {
  user_id = "user_01JEXISTING"
  amount  = "100.50"
}
`,
				ExpectError: regexp.MustCompile(`minor\s+currency\s+units`),
			},
		},
	})
}

func TestWIFServiceAccountsIssuersAndRules(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Step 1: Reject WIF resources when provider is configured with static api_key
			{
				Config: mock.providerConfig() + `
resource "anthropic_service_account" "ci" {
  name              = "ci-runner"
  organization_role = "developer"
}
`,
				ExpectError: regexp.MustCompile(`static\s+Admin\s+API\s+keys\s+\(x-api-key\)\s+are\s+rejected`),
			},
			// Step 2: Reject unsafe wildcard in match.subject_prefix (e.g. "repo:moz*")
			{
				Config: mock.wifProviderConfig() + `
resource "anthropic_federation_rule" "unsafe_wildcard" {
  name               = "unsafe-rule"
  issuer_id          = "fdis_01"
  service_account_id = "svac_01"
  oauth_scope        = "workspace:developer"
  match = {
    subject_prefix = "repo:moz*"
  }
}
`,
				ExpectError: regexp.MustCompile(`Invalid Federation Rule Subject Prefix`),
			},
			// Step 3: Reject match block when neither subject_prefix nor condition is set
			{
				Config: mock.wifProviderConfig() + `
resource "anthropic_federation_rule" "missing_constraint" {
  name               = "empty-match-rule"
  issuer_id          = "fdis_01"
  service_account_id = "svac_01"
  oauth_scope        = "workspace:developer"
  match = {
    audience = "https://api.anthropic.com"
  }
}
`,
				ExpectError: regexp.MustCompile(`Missing Federation Rule Claim Constraint`),
			},
			// Step 4: Provision Workspace, Service Account, Service Account Workspace Membership,
			// Federation Issuer, and Federation Rule using WIF provider auth
			{
				Config: mock.wifProviderConfig() + `
resource "anthropic_workspace" "ci_ws" {
  name = "ci-workspace"
}

resource "anthropic_service_account" "ci" {
  name              = "ci-runner"
  organization_role = "user"
  description       = "GitHub Actions CI service account"
}

resource "anthropic_service_account_workspace" "ci_membership" {
  service_account_id = anthropic_service_account.ci.id
  workspace_id       = anthropic_workspace.ci_ws.id
  workspace_role     = "workspace_developer"
}

resource "anthropic_federation_issuer" "github" {
  name       = "github-actions"
  issuer_url = "https://token.actions.githubusercontent.com"
  jwks = {
    type = "discovery"
  }
}

resource "anthropic_federation_rule" "github_main" {
  name                      = "github-main-branch"
  issuer_id                 = anthropic_federation_issuer.github.id
  service_account_id        = anthropic_service_account.ci.id
  oauth_scope               = "workspace:developer"
  applies_to_all_workspaces = false
  workspace_ids             = [anthropic_workspace.ci_ws.id]
  match = {
    subject_prefix = "repo:example/app:ref:refs/heads/main"
    claims = {
      repository_owner = "example"
    }
  }
}

data "anthropic_service_accounts" "all" {
  depends_on = [anthropic_service_account.ci]
}

data "anthropic_federation_issuers" "all" {
  depends_on = [anthropic_federation_issuer.github]
}

data "anthropic_federation_rules" "all" {
  depends_on = [anthropic_federation_rule.github_main]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_service_account.ci", "name", "ci-runner"),
					resource.TestCheckResourceAttr("anthropic_service_account.ci", "organization_role", "user"),
					resource.TestCheckResourceAttr("anthropic_service_account.ci", "description", "GitHub Actions CI service account"),
					resource.TestCheckResourceAttr("anthropic_service_account_workspace.ci_membership", "workspace_role", "workspace_developer"),
					resource.TestCheckResourceAttr("anthropic_service_account_workspace.ci_membership", "implicit", "false"),
					resource.TestCheckResourceAttr("anthropic_federation_issuer.github", "name", "github-actions"),
					resource.TestCheckResourceAttr("anthropic_federation_issuer.github", "issuer_url", "https://token.actions.githubusercontent.com"),
					resource.TestCheckResourceAttr("anthropic_federation_issuer.github", "jwks.type", "discovery"),
					resource.TestCheckResourceAttr("anthropic_federation_rule.github_main", "name", "github-main-branch"),
					resource.TestCheckResourceAttr("anthropic_federation_rule.github_main", "oauth_scope", "workspace:developer"),
					resource.TestCheckResourceAttr("anthropic_federation_rule.github_main", "match.subject_prefix", "repo:example/app:ref:refs/heads/main"),
					resource.TestCheckResourceAttr("anthropic_federation_rule.github_main", "match.claims.repository_owner", "example"),
					resource.TestCheckResourceAttr("data.anthropic_service_accounts.all", "service_accounts.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_federation_issuers.all", "issuers.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_federation_rules.all", "rules.#", "1"),
				),
			},
			// Step 5: Update Service Account role, Service Account Workspace role, and Federation Rule match/scope
			{
				Config: mock.wifProviderConfig() + `
resource "anthropic_workspace" "ci_ws" {
  name = "ci-workspace"
}

resource "anthropic_service_account" "ci" {
  name              = "ci-runner"
  organization_role = "developer"
}

resource "anthropic_service_account_workspace" "ci_membership" {
  service_account_id = anthropic_service_account.ci.id
  workspace_id       = anthropic_workspace.ci_ws.id
  workspace_role     = "workspace_admin"
}

resource "anthropic_federation_issuer" "github" {
  name       = "github-actions"
  issuer_url = "https://token.actions.githubusercontent.com"
  check_jti  = true
  jwks = {
    type = "discovery"
  }
}

resource "anthropic_federation_rule" "github_main" {
  name                      = "github-main-branch"
  issuer_id                 = anthropic_federation_issuer.github.id
  service_account_id        = anthropic_service_account.ci.id
  oauth_scope               = "workspace:inference"
  applies_to_all_workspaces = true
  match = {
    subject_prefix = "repo:example/app:*"
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_service_account.ci", "organization_role", "developer"),
					resource.TestCheckNoResourceAttr("anthropic_service_account.ci", "description"),
					resource.TestCheckResourceAttr("anthropic_service_account_workspace.ci_membership", "workspace_role", "workspace_admin"),
					resource.TestCheckResourceAttr("anthropic_federation_issuer.github", "check_jti", "true"),
					resource.TestCheckResourceAttr("anthropic_federation_rule.github_main", "oauth_scope", "workspace:inference"),
					resource.TestCheckResourceAttr("anthropic_federation_rule.github_main", "applies_to_all_workspaces", "true"),
					resource.TestCheckResourceAttr("anthropic_federation_rule.github_main", "match.subject_prefix", "repo:example/app:*"),
					resource.TestCheckNoResourceAttr("anthropic_federation_rule.github_main", "match.claims"),
				),
			},
			// Step 6: Import state verification for WIF resources
			{
				ResourceName:      "anthropic_service_account.ci",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "anthropic_service_account_workspace.ci_membership",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "anthropic_federation_issuer.github",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "anthropic_federation_rule.github_main",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestRBACGroupMemberRemovedOutOfBand(t *testing.T) {
	mock := newMockAnthropicServer(t)
	config := mock.providerConfig() + `
resource "anthropic_rbac_group" "security" {
  name = "Security-Team"
}

resource "anthropic_rbac_group_member" "alice_sec" {
  group_id = anthropic_rbac_group.security.id
  user_id  = "user_01JEXISTING"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config},
			{
				PreConfig: func() {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					for _, members := range mock.rbacMembers {
						delete(members, "user_01JEXISTING")
					}
				},
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func TestFederationRuleWorkspaceBindings(t *testing.T) {
	mock := newMockAnthropicServer(t)
	base := mock.wifProviderConfig() + `
resource "anthropic_workspace" "a" {
  name = "ws-a"
}

resource "anthropic_workspace" "b" {
  name = "ws-b"
}

resource "anthropic_service_account" "ci" {
  name              = "ci-runner"
  organization_role = "developer"
}

resource "anthropic_federation_issuer" "github" {
  name       = "github-actions"
  issuer_url = "https://token.actions.githubusercontent.com"
  jwks = {
    type = "discovery"
  }
}
`
	rule := func(body string) string {
		return base + `
resource "anthropic_federation_rule" "r" {
  name               = "ci-rule"
  issuer_id          = anthropic_federation_issuer.github.id
  service_account_id = anthropic_service_account.ci.id
  oauth_scope        = "workspace:inference"
  match = {
    audience = "https://api.anthropic.com"
    claims = {
      sub   = "104892101234567890123"
      email = "ci@example-project.iam.gserviceaccount.com"
    }
  }
` + body + `
}
`
	}
	const r = "anthropic_federation_rule.r"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      rule(""),
				ExpectError: regexp.MustCompile(`Missing Federation Rule Workspace`),
			},
			{
				Config: rule(`
  description   = "two workspaces"
  workspace_ids = [anthropic_workspace.a.id, anthropic_workspace.b.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "workspace_ids.#", "2"),
					resource.TestCheckResourceAttr(r, "description", "two workspaces"),
				),
			},
			{
				Config: rule(`
  workspace_ids = [anthropic_workspace.b.id]
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "workspace_ids.#", "1"),
					resource.TestCheckResourceAttrPair(r, "workspace_ids.0", "anthropic_workspace.b", "id"),
					resource.TestCheckNoResourceAttr(r, "description"),
				),
			},
			{
				Config: rule(`
  applies_to_all_workspaces = true
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "applies_to_all_workspaces", "true"),
					resource.TestCheckNoResourceAttr(r, "workspace_ids"),
				),
			},
		},
	})
}
