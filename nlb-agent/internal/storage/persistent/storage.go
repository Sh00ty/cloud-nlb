package persistent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/reconciler"
	"github.com/rs/zerolog"
	"go.etcd.io/bbolt"
)

var (
	bucketMeta      = []byte("meta")
	bucketTGSpec    = []byte("tg_spec")
	bucketEndpoints = []byte("tg_endpoints")

	metaPlacementVersion = []byte("placement_version")
)

const (
	prefixDesired = "desired:"
	prefixActual  = "actual:"
)

// Storage provides persistent caching for reconciliation state.
//
// Stores both "desired" (from control-plane) and "actual" (applied to VPP)
// versions of each target group's spec and endpoints.
//
// All writes go to bbolt first, then update in-memory cache.
// Reads are served from in-memory cache populated on startup.
type Storage struct {
	db  *bbolt.DB
	log zerolog.Logger

	mu    sync.RWMutex
	cache *stateCache
}

type stateCache struct {
	placementVersion uint64

	desiredSpecs     map[models.TargetGroupID]reconciler.VersionedSpec
	actualSpecs      map[models.TargetGroupID]reconciler.VersionedSpec
	desiredEndpoints map[models.TargetGroupID]reconciler.VersionedEndpoints
	actualEndpoints  map[models.TargetGroupID]reconciler.VersionedEndpoints
}

type TargetGroupView struct {
	ID models.TargetGroupID

	DesiredSpec *reconciler.VersionedSpec
	ActualSpec  *reconciler.VersionedSpec

	DesiredEndpoints *reconciler.VersionedEndpoints
	ActualEndpoints  *reconciler.VersionedEndpoints
}

func newStateCache() *stateCache {
	return &stateCache{
		desiredSpecs:     make(map[models.TargetGroupID]reconciler.VersionedSpec),
		actualSpecs:      make(map[models.TargetGroupID]reconciler.VersionedSpec),
		desiredEndpoints: make(map[models.TargetGroupID]reconciler.VersionedEndpoints),
		actualEndpoints:  make(map[models.TargetGroupID]reconciler.VersionedEndpoints),
	}
}

func New(storagePath string, log zerolog.Logger) (res *Storage, err error) {
	err = os.Mkdir(path.Dir(storagePath), 0600)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("failed to create base db directory: %w", err)
	}

	db, err := bbolt.Open(storagePath, 0600, &bbolt.Options{
		Timeout:      500 * time.Millisecond,
		NoGrowSync:   false,
		FreelistType: bbolt.FreelistArrayType,
	})
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	defer func() {
		if err != nil {
			db.Close()
		}
	}()

	err = db.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{bucketMeta, bucketTGSpec, bucketEndpoints} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	s := &Storage{
		db:    db,
		log:   log.With().Str("component", "persistent_storage").Logger(),
		cache: newStateCache(),
	}
	if err := s.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("load from disk: %w", err)
	}
	s.log.Info().
		Uint64("placement_version", s.cache.placementVersion).
		Int("desired_specs", len(s.cache.desiredSpecs)).
		Int("actual_specs", len(s.cache.actualSpecs)).
		Int("desired_endpoints", len(s.cache.desiredEndpoints)).
		Int("actual_endpoints", len(s.cache.actualEndpoints)).
		Msg("storage loaded from disk")

	return s, nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func (s *Storage) GetPlacementVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.cache.placementVersion
}

func (s *Storage) SavePlacementVersion(_ context.Context, version uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if version <= s.cache.placementVersion {
		return false, nil
	}
	data, err := json.Marshal(version)
	if err != nil {
		return false, fmt.Errorf("marshal placement version: %w", err)
	}
	if err := s.putRaw(bucketMeta, metaPlacementVersion, data); err != nil {
		return false, err
	}
	s.cache.placementVersion = version
	return true, nil
}

func (s *Storage) SetDesiredSpec(_ context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec, version uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.cache.desiredSpecs[tgID]; ok && existing.Version >= version {
		return false, nil
	}
	entry := reconciler.VersionedSpec{Version: version, Spec: spec}
	if err := s.putJSON(bucketTGSpec, desiredKey(tgID), entry); err != nil {
		return false, err
	}
	s.cache.desiredSpecs[tgID] = entry
	return true, nil
}

func (s *Storage) GetDesiredSpec(tgID models.TargetGroupID) (*reconciler.VersionedSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.cache.desiredSpecs[tgID]
	if !ok {
		return nil, false
	}
	return &v, true
}

func (s *Storage) SetActualSpec(_ context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec, version uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := reconciler.VersionedSpec{Version: version, Spec: spec}
	// actual spec exists only in memory of agent
	s.cache.actualSpecs[tgID] = entry
	return nil
}

func (s *Storage) GetActualSpec(_ context.Context, tgID models.TargetGroupID) (*reconciler.VersionedSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.cache.actualSpecs[tgID]
	if !ok {
		return nil, false
	}
	return &v, true
}

func (s *Storage) SetDesiredEndpoints(_ context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec, version uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.cache.desiredEndpoints[tgID]; ok && existing.Version >= version {
		return false, nil
	}
	entry := reconciler.VersionedEndpoints{Version: version, Endpoints: endpoints}
	if err := s.putJSON(bucketEndpoints, desiredKey(tgID), entry); err != nil {
		return false, err
	}
	s.cache.desiredEndpoints[tgID] = entry
	return true, nil
}

func (s *Storage) GetDesiredEndpoints(ctx context.Context, tgID models.TargetGroupID) (*reconciler.VersionedEndpoints, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.cache.desiredEndpoints[tgID]
	if !ok {
		return nil, false
	}
	return &v, true
}

func (s *Storage) SetActualEndpoints(_ context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec, version uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := reconciler.VersionedEndpoints{Version: version, Endpoints: endpoints}
	s.cache.actualEndpoints[tgID] = entry
	return nil
}

func (s *Storage) GetActualEndpoints(_ context.Context, tgID models.TargetGroupID) (*reconciler.VersionedEndpoints, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	v, ok := s.cache.actualEndpoints[tgID]
	if !ok {
		return nil, false
	}
	return &v, true
}

// DeleteDesired removes all desired state for a target group.
// After this, reconciler will see actual-without-desired and trigger removal from VPP.
func (s *Storage) DeleteDesired(_ context.Context, tgIDs []models.TargetGroupID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.Update(func(tx *bbolt.Tx) error {
		specBucket := tx.Bucket(bucketTGSpec)
		epBucket := tx.Bucket(bucketEndpoints)
		for _, tgID := range tgIDs {
			if err := specBucket.Delete(desiredKey(tgID)); err != nil {
				return fmt.Errorf("delete desired spec: %w", err)
			}
			if err := epBucket.Delete(desiredKey(tgID)); err != nil {
				return fmt.Errorf("delete desired endpoints: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, tgID := range tgIDs {
		delete(s.cache.desiredSpecs, tgID)
		delete(s.cache.desiredEndpoints, tgID)
	}
	return nil
}

// DeleteActual removes all actual state for a target group.
// Called after successful removal from VPP.
func (s *Storage) DeleteActual(_ context.Context, tgID models.TargetGroupID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.cache.actualSpecs, tgID)
	delete(s.cache.actualEndpoints, tgID)
	return nil
}

func (s *Storage) GetTargetGroupView(tgID models.TargetGroupID) TargetGroupView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	view := TargetGroupView{ID: tgID}

	if v, ok := s.cache.desiredSpecs[tgID]; ok {
		cp := v
		view.DesiredSpec = &cp
	}
	if v, ok := s.cache.actualSpecs[tgID]; ok {
		cp := v
		view.ActualSpec = &cp
	}
	if v, ok := s.cache.desiredEndpoints[tgID]; ok {
		cp := v
		view.DesiredEndpoints = &cp
	}
	if v, ok := s.cache.actualEndpoints[tgID]; ok {
		cp := v
		view.ActualEndpoints = &cp
	}
	return view
}

func (s *Storage) GetAllTargetGroupIDs() []models.TargetGroupID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[models.TargetGroupID]struct{})

	for id := range s.cache.desiredSpecs {
		seen[id] = struct{}{}
	}
	for id := range s.cache.actualSpecs {
		seen[id] = struct{}{}
	}

	result := make([]models.TargetGroupID, 0, len(seen))
	for id := range seen {
		result = append(result, id)
	}
	return result
}

func (s *Storage) GetAllTargetGroupViews() []TargetGroupView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[models.TargetGroupID]struct{})
	for id := range s.cache.desiredSpecs {
		seen[id] = struct{}{}
	}
	for id := range s.cache.actualSpecs {
		seen[id] = struct{}{}
	}

	views := make([]TargetGroupView, 0, len(seen))
	for id := range seen {
		// Inline to avoid re-locking.
		view := TargetGroupView{ID: id}
		if v, ok := s.cache.desiredSpecs[id]; ok {
			cp := v
			view.DesiredSpec = &cp
		}
		if v, ok := s.cache.actualSpecs[id]; ok {
			cp := v
			view.ActualSpec = &cp
		}
		if v, ok := s.cache.desiredEndpoints[id]; ok {
			cp := v
			view.DesiredEndpoints = &cp
		}
		if v, ok := s.cache.actualEndpoints[id]; ok {
			cp := v
			view.ActualEndpoints = &cp
		}
		views = append(views, view)
	}
	return views
}

func (s *Storage) GetPlacement(ctx context.Context) (models.NodeState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tgStates := make(map[models.TargetGroupID]models.TargetGroupState, len(s.cache.desiredSpecs))

	for id, spec := range s.cache.desiredSpecs {
		state := models.TargetGroupState{
			ID:          id,
			SpecVersion: spec.Version,
		}
		if ep, ok := s.cache.desiredEndpoints[id]; ok {
			state.EndpointVersion = ep.Version
		}
		tgStates[id] = state
	}

	return models.NodeState{
		PlacementVersion:  s.cache.placementVersion,
		TargetGroupStates: tgStates,
	}, nil
}

func (s *Storage) putJSON(bucket []byte, key []byte, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return s.putRaw(bucket, key, data)
}

func (s *Storage) putRaw(bucket []byte, key []byte, data []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return fmt.Errorf("bucket %s not found", bucket)
		}
		return b.Put(key, data)
	})
}

func desiredKey(tgID models.TargetGroupID) []byte {
	return []byte(prefixDesired + string(tgID))
}

func isDesiredKey(key []byte) bool {
	return len(key) > len(prefixDesired) && string(key[:len(prefixDesired)]) == prefixDesired
}

func isActualKey(key []byte) bool {
	return len(key) > len(prefixActual) && string(key[:len(prefixActual)]) == prefixActual
}

func tgIDFromKey(key []byte, prefix string) models.TargetGroupID {
	return models.TargetGroupID(key[len(prefix):])
}

func (s *Storage) loadFromDisk() error {
	return s.db.View(func(tx *bbolt.Tx) error {
		// Load placement version.
		if err := s.loadPlacementVersion(tx); err != nil {
			return fmt.Errorf("load placement version: %w", err)
		}

		// Load specs.
		if err := s.loadBucket(tx, bucketTGSpec, s.loadSpecEntry); err != nil {
			return fmt.Errorf("load specs: %w", err)
		}

		// Load endpoints.
		if err := s.loadBucket(tx, bucketEndpoints, s.loadEndpointsEntry); err != nil {
			return fmt.Errorf("load endpoints: %w", err)
		}

		return nil
	})
}

func (s *Storage) loadPlacementVersion(tx *bbolt.Tx) error {
	b := tx.Bucket(bucketMeta)
	if b == nil {
		return nil
	}

	data := b.Get(metaPlacementVersion)
	if data == nil {
		return nil
	}

	var version uint64
	if err := json.Unmarshal(data, &version); err != nil {
		return fmt.Errorf("unmarshal placement version: %w", err)
	}

	s.cache.placementVersion = version
	return nil
}

type entryLoader func(key []byte, value []byte) error

func (s *Storage) loadBucket(tx *bbolt.Tx, bucketName []byte, loader entryLoader) error {
	b := tx.Bucket(bucketName)
	if b == nil {
		return nil
	}

	return b.ForEach(func(k, v []byte) error {
		if err := loader(k, v); err != nil {
			s.log.Warn().
				Err(err).
				Str("bucket", string(bucketName)).
				Str("key", string(k)).
				Msg("skipping corrupted entry")
		}
		return nil
	})
}

func (s *Storage) loadSpecEntry(key []byte, value []byte) error {
	var entry reconciler.VersionedSpec
	if err := json.Unmarshal(value, &entry); err != nil {
		return fmt.Errorf("unmarshal spec: %w", err)
	}

	if isDesiredKey(key) {
		id := tgIDFromKey(key, prefixDesired)
		s.cache.desiredSpecs[id] = entry
	} else if isActualKey(key) {
		id := tgIDFromKey(key, prefixActual)
		s.cache.actualSpecs[id] = entry
	}

	return nil
}

func (s *Storage) loadEndpointsEntry(key []byte, value []byte) error {
	var entry reconciler.VersionedEndpoints
	if err := json.Unmarshal(value, &entry); err != nil {
		return fmt.Errorf("unmarshal endpoints: %w", err)
	}

	if isDesiredKey(key) {
		id := tgIDFromKey(key, prefixDesired)
		s.cache.desiredEndpoints[id] = entry
	} else if isActualKey(key) {
		id := tgIDFromKey(key, prefixActual)
		s.cache.actualEndpoints[id] = entry
	}

	return nil
}
