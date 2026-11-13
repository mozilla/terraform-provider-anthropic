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
	_ datasource.DataSource              = &PluginSharesDataSource{}
	_ datasource.DataSourceWithConfigure = &PluginSharesDataSource{}
)

func NewPluginSharesDataSource() datasource.DataSource {
	return &PluginSharesDataSource{}
}

type PluginSharesDataSource struct {
	client *client.Client
}

type PluginShareModel struct {
	TargetType  types.String `tfsdk:"target_type"`
	RBACGroupID types.String `tfsdk:"rbac_group_id"`
	UserID      types.String `tfsdk:"user_id"`
	GrantedAt   types.String `tfsdk:"granted_at"`
}

type PluginSharesDataSourceModel struct {
	PluginID   types.String       `tfsdk:"plugin_id"`
	TargetType types.String       `tfsdk:"target_type"`
	Shares     []PluginShareModel `tfsdk:"shares"`
}

func (d *PluginSharesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin_shares"
}

func (d *PluginSharesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Lists the shares the owner of a member-owned Claude Enterprise plugin has given (`GET /v1/organizations/plugins/{id}/shares`). " +
			"Shares are read-only in the API: members give and withdraw them in claude.ai." + pluginsEnterpriseNote,
		Attributes: map[string]schema.Attribute{
			"plugin_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of a member-owned plugin (`plugin_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"target_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only shares with this kind of target: `organization`, `rbac_group` or `organization_member`.",
				Validators:          []validator.String{stringvalidator.OneOf("organization", "rbac_group", "organization_member")},
			},
			"shares": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Shares, most recently granted first.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"target_type":   computed("`organization` (every member), `rbac_group` or `organization_member`."),
					"rbac_group_id": computed("RBAC Group ID when `target_type` is `rbac_group`."),
					"user_id":       computed("User ID when `target_type` is `organization_member`."),
					"granted_at":    computed("RFC 3339 timestamp when the share was given."),
				}},
			},
		},
	}
}

func (d *PluginSharesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = pluginDataSourceClient(req, resp)
}

func (d *PluginSharesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state PluginSharesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	q := url.Values{}
	if v := state.TargetType.ValueString(); v != "" {
		q.Set("target_type", v)
	}
	list, err := d.client.ListPluginShares(ctx, state.PluginID.ValueString(), q)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Plugin Shares", err.Error())
		return
	}
	state.Shares = make([]PluginShareModel, 0, len(list))
	for _, s := range list {
		state.Shares = append(state.Shares, PluginShareModel{
			TargetType:  types.StringValue(s.Target.Type),
			RBACGroupID: types.StringPointerValue(s.Target.RBACGroupID),
			UserID:      types.StringPointerValue(s.Target.UserID),
			GrantedAt:   types.StringValue(s.GrantedAt),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
