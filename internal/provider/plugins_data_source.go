// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &PluginsDataSource{}
	_ datasource.DataSourceWithConfigure = &PluginsDataSource{}
)

func NewPluginsDataSource() datasource.DataSource {
	return &PluginsDataSource{}
}

type PluginsDataSource struct {
	client *client.Client
}

type PluginsDataSourceModel struct {
	MarketplaceID types.String  `tfsdk:"marketplace_id"`
	OwnerType     types.String  `tfsdk:"owner_type"`
	OwnerUserID   types.String  `tfsdk:"owner_user_id"`
	Plugins       []PluginModel `tfsdk:"plugins"`
}

func (d *PluginsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugins"
}

func (d *PluginsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Lists Claude Enterprise plugins, including those in members' personal marketplaces (`GET /v1/organizations/plugins`)." + pluginsEnterpriseNote,
		Attributes: map[string]schema.Attribute{
			"marketplace_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only plugins in this marketplace (`marketplace_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"owner_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only plugins with this owner: `organization` or `user`.",
				Validators:          []validator.String{stringvalidator.OneOf("organization", "user")},
			},
			"owner_user_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only plugins in this member's personal marketplaces (`user_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"plugins": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Plugins matching the filters.",
				NestedObject:        schema.NestedAttributeObject{Attributes: pluginDataSourceAttributes()},
			},
		},
	}
}

func (d *PluginsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = pluginDataSourceClient(req, resp)
}

func (d *PluginsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PluginsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	q := url.Values{}
	for k, v := range map[string]types.String{"marketplace_id": state.MarketplaceID, "owner_type": state.OwnerType, "owner_user_id": state.OwnerUserID} {
		if v.ValueString() != "" {
			q.Set(k, v.ValueString())
		}
	}
	list, err := d.client.ListPlugins(ctx, q)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Plugins", err.Error())
		return
	}
	state.Plugins = make([]PluginModel, 0, len(list))
	for i := range list {
		state.Plugins = append(state.Plugins, mapPlugin(&list[i]))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
