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
	_ resource.Resource                = &ExternalKeyResource{}
	_ resource.ResourceWithConfigure   = &ExternalKeyResource{}
	_ resource.ResourceWithImportState = &ExternalKeyResource{}
)

func NewExternalKeyResource() resource.Resource {
	return &ExternalKeyResource{}
}

type ExternalKeyResource struct {
	client *client.Client
}

type ExternalKeyProviderConfigModel struct {
	Type     types.String `tfsdk:"type"`
	KMSARN   types.String `tfsdk:"kms_arn"`
	Region   types.String `tfsdk:"region"`
	KeyName  types.String `tfsdk:"key_name"`
	TenantID types.String `tfsdk:"tenant_id"`
	VaultURI types.String `tfsdk:"vault_uri"`
	ClientID types.String `tfsdk:"client_id"`
}

type ExternalKeyModel struct {
	ID             types.String                    `tfsdk:"id"`
	DisplayName    types.String                    `tfsdk:"display_name"`
	Geo            types.String                    `tfsdk:"geo"`
	UserProfileID  types.String                    `tfsdk:"user_profile_id"`
	ProviderConfig *ExternalKeyProviderConfigModel `tfsdk:"provider_config"`
	Attachment     types.String                    `tfsdk:"attachment"`
	CreatedAt      types.String                    `tfsdk:"created_at"`
	UpdatedAt      types.String                    `tfsdk:"updated_at"`
}

func (r *ExternalKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_key"
}

func (r *ExternalKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a customer-managed encryption key (CMEK) config (`/v1/organizations/external_keys`). " +
			"The config holds a reference to a key in AWS KMS, Google Cloud KMS or Azure Key Vault, not key material. " +
			"`geo` and `provider_config` cannot be changed, and the config cannot be deleted, while any workspace references it. " +
			"Validate the key in the Console or through the API. " +
			"Import with the external key ID (`ekey_...`). ARN-form IDs, used on Claude Platform on AWS, are not supported yet.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Identifier of the external key config (`ekey_...`).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "Human-friendly display name. A name cannot be cleared once set, so removing it from config keeps the current name.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{nonEmptyStringValidator()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"geo": schema.StringAttribute{
				MarkdownDescription: "Data residency geo the key is used in. Only `us` is supported. Defaults to the API's choice.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf("us")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"user_profile_id": schema.StringAttribute{
				MarkdownDescription: "User profile to bind the key to. A bound key can only be attached to workspaces with the same user profile. " +
					"Omit only for the organization's own key. Changing it forces a new config.",
				Optional:      true,
				Validators:    []validator.String{idValidator()},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"provider_config": schema.SingleNestedAttribute{
				MarkdownDescription: "KMS provider identity and auth coordinates.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						MarkdownDescription: "KMS provider: `aws`, `gcp` or `azure`.",
						Required:            true,
						Validators:          []validator.String{stringvalidator.OneOf("aws", "gcp", "azure")},
					},
					"kms_arn": schema.StringAttribute{
						MarkdownDescription: "AWS only. Full ARN of the AWS KMS key.",
						Optional:            true,
					},
					"region": schema.StringAttribute{
						MarkdownDescription: "AWS only. AWS region, derived from `kms_arn` when omitted.",
						Optional:            true,
						Computed:            true,
					},
					"key_name": schema.StringAttribute{
						MarkdownDescription: "GCP: full resource name of the Cloud KMS key. Azure: name of the key within the vault.",
						Optional:            true,
					},
					"tenant_id": schema.StringAttribute{
						MarkdownDescription: "Azure only. Azure AD tenant ID.",
						Optional:            true,
					},
					"vault_uri": schema.StringAttribute{
						MarkdownDescription: "Azure only. Key Vault data-plane URI.",
						Optional:            true,
					},
					"client_id": schema.StringAttribute{
						MarkdownDescription: "Azure only. Application (client) ID of a single-tenant app registration. Omit to use Anthropic's multitenant app.",
						Optional:            true,
						Computed:            true,
						PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					},
				},
			},
			"attachment": schema.StringAttribute{
				MarkdownDescription: "`attached` if any live or archived workspace uses this config, otherwise `unattached`.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the config was created.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "RFC 3339 timestamp when the config was last updated.",
				Computed:            true,
			},
		},
	}
}

func (r *ExternalKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func emptyStringAsNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func (m *ExternalKeyProviderConfigModel) toAPI() client.ExternalKeyProviderConfig {
	return client.ExternalKeyProviderConfig{
		Type:     m.Type.ValueString(),
		KMSARN:   m.KMSARN.ValueString(),
		Region:   optionalString(m.Region),
		KeyName:  m.KeyName.ValueString(),
		TenantID: m.TenantID.ValueString(),
		VaultURI: m.VaultURI.ValueString(),
		ClientID: optionalString(m.ClientID),
	}
}

func mapExternalKeyToModel(k *client.ExternalKey, m *ExternalKeyModel) {
	m.ID = types.StringValue(k.ID)
	m.DisplayName = types.StringPointerValue(k.DisplayName)
	m.Geo = emptyStringAsNull(k.Geo)
	m.UserProfileID = types.StringPointerValue(k.UserProfileID)
	m.Attachment = types.StringValue(k.Attachment.Type)
	m.CreatedAt = types.StringValue(k.CreatedAt)
	m.UpdatedAt = types.StringValue(k.UpdatedAt)
	pc := k.ProviderConfig
	m.ProviderConfig = &ExternalKeyProviderConfigModel{
		Type:     types.StringValue(pc.Type),
		KMSARN:   emptyStringAsNull(pc.KMSARN),
		Region:   types.StringPointerValue(pc.Region),
		KeyName:  emptyStringAsNull(pc.KeyName),
		TenantID: emptyStringAsNull(pc.TenantID),
		VaultURI: emptyStringAsNull(pc.VaultURI),
		ClientID: types.StringPointerValue(pc.ClientID),
	}
}

func (r *ExternalKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ExternalKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.CreateExternalKey(ctx, client.CreateExternalKeyRequest{
		ProviderConfig: plan.ProviderConfig.toAPI(),
		DisplayName:    optionalString(plan.DisplayName),
		UserProfileID:  optionalString(plan.UserProfileID),
		Geo:            plan.Geo.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error Creating External Key", err.Error())
		return
	}

	mapExternalKeyToModel(key, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ExternalKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ExternalKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.GetExternalKey(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error Reading External Key", err.Error())
		return
	}

	mapExternalKeyToModel(key, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ExternalKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state ExternalKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Send only changed fields so a rename still works on an attached config, where geo and provider_config are locked.
	var body client.UpdateExternalKeyRequest
	if !plan.DisplayName.Equal(state.DisplayName) {
		body.DisplayName = optionalString(plan.DisplayName)
	}
	if !plan.Geo.IsUnknown() && !plan.Geo.Equal(state.Geo) {
		body.Geo = plan.Geo.ValueString()
	}
	p, s := plan.ProviderConfig, state.ProviderConfig
	if !p.Type.Equal(s.Type) || !p.KMSARN.Equal(s.KMSARN) || !p.KeyName.Equal(s.KeyName) ||
		!p.TenantID.Equal(s.TenantID) || !p.VaultURI.Equal(s.VaultURI) ||
		(!p.ClientID.IsUnknown() && !p.ClientID.Equal(s.ClientID)) ||
		(!p.Region.IsUnknown() && !p.Region.Equal(s.Region)) {
		pc := p.toAPI()
		body.ProviderConfig = &pc
	}

	key, err := r.client.UpdateExternalKey(ctx, state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error Updating External Key", err.Error())
		return
	}

	mapExternalKeyToModel(key, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ExternalKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ExternalKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteExternalKey(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Error Deleting External Key", err.Error())
	}
}

func (r *ExternalKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError(
			"Invalid Import Identifier",
			fmt.Sprintf("Expected external key ID matching %s, got %q", resourceIDPattern.String(), req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
