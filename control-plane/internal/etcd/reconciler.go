package etcd

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
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
	c.election = concurrency.NewElection(session, reconcilerLeadershipKey)

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
		log.Warn().Msgf("instance won leader election for %s", reconcilerLeadershipKey)
		return true, c.session.Done(), nil
	}
}

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

func (c *ReconcilerClient) GetAllDesiredSpecs(ctx context.Context) ([]*models.TargetGroupSpec, error) {
	return nil, nil
}
