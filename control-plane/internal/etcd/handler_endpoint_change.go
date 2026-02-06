package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
	"github.com/rs/zerolog/log"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type EndpointChangeHandler interface {
	HandleEndpointChange(context.Context, models.EndpointEvent, uint64)
}

type etcdEndpointChangelogHandler struct {
	handler EndpointChangeHandler
}

func NewEtcdEndpointChangelogHandler(handler EndpointChangeHandler) etcdEndpointChangelogHandler {
	return etcdEndpointChangelogHandler{handler: handler}
}

func (h etcdEndpointChangelogHandler) Handle(ctx context.Context, events []*clientv3.Event) error {
	for _, event := range events {
		parsedEvent, err := parseEndpointChange(event.Kv)
		if err != nil {
			log.Error().Err(err).Msg("failed to parse endpoint changelog event from kv entry")
			continue
		}
		modelEvent := models.EndpointEvent{
			Type:           parsedEvent.Type,
			TargetGroupID:  parsedEvent.TargetGroupID,
			DesiredVersion: parsedEvent.Timestamp,
			Time:           parsedEvent.Time,
			Spec: models.EndpointSpec{
				IP:     net.ParseIP(parsedEvent.Endpoint.IP),
				Port:   parsedEvent.Endpoint.Port,
				Weight: parsedEvent.Endpoint.Weight,
			},
		}
		h.handler.HandleEndpointChange(ctx, modelEvent, uint64(event.Kv.ModRevision))
	}
	return nil
}

func parseEndpointChange(kv *mvccpb.KeyValue) (*endpointLogEntry, error) {
	key := string(kv.Key)
	afterPrefix, ok := strings.CutPrefix(key, tgEndpointsLogFolder()+"/")
	if !ok {
		return nil, fmt.Errorf("not found changelog prefix in key: %s", key)
	}
	_, _, found := strings.Cut(afterPrefix, "/")
	if !found {
		return nil, fmt.Errorf("not found tgID and event timestamp in key: %s", key)
	}
	result := new(endpointLogEntry)
	err := json.Unmarshal(kv.Value, result)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal endpoint log entry: %w", err)
	}
	return result, nil
}
