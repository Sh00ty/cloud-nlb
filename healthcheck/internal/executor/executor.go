package executor

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/models"
)

type Notifier interface {
	NotifyHcStatusChanged(models.HcEvent)
}

func NewExecutor(notifier Notifier, concurrency uint16, buffer uint32) *executor {
	channelBufferCapacity.Set(float64(buffer))
	return &executor{
		inputChan:   make(chan *models.HealthCheck, buffer),
		close:       make(chan struct{}),
		concurrency: concurrency,
		notifier:    notifier,
	}
}

type executor struct {
	concurrency uint16
	inputChan   chan *models.HealthCheck

	notifier Notifier

	// closed by atomic
	closed     int64
	inProgress int64
	close      chan struct{}
}

func (e *executor) Run() {
	for i := range e.concurrency {
		go func() {
			for task := range e.inputChan {
				channelBufferUsage.Set(float64(len(e.inputChan)))

				log.Debug().Msgf("executor [%d] received task: %+v", i, task.Target)

				ts := time.Now()
				changed := task.Executable.DoHealthCheckIteration()
				taskDuration.
					WithLabelValues(boolToStr(changed)).
					Observe(time.Since(ts).Seconds())

				if changed {
					newStatus, err := task.Executable.Info()
					e.notifier.NotifyHcStatusChanged(models.HcEvent{
						TargetGroup: task.Settings.TargetGroup,
						Target:      task.Target,
						HcInterval:  task.Settings.Interval,
						NewStatus:   newStatus,
						Error:       err,
					})
				}
			}
		}()
	}
}

func (e *executor) ExecuteHealthCheck(t *models.HealthCheck) error {
	if atomic.LoadInt64(&e.closed) == 1 {
		return fmt.Errorf("executor already closed")
	}
	atomic.AddInt64(&e.inProgress, 1)
	defer atomic.AddInt64(&e.inProgress, -1)

	select {
	case e.inputChan <- t:
		channelBufferUsage.Set(float64(len(e.inputChan)))
		return nil
	case <-e.close:
		return fmt.Errorf("failed to send task to executor: closed")
	}
}

func (e *executor) Close() {
	atomic.AddInt64(&e.closed, 1)
	close(e.close)
	for atomic.LoadInt64(&e.inProgress) != 0 {
		// тут очень небольшая вероятность, что кто-то будет in-progress
		runtime.Gosched()
	}
	close(e.inputChan)
}
