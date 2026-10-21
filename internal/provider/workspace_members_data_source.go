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
	_ datasource.DataSource              = &WorkspaceMembersDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceMembersDataSource{}
)

func NewWorkspaceMembersDataSource() datasource.DataSource {
	return &WorkspaceMembersDataSource{}
}

type WorkspaceMembersDataSource struct {
	client *client.Client
}

type WorkspaceMembersDataSourceModel struct {
	WorkspaceID types.String                     `tfsdk:"workspace_id"`
	Members     []WorkspaceMemberDataSourceModel `tfsdk:"members"`
}

func (d *WorkspaceMembersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_members"
}

func (d *WorkspaceMembersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all members of a Claude Console workspace (`GET /v1/organizations/workspaces/{workspace_id}/members`).",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the workspace (`wrkspc_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"members": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of workspace members.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Composite identifier (`workspace_id/user_id`).",
						},
						"workspace_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Identifier of the workspace.",
						},
						"user_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Identifier of the user.",
						},
						"workspace_role": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Workspace role of the user.",
						},
						"type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type (`workspace_member`).",
						},
					},
				},
			},
		},
	}
}

func (d *WorkspaceMembersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *WorkspaceMembersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceMembersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	members, err := d.client.ListWorkspaceMembers(ctx, state.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Listing Workspace Members",
			fmt.Sprintf("Could not list members for workspace %s: %s", state.WorkspaceID.ValueString(), err),
		)
		return
	}

	state.Members = make([]WorkspaceMemberDataSourceModel, 0, len(members))
	for _, wm := range members {
		state.Members = append(state.Members, WorkspaceMemberDataSourceModel{
			ID:            types.StringValue(fmt.Sprintf("%s/%s", wm.WorkspaceID, wm.UserID)),
			WorkspaceID:   types.StringValue(wm.WorkspaceID),
			UserID:        types.StringValue(wm.UserID),
			WorkspaceRole: types.StringValue(wm.WorkspaceRole),
			Type:          types.StringValue(wm.Type),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
