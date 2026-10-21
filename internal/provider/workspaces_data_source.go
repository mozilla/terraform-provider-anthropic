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
	_ datasource.DataSource              = &WorkspacesDataSource{}
	_ datasource.DataSourceWithConfigure = &WorkspacesDataSource{}
)

func NewWorkspacesDataSource() datasource.DataSource {
	return &WorkspacesDataSource{}
}

type WorkspacesDataSource struct {
	client *client.Client
}

type WorkspacesDataSourceModel struct {
	IncludeArchived types.Bool                 `tfsdk:"include_archived"`
	Workspaces      []WorkspaceDataSourceModel `tfsdk:"workspaces"`
}

func (d *WorkspacesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspaces"
}

func (d *WorkspacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists Claude Console workspaces in the organization (`GET /v1/organizations/workspaces`).",
		Attributes: map[string]schema.Attribute{
			"include_archived": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Whether to include archived workspaces in the returned list. Defaults to `false`.",
			},
			"workspaces": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "List of workspaces.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Unique identifier of the workspace.",
						},
						"name": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Display name of the workspace.",
						},
						"display_color": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Display color of the workspace.",
						},
						"created_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the workspace was created.",
						},
						"archived_at": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "RFC 3339 timestamp when the workspace was archived, or `null` if active.",
						},
						"data_residency": schema.SingleNestedAttribute{
							Computed:            true,
							MarkdownDescription: "Data residency settings of the workspace.",
							Attributes: map[string]schema.Attribute{
								"workspace_geo": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "Geographic region where workspace data is stored at rest.",
								},
								"default_inference_geo": schema.StringAttribute{
									Computed:            true,
									MarkdownDescription: "Default geography for model inference.",
								},
								"allowed_inference_geos": schema.SetAttribute{
									ElementType:         types.StringType,
									Computed:            true,
									MarkdownDescription: "Allowed geographies for model inference.",
								},
							},
						},
						"type": schema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Object type (`workspace`).",
						},
					},
				},
			},
		},
	}
}

func (d *WorkspacesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *WorkspacesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspacesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	includeArchived := !state.IncludeArchived.IsNull() && state.IncludeArchived.ValueBool()
	workspaces, err := d.client.ListWorkspaces(ctx, includeArchived)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Workspaces", fmt.Sprintf("Could not list workspaces: %s", err))
		return
	}

	state.Workspaces = make([]WorkspaceDataSourceModel, 0, len(workspaces))
	for i := range workspaces {
		var rm WorkspaceResourceModel
		resp.Diagnostics.Append(mapWorkspaceToResourceModel(ctx, &workspaces[i], &rm)...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Workspaces = append(state.Workspaces, WorkspaceDataSourceModel(rm))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
