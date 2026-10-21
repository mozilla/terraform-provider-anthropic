// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &RBACGroupDataSource{}
	_ datasource.DataSourceWithConfigure = &RBACGroupDataSource{}
)

func NewRBACGroupDataSource() datasource.DataSource {
	return &RBACGroupDataSource{}
}

type RBACGroupDataSource struct {
	client *client.Client
}

type RBACGroupDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	RoleIDs    types.Set    `tfsdk:"role_ids"`
	SourceType types.String `tfsdk:"source_type"`
	CreatedAt  types.String `tfsdk:"created_at"`
	UpdatedAt  types.String `tfsdk:"updated_at"`
}

func (d *RBACGroupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rbac_group"
}

func (d *RBACGroupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Fetches a single Claude Enterprise RBAC group by ID (`/v1/organizations/rbac_groups/{group_id}`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the RBAC group.",
				Required:            true,
				Validators: []validator.String{
					idValidator(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the RBAC group.",
				Computed:            true,
			},
			"role_ids": schema.SetAttribute{
				MarkdownDescription: "Set of RBAC role IDs assigned to this group.",
				ElementType:         types.StringType,
				Computed:            true,
			},
			"source_type": schema.StringAttribute{
				MarkdownDescription: "Provisioning source of the group (`api` or `scim`).",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the group was created.",
				Computed:            true,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the group was last updated.",
				Computed:            true,
			},
		},
	}
}

func (d *RBACGroupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}
	d.client = c
}

func (d *RBACGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data RBACGroupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := d.client.GetRBACGroup(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading RBAC Group", err.Error())
		return
	}

	data.ID = types.StringValue(group.ID)
	data.Name = types.StringValue(group.Name)
	data.SourceType = types.StringValue(group.SourceType)
	data.CreatedAt = types.StringValue(group.CreatedAt)
	data.UpdatedAt = types.StringValue(group.UpdatedAt)

	roleIDs, degraded := group.EffectiveRoleIDs()
	if degraded {
		resp.Diagnostics.AddWarning(
			"Degraded RBAC Group Role Read",
			fmt.Sprintf("The Anthropic API returned null for role assignments on RBAC group %q (%s).", group.Name, group.ID),
		)
		data.RoleIDs = types.SetNull(types.StringType)
	} else if len(roleIDs) == 0 {
		data.RoleIDs = types.SetValueMust(types.StringType, []attr.Value{})
	} else {
		setVal, diags := types.SetValueFrom(ctx, types.StringType, roleIDs)
		resp.Diagnostics.Append(diags...)
		data.RoleIDs = setVal
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
