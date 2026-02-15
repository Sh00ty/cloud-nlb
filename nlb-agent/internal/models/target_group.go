package models

import "net"

type TargetGroupID string

type Protocol string

const (
	TCP Protocol = "TCP"
	UDP Protocol = "UDP"
)

type TargetGroupSpec struct {
	VirtualIP net.IP
	Port      uint32
	Protocol  Protocol
}

type TargetGroup struct {
}

type TargetGroupState struct {
	ID              TargetGroupID
	SpecVersion     uint64
	EndpointVersion uint64
}

type EndpointSpec struct {
	IP     net.IP
	Weight uint32
	Port   uint16
}

type EndpointEvent struct {
	Spec    EndpointSpec
	Removed bool
}

type TargetGroupChange struct {
	ID          TargetGroupID
	SpecVersion uint64
	Spec        *TargetGroupSpec

	EndpointsVersion uint64
	Changelog        []EndpointEvent
	// TODO: snapshot
}
