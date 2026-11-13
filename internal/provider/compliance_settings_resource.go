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

// complianceSettingsID is the fixed ID of the per-organization compliance settings singleton.
const complianceSettingsID = "compliance_settings"

var (
	_ resource.Resource                = &ComplianceSettingsResource{}
	_ resource.ResourceWithConfigure   = &ComplianceSettingsResource{}
	_ resource.ResourceWithImportState = &ComplianceSettingsResource{}
)

func NewComplianceSettingsResource() resource.Resource {
	return &ComplianceSettingsResource{}
}

type ComplianceSettingsResource struct {
	client *client.Client
}

type ComplianceSettingsModel struct {
	ID    types.String `tfsdk:"id"`
	State types.String `tfsdk:"state"`
}

func (r *ComplianceSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compliance_settings"
}

func (r *ComplianceSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages the organization's Compliance Settings (`/v1/organizations/compliance_settings`), which turn the Compliance API on or off. " +
			"Declare it at most once per organization. " +
			"Destroy only removes it from Terraform state and leaves the setting as it is. " +
			"Import with the fixed ID `" + complianceSettingsID + "`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Always `" + complianceSettingsID + "`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "Whether the Compliance API is enabled: `enabled` or `disabled`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf("enabled", "disabled")},
			},
		},
	}
}

func (r *ComplianceSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ComplianceSettingsResource) apply(ctx context.Context, model *ComplianceSettingsModel) error {
	s, err := r.client.UpdateComplianceSettings(ctx, model.State.ValueString())
	if err != nil {
		return err
	}
	model.ID = types.StringValue(complianceSettingsID)
	model.State = types.StringValue(s.State.Type)
	return nil
}

func (r *ComplianceSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ComplianceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Updating Compliance Settings", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ComplianceSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ComplianceSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.client.GetComplianceSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Compliance Settings", err.Error())
		return
	}
	state.ID = types.StringValue(complianceSettingsID)
	state.State = types.StringValue(s.State.Type)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ComplianceSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ComplianceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.apply(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error Updating Compliance Settings", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete only forgets the singleton. The API has no reset, and guessing a default could switch off compliance logging.
func (r *ComplianceSettingsResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning(
		"Compliance Settings Left Unchanged",
		"anthropic_compliance_settings was removed from Terraform state. The organization's Compliance API setting was not changed.",
	)
}

func (r *ComplianceSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != complianceSettingsID {
		resp.Diagnostics.AddError("Invalid Import Identifier", fmt.Sprintf("Expected %q, got %q", complianceSettingsID, req.ID))
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
