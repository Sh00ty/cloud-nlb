package etcd

import (
	"fmt"
	"path"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

const (
	networkLoadBalancerFolder = "/nlb-registry"
	// nlb-registry/target-groups
	targetGroupsFolder = networkLoadBalancerFolder + "/target-groups"
	eventKeyTemplate   = "%05d"
)

// nlb-registry/target-groups/tg-1(%s)
func tgPath(tgID models.TargetGroupID) string {
	return targetGroupsFolder + "/" + string(tgID)
}

// nlb-registry/eventlog
func eventLogPath() string {
	return path.Join(networkLoadBalancerFolder, "eventlog")
}

// nlb-registry/eventlog/tg-1(%s)/00001(%d)
func tgEventKey(tgID models.TargetGroupID, eventID uint64) string {
	return path.Join(
		eventLogPath(),
		string(tgID),
		fmt.Sprintf(eventKeyTemplate, eventID),
	)
}

// nlb-registry/eventlog/pending-status
func pendingEvents() string {
	return path.Join(eventLogPath(), "pending-status")
}

// nlb-registry/eventlog/pending-status/tg-1(%s)/00001(%d)
func tgPendingEventStatus(tgID models.TargetGroupID, eventID uint64) string {
	return path.Join(
		pendingEvents(),
		string(tgID),
		fmt.Sprintf(eventKeyTemplate, eventID),
	)
}

// nlb-registry/target-groups/tg-1(%s)/desired
func tgDesiredFolder(tgID models.TargetGroupID) string {
	return path.Join(
		tgPath(tgID),
		"desired",
	)
}

// nlb-registry/target-groups/tg-1(%s)/timestamp
func tgTimestamp(tgID models.TargetGroupID) string {
	return path.Join(
		tgPath(tgID),
		"timestamp",
	)
}

// nlb-registry/target-groups/tg-1(%s)/desired/version
func tgDesiredVersion(tgID models.TargetGroupID) string {
	return path.Join(
		tgDesiredFolder(tgID),
		"version",
	)
}

// nlb-registry/target-groups/tg-1(%s)/desired/spec
func tgDesiredSpecPath(tgID models.TargetGroupID) string {
	return path.Join(
		tgDesiredFolder(tgID),
		"spec",
	)
}

// nlb-registry/target-groups/tg-1(%s)/desired/endpoints
func tgDesiredEndpointsPath(tgID models.TargetGroupID) string {
	return path.Join(tgDesiredFolder(tgID), "endpoints")
}

// nlb-registry/target-groups/tg-1(%s)/desired/endpoints-checksum
func tgDesiredEndpointsChecksum(tgID models.TargetGroupID) string {
	// TODO: later here will be batches
	return path.Join(tgDesiredFolder(tgID), "endpoints-checksum")
}

// nlb-registry/target-groups/tg-1(%s)/applied
func tgAppliedFolder(tgID models.TargetGroupID) string {
	return path.Join(tgPath(tgID), "applied")
}

// nlb-registry/target-groups/tg-1(%s)/applied/version
func tgAppliedVersionKey(tgID models.TargetGroupID) string {
	return path.Join(tgAppliedFolder(tgID), "version")
}

// nlb-registry/target-groups/tg-1(%s)/applied/spec
func tgAppliedSpecPath(tgID models.TargetGroupID) string {
	return path.Join(tgAppliedFolder(tgID), "spec")
}

// nlb-registry/target-groups/tg-1(%s)/applied/endpoints
func tgAppliedEndpointsPath(tgID models.TargetGroupID) string {
	return path.Join(tgAppliedFolder(tgID), "endpoints")
}

// nlb-registry/target-groups/tg-1(%s)/applied/endpoints-checksum
func tgAppliedEndpointsChecksum(tgID models.TargetGroupID) string {
	// TODO: later here will be batches
	return path.Join(tgAppliedFolder(tgID), "endpoints-checksum")
}
