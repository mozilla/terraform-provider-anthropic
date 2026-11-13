// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                = &WorkspaceRateLimitResource{}
	_ resource.ResourceWithConfigure   = &WorkspaceRateLimitResource{}
	_ resource.ResourceWithImportState = &WorkspaceRateLimitResource{}
)

func NewWorkspaceRateLimitResource() resource.Resource {
	return &WorkspaceRateLimitResource{}
}

type WorkspaceRateLimitResource struct {
	client *client.Client
}

type WorkspaceRateLimitResourceModel struct {
	ID          types.String `tfsdk:"id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	RateLimitID types.String `tfsdk:"rate_limit_id"`
	Limits      types.Map    `tfsdk:"limits"`
}

func (r *WorkspaceRateLimitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_rate_limit"
}

func (r *WorkspaceRateLimitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a workspace's rate limit overrides for one rate limit group (`/v1/organizations/workspaces/{workspace_id}/rate_limits/{rate_limit_id}`). " +
			"Overrides for limiter types not in `limits` are cleared. Destroy clears all of them, and the workspace falls back to the organization limits. " +
			"A value above the organization limit is stored, but the lower of the two is enforced. Changes take effect within about an hour.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite identifier `<workspace_id>/<rate_limit_id>`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the workspace.",
				Required:            true,
				Validators:          []validator.String{idValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"rate_limit_id": schema.StringAttribute{
				MarkdownDescription: "The `id` of the organization rate limit entry to override (from the `anthropic_rate_limits` data source), not the `group_id`.",
				Required:            true,
				Validators:          []validator.String{idValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"limits": schema.MapAttribute{
				MarkdownDescription: "Override values keyed by limiter type: `requests_per_minute`, `input_tokens_per_minute`, `input_tokens_per_minute_cache_aware`, " +
					"`output_tokens_per_minute`, `tokens_per_minute`, `tokens_per_day`, `tool_uses_per_second`, `fast_itpmca`, `fast_otpm`.",
				Required:    true,
				ElementType: types.Int64Type,
				Validators: []validator.Map{
					mapvalidator.SizeAtLeast(1),
					mapvalidator.KeysAre(stringvalidator.OneOf(
						"fast_itpmca",
						"fast_otpm",
						"input_tokens_per_minute",
						"input_tokens_per_minute_cache_aware",
						"output_tokens_per_minute",
						"requests_per_minute",
						"tokens_per_day",
						"tokens_per_minute",
						"tool_uses_per_second",
					)),
				},
			},
		},
	}
}

func (r *WorkspaceRateLimitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WorkspaceRateLimitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WorkspaceRateLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Clear overrides set outside Terraform so the resource is authoritative from the first apply.
	entries, err := r.client.ListWorkspaceRateLimits(ctx, plan.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Workspace Rate Limit", err.Error())
		return
	}
	old := map[string]int64{}
	for _, e := range entries {
		if e.RateLimitID != plan.RateLimitID.ValueString() {
			continue
		}
		for _, l := range e.Limits {
			if isWorkspaceOverride(l) {
				old[l.Type] = l.Value
			}
		}
	}
	r.apply(ctx, &plan, old, &resp.State, &resp.Diagnostics, "Error Setting Workspace Rate Limit")
}

func (r *WorkspaceRateLimitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state WorkspaceRateLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	old, d := rateLimitMap(ctx, state.Limits)
	resp.Diagnostics.Append(d...)
	r.apply(ctx, &plan, old, &resp.State, &resp.Diagnostics, "Error Updating Workspace Rate Limit")
}

// apply sends the planned values and clears any limiter type present in old but absent from the plan.
func (r *WorkspaceRateLimitResource) apply(ctx context.Context, plan *WorkspaceRateLimitResourceModel, old map[string]int64, state *tfsdk.State, diags *diag.Diagnostics, summary string) {
	want, d := rateLimitMap(ctx, plan.Limits)
	diags.Append(d...)
	if diags.HasError() {
		return
	}
	updates := make([]client.WorkspaceRateLimitUpdate, 0, len(want)+len(old))
	for _, k := range slices.Sorted(maps.Keys(want)) {
		v := want[k]
		updates = append(updates, client.WorkspaceRateLimitUpdate{Type: k, Value: &v})
	}
	for _, k := range slices.Sorted(maps.Keys(old)) {
		if _, ok := want[k]; !ok {
			updates = append(updates, client.WorkspaceRateLimitUpdate{Type: k})
		}
	}

	out, err := r.client.UpdateWorkspaceRateLimit(ctx, plan.WorkspaceID.ValueString(), plan.RateLimitID.ValueString(), updates)
	if err != nil {
		diags.AddError(summary, err.Error())
		return
	}
	plan.ID = types.StringValue(plan.WorkspaceID.ValueString() + "/" + plan.RateLimitID.ValueString())
	plan.Limits, d = workspaceOverrides(out)
	diags.Append(d...)
	diags.Append(state.Set(ctx, plan)...)
}

func (r *WorkspaceRateLimitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WorkspaceRateLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entries, err := r.client.ListWorkspaceRateLimits(ctx, state.WorkspaceID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Workspace Rate Limit", err.Error())
		return
	}
	for i := range entries {
		if entries[i].RateLimitID != state.RateLimitID.ValueString() {
			continue
		}
		limits, d := workspaceOverrides(&entries[i])
		resp.Diagnostics.Append(d...)
		if len(limits.Elements()) == 0 {
			break
		}
		state.Limits = limits
		state.ID = types.StringValue(state.WorkspaceID.ValueString() + "/" + state.RateLimitID.ValueString())
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}
	resp.State.RemoveResource(ctx)
}

func (r *WorkspaceRateLimitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WorkspaceRateLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	old, d := rateLimitMap(ctx, state.Limits)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() || len(old) == 0 {
		return
	}
	updates := make([]client.WorkspaceRateLimitUpdate, 0, len(old))
	for _, k := range slices.Sorted(maps.Keys(old)) {
		updates = append(updates, client.WorkspaceRateLimitUpdate{Type: k})
	}
	_, err := r.client.UpdateWorkspaceRateLimit(ctx, state.WorkspaceID.ValueString(), state.RateLimitID.ValueString(), updates)
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Clearing Workspace Rate Limit", err.Error())
	}
}

func (r *WorkspaceRateLimitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	wsID, rlID, err := splitCompositeID(req.ID, "workspace_id", "rate_limit_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid Workspace Rate Limit Import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace_id"), wsID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rate_limit_id"), rlID)...)
}

func rateLimitMap(ctx context.Context, m types.Map) (map[string]int64, diag.Diagnostics) {
	out := map[string]int64{}
	if m.IsNull() || m.IsUnknown() {
		return out, nil
	}
	return out, m.ElementsAs(ctx, &out, false)
}

// workspaceOverrides keeps only values stored on the workspace, not ones inherited from the organization.
func workspaceOverrides(e *client.WorkspaceRateLimit) (types.Map, diag.Diagnostics) {
	vals := map[string]int64{}
	for _, l := range e.Limits {
		if isWorkspaceOverride(l) {
			vals[l.Type] = l.Value
		}
	}
	return types.MapValueFrom(context.Background(), types.Int64Type, vals)
}

func isWorkspaceOverride(l client.RateLimitValue) bool {
	return l.Source != nil && l.Source.Type == "workspace"
}
