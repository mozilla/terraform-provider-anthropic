// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"regexp"

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
	claudeTagSlackChannelIDPattern   = regexp.MustCompile(`^slch_[A-Za-z0-9_-]+$`)
	claudeTagSlackWorkspaceIDPattern = regexp.MustCompile(`^slws_[A-Za-z0-9_-]+$`)
)

var (
	_ resource.Resource                = &ClaudeTagBundleTargetResource{}
	_ resource.ResourceWithConfigure   = &ClaudeTagBundleTargetResource{}
	_ resource.ResourceWithImportState = &ClaudeTagBundleTargetResource{}
)

func NewClaudeTagBundleTargetResource() resource.Resource {
	return &ClaudeTagBundleTargetResource{}
}

type ClaudeTagBundleTargetResource struct {
	client *client.Client
}

type ClaudeTagBundleTargetResourceModel struct {
	ID               types.String `tfsdk:"id"`
	BundleID         types.String `tfsdk:"bundle_id"`
	SlackChannelID   types.String `tfsdk:"slack_channel_id"`
	SlackWorkspaceID types.String `tfsdk:"slack_workspace_id"`
	Type             types.String `tfsdk:"type"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (r *ClaudeTagBundleTargetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claude_tag_bundle_target"
}

func (r *ClaudeTagBundleTargetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Experimental. Attaches a Claude Tag (Claude in Slack) Bundle to a Slack channel or a Slack workspace " +
			"(`/v1/organizations/claude_tag/bundles/{bundle_id}/targets`). " +
			"A Bundle attached to a workspace can be used in every channel in it. The channel or workspace must already be set up. " +
			"All arguments force replacement.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Bundle Target identifier (`btgt_...`).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"bundle_id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the Bundle to attach (`capp_...`).",
				Required:            true,
				Validators:          []validator.String{idValidator()},
				PlanModifiers:       replace,
			},
			"slack_channel_id": schema.StringAttribute{
				MarkdownDescription: "Claude Tag Slack channel identifier (`slch_...`), the `id` of `anthropic_claude_tag_slack_channel`, not Slack's `C...` channel ID. " +
					"Exactly one of `slack_channel_id` or `slack_workspace_id` is required.",
				Optional:      true,
				PlanModifiers: replace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(claudeTagSlackChannelIDPattern, "must be a Claude Tag Slack channel ID starting with slch_"),
					stringvalidator.ExactlyOneOf(path.MatchRoot("slack_workspace_id")),
				},
			},
			"slack_workspace_id": schema.StringAttribute{
				MarkdownDescription: "Claude Tag Slack workspace identifier (`slws_...`), not Slack's `T...` workspace ID.",
				Optional:            true,
				PlanModifiers:       replace,
				Validators: []validator.String{
					stringvalidator.RegexMatches(claudeTagSlackWorkspaceIDPattern, "must be a Claude Tag Slack workspace ID starting with slws_"),
				},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Target type: `slack_channel` or `slack_workspace`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the Bundle was attached.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ClaudeTagBundleTargetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ClaudeTagBundleTargetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ClaudeTagBundleTargetResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	target := client.CreateClaudeTagBundleTargetRequest{Type: "slack_workspace", SlackWorkspaceID: plan.SlackWorkspaceID.ValueString()}
	if !plan.SlackChannelID.IsNull() {
		target = client.CreateClaudeTagBundleTargetRequest{Type: "slack_channel", SlackChannelID: plan.SlackChannelID.ValueString()}
	}

	t, err := r.client.CreateClaudeTagBundleTarget(ctx, plan.BundleID.ValueString(), target)
	if err != nil {
		resp.Diagnostics.AddError("Error Attaching Claude Tag Bundle", err.Error())
		return
	}
	mapClaudeTagBundleTarget(t, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ClaudeTagBundleTargetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ClaudeTagBundleTargetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	t, err := r.client.GetClaudeTagBundleTarget(ctx, state.BundleID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Claude Tag Bundle Target", err.Error())
		return
	}
	mapClaudeTagBundleTarget(t, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every argument forces replacement.
func (r *ClaudeTagBundleTargetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unsupported Update", "Claude Tag Bundle Targets cannot be updated in place.")
}

func (r *ClaudeTagBundleTargetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ClaudeTagBundleTargetResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteClaudeTagBundleTarget(ctx, state.BundleID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Detaching Claude Tag Bundle", err.Error())
	}
}

func (r *ClaudeTagBundleTargetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	bundleID, targetID, err := splitCompositeID(req.ID, "bundle_id", "target_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import Identifier", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), targetID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("bundle_id"), bundleID)...)
}

func mapClaudeTagBundleTarget(t *client.ClaudeTagBundleTarget, m *ClaudeTagBundleTargetResourceModel) {
	m.ID = types.StringValue(t.ID)
	m.Type = types.StringValue(t.Type)
	m.CreatedAt = types.StringValue(t.CreatedAt)
	m.SlackChannelID = types.StringNull()
	m.SlackWorkspaceID = types.StringNull()
	if t.Type == "slack_channel" {
		m.SlackChannelID = types.StringValue(t.SlackChannelID)
	} else {
		m.SlackWorkspaceID = types.StringValue(t.SlackWorkspaceID)
	}
}
