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
	_ resource.Resource                = &APIKeyResource{}
	_ resource.ResourceWithConfigure   = &APIKeyResource{}
	_ resource.ResourceWithImportState = &APIKeyResource{}
)

func NewAPIKeyResource() resource.Resource {
	return &APIKeyResource{}
}

// APIKeyResource manages an existing Console API key's name and status via /v1/organizations/api_keys/{api_key_id}.
// Because the Admin API does not create raw API key secrets, Create adopts the key by id and updates its name/status;
// Delete deactivates the key by setting status = "inactive".
type APIKeyResource struct {
	client *client.Client
}

type APIKeyResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Status         types.String `tfsdk:"status"`
	PartialKeyHint types.String `tfsdk:"partial_key_hint"`
	WorkspaceID    types.String `tfsdk:"workspace_id"`
	CreatedAt      types.String `tfsdk:"created_at"`
	CreatedByID    types.String `tfsdk:"created_by_id"`
	Type           types.String `tfsdk:"type"`
}

func (r *APIKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *APIKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the lifecycle (`name` and `status`) of an existing Claude Console API key (`/v1/organizations/api_keys/{api_key_id}`). " +
			"Because the Anthropic Admin API never creates or returns plaintext key secrets, `Create` adopts an existing API key by `id` and reconciles its `name` and `status`. " +
			"Destroying this resource deactivates the key (`status = \"inactive\"`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique identifier of the API key (`apikey_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Display name of the API key.",
				Validators:          []validator.String{nonEmptyStringValidator()},
			},
			"status": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Operational status of the API key (`active`, `inactive`, `archived`).",
				Validators: []validator.String{
					stringvalidator.OneOf("active", "inactive", "archived"),
				},
			},
			"partial_key_hint": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Partially redacted hint of the API key (e.g. `sk-ant-api03-R2D...igAA`).",
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

func (r *APIKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *APIKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan APIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.UpdateAPIKey(ctx, plan.ID.ValueString(), client.UpdateAPIKeyRequest{
		Name:   plan.Name.ValueString(),
		Status: plan.Status.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Managing API Key", fmt.Sprintf("Could not update API key %s: %s", plan.ID.ValueString(), err))
		return
	}

	mapAPIKeyToModel(key, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *APIKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state APIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.GetAPIKey(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading API Key", fmt.Sprintf("Could not read API key %s: %s", state.ID.ValueString(), err))
		return
	}

	mapAPIKeyToModel(key, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *APIKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan APIKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.UpdateAPIKey(ctx, plan.ID.ValueString(), client.UpdateAPIKeyRequest{
		Name:   plan.Name.ValueString(),
		Status: plan.Status.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Updating API Key", fmt.Sprintf("Could not update API key %s: %s", plan.ID.ValueString(), err))
		return
	}

	mapAPIKeyToModel(key, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *APIKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state APIKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.Status.ValueString() == "inactive" || state.Status.ValueString() == "archived" {
		return
	}

	_, err := r.client.UpdateAPIKey(ctx, state.ID.ValueString(), client.UpdateAPIKeyRequest{
		Status: "inactive",
	})
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Deactivating API Key", fmt.Sprintf("Could not deactivate API key %s: %s", state.ID.ValueString(), err))
	}
}

func (r *APIKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid API Key Import ID", fmt.Sprintf("Import ID %q must contain only alphanumeric characters, underscores, or hyphens.", req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapAPIKeyToModel(key *client.APIKey, m *APIKeyResourceModel) {
	m.ID = types.StringValue(key.ID)
	m.Name = types.StringValue(key.Name)
	m.Status = types.StringValue(key.Status)
	m.PartialKeyHint = types.StringValue(key.PartialKeyHint)
	if key.WorkspaceID != nil && *key.WorkspaceID != "" {
		m.WorkspaceID = types.StringValue(*key.WorkspaceID)
	} else {
		m.WorkspaceID = types.StringNull()
	}
	m.CreatedAt = types.StringValue(key.CreatedAt)
	if key.CreatedBy != nil && key.CreatedBy.ID != "" {
		m.CreatedByID = types.StringValue(key.CreatedBy.ID)
	} else {
		m.CreatedByID = types.StringNull()
	}
	m.Type = types.StringValue(key.Type)
}
