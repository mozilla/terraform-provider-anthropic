# Look a workspace up by Slack's own ID.
data "anthropic_claude_tag_slack_workspaces" "example" {
  slack_workspace_id = "T00000000"
}
