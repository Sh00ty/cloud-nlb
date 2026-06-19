package kafka

import "time"

type EndpointStatusDto struct {
	TargetGroup string    `json:"target_group"`
	RealIP      string    `json:"real_ip"`
	Port        int       `json:"port"`
	Vshard      int       `json:"vshard"`
	UpdatedAt   time.Time `json:"updated_at"`
	Status      bool      `json:"status"`
}

type Message[T any] struct {
	Value *Value[T] `json:"value"`
	Topic string    `json:"topic"`
	Key   T         `json:"key"`
}

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
