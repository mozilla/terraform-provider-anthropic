# Changelog

## [0.2.1](https://github.com/mozilla/terraform-provider-anthropic/compare/v0.2.0...v0.2.1) (2026-11-13)


### Bug Fixes

* accept Claude Enterprise admin keys without a prefix warning ([#13](https://github.com/mozilla/terraform-provider-anthropic/issues/13)) ([de459dc](https://github.com/mozilla/terraform-provider-anthropic/commit/de459dc06325b8fb9969b4ca97bb7d7fee8c63e1))

## [0.2.0](https://github.com/mozilla/terraform-provider-anthropic/compare/v0.1.0...v0.2.0) (2026-11-13)


### ⚠ BREAKING CHANGES

* move to Anthropic API version 2026-10-21 and Go SDK v2. workspace_id on API keys now holds the workspace ID for keys in the Default workspace, and ANTHROPIC_AUTH_TOKEN is no longer read (set ANTHROPIC_OAUTH_TOKEN instead)

### Features

* add Console limits and the newer Admin API settings ([8347a89](https://github.com/mozilla/terraform-provider-anthropic/commit/8347a898d96c97e9509ab8a0d3d12b847e79e80d))
* allow importing admin service accounts and org:admin federation rules ([8347a89](https://github.com/mozilla/terraform-provider-anthropic/commit/8347a898d96c97e9509ab8a0d3d12b847e79e80d))
* move to Anthropic API version 2026-10-21 and Go SDK v2. workspace_id on API keys now holds the workspace ID for keys in the Default workspace, and ANTHROPIC_AUTH_TOKEN is no longer read (set ANTHROPIC_OAUTH_TOKEN instead) ([495e37c](https://github.com/mozilla/terraform-provider-anthropic/commit/495e37cc6c0cdfa763d83ccbd5c386191ac7bc23))

## 0.1.0 (2026-10-21)


### Features

* add terraform provider for anthropic ([53b3a3b](https://github.com/mozilla/terraform-provider-anthropic/commit/53b3a3bd1fa146c98b36212aad0eb431c9241c61))


### Bug Fixes

* require https and update vulnerable dependencies ([2eec139](https://github.com/mozilla/terraform-provider-anthropic/commit/2eec13924243509f00441bede9c116749556f40a))
* require https and update vulnerable dependencies ([530c8c1](https://github.com/mozilla/terraform-provider-anthropic/commit/530c8c18cafff001e5725492141cdba13940b8b2))
