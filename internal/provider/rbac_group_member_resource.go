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
	_ resource.Resource                = &RBACGroupMemberResource{}
	_ resource.ResourceWithConfigure   = &RBACGroupMemberResource{}
	_ resource.ResourceWithImportState = &RBACGroupMemberResource{}
)

func NewRBACGroupMemberResource() resource.Resource {
	return &RBACGroupMemberResource{}
}

type RBACGroupMemberResource struct {
	client *client.Client
}

type RBACGroupMemberResourceModel struct {
	ID        types.String `tfsdk:"id"`
	GroupID   types.String `tfsdk:"group_id"`
	UserID    types.String `tfsdk:"user_id"`
	Email     types.String `tfsdk:"email"`
	Name      types.String `tfsdk:"name"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *RBACGroupMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rbac_group_member"
}

func (r *RBACGroupMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a user's membership in an API-managed Claude Enterprise RBAC group (`/v1/organizations/rbac_groups/{group_id}/members`). " +
			"Note that memberships in SCIM-provisioned groups (`source_type = \"scim\"`) are controlled by your identity provider and cannot be modified via the Admin API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier in the format `<group_id>/<user_id>`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"group_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the target RBAC group.",
				Required:            true,
				Validators: []validator.String{
					idValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the organization user to add to the group.",
				Required:            true,
				Validators: []validator.String{
					idValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "Email address of the member.",
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the member.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the membership was created.",
				Computed:            true,
			},
		},
	}
}

func (r *RBACGroupMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *RBACGroupMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RBACGroupMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := plan.GroupID.ValueString()
	userID := plan.UserID.ValueString()

	member, err := r.client.AddRBACGroupMember(ctx, groupID, userID)
	if err != nil {
		if client.IsBadRequest(err) {
			resp.Diagnostics.AddError(
				"Error Adding RBAC Group Member",
				fmt.Sprintf(
					"Failed to add user %q to RBAC group %q. If this group has source_type = \"scim\", "+
						"group memberships are managed by your identity provider and cannot be modified via the Admin API. API error: %s",
					userID, groupID, err.Error(),
				),
			)
			return
		}
		resp.Diagnostics.AddError("Error Adding RBAC Group Member", err.Error())
		return
	}

	resolvedGroupID := member.EffectiveGroupID(groupID)
	plan.ID = types.StringValue(fmt.Sprintf("%s/%s", resolvedGroupID, member.UserID))
	plan.GroupID = types.StringValue(resolvedGroupID)
	plan.UserID = types.StringValue(member.UserID)
	plan.Email = types.StringValue(member.Email)
	plan.Name = types.StringValue(member.Name)
	plan.CreatedAt = types.StringValue(member.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RBACGroupMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RBACGroupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groupID := state.GroupID.ValueString()
	userID := state.UserID.ValueString()

	member, err := r.client.FindRBACGroupMember(ctx, groupID, userID)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading RBAC Group Member", err.Error())
		return
	}
	if member == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resolvedGroupID := member.EffectiveGroupID(groupID)
	state.ID = types.StringValue(fmt.Sprintf("%s/%s", resolvedGroupID, member.UserID))
	state.GroupID = types.StringValue(resolvedGroupID)
	state.UserID = types.StringValue(member.UserID)
	state.Email = types.StringValue(member.Email)
	state.Name = types.StringValue(member.Name)
	state.CreatedAt = types.StringValue(member.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RBACGroupMemberResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unsupported Update Operation",
		"All configurable attributes on anthropic_rbac_group_member require replacement.",
	)
}

func (r *RBACGroupMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RBACGroupMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveRBACGroupMember(ctx, state.GroupID.ValueString(), state.UserID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Removing RBAC Group Member", err.Error())
		return
	}
}

func (r *RBACGroupMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupID, userID, err := splitCompositeID(req.ID, "group_id", "user_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import Identifier", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s", groupID, userID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), groupID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), userID)...)
}
