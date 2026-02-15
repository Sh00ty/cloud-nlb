package etcd

import (
	"context"
	"fmt"
	"path"

	"github.com/rs/zerolog/log"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

const (
	dplKeyPrefix = "/cloud-nlb-registry/data-planes"
)

func dplConfigs() string {
	return path.Join(dplKeyPrefix, "configs")
}

// TODO: in future
// func dplConfig(nodeID string) string {
// 	return path.Join(dplConfigs(), nodeID)
// }

func dplStatuses() string {
	return path.Join(dplKeyPrefix, "statuses")
}

func dplStatus(nodeID string) string {
	return path.Join(dplStatuses(), nodeID)
}

// it will be better to make cpl stubs with the same functionality, but
// for now it will be faster to make all in etcd
type Client struct {
	etcd       *clientv3.Client
	nodeID     string
	session    *concurrency.Session
	sessionTTL uint8
	leaseID    clientv3.LeaseID
}

func NewClient(ctx context.Context, host string, nodeID string, ttlUntilDeathInSec uint8) (*Client, error) {
	clnt, err := clientv3.New(clientv3.Config{
		Endpoints: []string{host},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}
	return &Client{
		etcd:       clnt,
		nodeID:     nodeID,
		sessionTTL: ttlUntilDeathInSec,
	}, nil
}

func (c *Client) Register(ctx context.Context) error {
	resp, err := c.etcd.KV.Get(ctx, dplStatus(c.nodeID))
	if err != nil {
		return fmt.Errorf("failed to get current data-plane status: %w", err)
	}
	var (
		leaseID = int64(0)
	)
	if len(resp.Kvs) != 0 {
		leaseID = resp.Kvs[0].Lease
	}
	err = c.acquireSession(ctx, leaseID)
	if err != nil {
		return fmt.Errorf("failed to acquire node etcd session: %w", err)
	}
	_, err = c.etcd.KV.Put(
		ctx,
		dplStatus(c.nodeID),
		"alive",
		clientv3.WithLease(c.leaseID),
	)
	if err != nil {
		return fmt.Errorf("failed to mark node as alive: %w", err)
	}
	return nil
}

func (c *Client) acquireSession(ctx context.Context, leaseId int64) error {
	opts := []concurrency.SessionOption{
		concurrency.WithContext(ctx),
		concurrency.WithTTL(int(c.sessionTTL)),
	}
	if leaseId != 0 {
		opts = append(opts, concurrency.WithLease(clientv3.LeaseID(leaseId)))
	}
	session, err := concurrency.NewSession(c.etcd, opts...)
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	c.session = session
	c.leaseID = session.Lease()
	return nil
}

func (c *Client) Close(ctx context.Context) error {
	err := c.session.Close()
	if err != nil {
		log.Error().Err(err).Msg("error during closing etcd session")
	}
	return c.etcd.Close()
}
