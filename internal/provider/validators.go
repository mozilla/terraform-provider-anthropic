// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var (
	resourceIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	slugPattern        = regexp.MustCompile(`^[a-z0-9-]+$`)
	centsAmountPattern = regexp.MustCompile(`^\d+$`)
	httpsURLPattern    = regexp.MustCompile(`^https://[^/\s]+(/\S*)?$`)
)

// idValidator ensures resource and data-source identifiers contain only safe characters
// (alphanumerics, underscores, and hyphens), preventing path traversal or query injection.
func idValidator() validator.String {
	return stringvalidator.RegexMatches(
		resourceIDPattern,
		"must contain only alphanumeric characters, underscores, or hyphens",
	)
}

// slugValidator ensures service account, federation issuer, and federation rule names
// match Anthropic's slug constraint (lowercase letters, numbers, and hyphens).
func slugValidator() validator.String {
	return stringvalidator.RegexMatches(
		slugPattern,
		"must contain only lowercase alphanumeric characters and hyphens (^[a-z0-9-]+$)",
	)
}

// serviceAccountRoleValidator validates organization-level roles supported for API-created service accounts.
// Admin service accounts can only be created in the Console, not via the REST API.
func serviceAccountRoleValidator() validator.String {
	return stringvalidator.OneOf(
		"developer",
		"user",
	)
}

// serviceAccountWorkspaceRoleValidator validates workspace-level roles supported for service accounts.
func serviceAccountWorkspaceRoleValidator() validator.String {
	return stringvalidator.OneOf(
		"workspace_user",
		"workspace_developer",
		"workspace_restricted_developer",
		"workspace_admin",
	)
}

// federationOAuthScopeValidator validates OAuth scopes allowed on Workload Identity Federation rules.
func federationOAuthScopeValidator() validator.String {
	return stringvalidator.OneOf(
		"workspace:developer",
		"workspace:inference",
	)
}

// ValidateSubjectPrefix enforces Anthropic WIF subject_prefix safety rules:
//   - Rejects empty or whitespace-only values.
//   - Rejects a bare "*" wildcard.
//   - If "*" is present, it must appear only once as the final character and must
//     immediately follow a segment separator (":" or "/"), e.g. "repo:example/app:*"
//     or "repo:example/*". Partial token wildcards such as "repo:moz*" are rejected.
func ValidateSubjectPrefix(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("subject_prefix must not be empty")
	}
	if v == "*" {
		return fmt.Errorf("subject_prefix cannot be a bare wildcard \"*\"")
	}
	if strings.Contains(v, "*") {
		if strings.Count(v, "*") > 1 || !strings.HasSuffix(v, "*") {
			return fmt.Errorf("wildcard \"*\" in subject_prefix is only permitted as the trailing character")
		}
		if len(v) <= 2 || (!strings.HasSuffix(v, ":*") && !strings.HasSuffix(v, "/*")) {
			return fmt.Errorf("wildcard \"*\" in subject_prefix must immediately follow a \":\" or \"/\" delimiter (e.g. \"repo:example-org/app:*\" or \"repo:example-org/*\"), got %q", v)
		}
	}
	return nil
}

type subjectPrefixStringValidator struct{}

func (v subjectPrefixStringValidator) Description(_ context.Context) string {
	return "must be a non-empty subject match or a delimiter-bounded prefix wildcard ending in ':*' or '/*'"
}

func (v subjectPrefixStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v subjectPrefixStringValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := ValidateSubjectPrefix(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Federation Rule Subject Prefix",
			err.Error(),
		)
	}
}

func subjectPrefixValidator() validator.String {
	return subjectPrefixStringValidator{}
}

// nonEmptyStringValidator ensures a string attribute is not empty or whitespace-only.
func nonEmptyStringValidator() validator.String {
	return stringvalidator.LengthAtLeast(1)
}

// httpsURLValidator ensures a URL that keys are trusted from uses https.
func httpsURLValidator() validator.String {
	return stringvalidator.RegexMatches(httpsURLPattern, "must be an https URL")
}

// centsAmountValidator ensures monetary amounts are non-negative integer strings in cents.
func centsAmountValidator() validator.String {
	return stringvalidator.RegexMatches(
		centsAmountPattern,
		"must be a non-negative integer string in minor currency units (cents), e.g. \"10000\" for $100.00",
	)
}

// orgRoleValidator validates organization-level roles supported across Console and Enterprise.
func orgRoleValidator() validator.String {
	return stringvalidator.OneOf(
		"user",
		"developer",
		"billing",
		"admin",
		"claude_code_user",
		"membership_admin",
		"managed",
	)
}

// workspaceRoleValidator validates workspace-level roles in Claude Console.
func workspaceRoleValidator() validator.String {
	return stringvalidator.OneOf(
		"workspace_user",
		"workspace_developer",
		"workspace_restricted_developer",
		"workspace_admin",
		"workspace_billing",
	)
}

// splitCompositeID splits a "left/right" import identifier and validates both segments.
func splitCompositeID(rawID, leftName, rightName string) (string, string, error) {
	parts := strings.Split(rawID, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected import identifier in format %q, got %q", leftName+"/"+rightName, rawID)
	}
	if !resourceIDPattern.MatchString(parts[0]) {
		return "", "", fmt.Errorf("invalid %s %q: must contain only alphanumeric characters, underscores, or hyphens", leftName, parts[0])
	}
	if !resourceIDPattern.MatchString(parts[1]) {
		return "", "", fmt.Errorf("invalid %s %q: must contain only alphanumeric characters, underscores, or hyphens", rightName, parts[1])
	}
	return parts[0], parts[1], nil
}
