// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &PluginVersionsDataSource{}
	_ datasource.DataSourceWithConfigure = &PluginVersionsDataSource{}
)

func NewPluginVersionsDataSource() datasource.DataSource {
	return &PluginVersionsDataSource{}
}

type PluginVersionsDataSource struct {
	client *client.Client
}

type PluginVersionModel struct {
	ID              types.String            `tfsdk:"id"`
	DisplayName     types.String            `tfsdk:"display_name"`
	Description     types.String            `tfsdk:"description"`
	ManifestVersion types.String            `tfsdk:"manifest_version"`
	Reach           types.String            `tfsdk:"reach"`
	ReleaseNotes    types.String            `tfsdk:"release_notes"`
	Components      []PluginComponentModel  `tfsdk:"components"`
	ContentScan     *PluginContentScanModel `tfsdk:"content_scan"`
	CreatedAt       types.String            `tfsdk:"created_at"`
}

type PluginVersionsDataSourceModel struct {
	PluginID types.String         `tfsdk:"plugin_id"`
	Versions []PluginVersionModel `tfsdk:"versions"`
}

func (d *PluginVersionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin_versions"
}

func (d *PluginVersionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Lists the stored versions of a Claude Enterprise plugin (`GET /v1/organizations/plugins/{id}/versions`)." + pluginsEnterpriseNote,
		Attributes: map[string]schema.Attribute{
			"plugin_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the plugin (`plugin_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"versions": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Versions of the plugin.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":               computed("ID of the version (`pluginver_...`)."),
					"display_name":     computed("Display name from the manifest. Null when none."),
					"description":      computed("Description from the manifest. Null when none."),
					"manifest_version": computed("Manifest `version`, normalized for display. May be null."),
					"reach":            computed("`contained`, `privileged` or `remote`. Null when not classified."),
					"release_notes":    computed("Release notes supplied with the upload. Null when none."),
					"components":       pluginComponentsAttribute("What the version contains. Null when not enumerated."),
					"content_scan":     pluginContentScanAttribute("Content scan of the version. Null when not scanned."),
					"created_at":       computed("RFC 3339 timestamp when the version was stored."),
				}},
			},
		},
	}
}

func (d *PluginVersionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = pluginDataSourceClient(req, resp)
}

func (d *PluginVersionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PluginVersionsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	list, err := d.client.ListPluginVersions(ctx, state.PluginID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Plugin Versions", err.Error())
		return
	}
	state.Versions = make([]PluginVersionModel, 0, len(list))
	for _, v := range list {
		state.Versions = append(state.Versions, PluginVersionModel{
			ID:              types.StringValue(v.ID),
			DisplayName:     types.StringPointerValue(v.DisplayName),
			Description:     types.StringPointerValue(v.Description),
			ManifestVersion: types.StringPointerValue(v.ManifestVersion),
			Reach:           types.StringPointerValue(v.Reach),
			ReleaseNotes:    types.StringPointerValue(v.ReleaseNotes),
			Components:      mapPluginComponents(v.Components),
			ContentScan:     mapPluginContentScan(v.ContentScan),
			CreatedAt:       types.StringValue(v.CreatedAt),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
