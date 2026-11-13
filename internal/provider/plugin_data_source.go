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
	_ datasource.DataSource              = &PluginDataSource{}
	_ datasource.DataSourceWithConfigure = &PluginDataSource{}
)

type PluginComponentModel struct {
	Name        types.String `tfsdk:"name"`
	Type        types.String `tfsdk:"type"`
	Description types.String `tfsdk:"description"`
}

type PluginContentScanModel struct {
	Status     types.String `tfsdk:"status"`
	Assessment types.String `tfsdk:"assessment"`
	Reason     types.String `tfsdk:"reason"`
}

type PluginModel struct {
	ID                                          types.String            `tfsdk:"id"`
	Name                                        types.String            `tfsdk:"name"`
	DisplayName                                 types.String            `tfsdk:"display_name"`
	Description                                 types.String            `tfsdk:"description"`
	MarketplaceID                               types.String            `tfsdk:"marketplace_id"`
	OwnerType                                   types.String            `tfsdk:"owner_type"`
	OwnerUserID                                 types.String            `tfsdk:"owner_user_id"`
	ManifestVersion                             types.String            `tfsdk:"manifest_version"`
	Reach                                       types.String            `tfsdk:"reach"`
	LatestVersionID                             types.String            `tfsdk:"latest_version_id"`
	ServedVersionID                             types.String            `tfsdk:"served_version_id"`
	ServedVersionPinned                         types.Bool              `tfsdk:"served_version_pinned"`
	OrganizationInstallationPreference          types.String            `tfsdk:"organization_installation_preference"`
	OrganizationInstallationPreferenceInherited types.Bool              `tfsdk:"organization_installation_preference_inherited"`
	Components                                  []PluginComponentModel  `tfsdk:"components"`
	ContentScan                                 *PluginContentScanModel `tfsdk:"content_scan"`
	CreatedAt                                   types.String            `tfsdk:"created_at"`
	UpdatedAt                                   types.String            `tfsdk:"updated_at"`
}

func mapPluginComponents(in []client.PluginComponent) []PluginComponentModel {
	if in == nil {
		return nil
	}
	out := make([]PluginComponentModel, 0, len(in))
	for _, c := range in {
		out = append(out, PluginComponentModel{
			Name:        types.StringValue(c.Name),
			Type:        types.StringValue(c.Type),
			Description: types.StringPointerValue(c.Description),
		})
	}
	return out
}

func mapPluginContentScan(in *client.PluginContentScan) *PluginContentScanModel {
	if in == nil {
		return nil
	}
	return &PluginContentScanModel{
		Status:     types.StringValue(in.Status),
		Assessment: types.StringPointerValue(in.Assessment),
		Reason:     types.StringPointerValue(in.Reason),
	}
}

func mapPlugin(p *client.Plugin) PluginModel {
	return PluginModel{
		ID:                                 types.StringValue(p.ID),
		Name:                               types.StringValue(p.Name),
		DisplayName:                        types.StringPointerValue(p.DisplayName),
		Description:                        types.StringPointerValue(p.Description),
		MarketplaceID:                      types.StringValue(p.MarketplaceID),
		OwnerType:                          types.StringValue(p.Owner.Type),
		OwnerUserID:                        types.StringPointerValue(p.Owner.UserID),
		ManifestVersion:                    types.StringPointerValue(p.ManifestVersion),
		Reach:                              types.StringPointerValue(p.Reach),
		LatestVersionID:                    types.StringValue(p.LatestVersionID),
		ServedVersionID:                    types.StringValue(p.ServedVersionID),
		ServedVersionPinned:                types.BoolValue(p.ServedVersionPinned),
		OrganizationInstallationPreference: types.StringPointerValue(p.OrganizationInstallationPreference),
		OrganizationInstallationPreferenceInherited: types.BoolPointerValue(p.OrganizationInstallationPreferenceInherited),
		Components:  mapPluginComponents(p.Components),
		ContentScan: mapPluginContentScan(p.ContentScan),
		CreatedAt:   types.StringValue(p.CreatedAt),
		UpdatedAt:   types.StringValue(p.UpdatedAt),
	}
}

func pluginComponentsAttribute(desc string) schema.Attribute {
	return schema.ListNestedAttribute{
		Computed:            true,
		MarkdownDescription: desc,
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the skill, command, agent, MCP server key, hook event or CLI executable."},
			"type":        schema.StringAttribute{Computed: true, MarkdownDescription: "`agent`, `cli`, `command`, `hook`, `mcp_server` or `skill`."},
			"description": schema.StringAttribute{Computed: true, MarkdownDescription: "What the component declares about itself. Null for MCP servers, hooks and CLIs."},
		}},
	}
}

func pluginContentScanAttribute(desc string) schema.Attribute {
	return schema.SingleNestedAttribute{
		Computed:            true,
		MarkdownDescription: desc,
		Attributes: map[string]schema.Attribute{
			"status":     schema.StringAttribute{Computed: true, MarkdownDescription: "`processing`, `completed` or `errored`."},
			"assessment": schema.StringAttribute{Computed: true, MarkdownDescription: "`pass`, `warn`, `fail` or `unknown`. Set only when `status` is `completed`."},
			"reason":     schema.StringAttribute{Computed: true, MarkdownDescription: "Main mechanism behind a `warn` or `fail`. Null otherwise."},
		},
	}
}

func pluginDataSourceAttributes() map[string]schema.Attribute {
	computed := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	return map[string]schema.Attribute{
		"id":                                   computed("ID of the plugin (`plugin_...`)."),
		"name":                                 computed("Lowercase identifier, unique within its marketplace."),
		"display_name":                         computed("Display name of the served version."),
		"description":                          computed("Description of the served version."),
		"marketplace_id":                       computed("ID of the marketplace the plugin lives in."),
		"owner_type":                           computed("`organization`, or `user` for a plugin in a member's personal marketplace."),
		"owner_user_id":                        computed("User ID of the owning member. Null for an organization plugin."),
		"manifest_version":                     computed("Manifest `version` of the served version, normalized for display. May be null."),
		"reach":                                computed("How far the served version reaches: `contained`, `privileged` or `remote`. Null when not classified."),
		"latest_version_id":                    computed("ID of the newest version."),
		"served_version_id":                    computed("ID of the version claude.ai serves to members."),
		"served_version_pinned":                schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the served version has been pinned."},
		"organization_installation_preference": computed("Organization-wide installation setting: `required`, `auto_install`, `available` or `not_available`. Null for a member-owned plugin."),
		"organization_installation_preference_inherited": schema.BoolAttribute{Computed: true, MarkdownDescription: "True while the organization-wide setting comes from the marketplace default. Null for a member-owned plugin."},
		"components":   pluginComponentsAttribute("What the served version contains. Null when not enumerated."),
		"content_scan": pluginContentScanAttribute("Content scan of the served version. Null when not scanned."),
		"created_at":   computed("RFC 3339 timestamp when the plugin was created."),
		"updated_at":   computed("RFC 3339 timestamp of the last served-version change."),
	}
}

func NewPluginDataSource() datasource.DataSource {
	return &PluginDataSource{}
}

type PluginDataSource struct {
	client *client.Client
}

func (d *PluginDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin"
}

func (d *PluginDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := pluginDataSourceAttributes()
	attrs["id"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "ID of the plugin (`plugin_...`).",
		Validators:          []validator.String{idValidator()},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Looks up a Claude Enterprise plugin (`GET /v1/organizations/plugins/{id}`)." + pluginsEnterpriseNote,
		Attributes:          attrs,
	}
}

func (d *PluginDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = pluginDataSourceClient(req, resp)
}

func (d *PluginDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PluginModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := d.client.GetPlugin(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Plugin", err.Error())
		return
	}
	state = mapPlugin(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
