package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"net"

	"github.com/rs/zerolog"

	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/endpointshc"
	"github.com/Sh00ty/cloud-nlb/nlb-agent/internal/models"
	kafka "github.com/segmentio/kafka-go"
)

type EndpointStatusesService interface {
	UpdateEndpointsStatuses(ctx context.Context, statuses map[models.TargetGroupID][]models.EndpointStatus) error
	IsWatchFor(ctx context.Context, tgID models.TargetGroupID) bool
	RemoveEndpoint(ctx context.Context, tgID models.TargetGroupID, ep endpointshc.EndpointKey) error
}

type StatusesWatcher struct {
	msgReader *kafka.Reader
	log       zerolog.Logger
	svc       EndpointStatusesService
}

func NewStatusesWatcher(
	ctx context.Context,
	nodeID string,
	addr string,
	topic string,
	svc EndpointStatusesService,
	log zerolog.Logger,
) *StatusesWatcher {
	log = log.With().Str("component", "ep_statuses_watcher").Logger()
	reader := kafka.NewReader(
		kafka.ReaderConfig{
			Brokers:     []string{addr},
			Topic:       topic,
			MaxBytes:    10 * 1024 * 1024,
			GroupID:     nodeID,
			StartOffset: kafka.LastOffset,
		})
	return &StatusesWatcher{
		msgReader: reader,
		log:       log,
		svc:       svc,
	}
}

func (w *StatusesWatcher) RunEndpointStatusesWatcher(ctx context.Context) error {
	for {
		msg, err := w.msgReader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			_ = w.msgReader.CommitMessages(ctx, msg)
			continue
		}

		goMsg := Value[EndpointStatusDto]{}
		err = json.Unmarshal(msg.Value, &goMsg)
		if err != nil {
			w.log.Error().Err(err).Msg("failed to decode message from json")
			_ = w.msgReader.CommitMessages(ctx, msg)
			continue
		}

		var (
			log    = w.log.With().Interface("message", goMsg).Logger()
			epStat = models.EndpointStatus{}
		)
		switch goMsg.Op {
		case "c", "r", "u":
			tgID := models.TargetGroupID(goMsg.After.TargetGroup)
			if !w.svc.IsWatchFor(ctx, epStat.Header.TargetGroupID) {
				log.Debug().Str("tg_id", string(tgID)).Msg("not updated endpoint status change: not watching for this target group")
				continue
			}

			epStat.Header = models.EndpointHdr{
				TargetGroupID: tgID,
				IP:            net.ParseIP(goMsg.After.RealIP),
				Port:          uint16(goMsg.After.Port),
			}
			epStat.Healthy = goMsg.After.Status
			epStat.UpdatedAt = goMsg.After.UpdatedAt
			statuses := map[models.TargetGroupID][]models.EndpointStatus{
				tgID: {epStat},
			}
			err := w.svc.UpdateEndpointsStatuses(ctx, statuses)
			if err != nil {
				log.Error().Err(err).Msg("failed to remove endpoint message")
				continue
			}
			log.Debug().Msg("updated endpoint status")
		case "d":
			epStat.Header = models.EndpointHdr{
				TargetGroupID: models.TargetGroupID(goMsg.Before.TargetGroup),
				IP:            net.ParseIP(goMsg.Before.RealIP),
				Port:          uint16(goMsg.Before.Port),
			}
			epStat.Healthy = goMsg.Before.Status
			epStat.UpdatedAt = goMsg.Before.UpdatedAt

			err := w.svc.RemoveEndpoint(ctx, epStat.Header.TargetGroupID, endpointshc.EpStatusKey(epStat))
			if err != nil {
				log.Error().Err(err).Msg("failed to remove endpoint message")
				continue
			}
			log.Debug().Msg("removed endpoint status")
		default:
			log.Info().Msgf("skipped change message with unknown op")
		}
		err = w.msgReader.CommitMessages(ctx, msg)
		if err != nil {
			log.Error().Err(err).Msg("failed to commit message: it will doubled")
		}
	}
}

func (w *StatusesWatcher) Close(ctx context.Context) error {
	return w.msgReader.Close()
}
