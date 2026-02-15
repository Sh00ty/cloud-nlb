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

type TargetGroupCreationHandler struct {
	ch chan reconciler.Event
}

func NewTargetGroupCreationHandler(ch chan reconciler.Event) *TargetGroupCreationHandler {
	return &TargetGroupCreationHandler{ch: ch}
}

func (h TargetGroupCreationHandler) Handle(ctx context.Context, event *clientv3.Event) error {
	if event.Kv == nil {
		return nil
	}
	key := string(event.Kv.Key)
	tgID, ok := strings.CutPrefix(key, TgSpecDesiredLatestFolder()+"/")
	if !ok {
		return fmt.Errorf("not found target group id in key: %s", key)
	}
	if event.Type == mvccpb.PUT && event.PrevKv == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case h.ch <- reconciler.Event{
			Type:          reconciler.TargetGroupCreated,
			TargetGroupID: (*models.TargetGroupID)(&tgID),
		}:
		}
	}
	return nil
}
