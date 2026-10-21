# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# Native Terraform 1.7+ mock_provider test suite for Claude Enterprise resources.

mock_provider "anthropic" {}

run "enterprise_rbac_and_spend_limits_plan" {
  command = plan

  assert {
    condition     = anthropic_rbac_group.security.name == "Security-Engineering"
    error_message = "Expected RBAC group name to be Security-Engineering"
  }

  assert {
    condition     = anthropic_spend_limit.capped_user.amount == "15000"
    error_message = "Expected capped spend limit amount to be 15000 cents ($150.00)"
  }

  assert {
    condition     = anthropic_spend_limit.unlimited_user.unlimited == true
    error_message = "Expected unlimited spend limit to have unlimited = true"
  }
}
