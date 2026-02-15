package targetwatcher

type TargetDto struct {
	TargetGroup string `json:"target_group"`
	RealIP      string `json:"real_ip"`
	Port        int    `json:"port"`
	Vshard      int    `json:"vshard"`
}

type Message[T any] struct {
	Value *Value[T] `json:"value"`
	Topic string    `json:"topic"`
	Key   T         `json:"key"`
}

// type Value struct {
// 	Value any  `json:"value"`
// 	Set   bool `json:"set"`
// }

type Source struct {
	TsMs       uint64 `json:"ts_ms"`
	CommitTime uint64 `json:"commit_time"`
	Table      string `json:"table"`
}

type Value[T any] struct {
	Before *T     `json:"before"`
	After  *T     `json:"after"`
	Op     string `json:"op"`
	TsMs   int64  `json:"ts_ms"`
}

// > docker exec redpanda rpk topic consume dbserver1.public.targets --offset -2                                                                                 [6:34:28]
// {
//   "topic": "dbserver1.public.targets",
//   "key": "{\"setting_id\":3,\"real_ip\":\"172.16.0.101\",\"port\":5432}",
//   "value": "{\"before\":null,\"after\":{\"setting_id\":3,\"real_ip\":\"172.16.0.101\",\"port\":5432},\"source\":{\"version\":\"2.3.4.Final\",\"connector\":\"postgresql\",\"name\":\"dbserver1\",\"ts_ms\":1763998468684,\"snapshot\":\"last\",\"db\":\"postgres\",\"sequence\":\"[null,\\\"24624248\\\"]\",\"schema\":\"public\",\"table\":\"targets\",\"txId\":748,\"lsn\":24624248,\"xmin\":null},\"op\":\"r\",\"ts_ms\":1763998468734,\"transaction\":null}",
//   "timestamp": 1763998469414,
//   "partition": 0,
//   "offset": 6
// }
// {
//   "topic": "dbserver1.public.targets",
//   "key": "{\"setting_id\":2,\"real_ip\":\"55.55.55.1\",\"port\":90}",
//   "value": "{\"before\":null,\"after\":{\"setting_id\":2,\"real_ip\":\"55.55.55.1\",\"port\":90},\"source\":{\"version\":\"2.3.4.Final\",\"connector\":\"postgresql\",\"name\":\"dbserver1\",\"ts_ms\":1763998524618,\"snapshot\":\"false\",\"db\":\"postgres\",\"sequence\":\"[null,\\\"24973432\\\"]\",\"schema\":\"public\",\"table\":\"targets\",\"txId\":760,\"lsn\":24973432,\"xmin\":null},\"op\":\"c\",\"ts_ms\":1763998525108,\"transaction\":null}",
//   "timestamp": 1763998525607,
//   "partition": 0,
//   "offset": 7
// }
