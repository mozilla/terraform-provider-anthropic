# Organization-wide setting for one plugin
resource "anthropic_plugin_installation_setting" "org" {
  plugin_id               = "plugin_01EXAMPLE"
  target                  = "organization"
  installation_preference = "available"
}

# Setting for one RBAC Group, which overrides the organization-wide setting for its members
resource "anthropic_plugin_installation_setting" "engineering" {
  plugin_id               = "plugin_01EXAMPLE"
  target                  = "grp_01EXAMPLE"
  installation_preference = "required"
}
