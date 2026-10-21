data "anthropic_rbac_roles" "all" {}

# Manage an API-created Claude Enterprise RBAC group
resource "anthropic_rbac_group" "security_engineers" {
  name     = "Security-Engineers"
  role_ids = [for r in data.anthropic_rbac_roles.all.roles : r.id if r.name == "Claude Code User"]
}

resource "anthropic_rbac_group_member" "alice" {
  group_id = anthropic_rbac_group.security_engineers.id
  user_id  = "user_01J9ABCDEF"
}
