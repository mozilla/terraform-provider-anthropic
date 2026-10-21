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
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &APIKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &APIKeyDataSource{}
)

func NewAPIKeyDataSource() datasource.DataSource {
	return &APIKeyDataSource{}
}

type APIKeyDataSource struct {
	client *client.Client
}

type APIKeyDataSourceModel = APIKeyResourceModel

func (d *APIKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (d *APIKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Retrieves metadata for a single Claude Console API key (`GET /v1/organizations/api_keys/{api_key_id}`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier of the API key (`apikey_...`).",
				Validators:          []validator.String{idValidator()},
			},
			"name": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Display name of the API key.",
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Operational status of the API key (`active`, `inactive`, `archived`).",
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
	}
}

func (d *APIKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *APIKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state APIKeyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := d.client.GetAPIKey(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading API Key", fmt.Sprintf("Could not read API key %s: %s", state.ID.ValueString(), err))
		return
	}

	mapAPIKeyToModel(key, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
