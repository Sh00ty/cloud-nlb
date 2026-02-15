package apirpc

import (
	"net/netip"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func validatePort(port uint32) error {
	if port >= 1<<16 {
		return status.Errorf(codes.InvalidArgument, "port can't be more then 2^16")
	}
	if port <= 0 {
		return status.Errorf(codes.InvalidArgument, "port must be at least 1")
	}
	return nil
}

func validateIp(ip string) error {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid ip addr: %v", err)
	}
	if !addr.IsValid() || !addr.Is4() {
		return status.Errorf(codes.InvalidArgument, "invalid ip addr or unsupported v6 version: %v", err)
	}
	return nil
}
