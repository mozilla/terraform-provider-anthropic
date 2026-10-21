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
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &FederationIssuerDataSource{}
	_ datasource.DataSourceWithConfigure = &FederationIssuerDataSource{}
)

func NewFederationIssuerDataSource() datasource.DataSource {
	return &FederationIssuerDataSource{}
}

type FederationIssuerDataSource struct {
	client *client.Client
}

func (d *FederationIssuerDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_issuer"
}

func (d *FederationIssuerDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads an OpenID Connect (OIDC) federation issuer by ID (`GET /v1/organizations/federation_issuers/{id}`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique tagged identifier of the federation issuer (`fdis_...`).",
				Validators: []validator.String{
					idValidator(),
				},
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
				MarkdownDescription: "JWKS configuration for verifying incoming JWT signatures.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Computed:            true,
						MarkdownDescription: "JWKS resolution strategy (`discovery` or `explicit_url`).",
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
	}
}

func (d *FederationIssuerDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *FederationIssuerDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state FederationIssuerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fi, err := d.client.GetFederationIssuer(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Federation Issuer", err.Error())
		return
	}

	populateFederationIssuerModel(&state, fi)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
