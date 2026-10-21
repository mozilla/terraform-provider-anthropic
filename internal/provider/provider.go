// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var _ provider.Provider = &AnthropicProvider{}

// AnthropicProvider implements the Terraform provider for Claude Console, Claude Enterprise, and WIF.
type AnthropicProvider struct {
	version string
}

// FederationProviderModel describes the Workload Identity Federation (WIF) configuration block.
type FederationProviderModel struct {
	FederationRuleID  types.String `tfsdk:"federation_rule_id"`
	OrganizationID    types.String `tfsdk:"organization_id"`
	ServiceAccountID  types.String `tfsdk:"service_account_id"`
	WorkspaceID       types.String `tfsdk:"workspace_id"`
	IdentityToken     types.String `tfsdk:"identity_token"`
	IdentityTokenFile types.String `tfsdk:"identity_token_file"`
}

// AnthropicProviderModel describes the provider configuration schema.
type AnthropicProviderModel struct {
	APIKey     types.String             `tfsdk:"api_key"`
	OAuthToken types.String             `tfsdk:"oauth_token"`
	Federation *FederationProviderModel `tfsdk:"federation"`
	BaseURL    types.String             `tfsdk:"base_url"`
	MaxRetries types.Int64              `tfsdk:"max_retries"`
}

// New returns a factory function for the AnthropicProvider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AnthropicProvider{
			version: version,
		}
	}
}

func (p *AnthropicProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "anthropic"
	resp.Version = p.version
}

func (p *AnthropicProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Anthropic provider manages administrative resources across **Claude Console** " +
			"(Workspaces, Workspace Members, Users, Invites, API Keys), **Claude Enterprise** " +
			"(RBAC Groups, RBAC Group Members, RBAC Roles, Per-User Spend Limits), and " +
			"**Workload Identity Federation (WIF)** (Service Accounts, Federation Issuers, Federation Rules) " +
			"using the official Anthropic Go SDK (`github.com/anthropics/anthropic-sdk-go`).",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Anthropic Admin API key (`sk-ant-admin...`). " +
					"Defaults to `ANTHROPIC_ADMIN_API_KEY`. " +
					"Note: Service Account and Workload Identity Federation endpoints reject static Admin API keys and require `oauth_token` or `federation`.",
			},
			"oauth_token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Anthropic OAuth access token (`sk-ant-oat01-...`) with `org:admin` scope. " +
					"Defaults to `ANTHROPIC_OAUTH_TOKEN` (or `ANTHROPIC_AUTH_TOKEN`). Required when managing Service Accounts or Federation resources without WIF.",
			},
			"federation": schema.SingleNestedAttribute{
				Optional: true,
				MarkdownDescription: "Workload Identity Federation (WIF) configuration for exchanging an external OIDC JWT " +
					"(e.g. GitHub Actions, GCP, AWS, Kubernetes) for a short-lived Anthropic OAuth access token via `POST /v1/oauth/token`.",
				Attributes: map[string]schema.Attribute{
					"federation_rule_id": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Federation rule ID (`fdrl_...`). Defaults to `ANTHROPIC_FEDERATION_RULE_ID`.",
					},
					"organization_id": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Anthropic organization UUID. Defaults to `ANTHROPIC_ORGANIZATION_ID`.",
					},
					"service_account_id": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Optional target service account ID (`svac_...`). Defaults to `ANTHROPIC_SERVICE_ACCOUNT_ID`.",
					},
					"workspace_id": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Optional workspace ID (`wrkspc_...` or `default`). Defaults to `ANTHROPIC_WORKSPACE_ID`.",
					},
					"identity_token": schema.StringAttribute{
						Optional:            true,
						Sensitive:           true,
						MarkdownDescription: "Inline OIDC JWT assertion. Defaults to `ANTHROPIC_IDENTITY_TOKEN`. Mutually exclusive with `identity_token_file`.",
					},
					"identity_token_file": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Path to a file containing a rotating OIDC JWT assertion. Defaults to `ANTHROPIC_IDENTITY_TOKEN_FILE`.",
					},
				},
			},
			"base_url": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base URL for the Anthropic API. " +
					"Defaults to `https://api.anthropic.com`. Must use `https`, except `http` on a loopback host for local testing. Intentionally does not read `ANTHROPIC_BASE_URL` from the environment to prevent credential exfiltration via ambient proxy variables.",
			},
			"max_retries": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Maximum number of retries on HTTP 429 rate-limit or 5xx/529 transient errors. Defaults to `4`.",
				Validators: []validator.Int64{
					int64validator.Between(0, 10),
				},
			},
		},
	}
}

func (p *AnthropicProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config AnthropicProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown Anthropic Admin API Key",
			"The provider cannot create the Anthropic API client as there is an unknown configuration value for api_key.",
		)
	}
	if config.OAuthToken.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("oauth_token"),
			"Unknown Anthropic OAuth Token",
			"The provider cannot create the Anthropic API client as there is an unknown configuration value for oauth_token.",
		)
	}
	if config.BaseURL.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("base_url"),
			"Unknown Anthropic Base URL",
			"The provider cannot create the Anthropic API client as there is an unknown configuration value for base_url.",
		)
	}
	// An unknown federation value must not fall back to ANTHROPIC_* env vars, which may name a different identity.
	if f := config.Federation; f != nil {
		for name, v := range map[string]types.String{
			"federation_rule_id":  f.FederationRuleID,
			"organization_id":     f.OrganizationID,
			"service_account_id":  f.ServiceAccountID,
			"workspace_id":        f.WorkspaceID,
			"identity_token":      f.IdentityToken,
			"identity_token_file": f.IdentityTokenFile,
		} {
			if v.IsUnknown() {
				resp.Diagnostics.AddAttributeError(
					path.Root("federation").AtName(name),
					"Unknown Federation Configuration Value",
					fmt.Sprintf("The provider cannot create the Anthropic API client as there is an unknown configuration value for federation.%s.", name),
				)
			}
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	baseURL := client.DefaultBaseURL
	if !config.BaseURL.IsNull() {
		if trimmed := strings.TrimSpace(config.BaseURL.ValueString()); trimmed != "" {
			baseURL = trimmed
		}
	}

	maxRetries := client.DefaultMaxRetries
	if !config.MaxRetries.IsNull() && !config.MaxRetries.IsUnknown() {
		maxRetries = int(config.MaxRetries.ValueInt64())
	}

	// Resolve explicit HCL configuration first, then fall back to environment variables.
	var apiKey string
	if !config.APIKey.IsNull() {
		apiKey = strings.TrimSpace(config.APIKey.ValueString())
	}
	var oauthToken string
	if !config.OAuthToken.IsNull() {
		oauthToken = strings.TrimSpace(config.OAuthToken.ValueString())
	}
	var fedCfg *client.FederationConfig
	if config.Federation != nil {
		fedCfg = resolveFederationConfig(config.Federation)
	}

	// If no explicit auth mode was set in HCL, resolve from environment variables
	// with precedence: WIF env vars -> OAuth token env vars -> Admin API key env var.
	if apiKey == "" && oauthToken == "" && fedCfg == nil {
		if envRuleID := strings.TrimSpace(os.Getenv("ANTHROPIC_FEDERATION_RULE_ID")); envRuleID != "" {
			fedCfg = resolveFederationConfig(nil)
		} else if envOAuth := firstNonEmptyEnv("ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN"); envOAuth != "" {
			oauthToken = envOAuth
		} else if envKey := strings.TrimSpace(os.Getenv("ANTHROPIC_ADMIN_API_KEY")); envKey != "" {
			apiKey = envKey
		}
	}

	if apiKey == "" && oauthToken == "" && fedCfg == nil {
		resp.Diagnostics.AddError(
			"Missing Anthropic Authentication Credentials",
			"Configure one of `api_key` (ANTHROPIC_ADMIN_API_KEY), `oauth_token` (ANTHROPIC_OAUTH_TOKEN), "+
				"or `federation` (ANTHROPIC_FEDERATION_RULE_ID, ANTHROPIC_ORGANIZATION_ID, ANTHROPIC_IDENTITY_TOKEN_FILE).",
		)
		return
	}

	if apiKey != "" && !strings.HasPrefix(apiKey, "sk-ant-admin") {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("api_key"),
			"Non-Standard Admin API Key Prefix",
			"Anthropic Organization Admin API keys typically begin with 'sk-ant-admin'. Standard workspace API keys will be rejected by /v1/organizations/* endpoints.",
		)
	}

	c, err := client.New(client.Config{
		APIKey:     apiKey,
		OAuthToken: oauthToken,
		Federation: fedCfg,
		BaseURL:    baseURL,
		MaxRetries: maxRetries,
		UserAgent:  fmt.Sprintf("terraform-provider-anthropic/%s", p.version),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Anthropic Admin API Client", err.Error())
		return
	}

	resp.DataSourceData = c
	resp.ResourceData = c
}

func resolveFederationConfig(m *FederationProviderModel) *client.FederationConfig {
	fc := &client.FederationConfig{
		FederationRuleID:  strings.TrimSpace(os.Getenv("ANTHROPIC_FEDERATION_RULE_ID")),
		OrganizationID:    strings.TrimSpace(os.Getenv("ANTHROPIC_ORGANIZATION_ID")),
		ServiceAccountID:  strings.TrimSpace(os.Getenv("ANTHROPIC_SERVICE_ACCOUNT_ID")),
		WorkspaceID:       strings.TrimSpace(os.Getenv("ANTHROPIC_WORKSPACE_ID")),
		IdentityToken:     strings.TrimSpace(os.Getenv("ANTHROPIC_IDENTITY_TOKEN")),
		IdentityTokenFile: strings.TrimSpace(os.Getenv("ANTHROPIC_IDENTITY_TOKEN_FILE")),
	}
	if m != nil {
		if !m.FederationRuleID.IsNull() && !m.FederationRuleID.IsUnknown() {
			fc.FederationRuleID = strings.TrimSpace(m.FederationRuleID.ValueString())
		}
		if !m.OrganizationID.IsNull() && !m.OrganizationID.IsUnknown() {
			fc.OrganizationID = strings.TrimSpace(m.OrganizationID.ValueString())
		}
		if !m.ServiceAccountID.IsNull() && !m.ServiceAccountID.IsUnknown() {
			fc.ServiceAccountID = strings.TrimSpace(m.ServiceAccountID.ValueString())
		}
		if !m.WorkspaceID.IsNull() && !m.WorkspaceID.IsUnknown() {
			fc.WorkspaceID = strings.TrimSpace(m.WorkspaceID.ValueString())
		}
		if !m.IdentityToken.IsNull() && !m.IdentityToken.IsUnknown() {
			fc.IdentityToken = strings.TrimSpace(m.IdentityToken.ValueString())
			if m.IdentityTokenFile.IsNull() {
				fc.IdentityTokenFile = ""
			}
		}
		if !m.IdentityTokenFile.IsNull() && !m.IdentityTokenFile.IsUnknown() {
			fc.IdentityTokenFile = strings.TrimSpace(m.IdentityTokenFile.ValueString())
			if m.IdentityToken.IsNull() {
				fc.IdentityToken = ""
			}
		}
	}
	return fc
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func (p *AnthropicProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewUserResource,
		NewInviteResource,
		NewWorkspaceResource,
		NewWorkspaceMemberResource,
		NewAPIKeyResource,
		NewRBACGroupResource,
		NewRBACGroupMemberResource,
		NewSpendLimitResource,
		NewServiceAccountResource,
		NewServiceAccountWorkspaceResource,
		NewFederationIssuerResource,
		NewFederationRuleResource,
	}
}

func (p *AnthropicProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewOrganizationDataSource,
		NewUserDataSource,
		NewUsersDataSource,
		NewInviteDataSource,
		NewInvitesDataSource,
		NewWorkspaceDataSource,
		NewWorkspacesDataSource,
		NewWorkspaceMemberDataSource,
		NewWorkspaceMembersDataSource,
		NewAPIKeyDataSource,
		NewAPIKeysDataSource,
		NewRBACGroupDataSource,
		NewRBACGroupsDataSource,
		NewRBACRolesDataSource,
		NewServiceAccountDataSource,
		NewServiceAccountsDataSource,
		NewFederationIssuerDataSource,
		NewFederationIssuersDataSource,
		NewFederationRuleDataSource,
		NewFederationRulesDataSource,
	}
}
