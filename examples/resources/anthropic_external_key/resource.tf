# AWS KMS key. The region is derived from the ARN when omitted.
resource "anthropic_external_key" "aws" {
  display_name = "Primary AWS key"
  provider_config = {
    type    = "aws"
    kms_arn = "arn:aws:kms:us-east-1:111122223333:key/00000000-0000-0000-0000-000000000000"
  }
}

# Google Cloud KMS key bound to one user profile.
resource "anthropic_external_key" "gcp" {
  display_name    = "Customer GCP key"
  user_profile_id = "uprof_01J9ABCDEF"
  provider_config = {
    type     = "gcp"
    key_name = "projects/example-project/locations/us/keyRings/example-ring/cryptoKeys/example-key"
  }
}

# Azure Key Vault key using Anthropic's multitenant app.
resource "anthropic_external_key" "azure" {
  provider_config = {
    type      = "azure"
    key_name  = "example-key"
    tenant_id = "00000000-0000-0000-0000-000000000000"
    vault_uri = "https://example-vault.vault.azure.net"
  }
}
