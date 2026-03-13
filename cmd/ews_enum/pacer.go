package main

import (
	"sync"
	"time"
)

// requestPacer enforces a minimum delay between request starts across all workers.
type requestPacer struct {
	delay time.Duration
	mu    sync.Mutex
	next  time.Time
}

func newRequestPacer(delayMS int) *requestPacer {
	if delayMS <= 0 {
		return nil
	}
	return &requestPacer{delay: time.Duration(delayMS) * time.Millisecond}
}

func (p *requestPacer) Wait(abort <-chan struct{}) bool {
	if p == nil {
		if abort == nil {
			return true
		}
		select {
		case <-abort:
			return false
		default:
			return true
		}
	}

	p.mu.Lock()
	now := time.Now()
	startAt := now
	if p.next.After(now) {
		startAt = p.next
	}
	p.next = startAt.Add(p.delay)
	wait := startAt.Sub(now)
	p.mu.Unlock()

	if wait <= 0 {
		if abort == nil {
			return true
		}
		select {
		case <-abort:
			return false
		default:
			return true
		}
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	if abort == nil {
		<-timer.C
		return true
	}

	select {
	case <-abort:
		return false
	case <-timer.C:
		return true
	}
}
