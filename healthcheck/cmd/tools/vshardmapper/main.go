package main

import (
	"fmt"
	"net"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/consistent"
	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/sharder"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
)

func main() {
	ch := consistent.NewCirle()
	ch2 := consistent.NewCirle()

	targets := []healthcheck.TargetAddr{
		{
			RealIP: net.IPv4(192, 168, 10, 10),
			Port:   80,
		},
		{
			RealIP: net.IPv4(192, 168, 10, 11),
			Port:   80,
		},
		{
			RealIP: net.IPv4(192, 168, 10, 12),
			Port:   80,
		},

		{
			RealIP: net.IPv4(10, 0, 0, 5),
			Port:   443,
		},
		{
			RealIP: net.IPv4(10, 0, 0, 6),
			Port:   443,
		},

		{
			RealIP: net.IPv4(172, 16, 0, 100),
			Port:   5432,
		},
		{
			RealIP: net.IPv4(172, 16, 0, 101),
			Port:   5432,
		},

		{
			RealIP: net.IPv4(192, 168, 5, 2),
			Port:   9050,
		},
		{
			RealIP: net.IPv4(192, 168, 5, 2),
			Port:   9051,
		},
		{
			RealIP: net.IPv4(192, 168, 5, 2),
			Port:   9052,
		},
		{
			RealIP: net.IPv4(192, 168, 5, 2),
			Port:   9053,
		},
	}

	sh, err := sharder.NewConsistentSharder(ch, ch2, 300, 2, "hc-worker-1")
	if err != nil {
		panic(err)
	}

	for _, target := range targets {
		tk := target.String()
		vs := sh.GetTargetVshard(tk)
		fmt.Printf("\n%s: %d", tk, vs)
	}
}
