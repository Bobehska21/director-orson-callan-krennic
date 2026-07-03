// Package debounce coalesces a burst of events for the same key into a single
// callback (trailing debounce) while guaranteeing a callback at least every
// maxWait, so a continuously-typing developer still gets periodic snapshots.
package debounce

import (
	"sync"
	"time"
)

// Debouncer fires fn(key) after quiet-period `wait`, or every `maxWait` at most.
type Debouncer struct {
	wait    time.Duration
	maxWait time.Duration
	fn      func(key string)

	mu     sync.Mutex
	states map[string]*state
	closed bool
}

type state struct {
	timer *time.Timer
	first time.Time
}

// New creates a Debouncer.
func New(wait, maxWait time.Duration, fn func(key string)) *Debouncer {
	return &Debouncer{wait: wait, maxWait: maxWait, fn: fn, states: map[string]*state{}}
}

// Trigger schedules (or reschedules) a callback for key.
func (d *Debouncer) Trigger(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	now := time.Now()
	st, ok := d.states[key]
	if !ok {
		st = &state{first: now}
		d.states[key] = st
		st.timer = time.AfterFunc(d.wait, func() { d.fire(key) })
		return
	}
	// Enforce the max-wait ceiling.
	if d.maxWait > 0 && now.Sub(st.first) >= d.maxWait {
		st.timer.Stop()
		go d.fire(key)
		return
	}
	st.timer.Reset(d.wait)
}

func (d *Debouncer) fire(key string) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	delete(d.states, key)
	fn := d.fn
	d.mu.Unlock()
	if fn != nil {
		fn(key)
	}
}

// Stop halts all pending timers.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	for _, st := range d.states {
		st.timer.Stop()
	}
	d.states = map[string]*state{}
}
