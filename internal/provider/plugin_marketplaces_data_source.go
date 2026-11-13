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
	_ datasource.DataSource              = &PluginMarketplacesDataSource{}
	_ datasource.DataSourceWithConfigure = &PluginMarketplacesDataSource{}
)

func NewPluginMarketplacesDataSource() datasource.DataSource {
	return &PluginMarketplacesDataSource{}
}

type PluginMarketplacesDataSource struct {
	client *client.Client
}

type PluginMarketplacesDataSourceModel struct {
	OwnerType    types.String                       `tfsdk:"owner_type"`
	Source       types.String                       `tfsdk:"source"`
	Marketplaces []PluginMarketplaceDataSourceModel `tfsdk:"marketplaces"`
}

func (d *PluginMarketplacesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin_marketplaces"
}

func (d *PluginMarketplacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := pluginMarketplaceDataSourceAttributes()
	attrs["id"] = schema.StringAttribute{Computed: true, MarkdownDescription: "ID of the plugin marketplace (`marketplace_...`)."}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Lists Claude Enterprise plugin marketplaces, the organization's own and its members' personal ones (`GET /v1/organizations/plugin_marketplaces`)." + pluginsEnterpriseNote,
		Attributes: map[string]schema.Attribute{
			"owner_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only marketplaces with this owner: `organization` or `user`.",
				Validators:          []validator.String{stringvalidator.OneOf("organization", "user")},
			},
			"source": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only marketplaces with this source: `manual`, `github`, `gitlab` or `public_git`.",
				Validators:          []validator.String{stringvalidator.OneOf("manual", "github", "gitlab", "public_git")},
			},
			"marketplaces": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Plugin marketplaces, newest first.",
				NestedObject:        schema.NestedAttributeObject{Attributes: attrs},
			},
		},
	}
}

func (d *PluginMarketplacesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = pluginDataSourceClient(req, resp)
}

func (d *PluginMarketplacesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PluginMarketplacesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	q := url.Values{}
	if v := state.OwnerType.ValueString(); v != "" {
		q.Set("owner_type", v)
	}
	if v := state.Source.ValueString(); v != "" {
		q.Set("source", v)
	}
	list, err := d.client.ListPluginMarketplaces(ctx, q)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Plugin Marketplaces", err.Error())
		return
	}
	state.Marketplaces = make([]PluginMarketplaceDataSourceModel, 0, len(list))
	for i := range list {
		state.Marketplaces = append(state.Marketplaces, PluginMarketplaceDataSourceModel{
			ID:                     types.StringValue(list[i].ID),
			PluginMarketplaceModel: mapPluginMarketplace(&list[i]),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
