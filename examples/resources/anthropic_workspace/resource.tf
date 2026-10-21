resource "anthropic_workspace" "production" {
  name = "production-inference"

  data_residency = {
    workspace_geo          = "us"
    default_inference_geo  = "us"
    allowed_inference_geos = ["us", "global"]
  }
}

resource "anthropic_workspace_member" "lead_developer" {
  workspace_id   = anthropic_workspace.production.id
  user_id        = "user_01J9ABCDEF"
  workspace_role = "workspace_admin"
}
