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
	_ datasource.DataSource              = &SpendLimitIncreaseRequestsDataSource{}
	_ datasource.DataSourceWithConfigure = &SpendLimitIncreaseRequestsDataSource{}
)

func NewSpendLimitIncreaseRequestsDataSource() datasource.DataSource {
	return &SpendLimitIncreaseRequestsDataSource{}
}

type SpendLimitIncreaseRequestsDataSource struct {
	client *client.Client
}

type SpendLimitIncreaseRequestsDataSourceModel struct {
	ActorIDs []types.String                   `tfsdk:"actor_ids"`
	Statuses []types.String                   `tfsdk:"statuses"`
	Requests []SpendLimitIncreaseRequestModel `tfsdk:"requests"`
}

type SpendLimitIncreaseRequestModel struct {
	ID           types.String `tfsdk:"id"`
	Status       types.String `tfsdk:"status"`
	Period       types.String `tfsdk:"period"`
	ActorType    types.String `tfsdk:"actor_type"`
	ActorID      types.String `tfsdk:"actor_id"`
	ResolvedByID types.String `tfsdk:"resolved_by_id"`
	CreatedAt    types.String `tfsdk:"created_at"`
	ResolvedAt   types.String `tfsdk:"resolved_at"`
}

func (d *SpendLimitIncreaseRequestsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_spend_limit_increase_requests"
}

func (d *SpendLimitIncreaseRequestsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists members' spend limit increase requests, most recent first (`GET /v1/organizations/spend_limit_increase_requests`). " +
			"Requests from former members are excluded. Approving or denying a request is an action and is not managed by Terraform.",
		Attributes: map[string]schema.Attribute{
			"actor_ids": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Only return requests from these members (`user_...` IDs).",
				Validators: []validator.List{
					listvalidator.ValueStringsAre(idValidator()),
				},
			},
			"statuses": schema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Only return requests with these statuses: `pending`, `approved`, `denied`.",
				Validators: []validator.List{
					listvalidator.ValueStringsAre(stringvalidator.OneOf("pending", "approved", "denied")),
				},
			},
			"requests": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Increase requests.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.StringAttribute{Computed: true, MarkdownDescription: "Request identifier."},
						"status":         schema.StringAttribute{Computed: true, MarkdownDescription: "`pending`, `approved`, or `denied`."},
						"period":         schema.StringAttribute{Computed: true, MarkdownDescription: "Spend limit period the request is for."},
						"actor_type":     schema.StringAttribute{Computed: true, MarkdownDescription: "`user_actor` or `scoped_api_key_actor`."},
						"actor_id":       schema.StringAttribute{Computed: true, MarkdownDescription: "User ID (or scoped API key ID) that submitted the request."},
						"resolved_by_id": schema.StringAttribute{Computed: true, MarkdownDescription: "User ID (or scoped API key ID) that approved or denied the request. Null while pending."},
						"created_at":     schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 timestamp when the request was created."},
						"resolved_at":    schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 timestamp when the request was resolved. Null while pending."},
					},
				},
			},
		},
	}
}

func (d *SpendLimitIncreaseRequestsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *SpendLimitIncreaseRequestsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state SpendLimitIncreaseRequestsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	reqs, err := d.client.ListSpendLimitIncreaseRequests(ctx, limitStringValues(state.ActorIDs), limitStringValues(state.Statuses))
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Spend Limit Increase Requests", err.Error())
		return
	}

	state.Requests = make([]SpendLimitIncreaseRequestModel, 0, len(reqs))
	for _, r := range reqs {
		resolvedBy := types.StringNull()
		if r.ResolvedBy != nil {
			resolvedBy = nullableLimitString(r.ResolvedBy.UserID + r.ResolvedBy.ScopedAPIKeyID)
		}
		state.Requests = append(state.Requests, SpendLimitIncreaseRequestModel{
			ID:           types.StringValue(r.ID),
			Status:       types.StringValue(r.Status),
			Period:       types.StringValue(r.Period),
			ActorType:    types.StringValue(r.Actor.Type),
			ActorID:      nullableLimitString(r.Actor.UserID + r.Actor.ScopedAPIKeyID),
			ResolvedByID: resolvedBy,
			CreatedAt:    types.StringValue(r.CreatedAt),
			ResolvedAt:   types.StringPointerValue(r.ResolvedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
