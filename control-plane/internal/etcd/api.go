package etcd

import (
	"context"
	"fmt"
	"strconv"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
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

func (c *ApiClient) AddEndpointIntoEventlog(
	ctx context.Context,
	tgID models.TargetGroupID,
	eventType models.EventType,
	eventPayload endpointSpec,
) error {
	timestampKey := tgEndpointsTimestamp(tgID)
	resp, err := c.etcd.Get(ctx, timestampKey)
	if err != nil {
		return fmt.Errorf("failed to get tg %s timestamp: %w", tgID, err)
	}
	if len(resp.Kvs) < 1 {
		return fmt.Errorf("tg %s timestamp not found", tgID)
	}
	tgOldTimestamp, err := strconv.ParseUint(string(resp.Kvs[0].Value), 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse tg %s timestamp: %w", tgID, err)
	}

	for range maxTxRetryAttempts {
		targetNewTimestamp := tgOldTimestamp + 1

		tx := c.etcd.Txn(ctx).If(
			clientv3.Compare(
				clientv3.Value(timestampKey),
				"=",
				strconv.FormatUint(tgOldTimestamp, 10),
			),
		).Then(
			clientv3.OpPut(timestampKey, strconv.FormatUint(targetNewTimestamp, 10)),

			clientv3.OpPut(
				tgPendingEventStatus(tgID, targetNewTimestamp),
				string(models.EventStatusPending),
			),
			clientv3.OpPut(
				tgEventKey(tgID, targetNewTimestamp),
				mustJsonMarshal(eventDto{
					Type:           eventType,
					DesiredVersion: uint(targetNewTimestamp),
					Time:           time.Now(),
					Deadline:       time.Now().Add(maxEventPendingDuration),
					Payload:        mustJsonMarshal(eventPayload),
				}),
			),
		).Else(
			clientv3.OpGet(timestampKey),
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
			return fmt.Errorf("failed to extract current timestamp for tg %s: %w", tgID, err)
		}
		lastTgTimestamp, err := strconv.ParseUint(tgTimestampStr, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse current timestamp for tg %s: %w", tgID, err)
		}
		tgOldTimestamp = lastTgTimestamp
	}
	return fmt.Errorf("failed add event into eventlog for tg %s: max attempts count exceeded", tgID)
}

func (c *ApiClient) SetTargetGroupSpec(ctx context.Context, tg models.TargetGroupSpec) error {
	const firstEventTimestamp = "0"

	tx := c.etcd.Txn(ctx).If(
		clientv3.Compare(
			clientv3.CreateRevision(tgSpecTimestamp(tg.ID)), "=", 0,
		),
	).Then(
		clientv3.OpPut(tgSpecTimestamp(tg.ID), firstEventTimestamp),
		clientv3.OpPut(tgEndpointsTimestamp(tg.ID), firstEventTimestamp),

		clientv3.OpPut(tgDesiredVersion(tg.ID), firstEventTimestamp),
		clientv3.OpPut(tgDesiredSpecPath(tg.ID), "{}"),
		// clientv3.OpPut(tgDesiredEndpointsPath(tg.ID), "[]"),
		// clientv3.OpPut(tgDesiredEndpointsChecksum(tg.ID), "0"),

		// clientv3.OpPut(tgAppliedVersionKey(tg.ID), firstEventTimestamp),
		// clientv3.OpPut(tgAppliedSpecPath(tg.ID), "{}"),
		// clientv3.OpPut(tgAppliedEndpointsPath(tg.ID), "[]"),
		// clientv3.OpPut(tgAppliedEndpointsChecksum(tg.ID), "0"),
	)
	_, err := tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to create target group: %w", err)
	}
	return c.AddEventIntoEventlog(
		ctx,
		specEventType,
		tg.ID,
		models.EventTypeUpdateTargetGroup,
		targetGroupSpec{
			Proto: tg.Proto,
			Port:  tg.Port,
			VIP:   tg.VirtualIP.String(),
		},
	)
}

func (c *ApiClient) AddEndpoint(ctx context.Context, tgID models.TargetGroupID, ep models.EndpointSpec) error {
	return c.AddEventIntoEventlog(
		ctx,
		endpointEventType,
		tgID,
		models.EventTypeAddEndpoint,
		endpointSpec{
			TargetGroupID: tgID,
			IP:            ep.IP.String(),
			Port:          ep.Port,
			Weight:        ep.Weight,
		},
	)
}

func (c *ApiClient) RemoveEndpoint(ctx context.Context, tgID models.TargetGroupID, ep models.EndpointSpec) error {
	return c.AddEventIntoEventlog(
		ctx,
		endpointEventType,
		tgID,
		models.EventTypeAddEndpoint,
		endpointSpec{
			TargetGroupID: tgID,
			IP:            ep.IP.String(),
			Port:          ep.Port,
		},
	)
}

func (c *ApiClient) RunEventlogWatcher(ctx context.Context) error {
	return nil
}
