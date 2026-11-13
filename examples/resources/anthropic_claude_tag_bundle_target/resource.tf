# Attach a Bundle (created in claude.ai) to one channel.
resource "anthropic_claude_tag_bundle_target" "channel" {
  bundle_id        = "capp_00000000"
  slack_channel_id = anthropic_claude_tag_slack_channel.example.id
}

# Attach a Bundle to a whole Slack workspace.
data "anthropic_claude_tag_slack_workspaces" "example" {
  slack_workspace_id = "T00000000"
}

resource "anthropic_claude_tag_bundle_target" "workspace" {
  bundle_id          = "capp_00000000"
  slack_workspace_id = data.anthropic_claude_tag_slack_workspaces.example.workspaces[0].id
}
