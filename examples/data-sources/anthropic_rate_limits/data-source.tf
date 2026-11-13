data "anthropic_rate_limits" "all" {}

data "anthropic_rate_limits" "model_groups" {
  group_type = "model_group"
}
