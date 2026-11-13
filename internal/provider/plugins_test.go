// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

type pluginMockState struct {
	marketplaces map[string]client.PluginMarketplace
	plugins      map[string]client.Plugin
	versions     map[string][]client.PluginVersion
	shares       map[string][]client.PluginShare
	settings     map[string]map[string]client.PluginInstallationSetting
}

func newPluginMockState() *pluginMockState {
	desc := "Reviews pull requests."
	return &pluginMockState{
		marketplaces: map[string]client.PluginMarketplace{
			"marketplace_01": {ID: "marketplace_01", Type: "plugin_marketplace", Name: "library", Source: "manual",
				Owner: client.PluginOwner{Type: "organization"}, CreatedAt: "2026-09-01T00:00:00Z"},
			"marketplace_02": {ID: "marketplace_02", Type: "plugin_marketplace", Name: "personal", Source: "github",
				Owner: client.PluginOwner{Type: "user", UserID: new("user_01")}, SyncStatus: new("success"),
				LastSyncEndedAt: new("2026-09-02T00:00:00Z"), LastSyncReadSha: new("abc123"), CreatedAt: "2026-09-02T00:00:00Z"},
		},
		plugins: map[string]client.Plugin{
			"plugin_01": {ID: "plugin_01", Type: "plugin", Name: "review-pr", DisplayName: new("Review PR"), Description: &desc,
				MarketplaceID: "marketplace_01", Owner: client.PluginOwner{Type: "organization"}, ManifestVersion: new("1.0.0"),
				Reach: new("contained"), LatestVersionID: "pluginver_01", ServedVersionID: "pluginver_01",
				OrganizationInstallationPreference: new("available"), OrganizationInstallationPreferenceInherited: new(true),
				Components:  []client.PluginComponent{{Name: "review-pr", Type: "skill", Description: &desc}},
				ContentScan: &client.PluginContentScan{Status: "completed", Assessment: new("pass")},
				CreatedAt:   "2026-09-01T00:00:00Z", UpdatedAt: "2026-09-01T00:00:00Z"},
			"plugin_02": {ID: "plugin_02", Type: "plugin", Name: "notes", MarketplaceID: "marketplace_02",
				Owner: client.PluginOwner{Type: "user", UserID: new("user_01")}, LatestVersionID: "pluginver_02",
				ServedVersionID: "pluginver_02", CreatedAt: "2026-09-02T00:00:00Z", UpdatedAt: "2026-09-02T00:00:00Z"},
		},
		versions: map[string][]client.PluginVersion{
			"plugin_01": {{ID: "pluginver_01", Type: "plugin_version", PluginID: "plugin_01", ReleaseNotes: new("First release."),
				ContentScan: &client.PluginContentScan{Status: "completed", Assessment: new("pass")}, CreatedAt: "2026-09-01T00:00:00Z"}},
		},
		shares: map[string][]client.PluginShare{
			"plugin_02": {{Type: "plugin_share", PluginID: "plugin_02", Target: client.PluginTarget{Type: "rbac_group", RBACGroupID: new("grp_01")},
				GrantedAt: "2026-09-03T00:00:00Z"}},
		},
		settings: map[string]map[string]client.PluginInstallationSetting{},
	}
}

func writePluginPage[T any](w http.ResponseWriter, data []T) {
	if data == nil {
		data = []T{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "has_more": false, "next_page": nil})
}

func pluginBetaOK(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("anthropic-beta") != client.BetaPlugins {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "expected anthropic-beta "+client.BetaPlugins)
		return false
	}
	return true
}

var pluginPreferences = []string{"required", "auto_install", "available", "not_available"}

func init() {
	extraRoutes["plugin_marketplaces"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if !pluginBetaOK(w, r) {
			return
		}
		st := extraState(m, "plugins", newPluginMockState)
		if len(sub) == 0 && r.Method == http.MethodGet {
			var out []client.PluginMarketplace
			for _, id := range slices.Sorted(maps.Keys(st.marketplaces)) {
				mp := st.marketplaces[id]
				if q := r.URL.Query().Get("owner_type"); q != "" && q != mp.Owner.Type {
					continue
				}
				if q := r.URL.Query().Get("source"); q != "" && q != mp.Source {
					continue
				}
				out = append(out, mp)
			}
			writePluginPage(w, out)
			return
		}
		if len(sub) != 1 {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
			return
		}
		mp, ok := st.marketplaces[sub[0]]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "marketplace not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, mp)
		case http.MethodPost:
			if mp.Owner.Type != "organization" {
				writeAPIError(w, http.StatusForbidden, "permission_error", "personal marketplace")
				return
			}
			var body struct {
				DefaultInstallationPreference string `json:"default_installation_preference"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !slices.Contains(pluginPreferences, body.DefaultInstallationPreference) {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "invalid default_installation_preference")
				return
			}
			mp.DefaultInstallationPreference = &body.DefaultInstallationPreference
			st.marketplaces[mp.ID] = mp
			writeJSON(w, http.StatusOK, mp)
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		}
	}

	extraRoutes["plugins"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if !pluginBetaOK(w, r) {
			return
		}
		st := extraState(m, "plugins", newPluginMockState)
		if len(sub) == 0 && r.Method == http.MethodGet {
			var out []client.Plugin
			for _, id := range slices.Sorted(maps.Keys(st.plugins)) {
				p := st.plugins[id]
				if q := r.URL.Query().Get("marketplace_id"); q != "" && q != p.MarketplaceID {
					continue
				}
				if q := r.URL.Query().Get("owner_type"); q != "" && q != p.Owner.Type {
					continue
				}
				out = append(out, p)
			}
			writePluginPage(w, out)
			return
		}
		if len(sub) == 0 {
			writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
			return
		}
		p, ok := st.plugins[sub[0]]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "plugin not found")
			return
		}
		switch {
		case len(sub) == 1 && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, p)
		case len(sub) == 2 && sub[1] == "versions" && r.Method == http.MethodGet:
			writePluginPage(w, st.versions[p.ID])
		case len(sub) == 2 && sub[1] == "shares" && r.Method == http.MethodGet:
			if p.Owner.Type != "user" {
				writeAPIError(w, http.StatusNotFound, "not_found_error", "organization plugins have no shares")
				return
			}
			writePluginPage(w, st.shares[p.ID])
		case len(sub) >= 2 && sub[1] == "installation_settings":
			if p.Owner.Type != "organization" {
				writeAPIError(w, http.StatusNotFound, "not_found_error", "member plugins have no installation settings")
				return
			}
			handlePluginSettings(st, w, r, p.ID, sub[2:])
		default:
			writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
		}
	}
}

func handlePluginSettings(st *pluginMockState, w http.ResponseWriter, r *http.Request, pluginID string, sub []string) {
	settings := st.settings[pluginID]
	if len(sub) == 0 && r.Method == http.MethodGet {
		var out []client.PluginInstallationSetting
		for _, k := range slices.Sorted(maps.Keys(settings)) {
			out = append(out, settings[k])
		}
		writePluginPage(w, out)
		return
	}
	if len(sub) != 1 {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
		return
	}
	target := sub[0]
	switch r.Method {
	case http.MethodPost:
		var body struct {
			InstallationPreference string `json:"installation_preference"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !slices.Contains(pluginPreferences, body.InstallationPreference) {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "invalid installation_preference")
			return
		}
		s, ok := settings[target]
		if !ok {
			s = client.PluginInstallationSetting{Type: "plugin_installation_setting", PluginID: pluginID, CreatedAt: "2026-09-04T00:00:00Z"}
			if target == "organization" {
				s.Target = client.PluginTarget{Type: "organization"}
			} else {
				s.Target = client.PluginTarget{Type: "rbac_group", RBACGroupID: new(target)}
			}
		}
		s.InstallationPreference = body.InstallationPreference
		s.UpdatedAt = "2026-09-05T00:00:00Z"
		if settings == nil {
			settings = map[string]client.PluginInstallationSetting{}
			st.settings[pluginID] = settings
		}
		settings[target] = s
		writeJSON(w, http.StatusOK, s)
	case http.MethodDelete:
		s, ok := settings[target]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "no setting for target")
			return
		}
		delete(settings, target)
		writeJSON(w, http.StatusOK, map[string]any{"type": "plugin_installation_setting_deleted", "plugin_id": pluginID, "target": s.Target})
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
	}
}

func pluginTestConfig(mpPref, orgPref string) string {
	return fmt.Sprintf(`
resource "anthropic_plugin_marketplace" "library" {
  marketplace_id                  = "marketplace_01"
  default_installation_preference = %q
}

resource "anthropic_plugin_installation_setting" "org" {
  plugin_id               = "plugin_01"
  target                  = "organization"
  installation_preference = %q
}

resource "anthropic_plugin_installation_setting" "group" {
  plugin_id               = "plugin_01"
  target                  = "grp_01"
  installation_preference = "available"
}
`, mpPref, orgPref)
}

func TestPluginResourcesAndDataSources(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if n := len(extraState(mock, "plugins", newPluginMockState).settings["plugin_01"]); n != 0 {
				return fmt.Errorf("expected installation settings removed, %d left", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + pluginTestConfig("available", "required") + `
data "anthropic_plugin_marketplace" "personal" {
  id = "marketplace_02"
}

data "anthropic_plugin_marketplaces" "org" {
  owner_type = "organization"
}

data "anthropic_plugin" "review" {
  id = "plugin_01"
}

data "anthropic_plugins" "all" {}

data "anthropic_plugin_versions" "review" {
  plugin_id = "plugin_01"
}

data "anthropic_plugin_shares" "notes" {
  plugin_id = "plugin_02"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_plugin_marketplace.library", "id", "marketplace_01"),
					resource.TestCheckResourceAttr("anthropic_plugin_marketplace.library", "default_installation_preference", "available"),
					resource.TestCheckResourceAttr("anthropic_plugin_marketplace.library", "source", "manual"),
					resource.TestCheckNoResourceAttr("anthropic_plugin_marketplace.library", "sync_status"),
					resource.TestCheckResourceAttr("anthropic_plugin_installation_setting.org", "id", "plugin_01/organization"),
					resource.TestCheckResourceAttr("anthropic_plugin_installation_setting.org", "target_type", "organization"),
					resource.TestCheckResourceAttr("anthropic_plugin_installation_setting.org", "installation_preference", "required"),
					resource.TestCheckResourceAttr("anthropic_plugin_installation_setting.group", "target_type", "rbac_group"),
					resource.TestCheckResourceAttr("data.anthropic_plugin_marketplace.personal", "owner_user_id", "user_01"),
					resource.TestCheckResourceAttr("data.anthropic_plugin_marketplace.personal", "sync_status", "success"),
					resource.TestCheckNoResourceAttr("data.anthropic_plugin_marketplace.personal", "default_installation_preference"),
					resource.TestCheckResourceAttr("data.anthropic_plugin_marketplaces.org", "marketplaces.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_plugin.review", "name", "review-pr"),
					resource.TestCheckResourceAttr("data.anthropic_plugin.review", "components.0.type", "skill"),
					resource.TestCheckResourceAttr("data.anthropic_plugin.review", "content_scan.assessment", "pass"),
					resource.TestCheckResourceAttr("data.anthropic_plugins.all", "plugins.#", "2"),
					resource.TestCheckNoResourceAttr("data.anthropic_plugins.all", "plugins.1.content_scan.status"),
					resource.TestCheckNoResourceAttr("data.anthropic_plugins.all", "plugins.1.organization_installation_preference_inherited"),
					resource.TestCheckResourceAttr("data.anthropic_plugin_versions.review", "versions.0.release_notes", "First release."),
					resource.TestCheckResourceAttr("data.anthropic_plugin_shares.notes", "shares.0.rbac_group_id", "grp_01"),
				),
			},
			{
				Config: mock.providerConfig() + pluginTestConfig("not_available", "auto_install"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_plugin_marketplace.library", "default_installation_preference", "not_available"),
					resource.TestCheckResourceAttr("anthropic_plugin_installation_setting.org", "installation_preference", "auto_install"),
				),
			},
			{
				ResourceName:      "anthropic_plugin_marketplace.library",
				ImportState:       true,
				ImportStateId:     "marketplace_01",
				ImportStateVerify: true,
			},
			{
				ResourceName:      "anthropic_plugin_installation_setting.org",
				ImportState:       true,
				ImportStateId:     "plugin_01/organization",
				ImportStateVerify: true,
			},
			{
				ResourceName:      "anthropic_plugin_installation_setting.group",
				ImportState:       true,
				ImportStateId:     "plugin_01/grp_01",
				ImportStateVerify: true,
			},
		},
	})
}
