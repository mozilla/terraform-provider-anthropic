// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource                     = &WorkspaceDataSource{}
	_ datasource.DataSourceWithConfigure        = &WorkspaceDataSource{}
	_ datasource.DataSourceWithConfigValidators = &WorkspaceDataSource{}
)

func NewWorkspaceDataSource() datasource.DataSource {
	return &WorkspaceDataSource{}
}

type WorkspaceDataSource struct {
	client *client.Client
}

type WorkspaceDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	DisplayColor  types.String `tfsdk:"display_color"`
	CreatedAt     types.String `tfsdk:"created_at"`
	ArchivedAt    types.String `tfsdk:"archived_at"`
	DataResidency types.Object `tfsdk:"data_residency"`
	Type          types.String `tfsdk:"type"`
}

func (d *WorkspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *WorkspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single Claude Console workspace by `id` or `name` (`/v1/organizations/workspaces`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Unique identifier of the workspace (`wrkspc_...`). Exactly one of `id` or `name` must be set.",
				Validators:          []validator.String{idValidator()},
			},
			"name": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Display name of the workspace. Exactly one of `id` or `name` must be set.",
				Validators:          []validator.String{nonEmptyStringValidator()},
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
	}
}

func (d *WorkspaceDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(
			path.MatchRoot("id"),
			path.MatchRoot("name"),
		),
	}
}

func (d *WorkspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *WorkspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state WorkspaceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var resolved *client.Workspace
	if !state.ID.IsNull() {
		ws, err := d.client.GetWorkspace(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error Reading Workspace", fmt.Sprintf("Could not read workspace %s: %s", state.ID.ValueString(), err))
			return
		}
		resolved = ws
	} else {
		targetName := state.Name.ValueString()
		all, err := d.client.ListWorkspaces(ctx, false)
		if err != nil {
			resp.Diagnostics.AddError("Error Listing Workspaces", fmt.Sprintf("Could not list workspaces: %s", err))
			return
		}
		var matches []client.Workspace
		for _, ws := range all {
			if ws.Name == targetName {
				matches = append(matches, ws)
			}
		}
		if len(matches) == 0 {
			resp.Diagnostics.AddError("Workspace Not Found", fmt.Sprintf("No active workspace found with name %q.", targetName))
			return
		}
		if len(matches) > 1 {
			resp.Diagnostics.AddError("Multiple Workspaces Found", fmt.Sprintf("Found %d workspaces named %q; look up by id instead.", len(matches), targetName))
			return
		}
		resolved = &matches[0]
	}

	var resModel WorkspaceResourceModel
	resp.Diagnostics.Append(mapWorkspaceToResourceModel(ctx, resolved, &resModel)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state = WorkspaceDataSourceModel(resModel)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
