# Singleton per organization. Destroying it leaves the organization's setting unchanged.
resource "anthropic_compliance_settings" "this" {
  state = "enabled"
}
