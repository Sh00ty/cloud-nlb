package targetwatcher

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/rs/zerolog/log"
	kafka "github.com/segmentio/kafka-go"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/coordinator"
	"github.com/Sh00ty/cloud-nlb/health-check-node/pkg/healthcheck"
)

type Coordinator interface {
	HandleTargetEvents(ctx context.Context, targetEvents []coordinator.TargetEvent) error
}

type CheckUpdateWatcher struct {
	msgReader *kafka.Reader
	crd       Coordinator
}

func NewCheckUpdateWatcher(ctx context.Context, nodeID string, addr string, topic string, crd Coordinator) *CheckUpdateWatcher {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{addr},
		Topic:       topic,
		MaxBytes:    10 * 1024 * 1024,
		GroupID:     nodeID,
		StartOffset: kafka.LastOffset,
	})
	return &CheckUpdateWatcher{
		msgReader: reader,
		crd:       crd,
	}
}

func (w *CheckUpdateWatcher) RunTargetWatcher(ctx context.Context) error {
	for {
		msg, err := w.msgReader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			_ = w.msgReader.CommitMessages(ctx, msg)
			continue
		}
		goMsg := Value[TargetDto]{}
		err = json.Unmarshal(msg.Value, &goMsg)
		if err != nil {
			log.Error().Err(err).Msg("failed to decode message from json")
			_ = w.msgReader.CommitMessages(ctx, msg)
			continue
		}

		var (
			eventOp = coordinator.Unknown
			target  = healthcheck.Target{}
			ts      = int64(goMsg.TsMs)
		)
		switch goMsg.Op {
		case "c", "r":
			eventOp = coordinator.Create
			target.TargetGroup = healthcheck.TargetGroupID(goMsg.After.TargetGroup)
			target.Port = uint16(goMsg.After.Port)
			target.RealIP = net.ParseIP(goMsg.After.RealIP)
		case "d":
			eventOp = coordinator.Delete
			target.TargetGroup = healthcheck.TargetGroupID(goMsg.Before.TargetGroup)
			target.Port = uint16(goMsg.Before.Port)
			target.RealIP = net.ParseIP(goMsg.Before.RealIP)
		case "u":
			eventOp = coordinator.Update
		default:
			eventOp = coordinator.Unknown
		}

		log.Info().Msgf("parsed cdc event: type=%d on target %+v", eventOp, target)

		err = w.crd.HandleTargetEvents(ctx, []coordinator.TargetEvent{
			{
				Operation: eventOp,
				Timestamp: time.Unix(int64(ts), 0),
				Target:    target,
			},
		})
		if err != nil {
			log.Error().Err(err).Msgf("failed to handle target event op %d: %+v", eventOp, target)
			continue
		}
		err = w.msgReader.CommitMessages(ctx, msg)
		if err != nil {
			log.Error().Err(err).Msg("failed to commit message: it will doubled")
		}
	}
}

func (w *CheckUpdateWatcher) Close(ctx context.Context) error {
	return w.msgReader.Close()
}
