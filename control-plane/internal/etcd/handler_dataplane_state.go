package etcd

import (
	"context"
	"fmt"
	"strings"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/reconciler"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type DataPlaneStateChangeHandler struct {
	ch chan reconciler.Event
}

func NewDataPlaneStateChangeHandler(ch chan reconciler.Event) *DataPlaneStateChangeHandler {
	return &DataPlaneStateChangeHandler{ch: ch}
}

func (h DataPlaneStateChangeHandler) Handle(ctx context.Context, event *clientv3.Event) error {
	if event.Kv == nil {
		return nil
	}
	ev := reconciler.Event{}

	nodeID, exists := strings.CutPrefix(string(event.Kv.Key), DataPlaneStatuses+"/")
	if !exists {
		return fmt.Errorf("failed to prase data-plane node id from event key: %s", event.Kv)
	}
	ev.NodeID = (*models.DataPlaneID)(&nodeID)

	switch {
	case event.Type == mvccpb.DELETE:
		ev.Type = reconciler.DataPlaneDead
	case event.Kv != nil:
		switch string(event.Kv.Value) {
		case "alive":
			ev.Type = reconciler.DataPlaneAlive
		case "drained":
			ev.Type = reconciler.DataPlaneDrained
		}
	default:
		return fmt.Errorf("dpl changes watcher: unsupported event type: empty kv or unknown state")
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("sending event into channel: %w", ctx.Err())
	case h.ch <- ev:
		return nil
	}
}
