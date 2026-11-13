# Adopts a marketplace created in claude.ai and sets its default installation setting.
resource "anthropic_plugin_marketplace" "library" {
  marketplace_id                  = "marketplace_01EXAMPLE"
  default_installation_preference = "available"
}
