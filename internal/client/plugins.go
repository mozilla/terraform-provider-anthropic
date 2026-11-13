// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

const (
	BetaPlugins = "ce-plugins-2026-09-01"

	// The API answers concurrent installation-setting writes on one plugin with a 503
	// that is safe to repeat. Do does not retry POST, so the set call retries itself.
	pluginSettingWriteAttempts = 4
	pluginSettingWriteBackoff  = 2 * time.Second
)

// PluginOwner is the organization, or the member whose personal marketplace holds the item.
type PluginOwner struct {
	Type   string  `json:"type"`
	UserID *string `json:"user_id,omitempty"`
}

// PluginTarget names who a setting or share applies to.
type PluginTarget struct {
	Type        string  `json:"type"`
	RBACGroupID *string `json:"rbac_group_id,omitempty"`
	UserID      *string `json:"user_id,omitempty"`
}

type PluginMarketplace struct {
	ID                            string      `json:"id"`
	Type                          string      `json:"type"`
	Name                          string      `json:"name"`
	Source                        string      `json:"source"`
	Owner                         PluginOwner `json:"owner"`
	DefaultInstallationPreference *string     `json:"default_installation_preference"`
	SyncStatus                    *string     `json:"sync_status"`
	LastSyncEndedAt               *string     `json:"last_sync_ended_at"`
	LastSyncReadSha               *string     `json:"last_sync_read_sha"`
	CreatedAt                     string      `json:"created_at"`
}

type PluginComponent struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description *string `json:"description"`
}

type PluginContentScan struct {
	Status     string  `json:"status"`
	Assessment *string `json:"assessment"`
	Reason     *string `json:"reason"`
}

type Plugin struct {
	ID                                          string             `json:"id"`
	Type                                        string             `json:"type"`
	Name                                        string             `json:"name"`
	DisplayName                                 *string            `json:"display_name"`
	Description                                 *string            `json:"description"`
	MarketplaceID                               string             `json:"marketplace_id"`
	Owner                                       PluginOwner        `json:"owner"`
	ManifestVersion                             *string            `json:"manifest_version"`
	Reach                                       *string            `json:"reach"`
	LatestVersionID                             string             `json:"latest_version_id"`
	ServedVersionID                             string             `json:"served_version_id"`
	ServedVersionPinned                         bool               `json:"served_version_pinned"`
	OrganizationInstallationPreference          *string            `json:"organization_installation_preference"`
	OrganizationInstallationPreferenceInherited *bool              `json:"organization_installation_preference_inherited"`
	Components                                  []PluginComponent  `json:"components"`
	ContentScan                                 *PluginContentScan `json:"content_scan"`
	CreatedAt                                   string             `json:"created_at"`
	UpdatedAt                                   string             `json:"updated_at"`
}

type PluginVersion struct {
	ID              string             `json:"id"`
	Type            string             `json:"type"`
	PluginID        string             `json:"plugin_id"`
	DisplayName     *string            `json:"display_name"`
	Description     *string            `json:"description"`
	ManifestVersion *string            `json:"manifest_version"`
	Reach           *string            `json:"reach"`
	ReleaseNotes    *string            `json:"release_notes"`
	Components      []PluginComponent  `json:"components"`
	ContentScan     *PluginContentScan `json:"content_scan"`
	CreatedAt       string             `json:"created_at"`
}

type PluginInstallationSetting struct {
	Type                   string       `json:"type"`
	PluginID               string       `json:"plugin_id"`
	Target                 PluginTarget `json:"target"`
	InstallationPreference string       `json:"installation_preference"`
	CreatedAt              string       `json:"created_at"`
	UpdatedAt              string       `json:"updated_at"`
}

// Matches reports whether the setting belongs to target, which is "organization" or an RBAC Group ID.
func (s PluginInstallationSetting) Matches(target string) bool {
	if target == "organization" {
		return s.Target.Type == "organization"
	}
	return s.Target.Type == "rbac_group" && s.Target.RBACGroupID != nil && *s.Target.RBACGroupID == target
}

type PluginShare struct {
	Type      string       `json:"type"`
	PluginID  string       `json:"plugin_id"`
	Target    PluginTarget `json:"target"`
	GrantedAt string       `json:"granted_at"`
}

func (c *Client) GetPluginMarketplace(ctx context.Context, marketplaceID string) (*PluginMarketplace, error) {
	p, err := Path("plugin_marketplaces", marketplaceID)
	if err != nil {
		return nil, err
	}
	var out PluginMarketplace
	if err := c.Do(ctx, http.MethodGet, p, nil, &out, WithBeta(BetaPlugins)); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePluginMarketplace sets the marketplace's default installation preference (POST /v1/organizations/plugin_marketplaces/{id}).
func (c *Client) UpdatePluginMarketplace(ctx context.Context, marketplaceID, defaultInstallationPreference string) (*PluginMarketplace, error) {
	p, err := Path("plugin_marketplaces", marketplaceID)
	if err != nil {
		return nil, err
	}
	body := map[string]string{"default_installation_preference": defaultInstallationPreference}
	var out PluginMarketplace
	if err := c.Do(ctx, http.MethodPost, p, body, &out, WithBeta(BetaPlugins)); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPluginMarketplaces lists marketplaces. query may hold owner_type and source filters.
func (c *Client) ListPluginMarketplaces(ctx context.Context, query url.Values) ([]PluginMarketplace, error) {
	p, err := Path("plugin_marketplaces")
	if err != nil {
		return nil, err
	}
	return ListAll[PluginMarketplace](ctx, c, p, query, WithBeta(BetaPlugins))
}

func (c *Client) GetPlugin(ctx context.Context, pluginID string) (*Plugin, error) {
	p, err := Path("plugins", pluginID)
	if err != nil {
		return nil, err
	}
	var out Plugin
	if err := c.Do(ctx, http.MethodGet, p, nil, &out, WithBeta(BetaPlugins)); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPlugins lists plugins. query may hold marketplace_id, owner_type and owner_user_id filters.
func (c *Client) ListPlugins(ctx context.Context, query url.Values) ([]Plugin, error) {
	p, err := Path("plugins")
	if err != nil {
		return nil, err
	}
	return ListAll[Plugin](ctx, c, p, query, WithBeta(BetaPlugins))
}

func (c *Client) ListPluginVersions(ctx context.Context, pluginID string) ([]PluginVersion, error) {
	p, err := Path("plugins", pluginID, "versions")
	if err != nil {
		return nil, err
	}
	return ListAll[PluginVersion](ctx, c, p, nil, WithBeta(BetaPlugins))
}

func (c *Client) ListPluginShares(ctx context.Context, pluginID string, query url.Values) ([]PluginShare, error) {
	p, err := Path("plugins", pluginID, "shares")
	if err != nil {
		return nil, err
	}
	return ListAll[PluginShare](ctx, c, p, query, WithBeta(BetaPlugins))
}

func (c *Client) ListPluginInstallationSettings(ctx context.Context, pluginID string) ([]PluginInstallationSetting, error) {
	p, err := Path("plugins", pluginID, "installation_settings")
	if err != nil {
		return nil, err
	}
	return ListAll[PluginInstallationSetting](ctx, c, p, nil, WithBeta(BetaPlugins))
}

// GetPluginInstallationSetting finds one target's setting by listing, since the API has no single-get endpoint.
// It returns a 404 APIError when the target holds no setting of its own.
func (c *Client) GetPluginInstallationSetting(ctx context.Context, pluginID, target string) (*PluginInstallationSetting, error) {
	settings, err := c.ListPluginInstallationSettings(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	for i := range settings {
		if settings[i].Matches(target) {
			return &settings[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Type: "not_found_error", Message: "plugin installation setting not found"}
}

// SetPluginInstallationSetting sets a target's setting (POST /v1/organizations/plugins/{id}/installation_settings/{target}).
func (c *Client) SetPluginInstallationSetting(ctx context.Context, pluginID, target, installationPreference string) (*PluginInstallationSetting, error) {
	p, err := Path("plugins", pluginID, "installation_settings", target)
	if err != nil {
		return nil, err
	}
	body := map[string]string{"installation_preference": installationPreference}
	for attempt := 1; ; attempt++ {
		var out PluginInstallationSetting
		err = c.Do(ctx, http.MethodPost, p, body, &out, WithBeta(BetaPlugins))
		if err == nil {
			return &out, nil
		}
		if StatusCode(err) != http.StatusServiceUnavailable || attempt >= pluginSettingWriteAttempts {
			return nil, err
		}
		timer := time.NewTimer(pluginSettingWriteBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// RemovePluginInstallationSetting removes a target's own setting (DELETE /v1/organizations/plugins/{id}/installation_settings/{target}).
func (c *Client) RemovePluginInstallationSetting(ctx context.Context, pluginID, target string) error {
	p, err := Path("plugins", pluginID, "installation_settings", target)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaPlugins))
}
