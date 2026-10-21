# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

mock_provider "anthropic" {}

run "wif_service_account_issuer_and_rule" {
  command = plan

  assert {
    condition     = anthropic_service_account.ci_runner.name == "ci-runner"
    error_message = "Expected service account name to be ci-runner."
  }

  assert {
    condition     = anthropic_service_account.ci_runner.organization_role == "developer"
    error_message = "Expected service account organization_role to be developer."
  }

  assert {
    condition     = anthropic_service_account_workspace.ci_runner_eng.workspace_role == "workspace_developer"
    error_message = "Expected service account workspace_role to be workspace_developer."
  }

  assert {
    condition     = anthropic_federation_issuer.github_actions.issuer_url == "https://token.actions.githubusercontent.com"
    error_message = "Expected federation issuer_url to match GitHub OIDC issuer."
  }

  assert {
    condition     = anthropic_federation_issuer.github_actions.jwks.type == "discovery"
    error_message = "Expected federation issuer jwks.type to be discovery."
  }

  assert {
    condition     = anthropic_federation_rule.github_main.oauth_scope == "workspace:developer"
    error_message = "Expected federation rule oauth_scope to be workspace:developer."
  }

  assert {
    condition     = anthropic_federation_rule.github_main.match.subject_prefix == "repo:example/app:ref:refs/heads/main"
    error_message = "Expected federation rule match.subject_prefix to match main branch pattern."
  }

  assert {
    condition     = anthropic_federation_rule.github_main.match.claims["repository_owner"] == "example"
    error_message = "Expected federation rule match.claims.repository_owner to be example."
  }
}

