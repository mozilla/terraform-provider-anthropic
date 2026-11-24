// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestExpandFederationIssuerJWKSInline(t *testing.T) {
	keys := `[{"kid":"a","kty":"RSA","n":"x","e":"AQAB"}]`

	out, err := expandFederationIssuerJWKS(&FederationIssuerJWKSModel{Type: types.StringValue("inline"), Keys: types.StringValue(keys)})
	if err != nil || len(out.Keys) != 1 || out.Keys[0]["kid"] != "a" {
		t.Fatalf("inline keys not expanded: %v %v", out.Keys, err)
	}

	if _, err := expandFederationIssuerJWKS(&FederationIssuerJWKSModel{Type: types.StringValue("inline"), Keys: types.StringValue(`{"kid":"a"}`)}); err == nil {
		t.Error("keys that are not a JSON array should error")
	}

	// Same keys with different formatting keep the prior string, so no diff.
	prior := types.StringValue("[ {\"kty\":\"RSA\", \"kid\":\"a\", \"e\":\"AQAB\", \"n\":\"x\"} ]")
	if got := flattenJWKSKeys(prior, out.Keys); !got.Equal(prior) {
		t.Errorf("semantically equal keys replaced prior: %s", got)
	}
	// Rotated keys replace it.
	rotated := []map[string]any{{"kid": "b", "kty": "RSA", "n": "y", "e": "AQAB"}}
	if got := flattenJWKSKeys(prior, rotated); got.Equal(prior) {
		t.Error("rotated keys did not replace prior")
	}
	if got := flattenJWKSKeys(prior, nil); !got.IsNull() {
		t.Errorf("no keys should be null, got %s", got)
	}
}
