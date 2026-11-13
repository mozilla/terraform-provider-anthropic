// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

// workspace ID -> rate_limit_id -> limiter type -> value
type mockWorkspaceRateLimits map[string]map[string]map[string]int64

var mockOrgRateLimits = []client.OrganizationRateLimit{
	{
		ID:     "rl_sonnet",
		Group:  client.RateLimitGroup{ID: "rlg_sonnet", Type: "model_group", DisplayName: "Claude Sonnet"},
		Limits: []client.RateLimitValue{{Type: "requests_per_minute", Value: 4000}, {Type: "input_tokens_per_minute", Value: 2000000}},
		Models: []string{"claude-sonnet-example"},
		Type:   "rate_limit",
	},
	{
		ID:     "rl_batch",
		Group:  client.RateLimitGroup{ID: "rlg_batch", Type: "batch"},
		Limits: []client.RateLimitValue{{Type: "requests_per_minute", Value: 1000}},
		Type:   "rate_limit",
	},
}

func limitsRejectBeta(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("anthropic-beta") != "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "unexpected anthropic-beta header")
		return true
	}
	return false
}

func init() {
	extraRoutes["rate_limits"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if limitsRejectBeta(w, r) || r.Method != http.MethodGet || len(sub) != 0 {
			return
		}
		out := []client.OrganizationRateLimit{}
		for _, e := range mockOrgRateLimits {
			if gt := r.URL.Query().Get("group_type"); gt != "" && e.Group.Type != gt {
				continue
			}
			out = append(out, e)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": out, "next_page": nil})
	}

	extraRoutes["workspaces/rate_limits"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if limitsRejectBeta(w, r) {
			return
		}
		st := extraState(m, "workspace_rate_limits", func() mockWorkspaceRateLimits { return mockWorkspaceRateLimits{} })
		ws := sub[0]
		entry := func(rlID string) client.WorkspaceRateLimit {
			e := client.WorkspaceRateLimit{RateLimitID: rlID, WorkspaceID: ws, Type: "workspace_rate_limit", Limits: []client.RateLimitValue{}}
			for _, o := range mockOrgRateLimits {
				if o.ID == rlID {
					e.Group, e.Models = o.Group, o.Models
				}
			}
			for k, v := range st[ws][rlID] {
				lv := client.RateLimitValue{Type: k, Value: v}
				lv.Source = &struct {
					Type string `json:"type"`
				}{"workspace"}
				e.Limits = append(e.Limits, lv)
			}
			// An inherited organization value is always listed so callers must filter on source.
			if _, ok := st[ws][rlID]["output_tokens_per_minute"]; !ok {
				lv := client.RateLimitValue{Type: "output_tokens_per_minute", Value: 400000}
				lv.Source = &struct {
					Type string `json:"type"`
				}{"organization"}
				e.Limits = append(e.Limits, lv)
			}
			return e
		}
		switch {
		case len(sub) == 2 && r.Method == http.MethodGet:
			out := []client.WorkspaceRateLimit{}
			for rlID, vals := range st[ws] {
				if len(vals) > 0 {
					out = append(out, entry(rlID))
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": out, "next_page": nil})
		case len(sub) == 3 && r.Method == http.MethodPost:
			var body struct {
				Limits []client.WorkspaceRateLimitUpdate `json:"limits"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if st[ws] == nil {
				st[ws] = map[string]map[string]int64{}
			}
			if st[ws][sub[2]] == nil {
				st[ws][sub[2]] = map[string]int64{}
			}
			for _, l := range body.Limits {
				if l.Value == nil {
					delete(st[ws][sub[2]], l.Type)
				} else {
					st[ws][sub[2]][l.Type] = *l.Value
				}
			}
			writeJSON(w, http.StatusOK, entry(sub[2]))
		default:
			writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
		}
	}

	extraRoutes["spend_limit_increase_requests"] = func(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
		if limitsRejectBeta(w, r) || r.Method != http.MethodGet || len(sub) != 0 {
			return
		}
		resolved := "2026-02-03T00:00:00Z"
		all := []client.SpendLimitIncreaseRequest{
			{ID: "slir_1", Actor: client.LimitActor{Type: "user_actor", UserID: "user_01JEXISTING"}, Period: "monthly", Status: "pending", CreatedAt: "2026-02-01T00:00:00Z"},
			{ID: "slir_2", Actor: client.LimitActor{Type: "user_actor", UserID: "user_01JEXISTING"}, ResolvedBy: &client.LimitActor{Type: "user_actor", UserID: "user_admin"}, Period: "monthly", Status: "denied", CreatedAt: "2026-02-02T00:00:00Z", ResolvedAt: &resolved},
		}
		statuses := r.URL.Query()["status[]"]
		out := []client.SpendLimitIncreaseRequest{}
		for _, q := range all {
			if len(statuses) == 0 || slices.Contains(statuses, q.Status) {
				out = append(out, q)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": out, "next_page": nil})
	}
}

func (m *mockAnthropicServer) handleEffectiveSpendLimits(w http.ResponseWriter, r *http.Request) {
	amount := "25000"
	splID := "spl_seat"
	all := []client.SpendSummary{
		{Actor: client.LimitActor{Type: "user_actor", UserID: "user_01JEXISTING"}, Amount: &amount, Currency: "USD", Period: "monthly", PeriodToDateSpend: "1200.5", Source: client.SpendLimitSource{Type: "seat_tier", SeatTier: "premium"}, SpendLimitID: &splID},
		{Actor: client.LimitActor{Type: "user_actor", UserID: "user_other"}, Currency: "USD", Period: "monthly", PeriodToDateSpend: "0", Source: client.SpendLimitSource{Type: "organization"}},
	}
	ids := r.URL.Query()["user_ids[]"]
	out := []client.SpendSummary{}
	for _, s := range all {
		if len(ids) == 0 || slices.Contains(ids, s.Actor.UserID) {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "next_page": nil})
}

func TestSpendLimitWorkspaceAndOrganizationScopes(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "ws" {
  workspace_id = "wrkspc_team"
  amount       = "50000"
}

resource "anthropic_spend_limit" "org" {
  organization = true
  amount       = "90000"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_spend_limit.ws", "workspace_id", "wrkspc_team"),
					resource.TestCheckNoResourceAttr("anthropic_spend_limit.ws", "user_id"),
					resource.TestCheckNoResourceAttr("anthropic_spend_limit.ws", "organization"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.ws", "period", "monthly"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.ws", "is_enabled", "true"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.org", "organization", "true"),
					resource.TestCheckResourceAttr("anthropic_spend_limit.org", "amount", "90000"),
				),
			},
			{
				ResourceName:      "anthropic_spend_limit.org",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "ws" {
  workspace_id = "wrkspc_team"
  amount       = "60000"
}
`,
				Check: resource.TestCheckResourceAttr("anthropic_spend_limit.ws", "amount", "60000"),
			},
			{
				ResourceName:      "anthropic_spend_limit.ws",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if n := len(mock.spendLimits); n != 0 {
				return fmt.Errorf("expected all spend limits deleted, %d remain", n)
			}
			return nil
		},
	})
}

func TestSpendLimitUserDailyImport(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "daily" {
  user_id = "user_01JEXISTING"
  period  = "daily"
  amount  = "500"
}
`,
				Check: resource.TestCheckResourceAttr("anthropic_spend_limit.daily", "period", "daily"),
			},
			{
				ResourceName:      "anthropic_spend_limit.daily",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestSpendLimitRefusesToAdoptExisting(t *testing.T) {
	mock := newMockAnthropicServer(t)
	amount := "70000"
	mock.mu.Lock()
	mock.spendLimits["spl_preexisting_org"] = client.SpendLimit{ID: "spl_preexisting_org", Amount: &amount, Currency: "USD", Period: "monthly", Scope: client.SpendLimitScope{Type: "organization"}, Type: "spend_limit"}
	mock.spendLimits["spl_preexisting_ws"] = client.SpendLimit{ID: "spl_preexisting_ws", Amount: &amount, Currency: "USD", Period: "monthly", Scope: client.SpendLimitScope{Type: "workspace", WorkspaceID: "wrkspc_team"}, Type: "spend_limit"}
	mock.mu.Unlock()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "org" {
  organization = true
  amount       = "90000"
}
`,
				ExpectError: regexp.MustCompile(`terraform import anthropic_spend_limit.<name>\s+spl_preexisting_org`),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "ws" {
  workspace_id = "wrkspc_team"
  amount       = "90000"
}
`,
				ExpectError: regexp.MustCompile(`spl_preexisting_ws`),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "other_ws" {
  workspace_id = "wrkspc_other"
  amount       = "90000"
}
`,
				Check: resource.TestCheckResourceAttr("anthropic_spend_limit.other_ws", "workspace_id", "wrkspc_other"),
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			for _, id := range []string{"spl_preexisting_org", "spl_preexisting_ws"} {
				if sl, ok := mock.spendLimits[id]; !ok || *sl.Amount != amount {
					return fmt.Errorf("pre-existing limit %s was changed or deleted", id)
				}
			}
			return nil
		},
	})
}

func TestSpendLimitScopeValidation(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "two_scopes" {
  user_id      = "user_01JEXISTING"
  workspace_id = "wrkspc_team"
  amount       = "100"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Attribute Combination`),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "weekly_ws" {
  workspace_id = "wrkspc_team"
  period       = "weekly"
  amount       = "100"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Spend Limit Period`),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "unlimited_org" {
  organization = true
  unlimited    = true
}
`,
				ExpectError: regexp.MustCompile(`Invalid Unlimited Spend Limit`),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_spend_limit" "default_ws" {
  workspace_id = "` + mockDefaultWorkspaceID + `"
  amount       = "100"
}
`,
				ExpectError: regexp.MustCompile(`Claude\s+Console\s+early\s+access`),
			},
		},
	})
}

func TestWorkspaceRateLimit(t *testing.T) {
	mock := newMockAnthropicServer(t)
	stored := func() map[string]int64 {
		mock.mu.Lock()
		defer mock.mu.Unlock()
		return extraState(mock, "workspace_rate_limits", func() mockWorkspaceRateLimits { return mockWorkspaceRateLimits{} })["wrkspc_team"]["rl_sonnet"]
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_workspace_rate_limit" "sonnet" {
  workspace_id  = "wrkspc_team"
  rate_limit_id = "rl_sonnet"
  limits = {
    requests_per_minute     = 100
    input_tokens_per_minute = 50000
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_workspace_rate_limit.sonnet", "id", "wrkspc_team/rl_sonnet"),
					resource.TestCheckResourceAttr("anthropic_workspace_rate_limit.sonnet", "limits.%", "2"),
					resource.TestCheckResourceAttr("anthropic_workspace_rate_limit.sonnet", "limits.requests_per_minute", "100"),
				),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_workspace_rate_limit" "sonnet" {
  workspace_id  = "wrkspc_team"
  rate_limit_id = "rl_sonnet"
  limits = {
    requests_per_minute = 200
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_workspace_rate_limit.sonnet", "limits.%", "1"),
					resource.TestCheckResourceAttr("anthropic_workspace_rate_limit.sonnet", "limits.requests_per_minute", "200"),
					func(_ *terraform.State) error {
						if got := stored(); len(got) != 1 || got["requests_per_minute"] != 200 {
							return fmt.Errorf("removed override was not cleared: %v", got)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "anthropic_workspace_rate_limit.sonnet",
				ImportState:       true,
				ImportStateId:     "wrkspc_team/rl_sonnet",
				ImportStateVerify: true,
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			if got := stored(); len(got) != 0 {
				return fmt.Errorf("expected overrides cleared on destroy, got %v", got)
			}
			return nil
		},
	})
}

func TestWorkspaceRateLimitClearsUnmanagedOverrides(t *testing.T) {
	mock := newMockAnthropicServer(t)
	st := func() mockWorkspaceRateLimits {
		return extraState(mock, "workspace_rate_limits", func() mockWorkspaceRateLimits { return mockWorkspaceRateLimits{} })
	}
	mock.mu.Lock()
	st()["wrkspc_team"] = map[string]map[string]int64{"rl_sonnet": {"tokens_per_day": 999}}
	mock.mu.Unlock()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_workspace_rate_limit" "sonnet" {
  workspace_id  = "wrkspc_team"
  rate_limit_id = "rl_sonnet"
  limits = {
    requests_per_minute = 100
  }
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_workspace_rate_limit.sonnet", "limits.%", "1"),
					resource.TestCheckResourceAttr("anthropic_workspace_rate_limit.sonnet", "limits.requests_per_minute", "100"),
					func(_ *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if got := st()["wrkspc_team"]["rl_sonnet"]; len(got) != 1 || got["requests_per_minute"] != 100 {
							return fmt.Errorf("pre-existing override was not cleared: %v", got)
						}
						return nil
					},
				),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_workspace_rate_limit" "sonnet" {
  workspace_id  = "wrkspc_team"
  rate_limit_id = "rl_sonnet"
  limits = {
    requests_per_minute = 100
  }
}
`,
				PlanOnly: true,
			},
		},
	})
}

func TestLimitsDataSources(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
data "anthropic_rate_limits" "all" {}

data "anthropic_rate_limits" "models" {
  group_type = "model_group"
}

data "anthropic_effective_spend_limits" "one" {
  user_ids = ["user_01JEXISTING"]
  periods  = ["monthly"]
}

data "anthropic_spend_limit_increase_requests" "denied" {
  statuses = ["denied"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.anthropic_rate_limits.all", "rate_limits.#", "2"),
					resource.TestCheckResourceAttr("data.anthropic_rate_limits.models", "rate_limits.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_rate_limits.models", "rate_limits.0.id", "rl_sonnet"),
					resource.TestCheckResourceAttr("data.anthropic_rate_limits.models", "rate_limits.0.group_display_name", "Claude Sonnet"),
					resource.TestCheckResourceAttr("data.anthropic_rate_limits.models", "rate_limits.0.limits.requests_per_minute", "4000"),
					resource.TestCheckResourceAttr("data.anthropic_rate_limits.models", "rate_limits.0.models.#", "1"),
					resource.TestCheckNoResourceAttr("data.anthropic_rate_limits.all", "rate_limits.1.models"),
					resource.TestCheckResourceAttr("data.anthropic_effective_spend_limits.one", "effective_limits.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_effective_spend_limits.one", "effective_limits.0.actor_id", "user_01JEXISTING"),
					resource.TestCheckResourceAttr("data.anthropic_effective_spend_limits.one", "effective_limits.0.amount", "25000"),
					resource.TestCheckResourceAttr("data.anthropic_effective_spend_limits.one", "effective_limits.0.source_type", "seat_tier"),
					resource.TestCheckResourceAttr("data.anthropic_effective_spend_limits.one", "effective_limits.0.source_id", "premium"),
					resource.TestCheckResourceAttr("data.anthropic_spend_limit_increase_requests.denied", "requests.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_spend_limit_increase_requests.denied", "requests.0.resolved_by_id", "user_admin"),
					resource.TestCheckResourceAttr("data.anthropic_spend_limit_increase_requests.denied", "requests.0.resolved_at", "2026-02-03T00:00:00Z"),
				),
			},
		},
	})
}
