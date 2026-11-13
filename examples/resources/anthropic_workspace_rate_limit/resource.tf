data "anthropic_rate_limits" "models" {
  group_type = "model_group"
}

# Override two limiter types for one rate limit group in a workspace.
# Overrides for limiter types not listed here are cleared.
resource "anthropic_workspace_rate_limit" "team_model_group" {
  workspace_id  = "wrkspc_01EXAMPLE"
  rate_limit_id = data.anthropic_rate_limits.models.rate_limits[0].id
  limits = {
    requests_per_minute     = 100
    input_tokens_per_minute = 50000
  }
}
