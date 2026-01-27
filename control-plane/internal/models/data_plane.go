package models

type DataPlaneID string

type DataPlane struct {
	NodeID        DataPlaneID
	Host          string
	TargetGroups  []TargetGroupID
	TargetVersion uint
	Status        DataPlaneStatus
}

type DataPlaneStatus struct {
	Healthy       bool
	ActualVersion uint
}
