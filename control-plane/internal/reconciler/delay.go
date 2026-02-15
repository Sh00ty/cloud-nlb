package reconciler

import (
	"fmt"
	"time"
)

type delayedEvent struct {
	ev      Event
	applyAt time.Time
}

func (e delayedEvent) String() string {
	return fmt.Sprintf("{event=%s, apply_at=%s}", e.ev, e.applyAt.Format(time.DateTime))
}

func (r *Reconciler) delayEvent(event Event, delayDuration time.Duration) {
	ev := delayedEvent{
		ev:      event,
		applyAt: time.Now().Add(delayDuration),
	}

	r.log.Info().Msgf("delayed event: %v", ev)

	front := r.delayedEvents.Front()
	if front == nil {
		r.delayEventTimer.Reset(time.Until(ev.applyAt))
		r.delayedEvents.PushFront(ev)
		return
	}
	frontEv := front.Value.(delayedEvent)
	if frontEv.applyAt.After(ev.applyAt) {
		r.delayEventTimer.Reset(time.Until(ev.applyAt))
		r.delayedEvents.PushFront(ev)
		return
	}
	for i := front.Next(); i != nil; i = i.Next() {
		listEv := i.Value.(delayedEvent)
		if listEv.applyAt.After(ev.applyAt) {
			r.delayedEvents.InsertBefore(ev, i)
			return
		}
	}
	r.delayedEvents.PushBack(ev)
}

func (r *Reconciler) handleDelayedEvent() {
	var (
		front = r.delayedEvents.Front()
		ev    = front.Value.(delayedEvent)
	)
	r.delayedEvents.Remove(front)

	r.log.Info().Msgf("got delayed event: %v", ev.ev)

	r.processIncomingEvent(ev.ev, false)
	if r.delayedEvents.Len() == 0 {
		return
	}
	newFront := r.delayedEvents.Front()
	r.delayEventTimer.Reset(time.Until(newFront.Value.(delayedEvent).applyAt))
}
