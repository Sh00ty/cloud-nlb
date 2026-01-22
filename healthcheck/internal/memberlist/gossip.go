package memberlist

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/rs/zerolog/log"

	"github.com/Sh00ty/network-lb/health-check-node/internal/models"
)

type Config struct {
	NodeName            string        `envconfig:"HC_WORKER_ID"`
	Port                int           `envconfig:"GOSSIP_PORT"`
	GossipProbeInterval time.Duration `envconfig:"GOSSIP_PROBE_INTERVAL"`
	GossipProbeTimeout  time.Duration `envconfig:"GOSSIP_PROBE_TIMEOUT"`
	SeedNodes           []string      `envconfig:"-"`
}

type MemberList struct {
	list      *memberlist.Memberlist
	seedNodes []string
}

func New(ctx context.Context, cfg Config, notify chan models.MemberShipEvent) (*MemberList, error) {
	const eventBufSize = 256

	events := make(chan memberlist.NodeEvent, eventBufSize)
	config := memberlist.DefaultLocalConfig()
	config.Name = cfg.NodeName
	config.BindPort = cfg.Port
	config.AdvertisePort = cfg.Port
	config.LogOutput = io.Discard
	config.ProbeInterval = cfg.GossipProbeInterval
	config.ProbeTimeout = cfg.GossipProbeTimeout
	config.Events = &memberlist.ChannelEventDelegate{
		Ch: events,
	}

	ml, err := memberlist.Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create memberlist: %w", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case mlEvent, opened := <-events:
				if !opened {
					return
				}
				log.Debug().Msgf(
					"got event from node %s: type=%d, node.status=%d",
					mlEvent.Node.Name,
					mlEvent.Event,
					mlEvent.Node.State,
				)
				eventType := models.MemberShipUnknown
				switch mlEvent.Event {
				case memberlist.NodeJoin:
					eventType = models.MemberShipNew
				case memberlist.NodeLeave:
					switch mlEvent.Node.State {
					case memberlist.StateLeft:
						eventType = models.MemberShipUpdating
					case memberlist.StateDead:
						eventType = models.MemberShipDead
					case memberlist.StateSuspect:
						eventType = models.MemberShipSuspect
					case memberlist.StateAlive:
						// что за бред
						eventType = models.MemberShipDead
					}
				case memberlist.NodeUpdate:
					if mlEvent.Node.State == memberlist.StateSuspect {
						// будем использовать для prefetch необходимых vshards
						// как только ловим dead то у нас уже сразу буду необходимы
						// проверки
						// need to add jitter
						// time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
						eventType = models.MemberShipSuspect
					}
				}
				if eventType == models.MemberShipUnknown {
					log.Warn().Msgf(
						"got unknown event from node %s: type=%d, node.status=%d",
						mlEvent.Node.Name,
						mlEvent.Event,
						mlEvent.Node.State,
					)
					continue
				}
				event := models.MemberShipEvent{
					Type: eventType,
					From: models.NodeID(mlEvent.Node.Name),
				}
				select {
				case notify <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return &MemberList{
		list:      ml,
		seedNodes: cfg.SeedNodes,
	}, nil
}

func (l *MemberList) Join(ctx context.Context) error {
	_, err := l.list.Join(l.seedNodes)
	if err != nil {
		return fmt.Errorf("failed to join memberlist: %w", err)
	}
	return nil
}

func (l *MemberList) GrasefullClose(timeout time.Duration) error {
	log.Warn().Msg("start gracefull leaving from gossip cluster")

	return l.list.Leave(timeout)
}

func (l *MemberList) Close() error {
	log.Warn().Msg("force live gossip cluter")

	return l.list.Shutdown()
}
