// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"context"
	"net/http"
	"net/url"
)

// LimitActor is the member or scoped API key an effective spend limit row or increase request belongs to.
type LimitActor struct {
	Type           string `json:"type"`
	UserID         string `json:"user_id,omitempty"`
	ScopedAPIKeyID string `json:"scoped_api_key_id,omitempty"`
}

// SpendLimitSource is the scope an effective spend limit was inherited from.
type SpendLimitSource struct {
	Type        string `json:"type"`
	UserID      string `json:"user_id,omitempty"`
	SeatTier    string `json:"seat_tier,omitempty"`
	RBACGroupID string `json:"rbac_group_id,omitempty"`
	Service     string `json:"service,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// SpendSummary is one (member, period) row from GET /v1/organizations/spend_limits/effective.
type SpendSummary struct {
	Actor             LimitActor       `json:"actor"`
	Amount            *string          `json:"amount"`
	Currency          string           `json:"currency"`
	Period            string           `json:"period"`
	PeriodToDateSpend string           `json:"period_to_date_spend"`
	Source            SpendLimitSource `json:"source"`
	SpendLimitID      *string          `json:"spend_limit_id"`
}

// ListEffectiveSpendLimits lists each member's effective spend limit (Claude Enterprise only).
func (c *Client) ListEffectiveSpendLimits(ctx context.Context, userIDs, periods []string) ([]SpendSummary, error) {
	p, err := Path("spend_limits", "effective")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, v := range userIDs {
		q.Add("user_ids[]", v)
	}
	for _, v := range periods {
		q.Add("period[]", v)
	}
	return ListAll[SpendSummary](ctx, c, p, q)
}

// SpendLimitIncreaseRequest is a member's request for a higher spend limit.
type SpendLimitIncreaseRequest struct {
	ID         string      `json:"id"`
	Actor      LimitActor  `json:"actor"`
	ResolvedBy *LimitActor `json:"resolved_by"`
	Period     string      `json:"period"`
	Status     string      `json:"status"`
	CreatedAt  string      `json:"created_at"`
	ResolvedAt *string     `json:"resolved_at"`
}

// ListSpendLimitIncreaseRequests lists spend limit increase requests, most recent first.
func (c *Client) ListSpendLimitIncreaseRequests(ctx context.Context, actorIDs, statuses []string) ([]SpendLimitIncreaseRequest, error) {
	p, err := Path("spend_limit_increase_requests")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, v := range actorIDs {
		q.Add("actor_ids[]", v)
	}
	for _, v := range statuses {
		q.Add("status[]", v)
	}
	return ListAll[SpendLimitIncreaseRequest](ctx, c, p, q)
}

// RateLimitGroup is the model family or API surface a rate limit entry applies to.
type RateLimitGroup struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name,omitempty"`
}

// RateLimitValue is one limiter value. OrgLimit and Source are only set on workspace entries.
type RateLimitValue struct {
	Type     string `json:"type"`
	Value    int64  `json:"value"`
	OrgLimit *int64 `json:"org_limit,omitempty"`
	Source   *struct {
		Type string `json:"type"`
	} `json:"source,omitempty"`
}

// OrganizationRateLimit is an entry from GET /v1/organizations/rate_limits.
type OrganizationRateLimit struct {
	ID     string           `json:"id"`
	Group  RateLimitGroup   `json:"group"`
	Limits []RateLimitValue `json:"limits"`
	Models []string         `json:"models"`
	Type   string           `json:"type"`
}

// ListOrganizationRateLimits lists the organization's Messages API rate limits.
func (c *Client) ListOrganizationRateLimits(ctx context.Context, groupType, model string) ([]OrganizationRateLimit, error) {
	p, err := Path("rate_limits")
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if groupType != "" {
		q.Set("group_type", groupType)
	}
	if model != "" {
		q.Set("model", model)
	}
	return ListAll[OrganizationRateLimit](ctx, c, p, q)
}

// WorkspaceRateLimit is a workspace's override entry for one rate limit group.
type WorkspaceRateLimit struct {
	RateLimitID string           `json:"rate_limit_id"`
	WorkspaceID string           `json:"workspace_id"`
	Group       RateLimitGroup   `json:"group"`
	Limits      []RateLimitValue `json:"limits"`
	Models      []string         `json:"models"`
	Type        string           `json:"type"`
}

// WorkspaceRateLimitUpdate sets (Value non-nil) or clears (Value nil) one limiter type's override.
type WorkspaceRateLimitUpdate struct {
	Type  string `json:"type"`
	Value *int64 `json:"value"`
}

// ListWorkspaceRateLimits lists the workspace-level overrides (not inherited values) for a workspace.
func (c *Client) ListWorkspaceRateLimits(ctx context.Context, workspaceID string) ([]WorkspaceRateLimit, error) {
	p, err := Path("workspaces", workspaceID, "rate_limits")
	if err != nil {
		return nil, err
	}
	return ListAll[WorkspaceRateLimit](ctx, c, p, nil)
}

// UpdateWorkspaceRateLimit sets or clears overrides for one rate limit group in a workspace.
// Limiter types not named in limits keep their current value.
func (c *Client) UpdateWorkspaceRateLimit(ctx context.Context, workspaceID, rateLimitID string, limits []WorkspaceRateLimitUpdate) (*WorkspaceRateLimit, error) {
	p, err := Path("workspaces", workspaceID, "rate_limits", rateLimitID)
	if err != nil {
		return nil, err
	}
	var out WorkspaceRateLimit
	body := struct {
		Limits []WorkspaceRateLimitUpdate `json:"limits"`
	}{limits}
	if err := c.Do(ctx, http.MethodPost, p, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
