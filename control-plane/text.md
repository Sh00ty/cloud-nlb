## Архитектура

## Запросы

grpcurl -plaintext \
  -d '{
    "node_id": {"value": "dataplane-node-001"},
    "placement_version": 6,
    "target_groups_status": {
      "mws-k8s-kubeproxy": {
        "spec_version": 0,
        "endpoints_version": 0,
        "last_applied_timestamp": 1704067200
      },
      "tg-api-backend-1": {
        "spec_version": 0,
        "endpoints_version": 0,
        "last_applied_timestamp": 1704067100
      }
    },
    "wait_timeout_seconds": 10,
    "request_id": "req-456"
  }' \
  localhost:9090 \
  cplpbv1.ControlPlaneService/StreamDataPlaneAssignments



grpcurl -plaintext -d '{
  "target_group_id": {"value": "mws-k8s-kubeproxy"},
  "endpoint": {
    "ip": "192.168.1.11",
    "port": 8080,
    "weight": 100
  },
  "operation": "ENDPOINT_OPERATION_TYPE_ADD",
  "idempotency_key": "add-endpoint-001"
}' localhost:9090 cplpbv1.ControlPlaneService/UpsertEndpoint


grpcurl -plaintext -d '{
  "id": {"value": "mws-k8s-kubeproxy"},
  "spec": {
    "protocol": "PROTOCOL_TCP",
    "port": 8081,
    "virtual_ip": "10.0.0.100"
  },
  "idempotency_key": "create-tg-001-v1"
}' localhost:9090 cplpbv1.ControlPlaneService/UpsertTargetGroupSpec
{}