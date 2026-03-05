package models

type ReconciliationUnit struct {
	PlacementVersion uint64
	Added            []*TargetGroupChange
	Updated          []*TargetGroupChange
	Removed          []TargetGroupID
}

type TargetGroupChange struct {
	ID          TargetGroupID
	SpecVersion uint64
	Spec        *TargetGroupSpec

	EndpointsVersion uint64
	Changelog        []EndpointEvent
	// TODO: snapshot
}
