resource "anthropic_webhook" "example" {
  url         = "https://hooks.example.com/anthropic"
  name        = "example-hook"
  description = "Example webhook endpoint"
  enabled_events = [
    "user_profile.enforcement_action_opened",
    "user_profile.enforcement_action_resolved",
  ]
}

# The signing secret is returned only on create
output "webhook_signing_secret" {
  value     = anthropic_webhook.example.signing_secret
  sensitive = true
}
