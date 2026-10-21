// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &InviteDataSource{}
	_ datasource.DataSourceWithConfigure = &InviteDataSource{}
)

func NewInviteDataSource() datasource.DataSource {
	return &InviteDataSource{}
}

type InviteDataSource struct {
	client *client.Client
}

type InviteDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	Email     types.String `tfsdk:"email"`
	Role      types.String `tfsdk:"role"`
	Status    types.String `tfsdk:"status"`
	InvitedAt types.String `tfsdk:"invited_at"`
	ExpiresAt types.String `tfsdk:"expires_at"`
	Type      types.String `tfsdk:"type"`
}

func (d *InviteDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invite"
}

func (d *InviteDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves a single organization invitation by `id` (`GET /v1/organizations/invites/{invite_id}`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier of the invite (`invite_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"email": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Email address of the invited user.",
			},
			"role": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Organization-level role assigned to the invitation.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current status of the invite (`pending`, `accepted`, `expired`, `deleted`).",
			},
			"invited_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the invitation was sent.",
			},
			"expires_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the invitation expires.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`invite`).",
			},
		},
	}
}

func (d *InviteDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *InviteDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state InviteDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inv, err := d.client.GetInvite(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Organization Invite", fmt.Sprintf("Could not read invite %s: %s", state.ID.ValueString(), err))
		return
	}

	state.ID = types.StringValue(inv.ID)
	state.Email = types.StringValue(inv.Email)
	state.Role = types.StringValue(inv.Role)
	state.Status = types.StringValue(inv.Status)
	state.InvitedAt = types.StringValue(inv.InvitedAt)
	state.ExpiresAt = types.StringValue(inv.ExpiresAt)
	state.Type = types.StringValue(inv.Type)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
