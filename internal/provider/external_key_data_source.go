// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &ExternalKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &ExternalKeyDataSource{}
	_ datasource.DataSource              = &ExternalKeysDataSource{}
	_ datasource.DataSourceWithConfigure = &ExternalKeysDataSource{}
)

func externalKeyDataSourceAttributes() map[string]schema.Attribute {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
	}
	return map[string]schema.Attribute{
		"id":              computed("Identifier of the external key config (`ekey_...`)."),
		"display_name":    computed("Human-friendly display name, or null if none was set."),
		"geo":             computed("Data residency geo the key is used in."),
		"user_profile_id": computed("User profile the key is bound to, or null if unbound."),
		"attachment":      computed("`attached` if any live or archived workspace uses this config, otherwise `unattached`."),
		"created_at":      computed("RFC 3339 timestamp when the config was created."),
		"updated_at":      computed("RFC 3339 timestamp when the config was last updated."),
		"provider_config": schema.SingleNestedAttribute{
			MarkdownDescription: "KMS provider identity and auth coordinates.",
			Computed:            true,
			Attributes: map[string]schema.Attribute{
				"type":      computed("KMS provider: `aws`, `gcp` or `azure`."),
				"kms_arn":   computed("AWS only. Full ARN of the AWS KMS key."),
				"region":    computed("AWS only. AWS region."),
				"key_name":  computed("GCP: full resource name of the Cloud KMS key. Azure: name of the key within the vault."),
				"tenant_id": computed("Azure only. Azure AD tenant ID."),
				"vault_uri": computed("Azure only. Key Vault data-plane URI."),
				"client_id": computed("Azure only. Application (client) ID, or null for Anthropic's multitenant app."),
			},
		},
	}
}

func dataSourceClientOrDiag(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
	}
	return c
}

func NewExternalKeyDataSource() datasource.DataSource {
	return &ExternalKeyDataSource{}
}

type ExternalKeyDataSource struct {
	client *client.Client
}

func (d *ExternalKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_key"
}

func (d *ExternalKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := externalKeyDataSourceAttributes()
	attrs["id"] = schema.StringAttribute{
		MarkdownDescription: "Identifier of the external key config (`ekey_...`).",
		Required:            true,
		Validators:          []validator.String{idValidator()},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a customer-managed encryption key (CMEK) config by ID (`/v1/organizations/external_keys/{id}`).",
		Attributes:          attrs,
	}
}

func (d *ExternalKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = dataSourceClientOrDiag(req, resp)
}

func (d *ExternalKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ExternalKeyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := d.client.GetExternalKey(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading External Key", err.Error())
		return
	}
	mapExternalKeyToModel(key, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func NewExternalKeysDataSource() datasource.DataSource {
	return &ExternalKeysDataSource{}
}

type ExternalKeysDataSource struct {
	client *client.Client
}

type ExternalKeysDataSourceModel struct {
	ExternalKeys []ExternalKeyModel `tfsdk:"external_keys"`
}

func (d *ExternalKeysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_keys"
}

func (d *ExternalKeysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the organization's customer-managed encryption key (CMEK) configs (`/v1/organizations/external_keys`), newest first.",
		Attributes: map[string]schema.Attribute{
			"external_keys": schema.ListNestedAttribute{
				MarkdownDescription: "External key configs in the organization.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: externalKeyDataSourceAttributes()},
			},
		},
	}
}

func (d *ExternalKeysDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = dataSourceClientOrDiag(req, resp)
}

func (d *ExternalKeysDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	keys, err := d.client.ListExternalKeys(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing External Keys", err.Error())
		return
	}
	state := ExternalKeysDataSourceModel{ExternalKeys: make([]ExternalKeyModel, len(keys))}
	for i := range keys {
		mapExternalKeyToModel(&keys[i], &state.ExternalKeys[i])
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
