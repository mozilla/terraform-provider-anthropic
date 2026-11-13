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
	_ datasource.DataSourceWithConfigure = &ClaudeTagBundleDataSource{}
	_ datasource.DataSourceWithConfigure = &ClaudeTagBundlesDataSource{}
)

func NewClaudeTagBundleDataSource() datasource.DataSource  { return &ClaudeTagBundleDataSource{} }
func NewClaudeTagBundlesDataSource() datasource.DataSource { return &ClaudeTagBundlesDataSource{} }

type ClaudeTagBundleDataSource struct{ client *client.Client }
type ClaudeTagBundlesDataSource struct{ client *client.Client }

type ClaudeTagBundleModel struct {
	ID          types.String `tfsdk:"id"`
	DisplayName types.String `tfsdk:"display_name"`
	Description types.String `tfsdk:"description"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

type ClaudeTagBundlesModel struct {
	Bundles []ClaudeTagBundleModel `tfsdk:"bundles"`
}

func claudeTagBundleAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":           schema.StringAttribute{MarkdownDescription: "Bundle identifier (`capp_...`).", Computed: true},
		"display_name": schema.StringAttribute{MarkdownDescription: "Bundle name.", Computed: true},
		"description":  schema.StringAttribute{MarkdownDescription: "What the Bundle is for. Null when it has none.", Computed: true},
		"created_at":   schema.StringAttribute{MarkdownDescription: "RFC 3339 timestamp when the Bundle was created.", Computed: true},
		"updated_at":   schema.StringAttribute{MarkdownDescription: "RFC 3339 timestamp of the last change to the Bundle.", Computed: true},
	}
}

func mapClaudeTagBundle(b client.ClaudeTagBundle) ClaudeTagBundleModel {
	return ClaudeTagBundleModel{
		ID:          types.StringValue(b.ID),
		DisplayName: types.StringValue(b.DisplayName),
		Description: types.StringPointerValue(b.Description),
		CreatedAt:   types.StringValue(b.CreatedAt),
		UpdatedAt:   types.StringValue(b.UpdatedAt),
	}
}

func configureClaudeTagDataSource(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
	}
	return c
}

const claudeTagBundleDSNote = "Bundles are created and edited in claude.ai. The API is read-only for them. " +
	"What a Bundle grants (connectors, repositories, credentials) is never returned."

func (d *ClaudeTagBundleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_bundle"
}

func (d *ClaudeTagBundleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := claudeTagBundleAttrs()
	attrs["id"] = schema.StringAttribute{
		MarkdownDescription: "Bundle identifier (`capp_...`).",
		Required:            true,
		Validators:          []validator.String{idValidator()},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Fetches one Claude Tag (Claude in Slack) Bundle (`/v1/organizations/claude_tag/bundles/{bundle_id}`). " + claudeTagBundleDSNote,
		Attributes:          attrs,
	}
}

func (d *ClaudeTagBundleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClaudeTagDataSource(req, resp)
}

func (d *ClaudeTagBundleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ClaudeTagBundleModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b, err := d.client.GetClaudeTagBundle(ctx, data.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Claude Tag Bundle", err.Error())
		return
	}
	data = mapClaudeTagBundle(*b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *ClaudeTagBundlesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_bundles"
}

func (d *ClaudeTagBundlesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Lists the organization's Claude Tag (Claude in Slack) Bundles (`/v1/organizations/claude_tag/bundles`). " + claudeTagBundleDSNote,
		Attributes: map[string]schema.Attribute{
			"bundles": schema.ListNestedAttribute{
				MarkdownDescription: "Bundles in the organization.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: claudeTagBundleAttrs()},
			},
		},
	}
}

func (d *ClaudeTagBundlesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClaudeTagDataSource(req, resp)
}

func (d *ClaudeTagBundlesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	bundles, err := d.client.ListClaudeTagBundles(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Claude Tag Bundles", err.Error())
		return
	}
	state := ClaudeTagBundlesModel{Bundles: make([]ClaudeTagBundleModel, 0, len(bundles))}
	for _, b := range bundles {
		state.Bundles = append(state.Bundles, mapClaudeTagBundle(b))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
