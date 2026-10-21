// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                = &WorkspaceMemberResource{}
	_ resource.ResourceWithConfigure   = &WorkspaceMemberResource{}
	_ resource.ResourceWithImportState = &WorkspaceMemberResource{}
)

func NewWorkspaceMemberResource() resource.Resource {
	return &WorkspaceMemberResource{}
}

type WorkspaceMemberResource struct {
	client *client.Client
}

type WorkspaceMemberResourceModel struct {
	ID            types.String `tfsdk:"id"`
	WorkspaceID   types.String `tfsdk:"workspace_id"`
	UserID        types.String `tfsdk:"user_id"`
	WorkspaceRole types.String `tfsdk:"workspace_role"`
	Type          types.String `tfsdk:"type"`
}

func (r *WorkspaceMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_member"
}

func (r *WorkspaceMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a user's membership and role within a Claude Console workspace (`/v1/organizations/workspaces/{workspace_id}/members`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier in the format `workspace_id/user_id`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the workspace (`wrkspc_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Identifier of the organization user (`user_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workspace_role": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Role of the user in the workspace (`workspace_user`, `workspace_developer`, `workspace_restricted_developer`, `workspace_admin`, `workspace_billing`).",
				Validators:          []validator.String{workspaceRoleValidator()},
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`workspace_member`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *WorkspaceMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *WorkspaceMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wm, err := r.client.CreateWorkspaceMember(ctx, plan.WorkspaceID.ValueString(), client.CreateWorkspaceMemberRequest{
		UserID:        plan.UserID.ValueString(),
		WorkspaceRole: plan.WorkspaceRole.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Adding Workspace Member",
			fmt.Sprintf("Could not add user %s to workspace %s: %s", plan.UserID.ValueString(), plan.WorkspaceID.ValueString(), err),
		)
		return
	}

	mapWorkspaceMemberToModel(wm, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wm, err := r.client.GetWorkspaceMember(ctx, state.WorkspaceID.ValueString(), state.UserID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Workspace Member",
			fmt.Sprintf("Could not read member %s in workspace %s: %s", state.UserID.ValueString(), state.WorkspaceID.ValueString(), err),
		)
		return
	}

	mapWorkspaceMemberToModel(wm, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wm, err := r.client.UpdateWorkspaceMember(ctx, plan.WorkspaceID.ValueString(), plan.UserID.ValueString(), client.UpdateWorkspaceMemberRequest{
		WorkspaceRole: plan.WorkspaceRole.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Workspace Member",
			fmt.Sprintf("Could not update member %s in workspace %s: %s", plan.UserID.ValueString(), plan.WorkspaceID.ValueString(), err),
		)
		return
	}

	mapWorkspaceMemberToModel(wm, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteWorkspaceMember(ctx, state.WorkspaceID.ValueString(), state.UserID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Removing Workspace Member",
			fmt.Sprintf("Could not remove member %s from workspace %s: %s", state.UserID.ValueString(), state.WorkspaceID.ValueString(), err),
		)
	}
}

func (r *WorkspaceMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	wsID, userID, err := splitCompositeID(req.ID, "workspace_id", "user_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid Workspace Member Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), wsID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
}

func mapWorkspaceMemberToModel(wm *client.WorkspaceMember, m *WorkspaceMemberResourceModel) {
	m.ID = types.StringValue(fmt.Sprintf("%s/%s", wm.WorkspaceID, wm.UserID))
	m.WorkspaceID = types.StringValue(wm.WorkspaceID)
	m.UserID = types.StringValue(wm.UserID)
	m.WorkspaceRole = types.StringValue(wm.WorkspaceRole)
	m.Type = types.StringValue(wm.Type)
}
