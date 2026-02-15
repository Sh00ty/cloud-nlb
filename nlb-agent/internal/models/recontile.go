package models

type ReconciliationUnit struct {
	PlacementVersion uint64
	Added            []*TargetGroupChange
	Updated          []*TargetGroupChange
	Removed          []TargetGroupID
}
