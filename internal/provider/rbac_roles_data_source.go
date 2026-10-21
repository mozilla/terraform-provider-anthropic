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
	_ datasource.DataSource              = &RBACRolesDataSource{}
	_ datasource.DataSourceWithConfigure = &RBACRolesDataSource{}
)

func NewRBACRolesDataSource() datasource.DataSource {
	return &RBACRolesDataSource{}
}

type RBACRolesDataSource struct {
	client *client.Client
}

type RBACRoleModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

type RBACRolesDataSourceModel struct {
	Roles []RBACRoleModel `tfsdk:"roles"`
}

func (d *RBACRolesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rbac_roles"
}

func (d *RBACRolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all available Claude Enterprise RBAC roles in the organization (`/v1/organizations/rbac_roles`).",
		Attributes: map[string]schema.Attribute{
			"roles": schema.ListNestedAttribute{
				MarkdownDescription: "List of RBAC roles in the organization.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "Unique identifier of the RBAC role.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "Name of the RBAC role.",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "Human-readable description of the RBAC role.",
							Computed:            true,
						},
						"created_at": schema.StringAttribute{
							MarkdownDescription: "RFC 3339 timestamp when the role was created.",
							Computed:            true,
						},
						"updated_at": schema.StringAttribute{
							MarkdownDescription: "RFC 3339 timestamp when the role was last updated.",
							Computed:            true,
						},
					},
				},
			},
		},
	}
}

func (d *RBACRolesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *RBACRolesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	roles, err := d.client.ListRBACRoles(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing RBAC Roles", err.Error())
		return
	}

	state := RBACRolesDataSourceModel{
		Roles: make([]RBACRoleModel, 0, len(roles)),
	}

	for _, role := range roles {
		item := RBACRoleModel{
			ID:        types.StringValue(role.ID),
			Name:      types.StringValue(role.Name),
			CreatedAt: types.StringValue(role.CreatedAt),
			UpdatedAt: types.StringValue(role.UpdatedAt),
		}
		if role.Description != nil {
			item.Description = types.StringValue(*role.Description)
		} else {
			item.Description = types.StringNull()
		}
		state.Roles = append(state.Roles, item)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
