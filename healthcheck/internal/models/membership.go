package models

type NodeID string

func (n NodeID) String() string {
	return string(n)
}

type MemberShipEventType int8

const (
	MemberShipUnknown MemberShipEventType = iota
	MemberShipNew
	MemberShipUpdating
	MemberShipSuspect
	MemberShipDead
)

type MemberShipEvent struct {
	Type MemberShipEventType
	From NodeID
}

type CoordinationEventType int8

const (
	CoordinationEventUnknown CoordinationEventType = iota
	CoordinationEventResyncedFromDb
	CoordinationEventDroped
)
