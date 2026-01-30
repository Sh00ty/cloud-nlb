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
	UpdatedAt time.Time
}

type TargetGroup struct {
	Version       uint64
	Spec          *TargetGroupSpec
	Endpoints     []EndpointSpec
	EndpointsHash []byte
}

type EndpointSpec struct {
	IP     net.IP
	Port   uint16
	Weight uint16
}

type Operation string

const (
	Add          Operation = "ADD"
	Remove       Operation = "REMOVE"
	AddTarget    Operation = "ADD_TARGET"
	RemoveTarget Operation = "REMOVE_TARGET"
)
