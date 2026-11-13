// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                   = &SpendLimitResource{}
	_ resource.ResourceWithConfigure      = &SpendLimitResource{}
	_ resource.ResourceWithImportState    = &SpendLimitResource{}
	_ resource.ResourceWithValidateConfig = &SpendLimitResource{}
)

func NewSpendLimitResource() resource.Resource {
	return &SpendLimitResource{}
}

type SpendLimitResource struct {
	client *client.Client
}

type SpendLimitResourceModel struct {
	ID           types.String `tfsdk:"id"`
	UserID       types.String `tfsdk:"user_id"`
	WorkspaceID  types.String `tfsdk:"workspace_id"`
	Organization types.Bool   `tfsdk:"organization"`
	IsEnabled    types.Bool   `tfsdk:"is_enabled"`
	Period       types.String `tfsdk:"period"`
	Amount       types.String `tfsdk:"amount"`
	Unlimited    types.Bool   `tfsdk:"unlimited"`
	Currency     types.String `tfsdk:"currency"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func (r *SpendLimitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spend_limit"
}

func (r *SpendLimitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a spend limit (`/v1/organizations/spend_limits`). Set exactly one scope: " +
			"`user_id` (Claude Enterprise), `workspace_id` or `organization = true` (Claude Console). " +
			"Set either `amount` in cents or `unlimited = true`. " +
			"Workspace and organization limits are early access, monthly and always capped. The Default workspace cannot have one. " +
			"Creating a workspace or organization limit fails if one already exists for that scope and period. Import it instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the spend limit (`spl_...`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the organization user to whom this spend limit applies (Claude Enterprise). Exactly one of `user_id`, `workspace_id`, or `organization` must be set.",
				Optional:            true,
				Validators: []validator.String{
					idValidator(),
					stringvalidator.ExactlyOneOf(path.MatchRoot("workspace_id"), path.MatchRoot("organization")),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the Claude Console workspace to which this spend limit applies (early access).",
				Optional:            true,
				Validators: []validator.String{
					idValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"organization": schema.BoolAttribute{
				MarkdownDescription: "Set to `true` for the Claude Console organization-wide spend limit (early access).",
				Optional:            true,
				Validators: []validator.Bool{
					boolvalidator.Equals(true),
				},
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"is_enabled": schema.BoolAttribute{
				MarkdownDescription: "`false` when extra usage is switched off for the organization (organization limit only). The amount is kept and applies again when switched back on. Always `true` for other scopes.",
				Computed:            true,
			},
			"period": schema.StringAttribute{
				MarkdownDescription: "Window the limit resets over: `daily`, `weekly`, or `monthly` (default). Workspace and organization limits must be `monthly`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("monthly"),
				Validators: []validator.String{
					stringvalidator.OneOf("daily", "weekly", "monthly"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"amount": schema.StringAttribute{
				MarkdownDescription: "Spend cap in minor currency units (cents) as a string, for example `\"10000\"` for $100.00 or `\"0\"` for $0.00. " +
					"Mutually exclusive with `unlimited = true`.",
				Optional: true,
				Validators: []validator.String{
					centsAmountValidator(),
					stringvalidator.ExactlyOneOf(path.MatchRoot("unlimited")),
				},
			},
			"unlimited": schema.BoolAttribute{
				MarkdownDescription: "Set to `true` for an unlimited spend override. " +
					"Mutually exclusive with `amount`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Validators: []validator.Bool{
					boolvalidator.Equals(true),
					boolvalidator.ExactlyOneOf(path.MatchRoot("amount")),
				},
			},
			"currency": schema.StringAttribute{
				MarkdownDescription: "ISO 4217 currency code returned by the API (e.g., `USD`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the spend limit was created.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the spend limit was last updated.",
				Computed:            true,
			},
		},
	}
}

func (r *SpendLimitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SpendLimitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SpendLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var amountPtr *string
	if !plan.Unlimited.ValueBool() && !plan.Amount.IsNull() && !plan.Amount.IsUnknown() {
		amt := plan.Amount.ValueString()
		amountPtr = &amt
	}

	scope := spendLimitScope(&plan)
	// The API upserts by (scope, period), so without this check Create would adopt an existing
	// workspace or organization limit and destroy would then delete it.
	if scope.Type != "user" {
		existing, err := r.client.ListSpendLimits(ctx, []string{scope.Type})
		if err != nil {
			resp.Diagnostics.AddError("Error Checking Existing Spend Limits", spendLimitErrorDetail(&plan, err))
			return
		}
		for _, e := range existing {
			if e.Scope == scope && e.Period == plan.Period.ValueString() {
				resp.Diagnostics.AddError("Spend Limit Already Exists",
					fmt.Sprintf("A %s %s spend limit already exists (%s). Import it instead of creating a new one:\n\n  terraform import anthropic_spend_limit.<name> %s", e.Period, scope.Type, e.ID, e.ID))
				return
			}
		}
	}

	limit, err := r.client.UpsertSpendLimit(ctx, client.UpsertSpendLimitRequest{
		Scope:  scope,
		Period: plan.Period.ValueString(),
		Amount: amountPtr,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Spend Limit", spendLimitErrorDetail(&plan, err))
		return
	}

	resp.Diagnostics.Append(mapSpendLimitToState(limit, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SpendLimitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SpendLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	limit, err := r.client.GetSpendLimit(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Spend Limit", err.Error())
		return
	}

	resp.Diagnostics.Append(mapSpendLimitToState(limit, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SpendLimitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SpendLimitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var amountPtr *string
	if !plan.Unlimited.ValueBool() && !plan.Amount.IsNull() && !plan.Amount.IsUnknown() {
		amt := plan.Amount.ValueString()
		amountPtr = &amt
	}

	limit, err := r.client.UpsertSpendLimit(ctx, client.UpsertSpendLimitRequest{
		Scope:  spendLimitScope(&plan),
		Period: plan.Period.ValueString(),
		Amount: amountPtr,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Spend Limit", spendLimitErrorDetail(&plan, err))
		return
	}

	resp.Diagnostics.Append(mapSpendLimitToState(limit, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SpendLimitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SpendLimitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteSpendLimit(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Deleting Spend Limit", err.Error())
		return
	}
}

func (r *SpendLimitResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError(
			"Invalid Import Identifier",
			fmt.Sprintf("Expected spend limit ID matching %s, got %q", resourceIDPattern.String(), req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *SpendLimitResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg SpendLimitResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.WorkspaceID.IsNull() && cfg.Organization.IsNull() {
		return
	}
	if !cfg.Period.IsNull() && !cfg.Period.IsUnknown() && cfg.Period.ValueString() != "monthly" {
		resp.Diagnostics.AddAttributeError(path.Root("period"), "Invalid Spend Limit Period",
			"Workspace and organization spend limits only support period = \"monthly\".")
	}
	if cfg.Unlimited.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("unlimited"), "Invalid Unlimited Spend Limit",
			"Workspace and organization spend limits always carry an amount. Set amount instead of unlimited = true.")
	}
}

func spendLimitScope(m *SpendLimitResourceModel) client.SpendLimitScope {
	switch {
	case !m.WorkspaceID.IsNull():
		return client.SpendLimitScope{Type: "workspace", WorkspaceID: m.WorkspaceID.ValueString()}
	case m.Organization.ValueBool():
		return client.SpendLimitScope{Type: "organization"}
	default:
		return client.SpendLimitScope{Type: "user", UserID: m.UserID.ValueString()}
	}
}

func spendLimitErrorDetail(m *SpendLimitResourceModel, err error) string {
	if !m.WorkspaceID.IsNull() && (client.IsBadRequest(err) || client.IsForbidden(err)) {
		return err.Error() + "\n\nWorkspace spend limits are a Claude Console early access feature, and the Default workspace cannot have a spend limit. Check that workspace_id names a non-default workspace and that the organization has access."
	}
	return err.Error()
}

func mapSpendLimitToState(limit *client.SpendLimit, model *SpendLimitResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	model.UserID = types.StringNull()
	model.WorkspaceID = types.StringNull()
	model.Organization = types.BoolNull()
	switch limit.Scope.Type {
	case "user":
		model.UserID = types.StringValue(limit.Scope.UserID)
	case "workspace":
		model.WorkspaceID = types.StringValue(limit.Scope.WorkspaceID)
	case "organization":
		model.Organization = types.BoolValue(true)
	default:
		diags.AddError("Unsupported Spend Limit Scope",
			fmt.Sprintf("Spend limit %s has scope type %q, which this resource does not manage (only user, workspace, and organization).", limit.ID, limit.Scope.Type))
		return diags
	}
	model.ID = types.StringValue(limit.ID)
	model.Period = types.StringValue(limit.Period)
	model.IsEnabled = types.BoolValue(limit.IsEnabled == nil || *limit.IsEnabled)
	model.Currency = types.StringValue(limit.Currency)
	model.CreatedAt = types.StringValue(limit.CreatedAt)
	model.UpdatedAt = types.StringValue(limit.UpdatedAt)

	if limit.Amount == nil {
		model.Amount = types.StringNull()
		model.Unlimited = types.BoolValue(true)
	} else {
		model.Amount = types.StringValue(*limit.Amount)
		model.Unlimited = types.BoolValue(false)
	}
	return diags
}
