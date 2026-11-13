// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                = &TelemetryDestinationResource{}
	_ resource.ResourceWithConfigure   = &TelemetryDestinationResource{}
	_ resource.ResourceWithImportState = &TelemetryDestinationResource{}
)

func NewTelemetryDestinationResource() resource.Resource {
	return &TelemetryDestinationResource{}
}

type TelemetryDestinationResource struct {
	client *client.Client
}

// telemetryDestinationFields are the attributes shared by the resource and the data sources.
type telemetryDestinationFields struct {
	ID                    types.String `tfsdk:"id"`
	DisplayName           types.String `tfsdk:"display_name"`
	EndpointURL           types.String `tfsdk:"endpoint_url"`
	HeaderNames           types.List   `tfsdk:"header_names"`
	WorkspaceScope        types.String `tfsdk:"workspace_scope"`
	Enabled               types.Bool   `tfsdk:"enabled"`
	DisabledReason        types.String `tfsdk:"disabled_reason"`
	TracesEnabled         types.Bool   `tfsdk:"traces_enabled"`
	TracesContentMode     types.String `tfsdk:"traces_content_mode"`
	TracesEndpointURL     types.String `tfsdk:"traces_endpoint_url"`
	TracesMaxContentBytes types.Int64  `tfsdk:"traces_max_content_bytes"`
	CreatedAt             types.String `tfsdk:"created_at"`
	UpdatedAt             types.String `tfsdk:"updated_at"`
}

type TelemetryDestinationResourceModel struct {
	telemetryDestinationFields
	Headers types.Map `tfsdk:"headers"`
}

func (r *TelemetryDestinationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_telemetry_destination"
}

func (r *TelemetryDestinationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Manages an OpenTelemetry (OTLP/HTTP) telemetry destination for the organization " +
			"(`/v1/organizations/telemetry_destinations`). " +
			"Use `anthropic_telemetry_destination_workspace` to manage the workspace list when `workspace_scope` is `include` or `exclude`. " +
			"Changing `workspace_scope` empties the workspace list. Apply again to re-attach those resources.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique identifier of the destination (`tdst_...`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"display_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable name, 1 to 255 characters.",
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"endpoint_url": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Base HTTPS URL of the collector's OTLP/HTTP endpoint, without `/v1/traces`. The API appends it.",
				Validators:          []validator.String{httpsURLValidator()},
			},
			"headers": schema.MapAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Sensitive:   true,
				MarkdownDescription: "Request headers sent with every delivery, typically the collector's authentication header. " +
					"Values are write-only. The API never returns them, so a value changed outside Terraform is not detected. " +
					"They are not populated on import.",
			},
			"header_names": schema.ListAttribute{
				ElementType:         types.StringType,
				Computed:            true,
				MarkdownDescription: "Names of the registered request headers, as returned by the API.",
			},
			"workspace_scope": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Which workspaces the destination covers: `all`, `include` (only listed workspaces) or `exclude` (all but listed workspaces). " +
					"Changing it empties the destination's workspace list.",
				Validators: []validator.String{stringvalidator.OneOf("all", "include", "exclude")},
			},
			"enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the destination receives deliveries. The API can disable a destination on its own, for example after sustained delivery failures. The next apply re-enables it.",
			},
			"disabled_reason": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Why the destination is disabled (`caller`, `sustained_failure`, `invalid_address`, `redirect`), or `null` when enabled.",
			},
			"traces_enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether the destination receives traces.",
			},
			"traces_content_mode": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("metadata_only"),
				MarkdownDescription: "What exported spans contain: `metadata_only` or `content` (prompts, responses, and tool inputs and outputs).",
				Validators:          []validator.String{stringvalidator.OneOf("metadata_only", "content")},
			},
			"traces_endpoint_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Full traces URL for a collector that doesn't take traces on `/v1/traces`. Must have the same scheme, host and port as `endpoint_url`.",
				Validators:          []validator.String{httpsURLValidator()},
			},
			"traces_max_content_bytes": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Most bytes of content one exported span can contain. Unset means only the API's maximum applies.",
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the destination was created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp of the last configuration or state change.",
			},
		},
	}
}

func (r *TelemetryDestinationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TelemetryDestinationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TelemetryDestinationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	headers := map[string]string{}
	resp.Diagnostics.Append(plan.Headers.ElementsAs(ctx, &headers, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tracesEnabled := plan.TracesEnabled.ValueBool()
	d, err := r.client.CreateTelemetryDestination(ctx, client.CreateTelemetryDestinationRequest{
		EndpointURL: plan.EndpointURL.ValueString(),
		Workspaces:  client.TelemetryDestinationScopeRef{Type: plan.WorkspaceScope.ValueString()},
		DisplayName: plan.DisplayName.ValueStringPointer(),
		Headers:     headers,
		Traces: &client.TelemetryTraces{
			ContentMode:     plan.TracesContentMode.ValueString(),
			Enabled:         &tracesEnabled,
			EndpointURL:     plan.TracesEndpointURL.ValueStringPointer(),
			MaxContentBytes: plan.TracesMaxContentBytes.ValueInt64Pointer(),
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Telemetry Destination", err.Error())
		return
	}

	// Create has no state field, so destinations always start enabled.
	// On failure the created destination is still saved, so it isn't orphaned.
	if !plan.Enabled.ValueBool() {
		if updated, err := r.client.UpdateTelemetryDestination(ctx, d.ID, map[string]any{"state": map[string]any{"type": "disabled"}}); err != nil {
			resp.Diagnostics.AddError("Error Disabling Telemetry Destination", err.Error())
		} else {
			d = updated
		}
	}

	resp.Diagnostics.Append(populateTelemetryDestinationFields(ctx, &plan.telemetryDestinationFields, d)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TelemetryDestinationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TelemetryDestinationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	d, err := r.client.GetTelemetryDestination(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Telemetry Destination", err.Error())
		return
	}
	resp.Diagnostics.Append(populateTelemetryDestinationFields(ctx, &state.telemetryDestinationFields, d)...)

	// Header values are write-only. Drop any that the API no longer reports so the next plan sends them again.
	if !state.Headers.IsNull() && !state.Headers.IsUnknown() {
		headers := map[string]string{}
		resp.Diagnostics.Append(state.Headers.ElementsAs(ctx, &headers, false)...)
		registered := map[string]bool{}
		for _, n := range d.HeaderNames {
			registered[strings.ToLower(n)] = true
		}
		for k := range headers {
			if !registered[strings.ToLower(k)] {
				delete(headers, k)
			}
		}
		m, diags := types.MapValueFrom(ctx, types.StringType, headers)
		resp.Diagnostics.Append(diags...)
		state.Headers = m
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *TelemetryDestinationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state TelemetryDestinationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	planHeaders := map[string]string{}
	resp.Diagnostics.Append(plan.Headers.ElementsAs(ctx, &planHeaders, false)...)
	var oldNames []string
	resp.Diagnostics.Append(state.HeaderNames.ElementsAs(ctx, &oldNames, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Headers merge by name on the API side: send every configured value, and null for names no longer configured.
	headers := map[string]any{}
	planLower := map[string]bool{}
	for k, v := range planHeaders {
		headers[k] = v
		planLower[strings.ToLower(k)] = true
	}
	for _, n := range oldNames {
		if !planLower[strings.ToLower(n)] {
			headers[n] = nil
		}
	}

	stateType := "enabled"
	if !plan.Enabled.ValueBool() {
		stateType = "disabled"
	}
	body := map[string]any{
		// endpoint_url is required whenever header values are sent. The current URL is accepted.
		"endpoint_url": plan.EndpointURL.ValueString(),
		"display_name": plan.DisplayName.ValueStringPointer(),
		"state":        map[string]any{"type": stateType},
		"traces": map[string]any{
			"enabled":           plan.TracesEnabled.ValueBool(),
			"content_mode":      plan.TracesContentMode.ValueString(),
			"endpoint_url":      plan.TracesEndpointURL.ValueStringPointer(),
			"max_content_bytes": plan.TracesMaxContentBytes.ValueInt64Pointer(),
		},
	}
	if len(headers) > 0 {
		body["headers"] = headers
	}
	if !plan.WorkspaceScope.Equal(state.WorkspaceScope) {
		body["workspaces"] = map[string]any{"type": plan.WorkspaceScope.ValueString()}
	}

	d, err := r.client.UpdateTelemetryDestination(ctx, state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Telemetry Destination", err.Error())
		return
	}

	resp.Diagnostics.Append(populateTelemetryDestinationFields(ctx, &plan.telemetryDestinationFields, d)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TelemetryDestinationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TelemetryDestinationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteTelemetryDestination(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Deleting Telemetry Destination", err.Error())
	}
}

func (r *TelemetryDestinationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid Import Identifier", fmt.Sprintf("Expected telemetry destination ID matching %s, got %q", resourceIDPattern.String(), req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func populateTelemetryDestinationFields(ctx context.Context, m *telemetryDestinationFields, d *client.TelemetryDestination) diag.Diagnostics {
	m.ID = types.StringValue(d.ID)
	m.DisplayName = types.StringPointerValue(d.DisplayName)
	m.EndpointURL = types.StringValue(d.EndpointURL)
	names := append([]string{}, d.HeaderNames...)
	slices.Sort(names)
	var diags diag.Diagnostics
	m.HeaderNames, diags = types.ListValueFrom(ctx, types.StringType, names)
	m.WorkspaceScope = types.StringValue(d.Workspaces.Type)
	m.Enabled = types.BoolValue(d.State.Type == "enabled")
	if d.State.Reason != nil {
		m.DisabledReason = types.StringValue(d.State.Reason.Type)
	} else {
		m.DisabledReason = types.StringNull()
	}
	m.TracesEnabled = types.BoolPointerValue(d.Traces.Enabled)
	m.TracesContentMode = types.StringValue(d.Traces.ContentMode)
	m.TracesEndpointURL = types.StringPointerValue(d.Traces.EndpointURL)
	m.TracesMaxContentBytes = types.Int64PointerValue(d.Traces.MaxContentBytes)
	m.CreatedAt = types.StringValue(d.CreatedAt)
	m.UpdatedAt = types.StringValue(d.UpdatedAt)
	return diags
}
