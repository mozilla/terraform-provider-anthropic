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
	_ resource.Resource                = &UserResource{}
	_ resource.ResourceWithConfigure   = &UserResource{}
	_ resource.ResourceWithImportState = &UserResource{}
)

func NewUserResource() resource.Resource {
	return &UserResource{}
}

// UserResource manages an organization member's role and membership lifecycle.
// Because users join an organization via invite or SSO/JIT, Create adopts an existing
// organization user by id and reconciles their role; Delete removes the user from the organization.
type UserResource struct {
	client *client.Client
}

type UserResourceModel struct {
	ID      types.String `tfsdk:"id"`
	Role    types.String `tfsdk:"role"`
	Email   types.String `tfsdk:"email"`
	Name    types.String `tfsdk:"name"`
	AddedAt types.String `tfsdk:"added_at"`
	Type    types.String `tfsdk:"type"`
}

func (r *UserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an existing organization member's organization-level role and membership lifecycle (`/v1/organizations/users/{user_id}`).\n\n" +
			"Because users join an organization by accepting an invitation (`anthropic_invite`) or via SSO/SCIM/JIT, `Create` adopts the existing member by `id` and sets their `role`. " +
			"**Warning**: Destroying this resource calls `DELETE /v1/organizations/users/{user_id}`, which removes the user from the organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier of the user (`user_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Organization-level role (`user`, `developer`, `billing`, `admin`, `claude_code_user`, `membership_admin`, `managed`).",
				Validators:          []validator.String{orgRoleValidator()},
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Email address of the user.",
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the user.",
			},
			"added_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the user was added to the organization.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`user`).",
			},
		},
	}
}

func (r *UserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	u, err := r.client.UpdateUser(ctx, plan.ID.ValueString(), plan.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Managing Organization User", fmt.Sprintf("Could not update user %s: %s", plan.ID.ValueString(), err))
		return
	}

	mapUserToModel(u, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	u, err := r.client.GetUser(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Organization User", fmt.Sprintf("Could not read user %s: %s", state.ID.ValueString(), err))
		return
	}

	mapUserToModel(u, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	u, err := r.client.UpdateUser(ctx, plan.ID.ValueString(), plan.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Organization User", fmt.Sprintf("Could not update user %s: %s", plan.ID.ValueString(), err))
		return
	}

	mapUserToModel(u, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteUser(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Removing Organization User", fmt.Sprintf("Could not delete user %s: %s", state.ID.ValueString(), err))
	}
}

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid User Import ID", fmt.Sprintf("Import ID %q must contain only alphanumeric characters, underscores, or hyphens.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapUserToModel(u *client.User, m *UserResourceModel) {
	m.ID = types.StringValue(u.ID)
	m.Role = types.StringValue(u.Role)
	m.Email = types.StringValue(u.Email)
	m.Name = types.StringValue(u.Name)
	m.AddedAt = types.StringValue(u.AddedAt)
	m.Type = types.StringValue(u.Type)
}
