package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/api/apiruntime"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/rs/zerolog/log"
)

type ApiClient struct {
	etcd *clientv3.Client
}

func NewApiClient(ctx context.Context, host string) (*ApiClient, error) {
	clnt, err := clientv3.New(clientv3.Config{
		Endpoints: []string{host},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}
	return &ApiClient{etcd: clnt}, nil
}

func (c *ApiClient) GracefulClose(ctx context.Context) error {
	return c.Close()
}

func (c *ApiClient) Close() error {
	err := c.etcd.Close()
	if err != nil {
		log.Error().Err(err).Msg("failed to close etcd client")
		return err
	}
	return nil
}

func (c *ApiClient) Client() *clientv3.Client {
	return c.etcd
}

func (c *ApiClient) Watch(
	ctx context.Context,
	key string,
	opts ...clientv3.OpOption,
) clientv3.WatchChan {
	return c.etcd.Watch(ctx, key, opts...)
}

func (c *ApiClient) RequestProgress(ctx context.Context) error {
	return c.etcd.RequestProgress(ctx)
}

func (c *ApiClient) ExecuteIncrementally(
	ctx context.Context,
	timeStampKey string,
	opFunc func(timeStampValue uint64) []clientv3.Op,
) error {
	resp, err := c.etcd.Get(ctx, timeStampKey)
	if err != nil {
		return fmt.Errorf("failed to get %s timestamp: %w", timeStampKey, err)
	}
	if len(resp.Kvs) < 1 {
		return fmt.Errorf("%s timestamp not found", timeStampKey)
	}
	tgOldTimestamp, err := strconv.ParseUint(string(resp.Kvs[0].Value), 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse %s timestamp: %w", timeStampKey, err)
	}

	for range maxTxRetryAttempts {
		targetNewTimestamp := tgOldTimestamp + 1
		opsOnIter := append(
			opFunc(targetNewTimestamp),
			clientv3.OpPut(
				timeStampKey,
				strconv.FormatUint(targetNewTimestamp, 10),
			),
		)

		tx := c.etcd.Txn(ctx).If(
			clientv3.Compare(
				clientv3.Value(timeStampKey),
				"=",
				strconv.FormatUint(tgOldTimestamp, 10),
			),
		).Then(
			opsOnIter...,
		).Else(
			clientv3.OpGet(timeStampKey),
		)
		resp, err := tx.Commit()
		if err != nil {
			return fmt.Errorf("failed to add event into eventlog: %w", err)
		}
		if resp.Succeeded {
			return nil
		}
		tgTimestampStr, err := extractStringFromTxnResponse(resp)
		if err != nil {
			return fmt.Errorf("failed to extract current timestamp %s: %w", timeStampKey, err)
		}
		lastTgTimestamp, err := strconv.ParseUint(tgTimestampStr, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse current timestamp %s: %w", timeStampKey, err)
		}
		tgOldTimestamp = lastTgTimestamp
	}
	return fmt.Errorf(
		"failed to execute ops with key increment for ts %s: max attempts count exceeded",
		timeStampKey,
	)
}

func (c *ApiClient) SetTargetGroupSpec(ctx context.Context, tg models.TargetGroupSpec) error {
	const firstEventTimestamp = "0"

	tx := c.etcd.Txn(ctx).If(
		clientv3.Compare(
			clientv3.CreateRevision(tgSpecTimestamp(tg.ID)), "=", 0,
		),
	).Then(
		clientv3.OpPut(tgSpecTimestamp(tg.ID), firstEventTimestamp),
		clientv3.OpPut(tgSpecDesiredVersion(tg.ID, 0), "{}"),
		clientv3.OpPut(tgSpecDesiredLatest(tg.ID), "{}"),
		clientv3.OpPut(
			tgSpecCurrent(tg.ID),
			mustJsonMarshal(targetGroupSpec{Time: time.Now()}),
		),

		clientv3.OpPut(tgEndpointsTimestamp(tg.ID), firstEventTimestamp),
		clientv3.OpPut(tgEndpointsCompacted(tg.ID), "[]"),
		clientv3.OpPut(tgEndpointsEvent(tg.ID, 0), nopEvent),

		clientv3.OpPut(tgAssignedDataPlanes(tg.ID), "[]"),
	)
	resp, err := tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to create target group: %w", err)
	}
	if resp.Succeeded {
		log.Info().Msgf("created brand new target group %s", tg.ID)
	}
	setSpecDesired := func(timestamp uint64) []clientv3.Op {
		spec := targetGroupSpec{
			Version: timestamp,
			VIP:     tg.VirtualIP.String(),
			Port:    tg.Port,
			Proto:   tg.Proto,
			Time:    time.Now(),
		}
		specBytes := mustJsonMarshal(spec)
		return []clientv3.Op{
			clientv3.OpPut(tgSpecDesiredVersion(tg.ID, timestamp), specBytes),
			clientv3.OpPut(tgSpecDesiredLatest(tg.ID), specBytes),
		}
	}

	err = c.ExecuteIncrementally(ctx, tgSpecTimestamp(tg.ID), setSpecDesired)
	if err != nil {
		return fmt.Errorf("failed to set target group desired spec: %w", err)
	}
	return nil
}

func (c *ApiClient) getEndpointEventOpFunc(
	eventType models.EventType,
	tgID models.TargetGroupID,
	ep models.EndpointSpec,
) func(timestamp uint64) []clientv3.Op {
	return func(timestamp uint64) []clientv3.Op {
		epEventSpec := endpointLogEntry{
			Type:          eventType,
			TargetGroupID: tgID,
			Timestamp:     timestamp,
			Time:          time.Now(),
			Endpoint: endpointSpec{
				IP:     ep.IP.String(),
				Port:   ep.Port,
				Weight: ep.Weight,
			},
		}
		epEventSpecBytes := mustJsonMarshal(epEventSpec)
		return []clientv3.Op{
			clientv3.OpPut(tgEndpointsEvent(tgID, timestamp), epEventSpecBytes),
		}
	}
}

func (c *ApiClient) AddEndpoint(
	ctx context.Context,
	tgID models.TargetGroupID,
	ep models.EndpointSpec,
) error {
	addEpFunc := c.getEndpointEventOpFunc(models.EventTypeAddEndpoint, tgID, ep)
	err := c.ExecuteIncrementally(ctx, tgEndpointsTimestamp(tgID), addEpFunc)
	if err != nil {
		return fmt.Errorf("failed to add endpoint addition event into changelog: %w", err)
	}
	return nil
}

func (c *ApiClient) RemoveEndpoint(
	ctx context.Context,
	tgID models.TargetGroupID,
	ep models.EndpointSpec,
) error {
	addEpFunc := c.getEndpointEventOpFunc(models.EventTypeRemoveEndpoint, tgID, ep)
	err := c.ExecuteIncrementally(ctx, tgEndpointsTimestamp(tgID), addEpFunc)
	if err != nil {
		return fmt.Errorf("failed to add endpoint removing event into changelog: %w", err)
	}
	return nil
}

func (c *ApiClient) GetTargetGroupDiff(
	ctx context.Context,
	current apiruntime.TargetGroupState,
) (models.TargetGroup, error) {
	// here we don't need consistency cause all event version and states can go only further (better for us)
	// TODO: changelog support
	result := models.TargetGroup{
		Spec: models.TargetGroupSpec{
			ID: current.TgID,
		},
		SpecVersion:         current.SpecVersion,
		EndpointVersion:     current.EndpointVersion,
		SnapshotLastVersion: current.SnapshotLastVersion,
	}

	specResp, err := c.etcd.KV.Get(ctx, tgSpecDesiredLatest(current.TgID))
	if err != nil {
		return models.TargetGroup{}, fmt.Errorf("failed to get target group spec from etcd: %w", err)
	}
	if len(specResp.Kvs) == 0 {
		return models.TargetGroup{}, fmt.Errorf("not found target group %s", current.TgID)
	}
	specDto := targetGroupSpec{}
	err = json.Unmarshal(specResp.Kvs[0].Value, &specDto)
	if err != nil {
		return models.TargetGroup{}, fmt.Errorf("failed to unmarshal target group spec: %w", err)
	}
	if specDto.Version > current.SpecVersion {
		result.Spec = models.TargetGroupSpec{
			ID:        current.TgID,
			Proto:     specDto.Proto,
			Port:      specDto.Port,
			VirtualIP: net.ParseIP(specDto.VIP),
			Time:      specDto.Time,
		}
		result.SpecVersion = specDto.Version
	}

	events, err := c.getEndpointChangelogSinceVersion(ctx, current.TgID, current.EndpointVersion)
	if err != nil {
		return models.TargetGroup{}, fmt.Errorf("failed to fetch changelog for tg: %w", err)
	}
	if len(events) == 0 {
		return result, nil
	}
	for _, ev := range events {
		modelEv := models.EndpointEvent{
			Type:           ev.Type,
			TargetGroupID:  ev.TargetGroupID,
			DesiredVersion: ev.Timestamp,
			Time:           ev.Time,
			Spec: models.EndpointSpec{
				IP:     net.ParseIP(ev.Endpoint.IP),
				Port:   ev.Endpoint.Port,
				Weight: ev.Endpoint.Weight,
			},
		}
		result.EndpointVersion = ev.Timestamp
		result.EndpointsChangelog = append(result.EndpointsChangelog, modelEv)
	}
	return result, nil
}

func (c *ApiClient) getEndpointChangelogSinceVersion(ctx context.Context, tgID models.TargetGroupID, ver uint64) ([]endpointLogEntry, error) {
	var (
		result   = make([]endpointLogEntry, 0, 128)
		prefix   = tgEndpointsLogFolder(tgID)
		startKey = tgEndpointsEvent(tgID, ver) + "0"
	)
	for {
		resp, err := c.etcd.KV.Get(
			ctx,
			startKey,
			clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend),
			clientv3.WithFromKey(),
			clientv3.WithLimit(256),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get pending events: %w", err)
		}
		if resp.Count == 0 {
			return result, nil
		}
		for _, kv := range resp.Kvs {
			if !strings.HasPrefix(string(kv.Key), prefix) {
				return result, nil
			}
			ev := endpointLogEntry{}
			err = json.Unmarshal(kv.Value, &ev)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal endpoint event: %w", err)
			}
			startKey = string(append(kv.Key, 0))
			result = append(result, ev)
		}
		if !resp.More {
			return result, nil
		}
	}

}
