#! /bin/bash

stream_id=$(docker exec yugabyte yb-admin --master_addresses yugabyte:7100 create_change_data_stream ysql.yugabyte | awk '{print $4}')

echo "stream_id ${stream_id}"

docker exec cdc-connector curl -i -X  POST -H  "Accept:application/json" -H  "Content-Type:application/json" -k http://localhost:8083/connectors/ -d '{
  "name": "ybconnector",
  "config": {
    "connector.class": "io.debezium.connector.yugabytedb.YugabyteDBgRPCConnector",
    "database.hostname":"yugabyte",
    "database.port":"5433",
    "database.master.addresses": "yugabyte:7100",
    "database.user": "yugabyte",
    "database.password": "yugabyte",
    "database.dbname" : "yugabyte",
    "database.server.name": "dbserver1",
    "table.include.list":"public.targets",
    "database.streamid":"'"$stream_id"'",
    "snapshot.mode":"initial"
  }
}'
