package metrics

import (
	"time"

	statsd "github.com/smira/go-statsd"
)

type Statsd struct {
	client *statsd.Client
}

func NewStatsd(nodeName string, prefix string, addr string) Metrics {
	clnt := statsd.NewClient(
		addr,
		statsd.MetricPrefix("apps.nlb."),
		statsd.DefaultTags(statsd.StringTag("node", nodeName)),
	)
	return &Statsd{
		client: clnt,
	}
}

func (s *Statsd) Increment(metric string) {
	s.client.Incr(metric, 1)
}

func (s *Statsd) Duration(metric string, duration time.Duration) {
	s.client.PrecisionTiming(metric, duration)
}
func (s *Statsd) Gauge(metric string, value int) {
	s.client.Gauge(metric, int64(value))
}
