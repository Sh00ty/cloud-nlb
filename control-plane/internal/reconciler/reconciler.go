package reconciler

import (
	"context"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

type TargetGroupSnapshot struct {
	TargetGroupSpec *models.TargetGroupSpec
	Version         uint64
}

type DataPlaneSnapshot struct {
}

type CoordinatorService interface {
}

type Reconciler struct {
	targetGroupsDesired      map[models.TargetGroupID]TargetGroupSnapshot
	targetGroupPendingEvents map[models.TargetGroupID][]*models.EndpointEvent
	// here we will store all event, that we can't apply, may be reorder, may be
	// something else
	// pendingEventNotiyer chan models.TargetGroupID
	coordinator CoordinatorService
}

func NewReconciler(coordinator CoordinatorService) *Reconciler {
	return &Reconciler{
		targetGroupsDesired:      make(map[models.TargetGroupID]TargetGroupSnapshot, 128),
		targetGroupPendingEvents: make(map[models.TargetGroupID][]*models.EndpointEvent, 128),
		coordinator:              coordinator,
	}
}

func (r *Reconciler) HandleEndpointChange(context.Context, models.EndpointEvent, uint64) {

}
