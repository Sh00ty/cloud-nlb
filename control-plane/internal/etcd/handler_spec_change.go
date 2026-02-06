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

type SpecChangeHandler interface {
	HandleTgSpecChange(
		ctx context.Context,
		spec models.TargetGroupSpec,
		desiredVersion uint64,
		rev uint64)
}

type etcdSpecChangeHandler struct {
	handler SpecChangeHandler
}

func NewEtcdSpecChangelogHandler(handler SpecChangeHandler) etcdSpecChangeHandler {
	return etcdSpecChangeHandler{handler: handler}
}

func (h etcdSpecChangeHandler) Handle(ctx context.Context, events []*clientv3.Event) error {
	for _, event := range events {
		tgID, specEvent, err := parseSpecChange(event.Kv)
		if err != nil {
			log.Error().Err(err).Msg("failed to parse spec change from kv entry")
			continue
		}
		modelEvent := models.TargetGroupSpec{
			ID:        tgID,
			Proto:     specEvent.Proto,
			Port:      specEvent.Port,
			VirtualIP: net.ParseIP(specEvent.VIP),
			Time:      specEvent.Time,
		}
		h.handler.HandleTgSpecChange(ctx, modelEvent, specEvent.Version, uint64(event.Kv.ModRevision))
	}
	return nil
}

func parseSpecChange(kv *mvccpb.KeyValue) (models.TargetGroupID, *targetGroupSpec, error) {
	key := string(kv.Key)
	tgID, ok := strings.CutPrefix(key, tgSpecDesiredLatestFolder()+"/")
	if !ok {
		return "", nil, fmt.Errorf("not found desired spec prefix in key: %s", key)
	}
	result := new(targetGroupSpec)
	err := json.Unmarshal(kv.Value, result)
	if err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal spec: %w", err)
	}
	return models.TargetGroupID(tgID), result, nil
}
