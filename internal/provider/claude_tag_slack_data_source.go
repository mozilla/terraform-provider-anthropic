// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSourceWithConfigure = &ClaudeTagSlackWorkspaceDataSource{}
	_ datasource.DataSourceWithConfigure = &ClaudeTagSlackWorkspacesDataSource{}
	_ datasource.DataSourceWithConfigure = &ClaudeTagSlackChannelDataSource{}
	_ datasource.DataSourceWithConfigure = &ClaudeTagSlackChannelsDataSource{}
)

func NewClaudeTagSlackWorkspaceDataSource() datasource.DataSource {
	return &ClaudeTagSlackWorkspaceDataSource{}
}
func NewClaudeTagSlackWorkspacesDataSource() datasource.DataSource {
	return &ClaudeTagSlackWorkspacesDataSource{}
}
func NewClaudeTagSlackChannelDataSource() datasource.DataSource {
	return &ClaudeTagSlackChannelDataSource{}
}
func NewClaudeTagSlackChannelsDataSource() datasource.DataSource {
	return &ClaudeTagSlackChannelsDataSource{}
}

type ClaudeTagSlackWorkspaceDataSource struct{ client *client.Client }
type ClaudeTagSlackWorkspacesDataSource struct{ client *client.Client }
type ClaudeTagSlackChannelDataSource struct{ client *client.Client }
type ClaudeTagSlackChannelsDataSource struct{ client *client.Client }

type ClaudeTagSlackWorkspaceModel struct {
	ID               types.String `tfsdk:"id"`
	SlackWorkspaceID types.String `tfsdk:"slack_workspace_id"`
	ClaudeTagSettingsModel
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

type ClaudeTagSlackWorkspaceDataSourceModel struct {
	ClaudeTagSlackWorkspaceModel
	IncludeInherited types.Bool `tfsdk:"include_inherited"`
}

type ClaudeTagSlackWorkspacesDataSourceModel struct {
	SlackWorkspaceID types.String                   `tfsdk:"slack_workspace_id"`
	IncludeInherited types.Bool                     `tfsdk:"include_inherited"`
	Workspaces       []ClaudeTagSlackWorkspaceModel `tfsdk:"workspaces"`
}

type ClaudeTagSlackChannelDataSourceModel struct {
	ClaudeTagSlackChannelModel
	IncludeInherited types.Bool `tfsdk:"include_inherited"`
}

type ClaudeTagSlackChannelsDataSourceModel struct {
	SlackWorkspaceID types.String                 `tfsdk:"slack_workspace_id"`
	SlackChannelID   types.String                 `tfsdk:"slack_channel_id"`
	IncludeInherited types.Bool                   `tfsdk:"include_inherited"`
	Channels         []ClaudeTagSlackChannelModel `tfsdk:"channels"`
}

const (
	includeInheritedDesc = "When `true`, settings hold what applies once the settings levels above are taken into account, instead of only what is stored at this level."
)

func claudeTagSlackAttrs(channel bool) map[string]schema.Attribute {
	attrs := map[string]schema.Attribute{
		"slack_workspace_id": schema.StringAttribute{MarkdownDescription: claudeTagSlackWorkspaceDsc, Computed: true},
		"enabled":            schema.BoolAttribute{MarkdownDescription: claudeTagEnabledDesc, Computed: true},
		"environment_id":     schema.StringAttribute{MarkdownDescription: claudeTagEnvironmentDesc, Computed: true},
		"guest_policy":       schema.StringAttribute{MarkdownDescription: claudeTagGuestPolicyDesc, Computed: true},
		"instructions":       schema.StringAttribute{MarkdownDescription: claudeTagInstructionsDesc, Computed: true},
		"model":              schema.StringAttribute{MarkdownDescription: claudeTagModelDesc, Computed: true},
		"created_at":         schema.StringAttribute{MarkdownDescription: "RFC 3339 timestamp when it was set up.", Computed: true},
		"updated_at":         schema.StringAttribute{MarkdownDescription: "RFC 3339 timestamp of the last change to Claude Tag's record of it.", Computed: true},
	}
	if channel {
		attrs["id"] = schema.StringAttribute{MarkdownDescription: "Claude Tag channel identifier (`slch_...`).", Computed: true}
		attrs["slack_channel_id"] = schema.StringAttribute{MarkdownDescription: claudeTagSlackChannelDsc, Computed: true}
		attrs["reply_mode"] = schema.StringAttribute{MarkdownDescription: claudeTagReplyModeDesc + " Null when the channel stores none.", Computed: true}
	} else {
		attrs["id"] = schema.StringAttribute{MarkdownDescription: "Claude Tag workspace identifier (`slws_...`).", Computed: true}
	}
	return attrs
}

func includeInheritedAttr() schema.BoolAttribute {
	return schema.BoolAttribute{MarkdownDescription: includeInheritedDesc, Optional: true}
}

func mapClaudeTagSlackWorkspace(ws *client.ClaudeTagSlackWorkspace) ClaudeTagSlackWorkspaceModel {
	m := ClaudeTagSlackWorkspaceModel{
		ID:               types.StringValue(ws.ID),
		SlackWorkspaceID: types.StringValue(ws.Slack.WorkspaceID),
		CreatedAt:        types.StringValue(ws.CreatedAt),
		UpdatedAt:        types.StringValue(ws.UpdatedAt),
	}
	mapClaudeTagSettings(ws.Settings, &m.ClaudeTagSettingsModel)
	return m
}

func mapClaudeTagSlackChannelValue(ch *client.ClaudeTagSlackChannel) ClaudeTagSlackChannelModel {
	var m ClaudeTagSlackChannelModel
	mapClaudeTagSlackChannel(ch, &m)
	return m
}

// Slack workspace (single)

func (d *ClaudeTagSlackWorkspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_slack_workspace"
}

func (d *ClaudeTagSlackWorkspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := claudeTagSlackAttrs(false)
	attrs["id"] = schema.StringAttribute{
		MarkdownDescription: "Claude Tag workspace identifier (`slws_...`).",
		Required:            true,
		Validators:          []validator.String{stringvalidator.RegexMatches(claudeTagSlackWorkspaceIDPattern, "must start with slws_")},
	}
	attrs["include_inherited"] = includeInheritedAttr()
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Fetches one Slack workspace set up for Claude Tag (Claude in Slack) and its settings " +
			"(`/v1/organizations/claude_tag/slack_workspaces/{id}`). " +
			"Workspaces are connected through the Slack app install in claude.ai, so there is no resource for them.",
		Attributes: attrs,
	}
}

func (d *ClaudeTagSlackWorkspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClaudeTagDataSource(req, resp)
}

func (d *ClaudeTagSlackWorkspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ClaudeTagSlackWorkspaceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ws, err := d.client.GetClaudeTagSlackWorkspace(ctx, data.ID.ValueString(), data.IncludeInherited.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Claude Tag Slack Workspace", err.Error())
		return
	}
	data.ClaudeTagSlackWorkspaceModel = mapClaudeTagSlackWorkspace(ws)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Slack workspaces (list)

func (d *ClaudeTagSlackWorkspacesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_slack_workspaces"
}

func (d *ClaudeTagSlackWorkspacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Lists the Slack workspaces set up for Claude Tag (Claude in Slack) (`/v1/organizations/claude_tag/slack_workspaces`).",
		Attributes: map[string]schema.Attribute{
			"slack_workspace_id": schema.StringAttribute{
				MarkdownDescription: "Optional filter: Slack's own workspace identifier (`T...`). Returns that workspace alone, or nothing when it is not set up.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(slackWorkspaceIDPattern, "must be a Slack workspace ID starting with T")},
			},
			"include_inherited": includeInheritedAttr(),
			"workspaces": schema.ListNestedAttribute{
				MarkdownDescription: "Slack workspaces, newest first.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: claudeTagSlackAttrs(false)},
			},
		},
	}
}

func (d *ClaudeTagSlackWorkspacesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClaudeTagDataSource(req, resp)
}

func (d *ClaudeTagSlackWorkspacesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ClaudeTagSlackWorkspacesDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	list, err := d.client.ListClaudeTagSlackWorkspaces(ctx, data.SlackWorkspaceID.ValueString(), data.IncludeInherited.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Claude Tag Slack Workspaces", err.Error())
		return
	}
	data.Workspaces = make([]ClaudeTagSlackWorkspaceModel, 0, len(list))
	for i := range list {
		data.Workspaces = append(data.Workspaces, mapClaudeTagSlackWorkspace(&list[i]))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Slack channel (single)

func (d *ClaudeTagSlackChannelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_slack_channel"
}

func (d *ClaudeTagSlackChannelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := claudeTagSlackAttrs(true)
	attrs["id"] = schema.StringAttribute{
		MarkdownDescription: "Claude Tag channel identifier (`slch_...`).",
		Required:            true,
		Validators:          []validator.String{stringvalidator.RegexMatches(claudeTagSlackChannelIDPattern, "must start with slch_")},
	}
	attrs["include_inherited"] = includeInheritedAttr()
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Fetches one Slack channel set up for Claude Tag (Claude in Slack) and its settings " +
			"(`/v1/organizations/claude_tag/slack_channels/{id}`).",
		Attributes: attrs,
	}
}

func (d *ClaudeTagSlackChannelDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClaudeTagDataSource(req, resp)
}

func (d *ClaudeTagSlackChannelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ClaudeTagSlackChannelDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ch, err := d.client.GetClaudeTagSlackChannel(ctx, data.ID.ValueString(), data.IncludeInherited.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Claude Tag Slack Channel", err.Error())
		return
	}
	data.ClaudeTagSlackChannelModel = mapClaudeTagSlackChannelValue(ch)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Slack channels (list)

func (d *ClaudeTagSlackChannelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_slack_channels"
}

func (d *ClaudeTagSlackChannelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Lists the channels of one Slack workspace that are set up for Claude Tag (Claude in Slack) " +
			"(`/v1/organizations/claude_tag/slack_channels`).",
		Attributes: map[string]schema.Attribute{
			"slack_workspace_id": schema.StringAttribute{
				MarkdownDescription: claudeTagSlackWorkspaceDsc,
				Required:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(slackWorkspaceIDPattern, "must be a Slack workspace ID starting with T")},
			},
			"slack_channel_id": schema.StringAttribute{
				MarkdownDescription: "Optional filter: Slack's own channel identifier (`C...` or `G...`). Returns that channel alone, or nothing when it is not set up.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(slackChannelIDPattern, "must be a Slack channel ID starting with C or G")},
			},
			"include_inherited": includeInheritedAttr(),
			"channels": schema.ListNestedAttribute{
				MarkdownDescription: "Set-up channels of the workspace.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: claudeTagSlackAttrs(true)},
			},
		},
	}
}

func (d *ClaudeTagSlackChannelsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClaudeTagDataSource(req, resp)
}

func (d *ClaudeTagSlackChannelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ClaudeTagSlackChannelsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	list, err := d.client.ListClaudeTagSlackChannels(ctx, data.SlackWorkspaceID.ValueString(), data.SlackChannelID.ValueString(), data.IncludeInherited.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Claude Tag Slack Channels", err.Error())
		return
	}
	data.Channels = make([]ClaudeTagSlackChannelModel, 0, len(list))
	for i := range list {
		data.Channels = append(data.Channels, mapClaudeTagSlackChannelValue(&list[i]))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
