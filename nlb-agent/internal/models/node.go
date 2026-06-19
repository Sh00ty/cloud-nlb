package models

type NodeState struct {
	NodeName          string
	PlacementVersion  uint64
	TargetGroupStates map[TargetGroupID]TargetGroupState
}

type TargetGroupState struct {
	ID              TargetGroupID
	SpecVersion     uint64
	EndpointVersion uint64
}
