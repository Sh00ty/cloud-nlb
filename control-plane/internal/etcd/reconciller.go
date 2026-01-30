package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

type ReconcillerClient struct {
	nodeID   string
	etcd     *clientv3.Client
	session  *concurrency.Session
	election *concurrency.Election

	eventChan chan *models.Event
}

func NewReconcillerClient(ctx context.Context, etcdhost string, nodeID string, eventChan chan *models.Event) (*ReconcillerClient, error) {
	clnt, err := clientv3.New(clientv3.Config{
		Endpoints: []string{etcdhost},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}
	return &ReconcillerClient{
		nodeID:    nodeID,
		etcd:      clnt,
		eventChan: eventChan,
	}, nil
}

func (c *ReconcillerClient) Close(ctx context.Context) {
	err := c.election.Resign(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to gracefully resign leader")
	}
	err = c.session.Close()
	if err != nil {
		log.Error().Err(err).Msg("failed to destroy session")
	}
	err = c.etcd.Close()
	if err != nil {
		log.Error().Err(err).Msg("failed to close etcd client")
	}
}

func (c *ReconcillerClient) BecomeLeaderReconciller(ctx context.Context) (bool, <-chan struct{}, error) {
	session, err := concurrency.NewSession(c.etcd, concurrency.WithContext(ctx), concurrency.WithTTL(15))
	if err != nil {
		return false, nil, fmt.Errorf("failed to create session: %w", err)
	}
	c.session = session
	c.election = concurrency.NewElection(session, "/nlb/reconciller/all-targets")

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
		log.Warn().Msg("instance won leader election for /nlb/reconciller/all-targets")
		return true, c.session.Done(), nil
	}
}

type WatchHandler func(ctx context.Context, events []*clientv3.Event)

func (c *ReconcillerClient) WatchEventlog(
	ctx context.Context,
	prefix string,
	handler WatchHandler,
	startRevision int64,
) error {
	ctx = clientv3.WithRequireLeader(ctx)

	watch := func(rev int64) clientv3.WatchChan {
		return c.etcd.Watcher.Watch(
			ctx,
			prefix,
			clientv3.WithRev(rev),
			clientv3.WithPrefix(),
			clientv3.WithCreatedNotify(),
			clientv3.WithFilterDelete(),
		)
	}
	watcherChan := watch(startRevision)

	go func() {
		lastRevision := startRevision
		for {
			select {
			case event, ok := <-watcherChan:
				if !ok {
					log.Info().Msg("watcher channel closed")
					return
				}
				if event.Canceled {
					log.Error().Err(event.Err()).Msg("watcher failure: canceled, retry")
					watcherChan = watch(lastRevision)
					continue
				}
				if event.Err() != nil {
					log.Error().Err(event.Err()).Msg("got unexpected watch error")
					continue
				}
				lastRevision = event.Header.Revision
				if event.IsProgressNotify() {
					log.Debug().Msgf("got progress notify message with revision %d", lastRevision)
					continue
				}
				// TODO: handler retry logic
				handler(ctx, event.Events)
			case <-ctx.Done():
				c.etcd.Watcher.Close()
				return
			}
		}
	}()
	return nil
}

type parsedEventKey struct {
	Key     string
	TgID    models.TargetGroupID
	EventID uint64
}

func ParseEventlogKey(key string) (parsedEventKey, error) {
	tgWithEventID, found := strings.CutPrefix(key, eventLogPath()+"/")
	if !found {
		return parsedEventKey{}, fmt.Errorf("not found pending nlb prefix")
	}
	tgIDStr, eventIDStr, found := strings.Cut(tgWithEventID, "/")
	if !found {
		return parsedEventKey{}, fmt.Errorf("can't parse tg-id and event-id")
	}
	eventID, err := strconv.ParseUint(eventIDStr, 10, 64)
	if err != nil {
		return parsedEventKey{}, fmt.Errorf("failed to parse event-id: %w", err)
	}
	return parsedEventKey{
		Key:     key,
		TgID:    models.TargetGroupID(tgIDStr),
		EventID: eventID,
	}, nil
}

func parseEvent(tgID models.TargetGroupID, eventPayload []byte) (models.Event, error) {
	eventDto := eventDto{}
	err := json.Unmarshal(eventPayload, &eventDto)
	if err != nil {
		return models.Event{}, fmt.Errorf("failed to unmarshal event: %w", err)
	}
	event := models.Event{
		Type:           eventDto.Type,
		Time:           eventDto.Time,
		Deadline:       eventDto.Deadline,
		DesiredVersion: eventDto.DesiredVersion,
	}
	switch event.Type {
	case models.EventTypeUpdateTargetGroup:
		tgSpecDto := targetGroupSpec{}
		err = json.Unmarshal([]byte(eventDto.Payload), &tgSpecDto)
		if err != nil {
			return models.Event{}, fmt.Errorf("failed to unmarshal event payload: %w", err)
		}
		tgSpec := models.TargetGroupSpec{
			ID:        tgID,
			Proto:     tgSpecDto.Proto,
			Port:      tgSpecDto.Port,
			VirtualIP: net.ParseIP(tgSpecDto.VIP),
		}
		event.TargetGroupSpec = &tgSpec
		return event, nil
	case models.EventTypeAddEndpoint, models.EventTypeRemoveEndpoint:
		epSpecDto := endpointSpec{}
		err = json.Unmarshal([]byte(eventDto.Payload), &epSpecDto)
		if err != nil {
			return event, fmt.Errorf("failed to unmarshal event payload: %w", err)
		}
		epSpec := models.EndpointSpec{
			IP:     net.ParseIP(epSpecDto.IP),
			Port:   epSpecDto.Port,
			Weight: epSpecDto.Weight,
		}
		event.EndpointSpec = &epSpec
		return event, nil
	}
	return event, fmt.Errorf("event type is not known: %s", event.Type)
}

var ErrParseKey = errors.New("key parse error")

func (c *ReconcillerClient) parseEventFromKV(ctx context.Context, kv *mvccpb.KeyValue) (*models.Event, error) {
	key := string(kv.Key)
	parsedKey, err := ParseEventlogKey(key)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse eventlog watch key %s: %w", ErrParseKey, key, err)
	}
	status := models.EventStatus(kv.Value)

	eventKey := tgEventKey(parsedKey.TgID, parsedKey.EventID)
	// TODO: make tx with cas to appling status?
	resp, err := c.etcd.Get(ctx, eventKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get event %s payload: %w", eventKey, err)
	}
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("not found event %s", eventKey)
	}
	parsedEvent, err := parseEvent(parsedKey.TgID, resp.Kvs[0].Value)
	if err != nil {
		return nil, fmt.Errorf("failed to parse event %s: %w", string(resp.Kvs[0].Value), err)
	}
	parsedEvent.Status = status
	return &parsedEvent, nil
}

func (c *ReconcillerClient) EventlogWatchHandler(ctx context.Context, events []*clientv3.Event) {
	for _, event := range events {
		parsedEvent, err := c.parseEventFromKV(ctx, event.Kv)
		if err != nil {
			log.Error().Err(err).Msg("failed to parse event from kv entry")
		}
		err = c.handleParsedEvent(ctx, parsedEvent)
		if err != nil {
			log.Error().Err(err).Msg("failed to handle parsed event")
		}
	}
}

func (c *ReconcillerClient) handleParsedEvent(ctx context.Context, event *models.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c.eventChan <- event:
		return nil
	}
}

func (c *ReconcillerClient) SetTargetGroupSpec(
	ctx context.Context,
	tgSpec *models.TargetGroupSpec,
	desiredVersion uint,
) (bool, error) {
	tx := c.etcd.KV.Txn(ctx)

	tgSpecDto := targetGroupSpec{
		Proto: tgSpec.Proto,
		Port:  tgSpec.Port,
		VIP:   tgSpec.VirtualIP.String(),
		// TODO: dpl nodes
	}
	desiredVersionStr := strconv.FormatUint(uint64(desiredVersion), 10)
	prevDesiredVersionStr := strconv.FormatUint(uint64(desiredVersion-1), 10)

	// TODO: we need to delete pending status for event
	tx = tx.If(
		clientv3.Compare(clientv3.Value(c.election.Key()), "=", c.nodeID),

		// clientv3.Compare(clientv3.Value(tgAppliedVersionKey(tgSpec.ID)), "<", desiredVersionStr),
		clientv3.Compare(clientv3.Value(tgDesiredVersion(tgSpec.ID)), "=", prevDesiredVersionStr),
	).Then(
		clientv3.OpPut(tgDesiredVersion(tgSpec.ID), desiredVersionStr),
		clientv3.OpPut(tgDesiredSpecPath(tgSpec.ID), mustJsonMarshal(tgSpecDto)),
		clientv3.OpDelete(specPendingEventStatus(tgSpec.ID, uint64(desiredVersion))),
	)
	resp, err := tx.Commit()
	if err != nil {
		return false, fmt.Errorf("failed to commit etcd transaction: %w", err)
	}
	if !resp.Succeeded {
		return false, nil
	}
	return true, nil
}

func (c *ReconcillerClient) GetAllPendingOperations(ctx context.Context) ([]*models.Event, error) {
	var (
		result   = make([]*models.Event, 0, 128)
		startKey = specPendingEvents()
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
			event, err := c.parseEventFromKV(ctx, kv)
			if err != nil {
				if errors.Is(err, ErrParseKey) {
					return result, nil
				}
				return nil, fmt.Errorf("failed to parse event from kv: %w", err)
			}
			startKey = string(append(kv.Key, 0))
			result = append(result, event)
		}
		if !resp.More {
			return result, nil
		}
	}
}

// version or spec or endpoint e.t.c.
func parseTgKey(key string, prefix string) (string, models.TargetGroupID, bool) {
	meaningAndTg, found := strings.CutPrefix(key, prefix+"/")
	if !found {
		return "", "", false
	}
	meaning, tgID, found := strings.Cut(meaningAndTg, "/")
	if !found {
		return "", "", false
	}
	return meaning, models.TargetGroupID(tgID), true
}

func (c *ReconcillerClient) GetAllTargetGroupsDesired(ctx context.Context) (map[models.TargetGroupID]*models.TargetGroup, error) {
	return c.getAllTargetGroups(ctx, true)
}

func (c *ReconcillerClient) getAllTargetGroups(ctx context.Context, desired bool) (map[models.TargetGroupID]*models.TargetGroup, error) {
	// TODO: make it for applyied too
	var (
		result   = make(map[models.TargetGroupID]*models.TargetGroup, 128)
		skip     = make(map[models.TargetGroupID]struct{})
		startKey = desiredFolder()
	)
	skipTg := func(tgID models.TargetGroupID) {
		delete(result, tgID)
		skip[tgID] = struct{}{}
	}

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
			part, tgID, ok := parseTgKey(string(kv.Key), desiredFolder())
			if !ok {
				return result, nil
			}
			if _, needSkip := skip[tgID]; needSkip {
				continue
			}
			current, exists := result[tgID]
			if !exists {
				current = &models.TargetGroup{}
				result[tgID] = current
			}
			switch part {
			case "spec":
				specDto := targetGroupSpec{}
				err := json.Unmarshal(kv.Value, &specDto)
				if err != nil {
					skipTg(tgID)
					log.Error().Err(err).Msgf("failed to parse target group %s spec, skip")
					continue
				}
				current.Spec = &models.TargetGroupSpec{
					ID:        tgID,
					Proto:     specDto.Proto,
					Port:      specDto.Port,
					VirtualIP: net.ParseIP(specDto.VIP),
				}
			case "endpoints":
				// TODO:
			case "endpoints-checksum":
				current.EndpointsHash = kv.Value
			case "version":
				parsedVersion, err := strconv.ParseUint(string(kv.Value), 10, 64)
				if err != nil {
					skipTg(tgID)
					log.Error().Err(err).Msgf("failed to parse target group %s version, skip")
					continue
				}
				current.Version = parsedVersion
			}
			startKey = string(append(kv.Key, 0))
		}
		if !resp.More {
			return result, nil
		}
	}
}

func (c *ReconcillerClient) SkipEvent(ctx context.Context, tgID models.TargetGroupID, desiredVersion uint64) (bool, error) {
	desiredVersionStr := strconv.FormatUint(uint64(desiredVersion), 10)
	prevDesiredVersionStr := strconv.FormatUint(uint64(desiredVersion-1), 10)

	tx := c.etcd.Txn(ctx)
	tx = tx.If(
		clientv3.Compare(clientv3.Value(c.election.Key()), "=", c.nodeID),

		// clientv3.Compare(clientv3.Value(tgAppliedVersionKey(tgID)), "<", desiredVersionStr),
		clientv3.Compare(clientv3.Value(tgDesiredVersion(tgID)), "=", prevDesiredVersionStr),
	).Then(
		clientv3.OpPut(tgDesiredVersion(tgID), desiredVersionStr),
		clientv3.OpDelete(specPendingEventStatus(tgID, uint64(desiredVersion))),
	)
	resp, err := tx.Commit()
	if err != nil {
		return false, fmt.Errorf("failed to commit etcd transaction: %w", err)
	}
	if !resp.Succeeded {
		return false, nil
	}
	return true, nil
}
