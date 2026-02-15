package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type PlacementChangeHandler interface {
	HandlePlacementChange(context.Context, models.DataPlaneID, models.Placement, uint64)
}

type etcdPlacementChangeHandler struct {
	handler PlacementChangeHandler
}

func NewEtcdPlacementChangeHandler(h PlacementChangeHandler) *etcdPlacementChangeHandler {
	return &etcdPlacementChangeHandler{handler: h}
}

func (h etcdPlacementChangeHandler) Handle(ctx context.Context, event *clientv3.Event) error {
	if event.Kv == nil {
		return nil
	}

	dplID, plDTO, err := parsePlacement(event.Kv)
	if err != nil {
		return fmt.Errorf("parsing data-plane placement change event: %w", err)
	}
	pl := models.Placement{
		Version:      plDTO.Version,
		TargetGroups: plDTO.TargetGroups,
	}
	h.handler.HandlePlacementChange(ctx, dplID, pl, uint64(event.Kv.ModRevision))
	return nil
}

func parsePlacement(kv *mvccpb.KeyValue) (models.DataPlaneID, dataplanePlacementDto, error) {
	key := string(kv.Key)
	dplID, found := strings.CutPrefix(key, DataPlanePlacements+"/")
	if !found {
		return "", dataplanePlacementDto{}, fmt.Errorf("not found data-plane node id in key: %s", key)
	}
	placementDto := dataplanePlacementDto{}
	err := json.Unmarshal(kv.Value, &placementDto)
	if err != nil {
		return "", dataplanePlacementDto{}, fmt.Errorf("unmarshaling dpl placement: %s", key)
	}
	return models.DataPlaneID(dplID), placementDto, nil
}
