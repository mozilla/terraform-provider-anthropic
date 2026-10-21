terraform {
  required_providers {
    anthropic = {
      source = "mozilla/anthropic"
    }
  }
}

# Option 1: Static Organization Admin API Key (Claude Console & Enterprise resources;
# note that Service Accounts and WIF resources require Option 2 or Option 3).
# Set ANTHROPIC_ADMIN_API_KEY in the environment (`sk-ant-admin01-...`).
provider "anthropic" {
  # api_key     = var.anthropic_admin_api_key
  # base_url    = "https://api.anthropic.com"
  # max_retries = 3
}

# Option 2: OAuth Bearer Token with `org:admin` scope (supports all resources including WIF).
# Set ANTHROPIC_OAUTH_TOKEN or ANTHROPIC_AUTH_TOKEN in the environment.
# provider "anthropic" {
#   oauth_token = var.anthropic_oauth_token
# }

# Option 3: Workload Identity Federation (WIF) via OIDC JWT token exchange
# (supports all resources including Service Accounts, Federation Issuers, and Federation Rules).
# provider "anthropic" {
#   federation = {
#     federation_rule_id  = var.federation_rule_id     # or ANTHROPIC_FEDERATION_RULE_ID
#     organization_id     = var.organization_id        # or ANTHROPIC_ORGANIZATION_ID
#     service_account_id  = var.service_account_id     # or ANTHROPIC_SERVICE_ACCOUNT_ID
#     identity_token_file = "/var/run/secrets/oidc/token" # or ANTHROPIC_IDENTITY_TOKEN_FILE / ANTHROPIC_IDENTITY_TOKEN
#   }
# }
