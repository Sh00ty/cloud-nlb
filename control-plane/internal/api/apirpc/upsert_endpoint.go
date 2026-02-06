package apirpc

import (
	"context"

	"github.com/Sh00ty/network-lb/control-plane/pkg/protobuf/api/proto/cplpbv1"
)

// Добавление или удаление эндпоинта из таргет-группы
// Может вызываться вашим health-check сервисом через Redpanda или напрямую.
// Для массовых обновлений рекомендуется использовать пакетные операции.
func (srv *Server) UpsertEndpoint(
	ctx context.Context,
	req *cplpbv1.UpsertEndpointRequest,
) (resp *cplpbv1.UpsertEndpointResponse, err error) {
	return nil, nil
}
