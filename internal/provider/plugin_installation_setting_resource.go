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
	_ resource.Resource                = &PluginInstallationSettingResource{}
	_ resource.ResourceWithConfigure   = &PluginInstallationSettingResource{}
	_ resource.ResourceWithImportState = &PluginInstallationSettingResource{}
)

func NewPluginInstallationSettingResource() resource.Resource {
	return &PluginInstallationSettingResource{}
}

type PluginInstallationSettingResource struct {
	client *client.Client
}

type PluginInstallationSettingResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	PluginID               types.String `tfsdk:"plugin_id"`
	Target                 types.String `tfsdk:"target"`
	InstallationPreference types.String `tfsdk:"installation_preference"`
	TargetType             types.String `tfsdk:"target_type"`
	CreatedAt              types.String `tfsdk:"created_at"`
	UpdatedAt              types.String `tfsdk:"updated_at"`
}

func (r *PluginInstallationSettingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_plugin_installation_setting"
}

func (r *PluginInstallationSettingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Manages an organization-owned plugin's installation setting for the whole organization or for one RBAC Group " +
			"(`/v1/organizations/plugins/{plugin_id}/installation_settings/{target}`)." + pluginsEnterpriseNote + "\n\n" +
			"Destroying an `organization` setting returns the plugin to its marketplace default. Destroying a group setting makes its members fall back to the organization-wide setting.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite identifier in the format `<plugin_id>/<target>`.",
				PlanModifiers:       keep,
			},
			"plugin_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "ID of an organization-owned plugin (`plugin_...`).",
				Validators:          []validator.String{idValidator()},
				PlanModifiers:       replace,
			},
			"target": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`organization` for the organization-wide setting, or an RBAC Group ID for that group's setting.",
				Validators:          []validator.String{idValidator()},
				PlanModifiers:       replace,
			},
			"installation_preference": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "`required`, `auto_install`, `available` or `not_available`.",
				Validators:          []validator.String{stringvalidator.OneOf(pluginInstallationPreferences...)},
			},
			"target_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`organization` or `rbac_group`, as returned by the API.",
				PlanModifiers:       keep,
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the target was first given a setting for this plugin.",
				PlanModifiers:       keep,
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the setting last changed.",
			},
		},
	}
}

func (r *PluginInstallationSettingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func mapPluginInstallationSetting(s *client.PluginInstallationSetting, target string, m *PluginInstallationSettingResourceModel) {
	m.ID = types.StringValue(s.PluginID + "/" + target)
	m.PluginID = types.StringValue(s.PluginID)
	m.Target = types.StringValue(target)
	m.InstallationPreference = types.StringValue(s.InstallationPreference)
	m.TargetType = types.StringValue(s.Target.Type)
	m.CreatedAt = types.StringValue(s.CreatedAt)
	m.UpdatedAt = types.StringValue(s.UpdatedAt)
}

func (r *PluginInstallationSettingResource) set(ctx context.Context, plan *PluginInstallationSettingResourceModel) error {
	s, err := r.client.SetPluginInstallationSetting(ctx, plan.PluginID.ValueString(), plan.Target.ValueString(), plan.InstallationPreference.ValueString())
	if err != nil {
		return err
	}
	mapPluginInstallationSetting(s, plan.Target.ValueString(), plan)
	return nil
}

func (r *PluginInstallationSettingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PluginInstallationSettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.set(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Setting Plugin Installation Setting", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PluginInstallationSettingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PluginInstallationSettingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.GetPluginInstallationSetting(ctx, state.PluginID.ValueString(), state.Target.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading Plugin Installation Setting", err.Error())
		return
	}
	mapPluginInstallationSetting(s, state.Target.ValueString(), &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *PluginInstallationSettingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PluginInstallationSettingResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.set(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Setting Plugin Installation Setting", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *PluginInstallationSettingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PluginInstallationSettingResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.RemovePluginInstallationSetting(ctx, state.PluginID.ValueString(), state.Target.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Removing Plugin Installation Setting", err.Error())
	}
}

func (r *PluginInstallationSettingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	pluginID, target, err := splitCompositeID(req.ID, "plugin_id", "target")
	if err != nil {
		resp.Diagnostics.AddError("Invalid Import Identifier", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("plugin_id"), pluginID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("target"), target)...)
}
