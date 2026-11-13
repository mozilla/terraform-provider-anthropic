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
	_ resource.Resource                = &TelemetryDestinationWorkspaceResource{}
	_ resource.ResourceWithConfigure   = &TelemetryDestinationWorkspaceResource{}
	_ resource.ResourceWithImportState = &TelemetryDestinationWorkspaceResource{}
)

func NewTelemetryDestinationWorkspaceResource() resource.Resource {
	return &TelemetryDestinationWorkspaceResource{}
}

type TelemetryDestinationWorkspaceResource struct {
	client *client.Client
}

type TelemetryDestinationWorkspaceResourceModel struct {
	ID            types.String `tfsdk:"id"`
	DestinationID types.String `tfsdk:"destination_id"`
	WorkspaceID   types.String `tfsdk:"workspace_id"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func (r *TelemetryDestinationWorkspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_telemetry_destination_workspace"
}

func (r *TelemetryDestinationWorkspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Puts one workspace on a telemetry destination's workspace list " +
			"(`/v1/organizations/telemetry_destinations/{destination_id}/workspaces`). " +
			"With `workspace_scope = \"include\"` the workspace's spans go to the destination. With `exclude` they don't. " +
			"Not supported when `workspace_scope` is `all`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier in the format `<destination_id>/<workspace_id>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"destination_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the telemetry destination (`tdst_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers:       replace,
			},
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the workspace (`wrkspc_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers:       replace,
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the workspace was added to the list.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *TelemetryDestinationWorkspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = c
}

func (r *TelemetryDestinationWorkspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TelemetryDestinationWorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.AddTelemetryDestinationWorkspace(ctx, plan.DestinationID.ValueString(), plan.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Adding Workspace to Telemetry Destination", err.Error())
		return
	}
	populateTelemetryDestinationWorkspaceModel(&plan, e)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TelemetryDestinationWorkspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TelemetryDestinationWorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	e, err := r.client.GetTelemetryDestinationWorkspace(ctx, state.DestinationID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Telemetry Destination Workspace", err.Error())
		return
	}
	populateTelemetryDestinationWorkspaceModel(&state, e)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every configurable attribute requires replacement.
func (r *TelemetryDestinationWorkspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan TelemetryDestinationWorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TelemetryDestinationWorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TelemetryDestinationWorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RemoveTelemetryDestinationWorkspace(ctx, state.DestinationID.ValueString(), state.WorkspaceID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Removing Workspace from Telemetry Destination", err.Error())
	}
}

func (r *TelemetryDestinationWorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	dstID, wsID, err := splitCompositeID(req.ID, "destination_id", "workspace_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid Telemetry Destination Workspace Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), dstID+"/"+wsID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("destination_id"), dstID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), wsID)...)
}

func populateTelemetryDestinationWorkspaceModel(m *TelemetryDestinationWorkspaceResourceModel, e *client.TelemetryDestinationWorkspace) {
	m.ID = types.StringValue(e.TelemetryDestinationID + "/" + e.WorkspaceID)
	m.DestinationID = types.StringValue(e.TelemetryDestinationID)
	m.WorkspaceID = types.StringValue(e.WorkspaceID)
	m.CreatedAt = types.StringValue(e.CreatedAt)
}
