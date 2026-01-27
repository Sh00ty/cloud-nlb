package etcd

import (
	"encoding/json"
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func extractStringFromTxnResponse(txResp *clientv3.TxnResponse) (string, error) {
	if len(txResp.Responses) == 0 {
		return "", fmt.Errorf("etcd empty response, expected uint value")
	}
	kvs := txResp.Responses[0].GetResponseRange().GetKvs()
	if len(kvs) < 1 {
		return "", fmt.Errorf("etcd response got zero kv pairs, expected at lease one")
	}
	return string(kvs[0].Value), nil
}

func mustJsonMarshal(val any) string {
	js, err := json.Marshal(val)
	if err != nil {
		panic(err)
	}
	return string(js)
}
