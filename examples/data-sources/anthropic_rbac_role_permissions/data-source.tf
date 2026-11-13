data "anthropic_rbac_roles" "all" {}

data "anthropic_rbac_role_permissions" "first" {
  role_id = data.anthropic_rbac_roles.all.roles[0].id
}
