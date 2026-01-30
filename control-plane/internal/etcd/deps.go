package etcd

import (
	"fmt"
	"path"
	"time"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

type targetGroupSpec struct {
	Proto              models.Protocol `json:"protocol"`
	Port               uint16          `json:"port"`
	VIP                string          `json:"vip"`
	AssignedDataPlanes []string        `json:"assigned_data_planes"`
}

type endpointEventDto struct {
	Type           models.EventType `json:"event_type"`
	DesiredVersion uint             `json:"desired_version"`
	Time           time.Time        `json:"time"`
	Payload        string           `json:"payload"`
}

type endpointSpec struct {
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

func eventlogFolder() string {
	return path.Join(networlLoadBalanerFolder, "eventlog")
}

// nlb-registry/eventlog/tg-1(%s)/00001(%d)
func tgEventKey(tgID models.TargetGroupID, eventID uint64) string {
	return path.Join(
		eventlogFolder(),
		string(tgID),
		fmt.Sprintf(eventKeyTemplate, eventID),
	)
}

// nlb-registry/eventlog/logtype/pending-status
func pendingEvents() string {
	return path.Join(eventlogFolder(), "pending-status")
}

// nlb-registry/eventlog/logtype/pending-status/tg-1(%s)/00001(%d)
func tgPendingEventStatus(tgID models.TargetGroupID, eventID uint64) string {
	return path.Join(
		eventlogFolder(),
		string(tgID),
		fmt.Sprintf(eventKeyTemplate, eventID),
	)
}

// nlb-registry/target-groups/desired
func desiredFolder() string {
	return path.Join(targetGroupsFolder, "desired")
}

// nlb-registry/target-groups/timestamps/spec/tg-1(%s)
func tgSpecTimestamp(tgID models.TargetGroupID) string {
	return path.Join(targetGroupsFolder, "timestamps/spec", string(tgID))
}

// nlb-registry/target-groups/timestamps/endpoints/tg-1(%s)
func tgEndpointsTimestamp(tgID models.TargetGroupID) string {
	return path.Join(targetGroupsFolder, "timestamps/endpoints", string(tgID))
}

// nlb-registry/target-groups/desired/version/spec/tg-1(%s)
func tgDesiredSpecVersion(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/version/spec/%s", desiredFolder(), tgID)
}

// nlb-registry/target-groups/desired/spec/tg-1(%s)
func tgDesiredSpecPath(tgID models.TargetGroupID) string {
	return fmt.Sprintf("%s/spec/%s", desiredFolder(), tgID)
}

// nlb-registry/target-groups/applied
func appliedFolder() string {
	return fmt.Sprintf("%s/applied", targetGroupsFolder)
}

// // nlb-registry/target-groups/applied/spec/tg-1(%s)
// func tgAppliedSpecPath(tgID models.TargetGroupID) string {
// 	return fmt.Sprintf("%s/spec/%s", appliedFolder(), tgID)
// }

// TODO: i want to split specs and endpoints
// endpoint log + snapshot and declarative specs
