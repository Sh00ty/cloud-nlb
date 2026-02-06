package etcd

import (
	"context"
	"sync/atomic"

	"github.com/rs/zerolog/log"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type Watcher struct {
	prefix          string
	handler         WatchHandler
	lastRevision    int64
	resetToRevision atomic.Pointer[int64]
	watcher         clientv3.Watcher
}

func NewWatcher(
	prefix string,
	handler WatchHandler,
	watcher clientv3.Watcher,
	startRevision int64,
) *Watcher {
	return &Watcher{
		prefix:       prefix,
		handler:      handler,
		watcher:      watcher,
		lastRevision: startRevision,
	}
}

func (w *Watcher) WatchEventlog(
	ctx context.Context,
) error {
	ctx = clientv3.WithRequireLeader(ctx)
	watch := func(rev int64) clientv3.WatchChan {
		return w.watcher.Watch(
			ctx,
			w.prefix,
			clientv3.WithRev(rev),
			clientv3.WithPrefix(),
			clientv3.WithCreatedNotify(),
			clientv3.WithFilterDelete(),
		)
	}
	var (
		watcherChan = watch(w.lastRevision)
		logger      = log.With().Str("prefix", w.prefix).Logger()
	)
	for {
		select {
		case event, ok := <-watcherChan:
			if !ok {
				logger.Info().Msg("watcher channel closed")
				return nil
			}
			if restartRev, need := w.checkNeedRestart(); need {
				logger.Warn().Msgf("restart watcher with revision %d", restartRev)
				w.lastRevision = restartRev
				watcherChan = watch(restartRev)
				continue
			}
			if event.Canceled {
				logger.Error().Err(event.Err()).Msg("watcher failure: canceled, retry")
				watcherChan = watch(w.lastRevision)
				continue
			}
			if event.Err() != nil {
				logger.Error().Err(event.Err()).Msg("got unexpected watch error")
				continue
			}
			w.lastRevision = event.Header.Revision
			if event.IsProgressNotify() {
				logger.Debug().Msgf(
					"got progress notify message with revision %d",
					w.lastRevision,
				)
				continue
			}
			err := w.handler(ctx, event.Events)
			if err != nil {
				logger.Error().Err(err).Msg("handler error, skip")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (w *Watcher) RestartFrom(revision int64) {
	w.resetToRevision.Store(&revision)
}

func (w *Watcher) checkNeedRestart() (int64, bool) {
	for {
		reset := w.resetToRevision.Load()
		if reset == nil {
			return 0, false
		}
		if !w.resetToRevision.CompareAndSwap(reset, nil) {
			continue
		}
		return *reset, true
	}
}
