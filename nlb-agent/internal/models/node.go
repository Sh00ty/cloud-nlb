package models

type NodeState struct {
	NodeName          string
	PlacementVersion  uint64
	TargetGroupStates map[TargetGroupID]TargetGroupState
}
