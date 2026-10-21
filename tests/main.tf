# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# Root configuration fixture used by `terraform test` in tests/*.tftest.hcl.

terraform {
  required_providers {
    anthropic = {
      source = "mozilla/anthropic"
    }
  }
}

resource "anthropic_workspace" "engineering" {
  name = "platform-engineering"

  data_residency = {
    default_inference_geo  = "us"
    allowed_inference_geos = ["unrestricted"]
  }
}

resource "anthropic_invite" "developer" {
  email = "dev@example.com"
  role  = "developer"
}

resource "anthropic_rbac_group" "security" {
  name = "Security-Engineering"
}

resource "anthropic_spend_limit" "capped_user" {
  user_id = "user_01JCAPPED"
  period  = "monthly"
  amount  = "15000"
}

resource "anthropic_spend_limit" "unlimited_user" {
  user_id   = "user_01JUNLIMITED"
  period    = "monthly"
  unlimited = true
}

resource "anthropic_service_account" "ci_runner" {
  name              = "ci-runner"
  organization_role = "developer"
  description       = "Automated CI service account"
}

resource "anthropic_service_account_workspace" "ci_runner_eng" {
  service_account_id = anthropic_service_account.ci_runner.id
  workspace_id       = anthropic_workspace.engineering.id
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
  name                      = "github-main-deploy"
  issuer_id                 = anthropic_federation_issuer.github_actions.id
  service_account_id        = anthropic_service_account.ci_runner.id
  oauth_scope               = "workspace:developer"
  applies_to_all_workspaces = false
  workspace_ids             = [anthropic_workspace.engineering.id]
  match = {
    subject_prefix = "repo:example/app:ref:refs/heads/main"
    claims = {
      repository_owner = "example"
    }
  }
  description = "Allow main branch workflows to exchange OIDC tokens"
}

