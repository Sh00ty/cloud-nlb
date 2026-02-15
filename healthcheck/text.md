## Requests

grpcurl -plaintext -d '{
  "target_group": "mws-k8s-kubeproxy",
  "endpoints": [
    {
      "real_ip": "192.168.5.2",
      "port": 9070
    }
  ]
}' localhost:9091 healthcheck.v1.HealthCheckService/RemoveEndpoints
{
  "removedCount": 1
}

grpcurl -plaintext -d '{
  "target_group": "mws-k8s-kubeproxy",
  "endpoints": [
    {
      "real_ip": "192.168.5.2",
      "port": 9070
    }
  ]
}' localhost:9091 healthcheck.v1.HealthCheckService/AddEndpoints
{
  "addedCount": 1
}

cat > create_settings.json << EOF
{
  "settings": {
    "target_group": "mws-2",
    "interval": "5s",
    "success_before_passing": 2,
    "failures_before_critical": 3,
    "initial_state": true,
    "strategy": "STRATEGY_TYPE_HTTP",
    "strategy_settings": {
      "timeout": 5,
      "path": "/health/",
      "scheme": "http",
      "method": "GET"
    }
  }
}
EOF

grpcurl -plaintext -d @ localhost:9090 healthcheck.v1.HealthCheckService/CreateSettings < create_settings.json



grpcurl -plaintext -d '{
  "target_group": "mws-k8s-kubeproxy"
}' localhost:9091 healthcheck.v1.HealthCheckService/GetEndpointStatuses
