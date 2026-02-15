package apiruntime

import (
	"context"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/cespare/xxhash/v2"
)

type ChannelNotifier struct {
	id       uint64
	ch       chan struct{}
	closed   atomic.Bool
	deadline atomic.Value
}

func (ar *ApiRuntime) GetNotifier(nodeID models.DataPlaneID, deadline time.Time) Notifier {
	var (
		s              = strconv.FormatUint(ar.notifierIdSaltGen.Add(1), 10)
		hash           = xxhash.Sum64String(string(nodeID) + s)
		atomicDeadline = atomic.Value{}
	)

	atomicDeadline.Store(deadline)
	return &ChannelNotifier{
		id:       hash,
		ch:       make(chan struct{}),
		closed:   atomic.Bool{},
		deadline: atomicDeadline,
	}
}

func (c *ChannelNotifier) ID() uint64 {
	return c.id
}
func (c *ChannelNotifier) Notify(ctx context.Context) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	close(c.ch)
}
func (c *ChannelNotifier) Wait(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-c.ch:
		return true
	}
}
func (c *ChannelNotifier) Expire() {
	c.deadline.Store(time.Time{})
}
func (c *ChannelNotifier) IsExpired() bool {
	return c.deadline.Load().(time.Time).Before(time.Now())
}
