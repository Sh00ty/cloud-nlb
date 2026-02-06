package models

type DataPlaneID string

type TargetGroupPlacement struct {
	TgID            TargetGroupID
	SpecVersion     uint64
	EndpointVersion uint64
}

type Placement struct {
	Version      uint64
	TargetGroups map[TargetGroupID]struct{}
}

type DataPlanePlacementInfo struct {
	NodeID  DataPlaneID
	Desired *Placement
}
