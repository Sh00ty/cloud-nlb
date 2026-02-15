package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/rs/zerolog/log"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
)

type ReconcilerClient struct {
	nodeID   string
	etcd     *clientv3.Client
	session  *concurrency.Session
	election *concurrency.Election
}

func NewReconcilerClient(
	ctx context.Context,
	etcdHosts []string,
	nodeID string,
) (*ReconcilerClient, error) {
	clnt, err := clientv3.New(clientv3.Config{
		Endpoints: etcdHosts,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}
	return &ReconcilerClient{
		nodeID: nodeID,
		etcd:   clnt,
	}, nil
}

func (c *ReconcilerClient) GracefulClose(ctx context.Context) error {
	err := c.election.Resign(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to gracefully resign leader")
	}
	return c.Close()
}

func (c *ReconcilerClient) Close() error {
	err := c.session.Close()
	if err != nil {
		log.Error().Err(err).Msg("failed to destroy session")
	}
	err = c.etcd.Close()
	if err != nil {
		log.Error().Err(err).Msg("failed to close etcd client")
		return err
	}
	return nil
}

func (c *ReconcilerClient) Client() *clientv3.Client {
	return c.etcd
}

func (c *ReconcilerClient) Watch(
	ctx context.Context,
	key string,
	opts ...clientv3.OpOption,
) clientv3.WatchChan {
	return c.etcd.Watch(ctx, key, opts...)
}

func (c *ReconcilerClient) RequestProgress(ctx context.Context) error {
	return c.etcd.RequestProgress(ctx)
}

func (c *ReconcilerClient) BecomeLeader(ctx context.Context) (bool, <-chan struct{}, error) {
	session, err := concurrency.NewSession(
		c.etcd,
		concurrency.WithContext(ctx),
		concurrency.WithTTL(leaderLeaseTTLInSeconds),
	)
	if err != nil {
		return false, nil, fmt.Errorf("failed to create session: %w", err)
	}
	c.session = session
	c.election = concurrency.NewElection(session, ReconcilerLeadershipKey)

	for {
		err = c.election.Campaign(ctx, c.nodeID)
		if errors.Is(err, concurrency.ErrElectionNotLeader) {
			continue
		}
		if errors.Is(err, context.Canceled) {
			return false, nil, nil
		}
		if err != nil {
			return false, nil, err
		}
		log.Warn().Msgf("instance won leader election for %s", ReconcilerLeadershipKey)
		return true, c.session.Done(), nil
	}
}

// TODO: for compaction
// func (c *ReconcilerClient) GetAllPendingOperations(ctx context.Context) ([]*models.Event, error) {
// 	var (
// 		result   = make([]*models.Event, 0, 128)
// 		startKey = pendingEvents()
// 	)
// 	for {
// 		resp, err := c.etcd.KV.Get(
// 			ctx,
// 			startKey,
// 			clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend),
// 			clientv3.WithFromKey(),
// 			clientv3.WithLimit(256),
// 		)
// 		if err != nil {
// 			return nil, fmt.Errorf("failed to get pending events: %w", err)
// 		}
// 		if resp.Count == 0 {
// 			return result, nil
// 		}
// 		for _, kv := range resp.Kvs {
// 			event, err := c.parseEventFromKV(ctx, kv)
// 			if err != nil {
// 				if errors.Is(err, ErrParseKey) {
// 					return result, nil
// 				}
// 				return nil, fmt.Errorf("failed to parse event from kv: %w", err)
// 			}
// 			startKey = string(append(kv.Key, 0))
// 			result = append(result, event)
// 		}
// 		if !resp.More {
// 			return result, nil
// 		}
// 	}
// }

func (c *ReconcilerClient) DataPlaneStatusesInitialSync(
	ctx context.Context,
	handler WatchHandler,
) ([]models.DataPlaneState, *Watcher, error) {
	resp, err := c.etcd.KV.Get(
		ctx,
		DataPlaneStatuses,
		clientv3.WithPrefix(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current data-planes statuses: %w", err)
	}
	var (
		states  = make([]models.DataPlaneState, 0, len(resp.Kvs))
		lastRev = int64(0)
	)
	for _, kv := range resp.Kvs {
		lastRev = max(kv.ModRevision, lastRev)

		nodeID, _ := strings.CutPrefix(string(kv.Key), DataPlaneStatuses+"/")
		states = append(states, models.DataPlaneState{
			ID:     models.DataPlaneID(nodeID),
			Status: models.DataPlaneStatus(kv.Value),
		})
	}

	if lastRev != 0 {
		lastRev++
	}
	w := NewWatcher(DataPlaneStatuses, handler, c.etcd.Watcher, lastRev)
	return states, w, nil
}

func (c *ReconcilerClient) TargetGroupsInitialSync(
	ctx context.Context,
	handler WatchHandler,
) ([]models.TargetGroupID, *Watcher, error) {
	resp, err := c.etcd.KV.Get(
		ctx,
		TgSpecDesiredLatestFolder(),
		clientv3.WithPrefix(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current data-planes statuses: %w", err)
	}
	var (
		tgIDs   = make([]models.TargetGroupID, 0, len(resp.Kvs))
		lastRev = int64(0)
	)
	for _, kv := range resp.Kvs {
		lastRev = max(kv.ModRevision, lastRev)

		tgID, ok := strings.CutPrefix(string(kv.Key), TgSpecDesiredLatestFolder()+"/")
		if !ok {
			return nil, nil, fmt.Errorf("failed to parse tg-id in key %s", string(kv.Key))
		}
		tgIDs = append(tgIDs, models.TargetGroupID(tgID))
	}
	if lastRev != 0 {
		lastRev++
	}
	w := NewWatcher(TgSpecDesiredLatestFolder(), handler, c.etcd.Watcher, lastRev)
	return tgIDs, w, nil
}

type dataplanePlacementDto struct {
	Version      uint64                            `json:"version"`
	Revision     uint64                            `json:"revision"`
	TargetGroups map[models.TargetGroupID]struct{} `json:"target_groups"`
}

func GetDataPlanesPlacements(
	clnt *clientv3.Client,
	ctx context.Context,
	handler WatchHandler,
) (map[models.DataPlaneID]models.Placement, *Watcher, error) {
	resp, err := clnt.Get(
		ctx,
		DataPlanePlacements,
		clientv3.WithPrefix(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current data-planes statuses: %w", err)
	}

	var (
		states  = make(map[models.DataPlaneID]models.Placement, len(resp.Kvs))
		lastRev = int64(0)
	)

	for _, kv := range resp.Kvs {
		nodeID, _ := strings.CutPrefix(string(kv.Key), DataPlanePlacements+"/")
		dto := dataplanePlacementDto{}
		err = json.Unmarshal(kv.Value, &dto)
		if err != nil {
			return nil, nil, fmt.Errorf("unmarshaling dpl placement info: %w", err)
		}
		lastRev = max(kv.ModRevision, lastRev)

		states[models.DataPlaneID(nodeID)] = models.Placement{
			Version:      dto.Version,
			TargetGroups: dto.TargetGroups,
		}
	}
	if handler == nil {
		return states, nil, nil
	}
	if lastRev != 0 {
		lastRev++
	}
	return states, NewWatcher(DataPlanePlacements, handler, clnt.Watcher, lastRev), nil
}

func (c *ReconcilerClient) UpdatePlacements(ctx context.Context, ids []models.DataPlaneID, pls []models.Placement) error {
	txOps := make([]clientv3.Op, 0, 128)

	for i, dplID := range ids {
		txOps = append(txOps,
			clientv3.OpPut(
				path.Join(DataPlanePlacements, string(dplID)),
				mustJsonMarshal(dataplanePlacementDto{
					Version:      pls[i].Version,
					TargetGroups: pls[i].TargetGroups,
				}),
			),
		)
	}
	tx := c.etcd.Txn(ctx).If(
		clientv3.Compare(clientv3.Value(c.election.Key()), "=", c.nodeID),
		clientv3.Compare(clientv3.LeaseValue(c.election.Key()), ">", 0),
	).Then(
		txOps...,
	)
	_, err := tx.Commit()
	if err != nil {
		return fmt.Errorf("committing dpl placement update tx: %w", err)
	}
	return nil
}
