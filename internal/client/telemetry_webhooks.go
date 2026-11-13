// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"context"
	"net/http"
	"net/url"
)

const (
	BetaTelemetryDestinations = "telemetry-destinations-2026-08-11"
	BetaWebhooks              = "webhooks-2026-10-07"
)

// ============================================================================
// Telemetry destinations (beta)
// ============================================================================

type TelemetryDestination struct {
	ID             string                       `json:"id"`
	CreatedAt      string                       `json:"created_at"`
	DeliveryHealth TelemetryDeliveryHealth      `json:"delivery_health"`
	DisplayName    *string                      `json:"display_name"`
	EndpointURL    string                       `json:"endpoint_url"`
	HeaderNames    []string                     `json:"header_names"`
	State          TelemetryDestinationState    `json:"state"`
	Traces         TelemetryTraces              `json:"traces"`
	UpdatedAt      string                       `json:"updated_at"`
	Workspaces     TelemetryDestinationScopeRef `json:"workspaces"`
	Type           string                       `json:"type"`
}

type TelemetryDeliveryHealth struct {
	LastError     *string `json:"last_error"`
	LastErrorAt   *string `json:"last_error_at"`
	LastSuccessAt *string `json:"last_success_at"`
}

type TelemetryDestinationState struct {
	Type   string `json:"type"`
	Reason *struct {
		Type string `json:"type"`
	} `json:"reason,omitempty"`
}

type TelemetryTraces struct {
	ContentMode     string  `json:"content_mode,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	EndpointURL     *string `json:"endpoint_url,omitempty"`
	MaxContentBytes *int64  `json:"max_content_bytes,omitempty"`
}

type TelemetryDestinationScopeRef struct {
	Type string `json:"type"`
}

type CreateTelemetryDestinationRequest struct {
	EndpointURL string                       `json:"endpoint_url"`
	Workspaces  TelemetryDestinationScopeRef `json:"workspaces"`
	DisplayName *string                      `json:"display_name,omitempty"`
	Headers     map[string]string            `json:"headers,omitempty"`
	Traces      *TelemetryTraces             `json:"traces,omitempty"`
}

type TelemetryDestinationWorkspace struct {
	CreatedAt              string `json:"created_at"`
	TelemetryDestinationID string `json:"telemetry_destination_id"`
	WorkspaceID            string `json:"workspace_id"`
	Type                   string `json:"type"`
}

func (c *Client) CreateTelemetryDestination(ctx context.Context, req CreateTelemetryDestinationRequest) (*TelemetryDestination, error) {
	p, err := Path("telemetry_destinations")
	if err != nil {
		return nil, err
	}
	var out TelemetryDestination
	if err := c.Do(ctx, http.MethodPost, p, req, &out, WithBeta(BetaTelemetryDestinations)); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetTelemetryDestination(ctx context.Context, id string) (*TelemetryDestination, error) {
	p, err := Path("telemetry_destinations", id)
	if err != nil {
		return nil, err
	}
	var out TelemetryDestination
	if err := c.Do(ctx, http.MethodGet, p, nil, &out, WithBeta(BetaTelemetryDestinations)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTelemetryDestination takes a raw body because the API distinguishes omitted fields from explicit nulls.
func (c *Client) UpdateTelemetryDestination(ctx context.Context, id string, body map[string]any) (*TelemetryDestination, error) {
	p, err := Path("telemetry_destinations", id)
	if err != nil {
		return nil, err
	}
	var out TelemetryDestination
	if err := c.Do(ctx, http.MethodPost, p, body, &out, WithBeta(BetaTelemetryDestinations)); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListTelemetryDestinations(ctx context.Context, workspaceIDs []string) ([]TelemetryDestination, error) {
	p, err := Path("telemetry_destinations")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, id := range workspaceIDs {
		q.Add("workspace_ids[]", id)
	}
	return ListAll[TelemetryDestination](ctx, c, p, q, WithBeta(BetaTelemetryDestinations))
}

func (c *Client) DeleteTelemetryDestination(ctx context.Context, id string) error {
	p, err := Path("telemetry_destinations", id)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaTelemetryDestinations))
}

func (c *Client) AddTelemetryDestinationWorkspace(ctx context.Context, destinationID, workspaceID string) (*TelemetryDestinationWorkspace, error) {
	p, err := Path("telemetry_destinations", destinationID, "workspaces", workspaceID)
	if err != nil {
		return nil, err
	}
	var out TelemetryDestinationWorkspace
	if err := c.Do(ctx, http.MethodPost, p, nil, &out, WithBeta(BetaTelemetryDestinations)); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTelemetryDestinationWorkspace scans the destination's list because the API has no single-entry read.
func (c *Client) GetTelemetryDestinationWorkspace(ctx context.Context, destinationID, workspaceID string) (*TelemetryDestinationWorkspace, error) {
	p, err := Path("telemetry_destinations", destinationID, "workspaces")
	if err != nil {
		return nil, err
	}
	items, err := ListAll[TelemetryDestinationWorkspace](ctx, c, p, nil, WithBeta(BetaTelemetryDestinations))
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].WorkspaceID == workspaceID {
			return &items[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Type: "not_found_error", Message: "workspace is not on the telemetry destination's list"}
}

func (c *Client) RemoveTelemetryDestinationWorkspace(ctx context.Context, destinationID, workspaceID string) error {
	p, err := Path("telemetry_destinations", destinationID, "workspaces", workspaceID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaTelemetryDestinations))
}

// ============================================================================
// Organization webhooks (beta)
// ============================================================================

type Webhook struct {
	ID             string   `json:"id"`
	CreatedAt      string   `json:"created_at"`
	Description    string   `json:"description"`
	DisabledReason string   `json:"disabled_reason"`
	EnabledEvents  []string `json:"enabled_events"`
	Name           string   `json:"name"`
	SigningSecret  string   `json:"signing_secret,omitempty"`
	State          string   `json:"state"`
	UpdatedAt      string   `json:"updated_at"`
	URL            string   `json:"url"`
	Type           string   `json:"type,omitempty"`
}

type CreateWebhookRequest struct {
	URL           string   `json:"url"`
	EnabledEvents []string `json:"enabled_events"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
}

type UpdateWebhookRequest struct {
	URL           string   `json:"url"`
	EnabledEvents []string `json:"enabled_events"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	State         string   `json:"state"`
}

func (c *Client) CreateWebhook(ctx context.Context, req CreateWebhookRequest) (*Webhook, error) {
	p, err := Path("webhooks")
	if err != nil {
		return nil, err
	}
	var out Webhook
	if err := c.Do(ctx, http.MethodPost, p, req, &out, WithBeta(BetaWebhooks)); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	p, err := Path("webhooks", id)
	if err != nil {
		return nil, err
	}
	var out Webhook
	if err := c.Do(ctx, http.MethodGet, p, nil, &out, WithBeta(BetaWebhooks)); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateWebhook(ctx context.Context, id string, req UpdateWebhookRequest) (*Webhook, error) {
	p, err := Path("webhooks", id)
	if err != nil {
		return nil, err
	}
	var out Webhook
	if err := c.Do(ctx, http.MethodPost, p, req, &out, WithBeta(BetaWebhooks)); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	p, err := Path("webhooks")
	if err != nil {
		return nil, err
	}
	return ListAll[Webhook](ctx, c, p, nil, WithBeta(BetaWebhooks))
}

func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	p, err := Path("webhooks", id)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaWebhooks))
}
