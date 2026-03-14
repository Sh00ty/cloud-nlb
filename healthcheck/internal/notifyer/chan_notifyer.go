package notifyer

import (
	"sync/atomic"

	"github.com/Sh00ty/cloud-nlb/health-check-node/internal/models"
)

type ChanNotifier struct {
	eventChan chan models.HcEvent
	closed    atomic.Bool
	close     chan struct{}
}

func NewNotifier(buf int) *ChanNotifier {
	return &ChanNotifier{
		eventChan: make(chan models.HcEvent, buf),
		closed:    atomic.Bool{},
		close:     make(chan struct{}),
	}
}

func (n *ChanNotifier) NotifyHcStatusChanged(event models.HcEvent) {
	if n.closed.Load() {
		return
	}
	select {
	case n.eventChan <- event:
	case <-n.close:
	default:
		if n.closed.Load() {
			return
		}
		// TODO: обработчик не справляется, видимо нужно еще
		// куда-то в более приоритетное место сообщить
		// что есть проблемы, расшардируйте меня!!!
		select {
		case n.eventChan <- event:
		case <-n.close:
		}
	}
}

func (n *ChanNotifier) GetEventChan() chan models.HcEvent {
	return n.eventChan
}

func (n *ChanNotifier) Close() {
	n.closed.Store(true)
	close(n.close)
	close(n.eventChan)
}
