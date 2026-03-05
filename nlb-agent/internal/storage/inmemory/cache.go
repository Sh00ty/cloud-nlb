package inmemory

import (
	"context"
	"sync"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/reconciler"
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

func (c *InMemStateCache) SavePlacementVersion(ctx context.Context, ver uint64) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	changed := false
	if c.AssignmentVersion < ver {
		c.AssignmentVersion = ver
		changed = true
	}
	return changed, nil
}

func (c *InMemStateCache) SetDesiredSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec, ver uint64) (bool, error) {
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
	return true, nil
}

func (c *InMemStateCache) SetDesiredEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec, ver uint64) (bool, error) {
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
	return true, nil
}

func (c *InMemStateCache) DeleteDesired(ctx context.Context, tgIDs []models.TargetGroupID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, tgID := range tgIDs {
		delete(c.cache, tgID)
	}
	return nil
}

func (c *InMemStateCache) GetPlacement(ctx context.Context) (models.NodeState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tgStates := make(map[models.TargetGroupID]models.TargetGroupState, len(c.cache))
	for _, entry := range c.cache {
		tgStates[entry.entry.ID] = entry.entry
	}
	return models.NodeState{
		PlacementVersion:  c.AssignmentVersion,
		TargetGroupStates: tgStates,
	}, nil
}
func (c *InMemStateCache) GetDesiredEndpoints(tgID models.TargetGroupID) (*reconciler.VersionedEndpoints, bool) {
	return nil, false
}

func (c *InMemStateCache) GetDesiredSpec(tgID models.TargetGroupID) (*reconciler.VersionedSpec, bool) {
	return nil, false
}
