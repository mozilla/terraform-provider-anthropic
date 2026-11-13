// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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
	_ resource.Resource                = &WebhookResource{}
	_ resource.ResourceWithConfigure   = &WebhookResource{}
	_ resource.ResourceWithImportState = &WebhookResource{}
)

func NewWebhookResource() resource.Resource {
	return &WebhookResource{}
}

type WebhookResource struct {
	client *client.Client
}

// webhookFields are the attributes shared by the resource and the data sources.
type webhookFields struct {
	ID             types.String `tfsdk:"id"`
	URL            types.String `tfsdk:"url"`
	EnabledEvents  types.Set    `tfsdk:"enabled_events"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Enabled        types.Bool   `tfsdk:"enabled"`
	DisabledReason types.String `tfsdk:"disabled_reason"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

type WebhookResourceModel struct {
	webhookFields
	SigningSecret types.String `tfsdk:"signing_secret"`
}

func (r *WebhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *WebhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Manages an organization webhook endpoint (`/v1/organizations/webhooks`). Anthropic must turn the API on for the organization. " +
			"Not available to organizations with HIPAA readiness enabled. The signing secret is returned only on create.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ID of the webhook endpoint.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "HTTPS URL that deliveries are sent to.",
				Validators:          []validator.String{httpsURLValidator()},
			},
			"enabled_events": schema.SetAttribute{
				ElementType: types.StringType,
				Required:    true,
				MarkdownDescription: "Event types delivered to the endpoint, for example `user_profile.enforcement_action_opened`, " +
					"`user_profile.enforcement_action_resolved` or `user_profile.program_status_updated`.",
				Validators: []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Short label for the endpoint.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Notes about the endpoint.",
			},
			"enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether deliveries are sent. Anthropic can disable an endpoint on its own (see `disabled_reason`). The next apply re-enables it. Events that happen while disabled are never delivered.",
			},
			"disabled_reason": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Why Anthropic disabled the endpoint automatically. Empty when enabled or disabled by you.",
			},
			"signing_secret": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "Secret Anthropic signs deliveries with (`whsec_...`). Returned only on create, and `null` after import.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the endpoint was created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the endpoint was last changed.",
			},
		},
	}
}

func (r *WebhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *WebhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WebhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var events []string
	resp.Diagnostics.Append(plan.EnabledEvents.ElementsAs(ctx, &events, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	w, err := r.client.CreateWebhook(ctx, client.CreateWebhookRequest{
		URL:           plan.URL.ValueString(),
		EnabledEvents: events,
		Name:          plan.Name.ValueString(),
		Description:   plan.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Webhook", err.Error())
		return
	}
	plan.SigningSecret = types.StringValue(w.SigningSecret)

	// Create has no state field, so endpoints always start enabled.
	// On failure the created endpoint and its secret are still saved, so the endpoint isn't orphaned.
	if !plan.Enabled.ValueBool() {
		if updated, err := r.client.UpdateWebhook(ctx, w.ID, webhookUpdateRequest(&plan, events)); err != nil {
			resp.Diagnostics.AddError("Error Disabling Webhook", err.Error())
		} else {
			w = updated
		}
	}

	resp.Diagnostics.Append(populateWebhookFields(ctx, &plan.webhookFields, w)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WebhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := r.client.GetWebhook(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Webhook", err.Error())
		return
	}
	// signing_secret is left as is: reads never return it.
	resp.Diagnostics.Append(populateWebhookFields(ctx, &state.webhookFields, w)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WebhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state WebhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	var events []string
	resp.Diagnostics.Append(plan.EnabledEvents.ElementsAs(ctx, &events, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := r.client.UpdateWebhook(ctx, state.ID.ValueString(), webhookUpdateRequest(&plan, events))
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Webhook", err.Error())
		return
	}
	plan.SigningSecret = state.SigningSecret
	resp.Diagnostics.Append(populateWebhookFields(ctx, &plan.webhookFields, w)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WebhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteWebhook(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Deleting Webhook", err.Error())
	}
}

func (r *WebhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid Import Identifier", fmt.Sprintf("Expected webhook ID matching %s, got %q", resourceIDPattern.String(), req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func webhookUpdateRequest(plan *WebhookResourceModel, events []string) client.UpdateWebhookRequest {
	state := "enabled"
	if !plan.Enabled.ValueBool() {
		state = "disabled"
	}
	return client.UpdateWebhookRequest{
		URL:           plan.URL.ValueString(),
		EnabledEvents: events,
		Name:          plan.Name.ValueString(),
		Description:   plan.Description.ValueString(),
		State:         state,
	}
}

func populateWebhookFields(ctx context.Context, m *webhookFields, w *client.Webhook) diag.Diagnostics {
	m.ID = types.StringValue(w.ID)
	m.URL = types.StringValue(w.URL)
	m.Name = types.StringValue(w.Name)
	m.Description = types.StringValue(w.Description)
	m.Enabled = types.BoolValue(w.State == "enabled")
	m.DisabledReason = types.StringValue(w.DisabledReason)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	m.UpdatedAt = types.StringValue(w.UpdatedAt)
	events := w.EnabledEvents
	if events == nil {
		events = []string{}
	}
	var diags diag.Diagnostics
	m.EnabledEvents, diags = types.SetValueFrom(ctx, types.StringType, events)
	return diags
}
