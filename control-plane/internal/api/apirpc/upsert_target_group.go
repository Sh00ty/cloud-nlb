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

// Создание или обновление таргет-группы
// Идемпотентная операция: повторный вызов с тем же idempotency_key
// вернёт тот же результат без побочных эффектов.
func (srv *Server) UpsertTargetGroupSpec(
	ctx context.Context,
	req *cplpbv1.UpsertTargetGroupSpecRequest,
) (resp *cplpbv1.UpsertTargetGroupSpecResponse, err error) {
	if req.Id == nil {
		return nil, status.Error(codes.InvalidArgument, "target group id is required")
	}
	if req.Spec == nil {
		return nil, status.Error(codes.InvalidArgument, "target group spec is required")
	}
	if err = validatePort(req.Spec.Port); err != nil {
		return nil, err
	}
	if err = validateIp(req.Spec.VirtualIp); err != nil {
		return nil, err
	}
	proto := models.TCP
	switch req.Spec.Protocol {
	case cplpbv1.Protocol_PROTOCOL_TCP:
		proto = models.TCP
	case cplpbv1.Protocol_PROTOCOL_UDP:
		proto = models.UDP
	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported target group protocol")
	}
	err = srv.runtime.UpsertTargetGroupSpec(ctx, models.TargetGroupSpec{
		ID:        models.TargetGroupID(req.Id.Value),
		Proto:     proto,
		Port:      uint16(req.Spec.Port),
		VirtualIP: net.ParseIP(req.Spec.VirtualIp),
		Time:      time.Now(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to upsert target group spec: %v", err)
	}
	return nil, nil
}
