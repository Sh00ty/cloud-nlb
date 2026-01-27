package etcd

import (
	"fmt"
	"time"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

type Event struct {
	Type           models.EventType `json:"event_type"`
	DesiredVersion uint             `json:"desired_version"`
	Time           time.Time        `json:"time"`
	Deadline       time.Time        `json:"deadline"`
	Payload        string           `json:"payload"`
}

type TargetGroupSpec struct {
	Proto              models.Protocol `json:"protocol"`
	Port               uint16          `json:"port"`
	VIP                string          `json:"vip"`
	AssignedDataPlanes []string        `json:"assigned_data_planes"`
}

type EndpointSpec struct {
	TargetGroupID models.TargetGroupID `json:"target_group_id"`
	IP            string               `json:"real_ip"`
	Port          uint16               `json:"port"`
	Weight        uint16               `json:"weight"`
}

const (
	maxTxRetryAttempts       = 5
	maxEventPendingDuration  = time.Minute
	eventKeyTemplate         = "%05d"
	networlLoadBalanerFolder = "/nlb-registry"
	// nlb-registry/target-groups
	targetGroupsFolder = networlLoadBalanerFolder + "/target-groups"
)

// nlb-registry/target-groups/tg-1(%s)
func tgPath(tgID models.TargetGroupID) string {
	return targetGroupsFolder + "/" + string(tgID)
}

// nlb-registry/eventlog
func eventLogPath() string {
	return networlLoadBalanerFolder + "/eventlog"
}

// nlb-registry/eventlog/tg-1(%s)/00001(%d)
func tgEventKey(tgID models.TargetGroupID, eventID uint64) string {
	return fmt.Sprintf("%s/%s/%s", eventLogPath(), tgID, fmt.Sprintf(eventKeyTemplate, eventID))
}

// nlb-registry/eventlog/pending-status
func pendingEvents() string {
	return fmt.Sprintf("%s/pending-status", eventLogPath())
}

// nlb-registry/eventlog/pending-status/tg-1(%s)/00001(%d)
func tgPendingEventStatus(tgID models.TargetGroupID, eventID uint64) string {
	return fmt.Sprintf("%s/%s/%s", pendingEvents(), tgID, fmt.Sprintf(eventKeyTemplate, eventID))
}

// nlb-registry/target-groups/tg-1(%s)/desired
func tgDesiredFolder(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/desired", tgPath(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/timestamp
func tgTimestamp(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/timestamp", tgPath(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/desired/version
func tgDesiredVersion(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/version", tgDesiredFolder(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/desired/spec
func tgDesiredSpecPath(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/spec", tgDesiredFolder(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/desired/endpoints
func tgDesiredEndpointsPath(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/endpoints", tgDesiredFolder(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/desired/endpoints-checksum
func tgDesiredEndpointsChecksum(tgID models.TargetGroupID) string {
	// TODO: later here will be batches
	return fmt.Sprintf("%s/endpoints-checksum", tgDesiredFolder(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/applied
func tgAppliedFolder(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/applied", tgPath(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/applied/version
func tgAppliedVersionKey(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/version", tgAppliedFolder(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/applied/spec
func tgAppliedSpecPath(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/spec", tgAppliedFolder(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/applied/endpoints
func tgAppliedEndpointsPath(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/endpoints", tgAppliedFolder(tgID))
}

// nlb-registry/target-groups/tg-1(%s)/applied/endpoints-checksum
func tgAppliedEndpointsChecksum(tgID models.TargetGroupID) string {
	// TODO: later here will be batches
	return fmt.Sprintf("%s/endpoints-checksum", tgAppliedFolder(tgID))
}
