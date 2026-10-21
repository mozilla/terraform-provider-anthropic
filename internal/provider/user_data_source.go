// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource                     = &UserDataSource{}
	_ datasource.DataSourceWithConfigure        = &UserDataSource{}
	_ datasource.DataSourceWithConfigValidators = &UserDataSource{}
)

func NewUserDataSource() datasource.DataSource {
	return &UserDataSource{}
}

type UserDataSource struct {
	client *client.Client
}

type UserDataSourceModel struct {
	ID      types.String `tfsdk:"id"`
	Email   types.String `tfsdk:"email"`
	Name    types.String `tfsdk:"name"`
	Role    types.String `tfsdk:"role"`
	AddedAt types.String `tfsdk:"added_at"`
	Type    types.String `tfsdk:"type"`
}

func (d *UserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *UserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single organization member by `id` or `email` (`/v1/organizations/users`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Unique identifier of the user (`user_...`). Exactly one of `id` or `email` must be set.",
				Validators:          []validator.String{idValidator()},
			},
			"email": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Email address of the user. Exactly one of `id` or `email` must be set.",
				Validators:          []validator.String{nonEmptyStringValidator()},
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the user.",
			},
			"role": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Organization-level role of the user.",
			},
			"added_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the user joined the organization.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`user`).",
			},
		},
	}
}

func (d *UserDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("email"),
		),
	}
}

func (d *UserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *UserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config UserDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var resolved *client.User
	if !config.ID.IsNull() {
		u, err := d.client.GetUser(ctx, config.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error Reading Organization User", fmt.Sprintf("Could not read user %s: %s", config.ID.ValueString(), err))
			return
		}
		resolved = u
	} else {
		targetEmail := config.Email.ValueString()
		users, err := d.client.ListUsers(ctx, targetEmail)
		if err != nil {
			resp.Diagnostics.AddError("Error Listing Organization Users", fmt.Sprintf("Could not search user by email %q: %s", targetEmail, err))
			return
		}
		var matches []client.User
		for _, u := range users {
			if strings.EqualFold(u.Email, targetEmail) {
				matches = append(matches, u)
			}
		}
		if len(matches) == 0 {
			resp.Diagnostics.AddError("User Not Found", fmt.Sprintf("No organization user found with email %q.", targetEmail))
			return
		}
		if len(matches) > 1 {
			resp.Diagnostics.AddError("Multiple Users Found", fmt.Sprintf("Found %d organization users matching email %q; look up by id instead.", len(matches), targetEmail))
			return
		}
		resolved = &matches[0]
	}

	state := UserDataSourceModel{
		ID:      types.StringValue(resolved.ID),
		Email:   types.StringValue(resolved.Email),
		Name:    types.StringValue(resolved.Name),
		Role:    types.StringValue(resolved.Role),
		AddedAt: types.StringValue(resolved.AddedAt),
		Type:    types.StringValue(resolved.Type),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
