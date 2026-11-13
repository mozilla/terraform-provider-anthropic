// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	slackWorkspaceIDPattern = regexp.MustCompile(`^T[A-Z0-9]+$`)
	slackChannelIDPattern   = regexp.MustCompile(`^[CG][A-Z0-9]+$`)

	claudeTagGuestPolicies = []string{"allow", "restrict", "channel_only"}
	claudeTagReplyModes    = []string{"automatic", "mention_only", "always_awake", "thread"}
)

var (
	_ resource.Resource                = &ClaudeTagSlackChannelResource{}
	_ resource.ResourceWithConfigure   = &ClaudeTagSlackChannelResource{}
	_ resource.ResourceWithImportState = &ClaudeTagSlackChannelResource{}
)

func NewClaudeTagSlackChannelResource() resource.Resource {
	return &ClaudeTagSlackChannelResource{}
}

type ClaudeTagSlackChannelResource struct {
	client *client.Client
}

// ClaudeTagSettingsModel holds the settings a channel or workspace can store. Null means not stored (inherited).
type ClaudeTagSettingsModel struct {
	Enabled       types.Bool   `tfsdk:"enabled"`
	EnvironmentID types.String `tfsdk:"environment_id"`
	GuestPolicy   types.String `tfsdk:"guest_policy"`
	Instructions  types.String `tfsdk:"instructions"`
	Model         types.String `tfsdk:"model"`
}

type ClaudeTagSlackChannelModel struct {
	ID               types.String `tfsdk:"id"`
	SlackWorkspaceID types.String `tfsdk:"slack_workspace_id"`
	SlackChannelID   types.String `tfsdk:"slack_channel_id"`
	ClaudeTagSettingsModel
	ReplyMode types.String `tfsdk:"reply_mode"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

const (
	claudeTagEnabledDesc       = "Whether Claude is on. `true` on a channel turns Claude on even when its workspace stores `false`. Null means not stored here (inherited)."
	claudeTagEnvironmentDesc   = "Environment Claude's sessions run in (`env_...`) or a self-hosted runner pool (`ccpool_...`). Null means not stored here (inherited)."
	claudeTagGuestPolicyDesc   = "What Claude does while a Slack guest is in the channel: `allow`, `restrict`, or `channel_only`. Null means not stored here (inherited)."
	claudeTagInstructionsDesc  = "Instructions for Claude, up to 8,192 characters. Instructions add up across settings levels, broadest first. Null means none stored here."
	claudeTagModelDesc         = "Model identifier or model line name Claude uses. Null means not stored here (inherited)."
	claudeTagReplyModeDesc     = "How Claude takes part in the channel: `automatic`, `mention_only`, `always_awake`, or `thread`."
	claudeTagSlackWorkspaceDsc = "Slack's own workspace identifier (`T...`), not the Claude Tag workspace `id`."
	claudeTagSlackChannelDsc   = "Slack's own channel identifier (`C...` or `G...`), not the Claude Tag channel `id`."
)

func (r *ClaudeTagSlackChannelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_slack_channel"
}

func (r *ClaudeTagSlackChannelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Sets up a Slack channel for Claude Tag (Claude in Slack) and manages its settings " +
			"(`/v1/organizations/claude_tag/slack_channels`). The Slack workspace must already be connected in claude.ai. " +
			"If the channel is already set up, create adopts it. Settings left unset are cleared so they are inherited.\n\n" +
			"Destroy only removes the channel from Terraform state. The channel keeps its settings and history. " +
			"To turn Claude off, set `enabled = false`. To delete the channel, use claude.ai or the API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Claude Tag channel identifier (`slch_...`).",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"slack_workspace_id": schema.StringAttribute{
				MarkdownDescription: claudeTagSlackWorkspaceDsc,
				Required:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.RegexMatches(slackWorkspaceIDPattern, "must be a Slack workspace ID starting with T")},
			},
			"slack_channel_id": schema.StringAttribute{
				MarkdownDescription: claudeTagSlackChannelDsc,
				Required:            true,
				PlanModifiers:       replace,
				Validators:          []validator.String{stringvalidator.RegexMatches(slackChannelIDPattern, "must be a Slack channel ID starting with C or G")},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: claudeTagEnabledDesc,
				Optional:            true,
			},
			"environment_id": schema.StringAttribute{
				MarkdownDescription: claudeTagEnvironmentDesc,
				Optional:            true,
				Validators:          []validator.String{idValidator()},
			},
			"guest_policy": schema.StringAttribute{
				MarkdownDescription: claudeTagGuestPolicyDesc,
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf(claudeTagGuestPolicies...)},
			},
			"instructions": schema.StringAttribute{
				MarkdownDescription: claudeTagInstructionsDesc,
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 8192)},
			},
			"model": schema.StringAttribute{
				MarkdownDescription: claudeTagModelDesc,
				Optional:            true,
				Validators:          []validator.String{nonEmptyStringValidator()},
			},
			"reply_mode": schema.StringAttribute{
				MarkdownDescription: claudeTagReplyModeDesc + " A stored reply mode can be changed but never cleared. Removing it from " +
					"the configuration keeps the current value. Null means none stored yet, and Claude follows the mode chosen in Slack or `automatic`.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: keep,
				Validators:    []validator.String{stringvalidator.OneOf(claudeTagReplyModes...)},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the channel was set up.",
				Computed:            true,
				PlanModifiers:       keep,
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp of the last change to Claude Tag's record of the channel.",
				Computed:            true,
			},
		},
	}
}

func (r *ClaudeTagSlackChannelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData),
		)
		return
	}
	r.client = c
}

func (r *ClaudeTagSlackChannelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ClaudeTagSlackChannelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Set up first and save state, so a refused settings update leaves a tainted resource instead of an orphan.
	// Replacing the tainted resource is safe because destroy only removes it from state.
	ch, err := r.client.CreateClaudeTagSlackChannel(ctx, client.CreateClaudeTagSlackChannelRequest{
		Slack: client.ClaudeTagSlackIdentifiers{
			WorkspaceID: plan.SlackWorkspaceID.ValueString(),
			ChannelID:   plan.SlackChannelID.ValueString(),
		},
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Setting Up Claude Tag Slack Channel", err.Error())
		return
	}
	desired := plan
	mapClaudeTagSlackChannel(ch, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ch, err = r.client.UpdateClaudeTagSlackChannel(ctx, ch.ID, client.UpdateClaudeTagSettingsRequest{
		Settings: claudeTagSettingsRequest(desired.ClaudeTagSettingsModel, desired.ReplyMode),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Applying Claude Tag Slack Channel Settings",
			fmt.Sprintf("The channel was set up as %s, but its settings were not applied: %s\n\n"+
				"Terraform marks the resource tainted. The next apply adopts the same channel and applies the settings again.", plan.ID.ValueString(), err))
		return
	}
	mapClaudeTagSlackChannel(ch, &desired)
	resp.Diagnostics.Append(resp.State.Set(ctx, &desired)...)
}

func (r *ClaudeTagSlackChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ClaudeTagSlackChannelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ch, err := r.client.GetClaudeTagSlackChannel(ctx, state.ID.ValueString(), false)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Claude Tag Slack Channel", err.Error())
		return
	}
	mapClaudeTagSlackChannel(ch, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ClaudeTagSlackChannelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ClaudeTagSlackChannelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ch, err := r.client.UpdateClaudeTagSlackChannel(ctx, plan.ID.ValueString(), client.UpdateClaudeTagSettingsRequest{
		Settings: claudeTagSettingsRequest(plan.ClaudeTagSettingsModel, plan.ReplyMode),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating Claude Tag Slack Channel", err.Error())
		return
	}
	mapClaudeTagSlackChannel(ch, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete only forgets the channel. The API delete also erases the channel's Claude history, so it is left to claude.ai or the API.
func (r *ClaudeTagSlackChannelResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Claude Tag Slack Channel Left In Place",
		"anthropic_claude_tag_slack_channel was removed from Terraform state. The channel, its settings and its Claude history were not changed. "+
			"To delete the channel and its history, use claude.ai or the API.",
	)
}

func (r *ClaudeTagSlackChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !claudeTagSlackChannelIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid Import Identifier",
			fmt.Sprintf("Expected a Claude Tag Slack channel ID matching %s, got %q", claudeTagSlackChannelIDPattern.String(), req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// claudeTagSettingsRequest sends every managed setting, with null for unset ones so the server clears them.
// reply_mode is only sent when set, because null can never clear it and is refused on a channel that stores one.
func claudeTagSettingsRequest(s ClaudeTagSettingsModel, replyMode types.String) client.ClaudeTagSettings {
	out := client.ClaudeTagSettings{
		Enabled:       s.Enabled.ValueBoolPointer(),
		EnvironmentID: s.EnvironmentID.ValueStringPointer(),
		Instructions:  s.Instructions.ValueStringPointer(),
		Model:         s.Model.ValueStringPointer(),
	}
	if !s.GuestPolicy.IsNull() {
		out.GuestPolicy = &client.ClaudeTagTypeRef{Type: s.GuestPolicy.ValueString()}
	}
	if !replyMode.IsNull() && !replyMode.IsUnknown() {
		out.ReplyMode = &client.ClaudeTagTypeRef{Type: replyMode.ValueString()}
	}
	return out
}

func typeRefValue(t *client.ClaudeTagTypeRef) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.Type)
}

// mapClaudeTagSettings copies API settings into m. The server trims instructions, so a prior value
// that only differs by surrounding white space (a heredoc's trailing newline) is kept.
func mapClaudeTagSettings(s client.ClaudeTagSettings, m *ClaudeTagSettingsModel) {
	m.Enabled = types.BoolPointerValue(s.Enabled)
	m.EnvironmentID = types.StringPointerValue(s.EnvironmentID)
	m.GuestPolicy = typeRefValue(s.GuestPolicy)
	m.Model = types.StringPointerValue(s.Model)
	prior := m.Instructions
	m.Instructions = types.StringPointerValue(s.Instructions)
	if s.Instructions != nil && !prior.IsNull() && !prior.IsUnknown() && strings.TrimSpace(prior.ValueString()) == *s.Instructions {
		m.Instructions = prior
	}
}

func mapClaudeTagSlackChannel(ch *client.ClaudeTagSlackChannel, m *ClaudeTagSlackChannelModel) {
	m.ID = types.StringValue(ch.ID)
	m.SlackWorkspaceID = types.StringValue(ch.Slack.WorkspaceID)
	m.SlackChannelID = types.StringValue(ch.Slack.ChannelID)
	mapClaudeTagSettings(ch.Settings, &m.ClaudeTagSettingsModel)
	m.ReplyMode = typeRefValue(ch.Settings.ReplyMode)
	m.CreatedAt = types.StringValue(ch.CreatedAt)
	m.UpdatedAt = types.StringValue(ch.UpdatedAt)
}
