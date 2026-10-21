# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# Native Terraform 1.7+ mock_provider test suite for Claude Console resources.

mock_provider "anthropic" {}

run "console_workspace_and_invite_plan" {
  command = plan

  assert {
    condition     = anthropic_workspace.engineering.name == "platform-engineering"
    error_message = "Expected workspace name to be platform-engineering"
  }

  assert {
    condition     = anthropic_workspace.engineering.data_residency.default_inference_geo == "us"
    error_message = "Expected default_inference_geo to be us"
  }

  assert {
    condition     = anthropic_invite.developer.role == "developer"
    error_message = "Expected invite role to be developer"
  }
}
