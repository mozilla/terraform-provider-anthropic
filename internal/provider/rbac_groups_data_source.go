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
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &RBACGroupsDataSource{}
	_ datasource.DataSourceWithConfigure = &RBACGroupsDataSource{}
)

func NewRBACGroupsDataSource() datasource.DataSource {
	return &RBACGroupsDataSource{}
}

type RBACGroupsDataSource struct {
	client *client.Client
}

type RBACGroupsDataSourceModel struct {
	Groups []RBACGroupDataSourceModel `tfsdk:"groups"`
}

func (d *RBACGroupsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rbac_groups"
}

func (d *RBACGroupsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all Claude Enterprise RBAC groups in the organization (`/v1/organizations/rbac_groups`).",
		Attributes: map[string]schema.Attribute{
			"groups": schema.ListNestedAttribute{
				MarkdownDescription: "List of RBAC groups in the organization.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Unique identifier of the RBAC group.",
							Computed:            true,
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
				},
			},
		},
	}
}

func (d *RBACGroupsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *RBACGroupsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	groups, err := d.client.ListRBACGroups(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing RBAC Groups", err.Error())
		return
	}

	state := RBACGroupsDataSourceModel{
		Groups: make([]RBACGroupDataSourceModel, 0, len(groups)),
	}

	for _, group := range groups {
		item := RBACGroupDataSourceModel{
			ID:         types.StringValue(group.ID),
			Name:       types.StringValue(group.Name),
			SourceType: types.StringValue(group.SourceType),
			CreatedAt:  types.StringValue(group.CreatedAt),
			UpdatedAt:  types.StringValue(group.UpdatedAt),
		}

		roleIDs, degraded := group.EffectiveRoleIDs()
		if degraded {
			resp.Diagnostics.AddWarning(
				"Degraded RBAC Group Role Read",
				fmt.Sprintf("The Anthropic API returned null for role assignments on RBAC group %q (%s).", group.Name, group.ID),
			)
			item.RoleIDs = types.SetNull(types.StringType)
		} else if len(roleIDs) == 0 {
			item.RoleIDs = types.SetValueMust(types.StringType, []attr.Value{})
		} else {
			setVal, diags := types.SetValueFrom(ctx, types.StringType, roleIDs)
			resp.Diagnostics.Append(diags...)
			item.RoleIDs = setVal
		}

		state.Groups = append(state.Groups, item)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
