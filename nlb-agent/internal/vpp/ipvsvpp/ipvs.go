// internal/vpp/ipvsvpp/ipvs.go
package ipvsvpp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
)

type IPVSVPP struct {
	log   zerolog.Logger
	mu    sync.Mutex
	specs map[models.TargetGroupID]models.TargetGroupSpec
}

func New(log zerolog.Logger) (*IPVSVPP, error) {
	for _, mod := range []string{"ip_vs", "ip_vs_rr", "ip_vs_wrr"} {
		if err := exec.Command("modprobe", mod).Run(); err != nil {
			log.Warn().Str("module", mod).Err(err).Msg("modprobe failed (may already be loaded)")
		}
	}

	// if err := enableIPForwarding(); err != nil {
	// 	return nil, fmt.Errorf("enabling ip forwarding: %w", err)
	// }

	log.Warn().Msg("IPVS VPP initialized")

	return &IPVSVPP{
		log:   log,
		specs: make(map[models.TargetGroupID]models.TargetGroupSpec),
	}, nil
}

func enableIPForwarding() error {
	if err := os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644); err != nil {
		out, err2 := exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("executing sysctl: %s: %w (write err: %v)", string(out), err2, err)
		}
	}
	return nil
}

func (v *IPVSVPP) ApplySpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	svcArgs := append(svcArgs(spec), "-s", "wrr")
	if err := ipvsRun("-A", svcArgs); err != nil {
		return fmt.Errorf("ipvs adding service: %w", err)
	}

	v.specs[tgID] = spec
	return nil
}

func (v *IPVSVPP) RemoveSpec(ctx context.Context, tgID models.TargetGroupID, spec models.TargetGroupSpec) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.log.Info().
		Str("tg", string(tgID)).
		Msg("removing spec")

	if err := ipvsRun("-D", svcArgs(spec)); err != nil {
		return fmt.Errorf("removing ipvs service: %w", err)
	}

	delete(v.specs, tgID)
	return nil
}

func (v *IPVSVPP) AddEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	spec, ok := v.specs[tgID]
	if !ok {
		return fmt.Errorf("spec for tg %s not found", tgID)
	}

	svc := svcArgs(spec)

	for _, ep := range endpoints {
		dest := destArgs(ep)

		if err := ipvsRun("-a", svc, dest); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				if err := ipvsRun("-e", svc, dest); err != nil {
					return fmt.Errorf("ipvs edit dest %s:%d: %w", ep.IP, ep.Port, err)
				}
				continue
			}
			return fmt.Errorf("adding ipvs dest %s:%d: %w", ep.IP, ep.Port, err)
		}
	}
	return nil
}

func (v *IPVSVPP) RemoveEndpoints(ctx context.Context, tgID models.TargetGroupID, endpoints []models.EndpointSpec) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	spec, ok := v.specs[tgID]
	if !ok {
		return nil
	}

	svc := svcArgs(spec)

	for _, ep := range endpoints {

		if err := ipvsRun("-d", svc, destArgsDeleteOnly(ep)); err != nil {
			return fmt.Errorf("removing ipvs endpoint: %s:%d", ep.IP, ep.Port)
		}
	}
	return nil
}

func svcArgs(spec models.TargetGroupSpec) []string {
	proto := "-t" // TCP
	if spec.Protocol == models.UDP {
		proto = "-u"
	}
	return []string{proto, fmt.Sprintf("0.0.0.0:%d", spec.Port)}
}

func destArgs(ep models.EndpointSpec) []string {
	return []string{
		"-r", fmt.Sprintf("%s:%d", ep.IP, ep.Port),
		"-w", fmt.Sprintf("%d", ep.Weight),
		"-m", // masquerading (NAT)
	}
}

func destArgsDeleteOnly(ep models.EndpointSpec) []string {
	return []string{
		"-r", fmt.Sprintf("%s:%d", ep.IP, ep.Port),
	}
}

func ipvsRun(action string, parts ...[]string) error {
	args := []string{action}
	for _, p := range parts {
		args = append(args, p...)
	}

	out, err := exec.Command("ipvsadm", args...).CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))

		if action == "-A" && strings.Contains(outStr, "already exists") {
			return nil
		}
		if (action == "-D" || action == "-d") && strings.Contains(outStr, "No such") {
			return nil
		}

		return fmt.Errorf("ipvsadm %v: %s: %w", args, outStr, err)
	}
	return nil
}
