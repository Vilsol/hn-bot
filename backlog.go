package main

import (
	"context"
	"log"
	"math"
	"sync"
	"time"
)

const (
	backlogMaxAttempts = 10
	backlogBaseBackoff = 30 * time.Second
	backlogMaxBackoff  = time.Hour
	backlogDrainTick   = 30 * time.Second
)

type Pending struct {
	ID        int64
	Title     string
	URL       string
	Attempts  int
	NextRetry time.Time
}

type Backlog struct {
	mu      sync.Mutex
	entries map[int64]*Pending
}

func NewBacklog() *Backlog {
	return &Backlog{entries: map[int64]*Pending{}}
}

// Add records a failed send and schedules a retry, or drops the entry if it
// has already exceeded the max-attempts limit.
func (b *Backlog) Add(id int64, title, url string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.entries[id]
	if !ok {
		p = &Pending{ID: id, Title: title, URL: url}
		b.entries[id] = p
	}
	p.Attempts++
	if p.Attempts >= backlogMaxAttempts {
		log.Printf("backlog: dropping [%d] %s after %d attempts", p.ID, p.Title, p.Attempts)
		delete(b.entries, id)
		return
	}
	p.NextRetry = time.Now().Add(backoff(p.Attempts))
}

// Drain attempts to send every entry whose NextRetry has passed. Successful
// sends are removed; failures are rescheduled (or dropped at the cap) via Add.
func (b *Backlog) Drain(ctx context.Context, send Sender) {
	now := time.Now()
	var due []Pending
	b.mu.Lock()
	for _, p := range b.entries {
		if !p.NextRetry.After(now) {
			due = append(due, *p)
		}
	}
	b.mu.Unlock()

	for _, p := range due {
		if ctx.Err() != nil {
			return
		}
		if err := send.Send(ctx, p.ID, p.Title, p.URL); err != nil {
			log.Printf("backlog: retry [%d] failed: %v", p.ID, err)
			b.Add(p.ID, p.Title, p.URL)
			continue
		}
		log.Printf("backlog: sent [%d] %s", p.ID, p.Title)
		b.mu.Lock()
		delete(b.entries, p.ID)
		b.mu.Unlock()
	}
}

func (b *Backlog) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

// Snapshot returns a copy of the current entries, ordered by id, for logging
// in dry-run mode.
func (b *Backlog) Snapshot() []Pending {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Pending, 0, len(b.entries))
	for _, p := range b.entries {
		out = append(out, *p)
	}
	return out
}

func backoff(attempts int) time.Duration {
	d := time.Duration(float64(backlogBaseBackoff) * math.Pow(2, float64(attempts-1)))
	if d <= 0 || d > backlogMaxBackoff {
		return backlogMaxBackoff
	}
	return d
}
