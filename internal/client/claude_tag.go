// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// BetaClaudeTag is sent by every Claude Tag (Claude in Slack) endpoint.
	BetaClaudeTag = "experimental-claude-tag-2026-10-21"
)

// ClaudeTagBundle is a named set of access created and edited in claude.ai.
type ClaudeTagBundle struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	Type        string  `json:"type"`
}

// ClaudeTagBundleTarget is one Bundle attached to one Slack channel or workspace.
// Exactly one of SlackChannelID or SlackWorkspaceID is set, depending on Type.
type ClaudeTagBundleTarget struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	SlackChannelID   string `json:"slack_channel_id,omitempty"`
	SlackWorkspaceID string `json:"slack_workspace_id,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
}

// CreateClaudeTagBundleTargetRequest is the create body: type plus exactly one of the two Slack IDs.
type CreateClaudeTagBundleTargetRequest struct {
	Type             string `json:"type"`
	SlackChannelID   string `json:"slack_channel_id,omitempty"`
	SlackWorkspaceID string `json:"slack_workspace_id,omitempty"`
}

// ClaudeTagTypeRef is the {"type": "..."} shape of guest_policy and reply_mode.
type ClaudeTagTypeRef struct {
	Type string `json:"type"`
}

// ClaudeTagSettings mirrors the settings object. Nil means the level does not store the setting.
type ClaudeTagSettings struct {
	Enabled       *bool             `json:"enabled"`
	EnvironmentID *string           `json:"environment_id"`
	GuestPolicy   *ClaudeTagTypeRef `json:"guest_policy"`
	Instructions  *string           `json:"instructions"`
	Model         *string           `json:"model"`
	ReplyMode     *ClaudeTagTypeRef `json:"reply_mode,omitempty"`
}

// ClaudeTagSlackIdentifiers holds Slack's own identifiers (T..., C...).
type ClaudeTagSlackIdentifiers struct {
	WorkspaceID string `json:"workspace_id"`
	ChannelID   string `json:"channel_id,omitempty"`
}

// ClaudeTagSlackChannel is a Slack channel set up for Claude Tag.
type ClaudeTagSlackChannel struct {
	ID        string                    `json:"id"`
	Slack     ClaudeTagSlackIdentifiers `json:"slack"`
	Settings  ClaudeTagSettings         `json:"settings"`
	CreatedAt string                    `json:"created_at"`
	UpdatedAt string                    `json:"updated_at"`
	Type      string                    `json:"type"`
}

// ClaudeTagSlackWorkspace is a Slack workspace set up for Claude Tag.
type ClaudeTagSlackWorkspace = ClaudeTagSlackChannel

// CreateClaudeTagSlackChannelRequest is the body for POST /claude_tag/slack_channels.
type CreateClaudeTagSlackChannelRequest struct {
	Slack ClaudeTagSlackIdentifiers `json:"slack"`
}

// UpdateClaudeTagSettingsRequest is the body for updating a channel's or workspace's settings.
// Settings fields encode null explicitly, which clears them on the server.
type UpdateClaudeTagSettingsRequest struct {
	Settings ClaudeTagSettings `json:"settings"`
}

func includeInheritedQuery(includeInherited bool) url.Values {
	q := url.Values{}
	if includeInherited {
		q.Set("include_inherited", strconv.FormatBool(true))
	}
	return q
}

// GetClaudeTagBundle retrieves a Bundle (GET /v1/organizations/claude_tag/bundles/{id}).
func (c *Client) GetClaudeTagBundle(ctx context.Context, bundleID string) (*ClaudeTagBundle, error) {
	p, err := Path("claude_tag", "bundles", bundleID)
	if err != nil {
		return nil, err
	}
	var b ClaudeTagBundle
	if err := c.Do(ctx, http.MethodGet, p, nil, &b, WithBeta(BetaClaudeTag)); err != nil {
		return nil, err
	}
	return &b, nil
}

// ListClaudeTagBundles lists Bundles (GET /v1/organizations/claude_tag/bundles).
func (c *Client) ListClaudeTagBundles(ctx context.Context) ([]ClaudeTagBundle, error) {
	p, err := Path("claude_tag", "bundles")
	if err != nil {
		return nil, err
	}
	return ListAll[ClaudeTagBundle](ctx, c, p, nil, WithBeta(BetaClaudeTag))
}

// CreateClaudeTagBundleTarget attaches a Bundle (POST /v1/organizations/claude_tag/bundles/{id}/targets).
func (c *Client) CreateClaudeTagBundleTarget(ctx context.Context, bundleID string, target CreateClaudeTagBundleTargetRequest) (*ClaudeTagBundleTarget, error) {
	p, err := Path("claude_tag", "bundles", bundleID, "targets")
	if err != nil {
		return nil, err
	}
	var t ClaudeTagBundleTarget
	if err := c.Do(ctx, http.MethodPost, p, target, &t, WithBeta(BetaClaudeTag)); err != nil {
		return nil, err
	}
	return &t, nil
}

// GetClaudeTagBundleTarget retrieves a Bundle Target.
func (c *Client) GetClaudeTagBundleTarget(ctx context.Context, bundleID, targetID string) (*ClaudeTagBundleTarget, error) {
	p, err := Path("claude_tag", "bundles", bundleID, "targets", targetID)
	if err != nil {
		return nil, err
	}
	var t ClaudeTagBundleTarget
	if err := c.Do(ctx, http.MethodGet, p, nil, &t, WithBeta(BetaClaudeTag)); err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteClaudeTagBundleTarget detaches a Bundle.
func (c *Client) DeleteClaudeTagBundleTarget(ctx context.Context, bundleID, targetID string) error {
	p, err := Path("claude_tag", "bundles", bundleID, "targets", targetID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaClaudeTag))
}

// CreateClaudeTagSlackChannel sets a Slack channel up, or returns it if already set up.
func (c *Client) CreateClaudeTagSlackChannel(ctx context.Context, req CreateClaudeTagSlackChannelRequest) (*ClaudeTagSlackChannel, error) {
	p, err := Path("claude_tag", "slack_channels")
	if err != nil {
		return nil, err
	}
	var ch ClaudeTagSlackChannel
	if err := c.Do(ctx, http.MethodPost, p, req, &ch, WithBeta(BetaClaudeTag)); err != nil {
		return nil, err
	}
	return &ch, nil
}

// GetClaudeTagSlackChannel retrieves a Slack channel and its settings.
func (c *Client) GetClaudeTagSlackChannel(ctx context.Context, id string, includeInherited bool) (*ClaudeTagSlackChannel, error) {
	p, err := Path("claude_tag", "slack_channels", id)
	if err != nil {
		return nil, err
	}
	var ch ClaudeTagSlackChannel
	if err := c.Do(ctx, http.MethodGet, p, nil, &ch, WithBeta(BetaClaudeTag), WithQuery(includeInheritedQuery(includeInherited))); err != nil {
		return nil, err
	}
	return &ch, nil
}

// UpdateClaudeTagSlackChannel changes the settings a Slack channel stores.
func (c *Client) UpdateClaudeTagSlackChannel(ctx context.Context, id string, req UpdateClaudeTagSettingsRequest) (*ClaudeTagSlackChannel, error) {
	p, err := Path("claude_tag", "slack_channels", id)
	if err != nil {
		return nil, err
	}
	var ch ClaudeTagSlackChannel
	if err := c.Do(ctx, http.MethodPost, p, req, &ch, WithBeta(BetaClaudeTag)); err != nil {
		return nil, err
	}
	return &ch, nil
}

// ListClaudeTagSlackChannels lists set-up channels of one Slack workspace (Slack's T... identifier).
// slackChannelID, when set, narrows the result to that Slack channel.
func (c *Client) ListClaudeTagSlackChannels(ctx context.Context, slackWorkspaceID, slackChannelID string, includeInherited bool) ([]ClaudeTagSlackChannel, error) {
	p, err := Path("claude_tag", "slack_channels")
	if err != nil {
		return nil, err
	}
	q := includeInheritedQuery(includeInherited)
	q.Set("slack_workspace_id", slackWorkspaceID)
	if slackChannelID != "" {
		q.Set("slack_channel_id", slackChannelID)
	}
	return ListAll[ClaudeTagSlackChannel](ctx, c, p, q, WithBeta(BetaClaudeTag))
}

// GetClaudeTagSlackWorkspace retrieves a Slack workspace and its settings.
func (c *Client) GetClaudeTagSlackWorkspace(ctx context.Context, id string, includeInherited bool) (*ClaudeTagSlackWorkspace, error) {
	p, err := Path("claude_tag", "slack_workspaces", id)
	if err != nil {
		return nil, err
	}
	var ws ClaudeTagSlackWorkspace
	if err := c.Do(ctx, http.MethodGet, p, nil, &ws, WithBeta(BetaClaudeTag), WithQuery(includeInheritedQuery(includeInherited))); err != nil {
		return nil, err
	}
	return &ws, nil
}

// ListClaudeTagSlackWorkspaces lists set-up Slack workspaces. slackWorkspaceID (T...), when set,
// narrows the result to that workspace.
func (c *Client) ListClaudeTagSlackWorkspaces(ctx context.Context, slackWorkspaceID string, includeInherited bool) ([]ClaudeTagSlackWorkspace, error) {
	p, err := Path("claude_tag", "slack_workspaces")
	if err != nil {
		return nil, err
	}
	q := includeInheritedQuery(includeInherited)
	if slackWorkspaceID != "" {
		q.Set("slack_workspace_id", slackWorkspaceID)
	}
	return ListAll[ClaudeTagSlackWorkspace](ctx, c, p, q, WithBeta(BetaClaudeTag))
}
