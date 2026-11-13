// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &EffectiveSpendLimitsDataSource{}
	_ datasource.DataSourceWithConfigure = &EffectiveSpendLimitsDataSource{}
)

func NewEffectiveSpendLimitsDataSource() datasource.DataSource {
	return &EffectiveSpendLimitsDataSource{}
}

type EffectiveSpendLimitsDataSource struct {
	client *client.Client
}

type EffectiveSpendLimitsDataSourceModel struct {
	UserIDs         []types.String             `tfsdk:"user_ids"`
	Periods         []types.String             `tfsdk:"periods"`
	EffectiveLimits []EffectiveSpendLimitModel `tfsdk:"effective_limits"`
}

type EffectiveSpendLimitModel struct {
	ActorType         types.String `tfsdk:"actor_type"`
	ActorID           types.String `tfsdk:"actor_id"`
	Period            types.String `tfsdk:"period"`
	Amount            types.String `tfsdk:"amount"`
	Currency          types.String `tfsdk:"currency"`
	PeriodToDateSpend types.String `tfsdk:"period_to_date_spend"`
	SpendLimitID      types.String `tfsdk:"spend_limit_id"`
	SourceType        types.String `tfsdk:"source_type"`
	SourceID          types.String `tfsdk:"source_id"`
}

func (d *EffectiveSpendLimitsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_effective_spend_limits"
}

func (d *EffectiveSpendLimitsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists each member's effective spend limit and period-to-date spend, one row per (member, period), with the scope the limit was inherited from " +
			"(`GET /v1/organizations/spend_limits/effective`). Claude Enterprise organizations only.",
		Attributes: map[string]schema.Attribute{
			"user_ids": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Only report these members (`user_...` IDs, at most 100). Omit for all members.",
				Validators: []validator.List{
					listvalidator.SizeAtMost(100),
					listvalidator.ValueStringsAre(idValidator()),
				},
			},
			"periods": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Only report these periods: `daily`, `weekly`, `monthly`. Omit for every period a member resolves a limit for.",
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf("daily", "weekly", "monthly")),
				},
			},
			"effective_limits": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Effective limit rows.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"actor_type":           schema.StringAttribute{Computed: true, MarkdownDescription: "`user_actor` or `scoped_api_key_actor`."},
						"actor_id":             schema.StringAttribute{Computed: true, MarkdownDescription: "User ID, or scoped API key ID for a scoped key actor."},
						"period":               schema.StringAttribute{Computed: true, MarkdownDescription: "Period this row reports."},
						"amount":               schema.StringAttribute{Computed: true, MarkdownDescription: "Effective limit in minor currency units (cents for USD). Null when no limit applies for this period."},
						"currency":             schema.StringAttribute{Computed: true, MarkdownDescription: "ISO 4217 billing currency code."},
						"period_to_date_spend": schema.StringAttribute{Computed: true, MarkdownDescription: "Spend so far this period in minor currency units. It may carry up to three decimal places."},
						"spend_limit_id":       schema.StringAttribute{Computed: true, MarkdownDescription: "ID of the spend limit that applies, if any."},
						"source_type":          schema.StringAttribute{Computed: true, MarkdownDescription: "Scope type the limit was inherited from (`user`, `seat_tier`, `rbac_group`, `organization_service`, `organization`, ...)."},
						"source_id":            schema.StringAttribute{Computed: true, MarkdownDescription: "Identifier of the source scope (user ID, seat tier, RBAC group ID, service, or workspace ID). Null for the organization scope."},
					},
				},
			},
		},
	}
}

func (d *EffectiveSpendLimitsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *EffectiveSpendLimitsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state EffectiveSpendLimitsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rows, err := d.client.ListEffectiveSpendLimits(ctx, limitStringValues(state.UserIDs), limitStringValues(state.Periods))
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Effective Spend Limits", err.Error())
		return
	}

	state.EffectiveLimits = make([]EffectiveSpendLimitModel, 0, len(rows))
	for _, r := range rows {
		// Each actor and source variant carries exactly one identifier field, so concatenation picks it.
		s := r.Source
		state.EffectiveLimits = append(state.EffectiveLimits, EffectiveSpendLimitModel{
			ActorType:         types.StringValue(r.Actor.Type),
			ActorID:           nullableLimitString(r.Actor.UserID + r.Actor.ScopedAPIKeyID),
			Period:            types.StringValue(r.Period),
			Amount:            types.StringPointerValue(r.Amount),
			Currency:          types.StringValue(r.Currency),
			PeriodToDateSpend: types.StringValue(r.PeriodToDateSpend),
			SpendLimitID:      types.StringPointerValue(r.SpendLimitID),
			SourceType:        types.StringValue(s.Type),
			SourceID:          nullableLimitString(s.UserID + s.SeatTier + s.RBACGroupID + s.Service + s.WorkspaceID),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func limitStringValues(in []types.String) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		out = append(out, v.ValueString())
	}
	return out
}
