package notify

import (
	"log"
	"sync"
	"time"

	"SnmpLens/pkg/events"
)

// Queued is one delivery waiting to be sent. It is deliberately self-contained:
// the dispatcher never reads the journal, so event retention can never strand a
// pending or dead delivery.
type Queued struct {
	ID       int64
	SinkID   string
	Subject  string
	Body     string
	Attempts int
	Event    events.Event
}

// Queue is the persistence the dispatcher needs. Implemented by pkg/storage.
type Queue interface {
	DueDeliveries(limit int) ([]Queued, error)
	MarkSent(id int64) error
	MarkFailed(id int64, errMsg string, nextTry time.Time, dead bool) error
}

// SinkResolver returns a live sink for an id, or false when the sink no longer
// exists or is disabled.
type SinkResolver func(sinkID string) (Sink, bool)

// StopGrace is how long Stop waits for a delivery already on the wire.
//
// Long enough for a syslog write or a webhook POST to a receiver that is
// answering, short enough that closing the window never feels stuck.
const StopGrace = 5 * time.Second

// Dispatcher drains the outbox on a ticker and on demand.
type Dispatcher struct {
	Queue    Queue
	Resolve  SinkResolver
	OnResult func(q Queued, err error, dead bool)

	interval time.Duration
	wake     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	running  sync.Mutex
	// loop is held by the drain goroutine for its whole life, so Stop can wait
	// for it rather than merely asking it to finish.
	loop sync.WaitGroup
}

// NewDispatcher returns a dispatcher that polls every interval.
func NewDispatcher(q Queue, resolve SinkResolver, interval time.Duration) *Dispatcher {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	return &Dispatcher{
		Queue:    q,
		Resolve:  resolve,
		interval: interval,
		// Buffered: a wake-up must never block the event insert path.
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

// Start runs the drain loop until Stop.
func (d *Dispatcher) Start() {
	d.loop.Add(1)
	go func() {
		defer d.loop.Done()
		ticker := time.NewTicker(d.interval)
		defer ticker.Stop()
		for {
			select {
			case <-d.done:
				return
			case <-ticker.C:
				d.Drain()
			case <-d.wake:
				d.Drain()
			}
		}
	}()
}

// Stop ends the drain loop and WAITS for a delivery already in flight.
//
// It used to close a channel and return, so the loop finished the pass it was
// in on its own goroutine with nobody waiting. On shutdown that means a send
// still running when the process exits: OnBeforeClose returns, Wails tears the
// app down, and an SMTP conversation or an HTTP POST is cut mid-way. The
// delivery is left `pending` with an attempt spent — neither sent nor reported
// as failed.
//
// The wait is BOUNDED. Drain stops starting new deliveries as soon as `done` is
// closed, so what is left to wait for is a single send — but that send is at the
// mercy of a relay that may be timing out, and an app that will not close is
// worse than an interrupted POST. After the grace period the delivery is
// abandoned in place: the row stays `pending` and the next launch picks it up.
//
// Safe to call more than once, and safe on a dispatcher that was never started.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() { close(d.done) })

	finished := make(chan struct{})
	go func() {
		d.loop.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(StopGrace):
		log.Printf("notify: a delivery was still in flight after %s; leaving it pending", StopGrace)
	}
}

// Wake asks for an immediate drain without blocking the caller.
func (d *Dispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default: // a drain is already pending; nothing to add
	}
}

// Drain sends every delivery that is due. Failures are rescheduled with
// exponential backoff; a permanent failure or too many attempts becomes a dead
// letter, which is kept for the operator to see rather than dropped.
func (d *Dispatcher) Drain() {
	// One drain at a time: a slow SMTP relay must not let the ticker stack up
	// overlapping passes that each retry the same rows.
	if !d.running.TryLock() {
		return
	}
	defer d.running.Unlock()

	if d.Queue == nil || d.Resolve == nil {
		return
	}

	batch, err := d.Queue.DueDeliveries(20)
	if err != nil {
		log.Printf("notify: reading the outbox failed: %v", err)
		return
	}

	for _, q := range batch {
		// Shutdown has begun: finish nothing new. A batch is up to 20
		// deliveries and an email sink waits 20 seconds by default, so
		// plowing on would hold the close for minutes. The rows stay
		// `pending` and are picked up on the next launch, which is what a
		// durable outbox is for.
		select {
		case <-d.done:
			return
		default:
		}

		sink, ok := d.Resolve(q.SinkID)
		if !ok {
			// The sink was deleted or disabled after the event was queued.
			// Dead-letter it rather than retrying forever against nothing.
			d.fail(q, errSinkMissing{q.SinkID}, true)
			continue
		}

		sendErr := sink.Send(q.Event, q.Subject, q.Body)
		if sendErr == nil {
			if err := d.Queue.MarkSent(q.ID); err != nil {
				log.Printf("notify: could not mark delivery %d sent: %v", q.ID, err)
			}
			if d.OnResult != nil {
				d.OnResult(q, nil, false)
			}
			continue
		}

		dead := Permanent(sendErr) || q.Attempts+1 >= MaxAttempts
		d.fail(q, sendErr, dead)
	}
}

func (d *Dispatcher) fail(q Queued, sendErr error, dead bool) {
	next := time.Now().Add(Backoff(q.Attempts + 1))
	if err := d.Queue.MarkFailed(q.ID, sendErr.Error(), next, dead); err != nil {
		log.Printf("notify: could not record delivery failure for %d: %v", q.ID, err)
	}
	if d.OnResult != nil {
		d.OnResult(q, sendErr, dead)
	}
}

type errSinkMissing struct{ id string }

func (e errSinkMissing) Error() string {
	return "sink " + e.id + " no longer exists or is disabled"
}
