// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &ComplianceSettingsDataSource{}
	_ datasource.DataSourceWithConfigure = &ComplianceSettingsDataSource{}
)

func NewComplianceSettingsDataSource() datasource.DataSource {
	return &ComplianceSettingsDataSource{}
}

type ComplianceSettingsDataSource struct {
	client *client.Client
}

func (d *ComplianceSettingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_compliance_settings"
}

func (d *ComplianceSettingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the organization's Compliance Settings (`/v1/organizations/compliance_settings`). " +
			"An organization with a parent organization reads the state inherited from the parent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Always `" + complianceSettingsID + "`.",
				Computed:            true,
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "Whether the Compliance API is enabled: `enabled` or `disabled`.",
				Computed:            true,
			},
		},
	}
}

func (d *ComplianceSettingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *ComplianceSettingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	s, err := d.client.GetComplianceSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Compliance Settings", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &ComplianceSettingsModel{
		ID:    types.StringValue(complianceSettingsID),
		State: types.StringValue(s.State.Type),
	})...)
}
