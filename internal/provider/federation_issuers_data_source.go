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
	_ datasource.DataSource              = &FederationIssuersDataSource{}
	_ datasource.DataSourceWithConfigure = &FederationIssuersDataSource{}
)

func NewFederationIssuersDataSource() datasource.DataSource {
	return &FederationIssuersDataSource{}
}

type FederationIssuersDataSource struct {
	client *client.Client
}

type FederationIssuersDataSourceModel struct {
	IncludeArchived types.Bool                      `tfsdk:"include_archived"`
	Issuers         []FederationIssuerResourceModel `tfsdk:"issuers"`
}

func (d *FederationIssuersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_issuers"
}

func (d *FederationIssuersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists OpenID Connect (OIDC) federation issuers in the organization (`GET /v1/organizations/federation_issuers`).",
		Attributes: map[string]schema.Attribute{
			"include_archived": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether to include archived federation issuers.",
			},
			"issuers": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of federation issuers.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Unique tagged identifier (`fdis_...`).",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Slug name of the federation issuer.",
						},
						"issuer_url": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "OIDC issuer URL.",
						},
						"check_jti": schema.BoolAttribute{
							Computed:            true,
							MarkdownDescription: "Whether single-use `jti` replay tracking is enforced.",
						},
						"max_jwt_lifetime_seconds": schema.Int64Attribute{
							Computed:            true,
							MarkdownDescription: "Maximum allowed JWT assertion lifetime in seconds.",
						},
						"jwks": schema.SingleNestedAttribute{
							Computed:            true,
							MarkdownDescription: "JWKS configuration.",
							Attributes: map[string]schema.Attribute{
								"type": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "JWKS resolution strategy.",
								},
								"discovery_base": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "Override base URL for OIDC discovery, or `null`.",
								},
								"url": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "Explicit JWKS endpoint URL, or `null`.",
								},
								"ca_cert_pem": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "Custom CA certificate PEM bundle, or `null`.",
								},
							},
						},
						"created_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the issuer was created.",
						},
						"updated_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the issuer was last updated.",
						},
						"archived_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the issuer was archived, or `null` if active.",
						},
						"type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type (`federation_issuer`).",
						},
					},
				},
			},
		},
	}
}

func (d *FederationIssuersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *FederationIssuersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state FederationIssuersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	includeArchived := !state.IncludeArchived.IsNull() && state.IncludeArchived.ValueBool()
	items, err := d.client.ListFederationIssuers(ctx, includeArchived)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Federation Issuers", err.Error())
		return
	}

	state.Issuers = make([]FederationIssuerResourceModel, 0, len(items))
	for i := range items {
		var m FederationIssuerResourceModel
		populateFederationIssuerModel(&m, &items[i])
		state.Issuers = append(state.Issuers, m)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
