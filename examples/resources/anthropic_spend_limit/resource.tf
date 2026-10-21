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
