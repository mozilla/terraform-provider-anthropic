// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                = &InviteResource{}
	_ resource.ResourceWithConfigure   = &InviteResource{}
	_ resource.ResourceWithImportState = &InviteResource{}
)

func NewInviteResource() resource.Resource {
	return &InviteResource{}
}

type InviteResource struct {
	client *client.Client
}

type InviteResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Email     types.String `tfsdk:"email"`
	Role      types.String `tfsdk:"role"`
	Status    types.String `tfsdk:"status"`
	InvitedAt types.String `tfsdk:"invited_at"`
	ExpiresAt types.String `tfsdk:"expires_at"`
	Type      types.String `tfsdk:"type"`
}

func (r *InviteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_invite"
}

func (r *InviteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an organization invitation (`/v1/organizations/invites`). " +
			"Invites are immutable; changing `email` or `role` replaces the invitation. " +
			"**Note:** In Claude Enterprise organizations with JIT (Just-In-Time) SSO or SCIM provisioning enabled, `/v1/organizations/invites` returns HTTP 400 and cannot be used to provision users.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique identifier of the invite (`invite_...`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"email": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Email address of the invited user.",
				Validators:          []validator.String{nonEmptyStringValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Organization-level role to grant upon acceptance (`user`, `developer`, `billing`, `admin`, `claude_code_user`, `membership_admin`, `managed`).",
				Validators:          []validator.String{orgRoleValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Current status of the invite (`pending`, `accepted`, `expired`, `deleted`).",
			},
			"invited_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the invitation was sent.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"expires_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the invitation expires.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`invite`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *InviteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *InviteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan InviteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inv, err := r.client.CreateInvite(ctx, client.CreateInviteRequest{
		Email: plan.Email.ValueString(),
		Role:  plan.Role.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Organization Invite", fmt.Sprintf("Could not create invite for %q: %s", plan.Email.ValueString(), err))
		return
	}

	mapInviteToModel(inv, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *InviteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state InviteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inv, err := r.client.GetInvite(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Organization Invite", fmt.Sprintf("Could not read invite %s: %s", state.ID.ValueString(), err))
		return
	}

	if inv.Status == "deleted" {
		resp.State.RemoveResource(ctx)
		return
	}

	mapInviteToModel(inv, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *InviteResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Invite Update Not Supported", "Invites are immutable; changing email or role requires replacement.")
}

func (r *InviteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state InviteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteInvite(ctx, state.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Deleting Organization Invite", fmt.Sprintf("Could not delete invite %s: %s", state.ID.ValueString(), err))
	}
}

func (r *InviteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid Invite Import ID", fmt.Sprintf("Import ID %q must contain only alphanumeric characters, underscores, or hyphens.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapInviteToModel(inv *client.Invite, m *InviteResourceModel) {
	m.ID = types.StringValue(inv.ID)
	m.Email = types.StringValue(inv.Email)
	m.Role = types.StringValue(inv.Role)
	m.Status = types.StringValue(inv.Status)
	m.InvitedAt = types.StringValue(inv.InvitedAt)
	m.ExpiresAt = types.StringValue(inv.ExpiresAt)
	m.Type = types.StringValue(inv.Type)
}
