// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                = &ServiceAccountResource{}
	_ resource.ResourceWithConfigure   = &ServiceAccountResource{}
	_ resource.ResourceWithImportState = &ServiceAccountResource{}
)

func NewServiceAccountResource() resource.Resource {
	return &ServiceAccountResource{}
}

type ServiceAccountResource struct {
	client *client.Client
}

type ServiceAccountResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	OrganizationRole types.String `tfsdk:"organization_role"`
	Description      types.String `tfsdk:"description"`
	CreatedAt        types.String `tfsdk:"created_at"`
	ArchivedAt       types.String `tfsdk:"archived_at"`
	Type             types.String `tfsdk:"type"`
}

func (r *ServiceAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_account"
}

func (r *ServiceAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a non-human Anthropic service account (`/v1/organizations/service_accounts`) used as a target identity for Workload Identity Federation (WIF). " +
			"Requires an `org:admin` OAuth access token (`oauth_token`) or `federation` authentication. " +
			"Deleting this resource archives the service account (`POST /v1/organizations/service_accounts/{id}/archive`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique tagged identifier of the service account (`svac_...`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug name of the service account (`^[a-z0-9-]+$`, 1-255 characters). Immutable after creation; changing `name` forces replacement.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					slugValidator(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"organization_role": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Organization-level role assigned to the service account (`developer`, `user`). Admin service accounts can only be created in the Console.",
				Validators: []validator.String{
					serviceAccountRoleValidator(),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Optional description of the service account (up to 2000 characters).",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(2000),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the service account was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"archived_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the service account was archived, or `null` if active.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`service_account`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ServiceAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ServiceAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ServiceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.CreateServiceAccountRequest{
		Name:             plan.Name.ValueString(),
		OrganizationRole: plan.OrganizationRole.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		desc := plan.Description.ValueString()
		createReq.Description = &desc
	}

	sa, err := r.client.CreateServiceAccount(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Service Account", err.Error())
		return
	}

	populateServiceAccountModel(&plan, sa)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ServiceAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ServiceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sa, err := r.client.GetServiceAccount(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Service Account",
			fmt.Sprintf("Could not read service account %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	if sa.ArchivedAt != nil && *sa.ArchivedAt != "" {
		resp.State.RemoveResource(ctx)
		return
	}

	populateServiceAccountModel(&state, sa)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ServiceAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ServiceAccountResourceModel
	var state ServiceAccountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := client.UpdateServiceAccountRequest{
		OrganizationRole: plan.OrganizationRole.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		desc := plan.Description.ValueString()
		updateReq.Description = &desc
	}

	sa, err := r.client.UpdateServiceAccount(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Service Account",
			fmt.Sprintf("Could not update service account %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	populateServiceAccountModel(&plan, sa)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ServiceAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ServiceAccountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ArchiveServiceAccount(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Archiving Service Account",
			fmt.Sprintf("Could not archive service account %s: %s", state.ID.ValueString(), err.Error()),
		)
	}
}

func (r *ServiceAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError(
			"Invalid Import Identifier",
			fmt.Sprintf("Expected service account ID matching %s, got %q", resourceIDPattern.String(), req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func populateServiceAccountModel(m *ServiceAccountResourceModel, sa *client.ServiceAccount) {
	m.ID = types.StringValue(sa.ID)
	m.Name = types.StringValue(sa.Name)
	m.OrganizationRole = types.StringValue(sa.OrganizationRole)
	if sa.Description != nil {
		m.Description = types.StringValue(*sa.Description)
	} else {
		m.Description = types.StringNull()
	}
	m.CreatedAt = types.StringValue(sa.CreatedAt)
	if sa.ArchivedAt != nil && *sa.ArchivedAt != "" {
		m.ArchivedAt = types.StringValue(*sa.ArchivedAt)
	} else {
		m.ArchivedAt = types.StringNull()
	}
	m.Type = types.StringValue(sa.Type)
}
