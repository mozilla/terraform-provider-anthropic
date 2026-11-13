# terraform-provider-anthropic

A Terraform and OpenTofu provider for Anthropic organizations. It manages Claude Console workspaces, users and API keys, Claude Enterprise groups and spend limits, and service accounts with workload identity federation (WIF).

Resource and data source reference is in [docs/](docs/). Examples are in [examples/](examples/).

## Requirements

- OpenTofu or Terraform. Tested with OpenTofu 1.12.
- Go 1.26 or later to build from source.

The provider uses Anthropic API version `2026-10-21` through the Go SDK v2.

## Authentication

The provider takes one of three credentials.

| Credential | Provider setting | Environment variable | Covers |
|---|---|---|---|
| Admin API key (`sk-ant-admin...`) | `api_key` | `ANTHROPIC_ADMIN_API_KEY` | Console and Enterprise resources. The API rejects admin keys on service account and federation endpoints. |
| OAuth token with `org:admin` scope | `oauth_token` | `ANTHROPIC_OAUTH_TOKEN` | All resources |
| WIF token exchange | `federation` | See below | All resources, when the rule grants `org:admin` |

WIF needs no stored secret, so use it in CI.

Settings in the provider block take precedence over the environment. With no credential in the block, the provider reads `ANTHROPIC_FEDERATION_RULE_ID` and the other federation variables first, then `ANTHROPIC_OAUTH_TOKEN`, then `ANTHROPIC_ADMIN_API_KEY`. A federation field left out of the block is read from its environment variable. The provider does not read `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_PROFILE` or Anthropic CLI profiles, because Claude Code, LLM gateways and other tools set them.

```hcl
provider "anthropic" {
  federation = {
    organization_id     = "00000000-0000-0000-0000-000000000000" # ANTHROPIC_ORGANIZATION_ID
    federation_rule_id  = "fdrl_..."                             # ANTHROPIC_FEDERATION_RULE_ID
    service_account_id  = "svac_..."                             # ANTHROPIC_SERVICE_ACCOUNT_ID
    workspace_id        = "wrkspc_..."                           # ANTHROPIC_WORKSPACE_ID
    identity_token_file = "/path/to/oidc.jwt"                    # ANTHROPIC_IDENTITY_TOKEN_FILE or ANTHROPIC_IDENTITY_TOKEN
  }
}
```

`workspace_id` is required when the rule covers all workspaces or more than one. The token exchange fails with `workspace_id_required` without it.

### Bootstrapping WIF

The provider cannot create the federation rule it authenticates through. The API only lets OAuth callers grant `workspace:developer` or `workspace:inference`, and an `org:admin` rule needs a Console session. Create that first rule by hand:

1. In the Claude Console, open the Connect workload wizard and pick your identity provider.
2. Choose the `org:admin` scope.
3. Match on claims that identify one workload, plus the audience `https://api.anthropic.com`.
4. Copy the organization, rule, service account and workspace IDs into the `federation` block or the environment variables.

Every other issuer, service account and rule can then be managed in code.

`subject_prefix` is an exact match on the JWT `sub` claim unless it ends in `*`, which makes it a prefix match. `repo:org/repo:*` admits every branch, tag and pull request in the repo. For Google, match `sub` (the service account's numeric unique ID) and `email` in `claims`.

## What it manages

"Live" means tested against a real Anthropic organization. Everything else is covered by unit tests against a mock API only.

| Area | Capability | Supported | Live | Notes |
|---|---|---|---|---|
| Console | Workspaces | Yes | Yes | Create, rename, data residency. Destroy archives. |
| Console | Workspace members | Yes | No | |
| Console | Users | Yes | No | Change org role or remove. Users are added by invite or SSO, not created. |
| Console | Invites | Yes | No | Fails with HTTP 400 when JIT SSO or SCIM provisioning is on. |
| Console | API keys | Import only | No | Rename and change status. The API cannot create keys. |
| Enterprise | Groups created through the API | Yes | No | Name and members. |
| Enterprise | SCIM-provisioned groups | Import and read | No | Cannot be renamed or deleted through the API. |
| Enterprise | Group role assignments | No | | Read-only. Assign in the Claude Enterprise admin settings or through SCIM. |
| Enterprise | Roles | Read only | No | `anthropic_rbac_roles` data source. |
| Enterprise | Per-user spend limits | Yes | No | Exactly one of `amount` (cents) or `unlimited = true`, so an unset variable cannot become unlimited. |
| Enterprise | Group, seat tier and org default spend limits | No | | Not in the API. Set in the Claude Enterprise admin settings. |
| WIF | Service accounts | Yes | Yes | Destroy archives. |
| WIF | Service account workspace membership | Yes | Yes | |
| WIF | Federation issuers | Yes | Yes | Destroy archives. |
| WIF | Federation rules (`workspace:developer`, `workspace:inference`) | Yes | Yes | Multiple workspaces, description. Destroy archives. |
| WIF | Federation rules with `org:admin` | No | | Needs a Console session. See [Bootstrapping WIF](#bootstrapping-wif). |

Data sources cover the organization, users, invites, workspaces, workspace members, API keys, groups, roles, service accounts, federation issuers and federation rules. See [docs/](docs/) for attributes.

## Security

* Requests go only to `base_url`, which defaults to `https://api.anthropic.com` and must use `https`. Plain `http` is allowed only on a loopback host for local testing.
* Redirects are not followed. Go keeps the `X-Api-Key` header on a redirect to another host, so following one could send the admin key there.
* An error response that is not an Anthropic API error is shown only as its HTTP status and request ID, because a proxy can echo the request, credentials included, into the body.

## Upgrading to 0.2.0

0.2.0 moves to API version `2026-10-21`. Two changes can affect existing configurations:

* `workspace_id` on `anthropic_api_key` and the API key data sources now holds the Default workspace's ID for a key bound to that workspace. It was `null` before. It stays `null` for a key that can be used in every workspace. The attribute is read-only, so the next plan reports it as changed outside Terraform rather than as a planned change.
* `ANTHROPIC_AUTH_TOKEN` is no longer read. Set `ANTHROPIC_OAUTH_TOKEN` instead.

## Development

```bash
make build    # build the provider binary
make test     # unit tests against a mock HTTP server
make vet
make fmt
make docs     # regenerate docs/ from the schema
```

The `tests/*.tftest.hcl` files plan against a mock provider. To run them, point a CLI config's `dev_overrides` at the built binary, using the key `registry.opentofu.org/mozilla/anthropic` for OpenTofu, then run `tofu test` from `tests/`. Skip `tofu init` when using dev overrides.

## License

[MPL 2.0](LICENSE)
