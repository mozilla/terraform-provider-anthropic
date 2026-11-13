// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mozilla/terraform-provider-anthropic/internal/client"
)

var (
	_ datasource.DataSource              = &WebhookDataSource{}
	_ datasource.DataSourceWithConfigure = &WebhookDataSource{}
	_ datasource.DataSource              = &WebhooksDataSource{}
	_ datasource.DataSourceWithConfigure = &WebhooksDataSource{}
)

func webhookDataAttributes() map[string]schema.Attribute {
	str := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc}
	}
	return map[string]schema.Attribute{
		"id":              str("ID of the webhook endpoint."),
		"url":             str("HTTPS URL that deliveries are sent to."),
		"enabled_events":  schema.SetAttribute{ElementType: types.StringType, Computed: true, MarkdownDescription: "Event types delivered to the endpoint."},
		"name":            str("Short label for the endpoint. Empty when none was given."),
		"description":     str("Notes about the endpoint. Empty when none were given."),
		"enabled":         schema.BoolAttribute{Computed: true, MarkdownDescription: "Whether deliveries are sent to the endpoint."},
		"disabled_reason": str("Why Anthropic disabled the endpoint automatically. Empty when enabled or disabled by you."),
		"created_at":      str("RFC 3339 timestamp when the endpoint was created."),
		"updated_at":      str("RFC 3339 timestamp when the endpoint was last changed."),
	}
}

// Singular

func NewWebhookDataSource() datasource.DataSource {
	return &WebhookDataSource{}
}

type WebhookDataSource struct {
	client *client.Client
}

func (d *WebhookDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (d *WebhookDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := webhookDataAttributes()
	attrs["id"] = schema.StringAttribute{
		Required:            true,
		MarkdownDescription: "ID of the webhook endpoint.",
		Validators:          []validator.String{idValidator()},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Reads an organization webhook endpoint by ID (`GET /v1/organizations/webhooks/{id}`). The signing secret is never returned by reads.",
		Attributes:          attrs,
	}
}

func (d *WebhookDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = telemetryWebhookDataSourceClient(req, resp)
}

func (d *WebhookDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state webhookFields
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	w, err := d.client.GetWebhook(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error Reading Webhook", err.Error())
		return
	}
	resp.Diagnostics.Append(populateWebhookFields(ctx, &state, w)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// List

func NewWebhooksDataSource() datasource.DataSource {
	return &WebhooksDataSource{}
}

type WebhooksDataSource struct {
	client *client.Client
}

type WebhooksDataSourceModel struct {
	Webhooks []webhookFields `tfsdk:"webhooks"`
}

func (d *WebhooksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhooks"
}

func (d *WebhooksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Beta. Lists the organization's webhook endpoints (`GET /v1/organizations/webhooks`). Workspace webhook endpoints are not included.",
		Attributes: map[string]schema.Attribute{
			"webhooks": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Organization webhook endpoints.",
				NestedObject:        schema.NestedAttributeObject{Attributes: webhookDataAttributes()},
			},
		},
	}
}

func (d *WebhooksDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = telemetryWebhookDataSourceClient(req, resp)
}

func (d *WebhooksDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	items, err := d.client.ListWebhooks(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error Listing Webhooks", err.Error())
		return
	}
	state := WebhooksDataSourceModel{Webhooks: make([]webhookFields, len(items))}
	for i := range items {
		resp.Diagnostics.Append(populateWebhookFields(ctx, &state.Webhooks[i], &items[i])...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
