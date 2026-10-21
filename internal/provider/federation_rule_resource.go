// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                   = &FederationRuleResource{}
	_ resource.ResourceWithConfigure      = &FederationRuleResource{}
	_ resource.ResourceWithImportState    = &FederationRuleResource{}
	_ resource.ResourceWithValidateConfig = &FederationRuleResource{}
)

func NewFederationRuleResource() resource.Resource {
	return &FederationRuleResource{}
}

type FederationRuleResource struct {
	client *client.Client
}

type FederationRuleMatchModel struct {
	Audience      types.String `tfsdk:"audience"`
	SubjectPrefix types.String `tfsdk:"subject_prefix"`
	Claims        types.Map    `tfsdk:"claims"`
	Condition     types.String `tfsdk:"condition"`
}

type FederationRuleResourceModel struct {
	ID                     types.String              `tfsdk:"id"`
	Name                   types.String              `tfsdk:"name"`
	Description            types.String              `tfsdk:"description"`
	IssuerID               types.String              `tfsdk:"issuer_id"`
	ServiceAccountID       types.String              `tfsdk:"service_account_id"`
	OAuthScope             types.String              `tfsdk:"oauth_scope"`
	TokenLifetimeSeconds   types.Int64               `tfsdk:"token_lifetime_seconds"`
	AppliesToAllWorkspaces types.Bool                `tfsdk:"applies_to_all_workspaces"`
	WorkspaceIDs           types.Set                 `tfsdk:"workspace_ids"`
	Match                  *FederationRuleMatchModel `tfsdk:"match"`
	CreatedAt              types.String              `tfsdk:"created_at"`
	UpdatedAt              types.String              `tfsdk:"updated_at"`
	ArchivedAt             types.String              `tfsdk:"archived_at"`
	Type                   types.String              `tfsdk:"type"`
}

func (r *FederationRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_rule"
}

func (r *FederationRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Workload Identity Federation (WIF) rule (`/v1/organizations/federation_rules`) that maps external OIDC JWT claims " +
			"from a federation issuer (`fdis_...`) to a target Anthropic service account (`svac_...`). " +
			"Requires an `org:admin` OAuth access token (`oauth_token`) or `federation` authentication. " +
			"Deleting this resource archives the rule (`POST /v1/organizations/federation_rules/{id}/archive`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique tagged identifier of the federation rule (`fdrl_...`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug name of the federation rule (`^[a-z0-9-]+$`, 1-255 characters).",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					slugValidator(),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Free-text description of the federation rule.",
			},
			"issuer_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of the associated federation issuer (`fdis_...`). Immutable after creation.",
				Validators: []validator.String{
					idValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"service_account_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Target service account ID (`svac_...`) that minted tokens will act as.",
				Validators: []validator.String{
					idValidator(),
				},
			},
			"oauth_scope": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "OAuth scope granted on minted tokens (`workspace:developer` or `workspace:inference`).",
				Validators: []validator.String{
					federationOAuthScopeValidator(),
				},
			},
			"token_lifetime_seconds": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Lifetime of minted OAuth tokens in seconds (`60` to `86400`, defaults to `3600`).",
				Validators: []validator.Int64{
					int64validator.Between(60, 86400),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"applies_to_all_workspaces": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Whether the federation rule applies to all current and future workspaces in the organization.",
			},
			"workspace_ids": schema.SetAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Set of workspace IDs (`wrkspc_...`) where this federation rule is enabled. Required unless `applies_to_all_workspaces` is `true`.",
			},
			"match": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "Claim matching conditions that incoming verified JWTs must satisfy.",
				Attributes: map[string]schema.Attribute{
					"audience": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Exact match against the JWT `aud` claim.",
					},
					"subject_prefix": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Matches the JWT `sub` claim (exact match unless ending in `:*` or `/*` for prefix wildcard, e.g. `repo:example-org/example-repo:*`).",
						Validators: []validator.String{
							subjectPrefixValidator(),
						},
					},
					"claims": schema.MapAttribute{
						ElementType:         types.StringType,
						Optional:            true,
						MarkdownDescription: "Exact-match `{claim: value}` pairs against top-level string claims.",
					},
					"condition": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Common Expression Language (CEL) expression evaluated over `claims` (up to 4096 characters).",
						Validators: []validator.String{
							stringvalidator.LengthAtMost(4096),
						},
					},
				},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the federation rule was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the federation rule was last updated.",
			},
			"archived_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the federation rule was archived, or `null` if active.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`federation_rule`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *FederationRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *FederationRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config FederationRuleResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !config.AppliesToAllWorkspaces.IsUnknown() && !config.AppliesToAllWorkspaces.ValueBool() &&
		!config.WorkspaceIDs.IsUnknown() && len(config.WorkspaceIDs.Elements()) == 0 {
		resp.Diagnostics.AddAttributeError(
			path.Root("workspace_ids"),
			"Missing Federation Rule Workspace",
			"Set `workspace_ids` or `applies_to_all_workspaces = true`. The API rejects a rule enabled for no workspace.",
		)
	}

	if config.Match == nil {
		return
	}

	if config.Match.SubjectPrefix.IsUnknown() || config.Match.Condition.IsUnknown() || config.Match.Claims.IsUnknown() {
		return
	}

	hasSubjectPrefix := !config.Match.SubjectPrefix.IsNull() && config.Match.SubjectPrefix.ValueString() != ""
	hasCondition := !config.Match.Condition.IsNull() && config.Match.Condition.ValueString() != ""
	hasClaims := len(config.Match.Claims.Elements()) > 0
	if !hasSubjectPrefix && !hasCondition && !hasClaims {
		resp.Diagnostics.AddAttributeError(
			path.Root("match"),
			"Missing Federation Rule Claim Constraint",
			"At least one of `match.subject_prefix`, `match.claims` or `match.condition` must be configured so that the federation rule does not match arbitrary JWTs from the issuer.",
		)
	}
}

func (r *FederationRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederationRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	match, diags := expandFederationRuleMatch(ctx, plan.Match)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	wsIDs, diags := workspaceIDList(ctx, plan.WorkspaceIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.CreateFederationRuleRequest{
		Name:                   plan.Name.ValueString(),
		Description:            optionalString(plan.Description),
		IssuerID:               plan.IssuerID.ValueString(),
		Target:                 client.ServiceAccountTarget(plan.ServiceAccountID.ValueString()),
		OAuthScope:             plan.OAuthScope.ValueString(),
		AppliesToAllWorkspaces: plan.AppliesToAllWorkspaces.ValueBool(),
		Match:                  match,
	}
	if len(wsIDs) > 0 {
		createReq.WorkspaceID = wsIDs[0]
	}
	if !plan.TokenLifetimeSeconds.IsNull() && !plan.TokenLifetimeSeconds.IsUnknown() {
		v := plan.TokenLifetimeSeconds.ValueInt64()
		createReq.TokenLifetimeSeconds = &v
	}

	fr, err := r.client.CreateFederationRule(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Federation Rule", err.Error())
		return
	}

	resp.Diagnostics.Append(populateFederationRuleModel(ctx, &plan, fr)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Save the rule before binding more workspaces so a failure below leaves it tracked (tainted), not orphaned.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() || len(wsIDs) <= 1 {
		return
	}

	for _, ws := range wsIDs[1:] {
		if err := r.client.AddFederationRuleWorkspace(ctx, fr.ID, ws); err != nil {
			resp.Diagnostics.AddError("Error Enabling Federation Rule Workspace",
				fmt.Sprintf("Could not enable federation rule %s for workspace %s: %s", fr.ID, ws, err.Error()))
			return
		}
	}
	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

func (r *FederationRuleResource) refresh(ctx context.Context, m *FederationRuleResourceModel, st *tfsdk.State, diags *diag.Diagnostics) {
	fr, err := r.client.GetFederationRule(ctx, m.ID.ValueString())
	if err != nil {
		diags.AddError("Error Reading Federation Rule",
			fmt.Sprintf("Could not read federation rule %s: %s", m.ID.ValueString(), err.Error()))
		return
	}
	diags.Append(populateFederationRuleModel(ctx, m, fr)...)
	if diags.HasError() {
		return
	}
	diags.Append(st.Set(ctx, m)...)
}

// workspaceIDList returns the set's elements sorted, so the workspace bound at create time is deterministic.
func workspaceIDList(ctx context.Context, s types.Set) ([]string, diag.Diagnostics) {
	var ids []string
	if s.IsNull() || s.IsUnknown() {
		return ids, nil
	}
	diags := s.ElementsAs(ctx, &ids, false)
	sort.Strings(ids)
	return ids, diags
}

func optionalString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func (r *FederationRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederationRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fr, err := r.client.GetFederationRule(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Federation Rule",
			fmt.Sprintf("Could not read federation rule %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	if fr.ArchivedAt != nil && *fr.ArchivedAt != "" {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(populateFederationRuleModel(ctx, &state, fr)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *FederationRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FederationRuleResourceModel
	var state FederationRuleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	match, diags := expandFederationRuleMatch(ctx, plan.Match)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	newWS, diags := workspaceIDList(ctx, plan.WorkspaceIDs)
	resp.Diagnostics.Append(diags...)
	oldWS, diags := workspaceIDList(ctx, state.WorkspaceIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ruleID := state.ID.ValueString()
	plan.ID = state.ID

	// Add before the update and remove after it, so the rule never passes through zero enabled workspaces.
	for _, ws := range setDifference(newWS, oldWS) {
		if err := r.client.AddFederationRuleWorkspace(ctx, ruleID, ws); err != nil {
			resp.Diagnostics.AddError("Error Enabling Federation Rule Workspace",
				fmt.Sprintf("Could not enable federation rule %s for workspace %s: %s", ruleID, ws, err.Error()))
			return
		}
	}

	appliesAll := plan.AppliesToAllWorkspaces.ValueBool()
	target := client.ServiceAccountTarget(plan.ServiceAccountID.ValueString())
	updateReq := client.UpdateFederationRuleRequest{
		Name:                   plan.Name.ValueString(),
		Description:            optionalString(plan.Description),
		Target:                 &target,
		OAuthScope:             plan.OAuthScope.ValueString(),
		AppliesToAllWorkspaces: &appliesAll,
		Match:                  &match,
	}
	if !plan.TokenLifetimeSeconds.IsNull() && !plan.TokenLifetimeSeconds.IsUnknown() {
		v := plan.TokenLifetimeSeconds.ValueInt64()
		updateReq.TokenLifetimeSeconds = &v
	}

	if _, err := r.client.UpdateFederationRule(ctx, ruleID, updateReq); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Federation Rule",
			fmt.Sprintf("Could not update federation rule %s: %s", ruleID, err.Error()),
		)
		return
	}

	for _, ws := range setDifference(oldWS, newWS) {
		if err := r.client.RemoveFederationRuleWorkspace(ctx, ruleID, ws); err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Error Disabling Federation Rule Workspace",
				fmt.Sprintf("Could not disable federation rule %s for workspace %s: %s", ruleID, ws, err.Error()))
			return
		}
	}

	r.refresh(ctx, &plan, &resp.State, &resp.Diagnostics)
}

// setDifference returns the elements of a that are not in b.
func setDifference(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, v := range b {
		in[v] = true
	}
	var out []string
	for _, v := range a {
		if !in[v] {
			out = append(out, v)
		}
	}
	return out
}

func (r *FederationRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FederationRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ArchiveFederationRule(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Archiving Federation Rule",
			fmt.Sprintf("Could not archive federation rule %s: %s", state.ID.ValueString(), err.Error()),
		)
	}
}

func (r *FederationRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError(
			"Invalid Import Identifier",
			fmt.Sprintf("Expected federation rule ID matching %s, got %q", resourceIDPattern.String(), req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func expandFederationRuleMatch(ctx context.Context, m *FederationRuleMatchModel) (client.FederationRuleMatch, diag.Diagnostics) {
	var diags diag.Diagnostics
	var out client.FederationRuleMatch
	if m == nil {
		return out, diags
	}
	if !m.Audience.IsNull() && !m.Audience.IsUnknown() {
		v := m.Audience.ValueString()
		out.Audience = &v
	}
	if !m.SubjectPrefix.IsNull() && !m.SubjectPrefix.IsUnknown() {
		v := m.SubjectPrefix.ValueString()
		if err := ValidateSubjectPrefix(v); err != nil {
			diags.AddAttributeError(
				path.Root("match").AtName("subject_prefix"),
				"Invalid Federation Rule Subject Prefix",
				err.Error(),
			)
		}
		out.SubjectPrefix = &v
	}
	if !m.Claims.IsNull() && !m.Claims.IsUnknown() {
		claims := make(map[string]string)
		diags.Append(m.Claims.ElementsAs(ctx, &claims, false)...)
		out.Claims = claims
	}
	if !m.Condition.IsNull() && !m.Condition.IsUnknown() {
		v := m.Condition.ValueString()
		out.Condition = &v
	}
	if (out.SubjectPrefix == nil || *out.SubjectPrefix == "") && (out.Condition == nil || *out.Condition == "") && len(out.Claims) == 0 {
		diags.AddAttributeError(
			path.Root("match"),
			"Missing Federation Rule Claim Constraint",
			"At least one of `match.subject_prefix`, `match.claims` or `match.condition` must be configured.",
		)
	}
	return out, diags
}

func populateFederationRuleModel(ctx context.Context, m *FederationRuleResourceModel, fr *client.FederationRule) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(fr.ID)
	m.Name = types.StringValue(fr.Name)
	// The API stores a cleared description as "", so keep null when config never set one.
	if fr.Description == "" && m.Description.IsNull() {
		m.Description = types.StringNull()
	} else {
		m.Description = types.StringValue(fr.Description)
	}
	m.IssuerID = types.StringValue(fr.IssuerID)
	m.ServiceAccountID = types.StringValue(fr.Target.ServiceAccountID)
	m.OAuthScope = types.StringValue(fr.OAuthScope)
	if fr.TokenLifetimeSeconds > 0 {
		m.TokenLifetimeSeconds = types.Int64Value(fr.TokenLifetimeSeconds)
	} else {
		m.TokenLifetimeSeconds = types.Int64Value(3600)
	}
	m.AppliesToAllWorkspaces = types.BoolValue(fr.AppliesToAllWorkspaces)

	if len(fr.WorkspaceIDs) == 0 && m.WorkspaceIDs.IsNull() {
		m.WorkspaceIDs = types.SetNull(types.StringType)
	} else {
		wsSet, d := types.SetValueFrom(ctx, types.StringType, fr.WorkspaceIDs)
		diags.Append(d...)
		m.WorkspaceIDs = wsSet
	}

	matchModel := &FederationRuleMatchModel{}
	if fr.Match.Audience != nil {
		matchModel.Audience = types.StringValue(*fr.Match.Audience)
	} else {
		matchModel.Audience = types.StringNull()
	}
	if fr.Match.SubjectPrefix != nil {
		matchModel.SubjectPrefix = types.StringValue(*fr.Match.SubjectPrefix)
	} else {
		matchModel.SubjectPrefix = types.StringNull()
	}
	if len(fr.Match.Claims) > 0 {
		claimsMap, cd := types.MapValueFrom(ctx, types.StringType, fr.Match.Claims)
		diags.Append(cd...)
		matchModel.Claims = claimsMap
	} else {
		matchModel.Claims = types.MapNull(types.StringType)
	}
	if fr.Match.Condition != nil {
		matchModel.Condition = types.StringValue(*fr.Match.Condition)
	} else {
		matchModel.Condition = types.StringNull()
	}
	m.Match = matchModel

	m.CreatedAt = types.StringValue(fr.CreatedAt)
	m.UpdatedAt = types.StringValue(fr.UpdatedAt)
	if fr.ArchivedAt != nil && *fr.ArchivedAt != "" {
		m.ArchivedAt = types.StringValue(*fr.ArchivedAt)
	} else {
		m.ArchivedAt = types.StringNull()
	}
	m.Type = types.StringValue(fr.Type)
	return diags
}
