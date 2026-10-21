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
