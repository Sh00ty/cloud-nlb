package mockhc

import (
	"time"
)

type MockHCSettings struct {
	Name     string
	Duration time.Duration
}

type MockHC struct {
	name     string
	duration time.Duration
	last     time.Time
}

func NewMockHC(settings *MockHCSettings) *MockHC {
	return &MockHC{
		name:     settings.Name,
		duration: settings.Duration,
		last:     time.Now(),
	}
}

func (h *MockHC) DoHealthCheck() (bool, error) {
	// log.Debug().Msgf("start executing hc %s since %s", h.name, time.Since(h.last))

	h.last = time.Now()
	time.Sleep(h.duration)
	return true, nil
}
