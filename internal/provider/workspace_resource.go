// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                = &WorkspaceResource{}
	_ resource.ResourceWithConfigure   = &WorkspaceResource{}
	_ resource.ResourceWithImportState = &WorkspaceResource{}
)

func NewWorkspaceResource() resource.Resource {
	return &WorkspaceResource{}
}

type WorkspaceResource struct {
	client *client.Client
}

type WorkspaceDataResidencyModel struct {
	WorkspaceGeo         types.String `tfsdk:"workspace_geo"`
	DefaultInferenceGeo  types.String `tfsdk:"default_inference_geo"`
	AllowedInferenceGeos types.Set    `tfsdk:"allowed_inference_geos"`
}

func dataResidencyAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"workspace_geo":          types.StringType,
		"default_inference_geo":  types.StringType,
		"allowed_inference_geos": types.SetType{ElemType: types.StringType},
	}
}

type WorkspaceResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	DisplayColor  types.String `tfsdk:"display_color"`
	CreatedAt     types.String `tfsdk:"created_at"`
	ArchivedAt    types.String `tfsdk:"archived_at"`
	DataResidency types.Object `tfsdk:"data_residency"`
	Type          types.String `tfsdk:"type"`
}

func (r *WorkspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *WorkspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Claude Console workspace (`/v1/organizations/workspaces`). " +
			"Deleting this resource archives the workspace (`POST /v1/organizations/workspaces/{id}/archive`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique identifier of the workspace (`wrkspc_...`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the workspace.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 100),
				},
			},
			"display_color": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Console display color assigned to the workspace.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the workspace was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"archived_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the workspace was archived, or `null` if active.",
			},
			"data_residency": schema.SingleNestedAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Data residency and inference geography configuration for the workspace.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"workspace_geo": schema.StringAttribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Geographic region where workspace data is stored at rest (e.g. `us`). Immutable after creation.",
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplaceIfConfigured(),
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"default_inference_geo": schema.StringAttribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Default geography used for model inference (`us` or `global`).",
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					"allowed_inference_geos": schema.SetAttribute{
						ElementType:         types.StringType,
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Allowed geographies for model inference (e.g. `[\"us\"]`, `[\"us\", \"global\"]`, or `[\"unrestricted\"]`).",
						Validators: []validator.Set{
							setvalidator.SizeAtLeast(1),
						},
						PlanModifiers: []planmodifier.Set{
							setplanmodifier.UseStateForUnknown(),
						},
					},
				},
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`workspace`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *WorkspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WorkspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dr, diags := expandDataResidency(ctx, plan.DataResidency, true)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ws, err := r.client.CreateWorkspace(ctx, client.CreateWorkspaceRequest{
		Name:          plan.Name.ValueString(),
		DataResidency: dr,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Workspace", fmt.Sprintf("Could not create workspace %q: %s", plan.Name.ValueString(), err))
		return
	}

	resp.Diagnostics.Append(mapWorkspaceToResourceModel(ctx, ws, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ws, err := r.client.GetWorkspace(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Workspace", fmt.Sprintf("Could not read workspace %s: %s", state.ID.ValueString(), err))
		return
	}

	if ws.ArchivedAt != nil && *ws.ArchivedAt != "" {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(mapWorkspaceToResourceModel(ctx, ws, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WorkspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WorkspaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	dr, diags := expandDataResidency(ctx, plan.DataResidency, false)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ws, err := r.client.UpdateWorkspace(ctx, plan.ID.ValueString(), client.UpdateWorkspaceRequest{
		Name:          plan.Name.ValueString(),
		DataResidency: dr,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Workspace", fmt.Sprintf("Could not update workspace %s: %s", plan.ID.ValueString(), err))
		return
	}

	resp.Diagnostics.Append(mapWorkspaceToResourceModel(ctx, ws, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WorkspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ArchiveWorkspace(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) && !client.IsBadRequest(err) {
		resp.Diagnostics.AddError("Error Archiving Workspace", fmt.Sprintf("Could not archive workspace %s: %s", state.ID.ValueString(), err))
	}
}

func (r *WorkspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid Workspace Import ID", fmt.Sprintf("Import ID %q must contain only alphanumeric characters, underscores, or hyphens.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func expandDataResidency(ctx context.Context, obj types.Object, includeWorkspaceGeo bool) (*client.DataResidency, diag.Diagnostics) {
	var diags diag.Diagnostics
	if obj.IsNull() || obj.IsUnknown() {
		return nil, diags
	}

	var m WorkspaceDataResidencyModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, diags
	}

	dr := &client.DataResidency{}
	hasAny := false
	if includeWorkspaceGeo && !m.WorkspaceGeo.IsNull() && !m.WorkspaceGeo.IsUnknown() {
		dr.WorkspaceGeo = m.WorkspaceGeo.ValueString()
		hasAny = true
	}
	if !m.DefaultInferenceGeo.IsNull() && !m.DefaultInferenceGeo.IsUnknown() {
		dr.DefaultInferenceGeo = m.DefaultInferenceGeo.ValueString()
		hasAny = true
	}
	if !m.AllowedInferenceGeos.IsNull() && !m.AllowedInferenceGeos.IsUnknown() {
		var geos []string
		diags.Append(m.AllowedInferenceGeos.ElementsAs(ctx, &geos, false)...)
		if diags.HasError() {
			return nil, diags
		}
		if len(geos) == 1 && geos[0] == "unrestricted" {
			dr.AllowedInferenceGeos = &client.AllowedInferenceGeos{Unrestricted: true}
		} else {
			dr.AllowedInferenceGeos = &client.AllowedInferenceGeos{Geos: geos}
		}
		hasAny = true
	}

	if !hasAny {
		return nil, diags
	}
	return dr, diags
}

func flattenDataResidency(ctx context.Context, dr *client.DataResidency) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	if dr == nil {
		return types.ObjectNull(dataResidencyAttrTypes()), diags
	}

	var allowedSet types.Set
	if dr.AllowedInferenceGeos == nil {
		allowedSet = types.SetNull(types.StringType)
	} else if dr.AllowedInferenceGeos.Unrestricted {
		s, d := types.SetValueFrom(ctx, types.StringType, []string{"unrestricted"})
		diags.Append(d...)
		allowedSet = s
	} else {
		s, d := types.SetValueFrom(ctx, types.StringType, dr.AllowedInferenceGeos.Geos)
		diags.Append(d...)
		allowedSet = s
	}
	if diags.HasError() {
		return types.ObjectNull(dataResidencyAttrTypes()), diags
	}

	wsGeo := types.StringNull()
	if dr.WorkspaceGeo != "" {
		wsGeo = types.StringValue(dr.WorkspaceGeo)
	}
	defGeo := types.StringNull()
	if dr.DefaultInferenceGeo != "" {
		defGeo = types.StringValue(dr.DefaultInferenceGeo)
	}

	obj, d := types.ObjectValue(dataResidencyAttrTypes(), map[string]attr.Value{
		"workspace_geo":          wsGeo,
		"default_inference_geo":  defGeo,
		"allowed_inference_geos": allowedSet,
	})
	diags.Append(d...)
	return obj, diags
}

func mapWorkspaceToResourceModel(ctx context.Context, ws *client.Workspace, m *WorkspaceResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(ws.ID)
	m.Name = types.StringValue(ws.Name)
	m.DisplayColor = types.StringValue(ws.DisplayColor)
	m.CreatedAt = types.StringValue(ws.CreatedAt)
	if ws.ArchivedAt != nil && *ws.ArchivedAt != "" {
		m.ArchivedAt = types.StringValue(*ws.ArchivedAt)
	} else {
		m.ArchivedAt = types.StringNull()
	}
	m.Type = types.StringValue(ws.Type)

	drObj, d := flattenDataResidency(ctx, ws.DataResidency)
	diags.Append(d...)
	m.DataResidency = drObj
	return diags
}
