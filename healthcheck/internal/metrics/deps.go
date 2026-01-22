package metrics

import "time"

type Metrics interface {
	Increment(string)
	Duration(string, time.Duration)
	Gauge(string, int)
}
