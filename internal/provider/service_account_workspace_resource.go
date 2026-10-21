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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                = &ServiceAccountWorkspaceResource{}
	_ resource.ResourceWithConfigure   = &ServiceAccountWorkspaceResource{}
	_ resource.ResourceWithImportState = &ServiceAccountWorkspaceResource{}
)

func NewServiceAccountWorkspaceResource() resource.Resource {
	return &ServiceAccountWorkspaceResource{}
}

type ServiceAccountWorkspaceResource struct {
	client *client.Client
}

type ServiceAccountWorkspaceResourceModel struct {
	ID               types.String `tfsdk:"id"`
	ServiceAccountID types.String `tfsdk:"service_account_id"`
	WorkspaceID      types.String `tfsdk:"workspace_id"`
	WorkspaceRole    types.String `tfsdk:"workspace_role"`
	CreatedByActorID types.String `tfsdk:"created_by_actor_id"`
	Implicit         types.Bool   `tfsdk:"implicit"`
	Type             types.String `tfsdk:"type"`
}

func (r *ServiceAccountWorkspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account_workspace"
}

func (r *ServiceAccountWorkspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an explicit workspace membership for a non-human Anthropic service account " +
			"(`/v1/organizations/service_accounts/{service_account_id}/workspaces`). " +
			"Requires an `org:admin` OAuth access token (`oauth_token`) or `federation` authentication. " +
			"Granting workspace membership is required before a service account with `organization_role = \"user\"` can mint workspace-scoped OAuth tokens via Workload Identity Federation.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier in the format `<service_account_id>/<workspace_id>`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"service_account_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the service account (`svac_...`).",
				Validators: []validator.String{
					idValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the workspace (`wrkspc_...`).",
				Validators: []validator.String{
					idValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workspace_role": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Role of the service account within the workspace (`workspace_user`, `workspace_developer`, `workspace_restricted_developer`, `workspace_admin`).",
				Validators: []validator.String{
					serviceAccountWorkspaceRoleValidator(),
				},
			},
			"created_by_actor_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the actor that created the workspace membership, or `null` for implicit memberships.",
			},
			"implicit": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the membership is derived implicitly from the service account's `organization_role`.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`service_account_workspace_member`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ServiceAccountWorkspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ServiceAccountWorkspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ServiceAccountWorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.AddServiceAccountToWorkspace(ctx, plan.ServiceAccountID.ValueString(), client.AddServiceAccountToWorkspaceRequest{
		WorkspaceID:   plan.WorkspaceID.ValueString(),
		WorkspaceRole: plan.WorkspaceRole.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Adding Service Account to Workspace",
			fmt.Sprintf(
				"Could not add service account %s to workspace %s: %s",
				plan.ServiceAccountID.ValueString(),
				plan.WorkspaceID.ValueString(),
				err.Error(),
			),
		)
		return
	}

	populateServiceAccountWorkspaceModel(&plan, m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ServiceAccountWorkspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ServiceAccountWorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.GetServiceAccountWorkspace(ctx, state.ServiceAccountID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Service Account Workspace Membership",
			fmt.Sprintf(
				"Could not read workspace membership %s/%s: %s",
				state.ServiceAccountID.ValueString(),
				state.WorkspaceID.ValueString(),
				err.Error(),
			),
		)
		return
	}

	populateServiceAccountWorkspaceModel(&state, m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ServiceAccountWorkspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ServiceAccountWorkspaceResourceModel
	var state ServiceAccountWorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	m, err := r.client.UpdateServiceAccountWorkspace(ctx, state.ServiceAccountID.ValueString(), state.WorkspaceID.ValueString(), client.UpdateServiceAccountWorkspaceRequest{
		WorkspaceRole: plan.WorkspaceRole.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Service Account Workspace Membership",
			fmt.Sprintf(
				"Could not update workspace membership %s/%s: %s",
				state.ServiceAccountID.ValueString(),
				state.WorkspaceID.ValueString(),
				err.Error(),
			),
		)
		return
	}

	populateServiceAccountWorkspaceModel(&plan, m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ServiceAccountWorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ServiceAccountWorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveServiceAccountFromWorkspace(ctx, state.ServiceAccountID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Removing Service Account from Workspace",
			fmt.Sprintf(
				"Could not remove service account %s from workspace %s: %s",
				state.ServiceAccountID.ValueString(),
				state.WorkspaceID.ValueString(),
				err.Error(),
			),
		)
	}
}

func (r *ServiceAccountWorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	saID, wsID, err := splitCompositeID(req.ID, "service_account_id", "workspace_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid Service Account Workspace Import ID", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s", saID, wsID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_account_id"), saID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), wsID)...)
}

func populateServiceAccountWorkspaceModel(m *ServiceAccountWorkspaceResourceModel, api *client.ServiceAccountWorkspaceMember) {
	m.ID = types.StringValue(fmt.Sprintf("%s/%s", api.ServiceAccountID, api.WorkspaceID))
	m.ServiceAccountID = types.StringValue(api.ServiceAccountID)
	m.WorkspaceID = types.StringValue(api.WorkspaceID)
	m.WorkspaceRole = types.StringValue(api.WorkspaceRole)
	if api.CreatedByActorID != nil {
		m.CreatedByActorID = types.StringValue(*api.CreatedByActorID)
	} else {
		m.CreatedByActorID = types.StringNull()
	}
	m.Implicit = types.BoolValue(api.Implicit)
	m.Type = types.StringValue(api.Type)
}
