// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	DefaultBaseURL          = "https://api.anthropic.com"
	MaxRetryAfter           = 60 * time.Second
	DefaultAnthropicVersion = "2023-06-01"
	DefaultMaxRetries       = 4
	DefaultTimeout          = 60 * time.Second
	DefaultBaseBackoff      = 250 * time.Millisecond
	DefaultMemberCacheTTL   = 5 * time.Second
	MaxResponseBodyBytes    = 4 << 20 // 4 MiB

	BetaCEUserManagement = "ce-user-management-2026-07-13"
	BetaSpendLimitReads  = "spend-limit-reads-2026-09-26"
)

// AuthMode identifies how the Anthropic client authenticates requests.
type AuthMode string

const (
	AuthModeAPIKey     AuthMode = "api_key"
	AuthModeOAuth      AuthMode = "oauth_token"
	AuthModeFederation AuthMode = "federation"
)

// FederationConfig configures Workload Identity Federation (WIF) token exchange
// via github.com/anthropics/anthropic-sdk-go/option.WithFederationTokenProvider.
type FederationConfig struct {
	FederationRuleID  string
	OrganizationID    string
	ServiceAccountID  string
	WorkspaceID       string
	IdentityToken     string
	IdentityTokenFile string
	TokenProvider     option.IdentityTokenFunc
}

// Config holds configuration parameters for constructing a Client.
type Config struct {
	APIKey           string
	OAuthToken       string
	Federation       *FederationConfig
	BaseURL          string
	AnthropicVersion string
	MaxRetries       int
	BaseBackoff      time.Duration
	MemberCacheTTL   time.Duration
	HTTPClient       *http.Client
	UserAgent        string
}

type cachedGroupMembers struct {
	members   []RBACGroupMember
	expiresAt time.Time
}

type inflightMemberCall struct {
	done    chan struct{}
	members []RBACGroupMember
	err     error
}

// Client is a thread-safe client for the Anthropic Admin API (Console, Enterprise, and WIF),
// backed by the official Anthropic Go SDK (github.com/anthropics/anthropic-sdk-go).
type Client struct {
	sdk              anthropic.Client
	authMode         AuthMode
	baseURL          string
	anthropicVersion string
	maxRetries       int
	baseBackoff      time.Duration
	memberCacheTTL   time.Duration
	httpClient       *http.Client
	userAgent        string

	memberCacheMu  sync.Mutex
	memberCache    map[string]cachedGroupMembers
	memberInflight map[string]*inflightMemberCall
}

// New creates a new hardened Anthropic Admin API client backed by anthropic.NewClient.
func New(cfg Config) (*Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	oauthToken := strings.TrimSpace(cfg.OAuthToken)
	hasFederation := cfg.Federation != nil

	modeCount := 0
	if apiKey != "" {
		modeCount++
	}
	if oauthToken != "" {
		modeCount++
	}
	if hasFederation {
		modeCount++
	}
	if modeCount == 0 {
		return nil, fmt.Errorf("one of api_key, oauth_token, or federation must be configured")
	}
	if modeCount > 1 {
		return nil, fmt.Errorf("only one of api_key, oauth_token, or federation may be configured at a time")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil || parsedBase.Host == "" {
		return nil, fmt.Errorf("invalid base_url %q: must be an absolute HTTPS URL", baseURL)
	}
	// Credentials are sent to base_url, so plain HTTP is only allowed for local test servers.
	if parsedBase.Scheme != "https" && !(parsedBase.Scheme == "http" && isLoopbackHost(parsedBase.Hostname())) {
		return nil, fmt.Errorf("invalid base_url %q: must use https", baseURL)
	}

	version := strings.TrimSpace(cfg.AnthropicVersion)
	if version == "" {
		version = DefaultAnthropicVersion
	}

	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	baseBackoff := cfg.BaseBackoff
	if baseBackoff <= 0 {
		baseBackoff = DefaultBaseBackoff
	}

	memberCacheTTL := cfg.MemberCacheTTL
	if memberCacheTTL <= 0 {
		memberCacheTTL = DefaultMemberCacheTTL
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: DefaultTimeout,
		}
	}

	userAgent := strings.TrimSpace(cfg.UserAgent)
	if userAgent == "" {
		userAgent = "terraform-provider-anthropic"
	}

	sdkOpts := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0), // Managed by Client.Do for Retry-After + 529 + configurable backoff
		option.WithHeader("anthropic-version", version),
		option.WithHeader("Accept", "application/json"),
		option.WithHeader("User-Agent", userAgent),
	}

	var authMode AuthMode
	switch {
	case apiKey != "":
		authMode = AuthModeAPIKey
		sdkOpts = append(sdkOpts,
			option.WithAPIKey(apiKey),
			option.WithHeaderDel("Authorization"),
		)
	case oauthToken != "":
		authMode = AuthModeOAuth
		sdkOpts = append(sdkOpts,
			option.WithAuthToken(oauthToken),
			option.WithHeaderDel("x-api-key"),
		)
	case hasFederation:
		authMode = AuthModeFederation
		fed := cfg.Federation
		ruleID := strings.TrimSpace(fed.FederationRuleID)
		orgID := strings.TrimSpace(fed.OrganizationID)
		if ruleID == "" {
			return nil, fmt.Errorf("federation.federation_rule_id is required")
		}
		if orgID == "" {
			return nil, fmt.Errorf("federation.organization_id is required")
		}

		var provider option.IdentityTokenFunc
		switch {
		case fed.TokenProvider != nil:
			provider = fed.TokenProvider
		case strings.TrimSpace(fed.IdentityTokenFile) != "":
			if strings.TrimSpace(fed.IdentityToken) != "" {
				return nil, fmt.Errorf("only one of federation.identity_token or federation.identity_token_file may be set")
			}
			provider = option.IdentityTokenFile(strings.TrimSpace(fed.IdentityTokenFile))
		case strings.TrimSpace(fed.IdentityToken) != "":
			staticToken := strings.TrimSpace(fed.IdentityToken)
			provider = func(_ context.Context) (string, error) {
				return staticToken, nil
			}
		default:
			return nil, fmt.Errorf("either federation.identity_token or federation.identity_token_file is required")
		}

		sdkOpts = append(sdkOpts,
			option.WithFederationTokenProvider(provider, option.FederationOptions{
				FederationRuleID: ruleID,
				OrganizationID:   orgID,
				ServiceAccountID: strings.TrimSpace(fed.ServiceAccountID),
				WorkspaceID:      strings.TrimSpace(fed.WorkspaceID),
			}),
			option.WithHeaderDel("x-api-key"),
		)
	}

	sdkClient := anthropic.NewClient(sdkOpts...)

	return &Client{
		sdk:              sdkClient,
		authMode:         authMode,
		baseURL:          baseURL,
		anthropicVersion: version,
		maxRetries:       maxRetries,
		baseBackoff:      baseBackoff,
		memberCacheTTL:   memberCacheTTL,
		httpClient:       httpClient,
		userAgent:        userAgent,
		memberCache:      make(map[string]cachedGroupMembers),
		memberInflight:   make(map[string]*inflightMemberCall),
	}, nil
}

// SDK returns a pointer to the underlying official Anthropic Go SDK client.
func (c *Client) SDK() *anthropic.Client {
	return &c.sdk
}

// AuthMode returns the active authentication mode (api_key, oauth_token, or federation).
func (c *Client) AuthMode() AuthMode {
	return c.authMode
}

// requireWIFCapableAuth verifies that the client is authenticated via OAuth or Workload Identity
// Federation before calling endpoints that reject static Admin API keys (x-api-key).
func (c *Client) requireWIFCapableAuth(endpointDesc string) error {
	if c.authMode == AuthModeAPIKey {
		return &APIError{
			StatusCode: http.StatusUnauthorized,
			Type:       "authentication_error",
			Message: fmt.Sprintf(
				"%s requires an org:admin OAuth access token (oauth_token / ANTHROPIC_OAUTH_TOKEN) or Workload Identity Federation (federation block); static Admin API keys (x-api-key) are rejected by this endpoint",
				endpointDesc,
			),
		}
	}
	return nil
}

// Path safely constructs an "/v1/organizations/..." URL path by validating that every
// segment is non-empty and escaping each segment with url.PathEscape.
// It also rejects "." and ".." path traversal segments explicitly.
func Path(segments ...string) (string, error) {
	if len(segments) == 0 {
		return "/v1/organizations", nil
	}
	escaped := make([]string, 0, len(segments))
	for i, seg := range segments {
		if seg == "" {
			return "", fmt.Errorf("URL path segment %d cannot be empty", i)
		}
		if seg == "." || seg == ".." || strings.Contains(seg, "/") || strings.Contains(seg, "\\") {
			return "", fmt.Errorf("URL path segment %q contains invalid path traversal or separator characters", seg)
		}
		escaped = append(escaped, url.PathEscape(seg))
	}
	return "/v1/organizations/" + strings.Join(escaped, "/"), nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type requestConfig struct {
	headers http.Header
	query   url.Values
}

// RequestOption customizes an individual API request.
type RequestOption func(*requestConfig)

// WithQuery merges query parameters into the request URL.
func WithQuery(q url.Values) RequestOption {
	return func(rc *requestConfig) {
		for k, vv := range q {
			rc.query.Del(k)
			for _, v := range vv {
				rc.query.Add(k, v)
			}
		}
	}
}

// WithBeta attaches an anthropic-beta header (if non-empty) and sets the ?beta=true query parameter.
func WithBeta(betaHeader string) RequestOption {
	return func(rc *requestConfig) {
		if betaHeader != "" {
			rc.headers.Add("anthropic-beta", betaHeader)
		}
		rc.query.Set("beta", "true")
	}
}

// Do executes an HTTP request through the official Anthropic Go SDK (c.sdk.Execute) with automatic
// retry/backoff on 429 rate-limit responses and transient 5xx/529 server errors.
func (c *Client) Do(ctx context.Context, method, path string, reqBody, respBody any, opts ...RequestOption) error {
	rc := &requestConfig{
		headers: make(http.Header),
		query:   make(url.Values),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(rc)
		}
	}

	var bodyBytes []byte
	if reqBody != nil {
		var err error
		bodyBytes, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	baseSDKOpts := make([]option.RequestOption, 0, len(rc.headers)+len(rc.query)+2)
	for k, vv := range rc.headers {
		for _, v := range vv {
			baseSDKOpts = append(baseSDKOpts, option.WithHeaderAdd(k, v))
		}
	}
	for k, vv := range rc.query {
		for _, v := range vv {
			baseSDKOpts = append(baseSDKOpts, option.WithQueryAdd(k, v))
		}
	}
	if bodyBytes != nil {
		baseSDKOpts = append(baseSDKOpts, option.WithRequestBody("application/json", bodyBytes))
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		var capturedStatus int
		var capturedHeaders http.Header
		var capturedBody []byte

		captureMiddleware := func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
			resp, err := next(req)
			if err != nil || resp == nil {
				return resp, err
			}
			capturedStatus = resp.StatusCode
			capturedHeaders = resp.Header.Clone()
			rawResp, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBodyBytes))
			_ = resp.Body.Close()
			if readErr != nil {
				return nil, fmt.Errorf("failed to read response body: %w", readErr)
			}
			capturedBody = rawResp
			resp.Body = io.NopCloser(bytes.NewReader(rawResp))
			return resp, nil
		}

		attemptOpts := make([]option.RequestOption, 0, len(baseSDKOpts)+1)
		attemptOpts = append(attemptOpts, baseSDKOpts...)
		attemptOpts = append(attemptOpts, option.WithMiddleware(captureMiddleware))

		err := c.sdk.Execute(ctx, method, path, nil, nil, attemptOpts...)
		if capturedStatus >= 200 && capturedStatus < 300 {
			if respBody != nil && len(bytes.TrimSpace(capturedBody)) > 0 {
				if decErr := json.Unmarshal(capturedBody, respBody); decErr != nil {
					return fmt.Errorf("failed to decode API response: %w", decErr)
				}
			}
			return nil
		}

		if capturedStatus == 0 && err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			if method != http.MethodPost && attempt < c.maxRetries && ctx.Err() == nil {
				if sleepErr := c.sleepBackoff(ctx, attempt, nil); sleepErr != nil {
					return lastErr
				}
				continue
			}
			return lastErr
		}

		apiErr := ParseAPIError(capturedStatus, capturedHeaders, capturedBody)
		var sdkErr *anthropic.Error
		if errors.As(err, &sdkErr) && apiErr.RequestID == "" && sdkErr.RequestID != "" {
			apiErr.RequestID = sdkErr.RequestID
		}
		lastErr = apiErr

		if shouldRetryStatus(method, capturedStatus) && attempt < c.maxRetries {
			if sleepErr := c.sleepBackoff(ctx, attempt, capturedHeaders); sleepErr != nil {
				return lastErr
			}
			continue
		}

		return apiErr
	}

	return lastErr
}

func shouldRetryStatus(method string, status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	// Do not retry non-idempotent POST requests on 5xx/529 responses because the server
	// may have already created the resource (workspaces, groups, service accounts, issuers, rules).
	if method == http.MethodPost {
		return false
	}
	switch status {
	case http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		529: // Anthropic Overloaded status
		return true
	default:
		return false
	}
}

func (c *Client) sleepBackoff(ctx context.Context, attempt int, headers http.Header) error {
	delay := c.computeBackoff(attempt, headers)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) computeBackoff(attempt int, headers http.Header) time.Duration {
	if headers != nil {
		if retryAfter := strings.TrimSpace(headers.Get("Retry-After")); retryAfter != "" {
			if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
				return min(time.Duration(secs)*time.Second, MaxRetryAfter)
			}
			if t, err := http.ParseTime(retryAfter); err == nil {
				if d := time.Until(t); d > 0 {
					return min(d, MaxRetryAfter)
				}
			}
		}
	}

	shift := min(attempt, 6)
	base := c.baseBackoff * time.Duration(1<<shift)
	// Add up to 25% jitter
	jitter := time.Duration(rand.Int64N(int64(base)/4 + 1))
	return base + jitter
}

// ============================================================================
// Organization & Users (Console + Enterprise)
// ============================================================================

// GetOrganization retrieves current organization metadata (GET /v1/organizations/me).
func (c *Client) GetOrganization(ctx context.Context) (*Organization, error) {
	p, err := Path("me")
	if err != nil {
		return nil, err
	}
	var org Organization
	if err := c.Do(ctx, http.MethodGet, p, nil, &org); err != nil {
		return nil, err
	}
	return &org, nil
}

// GetUser retrieves a single organization member by user ID (GET /v1/organizations/users/{user_id}).
func (c *Client) GetUser(ctx context.Context, userID string) (*User, error) {
	p, err := Path("users", userID)
	if err != nil {
		return nil, err
	}
	var u User
	if err := c.Do(ctx, http.MethodGet, p, nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// ListUsers lists organization members, optionally filtered by email (GET /v1/organizations/users).
func (c *Client) ListUsers(ctx context.Context, email string) ([]User, error) {
	p, err := Path("users")
	if err != nil {
		return nil, err
	}
	q := make(url.Values)
	if email != "" {
		q.Set("email", email)
	}
	return ListAll[User](ctx, c, p, q)
}

// UpdateUser updates an organization member's role (POST /v1/organizations/users/{user_id}).
func (c *Client) UpdateUser(ctx context.Context, userID, role string) (*User, error) {
	p, err := Path("users", userID)
	if err != nil {
		return nil, err
	}
	var u User
	if err := c.Do(ctx, http.MethodPost, p, UpdateUserRequest{Role: role}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteUser removes a member from the organization (DELETE /v1/organizations/users/{user_id}).
func (c *Client) DeleteUser(ctx context.Context, userID string) error {
	p, err := Path("users", userID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil)
}

// ============================================================================
// Invites (Console + Enterprise)
// ============================================================================

// CreateInvite sends an organization invitation (POST /v1/organizations/invites).
func (c *Client) CreateInvite(ctx context.Context, req CreateInviteRequest) (*Invite, error) {
	p, err := Path("invites")
	if err != nil {
		return nil, err
	}
	var inv Invite
	if err := c.Do(ctx, http.MethodPost, p, req, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetInvite retrieves a single invitation by ID (GET /v1/organizations/invites/{invite_id}).
func (c *Client) GetInvite(ctx context.Context, inviteID string) (*Invite, error) {
	p, err := Path("invites", inviteID)
	if err != nil {
		return nil, err
	}
	var inv Invite
	if err := c.Do(ctx, http.MethodGet, p, nil, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// ListInvites lists all organization invitations (GET /v1/organizations/invites).
func (c *Client) ListInvites(ctx context.Context) ([]Invite, error) {
	p, err := Path("invites")
	if err != nil {
		return nil, err
	}
	return ListAll[Invite](ctx, c, p, nil)
}

// DeleteInvite revokes a pending invitation (DELETE /v1/organizations/invites/{invite_id}).
func (c *Client) DeleteInvite(ctx context.Context, inviteID string) error {
	p, err := Path("invites", inviteID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil)
}

// ============================================================================
// Workspaces & Workspace Members (Claude Console)
// ============================================================================

// CreateWorkspace creates a new Console workspace (POST /v1/organizations/workspaces).
func (c *Client) CreateWorkspace(ctx context.Context, req CreateWorkspaceRequest) (*Workspace, error) {
	p, err := Path("workspaces")
	if err != nil {
		return nil, err
	}
	var ws Workspace
	if err := c.Do(ctx, http.MethodPost, p, req, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// GetWorkspace retrieves a single workspace by ID (GET /v1/organizations/workspaces/{workspace_id}).
func (c *Client) GetWorkspace(ctx context.Context, workspaceID string) (*Workspace, error) {
	p, err := Path("workspaces", workspaceID)
	if err != nil {
		return nil, err
	}
	var ws Workspace
	if err := c.Do(ctx, http.MethodGet, p, nil, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// ListWorkspaces lists all workspaces in the organization (GET /v1/organizations/workspaces).
func (c *Client) ListWorkspaces(ctx context.Context, includeArchived bool) ([]Workspace, error) {
	p, err := Path("workspaces")
	if err != nil {
		return nil, err
	}
	q := make(url.Values)
	if includeArchived {
		q.Set("include_archived", "true")
	}
	return ListAll[Workspace](ctx, c, p, q)
}

// UpdateWorkspace updates an existing workspace (POST /v1/organizations/workspaces/{workspace_id}).
func (c *Client) UpdateWorkspace(ctx context.Context, workspaceID string, req UpdateWorkspaceRequest) (*Workspace, error) {
	p, err := Path("workspaces", workspaceID)
	if err != nil {
		return nil, err
	}
	var ws Workspace
	if err := c.Do(ctx, http.MethodPost, p, req, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// ArchiveWorkspace archives a workspace (POST /v1/organizations/workspaces/{workspace_id}/archive).
func (c *Client) ArchiveWorkspace(ctx context.Context, workspaceID string) (*Workspace, error) {
	p, err := Path("workspaces", workspaceID, "archive")
	if err != nil {
		return nil, err
	}
	var ws Workspace
	if err := c.Do(ctx, http.MethodPost, p, struct{}{}, &ws); err != nil {
		return nil, err
	}
	return &ws, nil
}

// CreateWorkspaceMember adds a user to a workspace (POST /v1/organizations/workspaces/{workspace_id}/members).
func (c *Client) CreateWorkspaceMember(ctx context.Context, workspaceID string, req CreateWorkspaceMemberRequest) (*WorkspaceMember, error) {
	p, err := Path("workspaces", workspaceID, "members")
	if err != nil {
		return nil, err
	}
	var wm WorkspaceMember
	if err := c.Do(ctx, http.MethodPost, p, req, &wm); err != nil {
		return nil, err
	}
	return &wm, nil
}

// GetWorkspaceMember retrieves a workspace member (GET /v1/organizations/workspaces/{workspace_id}/members/{user_id}).
func (c *Client) GetWorkspaceMember(ctx context.Context, workspaceID, userID string) (*WorkspaceMember, error) {
	p, err := Path("workspaces", workspaceID, "members", userID)
	if err != nil {
		return nil, err
	}
	var wm WorkspaceMember
	if err := c.Do(ctx, http.MethodGet, p, nil, &wm); err != nil {
		return nil, err
	}
	return &wm, nil
}

// ListWorkspaceMembers lists all members of a workspace (GET /v1/organizations/workspaces/{workspace_id}/members).
func (c *Client) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]WorkspaceMember, error) {
	p, err := Path("workspaces", workspaceID, "members")
	if err != nil {
		return nil, err
	}
	return ListAll[WorkspaceMember](ctx, c, p, nil)
}

// UpdateWorkspaceMember updates a member's role in a workspace (POST /v1/organizations/workspaces/{workspace_id}/members/{user_id}).
func (c *Client) UpdateWorkspaceMember(ctx context.Context, workspaceID, userID string, req UpdateWorkspaceMemberRequest) (*WorkspaceMember, error) {
	p, err := Path("workspaces", workspaceID, "members", userID)
	if err != nil {
		return nil, err
	}
	var wm WorkspaceMember
	if err := c.Do(ctx, http.MethodPost, p, req, &wm); err != nil {
		return nil, err
	}
	return &wm, nil
}

// DeleteWorkspaceMember removes a user from a workspace (DELETE /v1/organizations/workspaces/{workspace_id}/members/{user_id}).
func (c *Client) DeleteWorkspaceMember(ctx context.Context, workspaceID, userID string) error {
	p, err := Path("workspaces", workspaceID, "members", userID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil)
}

// ============================================================================
// API Keys (Claude Console)
// ============================================================================

// APIKeyListFilter holds optional filters for listing organization API keys.
type APIKeyListFilter struct {
	Status          string
	WorkspaceID     string
	CreatedByUserID string
}

// GetAPIKey retrieves metadata for a single API key (GET /v1/organizations/api_keys/{api_key_id}).
func (c *Client) GetAPIKey(ctx context.Context, apiKeyID string) (*APIKey, error) {
	p, err := Path("api_keys", apiKeyID)
	if err != nil {
		return nil, err
	}
	var key APIKey
	if err := c.Do(ctx, http.MethodGet, p, nil, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// ListAPIKeys lists organization API keys (GET /v1/organizations/api_keys).
func (c *Client) ListAPIKeys(ctx context.Context, filter APIKeyListFilter) ([]APIKey, error) {
	p, err := Path("api_keys")
	if err != nil {
		return nil, err
	}
	q := make(url.Values)
	if filter.Status != "" {
		q.Set("status", filter.Status)
	}
	if filter.WorkspaceID != "" {
		q.Set("workspace_id", filter.WorkspaceID)
	}
	if filter.CreatedByUserID != "" {
		q.Set("created_by_user_id", filter.CreatedByUserID)
	}
	return ListAll[APIKey](ctx, c, p, q)
}

// UpdateAPIKey updates an API key's name and/or status (POST /v1/organizations/api_keys/{api_key_id}).
func (c *Client) UpdateAPIKey(ctx context.Context, apiKeyID string, req UpdateAPIKeyRequest) (*APIKey, error) {
	p, err := Path("api_keys", apiKeyID)
	if err != nil {
		return nil, err
	}
	var key APIKey
	if err := c.Do(ctx, http.MethodPost, p, req, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// ============================================================================
// Claude Enterprise: RBAC Groups, Group Members, and Roles
// ============================================================================

// CreateRBACGroup creates a custom RBAC group (POST /v1/organizations/rbac_groups).
// If the initial response returns degraded role data (null), it retries GET once to resolve roles.
func (c *Client) CreateRBACGroup(ctx context.Context, req CreateRBACGroupRequest) (*RBACGroup, error) {
	p, err := Path("rbac_groups")
	if err != nil {
		return nil, err
	}
	var g RBACGroup
	if err := c.Do(ctx, http.MethodPost, p, req, &g, WithBeta(BetaCEUserManagement)); err != nil {
		return nil, err
	}
	if _, degraded := g.EffectiveRoleIDs(); degraded && g.ID != "" {
		if refreshed, err := c.GetRBACGroup(ctx, g.ID); err == nil {
			return refreshed, nil
		}
	}
	return &g, nil
}

// GetRBACGroup retrieves an RBAC group by ID (GET /v1/organizations/rbac_groups/{id}).
func (c *Client) GetRBACGroup(ctx context.Context, groupID string) (*RBACGroup, error) {
	p, err := Path("rbac_groups", groupID)
	if err != nil {
		return nil, err
	}
	var g RBACGroup
	if err := c.Do(ctx, http.MethodGet, p, nil, &g, WithBeta(BetaCEUserManagement)); err != nil {
		return nil, err
	}
	return &g, nil
}

// ListRBACGroups lists all RBAC groups in the enterprise (GET /v1/organizations/rbac_groups).
func (c *Client) ListRBACGroups(ctx context.Context) ([]RBACGroup, error) {
	p, err := Path("rbac_groups")
	if err != nil {
		return nil, err
	}
	return ListAll[RBACGroup](ctx, c, p, nil, WithBeta(BetaCEUserManagement))
}

// UpdateRBACGroup renames a custom RBAC group (POST /v1/organizations/rbac_groups/{id}?beta=true).
func (c *Client) UpdateRBACGroup(ctx context.Context, groupID string, req UpdateRBACGroupRequest) (*RBACGroup, error) {
	p, err := Path("rbac_groups", groupID)
	if err != nil {
		return nil, err
	}
	var g RBACGroup
	if err := c.Do(ctx, http.MethodPost, p, req, &g, WithBeta(BetaCEUserManagement)); err != nil {
		return nil, err
	}
	if _, degraded := g.EffectiveRoleIDs(); degraded && g.ID != "" {
		if refreshed, err := c.GetRBACGroup(ctx, g.ID); err == nil {
			return refreshed, nil
		}
	}
	return &g, nil
}

// DeleteRBACGroup deletes a custom RBAC group (DELETE /v1/organizations/rbac_groups/{id}).
func (c *Client) DeleteRBACGroup(ctx context.Context, groupID string) error {
	p, err := Path("rbac_groups", groupID)
	if err != nil {
		return err
	}
	err = c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaCEUserManagement))
	c.InvalidateRBACGroupMemberCache(groupID)
	return err
}

// AddRBACGroupMember adds a user to a custom RBAC group (POST /v1/organizations/rbac_groups/{id}/members).
func (c *Client) AddRBACGroupMember(ctx context.Context, groupID, userID string) (*RBACGroupMember, error) {
	p, err := Path("rbac_groups", groupID, "members")
	if err != nil {
		return nil, err
	}
	var m RBACGroupMember
	if err := c.Do(ctx, http.MethodPost, p, AddRBACGroupMemberRequest{UserID: userID}, &m, WithBeta(BetaCEUserManagement)); err != nil {
		return nil, err
	}
	c.InvalidateRBACGroupMemberCache(groupID)
	return &m, nil
}

// RemoveRBACGroupMember removes a user from a custom RBAC group (DELETE /v1/organizations/rbac_groups/{id}/members/{user_id}).
func (c *Client) RemoveRBACGroupMember(ctx context.Context, groupID, userID string) error {
	p, err := Path("rbac_groups", groupID, "members", userID)
	if err != nil {
		return err
	}
	err = c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaCEUserManagement))
	c.InvalidateRBACGroupMemberCache(groupID)
	return err
}

// InvalidateRBACGroupMemberCache evicts any cached member list for groupID.
func (c *Client) InvalidateRBACGroupMemberCache(groupID string) {
	c.memberCacheMu.Lock()
	delete(c.memberCache, groupID)
	c.memberCacheMu.Unlock()
}

// ListRBACGroupMembers lists all members of an RBAC group (GET /v1/organizations/rbac_groups/{id}/members),
// coalescing concurrent reads for the same groupID and caching for memberCacheTTL so refreshing
// many anthropic_rbac_group_member resources in a single plan does not exhaust the 100 req/min rate limit.
func (c *Client) ListRBACGroupMembers(ctx context.Context, groupID string) ([]RBACGroupMember, error) {
	p, err := Path("rbac_groups", groupID, "members")
	if err != nil {
		return nil, err
	}

	c.memberCacheMu.Lock()
	if cached, ok := c.memberCache[groupID]; ok && time.Now().Before(cached.expiresAt) {
		out := make([]RBACGroupMember, len(cached.members))
		copy(out, cached.members)
		c.memberCacheMu.Unlock()
		return out, nil
	}
	if inflight, ok := c.memberInflight[groupID]; ok {
		c.memberCacheMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-inflight.done:
			if inflight.err != nil {
				return nil, inflight.err
			}
			out := make([]RBACGroupMember, len(inflight.members))
			copy(out, inflight.members)
			return out, nil
		}
	}

	call := &inflightMemberCall{done: make(chan struct{})}
	c.memberInflight[groupID] = call
	c.memberCacheMu.Unlock()

	members, fetchErr := ListAll[RBACGroupMember](ctx, c, p, nil, WithBeta(BetaCEUserManagement))

	c.memberCacheMu.Lock()
	call.members = members
	call.err = fetchErr
	delete(c.memberInflight, groupID)
	if fetchErr == nil && c.memberCacheTTL > 0 {
		cachedCopy := make([]RBACGroupMember, len(members))
		copy(cachedCopy, members)
		c.memberCache[groupID] = cachedGroupMembers{
			members:   cachedCopy,
			expiresAt: time.Now().Add(c.memberCacheTTL),
		}
	}
	close(call.done)
	c.memberCacheMu.Unlock()

	if fetchErr != nil {
		return nil, fetchErr
	}
	out := make([]RBACGroupMember, len(members))
	copy(out, members)
	return out, nil
}

// FindRBACGroupMember locates a single user in an RBAC group's member list.
func (c *Client) FindRBACGroupMember(ctx context.Context, groupID, userID string) (*RBACGroupMember, error) {
	if _, err := Path("rbac_groups", groupID, "members", userID); err != nil {
		return nil, err
	}
	members, err := c.ListRBACGroupMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if members[i].UserID == userID {
			return &members[i], nil
		}
	}
	return nil, nil
}

// ListRBACRoles lists all Enterprise RBAC roles (GET /v1/organizations/rbac_roles).
func (c *Client) ListRBACRoles(ctx context.Context) ([]RBACRole, error) {
	p, err := Path("rbac_roles")
	if err != nil {
		return nil, err
	}
	return ListAll[RBACRole](ctx, c, p, nil, WithBeta(BetaCEUserManagement))
}

// ============================================================================
// Claude Enterprise: Spend Limits
// ============================================================================

// UpsertSpendLimit creates or updates a per-user spend limit override (POST /v1/organizations/spend_limits).
func (c *Client) UpsertSpendLimit(ctx context.Context, req UpsertSpendLimitRequest) (*SpendLimit, error) {
	p, err := Path("spend_limits")
	if err != nil {
		return nil, err
	}
	var sl SpendLimit
	if err := c.Do(ctx, http.MethodPost, p, req, &sl, WithBeta(BetaSpendLimitReads)); err != nil {
		return nil, err
	}
	return &sl, nil
}

// GetSpendLimit retrieves a spend limit override by ID (GET /v1/organizations/spend_limits/{id}).
func (c *Client) GetSpendLimit(ctx context.Context, spendLimitID string) (*SpendLimit, error) {
	p, err := Path("spend_limits", spendLimitID)
	if err != nil {
		return nil, err
	}
	var sl SpendLimit
	if err := c.Do(ctx, http.MethodGet, p, nil, &sl, WithBeta(BetaSpendLimitReads)); err != nil {
		return nil, err
	}
	return &sl, nil
}

// DeleteSpendLimit deletes a per-user spend limit override (DELETE /v1/organizations/spend_limits/{id}).
func (c *Client) DeleteSpendLimit(ctx context.Context, spendLimitID string) error {
	p, err := Path("spend_limits", spendLimitID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil, WithBeta(BetaSpendLimitReads))
}

// ============================================================================
// Workload Identity Federation (WIF): Service Accounts, Issuers, and Rules
// ============================================================================

// CreateServiceAccount creates a new non-human service account (POST /v1/organizations/service_accounts).
func (c *Client) CreateServiceAccount(ctx context.Context, req CreateServiceAccountRequest) (*ServiceAccount, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/service_accounts"); err != nil {
		return nil, err
	}
	p, err := Path("service_accounts")
	if err != nil {
		return nil, err
	}
	var sa ServiceAccount
	if err := c.Do(ctx, http.MethodPost, p, req, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

// GetServiceAccount retrieves a single service account by ID (GET /v1/organizations/service_accounts/{id}).
func (c *Client) GetServiceAccount(ctx context.Context, serviceAccountID string) (*ServiceAccount, error) {
	if err := c.requireWIFCapableAuth("GET /v1/organizations/service_accounts/{id}"); err != nil {
		return nil, err
	}
	p, err := Path("service_accounts", serviceAccountID)
	if err != nil {
		return nil, err
	}
	var sa ServiceAccount
	if err := c.Do(ctx, http.MethodGet, p, nil, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

// ListServiceAccounts lists organization service accounts (GET /v1/organizations/service_accounts).
func (c *Client) ListServiceAccounts(ctx context.Context, includeArchived bool) ([]ServiceAccount, error) {
	if err := c.requireWIFCapableAuth("GET /v1/organizations/service_accounts"); err != nil {
		return nil, err
	}
	p, err := Path("service_accounts")
	if err != nil {
		return nil, err
	}
	q := make(url.Values)
	if includeArchived {
		q.Set("include_archived", "true")
	}
	return ListAll[ServiceAccount](ctx, c, p, q)
}

// UpdateServiceAccount updates an existing service account (POST /v1/organizations/service_accounts/{id}).
func (c *Client) UpdateServiceAccount(ctx context.Context, serviceAccountID string, req UpdateServiceAccountRequest) (*ServiceAccount, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/service_accounts/{id}"); err != nil {
		return nil, err
	}
	p, err := Path("service_accounts", serviceAccountID)
	if err != nil {
		return nil, err
	}
	var sa ServiceAccount
	if err := c.Do(ctx, http.MethodPost, p, req, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

// ArchiveServiceAccount archives a service account (POST /v1/organizations/service_accounts/{id}/archive).
func (c *Client) ArchiveServiceAccount(ctx context.Context, serviceAccountID string) (*ServiceAccount, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/service_accounts/{id}/archive"); err != nil {
		return nil, err
	}
	p, err := Path("service_accounts", serviceAccountID, "archive")
	if err != nil {
		return nil, err
	}
	var sa ServiceAccount
	if err := c.Do(ctx, http.MethodPost, p, struct{}{}, &sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

// AddServiceAccountToWorkspace adds a service account to a workspace
// (POST /v1/organizations/service_accounts/{service_account_id}/workspaces).
func (c *Client) AddServiceAccountToWorkspace(ctx context.Context, serviceAccountID string, req AddServiceAccountToWorkspaceRequest) (*ServiceAccountWorkspaceMember, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/service_accounts/{service_account_id}/workspaces"); err != nil {
		return nil, err
	}
	p, err := Path("service_accounts", serviceAccountID, "workspaces")
	if err != nil {
		return nil, err
	}
	var m ServiceAccountWorkspaceMember
	if err := c.Do(ctx, http.MethodPost, p, req, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListServiceAccountWorkspaces lists workspace memberships for a service account
// (GET /v1/organizations/service_accounts/{service_account_id}/workspaces).
func (c *Client) ListServiceAccountWorkspaces(ctx context.Context, serviceAccountID string) ([]ServiceAccountWorkspaceMember, error) {
	if err := c.requireWIFCapableAuth("GET /v1/organizations/service_accounts/{service_account_id}/workspaces"); err != nil {
		return nil, err
	}
	p, err := Path("service_accounts", serviceAccountID, "workspaces")
	if err != nil {
		return nil, err
	}
	return ListAll[ServiceAccountWorkspaceMember](ctx, c, p, nil)
}

// GetServiceAccountWorkspace retrieves a service account's membership in a specific workspace
// by listing the service account's workspace memberships.
func (c *Client) GetServiceAccountWorkspace(ctx context.Context, serviceAccountID, workspaceID string) (*ServiceAccountWorkspaceMember, error) {
	if _, err := Path("service_accounts", serviceAccountID, "workspaces", workspaceID); err != nil {
		return nil, err
	}
	members, err := c.ListServiceAccountWorkspaces(ctx, serviceAccountID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		if members[i].WorkspaceID == workspaceID {
			return &members[i], nil
		}
	}
	return nil, &APIError{
		StatusCode: http.StatusNotFound,
		Type:       "not_found_error",
		Message:    fmt.Sprintf("service account %s is not a member of workspace %s", serviceAccountID, workspaceID),
	}
}

// UpdateServiceAccountWorkspace updates a service account's role in a workspace
// (POST /v1/organizations/workspaces/{workspace_id}/service_accounts/{service_account_id}).
func (c *Client) UpdateServiceAccountWorkspace(ctx context.Context, serviceAccountID, workspaceID string, req UpdateServiceAccountWorkspaceRequest) (*ServiceAccountWorkspaceMember, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/workspaces/{workspace_id}/service_accounts/{service_account_id}"); err != nil {
		return nil, err
	}
	p, err := Path("workspaces", workspaceID, "service_accounts", serviceAccountID)
	if err != nil {
		return nil, err
	}
	var m ServiceAccountWorkspaceMember
	if err := c.Do(ctx, http.MethodPost, p, req, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// RemoveServiceAccountFromWorkspace removes an explicit workspace membership for a service account
// (DELETE /v1/organizations/service_accounts/{service_account_id}/workspaces/{workspace_id}).
func (c *Client) RemoveServiceAccountFromWorkspace(ctx context.Context, serviceAccountID, workspaceID string) error {
	if err := c.requireWIFCapableAuth("DELETE /v1/organizations/service_accounts/{service_account_id}/workspaces/{workspace_id}"); err != nil {
		return err
	}
	p, err := Path("service_accounts", serviceAccountID, "workspaces", workspaceID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil)
}

// CreateFederationIssuer creates an OIDC federation issuer (POST /v1/organizations/federation_issuers).
func (c *Client) CreateFederationIssuer(ctx context.Context, req CreateFederationIssuerRequest) (*FederationIssuer, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/federation_issuers"); err != nil {
		return nil, err
	}
	p, err := Path("federation_issuers")
	if err != nil {
		return nil, err
	}
	var fi FederationIssuer
	if err := c.Do(ctx, http.MethodPost, p, req, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

// GetFederationIssuer retrieves a federation issuer by ID (GET /v1/organizations/federation_issuers/{id}).
func (c *Client) GetFederationIssuer(ctx context.Context, issuerID string) (*FederationIssuer, error) {
	if err := c.requireWIFCapableAuth("GET /v1/organizations/federation_issuers/{id}"); err != nil {
		return nil, err
	}
	p, err := Path("federation_issuers", issuerID)
	if err != nil {
		return nil, err
	}
	var fi FederationIssuer
	if err := c.Do(ctx, http.MethodGet, p, nil, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

// ListFederationIssuers lists all federation issuers (GET /v1/organizations/federation_issuers).
func (c *Client) ListFederationIssuers(ctx context.Context, includeArchived bool) ([]FederationIssuer, error) {
	if err := c.requireWIFCapableAuth("GET /v1/organizations/federation_issuers"); err != nil {
		return nil, err
	}
	p, err := Path("federation_issuers")
	if err != nil {
		return nil, err
	}
	q := make(url.Values)
	if includeArchived {
		q.Set("include_archived", "true")
	}
	return ListAll[FederationIssuer](ctx, c, p, q)
}

// UpdateFederationIssuer updates an existing federation issuer (POST /v1/organizations/federation_issuers/{id}).
func (c *Client) UpdateFederationIssuer(ctx context.Context, issuerID string, req UpdateFederationIssuerRequest) (*FederationIssuer, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/federation_issuers/{id}"); err != nil {
		return nil, err
	}
	p, err := Path("federation_issuers", issuerID)
	if err != nil {
		return nil, err
	}
	var fi FederationIssuer
	if err := c.Do(ctx, http.MethodPost, p, req, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

// ArchiveFederationIssuer archives a federation issuer (POST /v1/organizations/federation_issuers/{id}/archive).
func (c *Client) ArchiveFederationIssuer(ctx context.Context, issuerID string) (*FederationIssuer, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/federation_issuers/{id}/archive"); err != nil {
		return nil, err
	}
	p, err := Path("federation_issuers", issuerID, "archive")
	if err != nil {
		return nil, err
	}
	var fi FederationIssuer
	if err := c.Do(ctx, http.MethodPost, p, struct{}{}, &fi); err != nil {
		return nil, err
	}
	return &fi, nil
}

// CreateFederationRule creates a Workload Identity Federation rule (POST /v1/organizations/federation_rules).
func (c *Client) CreateFederationRule(ctx context.Context, req CreateFederationRuleRequest) (*FederationRule, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/federation_rules"); err != nil {
		return nil, err
	}
	p, err := Path("federation_rules")
	if err != nil {
		return nil, err
	}
	var fr FederationRule
	if err := c.Do(ctx, http.MethodPost, p, req, &fr); err != nil {
		return nil, err
	}
	return &fr, nil
}

// AddFederationRuleWorkspace enables a rule for a workspace (POST /v1/organizations/federation_rules/{id}/workspaces).
func (c *Client) AddFederationRuleWorkspace(ctx context.Context, ruleID, workspaceID string) error {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/federation_rules/{id}/workspaces"); err != nil {
		return err
	}
	p, err := Path("federation_rules", ruleID, "workspaces")
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodPost, p, AddFederationRuleWorkspaceRequest{WorkspaceID: workspaceID}, nil)
}

// RemoveFederationRuleWorkspace disables a rule for a workspace (DELETE /v1/organizations/federation_rules/{id}/workspaces/{workspace_id}).
func (c *Client) RemoveFederationRuleWorkspace(ctx context.Context, ruleID, workspaceID string) error {
	if err := c.requireWIFCapableAuth("DELETE /v1/organizations/federation_rules/{id}/workspaces/{workspace_id}"); err != nil {
		return err
	}
	p, err := Path("federation_rules", ruleID, "workspaces", workspaceID)
	if err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, p, nil, nil)
}

// GetFederationRule retrieves a federation rule by ID (GET /v1/organizations/federation_rules/{id}).
func (c *Client) GetFederationRule(ctx context.Context, ruleID string) (*FederationRule, error) {
	if err := c.requireWIFCapableAuth("GET /v1/organizations/federation_rules/{id}"); err != nil {
		return nil, err
	}
	p, err := Path("federation_rules", ruleID)
	if err != nil {
		return nil, err
	}
	var fr FederationRule
	if err := c.Do(ctx, http.MethodGet, p, nil, &fr); err != nil {
		return nil, err
	}
	return &fr, nil
}

// ListFederationRules lists all federation rules (GET /v1/organizations/federation_rules).
func (c *Client) ListFederationRules(ctx context.Context, includeArchived bool) ([]FederationRule, error) {
	if err := c.requireWIFCapableAuth("GET /v1/organizations/federation_rules"); err != nil {
		return nil, err
	}
	p, err := Path("federation_rules")
	if err != nil {
		return nil, err
	}
	q := make(url.Values)
	if includeArchived {
		q.Set("include_archived", "true")
	}
	return ListAll[FederationRule](ctx, c, p, q)
}

// UpdateFederationRule updates a federation rule (POST /v1/organizations/federation_rules/{id}).
func (c *Client) UpdateFederationRule(ctx context.Context, ruleID string, req UpdateFederationRuleRequest) (*FederationRule, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/federation_rules/{id}"); err != nil {
		return nil, err
	}
	p, err := Path("federation_rules", ruleID)
	if err != nil {
		return nil, err
	}
	var fr FederationRule
	if err := c.Do(ctx, http.MethodPost, p, req, &fr); err != nil {
		return nil, err
	}
	return &fr, nil
}

// ArchiveFederationRule archives a federation rule (POST /v1/organizations/federation_rules/{id}/archive).
func (c *Client) ArchiveFederationRule(ctx context.Context, ruleID string) (*FederationRule, error) {
	if err := c.requireWIFCapableAuth("POST /v1/organizations/federation_rules/{id}/archive"); err != nil {
		return nil, err
	}
	p, err := Path("federation_rules", ruleID, "archive")
	if err != nil {
		return nil, err
	}
	var fr FederationRule
	if err := c.Do(ctx, http.MethodPost, p, struct{}{}, &fr); err != nil {
		return nil, err
	}
	return &fr, nil
}
