// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &InvitesDataSource{}
	_ datasource.DataSourceWithConfigure = &InvitesDataSource{}
)

func NewInvitesDataSource() datasource.DataSource {
	return &InvitesDataSource{}
}

type InvitesDataSource struct {
	client *client.Client
}

type InvitesDataSourceModel struct {
	Invites []InviteDataSourceModel `tfsdk:"invites"`
}

func (d *InvitesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invites"
}

func (d *InvitesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists all invitations in the Anthropic organization (`GET /v1/organizations/invites`).",
		Attributes: map[string]schema.Attribute{
			"invites": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of organization invitations.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Unique identifier of the invite.",
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
				},
			},
		},
	}
}

func (d *InvitesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *InvitesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	invites, err := d.client.ListInvites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Organization Invites", fmt.Sprintf("Could not list invites: %s", err))
		return
	}

	var state InvitesDataSourceModel
	state.Invites = make([]InviteDataSourceModel, 0, len(invites))
	for _, inv := range invites {
		state.Invites = append(state.Invites, InviteDataSourceModel{
			ID:        types.StringValue(inv.ID),
			Email:     types.StringValue(inv.Email),
			Role:      types.StringValue(inv.Role),
			Status:    types.StringValue(inv.Status),
			InvitedAt: types.StringValue(inv.InvitedAt),
			ExpiresAt: types.StringValue(inv.ExpiresAt),
			Type:      types.StringValue(inv.Type),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
