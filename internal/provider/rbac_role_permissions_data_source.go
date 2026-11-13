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
	_ datasource.DataSource              = &RBACRolePermissionsDataSource{}
	_ datasource.DataSourceWithConfigure = &RBACRolePermissionsDataSource{}
)

func NewRBACRolePermissionsDataSource() datasource.DataSource {
	return &RBACRolePermissionsDataSource{}
}

type RBACRolePermissionsDataSource struct {
	client *client.Client
}

type RBACRolePermissionModel struct {
	Action         types.String `tfsdk:"action"`
	ResourceType   types.String `tfsdk:"resource_type"`
	OrganizationID types.String `tfsdk:"organization_id"`
	ConnectorID    types.String `tfsdk:"connector_id"`
	ToolName       types.String `tfsdk:"tool_name"`
	Scope          types.String `tfsdk:"scope"`
}

type RBACRolePermissionsDataSourceModel struct {
	RoleID      types.String              `tfsdk:"role_id"`
	Permissions []RBACRolePermissionModel `tfsdk:"permissions"`
}

func (d *RBACRolePermissionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rbac_role_permissions"
}

func (d *RBACRolePermissionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the permissions a Claude Enterprise RBAC role grants (`/v1/organizations/rbac_roles/{id}/permissions`). " +
			"Roles are defined by Anthropic and are read-only through the API. " +
			"A `capability_access_all` or `capability_access_all_ga` action is a blanket grant that covers many product features.",
		Attributes: map[string]schema.Attribute{
			"role_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the RBAC role, for example from `anthropic_rbac_roles`.",
				Required:            true,
				Validators:          []validator.String{idValidator()},
			},
			"permissions": schema.ListNestedAttribute{
				MarkdownDescription: "Permissions the role grants.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"action":          computed("Action the permission grants on the resource, for example `chat`, `use` or `grant`."),
						"resource_type":   computed("Kind of resource: `organization`, `connector_tool`, `connector_scope`, `connector` or `all_connectors`."),
						"organization_id": computed("Organization the permission applies to (`organization` only)."),
						"connector_id":    computed("Connector the permission applies to (connector kinds only)."),
						"tool_name":       computed("Published connector tool name, possibly server-encoded (`connector_tool` only)."),
						"scope":           computed("OAuth scope, possibly server-encoded (`connector_scope` only)."),
					},
				},
			},
		},
	}
}

func (d *RBACRolePermissionsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = dataSourceClientOrDiag(req, resp)
}

func (d *RBACRolePermissionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state RBACRolePermissionsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	perms, err := d.client.ListRBACRolePermissions(ctx, state.RoleID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Listing RBAC Role Permissions", err.Error())
		return
	}
	state.Permissions = make([]RBACRolePermissionModel, 0, len(perms))
	for _, p := range perms {
		state.Permissions = append(state.Permissions, RBACRolePermissionModel{
			Action:         types.StringValue(p.Action),
			ResourceType:   types.StringValue(p.Resource.Type),
			OrganizationID: emptyStringAsNull(p.Resource.OrganizationID),
			ConnectorID:    emptyStringAsNull(p.Resource.ConnectorID),
			ToolName:       emptyStringAsNull(p.Resource.ToolName),
			Scope:          emptyStringAsNull(p.Resource.Scope),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
