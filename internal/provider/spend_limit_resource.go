// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/boolvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &SpendLimitResource{}
	_ resource.ResourceWithConfigure   = &SpendLimitResource{}
	_ resource.ResourceWithImportState = &SpendLimitResource{}
)

func NewSpendLimitResource() resource.Resource {
	return &SpendLimitResource{}
}

type SpendLimitResource struct {
	client *client.Client
}

type SpendLimitResourceModel struct {
	ID        types.String `tfsdk:"id"`
	UserID    types.String `tfsdk:"user_id"`
	Period    types.String `tfsdk:"period"`
	Amount    types.String `tfsdk:"amount"`
	Unlimited types.Bool   `tfsdk:"unlimited"`
	Currency  types.String `tfsdk:"currency"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func (r *SpendLimitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spend_limit"
}

func (r *SpendLimitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a per-user Claude Enterprise spend limit override (`/v1/organizations/spend_limits`). " +
			"To prevent accidental unlimited spend overrides when variables are null, you must configure **either** `amount` (in minor currency units / cents) **or** `unlimited = true`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the spend limit (`spl_...`).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the organization user to whom this spend limit applies.",
				Required:            true,
				Validators: []validator.String{
					idValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"period": schema.StringAttribute{
				MarkdownDescription: "Billing period for the spend limit. Currently only `monthly` is supported.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("monthly"),
				Validators: []validator.String{
					stringvalidator.OneOf("monthly"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"amount": schema.StringAttribute{
				MarkdownDescription: "Spend cap in minor currency units (cents) as a string, e.g. `\"10000\"` for $100.00 or `\"0\"` for $0.00. " +
					"Mutually exclusive with `unlimited = true`.",
				Optional: true,
				Validators: []validator.String{
					centsAmountValidator(),
					stringvalidator.ExactlyOneOf(path.MatchRoot("unlimited")),
				},
			},
			"unlimited": schema.BoolAttribute{
				MarkdownDescription: "Set to `true` to explicitly grant an unlimited spend override (`\"amount\": null` in the Anthropic API). " +
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

	limit, err := r.client.UpsertSpendLimit(ctx, client.UpsertSpendLimitRequest{
		UserID: plan.UserID.ValueString(),
		Period: plan.Period.ValueString(),
		Amount: amountPtr,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Spend Limit", err.Error())
		return
	}

	mapSpendLimitToState(limit, &plan)
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

	mapSpendLimitToState(limit, &state)
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
		UserID: plan.UserID.ValueString(),
		Period: plan.Period.ValueString(),
		Amount: amountPtr,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Spend Limit", err.Error())
		return
	}

	mapSpendLimitToState(limit, &plan)
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

func mapSpendLimitToState(limit *client.SpendLimit, model *SpendLimitResourceModel) {
	model.ID = types.StringValue(limit.ID)
	model.UserID = types.StringValue(limit.Scope.UserID)
	model.Period = types.StringValue(limit.Period)
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
}
