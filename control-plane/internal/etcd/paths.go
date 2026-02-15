package etcd

import (
	"fmt"
	"path"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
)

/*
cloud-nlb-registry/target-groups/specs/timestamp/tg-1(%s)
cloud-nlb-registry/target-groups/specs/desired/tg-1(%s)/v5(%d)
cloud-nlb-registry/target-groups/specs/current/tg-1(%s)/

cloud-nlb-registry/target-groups/endpoints/timestamp/tg-1(%s)
cloud-nlb-registry/target-groups/endpoints/compacted/tg-1(%s)
cloud-nlb-registry/target-groups/endpoints/changelog/tg-1(%s)

TODO:
cloud-nlb-registry/target-groups/tg-1(%s)/assigned/dpl-1(%s) // to make atomic write here and into dpl

cloud-nlb-registry/data-planes/dpl-1(%s)/status
cloud-nlb-registry/data-planes/dpl-1(%s)/timestamp
cloud-nlb-registry/data-planes/dpl-1(%s)/assigned/tg1
cloud-nlb-registry/data-planes/dpl-1(%s)/assigned/tg2


data plane makes request to control-plane:

https://control-plane.cloud-nlb.svc/?node_id="dc-node-xxx"&placement-version=6
{
	"target-groups" : [
		{
			"name": "tg-1",
			"spec-version": "3",
			"endpoints-version": "4"
		},
		{
			"name": "tg-2",
			"spec-version": "1",
			"endpoints-version": "0"
		}
	],
}

and control-plane answer is:

{
	placement-version : "89",
	"new" : [
		{
			"name": "tg-1",
			"spec": {...},
			"endpoints" : [
				{...},
				{...},
				{...}
			]
		},
		...
	],
	"update" : [
		{
			"name" : "tg-2",
			"spec" : {spec}, //or null if nothing changed
			"endpoints": {
				"add": [{...}, {...}, {...}],
				"removed": [{...}, {...}, {...}],
				"snapshot" : [{...}, {...}, {...}] // if we can't make diff on control-plane
			}
		},
		...
	],
	"removed": : ["tg3", "tg-4"]
}
*/

const (
	NetworkLoadBalancerFolder = "/cloud-nlb-registry"
	TargetGroupsFolder        = NetworkLoadBalancerFolder + "/target-groups"

	DataPlanesFolder    = NetworkLoadBalancerFolder + "/data-planes"
	DataPlanePlacements = DataPlanesFolder + "/placements"
	DataPlaneStatuses   = DataPlanesFolder + "/statuses"

	ReconcilerLeadershipKey = "/cloud-nlb/reconciler/all-targets"

	nopEvent = "{}"

	eventKeyTemplate = "%05d"
)

// cloud-nlb-registry/target-groups/spec/
func specFolder() string {
	return path.Join(TargetGroupsFolder, "spec")
}

// cloud-nlb-registry/target-groups/spec/timestamp/tg-1(%s)
func tgSpecTimestamp(tgID models.TargetGroupID) string {
	return path.Join(
		specFolder(),
		"timestamp",
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/spec/desired
func specsDesiredFolder() string {
	return path.Join(
		specFolder(),
		"desired",
	)
}

// cloud-nlb-registry/target-groups/spec/desired/tg-1(%s)
func tgSpecDesiredFolder(tgID models.TargetGroupID) string {
	return path.Join(
		specsDesiredFolder(),
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/spec/desired/tg-1(%s)/00001(%d)
func tgSpecDesiredVersion(tgID models.TargetGroupID, time uint64) string {
	return path.Join(
		tgSpecDesiredFolder(tgID),
		fmt.Sprintf(eventKeyTemplate, time),
	)
}

// cloud-nlb-registry/target-groups/spec/desired/latest
func TgSpecDesiredLatestFolder() string {
	return path.Join(
		specsDesiredFolder(),
		"latest",
	)
}

// cloud-nlb-registry/target-groups/spec/desired/latest/tg-1(%s)
func tgSpecDesiredLatest(tgID models.TargetGroupID) string {
	return path.Join(
		TgSpecDesiredLatestFolder(),
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/spec/current
func currentSpecsFolder() string {
	return path.Join(
		specFolder(),
		"current",
	)
}

// cloud-nlb-registry/target-groups/spec/current/
func tgSpecCurrent(tgID models.TargetGroupID) string {
	return path.Join(
		currentSpecsFolder(),
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/endpoints
func endpointsFolder() string {
	return path.Join(
		TargetGroupsFolder,
		"endpoints",
	)
}

// cloud-nlb-registry/target-groups/endpoints/timestamp/tg-1(%s)
func tgEndpointsTimestamp(tgID models.TargetGroupID) string {
	return path.Join(
		endpointsFolder(),
		"timestamp",
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/endpoints/compacted/tg-1(%s)
func tgEndpointsCompacted(tgID models.TargetGroupID) string {
	return path.Join(
		endpointsFolder(),
		"compacted",
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/endpoints/changelog
func EndpointsLogFolder() string {
	return path.Join(
		endpointsFolder(),
		"changelog",
	)
}

// cloud-nlb-registry/target-groups/endpoints/changelog/tg-1(%s)
func tgEndpointsLogFolder(tgID models.TargetGroupID) string {
	return path.Join(
		EndpointsLogFolder(),
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/endpoints/changelog/tg-1(%s)/00001(%d)
func tgEndpointsEvent(tgID models.TargetGroupID, time uint64) string {
	return path.Join(
		tgEndpointsLogFolder(tgID),
		fmt.Sprintf(eventKeyTemplate, time),
	)
}

// cloud-nlb-registry/target-groups/assigned
func assignedDataPlanesFolder() string {
	return path.Join(
		TargetGroupsFolder,
		"assigned",
	)
}

// cloud-nlb-registry/target-groups/assigned/tg-1(%s)
func tgAssignedDataPlanes(tgID models.TargetGroupID) string {
	return path.Join(
		assignedDataPlanesFolder(),
		string(tgID),
	)
}

// cloud-nlb-registry/target-groups/assigned/tg-1(%s)/dpl-1(%s)
func tgAssignedDataPlaneNode(tgID models.TargetGroupID, nodeID models.DataPlaneID) string {
	return path.Join(
		tgAssignedDataPlanes(tgID),
		string(nodeID),
	)
}
