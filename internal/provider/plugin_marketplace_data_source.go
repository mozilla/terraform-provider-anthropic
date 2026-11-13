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
	_ datasource.DataSource              = &PluginMarketplaceDataSource{}
	_ datasource.DataSourceWithConfigure = &PluginMarketplaceDataSource{}
)

const pluginsEnterpriseNote = " Available to Claude Enterprise organizations only."

// PluginMarketplaceModel holds the read-only marketplace fields shared by the resource and data sources.
type PluginMarketplaceModel struct {
	Name                          types.String `tfsdk:"name"`
	Source                        types.String `tfsdk:"source"`
	OwnerType                     types.String `tfsdk:"owner_type"`
	OwnerUserID                   types.String `tfsdk:"owner_user_id"`
	DefaultInstallationPreference types.String `tfsdk:"default_installation_preference"`
	SyncStatus                    types.String `tfsdk:"sync_status"`
	LastSyncEndedAt               types.String `tfsdk:"last_sync_ended_at"`
	LastSyncReadSha               types.String `tfsdk:"last_sync_read_sha"`
	CreatedAt                     types.String `tfsdk:"created_at"`
}

type PluginMarketplaceDataSourceModel struct {
	ID types.String `tfsdk:"id"`
	PluginMarketplaceModel
}

func mapPluginMarketplace(m *client.PluginMarketplace) PluginMarketplaceModel {
	return PluginMarketplaceModel{
		Name:                          types.StringValue(m.Name),
		Source:                        types.StringValue(m.Source),
		OwnerType:                     types.StringValue(m.Owner.Type),
		OwnerUserID:                   types.StringPointerValue(m.Owner.UserID),
		DefaultInstallationPreference: types.StringPointerValue(m.DefaultInstallationPreference),
		SyncStatus:                    types.StringPointerValue(m.SyncStatus),
		LastSyncEndedAt:               types.StringPointerValue(m.LastSyncEndedAt),
		LastSyncReadSha:               types.StringPointerValue(m.LastSyncReadSha),
		CreatedAt:                     types.StringValue(m.CreatedAt),
	}
}

func pluginMarketplaceDataSourceAttributes() map[string]schema.Attribute {
	computed := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	return map[string]schema.Attribute{
		"name":                            computed("Name of the marketplace, fixed for its lifetime."),
		"source":                          computed("Where its plugins come from: `manual` (uploaded), or `github`, `gitlab`, `public_git` (synchronized from a Git repository)."),
		"owner_type":                      computed("`organization`, or `user` for a member's personal marketplace."),
		"owner_user_id":                   computed("User ID of the member who owns a personal marketplace. Null for an organization marketplace."),
		"default_installation_preference": computed("Organization-wide installation setting for plugins without their own: `required`, `auto_install`, `available` or `not_available`. Null for a personal marketplace."),
		"sync_status":                     computed("Outcome of the most recent repository synchronization. Null when never synchronized."),
		"last_sync_ended_at":              computed("RFC 3339 timestamp when the most recent synchronization attempt ended. Null when not synchronized."),
		"last_sync_read_sha":              computed("Commit the last synchronization read. Null when not synchronized."),
		"created_at":                      computed("RFC 3339 timestamp when the marketplace was created."),
	}
}

func NewPluginMarketplaceDataSource() datasource.DataSource {
	return &PluginMarketplaceDataSource{}
}

type PluginMarketplaceDataSource struct {
	client *client.Client
}

func (d *PluginMarketplaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin_marketplace"
}

func (d *PluginMarketplaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := pluginMarketplaceDataSourceAttributes()
	attrs["id"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "ID of the plugin marketplace (`marketplace_...`).",
		Validators:          []validator.String{idValidator()},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Looks up a Claude Enterprise plugin marketplace (`GET /v1/organizations/plugin_marketplaces/{id}`)." + pluginsEnterpriseNote,
		Attributes:          attrs,
	}
}

func (d *PluginMarketplaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = pluginDataSourceClient(req, resp)
}

func (d *PluginMarketplaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PluginMarketplaceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := d.client.GetPluginMarketplace(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Plugin Marketplace", err.Error())
		return
	}
	state.PluginMarketplaceModel = mapPluginMarketplace(m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// pluginDataSourceClient extracts the provider client for the plugin data sources.
func pluginDataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return nil
	}
	return c
}
