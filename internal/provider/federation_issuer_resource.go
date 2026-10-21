// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ resource.Resource                = &FederationIssuerResource{}
	_ resource.ResourceWithConfigure   = &FederationIssuerResource{}
	_ resource.ResourceWithImportState = &FederationIssuerResource{}
)

func NewFederationIssuerResource() resource.Resource {
	return &FederationIssuerResource{}
}

type FederationIssuerResource struct {
	client *client.Client
}

type FederationIssuerJWKSModel struct {
	Type          types.String `tfsdk:"type"`
	DiscoveryBase types.String `tfsdk:"discovery_base"`
	URL           types.String `tfsdk:"url"`
	CACertPEM     types.String `tfsdk:"ca_cert_pem"`
}

type FederationIssuerResourceModel struct {
	ID                    types.String               `tfsdk:"id"`
	Name                  types.String               `tfsdk:"name"`
	IssuerURL             types.String               `tfsdk:"issuer_url"`
	CheckJTI              types.Bool                 `tfsdk:"check_jti"`
	MaxJWTLifetimeSeconds types.Int64                `tfsdk:"max_jwt_lifetime_seconds"`
	JWKS                  *FederationIssuerJWKSModel `tfsdk:"jwks"`
	CreatedAt             types.String               `tfsdk:"created_at"`
	UpdatedAt             types.String               `tfsdk:"updated_at"`
	ArchivedAt            types.String               `tfsdk:"archived_at"`
	Type                  types.String               `tfsdk:"type"`
}

func (r *FederationIssuerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_federation_issuer"
}

func (r *FederationIssuerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an OpenID Connect (OIDC) federation issuer (`/v1/organizations/federation_issuers`) for Workload Identity Federation (WIF). " +
			"Requires an `org:admin` OAuth access token (`oauth_token`) or `federation` authentication. " +
			"Deleting this resource archives the issuer (`POST /v1/organizations/federation_issuers/{id}/archive`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique tagged identifier of the federation issuer (`fdis_...`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug name of the federation issuer (`^[a-z0-9-]+$`, 1-255 characters).",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
					slugValidator(),
				},
			},
			"issuer_url": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Exact OIDC `iss` claim URL trusted by the organization (e.g. `https://token.actions.githubusercontent.com`).",
				Validators: []validator.String{
					nonEmptyStringValidator(),
				},
			},
			"check_jti": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Whether single-use `jti` replay tracking is enforced on JWT assertions. Defaults to `true`.",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"max_jwt_lifetime_seconds": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Maximum allowed `iat`-to-`exp` lifetime in seconds (`1` to `176400`, defaults to `3600`).",
				Validators: []validator.Int64{
					int64validator.Between(1, 176400),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"jwks": schema.SingleNestedAttribute{
				Required:            true,
				MarkdownDescription: "Configuration for fetching JSON Web Key Sets (JWKS) to verify incoming JWT signatures.",
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "JWKS resolution strategy (`discovery` or `explicit_url`).",
						Validators: []validator.String{
							stringvalidator.OneOf("discovery", "explicit_url"),
						},
					},
					"discovery_base": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Optional override base URL for OIDC `.well-known/openid-configuration` discovery.",
					},
					"url": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Explicit JWKS endpoint URL when `type = \"explicit_url\"`.",
					},
					"ca_cert_pem": schema.StringAttribute{
						Optional:            true,
						MarkdownDescription: "Optional custom CA certificate PEM bundle for TLS verification.",
					},
				},
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the issuer was created.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the issuer was last updated.",
			},
			"archived_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp when the issuer was archived, or `null` if active.",
			},
			"type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Object type (`federation_issuer`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *FederationIssuerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *FederationIssuerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan FederationIssuerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := client.CreateFederationIssuerRequest{
		Name:      plan.Name.ValueString(),
		IssuerURL: plan.IssuerURL.ValueString(),
		JWKS:      expandFederationIssuerJWKS(plan.JWKS),
	}
	if !plan.CheckJTI.IsNull() && !plan.CheckJTI.IsUnknown() {
		v := plan.CheckJTI.ValueBool()
		createReq.CheckJTI = &v
	}
	if !plan.MaxJWTLifetimeSeconds.IsNull() && !plan.MaxJWTLifetimeSeconds.IsUnknown() {
		v := plan.MaxJWTLifetimeSeconds.ValueInt64()
		createReq.MaxJWTLifetimeSeconds = &v
	}

	fi, err := r.client.CreateFederationIssuer(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error Creating Federation Issuer", err.Error())
		return
	}

	populateFederationIssuerModel(&plan, fi)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederationIssuerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state FederationIssuerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fi, err := r.client.GetFederationIssuer(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Federation Issuer",
			fmt.Sprintf("Could not read federation issuer %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	if fi.ArchivedAt != nil && *fi.ArchivedAt != "" {
		resp.State.RemoveResource(ctx)
		return
	}

	populateFederationIssuerModel(&state, fi)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *FederationIssuerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan FederationIssuerResourceModel
	var state FederationIssuerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	jwks := expandFederationIssuerJWKS(plan.JWKS)
	updateReq := client.UpdateFederationIssuerRequest{
		Name:      plan.Name.ValueString(),
		IssuerURL: plan.IssuerURL.ValueString(),
		JWKS:      &jwks,
	}
	if !plan.CheckJTI.IsNull() && !plan.CheckJTI.IsUnknown() {
		v := plan.CheckJTI.ValueBool()
		updateReq.CheckJTI = &v
	}
	if !plan.MaxJWTLifetimeSeconds.IsNull() && !plan.MaxJWTLifetimeSeconds.IsUnknown() {
		v := plan.MaxJWTLifetimeSeconds.ValueInt64()
		updateReq.MaxJWTLifetimeSeconds = &v
	}

	fi, err := r.client.UpdateFederationIssuer(ctx, state.ID.ValueString(), updateReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Federation Issuer",
			fmt.Sprintf("Could not update federation issuer %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	populateFederationIssuerModel(&plan, fi)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *FederationIssuerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state FederationIssuerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.ArchiveFederationIssuer(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Archiving Federation Issuer",
			fmt.Sprintf("Could not archive federation issuer %s: %s", state.ID.ValueString(), err.Error()),
		)
	}
}

func (r *FederationIssuerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !resourceIDPattern.MatchString(req.ID) {
		resp.Diagnostics.AddError(
			"Invalid Import Identifier",
			fmt.Sprintf("Expected federation issuer ID matching %s, got %q", resourceIDPattern.String(), req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func expandFederationIssuerJWKS(m *FederationIssuerJWKSModel) client.FederationIssuerJWKS {
	if m == nil {
		return client.FederationIssuerJWKS{Type: "discovery"}
	}
	out := client.FederationIssuerJWKS{
		Type: m.Type.ValueString(),
	}
	if !m.DiscoveryBase.IsNull() && !m.DiscoveryBase.IsUnknown() {
		v := m.DiscoveryBase.ValueString()
		out.DiscoveryBase = &v
	}
	if !m.URL.IsNull() && !m.URL.IsUnknown() {
		v := m.URL.ValueString()
		out.URL = &v
	}
	if !m.CACertPEM.IsNull() && !m.CACertPEM.IsUnknown() {
		v := m.CACertPEM.ValueString()
		out.CACertPEM = &v
	}
	return out
}

func populateFederationIssuerModel(m *FederationIssuerResourceModel, fi *client.FederationIssuer) {
	m.ID = types.StringValue(fi.ID)
	m.Name = types.StringValue(fi.Name)
	m.IssuerURL = types.StringValue(fi.IssuerURL)
	if fi.CheckJTI != nil {
		m.CheckJTI = types.BoolValue(*fi.CheckJTI)
	} else {
		m.CheckJTI = types.BoolValue(true)
	}
	if fi.MaxJWTLifetimeSeconds != nil {
		m.MaxJWTLifetimeSeconds = types.Int64Value(*fi.MaxJWTLifetimeSeconds)
	} else {
		m.MaxJWTLifetimeSeconds = types.Int64Value(3600)
	}

	jwksModel := &FederationIssuerJWKSModel{
		Type: types.StringValue(fi.JWKS.Type),
	}
	if fi.JWKS.DiscoveryBase != nil {
		jwksModel.DiscoveryBase = types.StringValue(*fi.JWKS.DiscoveryBase)
	} else {
		jwksModel.DiscoveryBase = types.StringNull()
	}
	if fi.JWKS.URL != nil {
		jwksModel.URL = types.StringValue(*fi.JWKS.URL)
	} else {
		jwksModel.URL = types.StringNull()
	}
	if fi.JWKS.CACertPEM != nil {
		jwksModel.CACertPEM = types.StringValue(*fi.JWKS.CACertPEM)
	} else {
		jwksModel.CACertPEM = types.StringNull()
	}
	m.JWKS = jwksModel

	m.CreatedAt = types.StringValue(fi.CreatedAt)
	m.UpdatedAt = types.StringValue(fi.UpdatedAt)
	if fi.ArchivedAt != nil && *fi.ArchivedAt != "" {
		m.ArchivedAt = types.StringValue(*fi.ArchivedAt)
	} else {
		m.ArchivedAt = types.StringNull()
	}
	m.Type = types.StringValue(fi.Type)
}
