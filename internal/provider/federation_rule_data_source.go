// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &FederationRuleDataSource{}
	_ datasource.DataSourceWithConfigure = &FederationRuleDataSource{}
)

func NewFederationRuleDataSource() datasource.DataSource {
	return &FederationRuleDataSource{}
}

type FederationRuleDataSource struct {
	client *client.Client
}

func (d *FederationRuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_rule"
}

func (d *FederationRuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a Workload Identity Federation (WIF) rule by ID (`GET /v1/organizations/federation_rules/{id}`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique tagged identifier of the federation rule (`fdrl_...`).",
				Validators: []validator.String{
					idValidator(),
				},
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
				MarkdownDescription: "Space-separated OAuth scope(s) granted on minted tokens.",
			},
			"token_lifetime_seconds": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Lifetime of minted OAuth tokens in seconds.",
			},
			"applies_to_all_workspaces": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the federation rule applies to all workspaces.",
			},
			"workspace_ids": schema.SetAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Set of workspace IDs where this federation rule is enabled.",
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
	}
}

func (d *FederationRuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *FederationRuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state FederationRuleResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fr, err := d.client.GetFederationRule(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Federation Rule", err.Error())
		return
	}

	resp.Diagnostics.Append(populateFederationRuleModel(ctx, &state, fr)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
