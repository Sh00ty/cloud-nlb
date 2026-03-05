package reconciler

import (
	"context"
	"fmt"
	"sync"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	"github.com/rs/zerolog"
	"go.uber.org/multierr"
)

type reconcileTask struct {
	tgID      models.TargetGroupID
	lastError error
	attempt   uint8
}

type worker struct {
	id int

	stor                   Storage
	endpointsStatusManager EndpointsStatusManager
	vpp                    VPPManager

	mu          sync.Mutex
	pending     map[models.TargetGroupID]struct{}
	queue       chan reconcileTask
	maxAttempts uint8

	log zerolog.Logger
}

func (w *worker) enqueue(task reconcileTask) {
	if task.attempt > w.maxAttempts {
		w.log.Error().
			Err(task.lastError).
			Str("tg_id", string(task.tgID)).
			Msg("drop reconciliation task: max reconcile attempts exceeded")
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.pending[task.tgID]; exists {
		return
	}

	select {
	case w.queue <- task:
		w.pending[task.tgID] = struct{}{}
	default:
		w.log.Warn().
			Str("tg_id", string(task.tgID)).
			Msg("worker queue full, dropping reconcile task")
	}
}

func (w *worker) run(ctx context.Context) {
	w.log.Info().Msg("worker started")

	for {
		select {
		case <-ctx.Done():
			w.log.Info().Msg("worker stopped")
			return
		case task := <-w.queue:
			w.removePending(task.tgID)
			w.processTask(ctx, task)
		}
	}
}

func (w *worker) removePending(tgID models.TargetGroupID) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.pending, tgID)
}

func (w *worker) processTask(ctx context.Context, task reconcileTask) {
	log := w.log.With().Str("tg_id", string(task.tgID)).Logger()

	reconciled, err := w.reconcileTargetGroup(ctx, task.tgID)
	if err != nil {
		log.Error().Err(err).Msg("reconciliation failed, will retry on next cycle")

		task.lastError = multierr.Append(task.lastError, err)
		task.attempt++
		w.enqueue(task)
		return
	}

	if reconciled {
		log.Info().Msg("reconciliation completed")
	} else {
		log.Info().Msg("already in sync")
	}
}

func (w *worker) reconcileTargetGroup(ctx context.Context, tgID models.TargetGroupID) (bool, error) {
	var (
		desiredSpec, hasDesired = w.stor.GetDesiredSpec(tgID)
		actualSpec, hasActual   = w.stor.GetActualSpec(ctx, tgID)
	)
	if !hasDesired {
		if hasActual {
			return true, w.removeTG(ctx, tgID, actualSpec)
		}
		return false, nil
	}

	changed := false

	if !hasActual || actualSpec.Version != desiredSpec.Version {
		if err := w.reconcileSpec(ctx, tgID, desiredSpec); err != nil {
			return changed, fmt.Errorf("reconciling tg spec: %w", err)
		}
		changed = true
	}

	var (
		desiredEps, hasDesiredEps = w.stor.GetDesiredEndpoints(ctx, tgID)
		actualEps, hasActualEps   = w.stor.GetActualEndpoints(ctx, tgID)
	)

	needEpReconcile := hasDesiredEps && (!hasActualEps || actualEps.Version != desiredEps.Version)
	if needEpReconcile {
		if err := w.reconcileEndpoints(ctx, tgID, desiredEps, actualEps); err != nil {
			return changed, fmt.Errorf("reconciling endpoints: %w", err)
		}
		changed = true
	}

	return changed, nil
}

func (w *worker) reconcileSpec(ctx context.Context, tgID models.TargetGroupID, desired *VersionedSpec) error {
	w.log.Debug().
		Str("tg_id", string(tgID)).
		Uint64("version", desired.Version).
		Msg("applying spec to VPP")

	if err := w.vpp.ApplySpec(ctx, tgID, desired.Spec); err != nil {
		return fmt.Errorf("applying spec in vpp: %w", err)
	}

	if err := w.stor.SetActualSpec(ctx, tgID, desired.Spec, desired.Version); err != nil {
		return fmt.Errorf("persisting actual spec: %w", err)
	}
	return nil
}

func (w *worker) reconcileEndpoints(
	ctx context.Context,
	tgID models.TargetGroupID,
	desired *VersionedEndpoints,
	actual *VersionedEndpoints,
) error {
	var actualEps []models.EndpointSpec
	if actual != nil {
		actualEps = actual.Endpoints
	}

	w.log.Debug().
		Str("tg_id", string(tgID)).
		Uint64("version", desired.Version).
		Int("desired_count", len(desired.Endpoints)).
		Int("actual_count", len(actualEps)).
		Msg("applying endpoints to VPP")

	toAdd, toDelete := w.getEndpointsDiff(ctx, tgID, desired.Endpoints, actualEps)
	if err := w.vpp.RemoveEndpoints(ctx, tgID, toDelete); err != nil {
		return fmt.Errorf("removing endpoints from vpp: %w", err)
	}
	if err := w.vpp.AddEndpoints(ctx, tgID, toAdd); err != nil {
		return fmt.Errorf("adding endpoints to vpp: %w", err)
	}
	if err := w.stor.SetActualEndpoints(ctx, tgID, desired.Endpoints, desired.Version); err != nil {
		return fmt.Errorf("persisting actual endpoints: %w", err)
	}

	return nil
}

func (w *worker) getEndpointsDiff(
	ctx context.Context,
	tgID models.TargetGroupID,
	desired, actual []models.EndpointSpec,
) (toAdd []models.EndpointSpec, toDelete []models.EndpointSpec) {
	type epKey struct {
		ip   string
		port uint16
	}

	var (
		desiredMap = make(map[epKey]models.EndpointSpec, len(desired))
		actualMap  = make(map[epKey]models.EndpointSpec, len(actual))
	)
	for _, ep := range desired {
		desiredMap[epKey{
			ip:   ep.IP.String(),
			port: ep.Port,
		}] = ep
	}
	for _, ep := range actual {
		actualMap[epKey{
			ip:   ep.IP.String(),
			port: ep.Port,
		}] = ep
	}

	for k, desiredEp := range desiredMap {
		healthy := w.endpointsStatusManager.GetEndpointsStatus(ctx, tgID, models.EndpointHdr{
			TargetGroupID: tgID,
			IP:            desiredEp.IP,
			Port:          desiredEp.Port,
		})

		actualEp, exists := actualMap[k]
		if !exists && healthy {
			toAdd = append(toAdd, desiredEp)
			continue
		}
		if desiredEp.Weight != actualEp.Weight {
			toAdd = append(toAdd, desiredEp)
			toDelete = append(toDelete, actualEp)
		}
	}
	for k, actualEp := range actualMap {
		healthy := w.endpointsStatusManager.GetEndpointsStatus(ctx, tgID, models.EndpointHdr{
			TargetGroupID: tgID,
			IP:            actualEp.IP,
			Port:          actualEp.Port,
		})

		_, exists := desiredMap[k]
		if !exists || !healthy {
			toDelete = append(toDelete, actualEp)
		}
	}
	return
}

func (w *worker) removeTG(ctx context.Context, tgID models.TargetGroupID, spec *VersionedSpec) error {
	w.log.Info().
		Str("tg_id", string(tgID)).
		Msg("removing target group from VPP")

	actualEps, _ := w.stor.GetActualEndpoints(ctx, tgID)
	if actualEps != nil {
		if err := w.vpp.RemoveEndpoints(ctx, tgID, actualEps.Endpoints); err != nil {
			return fmt.Errorf("removing endpoints from vpp: %w", err)
		}
	}
	if spec != nil {
		if err := w.vpp.RemoveSpec(ctx, tgID, spec.Spec); err != nil {
			return fmt.Errorf("removing tg spec from vpp: %w", err)
		}
	}
	if err := w.stor.DeleteActual(ctx, tgID); err != nil {
		return fmt.Errorf("deleting actual data from storage: %w", err)
	}
	return nil
}
