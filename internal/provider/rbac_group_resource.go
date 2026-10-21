// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                = &RBACGroupResource{}
	_ resource.ResourceWithConfigure   = &RBACGroupResource{}
	_ resource.ResourceWithImportState = &RBACGroupResource{}
)

func NewRBACGroupResource() resource.Resource {
	return &RBACGroupResource{}
}

type RBACGroupResource struct {
	client *client.Client
}

type RBACGroupResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	RoleIDs    types.Set    `tfsdk:"role_ids"`
	SourceType types.String `tfsdk:"source_type"`
	CreatedAt  types.String `tfsdk:"created_at"`
	UpdatedAt  types.String `tfsdk:"updated_at"`
}

func (r *RBACGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rbac_group"
}

func (r *RBACGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Claude Enterprise RBAC group (`/v1/organizations/rbac_groups?beta=true`). " +
			"Supports creating and renaming custom API groups (`source_type = \"api\"`) and importing SCIM-synced groups (`source_type = \"scim\"`). " +
			"Note that `role_ids` is read-only (Computed) because the Anthropic Admin API only accepts `name` on create and update; role assignments to groups are configured in the Claude Enterprise UI.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the RBAC group.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Display name of the RBAC group. Cannot be modified for SCIM-provisioned groups (`source_type = \"scim\"`).",
				Required:            true,
				Validators: []validator.String{
					nonEmptyStringValidator(),
				},
			},
			"role_ids": schema.SetAttribute{
				MarkdownDescription: "Read-only set of RBAC role IDs currently assigned to this group in Claude Enterprise.",
				ElementType:         types.StringType,
				Computed:            true,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"source_type": schema.StringAttribute{
				MarkdownDescription: "Provisioning source of the group (e.g., `api` or `scim`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the group was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the group was last updated.",
				Computed:            true,
			},
		},
	}
}

func (r *RBACGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *RBACGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RBACGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.client.CreateRBACGroup(ctx, client.CreateRBACGroupRequest{
		Name: plan.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating RBAC Group", err.Error())
		return
	}

	r.mapGroupToState(ctx, group, &plan, types.SetNull(types.StringType), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RBACGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RBACGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.client.GetRBACGroup(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading RBAC Group", err.Error())
		return
	}

	r.mapGroupToState(ctx, group, &state, state.RoleIDs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *RBACGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan RBACGroupResourceModel
	var state RBACGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.SourceType.ValueString() == "scim" {
		resp.Diagnostics.AddError(
			"Cannot Rename SCIM-Managed RBAC Group",
			fmt.Sprintf(
				"Group %q (%s) has source_type = \"scim\" and is managed by your identity provider. "+
					"The Anthropic Admin API does not permit renaming SCIM groups; rename the group in your IdP instead.",
				state.Name.ValueString(), state.ID.ValueString(),
			),
		)
		return
	}

	updateReq := client.UpdateRBACGroupRequest{
		Name: plan.Name.ValueString(),
	}

	group, err := r.client.UpdateRBACGroup(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating RBAC Group", err.Error())
		return
	}

	r.mapGroupToState(ctx, group, &plan, state.RoleIDs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *RBACGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RBACGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteRBACGroup(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			return
		}
		if client.IsBadRequest(err) && state.SourceType.ValueString() == "scim" {
			resp.Diagnostics.AddError(
				"Cannot Delete SCIM-Managed RBAC Group",
				fmt.Sprintf(
					"Group %q (%s) has source_type = \"scim\" and cannot be deleted via the Anthropic Admin API. "+
						"Deprovision the group in your identity provider or remove it from Terraform state using `terraform state rm`. API error: %s",
					state.Name.ValueString(), state.ID.ValueString(), err.Error(),
				),
			)
			return
		}
		resp.Diagnostics.AddError("Error Deleting RBAC Group", err.Error())
		return
	}
}

func (r *RBACGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError(
			"Invalid Import Identifier",
			fmt.Sprintf("Expected RBAC group ID matching %s, got %q", resourceIDPattern.String(), req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *RBACGroupResource) mapGroupToState(
	ctx context.Context,
	group *client.RBACGroup,
	model *RBACGroupResourceModel,
	fallbackRoleIDs types.Set,
	diags *diag.Diagnostics,
) {
	model.ID = types.StringValue(group.ID)
	model.Name = types.StringValue(group.Name)
	model.SourceType = types.StringValue(group.SourceType)
	model.CreatedAt = types.StringValue(group.CreatedAt)
	model.UpdatedAt = types.StringValue(group.UpdatedAt)

	roleIDs, degraded := group.EffectiveRoleIDs()
	if degraded {
		diags.AddWarning(
			"Degraded RBAC Group Role Read",
			fmt.Sprintf(
				"The Anthropic API returned null for role assignments on RBAC group %q (%s), indicating a degraded backend read. "+
					"Preserving prior state for role_ids to avoid spurious updates, but external RBAC role drift cannot be verified until the API recovers.",
				group.Name, group.ID,
			),
		)
		if !fallbackRoleIDs.IsNull() && !fallbackRoleIDs.IsUnknown() {
			model.RoleIDs = fallbackRoleIDs
		} else {
			model.RoleIDs = types.SetValueMust(types.StringType, []attr.Value{})
		}
		return
	}

	if len(roleIDs) == 0 {
		model.RoleIDs = types.SetValueMust(types.StringType, []attr.Value{})
		return
	}

	setVal, d := types.SetValueFrom(ctx, types.StringType, roleIDs)
	diags.Append(d...)
	model.RoleIDs = setVal
}
