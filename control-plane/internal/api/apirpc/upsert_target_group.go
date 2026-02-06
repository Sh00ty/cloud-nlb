package apirpc

import (
	"context"

	"github.com/Sh00ty/network-lb/control-plane/pkg/protobuf/api/proto/cplpbv1"
)

// Создание или обновление таргет-группы
// Идемпотентная операция: повторный вызов с тем же idempotency_key
// вернёт тот же результат без побочных эффектов.
func (srv *Server) UpsertTargetGroupSpec(
	ctx context.Context,
	req *cplpbv1.UpsertTargetGroupSpecRequest,
) (resp *cplpbv1.UpsertTargetGroupSpecResponse, err error) {
	return nil, nil
}
