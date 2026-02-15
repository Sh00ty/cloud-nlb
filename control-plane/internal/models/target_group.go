package models

import (
	"net"
	"time"
)

type TargetGroupID string

type Protocol uint8

const (
	TCP Protocol = iota + 1
	UDP
)

type TargetGroupSpec struct {
	ID        TargetGroupID
	Proto     Protocol
	Port      uint16
	VirtualIP net.IP
	Time      time.Time
}

type TargetGroup struct {
	Spec        TargetGroupSpec
	SpecVersion uint64

	EndpointVersion     uint64
	SnapshotLastVersion uint64
	EndpointsSnapshot   []EndpointSpec
	EndpointsChangelog  []EndpointEvent
}

type EndpointSpec struct {
	IP     net.IP
	Port   uint16
	Weight uint16
}

type EventType string

const (
	EventTypeUnknown        EventType = "UNKNOWN"
	EventTypeAddEndpoint    EventType = "ADD_ENDPOINT"
	EventTypeRemoveEndpoint EventType = "REMOVE_ENDPOINT"
)

type EventStatus string

type EndpointEvent struct {
	Type           EventType
	TargetGroupID  TargetGroupID
	DesiredVersion uint64
	Time           time.Time
	Spec           EndpointSpec
}
