package sender

import (
	"context"
	"sync"
	"time"

	retry "github.com/avast/retry-go/v4"
	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/models"
)

type StatusRepository interface {
	UpdateTargetsStatuses(ctx context.Context, event []models.HcEvent) (int, error)
}

func NewSenderController(
	eventCh chan models.HcEvent,
	statusRepo StatusRepository,
	retryTimeout time.Duration,
) *SenderControler {
	return &SenderControler{
		events:      eventCh,
		statusRepo:  statusRepo,
		ttlTicker:   time.NewTicker(retryTimeout),
		unsentGuard: &sync.Mutex{},
		unsent:      make([]models.HcEvent, 0),
	}
}

type SenderControler struct {
	events      chan models.HcEvent
	ttlTicker   *time.Ticker
	statusRepo  StatusRepository
	unsentGuard *sync.Mutex
	unsent      []models.HcEvent
}

func (c *SenderControler) Run(ctx context.Context) {
	for {
		select {
		case _, ok := <-c.ttlTicker.C:
			if !ok {
				return
			}
			c.sendUnsentEvents(ctx)
		case event, ok := <-c.events:
			if !ok {
				return
			}
			start := time.Now()
			err := retry.Do(
				func() error {
					_, err := c.statusRepo.UpdateTargetsStatuses(ctx, []models.HcEvent{event})
					return err
				},
				retry.Attempts(3),
			)
			sendDuration.WithLabelValues(boolToStr(err == nil)).Observe(time.Since(start).Seconds())

			if err != nil {
				log.Error().Err(err).Msg("failed to save target update, put it into unsent queue")
				c.unsentGuard.Lock()
				c.unsent = append(c.unsent, event)
				unsentQueueSize.Set(float64(len(c.unsent)))
				c.unsentGuard.Unlock()
			}
		}
	}
}

func (c *SenderControler) sendUnsentEvents(ctx context.Context) {
	c.unsentGuard.Lock()
	defer c.unsentGuard.Unlock()

	if len(c.unsent) == 0 {
		return
	}

	start := time.Now()
	total := len(c.unsent)

	done, err := c.statusRepo.UpdateTargetsStatuses(ctx, c.unsent)
	if err != nil {
		log.Warn().Err(err).Msgf("failed to update unsent events: done %d", done)

		unsentResyncDuration.WithLabelValues("false").Observe(time.Since(start).Seconds())
		unsentResyncEventsTotal.WithLabelValues("sent").Add(float64(done))
		unsentResyncEventsTotal.WithLabelValues("remained").Add(float64(total - done))

		newUnsent := make([]models.HcEvent, total-done)
		copy(newUnsent, c.unsent[done:])
		c.unsent = newUnsent
		unsentQueueSize.Set(float64(len(c.unsent)))
		return
	}

	unsentResyncDuration.WithLabelValues("true").Observe(time.Since(start).Seconds())
	unsentResyncEventsTotal.WithLabelValues("sent").Add(float64(total))

	c.unsent = c.unsent[:0]
	unsentQueueSize.Set(float64(0))
}
