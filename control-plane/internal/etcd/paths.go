package etcd

import (
	"fmt"
	"path"

	"github.com/Sh00ty/network-lb/control-plane/internal/models"
)

/*
nlb-registry/target-groups/specs/timestamp/tg-1(%s)
nlb-registry/target-groups/specs/desired/tg-1(%s)/v5(%d)
nlb-registry/target-groups/specs/current/tg-1(%s)/

nlb-registry/target-groups/endpoints/timestamp/tg-1(%s)
nlb-registry/target-groups/endpoints/compacted/tg-1(%s)
nlb-registry/target-groups/endpoints/changelog/tg-1(%s)

TODO:
nlb-registry/target-groups/tg-1(%s)/assigned/dpl-1(%s) // to make atomic write here and into dpl

nlb-registry/data-planes/dpl-1(%s)/status
nlb-registry/data-planes/dpl-1(%s)/timestamp
nlb-registry/data-planes/dpl-1(%s)/assigned/tg1
nlb-registry/data-planes/dpl-1(%s)/assigned/tg2


data plane makes request to control-plane:

https://control-plane.nlb.svc/?node_id="dc-node-xxx"&placement-version=6
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
	networkLoadBalancerFolder = "/nlb-registry"
	targetGroupsFolder        = networkLoadBalancerFolder + "/target-groups"
	dataPlanesFolder          = networkLoadBalancerFolder + "/data-planes"
	reconcilerLeadershipKey   = "/nlb/reconciler/all-targets"

	nopEvent = "{}"

	eventKeyTemplate = "%05d"
)

// nlb-registry/target-groups/spec/
func specFolder() string {
	return path.Join(targetGroupsFolder, "spec")
}

// nlb-registry/target-groups/spec/timestamp/tg-1(%s)
func tgSpecTimestamp(tgID models.TargetGroupID) string {
	return path.Join(
		specFolder(),
		"timestamp",
		string(tgID),
	)
}

// nlb-registry/target-groups/spec/desired
func specsDesiredFolder() string {
	return path.Join(
		specFolder(),
		"desired",
	)
}

// nlb-registry/target-groups/spec/desired/tg-1(%s)
func tgSpecDesiredFolder(tgID models.TargetGroupID) string {
	return path.Join(
		specsDesiredFolder(),
		string(tgID),
	)
}

// nlb-registry/target-groups/spec/desired/tg-1(%s)/00001(%d)
func tgSpecDesiredVersion(tgID models.TargetGroupID, time uint64) string {
	return path.Join(
		tgSpecDesiredFolder(tgID),
		fmt.Sprintf(eventKeyTemplate, time),
	)
}

// nlb-registry/target-groups/spec/desired/latest/tg-1(%s)
func tgSpecDesiredLatestFolder() string {
	return path.Join(
		specsDesiredFolder(),
		"latest",
	)
}

// nlb-registry/target-groups/spec/desired/latest/tg-1(%s)
func tgSpecDesiredLatest(tgID models.TargetGroupID) string {
	return path.Join(
		tgSpecDesiredLatestFolder(),
		string(tgID),
	)
}

// nlb-registry/target-groups/spec/current
func currentSpecsFolder() string {
	return path.Join(
		specFolder(),
		"current",
	)
}

// nlb-registry/target-groups/spec/current/
func tgSpecCurrent(tgID models.TargetGroupID) string {
	return path.Join(
		currentSpecsFolder(),
		string(tgID),
	)
}

// nlb-registry/target-groups/endpoints
func endpointsFolder() string {
	return path.Join(
		targetGroupsFolder,
		"endpoints",
	)
}

// nlb-registry/target-groups/endpoints/timestamp/tg-1(%s)
func tgEndpointsTimestamp(tgID models.TargetGroupID) string {
	return path.Join(
		endpointsFolder(),
		"timestamp",
		string(tgID),
	)
}

// nlb-registry/target-groups/endpoints/compacted/tg-1(%s)
func tgEndpointsCompacted(tgID models.TargetGroupID) string {
	return path.Join(
		endpointsFolder(),
		"compacted",
		string(tgID),
	)
}

// nlb-registry/target-groups/endpoints/changelog
func tgEndpointsLogFolder() string {
	return path.Join(
		endpointsFolder(),
		"changelog",
	)
}

// nlb-registry/target-groups/endpoints/changelog/tg-1(%s)/00001(%d)
func tgEndpointsEvent(tgID models.TargetGroupID, time uint64) string {
	return path.Join(
		endpointsFolder(),
		"changelog",
		string(tgID),
		fmt.Sprintf(eventKeyTemplate, time),
	)
}

// nlb-registry/target-groups/assigned
func assignedDataPlanesFolder() string {
	return path.Join(
		targetGroupsFolder,
		"assigned",
	)
}

// nlb-registry/target-groups/assigned/tg-1(%s)
func tgAssignedDataPlanes(tgID models.TargetGroupID) string {
	return path.Join(
		assignedDataPlanesFolder(),
		string(tgID),
	)
}

// nlb-registry/target-groups/assigned/tg-1(%s)/dpl-1(%s)
func tgAssignedDataPlaneNode(tgID models.TargetGroupID, nodeID string) string {
	return path.Join(
		tgAssignedDataPlanes(tgID),
		nodeID,
	)
}
