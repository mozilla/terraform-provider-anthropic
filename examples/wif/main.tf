# Workload Identity Federation (WIF) & Service Account Example
# Requires provider authentication via `oauth_token` (with `org:admin` scope) or `federation`.

resource "anthropic_workspace" "platform" {
  name = "platform-automation"
}

resource "anthropic_service_account" "github_ci" {
  name              = "github-actions-ci"
  organization_role = "developer"
  description       = "Service account assumed by GitHub Actions workflows via WIF"
}

resource "anthropic_service_account_workspace" "github_ci_platform" {
  service_account_id = anthropic_service_account.github_ci.id
  workspace_id       = anthropic_workspace.platform.id
  workspace_role     = "workspace_developer"
}

resource "anthropic_federation_issuer" "github_actions" {
  name       = "github-actions"
  issuer_url = "https://token.actions.githubusercontent.com"
  jwks = {
    type = "discovery"
  }
}

resource "anthropic_federation_rule" "github_main" {
  name                      = "github-main-branch"
  issuer_id                 = anthropic_federation_issuer.github_actions.id
  service_account_id        = anthropic_service_account.github_ci.id
  oauth_scope               = "workspace:developer"
  applies_to_all_workspaces = false
  workspace_ids             = [anthropic_workspace.platform.id]
  match = {
    audience = "https://api.anthropic.com"
    claims = {
      repository         = "example/platform"
      ref                = "refs/heads/main"
      runner_environment = "github-hosted"
    }
  }
  description = "Allow main branch workflows in example/platform to exchange OIDC tokens"
}

