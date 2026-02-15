package apiruntime

import (
	"context"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
)

type Coordinator interface {
	GetTargetGroupDiff(ctx context.Context, current TargetGroupState) (models.TargetGroup, error)
	AddEndpoint(ctx context.Context, tgID models.TargetGroupID, ep models.EndpointSpec) error
	RemoveEndpoint(ctx context.Context, tgID models.TargetGroupID, ep models.EndpointSpec) error
	SetTargetGroupSpec(ctx context.Context, tg models.TargetGroupSpec) error
}

type Notifier interface {
	ID() uint64
	Notify(ctx context.Context)
	Wait(ctx context.Context) bool
	Expire()
	IsExpired() bool
}

type TargetGroupChange struct {
	ID          models.TargetGroupID
	SpecVersion uint64
	Spec        *models.TargetGroupSpec

	EndpointVersion uint64
	Snapshot        []models.EndpointSpec
	Changelog       []models.EndpointEvent
}

type TargetGroupState struct {
	TgID                models.TargetGroupID
	SpecVersion         uint64
	SnapshotLastVersion uint64
	EndpointVersion     uint64
}
