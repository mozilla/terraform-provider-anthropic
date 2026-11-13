// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"context"
	"net/http"
)

// Compliance settings, external keys and RBAC role permissions are GA, so no anthropic-beta header is sent.

// ComplianceSettingsState is the {"type": "enabled"|"disabled"} object form of the compliance state.
type ComplianceSettingsState struct {
	Type string `json:"type"`
}

// ExternalKeyAttachment is {"type": "attached"|"unattached"}.
type ExternalKeyAttachment struct {
	Type string `json:"type"`
}

// ComplianceSettings is the organization singleton from /v1/organizations/compliance_settings.
type ComplianceSettings struct {
	State ComplianceSettingsState `json:"state"`
	Type  string                  `json:"type"`
}

// GetComplianceSettings reads the organization's compliance settings (GET /v1/organizations/compliance_settings).
func (c *Client) GetComplianceSettings(ctx context.Context) (*ComplianceSettings, error) {
	p, err := Path("compliance_settings")
	if err != nil {
		return nil, err
	}
	var out ComplianceSettings
	if err := c.Do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateComplianceSettings sets the compliance state, "enabled" or "disabled" (POST /v1/organizations/compliance_settings).
func (c *Client) UpdateComplianceSettings(ctx context.Context, state string) (*ComplianceSettings, error) {
	p, err := Path("compliance_settings")
	if err != nil {
		return nil, err
	}
	var out ComplianceSettings
	body := map[string]ComplianceSettingsState{"state": {Type: state}}
	if err := c.Do(ctx, http.MethodPost, p, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExternalKeyProviderConfig is the KMS provider identity of an external key. Which fields apply depends on Type
// ("aws": kms_arn, region; "gcp": key_name; "azure": key_name, tenant_id, vault_uri, client_id).
type ExternalKeyProviderConfig struct {
	Type     string  `json:"type"`
	KMSARN   string  `json:"kms_arn,omitempty"`
	Region   *string `json:"region,omitempty"`
	KeyName  string  `json:"key_name,omitempty"`
	TenantID string  `json:"tenant_id,omitempty"`
	VaultURI string  `json:"vault_uri,omitempty"`
	ClientID *string `json:"client_id,omitempty"`
}

// ExternalKey is a CMEK external key config from /v1/organizations/external_keys.
type ExternalKey struct {
	ID             string                    `json:"id"`
	Attachment     ExternalKeyAttachment     `json:"attachment"`
	CreatedAt      string                    `json:"created_at"`
	UpdatedAt      string                    `json:"updated_at"`
	DisplayName    *string                   `json:"display_name"`
	Geo            string                    `json:"geo"`
	ProviderConfig ExternalKeyProviderConfig `json:"provider_config"`
	UserProfileID  *string                   `json:"user_profile_id"`
	Type           string                    `json:"type"`
}

// CreateExternalKeyRequest is the body for POST /v1/organizations/external_keys.
type CreateExternalKeyRequest struct {
	ProviderConfig ExternalKeyProviderConfig `json:"provider_config"`
	DisplayName    *string                   `json:"display_name,omitempty"`
	UserProfileID  *string                   `json:"user_profile_id,omitempty"`
	Geo            string                    `json:"geo,omitempty"`
}

// UpdateExternalKeyRequest is the body for POST /v1/organizations/external_keys/{id}. Omitted fields are unchanged.
type UpdateExternalKeyRequest struct {
	DisplayName    *string                    `json:"display_name,omitempty"`
	Geo            string                     `json:"geo,omitempty"`
	ProviderConfig *ExternalKeyProviderConfig `json:"provider_config,omitempty"`
}

// CreateExternalKey creates an external key config (POST /v1/organizations/external_keys).
func (c *Client) CreateExternalKey(ctx context.Context, req CreateExternalKeyRequest) (*ExternalKey, error) {
	p, err := Path("external_keys")
	if err != nil {
		return nil, err
	}
	var out ExternalKey
	if err := c.Do(ctx, http.MethodPost, p, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetExternalKey reads an external key config (GET /v1/organizations/external_keys/{id}).
func (c *Client) GetExternalKey(ctx context.Context, id string) (*ExternalKey, error) {
	p, err := Path("external_keys", id)
	if err != nil {
		return nil, err
	}
	var out ExternalKey
	if err := c.Do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListExternalKeys lists all external key configs (GET /v1/organizations/external_keys).
func (c *Client) ListExternalKeys(ctx context.Context) ([]ExternalKey, error) {
	p, err := Path("external_keys")
	if err != nil {
		return nil, err
	}
	return ListAll[ExternalKey](ctx, c, p, nil)
}

// UpdateExternalKey partially updates an external key config (POST /v1/organizations/external_keys/{id}).
func (c *Client) UpdateExternalKey(ctx context.Context, id string, req UpdateExternalKeyRequest) (*ExternalKey, error) {
	p, err := Path("external_keys", id)
	if err != nil {
		return nil, err
	}
	var out ExternalKey
	if err := c.Do(ctx, http.MethodPost, p, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteExternalKey deletes an external key config (DELETE /v1/organizations/external_keys/{id}).
func (c *Client) DeleteExternalKey(ctx context.Context, id string) error {
	p, err := Path("external_keys", id)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil)
}

// RBACRolePermissionResource is the tagged union a role permission applies to. Fields depend on Type
// ("organization", "connector_tool", "connector_scope", "connector", "all_connectors").
type RBACRolePermissionResource struct {
	Type           string `json:"type"`
	OrganizationID string `json:"organization_id"`
	ConnectorID    string `json:"connector_id"`
	ToolName       string `json:"tool_name"`
	Scope          string `json:"scope"`
}

// RBACRolePermission is one grant from /v1/organizations/rbac_roles/{id}/permissions.
type RBACRolePermission struct {
	Action   string                     `json:"action"`
	Resource RBACRolePermissionResource `json:"resource"`
	Type     string                     `json:"type"`
}

// ListRBACRolePermissions lists the permissions a role grants (GET /v1/organizations/rbac_roles/{id}/permissions).
func (c *Client) ListRBACRolePermissions(ctx context.Context, roleID string) ([]RBACRolePermission, error) {
	p, err := Path("rbac_roles", roleID, "permissions")
	if err != nil {
		return nil, err
	}
	return ListAll[RBACRolePermission](ctx, c, p, nil)
}
