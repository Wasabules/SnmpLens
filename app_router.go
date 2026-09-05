package main

import (
	"log"
	"sync"
	"time"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/storage"
)

// Routing off the producer's goroutine.
//
// A trap arrives on gosnmp's UDP receive loop, which is STRICTLY SERIAL: one
// goroutine, ReadFromUDP then handler then the next read, with no goroutine per
// datagram. Every millisecond the handler spends is a millisecond not reading
// the socket, and what does not fit the socket buffer is dropped by the kernel
// before Go ever sees it — silently, with no error and nothing to count.
//
// Measured, per event: the journal insert costs 0.97 ms and routing another
// 1.30 ms, essentially all of it a SECOND write transaction. Moving that off the
// loop roughly halves the service time. Batching is what makes it work rather
// than just relocating the queue: unbatched, the router costs the same 1.0 ms
// per event and cannot keep up with a producer it just made twice as fast. At 50
// events per transaction it costs 0.13 ms.
//
// The INSERT stays on the producer's goroutine. gosnmp sends an INFORM's
// acknowledgement after the handler returns, and acknowledging a confirmed
// notification before it is durably journalled would be a lie.

const (
	// routeQueueDepth is how many journalled events may be waiting to be routed.
	//
	// A full queue is not a failure: the producer routes inline instead, which
	// is what it did before any of this existed. So this trades memory for how
	// large a burst can be absorbed before falling back to the slower path.
	routeQueueDepth = 2048

	// routeBatchSize bounds ONE transaction, and therefore how long the write
	// lock is held against the trap listener's insert.
	//
	// Not the whole queue. SQLite serialises writers; 10 000 rows was measured
	// at 392 ms and a replay of the full retention caps would be about 14
	// seconds — long enough for the busy timeout to expire and a trap to be lost
	// while its INFORM is acknowledged. At 50 events a transaction measures
	// about 4 ms.
	routeBatchSize = 50

	// routeFlushInterval bounds how long a queued event waits when the batch
	// never fills. An alert is not urgent to the millisecond, but it should not
	// sit behind a quiet period either.
	routeFlushInterval = 250 * time.Millisecond

	// routeStopGrace bounds the wait at shutdown, for the same reason
	// notify.StopGrace does: an application that will not close is worse than a
	// flush that finishes on the next launch, which the watermark makes safe.
	routeStopGrace = 5 * time.Second
)

// eventRouter carries journalled events from their producer to the outbox.
type eventRouter struct {
	app   *App
	queue chan events.Event
	done  chan struct{}
	loop  sync.WaitGroup
	once  sync.Once

	// mu guards the in-flight bookkeeping below.
	//
	// seq order is NOT routing order: seq is allocated by the insert, and the
	// event reaches the queue some time later — measured 2.4 ms, p95 7.5 ms.
	// Two events can therefore be handed over in the opposite order to their
	// seqs, so the watermark cannot be "the highest seq in the batch I just
	// flushed": that would strand a lower-seq event still waiting.
	//
	// What is safe is the LOWEST seq still in flight, minus one.
	mu        sync.Mutex
	inflight  map[int64]bool
	highest   int64 // the highest seq ever accepted
	confirmed int64 // the highest seq known to be written
}

func newEventRouter(a *App) *eventRouter {
	return &eventRouter{
		app:      a,
		queue:    make(chan events.Event, routeQueueDepth),
		done:     make(chan struct{}),
		inflight: map[int64]bool{},
	}
}

// accept registers an event as owed and hands it to the router.
//
// Returns false when the queue is full, and the caller must then route inline —
// never drop. The fallback is exactly today's behaviour, so the worst case of
// this whole design is the performance we had before it.
func (r *eventRouter) accept(e events.Event) bool {
	r.mu.Lock()
	if e.Seq > r.highest {
		r.highest = e.Seq
	}
	r.inflight[e.Seq] = true
	r.mu.Unlock()

	select {
	case r.queue <- e:
		return true
	default:
		// No room. Take it back out of flight; the caller routes it now.
		r.settle(nil, e.Seq)
		return false
	}
}

// settle marks seqs as no longer owed and returns the watermark that is now
// safe to record.
func (r *eventRouter) settle(seqs []int64, single int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, s := range seqs {
		delete(r.inflight, s)
	}
	if single != 0 {
		delete(r.inflight, single)
	}

	// The lowest seq still owed bounds everything: nothing at or above it can
	// be claimed as routed.
	lowest := int64(0)
	for s := range r.inflight {
		if lowest == 0 || s < lowest {
			lowest = s
		}
	}
	mark := r.highest
	if lowest > 0 {
		mark = lowest - 1
	}
	if mark > r.confirmed {
		r.confirmed = mark
	}
	return r.confirmed
}

func (r *eventRouter) start() {
	r.loop.Add(1)
	go func() {
		defer r.loop.Done()
		ticker := time.NewTicker(routeFlushInterval)
		defer ticker.Stop()

		batch := make([]events.Event, 0, routeBatchSize)
		for {
			select {
			case <-r.done:
				// Drain what is already queued, then stop. The queue is closed
				// to new work by the caller stopping its producers first.
				r.drain(&batch)
				return

			case e := <-r.queue:
				batch = append(batch, e)
				if len(batch) >= routeBatchSize {
					r.flush(&batch)
				}

			case <-ticker.C:
				r.flush(&batch)
			}
		}
	}()
}

// drain empties the queue at shutdown, in bounded transactions.
func (r *eventRouter) drain(batch *[]events.Event) {
	for {
		select {
		case e := <-r.queue:
			*batch = append(*batch, e)
			if len(*batch) >= routeBatchSize {
				r.flush(batch)
			}
		default:
			r.flush(batch)
			return
		}
	}
}

// flush routes a batch and writes it, with the watermark, in one transaction.
func (r *eventRouter) flush(batch *[]events.Event) {
	if len(*batch) == 0 {
		return
	}
	pending := *batch
	*batch = (*batch)[:0]

	groups := make([]storage.RoutedGroup, 0, len(pending))
	seqs := make([]int64, 0, len(pending))
	var failed int
	for _, e := range pending {
		g, err := r.app.routedGroupsFor(e)
		if err != nil {
			// Not routed, so it must NOT be settled: leaving it in flight pins
			// the watermark below it, and the next launch replays it. Claiming
			// it here would deliver it nowhere and replay it never.
			//
			// Best effort at retrying it in this process too; if the queue is
			// full, the watermark simply stays put until a restart.
			failed++
			select {
			case r.queue <- e:
			default:
			}
			continue
		}
		seqs = append(seqs, e.Seq)
		groups = append(groups, g...)
	}
	if failed > 0 {
		log.Printf("notify: %d of %d events could not be routed and stay above the "+
			"watermark", failed, len(pending))
	}

	mark := r.settle(seqs, 0)
	if err := r.app.storage.EnqueueRouted(groups, mark); err != nil {
		// The events stay above the watermark, because settle only ever moves
		// the number we intend to write and EnqueueRouted is atomic — so a
		// failure leaves the stored watermark where it was, and the next launch
		// replays them. Copying flushBatch, which drops its buffer before the
		// write and only logs, would lose the whole batch here.
		log.Printf("notify: routing %d events failed, they will be replayed on the "+
			"next launch: %v", len(pending), err)
		return
	}
	if r.app.dispatcher != nil {
		r.app.dispatcher.Wake()
	}
}

// stop drains what is queued and waits, bounded.
func (r *eventRouter) stop() {
	r.once.Do(func() { close(r.done) })

	finished := make(chan struct{})
	go func() {
		r.loop.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(routeStopGrace):
		log.Printf("notify: routing was still running after %s; the events it had "+
			"not written stay above the watermark and are replayed next launch",
			routeStopGrace)
	}
}

// replayUnrouted routes everything journalled above the watermark.
//
// This is what makes async routing as durable as doing it inline: a crash
// between the insert and the flush loses nothing, because the journal is the
// record and the watermark says how far routing got.
func (a *App) replayUnrouted() {
	if a.storage == nil {
		return
	}
	mark, err := a.storage.RoutedThrough()
	if err != nil {
		log.Printf("notify: could not read how far routing got (%v); not replaying", err)
		return
	}

	total := 0
	for {
		pending, err := a.storage.EventsAfter(mark, routeBatchSize)
		if err != nil {
			log.Printf("notify: replay stopped after %d events: %v", total, err)
			return
		}
		if len(pending) == 0 {
			break
		}

		groups := make([]storage.RoutedGroup, 0, len(pending))
		for _, e := range pending {
			g, err := a.routedGroupsFor(e)
			if err != nil {
				// Stop rather than skip: advancing past an event that could not
				// be routed would lose it for good, and replay is the last
				// chance it gets.
				log.Printf("notify: replay stopped at seq %d after %d events: %v",
					e.Seq, total, err)
				return
			}
			groups = append(groups, g...)
		}
		mark = pending[len(pending)-1].Seq
		if err := a.storage.EnqueueRouted(groups, mark); err != nil {
			log.Printf("notify: replay stopped after %d events: %v", total, err)
			return
		}
		total += len(pending)
	}

	if total > 0 {
		log.Printf("notify: replayed %d events that were journalled but not routed", total)
		if a.dispatcher != nil {
			a.dispatcher.Wake()
		}
	}
}
