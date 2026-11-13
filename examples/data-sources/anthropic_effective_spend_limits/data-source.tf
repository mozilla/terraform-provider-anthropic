data "anthropic_effective_spend_limits" "member" {
  user_ids = ["user_01EXAMPLE"]
  periods  = ["monthly"]
}
