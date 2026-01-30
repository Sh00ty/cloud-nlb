package models

import "time"

// TODO:s
type EventType string

const (
	EventTypeUnknown           EventType = "UNKNOWN"
	EventTypeUpdateTargetGroup EventType = "UPDATE_TARGETGROUP"
	EventTypeAddEndpoint       EventType = "ADD_ENDPOINT"
	EventTypeRemoveEndpoint    EventType = "REMOVE_ENDPOINT"
)

type EventStatus string

const (
	EventStatusCreated EventStatus = "CREATED"
	EventStatusPending EventStatus = "PENDING"
	EventStatusApplied EventStatus = "APPLIED"
)

type Event struct {
	Type            EventType
	Status          EventStatus
	Time            time.Time
	Deadline        time.Time
	DesiredVersion  uint
	TargetGroupSpec *TargetGroupSpec
	EndpointSpec    *EndpointSpec
}
