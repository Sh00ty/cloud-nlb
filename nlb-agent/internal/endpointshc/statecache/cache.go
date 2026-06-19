package statecache

import (
	"context"
	"sync"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
)

type TgEndpointsStateCache struct {
	guard sync.RWMutex
	cache map[models.TargetGroupID]uint64
}

func New() *TgEndpointsStateCache {
	return &TgEndpointsStateCache{
		cache: make(map[models.TargetGroupID]uint64, 128),
	}
}

func (c *TgEndpointsStateCache) GetTgEndpointsVerState(ctx context.Context, tg models.TargetGroupID) (uint64, bool) {
	c.guard.Lock()
	defer c.guard.Unlock()

	ver, exists := c.cache[tg]
	return ver, exists
}

func (c *TgEndpointsStateCache) SetTgEndpointsVerState(
	ctx context.Context,
	tg models.TargetGroupID,
	stateVer uint64,
) {
	c.guard.Lock()
	defer c.guard.Unlock()

	c.cache[tg] = stateVer
}
