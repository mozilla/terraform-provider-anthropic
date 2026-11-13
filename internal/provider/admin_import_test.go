// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

func TestAdminServiceAccountAndRuleCreateRejected(t *testing.T) {
	mock := newMockAnthropicServer(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: mock.wifProviderConfig() + `
resource "anthropic_service_account" "admin" {
  name              = "bootstrap-admin"
  organization_role = "admin"
}
`,
				ExpectError: regexp.MustCompile(`Admin Service Account Cannot Be Created`),
			},
			{
				Config: mock.wifProviderConfig() + `
resource "anthropic_federation_rule" "admin" {
  name                      = "bootstrap-admin"
  issuer_id                 = "fdis_01Bootstrap"
  service_account_id        = "svac_01Bootstrap"
  oauth_scope               = "org:admin"
  applies_to_all_workspaces = true
  match = {
    subject_prefix = "repo:example/infra:ref:refs/heads/main"
  }
}
`,
				ExpectError: regexp.MustCompile(`org:admin Federation Rule Cannot Be Created`),
			},
		},
	})
}

func TestAdminServiceAccountAndRuleImport(t *testing.T) {
	mock := newMockAnthropicServer(t)

	desc := "Created in the Console"
	sub := "repo:example/infra:ref:refs/heads/main"
	mock.mu.Lock()
	mock.serviceAccounts["svac_01Bootstrap"] = client.ServiceAccount{
		ID:               "svac_01Bootstrap",
		Name:             "bootstrap-admin",
		OrganizationRole: "admin",
		Description:      &desc,
		CreatedAt:        "2026-01-01T00:00:00Z",
		Type:             "service_account",
	}
	mock.federationRules["fdrl_01Bootstrap"] = client.FederationRule{
		ID:                     "fdrl_01Bootstrap",
		Name:                   "bootstrap-admin",
		IssuerID:               "fdis_01Bootstrap",
		Target:                 client.ServiceAccountTarget("svac_01Bootstrap"),
		OAuthScope:             "org:admin",
		TokenLifetimeSeconds:   3600,
		AppliesToAllWorkspaces: true,
		WorkspaceIDs:           []string{},
		Match:                  client.FederationRuleMatch{SubjectPrefix: &sub},
		CreatedAt:              "2026-01-01T00:00:00Z",
		UpdatedAt:              "2026-01-01T00:00:00Z",
		Type:                   "federation_rule",
	}
	mock.mu.Unlock()

	config := mock.wifProviderConfig() + `
resource "anthropic_service_account" "admin" {
  name              = "bootstrap-admin"
  organization_role = "admin"
  description       = "Created in the Console"
}

resource "anthropic_federation_rule" "admin" {
  name                      = "bootstrap-admin"
  issuer_id                 = "fdis_01Bootstrap"
  service_account_id        = anthropic_service_account.admin.id
  oauth_scope               = "org:admin"
  applies_to_all_workspaces = true
  match = {
    subject_prefix = "repo:example/infra:ref:refs/heads/main"
  }
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config,
				ResourceName:       "anthropic_service_account.admin",
				ImportState:        true,
				ImportStateId:      "svac_01Bootstrap",
				ImportStatePersist: true,
			},
			{
				Config:             config,
				ResourceName:       "anthropic_federation_rule.admin",
				ImportState:        true,
				ImportStateId:      "fdrl_01Bootstrap",
				ImportStatePersist: true,
			},
			{
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
