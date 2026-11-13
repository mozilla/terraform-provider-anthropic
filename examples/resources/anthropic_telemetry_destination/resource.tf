variable "otel_collector_token" {
  type      = string
  sensitive = true
}

# Send spans from selected workspaces to an OTLP/HTTP collector
resource "anthropic_telemetry_destination" "collector" {
  display_name    = "Example collector"
  endpoint_url    = "https://collector.example.com/otlp"
  workspace_scope = "include"

  headers = {
    authorization = "Bearer ${var.otel_collector_token}"
  }

  traces_content_mode = "metadata_only"
}
