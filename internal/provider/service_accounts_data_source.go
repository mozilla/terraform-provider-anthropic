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
	_ datasource.DataSource              = &ServiceAccountsDataSource{}
	_ datasource.DataSourceWithConfigure = &ServiceAccountsDataSource{}
)

func NewServiceAccountsDataSource() datasource.DataSource {
	return &ServiceAccountsDataSource{}
}

type ServiceAccountsDataSource struct {
	client *client.Client
}

type ServiceAccountsDataSourceModel struct {
	IncludeArchived types.Bool                    `tfsdk:"include_archived"`
	ServiceAccounts []ServiceAccountResourceModel `tfsdk:"service_accounts"`
}

func (d *ServiceAccountsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_accounts"
}

func (d *ServiceAccountsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists Anthropic organization service accounts (`GET /v1/organizations/service_accounts`).",
		Attributes: map[string]schema.Attribute{
			"include_archived": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether to include archived service accounts in the result list.",
			},
			"service_accounts": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of organization service accounts.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Unique tagged identifier of the service account (`svac_...`).",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Slug name of the service account.",
						},
						"organization_role": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Organization-level role assigned to the service account.",
						},
						"description": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Description of the service account, or `null`.",
						},
						"created_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the service account was created.",
						},
						"archived_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the service account was archived, or `null` if active.",
						},
						"type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type (`service_account`).",
						},
					},
				},
			},
		},
	}
}

func (d *ServiceAccountsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ServiceAccountsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ServiceAccountsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	includeArchived := !state.IncludeArchived.IsNull() && state.IncludeArchived.ValueBool()
	items, err := d.client.ListServiceAccounts(ctx, includeArchived)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Service Accounts", err.Error())
		return
	}

	state.ServiceAccounts = make([]ServiceAccountResourceModel, 0, len(items))
	for i := range items {
		var m ServiceAccountResourceModel
		populateServiceAccountModel(&m, &items[i])
		state.ServiceAccounts = append(state.ServiceAccounts, m)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
