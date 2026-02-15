package inmemory

import (
	"context"
	"sync"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
)

type cacheEntry struct {
	mu    sync.Mutex
	entry models.TargetGroupState
}

type InMemStateCache struct {
	AssignmentVersion uint64
	cache             map[models.TargetGroupID]*cacheEntry
	mu                *sync.Mutex
}

func NewInMemoryState() *InMemStateCache {
	return &InMemStateCache{
		cache: make(map[models.TargetGroupID]*cacheEntry, 128),
		mu:    &sync.Mutex{},
	}
}

func (c *InMemStateCache) SetAssignment(ctx context.Context, ver uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.AssignmentVersion < ver {
		c.AssignmentVersion = ver
	}
	return nil
}

func (c *InMemStateCache) SetTargetGroupSpecVersion(ctx context.Context, tgID models.TargetGroupID, ver uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.cache[tgID]
	if entry == nil {
		entry = new(cacheEntry)
	}
	entry.entry.ID = tgID
	if entry.entry.SpecVersion < ver {
		entry.entry.SpecVersion = ver
	}
	c.cache[tgID] = entry
	return nil
}

func (c *InMemStateCache) SetTargetGroupEndpointVersion(ctx context.Context, tgID models.TargetGroupID, ver uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := c.cache[tgID]
	if entry == nil {
		entry = new(cacheEntry)
	}
	entry.entry.ID = tgID
	if entry.entry.EndpointVersion < ver {
		entry.entry.EndpointVersion = ver
	}
	c.cache[tgID] = entry
	return nil
}

func (c *InMemStateCache) RemoveTargetGroup(ctx context.Context, tgID models.TargetGroupID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, tgID)
	return nil
}

func (c *InMemStateCache) GetAllTargetGroupsStates(ctx context.Context) (uint64, []models.TargetGroupState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make([]models.TargetGroupState, 0, len(c.cache))
	for _, entry := range c.cache {
		result = append(result, entry.entry)
	}
	return c.AssignmentVersion, result, nil
}
