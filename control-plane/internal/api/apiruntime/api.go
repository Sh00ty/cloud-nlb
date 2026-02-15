package apiruntime

import (
	"context"
	"fmt"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
)

func (ar *ApiRuntime) UpsertEndpoint(ctx context.Context, epEvents models.EndpointEvent) error {
	switch epEvents.Type {
	case models.EventTypeAddEndpoint:
		return ar.crd.AddEndpoint(ctx, epEvents.TargetGroupID, epEvents.Spec)
	case models.EventTypeRemoveEndpoint:
		return ar.crd.RemoveEndpoint(ctx, epEvents.TargetGroupID, epEvents.Spec)
	}
	return fmt.Errorf("unsupported event type")
}

func (ar *ApiRuntime) UpsertTargetGroupSpec(ctx context.Context, tgSpec models.TargetGroupSpec) error {
	return ar.crd.SetTargetGroupSpec(ctx, tgSpec)
}
