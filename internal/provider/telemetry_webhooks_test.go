// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

type mockTelemetryState struct {
	seq        int
	dests      map[string]*client.TelemetryDestination
	headers    map[string]map[string]string
	workspaces map[string][]client.TelemetryDestinationWorkspace
}

type mockWebhookState struct {
	seq      int
	webhooks map[string]client.Webhook
}

func telemetryState(m *mockAnthropicServer) *mockTelemetryState {
	return extraState(m, "telemetry_destinations", func() *mockTelemetryState {
		return &mockTelemetryState{
			dests:      map[string]*client.TelemetryDestination{},
			headers:    map[string]map[string]string{},
			workspaces: map[string][]client.TelemetryDestinationWorkspace{},
		}
	})
}

func webhookState(m *mockAnthropicServer) *mockWebhookState {
	return extraState(m, "webhooks", func() *mockWebhookState {
		return &mockWebhookState{webhooks: map[string]client.Webhook{}}
	})
}

func init() {
	extraRoutes["telemetry_destinations"] = handleMockTelemetryDestinations
	extraRoutes["webhooks"] = handleMockWebhooks
}

func setMockTelemetryHeaderNames(d *client.TelemetryDestination, h map[string]string) {
	d.HeaderNames = make([]string, 0, len(h))
	for k := range h {
		d.HeaderNames = append(d.HeaderNames, k)
	}
	sort.Strings(d.HeaderNames)
}

func handleMockTelemetryDestinations(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
	if r.Header.Get("anthropic-beta") != client.BetaTelemetryDestinations {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "missing or unexpected anthropic-beta header for telemetry_destinations")
		return
	}
	st := telemetryState(m)
	now := "2026-11-01T00:00:00Z"

	if len(sub) == 0 {
		switch r.Method {
		case http.MethodPost:
			var req client.CreateTelemetryDestinationRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EndpointURL == "" || req.Workspaces.Type == "" {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "endpoint_url and workspaces are required")
				return
			}
			st.seq++
			id := fmt.Sprintf("tdst_%02d", st.seq)
			d := &client.TelemetryDestination{
				ID: id, CreatedAt: now, UpdatedAt: now, DisplayName: req.DisplayName, EndpointURL: req.EndpointURL,
				State:      client.TelemetryDestinationState{Type: "enabled"},
				Workspaces: req.Workspaces,
				Type:       "telemetry_destination",
			}
			if req.Traces != nil {
				d.Traces = *req.Traces
			}
			if d.Traces.Enabled == nil {
				t := true
				d.Traces.Enabled = &t
			}
			if d.Traces.ContentMode == "" {
				d.Traces.ContentMode = "metadata_only"
			}
			h := map[string]string{}
			for k, v := range req.Headers {
				h[k] = v
			}
			st.headers[id] = h
			setMockTelemetryHeaderNames(d, h)
			st.dests[id] = d
			writeJSON(w, http.StatusOK, d)
		case http.MethodGet:
			filter := r.URL.Query()["workspace_ids[]"]
			out := []client.TelemetryDestination{}
			for _, d := range st.dests {
				if len(filter) > 0 && d.Workspaces.Type != "all" {
					onList := slices.ContainsFunc(st.workspaces[d.ID], func(e client.TelemetryDestinationWorkspace) bool {
						return slices.Contains(filter, e.WorkspaceID)
					})
					if onList != (d.Workspaces.Type == "include") {
						continue
					}
				}
				out = append(out, *d)
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": out, "next_page": nil})
		}
		return
	}

	id := sub[0]
	d, ok := st.dests[id]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "telemetry destination not found")
		return
	}

	if len(sub) == 1 {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, d)
		case http.MethodDelete:
			delete(st.dests, id)
			delete(st.headers, id)
			delete(st.workspaces, id)
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "type": "telemetry_destination_deleted"})
		case http.MethodPost:
			var body map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&body)
			if raw, ok := body["headers"]; ok {
				if _, hasURL := body["endpoint_url"]; !hasURL {
					writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "setting headers requires endpoint_url")
					return
				}
				var hs map[string]*string
				_ = json.Unmarshal(raw, &hs)
				for k, v := range hs {
					if v == nil {
						delete(st.headers[id], k)
					} else {
						st.headers[id][k] = *v
					}
				}
				setMockTelemetryHeaderNames(d, st.headers[id])
			}
			if raw, ok := body["endpoint_url"]; ok {
				_ = json.Unmarshal(raw, &d.EndpointURL)
			}
			if raw, ok := body["display_name"]; ok {
				d.DisplayName = nil
				_ = json.Unmarshal(raw, &d.DisplayName)
			}
			if raw, ok := body["state"]; ok {
				_ = json.Unmarshal(raw, &d.State)
				d.State.Reason = nil
				if d.State.Type == "disabled" {
					d.State.Reason = &struct {
						Type string `json:"type"`
					}{Type: "caller"}
				}
			}
			if raw, ok := body["traces"]; ok {
				var t client.TelemetryTraces
				_ = json.Unmarshal(raw, &t)
				d.Traces = t
			}
			if raw, ok := body["workspaces"]; ok {
				var ws client.TelemetryDestinationScopeRef
				_ = json.Unmarshal(raw, &ws)
				if ws.Type != d.Workspaces.Type {
					st.workspaces[id] = nil
				}
				d.Workspaces = ws
			}
			d.UpdatedAt = "2026-11-02T00:00:00Z"
			writeJSON(w, http.StatusOK, d)
		}
		return
	}

	if sub[1] != "workspaces" {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
		return
	}
	if len(sub) == 2 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"data": append([]client.TelemetryDestinationWorkspace{}, st.workspaces[id]...), "next_page": nil})
		return
	}
	if len(sub) != 3 {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
		return
	}
	if d.Workspaces.Type == "all" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "destination covers all workspaces and has no list")
		return
	}
	wsID := sub[2]
	idx := slices.IndexFunc(st.workspaces[id], func(e client.TelemetryDestinationWorkspace) bool { return e.WorkspaceID == wsID })
	switch r.Method {
	case http.MethodPost:
		if idx < 0 {
			st.workspaces[id] = append(st.workspaces[id], client.TelemetryDestinationWorkspace{
				CreatedAt: now, TelemetryDestinationID: id, WorkspaceID: wsID, Type: "telemetry_destination_workspace",
			})
			idx = len(st.workspaces[id]) - 1
		}
		writeJSON(w, http.StatusOK, st.workspaces[id][idx])
	case http.MethodDelete:
		if idx < 0 {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "workspace is not on the list")
			return
		}
		st.workspaces[id] = slices.Delete(st.workspaces[id], idx, idx+1)
		writeJSON(w, http.StatusOK, map[string]any{"telemetry_destination_id": id, "workspace_id": wsID, "type": "telemetry_destination_workspace_deleted"})
	}
}

func handleMockWebhooks(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
	if r.Header.Get("anthropic-beta") != client.BetaWebhooks {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "missing or unexpected anthropic-beta header for webhooks")
		return
	}
	st := webhookState(m)

	if len(sub) == 0 {
		switch r.Method {
		case http.MethodPost:
			var req client.CreateWebhookRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" || len(req.EnabledEvents) == 0 {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "url and enabled_events are required")
				return
			}
			st.seq++
			wh := client.Webhook{
				ID: fmt.Sprintf("whep_%02d", st.seq), CreatedAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-01T00:00:00Z",
				Description: req.Description, EnabledEvents: req.EnabledEvents, Name: req.Name, State: "enabled", URL: req.URL,
			}
			st.webhooks[wh.ID] = wh
			wh.SigningSecret = "whsec_test-secret-not-real"
			writeJSON(w, http.StatusOK, wh)
		case http.MethodGet:
			out := []client.Webhook{}
			for _, wh := range st.webhooks {
				out = append(out, wh)
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": out, "has_more": false})
		}
		return
	}

	wh, ok := st.webhooks[sub[0]]
	if !ok || len(sub) != 1 {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "webhook not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, wh)
	case http.MethodPost:
		var req client.UpdateWebhookRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		wh.URL, wh.Name, wh.Description, wh.State = req.URL, req.Name, req.Description, req.State
		if len(req.EnabledEvents) > 0 {
			wh.EnabledEvents = req.EnabledEvents
		}
		wh.UpdatedAt = "2026-11-02T00:00:00Z"
		st.webhooks[wh.ID] = wh
		writeJSON(w, http.StatusOK, wh)
	case http.MethodDelete:
		delete(st.webhooks, wh.ID)
		writeJSON(w, http.StatusOK, map[string]any{"id": wh.ID, "type": "webhook_endpoint_deleted"})
	}
}

func TestTelemetryDestinationLifecycle(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if n := len(telemetryState(mock).dests); n != 0 {
				return fmt.Errorf("%d telemetry destinations left after destroy", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_telemetry_destination" "otel" {
  display_name    = "Example collector"
  endpoint_url    = "https://collector.example.com/otlp"
  workspace_scope = "include"
  headers = {
    authorization = "Bearer test-token-not-real"
  }
}

resource "anthropic_telemetry_destination_workspace" "ws" {
  destination_id = anthropic_telemetry_destination.otel.id
  workspace_id   = "wrkspc_test01"
}

data "anthropic_telemetry_destination" "by_id" {
  id = anthropic_telemetry_destination.otel.id
}

data "anthropic_telemetry_destinations" "covering" {
  workspace_ids = [anthropic_telemetry_destination_workspace.ws.workspace_id]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "id", "tdst_01"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "header_names.#", "1"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "header_names.0", "authorization"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "enabled", "true"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "traces_content_mode", "metadata_only"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "traces_enabled", "true"),
					resource.TestCheckNoResourceAttr("anthropic_telemetry_destination.otel", "traces_max_content_bytes"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination_workspace.ws", "id", "tdst_01/wrkspc_test01"),
					resource.TestCheckResourceAttr("data.anthropic_telemetry_destination.by_id", "display_name", "Example collector"),
					resource.TestCheckResourceAttr("data.anthropic_telemetry_destination.by_id", "workspace_scope", "include"),
					resource.TestCheckResourceAttr("data.anthropic_telemetry_destinations.covering", "telemetry_destinations.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_telemetry_destinations.covering", "telemetry_destinations.0.id", "tdst_01"),
				),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_telemetry_destination" "otel" {
  endpoint_url             = "https://collector.example.com/otlp"
  workspace_scope          = "include"
  enabled                  = false
  traces_content_mode      = "content"
  traces_max_content_bytes = 4096
  headers = {
    x-api-token = "test-token-not-real"
  }
}

resource "anthropic_telemetry_destination_workspace" "ws" {
  destination_id = anthropic_telemetry_destination.otel.id
  workspace_id   = "wrkspc_test01"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("anthropic_telemetry_destination.otel", "display_name"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "header_names.#", "1"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "header_names.0", "x-api-token"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "enabled", "false"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "disabled_reason", "caller"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "traces_content_mode", "content"),
					resource.TestCheckResourceAttr("anthropic_telemetry_destination.otel", "traces_max_content_bytes", "4096"),
				),
			},
			{
				ResourceName:            "anthropic_telemetry_destination.otel",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"headers"},
			},
			{
				ResourceName:      "anthropic_telemetry_destination_workspace.ws",
				ImportState:       true,
				ImportStateId:     "tdst_01/wrkspc_test01",
				ImportStateVerify: true,
			},
		},
	})
}

func TestWebhookLifecycle(t *testing.T) {
	mock := newMockAnthropicServer(t)
	const secret = "whsec_test-secret-not-real"

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			if n := len(webhookState(mock).webhooks); n != 0 {
				return fmt.Errorf("%d webhooks left after destroy", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
resource "anthropic_webhook" "hook" {
  url            = "https://hooks.example.com/anthropic"
  enabled_events = ["user_profile.enforcement_action_opened"]
  enabled        = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_webhook.hook", "id", "whep_01"),
					resource.TestCheckResourceAttr("anthropic_webhook.hook", "signing_secret", secret),
					resource.TestCheckResourceAttr("anthropic_webhook.hook", "enabled", "false"),
					resource.TestCheckResourceAttr("anthropic_webhook.hook", "name", ""),
				),
			},
			{
				Config: mock.providerConfig() + `
resource "anthropic_webhook" "hook" {
  url         = "https://hooks.example.com/anthropic/v2"
  name        = "example-hook"
  description = "Example endpoint"
  enabled_events = [
    "user_profile.enforcement_action_opened",
    "user_profile.enforcement_action_resolved",
  ]
}

data "anthropic_webhook" "by_id" {
  id = anthropic_webhook.hook.id
}

data "anthropic_webhooks" "all" {
  depends_on = [anthropic_webhook.hook]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_webhook.hook", "signing_secret", secret),
					resource.TestCheckResourceAttr("anthropic_webhook.hook", "enabled", "true"),
					resource.TestCheckResourceAttr("anthropic_webhook.hook", "enabled_events.#", "2"),
					resource.TestCheckResourceAttr("data.anthropic_webhook.by_id", "name", "example-hook"),
					resource.TestCheckResourceAttr("data.anthropic_webhook.by_id", "url", "https://hooks.example.com/anthropic/v2"),
					resource.TestCheckResourceAttr("data.anthropic_webhooks.all", "webhooks.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_webhooks.all", "webhooks.0.id", "whep_01"),
				),
			},
			{
				ResourceName:            "anthropic_webhook.hook",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_secret"},
			},
		},
	})
}
