resource "anthropic_telemetry_destination_workspace" "example" {
  destination_id = anthropic_telemetry_destination.collector.id
  workspace_id   = "wrkspc_01ABCDEF"
}
