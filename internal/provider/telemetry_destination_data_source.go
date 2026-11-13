// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &TelemetryDestinationDataSource{}
	_ datasource.DataSourceWithConfigure = &TelemetryDestinationDataSource{}
	_ datasource.DataSource              = &TelemetryDestinationsDataSource{}
	_ datasource.DataSourceWithConfigure = &TelemetryDestinationsDataSource{}
)

type TelemetryDestinationDataModel struct {
	telemetryDestinationFields
	LastError     types.String `tfsdk:"last_error"`
	LastErrorAt   types.String `tfsdk:"last_error_at"`
	LastSuccessAt types.String `tfsdk:"last_success_at"`
}

func populateTelemetryDestinationData(ctx context.Context, m *TelemetryDestinationDataModel, d *client.TelemetryDestination) diag.Diagnostics {
	m.LastError = types.StringPointerValue(d.DeliveryHealth.LastError)
	m.LastErrorAt = types.StringPointerValue(d.DeliveryHealth.LastErrorAt)
	m.LastSuccessAt = types.StringPointerValue(d.DeliveryHealth.LastSuccessAt)
	return populateTelemetryDestinationFields(ctx, &m.telemetryDestinationFields, d)
}

func telemetryDestinationDataAttributes() map[string]schema.Attribute {
	str := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	return map[string]schema.Attribute{
		"id":                       str("Unique identifier of the destination (`tdst_...`)."),
		"display_name":             str("Human-readable name, or `null`."),
		"endpoint_url":             str("Base URL of the collector's OTLP/HTTP endpoint."),
		"header_names":             schema.ListAttribute{ElementType: types.StringType, Computed: true, MarkdownDescription: "Names of the registered request headers. Values are never returned."},
		"workspace_scope":          str("Which workspaces the destination covers: `all`, `include` or `exclude`."),
		"enabled":                  schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the destination receives deliveries."},
		"disabled_reason":          str("Why the destination is disabled, or `null` when enabled."),
		"traces_enabled":           schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether the destination receives traces."},
		"traces_content_mode":      str("What exported spans contain: `metadata_only` or `content`."),
		"traces_endpoint_url":      str("Full traces URL, or `null` when traces go to `endpoint_url` plus `/v1/traces`."),
		"traces_max_content_bytes": schema.Int64Attribute{Computed: true, MarkdownDescription: "Most bytes of content per span, or `null` when only the API's maximum applies."},
		"created_at":               str("RFC 3339 timestamp when the destination was created."),
		"updated_at":               str("RFC 3339 timestamp of the last configuration or state change."),
		"last_error":               str("Most recent delivery error, or `null`."),
		"last_error_at":            str("RFC 3339 timestamp of the most recent delivery error, or `null`."),
		"last_success_at":          str("RFC 3339 timestamp of the most recent successful delivery, or `null`."),
	}
}

func telemetryWebhookDataSourceClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type", fmt.Sprintf("Expected *client.Client, got: %T.", req.ProviderData))
	}
	return c
}

// Singular

func NewTelemetryDestinationDataSource() datasource.DataSource {
	return &TelemetryDestinationDataSource{}
}

type TelemetryDestinationDataSource struct {
	client *client.Client
}

func (d *TelemetryDestinationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_telemetry_destination"
}

func (d *TelemetryDestinationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := telemetryDestinationDataAttributes()
	attrs["id"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "Unique identifier of the destination (`tdst_...`).",
		Validators:          []validator.String{idValidator()},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Reads a telemetry destination by ID, including its most recent delivery results (`GET /v1/organizations/telemetry_destinations/{id}`).",
		Attributes:          attrs,
	}
}

func (d *TelemetryDestinationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = telemetryWebhookDataSourceClient(req, resp)
}

func (d *TelemetryDestinationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state TelemetryDestinationDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dst, err := d.client.GetTelemetryDestination(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Telemetry Destination", err.Error())
		return
	}
	resp.Diagnostics.Append(populateTelemetryDestinationData(ctx, &state, dst)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// List

func NewTelemetryDestinationsDataSource() datasource.DataSource {
	return &TelemetryDestinationsDataSource{}
}

type TelemetryDestinationsDataSource struct {
	client *client.Client
}

type TelemetryDestinationsDataSourceModel struct {
	WorkspaceIDs          types.List                      `tfsdk:"workspace_ids"`
	TelemetryDestinations []TelemetryDestinationDataModel `tfsdk:"telemetry_destinations"`
}

func (d *TelemetryDestinationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_telemetry_destinations"
}

func (d *TelemetryDestinationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Lists the organization's telemetry destinations, newest first (`GET /v1/organizations/telemetry_destinations`).",
		Attributes: map[string]schema.Attribute{
			"workspace_ids": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Return only destinations that cover at least one of these workspaces (`wrkspc_...`). At most 100.",
			},
			"telemetry_destinations": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Telemetry destinations of the organization.",
				NestedObject:        schema.NestedAttributeObject{Attributes: telemetryDestinationDataAttributes()},
			},
		},
	}
}

func (d *TelemetryDestinationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = telemetryWebhookDataSourceClient(req, resp)
}

func (d *TelemetryDestinationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state TelemetryDestinationsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	var wsIDs []string
	resp.Diagnostics.Append(state.WorkspaceIDs.ElementsAs(ctx, &wsIDs, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	items, err := d.client.ListTelemetryDestinations(ctx, wsIDs)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Telemetry Destinations", err.Error())
		return
	}
	state.TelemetryDestinations = make([]TelemetryDestinationDataModel, len(items))
	for i := range items {
		resp.Diagnostics.Append(populateTelemetryDestinationData(ctx, &state.TelemetryDestinations[i], &items[i])...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
