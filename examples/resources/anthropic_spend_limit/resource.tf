# Capped monthly spend limit ($150.00 = "15000" cents)
resource "anthropic_spend_limit" "engineer_monthly_cap" {
  user_id = "user_01J9ABCDEF"
  period  = "monthly"
  amount  = "15000"
}

# Explicit unlimited monthly spend override ("amount": null in the Anthropic API)
resource "anthropic_spend_limit" "principal_unlimited" {
  user_id   = "user_01J9XYZ123"
  period    = "monthly"
  unlimited = true
}

# Claude Console workspace spend limit (early access, monthly only, always capped)
resource "anthropic_spend_limit" "team_workspace" {
  workspace_id = "wrkspc_01EXAMPLE"
  amount       = "50000"
}

# Claude Console organization-wide spend limit (early access)
resource "anthropic_spend_limit" "organization" {
  organization = true
  amount       = "100000"
}
