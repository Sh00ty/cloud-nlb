package controlplane

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/pkg/protobuf/api/proto/cplpbv1"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type ControlPlaneClient struct {
	clnt             cplpbv1.ControlPlaneServiceClient
	longPollDuration time.Duration
}

func NewClient(cplAddr string, longPollDuration time.Duration) (*ControlPlaneClient, error) {
	conn, err := grpc.NewClient(cplAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create control-plane grpc client: %w", err)
	}
	return &ControlPlaneClient{
		clnt:             cplpbv1.NewControlPlaneServiceClient(conn),
		longPollDuration: longPollDuration,
	}, nil
}

func (c *ControlPlaneClient) PollUpdatesIfExists(ctx context.Context, state models.NodeState, reqID string) (*models.ReconciliationUnit, error) {
	targetGroupsStatus := make(map[string]*cplpbv1.TargetGroupStatus, len(state.TargetGroupStates))
	for tgID, tgStat := range state.TargetGroupStates {
		targetGroupsStatus[string(tgID)] = &cplpbv1.TargetGroupStatus{
			SpecVersion:      tgStat.SpecVersion,
			EndpointsVersion: tgStat.EndpointVersion,
		}
	}

	resp, err := c.clnt.StreamDataPlaneAssignments(
		ctx,
		&cplpbv1.DataPlaneAssignmentRequest{
			NodeId:             &cplpbv1.DataPlaneID{Value: state.NodeName},
			WaitTimeoutSeconds: uint32(c.longPollDuration.Seconds()),
			RequestId:          reqID,
			PlacementVersion:   state.PlacementVersion,
			TargetGroupsStatus: targetGroupsStatus,
		},
	)
	if err != nil {
		code := status.Code(err)
		switch code {
		case codes.OK:
		case codes.InvalidArgument:
			return nil, fmt.Errorf("invalid request to control-plane: %w", err)
		case codes.Internal:
			return nil, fmt.Errorf("got control-plane internal error: %w", err)
		default:
			return nil, fmt.Errorf("failed to request stream from control plane, has unknown error: %w", err)
		}
	}
	msg := new(cplpbv1.DataPlaneAssignmentResponse)
	err = resp.RecvMsg(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to get response from data-plane stream: %w", err)
	}
	switch msg.ResponseCode {
	case cplpbv1.ResponseCode_RESPONSE_CODE_NO_CHANGES:
		return nil, nil
	case cplpbv1.ResponseCode_RESPONSE_CODE_HAS_CHANGES:
	case cplpbv1.ResponseCode_RESPONSE_CODE_INTERNAL_ERROR:
		return nil, fmt.Errorf("received message with internal error")
	}
	return &models.ReconciliationUnit{
		PlacementVersion: msg.PlacementVersion,
		Added:            assignmentListToModel(msg.Added),
		Updated:          assignmentListToModel(msg.Updated),
		Removed:          targetGroupIDToModel(msg.Removed),
	}, nil
}

func targetGroupIDToModel(tgList []*cplpbv1.TargetGroupID) []models.TargetGroupID {
	result := make([]models.TargetGroupID, 0, len(tgList))
	for _, tgID := range tgList {
		result = append(result, models.TargetGroupID(tgID.Value))
	}
	return result
}

func assignmentListToModel(asList []*cplpbv1.TargetGroupAssignment) []*models.TargetGroupChange {
	result := make([]*models.TargetGroupChange, 0, len(asList))
	for _, as := range asList {
		result = append(result, assignmentToModel(as))
	}
	return result
}

func assignmentToModel(as *cplpbv1.TargetGroupAssignment) *models.TargetGroupChange {
	result := models.TargetGroupChange{
		ID:               models.TargetGroupID(as.Id.Value),
		SpecVersion:      as.SpecVersion,
		Spec:             specToModel(as.Spec),
		Changelog:        changelogToModel(as.Changelog),
		EndpointsVersion: as.EndpointsVersion,
	}
	return &result
}

func specToModel(spec *cplpbv1.TargetGroupSpec) *models.TargetGroupSpec {
	if spec == nil {
		return nil
	}
	protocol := models.TCP
	if spec.Protocol == cplpbv1.Protocol_PROTOCOL_UDP {
		protocol = models.UDP
	}
	return &models.TargetGroupSpec{
		VirtualIP: net.ParseIP(spec.VirtualIp),
		Port:      spec.Port,
		Protocol:  protocol,
	}
}

func changelogToModel(changelog []*cplpbv1.EndpointChangelogEntry) []models.EndpointEvent {
	result := make([]models.EndpointEvent, 0, len(changelog))
	for _, entry := range changelog {
		result = append(result, models.EndpointEvent{
			Spec:    endpointSpecToModel(entry.Spec),
			Removed: entry.Deleted,
		})
	}
	return result
}

func endpointSpecToModel(spec *cplpbv1.Endpoint) models.EndpointSpec {
	if spec == nil {
		return models.EndpointSpec{}
	}
	return models.EndpointSpec{
		IP:     net.ParseIP(spec.Ip),
		Port:   uint16(spec.Port),
		Weight: spec.Port,
	}
}
