package apirpc

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/api/apiruntime"
	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/Sh00ty/cloud-nlb/control-plane/pkg/protobuf/api/proto/cplpbv1"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Long-polling запрос для получения изменений размещения
// Потоковый ответ для поддержки серверных событий:
// - Первое сообщение: ответ с изменениями или таймаутом
// - Последующие сообщения: только при наличии изменений (если wait_timeout > 0)
func (srv *Server) StreamDataPlaneAssignments(
	req *cplpbv1.DataPlaneAssignmentRequest,
	stream cplpbv1.ControlPlaneService_StreamDataPlaneAssignmentsServer,
) error {
	ctx := stream.Context()

	if req.NodeId.GetValue() == "" {
		return status.Error(codes.InvalidArgument, "data-plane node id is required")
	}
	if req.WaitTimeoutSeconds > 60 {
		return status.Error(codes.InvalidArgument, "too big wait timeout, allowed less then 60 sec")
	}
	if req.WaitTimeoutSeconds == 0 {
		req.WaitTimeoutSeconds = 5
	}

	var (
		deadline         = time.Now().Add(time.Duration(req.WaitTimeoutSeconds) * time.Second)
		nodeID           = models.DataPlaneID(req.GetNodeId().GetValue())
		notifier         = srv.runtime.GetNotifier(nodeID, deadline)
		parsedTgStatuses = parsePlacement(req.TargetGroupsStatus)

		rtReq = apiruntime.DataPlaneCurrentState{
			NodeID:             nodeID,
			Notifier:           notifier,
			TargetGroupsStatus: parsedTgStatuses,
			Placement: models.Placement{
				Version:      req.GetPlacementVersion(),
				TargetGroups: extractTargetGroupsMap(parsedTgStatuses),
			},
		}
	)

	witCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	for {
		resp, err := srv.runtime.GetChangesForDataPlane(ctx, rtReq)
		if err != nil {
			return status.Errorf(codes.Internal, "got api-runtime error: %v", err)
		}
		if !resp.NeedWait {
			err = stream.Send(cplChangesToPb(resp))
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				log.Error().Err(err).Msgf("failed to send response to dpl %s", nodeID)
				return status.Errorf(codes.Internal, "failed to send response to data-plane %s: %v", nodeID, err)
			}
			// TODO: support need more
			return nil
		}

		if !notifier.Wait(witCtx) {
			err = stream.Send(&cplpbv1.DataPlaneAssignmentResponse{
				ResponseCode: cplpbv1.ResponseCode_RESPONSE_CODE_NO_CHANGES,
			})
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				log.Error().Err(err).Msgf("failed to send response to dpl %s: %v", nodeID, err)
			}
			return nil
		}
	}
}

func parsePlacement(
	input map[string]*cplpbv1.TargetGroupStatus,
) map[models.TargetGroupID]models.TargetGroupPlacement {
	result := make(map[models.TargetGroupID]models.TargetGroupPlacement, len(input))

	for tgID, stat := range input {
		result[models.TargetGroupID(tgID)] = models.TargetGroupPlacement{
			TgID:            models.TargetGroupID(tgID),
			SpecVersion:     stat.SpecVersion,
			EndpointVersion: stat.EndpointsVersion,
		}
	}
	return result
}

func extractTargetGroupsMap(
	m map[models.TargetGroupID]models.TargetGroupPlacement,
) map[models.TargetGroupID]struct{} {
	result := make(map[models.TargetGroupID]struct{}, len(m))
	for tgID := range m {
		result[tgID] = struct{}{}
	}
	return result
}

func cplChangesToPb(cplResp apiruntime.DataPlaneChanges) *cplpbv1.DataPlaneAssignmentResponse {
	return &cplpbv1.DataPlaneAssignmentResponse{
		PlacementVersion: cplResp.PlacementVersion,
		Added:            tgChangesToPb(cplResp.New),
		Updated:          tgChangesToPb(cplResp.Update),
		Removed:          targetGroupIDSliceToPb(cplResp.Removed),
		HasMore:          false,
		ResponseCode:     cplpbv1.ResponseCode_RESPONSE_CODE_HAS_CHANGES,
	}
}

func targetGroupIDSliceToPb(tgIDs []models.TargetGroupID) []*cplpbv1.TargetGroupID {
	result := make([]*cplpbv1.TargetGroupID, 0, len(tgIDs))
	for _, tgID := range tgIDs {
		result = append(result, &cplpbv1.TargetGroupID{Value: string(tgID)})
	}
	return result
}

func tgChangesToPb(changes []apiruntime.TargetGroupChange) []*cplpbv1.TargetGroupAssignment {
	result := make([]*cplpbv1.TargetGroupAssignment, 0, len(changes))
	for _, ch := range changes {
		result = append(result, tgChangeToPb(ch))
	}
	return result
}

func tgChangeToPb(tgChange apiruntime.TargetGroupChange) *cplpbv1.TargetGroupAssignment {
	return &cplpbv1.TargetGroupAssignment{
		Id:               &cplpbv1.TargetGroupID{Value: string(tgChange.ID)},
		SpecVersion:      tgChange.SpecVersion,
		Spec:             tgSpecToPb(tgChange.Spec),
		EndpointsVersion: tgChange.EndpointVersion,
		Snapshot: &cplpbv1.EndpointsSnapshot{
			TotalCount: uint32(len(tgChange.Snapshot)),
			Endpoints:  tgEndpointSliceToPb(tgChange.Snapshot),
			Checksum:   "sha256:TODO:",
		},
		Changelog: tgEventsSliceToPb(tgChange.Changelog),
	}
}

func tgSpecToPb(spec *models.TargetGroupSpec) *cplpbv1.TargetGroupSpec {
	if spec == nil {
		return nil
	}
	proto := cplpbv1.Protocol_PROTOCOL_UNSPECIFIED
	switch spec.Proto {
	case models.TCP:
		proto = cplpbv1.Protocol_PROTOCOL_TCP
	case models.UDP:
		proto = cplpbv1.Protocol_PROTOCOL_UDP
	}
	return &cplpbv1.TargetGroupSpec{
		Protocol:  proto,
		Port:      uint32(spec.Port),
		VirtualIp: spec.VirtualIP.String(),
	}
}

func tgEndpointSliceToPb(endpoints []models.EndpointSpec) []*cplpbv1.Endpoint {
	result := make([]*cplpbv1.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		result = append(result, &cplpbv1.Endpoint{
			Ip:     ep.IP.String(),
			Port:   uint32(ep.Port),
			Weight: uint32(ep.Weight),
		})
	}
	return result
}

func tgEventsSliceToPb(events []models.EndpointEvent) []*cplpbv1.EndpointChangelogEntry {
	result := make([]*cplpbv1.EndpointChangelogEntry, 0, len(events))
	for _, ev := range events {
		result = append(result, &cplpbv1.EndpointChangelogEntry{
			Spec: &cplpbv1.Endpoint{
				Ip:     ev.Spec.IP.String(),
				Port:   uint32(ev.Spec.Port),
				Weight: uint32(ev.Spec.Weight),
			},
			Deleted: ev.Type == models.EventTypeRemoveEndpoint,
		})
	}
	return result
}
