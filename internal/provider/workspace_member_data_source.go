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
	_ datasource.DataSource              = &WorkspaceMemberDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspaceMemberDataSource{}
)

func NewWorkspaceMemberDataSource() datasource.DataSource {
	return &WorkspaceMemberDataSource{}
}

type WorkspaceMemberDataSource struct {
	client *client.Client
}

type WorkspaceMemberDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	WorkspaceID   types.String `tfsdk:"workspace_id"`
	UserID        types.String `tfsdk:"user_id"`
	WorkspaceRole types.String `tfsdk:"workspace_role"`
	Type          types.String `tfsdk:"type"`
}

func (d *WorkspaceMemberDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_member"
}

func (d *WorkspaceMemberDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves a single member of a Claude Console workspace (`GET /v1/organizations/workspaces/{workspace_id}/members/{user_id}`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier (`workspace_id/user_id`).",
			},
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the workspace (`wrkspc_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"user_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the user (`user_...`).",
				Validators:          []validator.String{idValidator()},
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
	}
}

func (d *WorkspaceMemberDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *WorkspaceMemberDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceMemberDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wm, err := d.client.GetWorkspaceMember(ctx, state.WorkspaceID.ValueString(), state.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Workspace Member",
			fmt.Sprintf("Could not read member %s in workspace %s: %s", state.UserID.ValueString(), state.WorkspaceID.ValueString(), err),
		)
		return
	}

	state.ID = types.StringValue(fmt.Sprintf("%s/%s", wm.WorkspaceID, wm.UserID))
	state.WorkspaceID = types.StringValue(wm.WorkspaceID)
	state.UserID = types.StringValue(wm.UserID)
	state.WorkspaceRole = types.StringValue(wm.WorkspaceRole)
	state.Type = types.StringValue(wm.Type)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
