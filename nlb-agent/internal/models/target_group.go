package models

import (
	"net"
	"time"
)

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

type EndpointSpec struct {
	IP     net.IP
	Weight uint32
	Port   uint16
}

type EndpointHdr struct {
	TargetGroupID TargetGroupID
	IP            net.IP
	Port          uint16
}

type EndpointStatus struct {
	Header    EndpointHdr
	UpdatedAt time.Time
	Healthy   bool
}

type EndpointEvent struct {
	Spec    EndpointSpec
	Removed bool
}
