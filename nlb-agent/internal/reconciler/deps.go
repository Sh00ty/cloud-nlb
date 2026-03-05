package reconciler

import (
	"context"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
)

type VersionedSpec struct {
	Version uint64                 `json:"version"`
	Spec    models.TargetGroupSpec `json:"spec"`
}

type VersionedEndpoints struct {
	Version   uint64                `json:"version"`
	Endpoints []models.EndpointSpec `json:"endpoints"`
}

type Storage interface {
	GetPlacement(ctx context.Context) (models.NodeState, error)
	SavePlacementVersion(ctx context.Context, version uint64) (bool, error)

	GetDesiredSpec(tgID models.TargetGroupID) (*VersionedSpec, bool)
	SetDesiredSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec, version uint64) (bool, error)

	GetActualSpec(_ context.Context, tgID models.TargetGroupID) (*VersionedSpec, bool)
	SetActualSpec(_ context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec, version uint64) error

	GetDesiredEndpoints(ctx context.Context, tgID models.TargetGroupID) (*VersionedEndpoints, bool)
	SetDesiredEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec, version uint64) (bool, error)

	GetActualEndpoints(_ context.Context, tgID models.TargetGroupID) (*VersionedEndpoints, bool)
	SetActualEndpoints(_ context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec, version uint64) error

	DeleteDesired(ctx context.Context, tgIDs []models.TargetGroupID) error
	DeleteActual(_ context.Context, tgID models.TargetGroupID) error

	GetAllTargetGroupIDs() []models.TargetGroupID
}

type EndpointsStatusManager interface {
	WatchForTargetGroup(ctx context.Context, tgIDs models.TargetGroupID) error
	StopWatchForTargetGroup(ctx context.Context, tgID models.TargetGroupID) error

	GetEndpointsStatus(ctx context.Context, tgID models.TargetGroupID, endpoint models.EndpointHdr) bool
}

type VPPManager interface {
	ApplySpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error
	RemoveSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error
	AddEndpoints(ctx context.Context, tgID models.TargetGroupID, desired []models.EndpointSpec) error
	RemoveEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) error
}
