// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &RateLimitsDataSource{}
	_ datasource.DataSourceWithConfigure = &RateLimitsDataSource{}
)

func NewRateLimitsDataSource() datasource.DataSource {
	return &RateLimitsDataSource{}
}

type RateLimitsDataSource struct {
	client *client.Client
}

type RateLimitsDataSourceModel struct {
	GroupType  types.String          `tfsdk:"group_type"`
	Model      types.String          `tfsdk:"model"`
	RateLimits []RateLimitEntryModel `tfsdk:"rate_limits"`
}

type RateLimitEntryModel struct {
	ID               types.String `tfsdk:"id"`
	GroupID          types.String `tfsdk:"group_id"`
	GroupType        types.String `tfsdk:"group_type"`
	GroupDisplayName types.String `tfsdk:"group_display_name"`
	Models           types.List   `tfsdk:"models"`
	Limits           types.Map    `tfsdk:"limits"`
}

func (d *RateLimitsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rate_limits"
}

func (d *RateLimitsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the organization's Messages API rate limits, one entry per rate limit group (`GET /v1/organizations/rate_limits`).",
		Attributes: map[string]schema.Attribute{
			"group_type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only return groups of this type: `model_group`, `batch`, `token_count`, `files`, `skills`, or `web_search`.",
				Validators: []validator.String{
					stringvalidator.OneOf("batch", "files", "model_group", "skills", "token_count", "web_search"),
				},
			},
			"model": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only return the entry containing this model name or alias. The API returns 404 if the model has no rate limits.",
			},
			"rate_limits": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Rate limit entries.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Entry identifier, unique per organization. Use it as `rate_limit_id` on `anthropic_workspace_rate_limit`.",
						},
						"group_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Rate limit group identifier, the same in every organization.",
						},
						"group_type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Group type.",
						},
						"group_display_name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Display name of a model group, for display only. Null for other group types.",
						},
						"models": schema.ListAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Model names and aliases the entry covers. Null when `group_type` is not `model_group`.",
						},
						"limits": schema.MapAttribute{
							Computed:            true,
							ElementType:         types.Int64Type,
							MarkdownDescription: "Limit values keyed by limiter type (for example `requests_per_minute`).",
						},
					},
				},
			},
		},
	}
}

func (d *RateLimitsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *RateLimitsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state RateLimitsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entries, err := d.client.ListOrganizationRateLimits(ctx, state.GroupType.ValueString(), state.Model.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Rate Limits", err.Error())
		return
	}

	state.RateLimits = make([]RateLimitEntryModel, 0, len(entries))
	for _, e := range entries {
		vals := make(map[string]int64, len(e.Limits))
		for _, l := range e.Limits {
			vals[l.Type] = l.Value
		}
		limits, diags := types.MapValueFrom(ctx, types.Int64Type, vals)
		resp.Diagnostics.Append(diags...)
		models := types.ListNull(types.StringType)
		if e.Models != nil {
			models, diags = types.ListValueFrom(ctx, types.StringType, e.Models)
			resp.Diagnostics.Append(diags...)
		}
		state.RateLimits = append(state.RateLimits, RateLimitEntryModel{
			ID:               types.StringValue(e.ID),
			GroupID:          types.StringValue(e.Group.ID),
			GroupType:        types.StringValue(e.Group.Type),
			GroupDisplayName: nullableLimitString(e.Group.DisplayName),
			Models:           models,
			Limits:           limits,
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// nullableLimitString maps an empty API string to null.
func nullableLimitString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
