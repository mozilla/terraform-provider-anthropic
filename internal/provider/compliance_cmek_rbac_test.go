// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

type complianceMockState struct {
	settings client.ComplianceSettings
}

type externalKeyMockState struct {
	keys map[string]client.ExternalKey
	seq  int
}

func rejectAnyBetaHeader(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("anthropic-beta") != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "unexpected anthropic-beta header")
		return true
	}
	return false
}

func init() {
	extraRoutes["compliance_settings"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if rejectAnyBetaHeader(w, r) || len(sub) != 0 {
			return
		}
		st := extraState(m, "compliance_settings", func() *complianceMockState {
			return &complianceMockState{settings: client.ComplianceSettings{State: client.ComplianceSettingsState{Type: "disabled"}, Type: "compliance_settings"}}
		})
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, st.settings)
		case http.MethodPost:
			var body struct {
				State client.ComplianceSettingsState `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.State.Type != "enabled" && body.State.Type != "disabled") {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "state must be {\"type\": \"enabled\"|\"disabled\"}")
				return
			}
			st.settings.State = body.State
			writeJSON(w, http.StatusOK, st.settings)
		}
	}

	extraRoutes["external_keys"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if rejectAnyBetaHeader(w, r) {
			return
		}
		st := extraState(m, "external_keys", func() *externalKeyMockState {
			return &externalKeyMockState{keys: map[string]client.ExternalKey{}}
		})
		deriveRegion := func(pc *client.ExternalKeyProviderConfig) {
			if pc.Type == "aws" && pc.Region == nil {
				region := "us-east-1"
				pc.Region = &region
			}
		}
		if len(sub) == 0 {
			switch r.Method {
			case http.MethodGet:
				list := make([]client.ExternalKey, 0, len(st.keys))
				for _, k := range st.keys {
					list = append(list, k)
				}
				writeJSON(w, http.StatusOK, client.Page[client.ExternalKey]{Data: list})
			case http.MethodPost:
				var req client.CreateExternalKeyRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				st.seq++
				deriveRegion(&req.ProviderConfig)
				geo := req.Geo
				if geo == "" {
					geo = "us"
				}
				k := client.ExternalKey{
					ID:             fmt.Sprintf("ekey_test%d", st.seq),
					Attachment:     client.ExternalKeyAttachment{Type: "unattached"},
					CreatedAt:      "2026-11-01T00:00:00Z",
					UpdatedAt:      "2026-11-01T00:00:00Z",
					DisplayName:    req.DisplayName,
					Geo:            geo,
					ProviderConfig: req.ProviderConfig,
					UserProfileID:  req.UserProfileID,
					Type:           "external_key",
				}
				st.keys[k.ID] = k
				writeJSON(w, http.StatusOK, k)
			}
			return
		}
		k, ok := st.keys[sub[0]]
		if !ok || len(sub) != 1 {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "external key not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, k)
		case http.MethodPost:
			var req client.UpdateExternalKeyRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if k.Attachment.Type == "attached" && (req.Geo != "" || req.ProviderConfig != nil) {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "external key in workspace policy")
				return
			}
			if req.DisplayName != nil {
				k.DisplayName = req.DisplayName
			}
			if req.Geo != "" {
				k.Geo = req.Geo
			}
			if req.ProviderConfig != nil {
				deriveRegion(req.ProviderConfig)
				k.ProviderConfig = *req.ProviderConfig
			}
			k.UpdatedAt = "2026-11-02T00:00:00Z"
			st.keys[k.ID] = k
			writeJSON(w, http.StatusOK, k)
		case http.MethodDelete:
			if k.Attachment.Type == "attached" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "external key in workspace policy")
				return
			}
			delete(st.keys, k.ID)
			writeJSON(w, http.StatusOK, map[string]string{"id": k.ID, "type": "external_key_deleted"})
		}
	}

	extraRoutes["rbac_roles/permissions"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if rejectAnyBetaHeader(w, r) {
			return
		}
		if r.Method != http.MethodGet || len(sub) != 2 || sub[0] != "role_admin" {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "rbac role not found")
			return
		}
		writeJSON(w, http.StatusOK, client.Page[client.RBACRolePermission]{Data: []client.RBACRolePermission{
			{Action: "capability_access_all", Resource: client.RBACRolePermissionResource{Type: "organization", OrganizationID: "org_01JTESTORG"}, Type: "rbac_role_permission"},
			{Action: "use", Resource: client.RBACRolePermissionResource{Type: "connector_tool", ConnectorID: "conn_example", ToolName: "search"}, Type: "rbac_role_permission"},
		}})
	}
}

func TestComplianceSettings(t *testing.T) {
	mock := newMockAnthropicServer(t)
	cfg := func(state string) string {
		return mock.providerConfig() + fmt.Sprintf(`
resource "anthropic_compliance_settings" "this" {
  state = %q
}

data "anthropic_compliance_settings" "this" {
  depends_on = [anthropic_compliance_settings.this]
}
`, state)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if got := extraState(mock, "compliance_settings", func() *complianceMockState { return nil }).settings.State.Type; got != "disabled" {
				return fmt.Errorf("destroy changed compliance state to %q", got)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: cfg("enabled"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_compliance_settings.this", "id", "compliance_settings"),
					resource.TestCheckResourceAttr("anthropic_compliance_settings.this", "state", "enabled"),
					resource.TestCheckResourceAttr("data.anthropic_compliance_settings.this", "state", "enabled"),
				),
			},
			{
				Config: cfg("disabled"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_compliance_settings.this", "state", "disabled"),
					resource.TestCheckResourceAttr("data.anthropic_compliance_settings.this", "state", "disabled"),
				),
			},
			{
				ResourceName:      "anthropic_compliance_settings.this",
				ImportState:       true,
				ImportStateId:     "compliance_settings",
				ImportStateVerify: true,
			},
		},
	})
}

func TestExternalKey(t *testing.T) {
	mock := newMockAnthropicServer(t)
	setAttachment := func(v string) func() {
		return func() {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			st := extraState(mock, "external_keys", func() *externalKeyMockState { return nil })
			for id, k := range st.keys {
				k.Attachment.Type = v
				st.keys[id] = k
			}
		}
	}
	cfg := func(name string) string {
		return mock.providerConfig() + fmt.Sprintf(`
resource "anthropic_external_key" "aws" {
  display_name = %q
  provider_config = {
    type    = "aws"
    kms_arn = "arn:aws:kms:us-east-1:111122223333:key/example"
  }
}

data "anthropic_external_key" "aws" {
  id = anthropic_external_key.aws.id
}

data "anthropic_external_keys" "all" {
  depends_on = [anthropic_external_key.aws]
}
`, name)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if n := len(extraState(mock, "external_keys", func() *externalKeyMockState { return nil }).keys); n != 0 {
				return fmt.Errorf("%d external keys left after destroy", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: cfg("primary"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_external_key.aws", "id", "ekey_test1"),
					resource.TestCheckResourceAttr("anthropic_external_key.aws", "geo", "us"),
					resource.TestCheckResourceAttr("anthropic_external_key.aws", "attachment", "unattached"),
					resource.TestCheckResourceAttr("anthropic_external_key.aws", "provider_config.region", "us-east-1"),
					resource.TestCheckNoResourceAttr("anthropic_external_key.aws", "provider_config.key_name"),
					resource.TestCheckNoResourceAttr("anthropic_external_key.aws", "user_profile_id"),
					resource.TestCheckResourceAttr("data.anthropic_external_key.aws", "display_name", "primary"),
					resource.TestCheckResourceAttr("data.anthropic_external_key.aws", "provider_config.type", "aws"),
					resource.TestCheckResourceAttr("data.anthropic_external_keys.all", "external_keys.#", "1"),
				),
			},
			// The mock rejects geo or provider_config on an attached key, so this rename proves only display_name is sent.
			{
				PreConfig: setAttachment("attached"),
				Config:    cfg("renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_external_key.aws", "display_name", "renamed"),
					resource.TestCheckResourceAttr("anthropic_external_key.aws", "attachment", "attached"),
					resource.TestCheckResourceAttr("anthropic_external_key.aws", "provider_config.region", "us-east-1"),
				),
			},
			{
				PreConfig: setAttachment("unattached"),
				Config:    cfg("renamed"),
				Check:     resource.TestCheckResourceAttr("anthropic_external_key.aws", "attachment", "unattached"),
			},
			{
				ResourceName:      "anthropic_external_key.aws",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestRBACRolePermissionsDataSource(t *testing.T) {
	mock := newMockAnthropicServer(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
data "anthropic_rbac_role_permissions" "admin" {
  role_id = "role_admin"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.anthropic_rbac_role_permissions.admin", "permissions.#", "2"),
					resource.TestCheckResourceAttr("data.anthropic_rbac_role_permissions.admin", "permissions.0.action", "capability_access_all"),
					resource.TestCheckResourceAttr("data.anthropic_rbac_role_permissions.admin", "permissions.0.resource_type", "organization"),
					resource.TestCheckResourceAttr("data.anthropic_rbac_role_permissions.admin", "permissions.0.organization_id", "org_01JTESTORG"),
					resource.TestCheckNoResourceAttr("data.anthropic_rbac_role_permissions.admin", "permissions.0.connector_id"),
					resource.TestCheckResourceAttr("data.anthropic_rbac_role_permissions.admin", "permissions.1.tool_name", "search"),
				),
			},
		},
	})
}
