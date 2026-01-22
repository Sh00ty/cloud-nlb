package main

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/Sh00ty/network-lb/health-check-node/internal/coordinator/repository/yugabyte"
	"github.com/Sh00ty/network-lb/health-check-node/pkg/healthcheck"
)

func main() {
	const count = 10000

	ctx := context.Background()
	r, err := yugabyte.NewRepo(ctx, "postgres", "postgres", "127.0.0.1", 5432)
	if err != nil {
		panic(err)
	}

	for i := 0; i < count; i++ {
		k := i
		ip := *(*[4]byte)(unsafe.Pointer(&k))
		trg := healthcheck.Target{
			SettingID: 3,
			RealIP:    (ip[:]),
			Port:      uint16(i % 10000),
		}
		err = r.CreateTarget(ctx, trg, "")
		if err != nil {
			fmt.Println(err)
		}
	}
}
