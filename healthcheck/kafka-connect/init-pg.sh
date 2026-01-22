#! /bin/bash

set -e

echo "Creating Debezium connector for PostgreSQL..."

curl -i -X POST \
  -H "Accept:application/json" \
  -H "Content-Type:application/json" \
  http://localhost:8083/connectors/ \
  -d '{
    "name": "postgres-connector",
    "config": {
      "connector.class": "io.debezium.connector.postgresql.PostgresConnector",
      "database.hostname": "postgres",
      "database.port": "5432",
      "database.user": "postgres",
      "database.password": "postgres",
      "topic.prefix": "dbserver1",
      "database.dbname": "postgres",
      "database.server.name": "dbserver1",
      "table.include.list": "public.targets",
      "plugin.name": "pgoutput",
      "slot.name": "debezium_postgres",
      "publication.name": "dbz_publication",
      "publication.autocreate.mode": "filtered",
      "snapshot.mode": "initial",
      "key.converter": "org.apache.kafka.connect.json.JsonConverter",
      "key.converter.schemas.enable": "false",
      "value.converter": "org.apache.kafka.connect.json.JsonConverter",
      "value.converter.schemas.enable": "false"
    }
  }'

echo "\nCDC connector created!"