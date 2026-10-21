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
	_ datasource.DataSource              = &APIKeysDataSource{}
	_ datasource.DataSourceWithConfigure = &APIKeysDataSource{}
)

func NewAPIKeysDataSource() datasource.DataSource {
	return &APIKeysDataSource{}
}

type APIKeysDataSource struct {
	client *client.Client
}

type APIKeysDataSourceModel struct {
	WorkspaceID     types.String            `tfsdk:"workspace_id"`
	Status          types.String            `tfsdk:"status"`
	CreatedByUserID types.String            `tfsdk:"created_by_user_id"`
	APIKeys         []APIKeyDataSourceModel `tfsdk:"api_keys"`
}

func (d *APIKeysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_keys"
}

func (d *APIKeysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists Claude Console API keys in the organization (`GET /v1/organizations/api_keys`), with optional filtering by `workspace_id`, `status`, or `created_by_user_id`.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional workspace ID filter (`wrkspc_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"status": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional status filter (`active`, `inactive`, `archived`).",
				Validators: []validator.String{
					stringvalidator.OneOf("active", "inactive", "archived"),
				},
			},
			"created_by_user_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional filter by creator user ID (`user_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"api_keys": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of organization API keys.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Unique identifier of the API key.",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Display name of the API key.",
						},
						"status": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Operational status of the API key.",
						},
						"partial_key_hint": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Partially redacted hint of the API key.",
						},
						"workspace_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Workspace ID the API key belongs to, or `null` for the default workspace.",
						},
						"created_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the API key was created.",
						},
						"created_by_id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "ID of the user or service account that created the API key.",
						},
						"type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type (`api_key`).",
						},
					},
				},
			},
		},
	}
}

func (d *APIKeysDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *APIKeysDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state APIKeysDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var filter client.APIKeyListFilter
	if !state.WorkspaceID.IsNull() {
		filter.WorkspaceID = state.WorkspaceID.ValueString()
	}
	if !state.Status.IsNull() {
		filter.Status = state.Status.ValueString()
	}
	if !state.CreatedByUserID.IsNull() {
		filter.CreatedByUserID = state.CreatedByUserID.ValueString()
	}

	keys, err := d.client.ListAPIKeys(ctx, filter)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing API Keys", fmt.Sprintf("Could not list API keys: %s", err))
		return
	}

	state.APIKeys = make([]APIKeyDataSourceModel, 0, len(keys))
	for i := range keys {
		var m APIKeyDataSourceModel
		mapAPIKeyToModel(&keys[i], &m)
		state.APIKeys = append(state.APIKeys, m)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
