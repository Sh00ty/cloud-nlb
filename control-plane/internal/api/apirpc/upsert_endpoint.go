package apirpc

import (
	"context"
	"net"
	"time"

	"github.com/Sh00ty/cloud-nlb/control-plane/internal/models"
	"github.com/Sh00ty/cloud-nlb/control-plane/pkg/protobuf/api/proto/cplpbv1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Добавление или удаление эндпоинта из таргет-группы
// Для массовых обновлений рекомендуется использовать пакетные операции.
func (srv *Server) UpsertEndpoint(
	ctx context.Context,
	req *cplpbv1.UpsertEndpointRequest,
) (resp *cplpbv1.UpsertEndpointResponse, err error) {

	if req.Endpoint == nil {
		return nil, status.Error(codes.InvalidArgument, "request endpoint data is empty")
	}
	if req.TargetGroupId == nil {
		return nil, status.Error(codes.InvalidArgument, "target group is required")
	}
	if err = validatePort(req.Endpoint.Port); err != nil {
		return nil, err
	}
	if req.Endpoint.Weight <= 0 || req.Endpoint.Weight > 100 {
		return nil, status.Errorf(codes.InvalidArgument, "endpoint weight value must be in: (0, 100]")
	}
	if err = validateIp(req.Endpoint.Ip); err != nil {
		return nil, err
	}
	opType := models.EventTypeAddEndpoint
	switch req.Operation {
	case cplpbv1.EndpointOperationType_ENDPOINT_OPERATION_TYPE_ADD:
		opType = models.EventTypeAddEndpoint
	case cplpbv1.EndpointOperationType_ENDPOINT_OPERATION_TYPE_REMOVE:
		opType = models.EventTypeRemoveEndpoint
	default:
		// case cplpbv1.EndpointOperationType_ENDPOINT_OPERATION_TYPE_UPDATE:
		// make delete + insert in one operation
		return nil, status.Errorf(codes.Unimplemented, "operation %s is unimplemented", req.Operation.String())
	}

	err = srv.runtime.UpsertEndpoint(ctx, models.EndpointEvent{
		Type:          opType,
		TargetGroupID: models.TargetGroupID(req.TargetGroupId.Value),
		Time:          time.Now(),
		Spec: models.EndpointSpec{
			IP:     net.ParseIP(req.Endpoint.Ip),
			Port:   uint16(req.Endpoint.Port),
			Weight: uint16(req.Endpoint.Weight),
		},
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to upsert endpoint: %v", err)
	}
	return
}
