// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &FederationRulesDataSource{}
	_ datasource.DataSourceWithConfigure = &FederationRulesDataSource{}
)

func NewFederationRulesDataSource() datasource.DataSource {
	return &FederationRulesDataSource{}
}

type FederationRulesDataSource struct {
	client *client.Client
}

type FederationRulesDataSourceModel struct {
	IncludeArchived types.Bool                    `tfsdk:"include_archived"`
	Rules           []FederationRuleResourceModel `tfsdk:"rules"`
}

func (d *FederationRulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_rules"
}

func (d *FederationRulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists Workload Identity Federation (WIF) rules in the organization (`GET /v1/organizations/federation_rules`).",
		Attributes: map[string]schema.Attribute{
			"include_archived": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether to include archived federation rules.",
			},
			"rules": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of federation rules.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Unique tagged identifier (`fdrl_...`).",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Slug name of the federation rule.",
						},
						"description": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Free-text description of the federation rule.",
						},
						"issuer_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "ID of the associated federation issuer (`fdis_...`).",
						},
						"service_account_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Target service account ID (`svac_...`).",
						},
						"oauth_scope": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Space-separated OAuth scope(s).",
						},
						"token_lifetime_seconds": schema.Int64Attribute{
							Computed:            true,
							MarkdownDescription: "Lifetime of minted OAuth tokens in seconds.",
						},
						"applies_to_all_workspaces": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether the rule applies to all workspaces.",
						},
						"workspace_ids": schema.SetAttribute{
							ElementType:         types.StringType,
							Computed:            true,
							MarkdownDescription: "Set of workspace IDs.",
						},
						"match": schema.SingleNestedAttribute{
							Computed:            true,
							MarkdownDescription: "Claim matching conditions.",
							Attributes: map[string]schema.Attribute{
								"audience": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "Exact match against the JWT `aud` claim, or `null`.",
								},
								"subject_prefix": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "Match against the JWT `sub` claim, or `null`.",
								},
								"claims": schema.MapAttribute{
									ElementType:         types.StringType,
									Computed:            true,
									MarkdownDescription: "Exact-match `{claim: value}` pairs, or `null`.",
								},
								"condition": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "CEL expression over `claims`, or `null`.",
								},
							},
						},
						"created_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the rule was created.",
						},
						"updated_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the rule was last updated.",
						},
						"archived_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the rule was archived, or `null` if active.",
						},
						"type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type (`federation_rule`).",
						},
					},
				},
			},
		},
	}
}

func (d *FederationRulesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected DataSource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}
	d.client = c
}

func (d *FederationRulesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state FederationRulesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	includeArchived := !state.IncludeArchived.IsNull() && state.IncludeArchived.ValueBool()
	items, err := d.client.ListFederationRules(ctx, includeArchived)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Federation Rules", err.Error())
		return
	}

	state.Rules = make([]FederationRuleResourceModel, 0, len(items))
	for i := range items {
		var m FederationRuleResourceModel
		resp.Diagnostics.Append(populateFederationRuleModel(ctx, &m, &items[i])...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Rules = append(state.Rules, m)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
