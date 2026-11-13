// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

type claudeTagMockState struct {
	bundles    map[string]client.ClaudeTagBundle
	targets    map[string]map[string]client.ClaudeTagBundleTarget
	workspaces map[string]*client.ClaudeTagSlackWorkspace
	channels   map[string]*client.ClaudeTagSlackChannel
	nextID     int
	// channelDeletes counts DELETE calls on a channel, which the provider must never send.
	channelDeletes int
}

func claudeTagState(m *mockAnthropicServer) *claudeTagMockState {
	return extraState(m, "claude_tag", func() *claudeTagMockState {
		enabled := true
		desc := "Example bundle"
		return &claudeTagMockState{
			bundles: map[string]client.ClaudeTagBundle{
				"capp_test01": {ID: "capp_test01", DisplayName: "Example", Description: &desc, CreatedAt: "2026-10-01T00:00:00Z", UpdatedAt: "2026-10-01T00:00:00Z", Type: "bundle"},
			},
			targets: map[string]map[string]client.ClaudeTagBundleTarget{},
			workspaces: map[string]*client.ClaudeTagSlackWorkspace{
				"slws_test01": {
					ID:        "slws_test01",
					Slack:     client.ClaudeTagSlackIdentifiers{WorkspaceID: "T00000000"},
					Settings:  client.ClaudeTagSettings{Enabled: &enabled},
					CreatedAt: "2026-10-01T00:00:00Z",
					UpdatedAt: "2026-10-01T00:00:00Z",
					Type:      "slack_workspace",
				},
			},
			channels: map[string]*client.ClaudeTagSlackChannel{},
		}
	})
}

func init() {
	extraRoutes["claude_tag"] = handleClaudeTag
}

func writeList[T any](w http.ResponseWriter, items []T) {
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false, "next_page": nil})
}

func handleClaudeTag(m *mockAnthropicServer, w http.ResponseWriter, r *http.Request, sub []string) {
	if r.Header.Get("anthropic-beta") != client.BetaClaudeTag {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "expected anthropic-beta "+client.BetaClaudeTag)
		return
	}
	st := claudeTagState(m)
	if len(sub) == 0 {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
		return
	}
	switch sub[0] {
	case "bundles":
		st.handleBundles(w, r, sub[1:])
	case "slack_workspaces":
		st.handleWorkspaces(w, r, sub[1:])
	case "slack_channels":
		st.handleChannels(w, r, sub[1:])
	default:
		writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
	}
}

func (st *claudeTagMockState) handleBundles(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 && r.Method == http.MethodGet {
		list := make([]client.ClaudeTagBundle, 0, len(st.bundles))
		for _, b := range st.bundles {
			list = append(list, b)
		}
		writeList(w, list)
		return
	}
	b, ok := st.bundles[sub[0]]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "bundle not found")
		return
	}
	if len(sub) == 1 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, b)
		return
	}
	if len(sub) < 2 || sub[1] != "targets" {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "not found")
		return
	}
	targets := st.targets[b.ID]
	if targets == nil {
		targets = map[string]client.ClaudeTagBundleTarget{}
		st.targets[b.ID] = targets
	}
	if len(sub) == 2 && r.Method == http.MethodPost {
		var body client.CreateClaudeTagBundleTargetRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		req := client.ClaudeTagBundleTarget{Type: body.Type, SlackChannelID: body.SlackChannelID, SlackWorkspaceID: body.SlackWorkspaceID}
		switch {
		case req.Type == "slack_channel" && st.channels[req.SlackChannelID] != nil && req.SlackWorkspaceID == "":
		case req.Type == "slack_workspace" && st.workspaces[req.SlackWorkspaceID] != nil && req.SlackChannelID == "":
		default:
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "unknown or mismatched target")
			return
		}
		for _, t := range targets {
			if t.Type == req.Type && t.SlackChannelID == req.SlackChannelID && t.SlackWorkspaceID == req.SlackWorkspaceID {
				writeJSON(w, http.StatusOK, t)
				return
			}
		}
		st.nextID++
		req.ID = fmt.Sprintf("btgt_%d", st.nextID)
		req.CreatedAt = "2026-10-02T00:00:00Z"
		targets[req.ID] = req
		writeJSON(w, http.StatusOK, req)
		return
	}
	if len(sub) == 3 {
		t, ok := targets[sub[2]]
		if !ok {
			writeAPIError(w, http.StatusNotFound, "not_found_error", "bundle target not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, t)
		case http.MethodDelete:
			delete(targets, t.ID)
			writeJSON(w, http.StatusOK, map[string]string{"id": t.ID, "type": "bundle_target_deleted"})
		}
	}
}

func (st *claudeTagMockState) handleWorkspaces(w http.ResponseWriter, r *http.Request, sub []string) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "not supported by mock")
		return
	}
	if len(sub) == 0 {
		filter := r.URL.Query().Get("slack_workspace_id")
		list := []client.ClaudeTagSlackWorkspace{}
		for _, ws := range st.workspaces {
			if filter == "" || ws.Slack.WorkspaceID == filter {
				list = append(list, *ws)
			}
		}
		writeList(w, list)
		return
	}
	ws, ok := st.workspaces[sub[0]]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

// applySettings mimics the partial update: absent keys are unchanged, null clears, reply_mode null never clears.
func applySettings(s *client.ClaudeTagSettings, raw map[string]json.RawMessage) error {
	for k, v := range raw {
		null := string(v) == "null"
		switch k {
		case "enabled":
			s.Enabled = nil
			if !null {
				s.Enabled = new(bool)
				_ = json.Unmarshal(v, s.Enabled)
			}
		case "environment_id", "model", "instructions":
			var p *string
			if !null {
				p = new(string)
				_ = json.Unmarshal(v, p)
				if k == "instructions" {
					*p = strings.TrimSpace(*p)
				}
			}
			switch k {
			case "environment_id":
				s.EnvironmentID = p
			case "model":
				s.Model = p
			default:
				s.Instructions = p
			}
		case "guest_policy":
			s.GuestPolicy = nil
			if !null {
				s.GuestPolicy = &client.ClaudeTagTypeRef{}
				_ = json.Unmarshal(v, s.GuestPolicy)
			}
		case "reply_mode":
			if null {
				if s.ReplyMode != nil {
					return fmt.Errorf("reply_mode cannot be cleared")
				}
				continue
			}
			s.ReplyMode = &client.ClaudeTagTypeRef{}
			_ = json.Unmarshal(v, s.ReplyMode)
		default:
			return fmt.Errorf("unknown setting %q", k)
		}
	}
	return nil
}

func (st *claudeTagMockState) handleChannels(w http.ResponseWriter, r *http.Request, sub []string) {
	if len(sub) == 0 {
		switch r.Method {
		case http.MethodGet:
			q := r.URL.Query()
			list := []client.ClaudeTagSlackChannel{}
			for _, ch := range st.channels {
				if ch.Slack.WorkspaceID == q.Get("slack_workspace_id") && (q.Get("slack_channel_id") == "" || ch.Slack.ChannelID == q.Get("slack_channel_id")) {
					list = append(list, *ch)
				}
			}
			writeList(w, list)
		case http.MethodPost:
			var req struct {
				Slack    client.ClaudeTagSlackIdentifiers `json:"slack"`
				Settings map[string]json.RawMessage       `json:"settings"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			for _, ch := range st.channels {
				if ch.Slack == req.Slack {
					writeJSON(w, http.StatusOK, ch)
					return
				}
			}
			if st.workspaceBySlackID(req.Slack.WorkspaceID) == nil || req.Slack.ChannelID == "" {
				writeAPIError(w, http.StatusNotFound, "not_found_error", "slack channel not found")
				return
			}
			st.nextID++
			ch := &client.ClaudeTagSlackChannel{
				ID:        fmt.Sprintf("slch_%d", st.nextID),
				Slack:     req.Slack,
				CreatedAt: "2026-10-02T00:00:00Z",
				UpdatedAt: "2026-10-02T00:00:00Z",
				Type:      "slack_channel",
			}
			if err := applySettings(&ch.Settings, req.Settings); err != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
				return
			}
			st.channels[ch.ID] = ch
			writeJSON(w, http.StatusOK, ch)
		}
		return
	}

	ch, ok := st.channels[sub[0]]
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found_error", "slack channel not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("include_inherited") == "true" {
			out := *ch
			if out.Settings.Enabled == nil {
				out.Settings.Enabled = st.workspaceBySlackID(ch.Slack.WorkspaceID).Settings.Enabled
			}
			writeJSON(w, http.StatusOK, out)
			return
		}
		writeJSON(w, http.StatusOK, ch)
	case http.MethodPost:
		var req struct {
			Settings map[string]json.RawMessage `json:"settings"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Settings == nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "settings is required")
			return
		}
		updated := ch.Settings
		if err := applySettings(&updated, req.Settings); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		ch.Settings = updated
		ch.UpdatedAt = "2026-10-03T00:00:00Z"
		writeJSON(w, http.StatusOK, ch)
	case http.MethodDelete:
		st.channelDeletes++
		writeAPIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "provider must not delete Claude Tag channels")
	}
}

func (st *claudeTagMockState) workspaceBySlackID(slackID string) *client.ClaudeTagSlackWorkspace {
	for _, ws := range st.workspaces {
		if ws.Slack.WorkspaceID == slackID {
			return ws
		}
	}
	return nil
}

func TestClaudeTagResourcesAndDataSources(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			mock.mu.Lock()
			defer mock.mu.Unlock()
			st := claudeTagState(mock)
			if len(st.targets["capp_test01"]) != 0 {
				return fmt.Errorf("claude tag bundle targets left after destroy")
			}
			if st.channelDeletes != 0 || len(st.channels) == 0 {
				return fmt.Errorf("destroy must leave claude tag channels in place (deletes sent: %d)", st.channelDeletes)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: mock.providerConfig() + `
data "anthropic_claude_tag_bundles" "all" {}

data "anthropic_claude_tag_bundle" "one" {
  id = "capp_test01"
}

data "anthropic_claude_tag_slack_workspaces" "by_slack_id" {
  slack_workspace_id = "T00000000"
}

data "anthropic_claude_tag_slack_workspace" "one" {
  id = "slws_test01"
}

resource "anthropic_claude_tag_slack_channel" "eng" {
  slack_workspace_id = "T00000000"
  slack_channel_id   = "C00000000"
  enabled            = true
  model              = "claude-example-model"
  guest_policy       = "restrict"
  reply_mode         = "mention_only"
  instructions       = <<-EOT
    Answer briefly.
  EOT
}

resource "anthropic_claude_tag_bundle_target" "channel" {
  bundle_id        = data.anthropic_claude_tag_bundle.one.id
  slack_channel_id = anthropic_claude_tag_slack_channel.eng.id
}

resource "anthropic_claude_tag_bundle_target" "workspace" {
  bundle_id          = "capp_test01"
  slack_workspace_id = data.anthropic_claude_tag_slack_workspace.one.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_bundles.all", "bundles.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_bundle.one", "display_name", "Example"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_bundle.one", "description", "Example bundle"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_slack_workspaces.by_slack_id", "workspaces.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_slack_workspaces.by_slack_id", "workspaces.0.id", "slws_test01"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_slack_workspace.one", "enabled", "true"),
					resource.TestCheckNoResourceAttr("data.anthropic_claude_tag_slack_workspace.one", "model"),
					resource.TestCheckResourceAttrSet("anthropic_claude_tag_slack_channel.eng", "id"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_slack_channel.eng", "enabled", "true"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_slack_channel.eng", "guest_policy", "restrict"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_slack_channel.eng", "reply_mode", "mention_only"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_slack_channel.eng", "instructions", "Answer briefly.\n"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_bundle_target.channel", "type", "slack_channel"),
					resource.TestCheckNoResourceAttr("anthropic_claude_tag_bundle_target.channel", "slack_workspace_id"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_bundle_target.workspace", "type", "slack_workspace"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_bundle_target.workspace", "slack_workspace_id", "slws_test01"),
				),
			},
			// Clear model, change guest policy and instructions, drop reply_mode (kept), and disable so destroy is allowed.
			{
				Config: mock.providerConfig() + `
resource "anthropic_claude_tag_slack_channel" "eng" {
  slack_workspace_id = "T00000000"
  slack_channel_id   = "C00000000"
  enabled            = false
  guest_policy       = "allow"
  instructions       = "Answer in full sentences."
}

resource "anthropic_claude_tag_bundle_target" "channel" {
  bundle_id        = "capp_test01"
  slack_channel_id = anthropic_claude_tag_slack_channel.eng.id
}

data "anthropic_claude_tag_slack_channels" "all" {
  slack_workspace_id = "T00000000"
  slack_channel_id   = "C00000000"
  include_inherited  = true
  depends_on         = [anthropic_claude_tag_slack_channel.eng]
}

data "anthropic_claude_tag_slack_channel" "one" {
  id = anthropic_claude_tag_slack_channel.eng.id
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("anthropic_claude_tag_slack_channel.eng", "enabled", "false"),
					resource.TestCheckNoResourceAttr("anthropic_claude_tag_slack_channel.eng", "model"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_slack_channel.eng", "guest_policy", "allow"),
					resource.TestCheckResourceAttr("anthropic_claude_tag_slack_channel.eng", "reply_mode", "mention_only"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_slack_channels.all", "channels.#", "1"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_slack_channels.all", "channels.0.slack_channel_id", "C00000000"),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_slack_channel.one", "instructions", "Answer in full sentences."),
					resource.TestCheckResourceAttr("data.anthropic_claude_tag_slack_channel.one", "reply_mode", "mention_only"),
					func(s *terraform.State) error {
						mock.mu.Lock()
						defer mock.mu.Unlock()
						if n := len(claudeTagState(mock).targets["capp_test01"]); n != 1 {
							return fmt.Errorf("expected workspace bundle target detached, %d targets left", n)
						}
						return nil
					},
				),
			},
			{
				ResourceName:      "anthropic_claude_tag_slack_channel.eng",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "anthropic_claude_tag_bundle_target.channel",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["anthropic_claude_tag_bundle_target.channel"]
					return rs.Primary.Attributes["bundle_id"] + "/" + rs.Primary.ID, nil
				},
			},
		},
	})
}

func TestClaudeTagSlackChannelDestroyLeavesChannel(t *testing.T) {
	mock := newMockAnthropicServer(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Channel inherits enabled = true from the workspace.
				Config: mock.providerConfig() + `
resource "anthropic_claude_tag_slack_channel" "eng" {
  slack_workspace_id = "T00000000"
  slack_channel_id   = "C00000000"
}
`,
				Check: resource.TestCheckNoResourceAttr("anthropic_claude_tag_slack_channel.eng", "enabled"),
			},
			{
				Config: mock.providerConfig(),
				Check: func(_ *terraform.State) error {
					mock.mu.Lock()
					defer mock.mu.Unlock()
					st := claudeTagState(mock)
					if st.channelDeletes != 0 || len(st.channels) != 1 {
						return fmt.Errorf("destroy must not delete the channel: deletes=%d channels=%d", st.channelDeletes, len(st.channels))
					}
					return nil
				},
			},
		},
	})
}
