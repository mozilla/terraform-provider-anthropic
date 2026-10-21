// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"encoding/json"
	"fmt"
)

// Organization represents the response from GET /v1/organizations/me.
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// User represents an organization member from /v1/organizations/users.
type User struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	AddedAt string `json:"added_at"`
	Type    string `json:"type"`
}

// UpdateUserRequest is the request body for POST /v1/organizations/users/{user_id}.
type UpdateUserRequest struct {
	Role string `json:"role"`
}

// DeleteResponse represents a deletion confirmation payload (e.g. user_deleted, invite_deleted).
type DeleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// Invite represents an organization invitation from /v1/organizations/invites.
type Invite struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	InvitedAt string `json:"invited_at"`
	ExpiresAt string `json:"expires_at"`
	Type      string `json:"type"`
}

// CreateInviteRequest is the request body for POST /v1/organizations/invites.
type CreateInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// AllowedInferenceGeos handles the Anthropic Workspace API's union type for allowed_inference_geos,
// which can be serialized either as a JSON string ("unrestricted") or an array of strings (["us", "global"]).
type AllowedInferenceGeos struct {
	Unrestricted bool
	Geos         []string
}

func (a AllowedInferenceGeos) MarshalJSON() ([]byte, error) {
	if a.Unrestricted {
		return json.Marshal("unrestricted")
	}
	if a.Geos == nil {
		return []byte("null"), nil
	}
	return json.Marshal(a.Geos)
}

func (a *AllowedInferenceGeos) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if str == "unrestricted" {
			a.Unrestricted = true
			a.Geos = nil
			return nil
		}
		a.Unrestricted = false
		a.Geos = []string{str}
		return nil
	}

	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		a.Unrestricted = false
		a.Geos = list
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into AllowedInferenceGeos", string(data))
}

// DataResidency represents workspace data residency and inference geography controls.
type DataResidency struct {
	WorkspaceGeo         string                `json:"workspace_geo,omitempty"`
	DefaultInferenceGeo  string                `json:"default_inference_geo,omitempty"`
	AllowedInferenceGeos *AllowedInferenceGeos `json:"allowed_inference_geos,omitempty"`
}

// Workspace represents a Claude Console workspace from /v1/organizations/workspaces.
type Workspace struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	DisplayColor  string         `json:"display_color"`
	CreatedAt     string         `json:"created_at"`
	ArchivedAt    *string        `json:"archived_at"`
	DataResidency *DataResidency `json:"data_residency,omitempty"`
	Type          string         `json:"type"`
}

// CreateWorkspaceRequest is the request body for POST /v1/organizations/workspaces.
type CreateWorkspaceRequest struct {
	Name          string         `json:"name"`
	DataResidency *DataResidency `json:"data_residency,omitempty"`
}

// UpdateWorkspaceRequest is the request body for POST /v1/organizations/workspaces/{workspace_id}.
type UpdateWorkspaceRequest struct {
	Name          string         `json:"name"`
	DataResidency *DataResidency `json:"data_residency,omitempty"`
}

// WorkspaceMember represents a member of a workspace from /v1/organizations/workspaces/{workspace_id}/members.
type WorkspaceMember struct {
	UserID        string `json:"user_id"`
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceRole string `json:"workspace_role"`
	Type          string `json:"type"`
}

// CreateWorkspaceMemberRequest is the request body for POST /v1/organizations/workspaces/{workspace_id}/members.
type CreateWorkspaceMemberRequest struct {
	UserID        string `json:"user_id"`
	WorkspaceRole string `json:"workspace_role"`
}

// UpdateWorkspaceMemberRequest is the request body for POST /v1/organizations/workspaces/{workspace_id}/members/{user_id}.
type UpdateWorkspaceMemberRequest struct {
	WorkspaceRole string `json:"workspace_role"`
}

// ActorRef identifies the creator of an API key.
type ActorRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// APIKey represents an organization API key from /v1/organizations/api_keys.
type APIKey struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	PartialKeyHint string    `json:"partial_key_hint"`
	WorkspaceID    *string   `json:"workspace_id"`
	CreatedAt      string    `json:"created_at"`
	CreatedBy      *ActorRef `json:"created_by"`
	Type           string    `json:"type"`
}

// UpdateAPIKeyRequest is the request body for POST /v1/organizations/api_keys/{api_key_id}.
type UpdateAPIKeyRequest struct {
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
}

// RBACGroup represents a Claude Enterprise RBAC group from /v1/organizations/rbac_groups.
// It unmarshals both "role_ids" and "roles" to support both SDK and documented REST payloads.
type RBACGroup struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SourceType string    `json:"source_type"`
	RoleIDs    *[]string `json:"role_ids"`
	Roles      *[]string `json:"roles"`
	CreatedAt  string    `json:"created_at"`
	UpdatedAt  string    `json:"updated_at"`
	Type       string    `json:"type"`
}

// EffectiveRoleIDs returns the resolved slice of role IDs and whether the response was a
// degraded read (explicit JSON null on both role_ids and roles).
func (g *RBACGroup) EffectiveRoleIDs() ([]string, bool) {
	if g.RoleIDs != nil {
		return *g.RoleIDs, false
	}
	if g.Roles != nil {
		return *g.Roles, false
	}
	return nil, true
}

// CreateRBACGroupRequest is the request body for POST /v1/organizations/rbac_groups?beta=true.
// Only "name" is accepted by the REST API; role_ids are read-only via the API.
type CreateRBACGroupRequest struct {
	Name string `json:"name"`
}

// UpdateRBACGroupRequest is the request body for POST /v1/organizations/rbac_groups/{id}?beta=true.
// Only "name" is accepted by the REST API; role_ids are read-only via the API.
type UpdateRBACGroupRequest struct {
	Name string `json:"name"`
}

// RBACGroupMember represents a Claude Enterprise RBAC group member from /v1/organizations/rbac_groups/{id}/members.
// It unmarshals both "group_id" and "rbac_group_id".
type RBACGroupMember struct {
	UserID      string `json:"user_id"`
	GroupID     string `json:"group_id"`
	RBACGroupID string `json:"rbac_group_id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
	Type        string `json:"type"`
}

// EffectiveGroupID returns the non-empty group ID from the response, falling back to fallbackGroupID.
func (m *RBACGroupMember) EffectiveGroupID(fallbackGroupID string) string {
	if m.GroupID != "" {
		return m.GroupID
	}
	if m.RBACGroupID != "" {
		return m.RBACGroupID
	}
	return fallbackGroupID
}

// AddRBACGroupMemberRequest is the request body for POST /v1/organizations/rbac_groups/{id}/members.
type AddRBACGroupMemberRequest struct {
	UserID string `json:"user_id"`
}

// RBACRole represents a Claude Enterprise role from GET /v1/organizations/rbac_roles.
type RBACRole struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	Type        string  `json:"type"`
}

// SpendLimitScope identifies the target of a spend limit rule.
type SpendLimitScope struct {
	Type   string `json:"type"`
	UserID string `json:"user_id"`
}

// SpendLimit represents a Claude Enterprise per-user spend limit override from /v1/organizations/spend_limits.
type SpendLimit struct {
	ID        string          `json:"id"`
	Amount    *string         `json:"amount"`
	Currency  string          `json:"currency"`
	Period    string          `json:"period"`
	Scope     SpendLimitScope `json:"scope"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
	Type      string          `json:"type"`
}

// UpsertSpendLimitRequest is the request body for POST /v1/organizations/spend_limits.
// Amount intentionally does NOT have omitempty so that nil serializes as `"amount": null`
// when configuring an explicit unlimited spend override.
type UpsertSpendLimitRequest struct {
	UserID string  `json:"user_id"`
	Period string  `json:"period"`
	Amount *string `json:"amount"`
}

// ============================================================================
// Workload Identity Federation (WIF) & Service Accounts
// ============================================================================

// ServiceAccount represents a non-human machine identity from /v1/organizations/service_accounts.
type ServiceAccount struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	OrganizationRole string  `json:"organization_role"`
	Description      *string `json:"description"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at,omitempty"`
	ArchivedAt       *string `json:"archived_at"`
	Type             string  `json:"type"`
}

// CreateServiceAccountRequest is the request body for POST /v1/organizations/service_accounts.
type CreateServiceAccountRequest struct {
	Name             string  `json:"name"`
	OrganizationRole string  `json:"organization_role"`
	Description      *string `json:"description,omitempty"`
}

// UpdateServiceAccountRequest is the request body for POST /v1/organizations/service_accounts/{id}.
// Service account names are immutable after creation; only organization_role and description can be updated.
type UpdateServiceAccountRequest struct {
	OrganizationRole string  `json:"organization_role,omitempty"`
	Description      *string `json:"description,omitempty"`
}

// ServiceAccountWorkspaceMember represents a service account's membership in a workspace
// from /v1/organizations/service_accounts/{service_account_id}/workspaces.
type ServiceAccountWorkspaceMember struct {
	ServiceAccountID string  `json:"service_account_id"`
	WorkspaceID      string  `json:"workspace_id"`
	WorkspaceRole    string  `json:"workspace_role"`
	CreatedByActorID *string `json:"created_by_actor_id,omitempty"`
	Implicit         bool    `json:"implicit"`
	Type             string  `json:"type"`
}

// AddServiceAccountToWorkspaceRequest is the request body for
// POST /v1/organizations/service_accounts/{service_account_id}/workspaces.
type AddServiceAccountToWorkspaceRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceRole string `json:"workspace_role"`
}

// UpdateServiceAccountWorkspaceRequest is the request body for
// POST /v1/organizations/workspaces/{workspace_id}/service_accounts/{service_account_id}.
type UpdateServiceAccountWorkspaceRequest struct {
	WorkspaceRole string `json:"workspace_role"`
}

// FederationIssuerJWKS configures how signing keys are fetched for an OIDC federation issuer.
type FederationIssuerJWKS struct {
	Type          string           `json:"type"`
	DiscoveryBase *string          `json:"discovery_base,omitempty"`
	URL           *string          `json:"url,omitempty"`
	CACertPEM     *string          `json:"ca_cert_pem,omitempty"`
	Keys          []map[string]any `json:"keys,omitempty"`
}

// FederationIssuer represents an OIDC identity provider from /v1/organizations/federation_issuers.
type FederationIssuer struct {
	ID                    string               `json:"id"`
	Name                  string               `json:"name"`
	IssuerURL             string               `json:"issuer_url"`
	CheckJTI              *bool                `json:"check_jti"`
	MaxJWTLifetimeSeconds *int64               `json:"max_jwt_lifetime_seconds"`
	JWKS                  FederationIssuerJWKS `json:"jwks"`
	CreatedAt             string               `json:"created_at"`
	UpdatedAt             string               `json:"updated_at"`
	ArchivedAt            *string              `json:"archived_at"`
	Type                  string               `json:"type"`
}

// CreateFederationIssuerRequest is the request body for POST /v1/organizations/federation_issuers.
type CreateFederationIssuerRequest struct {
	Name                  string               `json:"name"`
	IssuerURL             string               `json:"issuer_url"`
	CheckJTI              *bool                `json:"check_jti,omitempty"`
	MaxJWTLifetimeSeconds *int64               `json:"max_jwt_lifetime_seconds,omitempty"`
	JWKS                  FederationIssuerJWKS `json:"jwks"`
}

// UpdateFederationIssuerRequest is the request body for POST /v1/organizations/federation_issuers/{id}.
type UpdateFederationIssuerRequest struct {
	Name                  string                `json:"name,omitempty"`
	IssuerURL             string                `json:"issuer_url,omitempty"`
	CheckJTI              *bool                 `json:"check_jti,omitempty"`
	MaxJWTLifetimeSeconds *int64                `json:"max_jwt_lifetime_seconds,omitempty"`
	JWKS                  *FederationIssuerJWKS `json:"jwks,omitempty"`
}

// FederationRuleMatch defines JWT claim matching conditions for a federation rule.
type FederationRuleMatch struct {
	Audience      *string           `json:"audience,omitempty"`
	SubjectPrefix *string           `json:"subject_prefix,omitempty"`
	Claims        map[string]string `json:"claims,omitempty"`
	Condition     *string           `json:"condition,omitempty"`
}

// FederationRule represents a WIF rule from /v1/organizations/federation_rules.
type FederationRule struct {
	ID                     string               `json:"id"`
	Name                   string               `json:"name"`
	Description            string               `json:"description"`
	IssuerID               string               `json:"issuer_id"`
	Target                 FederationRuleTarget `json:"target"`
	OAuthScope             string               `json:"oauth_scope"`
	TokenLifetimeSeconds   int64                `json:"token_lifetime_seconds"`
	AppliesToAllWorkspaces bool                 `json:"applies_to_all_workspaces"`
	WorkspaceIDs           []string             `json:"workspace_ids"`
	Match                  FederationRuleMatch  `json:"match"`
	CreatedAt              string               `json:"created_at"`
	UpdatedAt              string               `json:"updated_at"`
	ArchivedAt             *string              `json:"archived_at"`
	Type                   string               `json:"type"`
}

// FederationRuleTarget is the identity tokens minted via a rule act as. The API only supports service_account.
type FederationRuleTarget struct {
	Type             string `json:"type"`
	ServiceAccountID string `json:"service_account_id"`
}

// ServiceAccountTarget builds a service_account rule target.
func ServiceAccountTarget(serviceAccountID string) FederationRuleTarget {
	return FederationRuleTarget{Type: "service_account", ServiceAccountID: serviceAccountID}
}

// CreateFederationRuleRequest is the request body for POST /v1/organizations/federation_rules (one workspace max).
type CreateFederationRuleRequest struct {
	Name                   string               `json:"name"`
	Description            *string              `json:"description,omitempty"`
	IssuerID               string               `json:"issuer_id"`
	Target                 FederationRuleTarget `json:"target"`
	OAuthScope             string               `json:"oauth_scope"`
	TokenLifetimeSeconds   *int64               `json:"token_lifetime_seconds,omitempty"`
	AppliesToAllWorkspaces bool                 `json:"applies_to_all_workspaces"`
	WorkspaceID            string               `json:"workspace_id,omitempty"`
	Match                  FederationRuleMatch  `json:"match"`
}

// UpdateFederationRuleRequest is the request body for POST /v1/organizations/federation_rules/{id}.
// Description has no omitempty because the API clears it on null.
type UpdateFederationRuleRequest struct {
	Name                   string                `json:"name,omitempty"`
	Description            *string               `json:"description"`
	Target                 *FederationRuleTarget `json:"target,omitempty"`
	OAuthScope             string                `json:"oauth_scope,omitempty"`
	TokenLifetimeSeconds   *int64                `json:"token_lifetime_seconds,omitempty"`
	AppliesToAllWorkspaces *bool                 `json:"applies_to_all_workspaces,omitempty"`
	Match                  *FederationRuleMatch  `json:"match,omitempty"`
}

// AddFederationRuleWorkspaceRequest is the request body for POST /v1/organizations/federation_rules/{id}/workspaces.
type AddFederationRuleWorkspaceRequest struct {
	WorkspaceID string `json:"workspace_id"`
}
