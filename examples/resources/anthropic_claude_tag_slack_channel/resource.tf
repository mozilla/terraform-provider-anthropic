# Set a Slack channel up for Claude Tag. The Slack workspace must already be
# connected to the organization in claude.ai.
resource "anthropic_claude_tag_slack_channel" "example" {
  slack_workspace_id = "T00000000"
  slack_channel_id   = "C00000000"

  enabled      = true
  model        = "claude-example-model"
  guest_policy = "restrict"
  reply_mode   = "mention_only"
  instructions = <<-EOT
    Answer briefly and link to sources.
  EOT
}
