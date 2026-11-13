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
	_ resource.Resource                = &PluginMarketplaceResource{}
	_ resource.ResourceWithConfigure   = &PluginMarketplaceResource{}
	_ resource.ResourceWithImportState = &PluginMarketplaceResource{}
)

var pluginInstallationPreferences = []string{"required", "auto_install", "available", "not_available"}

func NewPluginMarketplaceResource() resource.Resource {
	return &PluginMarketplaceResource{}
}

type PluginMarketplaceResource struct {
	client *client.Client
}

type PluginMarketplaceResourceModel struct {
	ID            types.String `tfsdk:"id"`
	MarketplaceID types.String `tfsdk:"marketplace_id"`
	PluginMarketplaceModel
}

func (r *PluginMarketplaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin_marketplace"
}

func (r *PluginMarketplaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stable := func(desc string) schema.Attribute {
		return schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: desc,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	changing := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Manages the default installation setting of an existing organization plugin marketplace (`POST /v1/organizations/plugin_marketplaces/{id}`)." + pluginsEnterpriseNote + "\n\n" +
			"Marketplaces are created and deleted in claude.ai, so this resource adopts an existing one. " +
			"Destroy only removes it from Terraform state and leaves the last default in place.",
		Attributes: map[string]schema.Attribute{
			"id": stable("Same as `marketplace_id`."),
			"marketplace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of an existing organization plugin marketplace (`marketplace_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"default_installation_preference": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Installation setting every plugin in the marketplace gets unless it has its own: `required`, `auto_install`, `available` or `not_available`.",
				Validators:          []validator.String{stringvalidator.OneOf(pluginInstallationPreferences...)},
			},
			"name":               stable("Name of the marketplace, fixed for its lifetime."),
			"source":             stable("Where its plugins come from: `manual` (uploaded), or `github`, `gitlab`, `public_git` (synchronized from a Git repository)."),
			"owner_type":         stable("Owner of the marketplace, always `organization` for one managed here."),
			"owner_user_id":      stable("Always null for an organization marketplace."),
			"created_at":         stable("RFC 3339 timestamp when the marketplace was created."),
			"sync_status":        changing("Outcome of the most recent repository synchronization. Null when never synchronized."),
			"last_sync_ended_at": changing("RFC 3339 timestamp when the most recent synchronization attempt ended. Null when not synchronized."),
			"last_sync_read_sha": changing("Commit the last synchronization read. Null when not synchronized."),
		},
	}
}

func (r *PluginMarketplaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = c
}

func (r *PluginMarketplaceResource) write(ctx context.Context, plan *PluginMarketplaceResourceModel) error {
	m, err := r.client.UpdatePluginMarketplace(ctx, plan.MarketplaceID.ValueString(), plan.DefaultInstallationPreference.ValueString())
	if err != nil {
		return err
	}
	plan.ID = types.StringValue(m.ID)
	plan.PluginMarketplaceModel = mapPluginMarketplace(m)
	return nil
}

func (r *PluginMarketplaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PluginMarketplaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Updating Plugin Marketplace", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PluginMarketplaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PluginMarketplaceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m, err := r.client.GetPluginMarketplace(ctx, state.MarketplaceID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Plugin Marketplace", err.Error())
		return
	}
	state.ID = types.StringValue(m.ID)
	state.MarketplaceID = types.StringValue(m.ID)
	state.PluginMarketplaceModel = mapPluginMarketplace(m)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PluginMarketplaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PluginMarketplaceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Updating Plugin Marketplace", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PluginMarketplaceResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Plugin Marketplace Left In Place",
		"The API cannot delete a plugin marketplace or remove its default installation setting. It was removed from Terraform state only.",
	)
}

func (r *PluginMarketplaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid Import Identifier", fmt.Sprintf("Expected plugin marketplace ID matching %s, got %q", resourceIDPattern.String(), req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("marketplace_id"), req, resp)
}
