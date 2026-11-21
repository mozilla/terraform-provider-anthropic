// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import "testing"

func TestHTTPSURLPattern(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://token.actions.githubusercontent.com":       true,
		"https://accounts.google.com/.well-known/jwks.json": true,
		"http://token.actions.githubusercontent.com":        false,
		"https://":                            false,
		"https://example.com/a b":             false,
		"token.actions.githubusercontent.com": false,
	} {
		if got := httpsURLPattern.MatchString(url); got != ok {
			t.Errorf("%q: got %v, want %v", url, got, ok)
		}
	}
}

func TestValidateSubjectPrefix(t *testing.T) {
	for v, ok := range map[string]bool{
		// Exact matches.
		"repo:example-org/app:ref:refs/heads/main": true,
		"system:serviceaccount:prod:worker":        true,
		// Trailing wildcard after a delimiter.
		"repo:example-org/app:*": true,
		"repo:example-org/*":     true,
		// Trailing wildcard after any other character, a prefix match per the API.
		"arn:aws:iam::123456789012:role/aws-reserved/sso.amazonaws.com/AWSReservedSSO_developer-*": true,
		"repo:moz*": true,
		// Rejected.
		"":         false,
		"   ":      false,
		"*":        false,
		"a*b":      false,
		"a**":      false,
		"*a":       false,
		"repo:*:*": false,
	} {
		if err := ValidateSubjectPrefix(v); (err == nil) != ok {
			t.Errorf("%q: got err=%v, want ok=%v", v, err, ok)
		}
	}
}
