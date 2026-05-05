package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSender struct {
	failures int
	sent     []int64
}

func (f *fakeSender) Send(_ context.Context, id int64, _, _ string) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("boom")
	}
	f.sent = append(f.sent, id)
	return nil
}

// forceDue makes every entry immediately drainable; saves us from waiting on
// real backoff timers in tests.
func (b *Backlog) forceDue() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.entries {
		p.NextRetry = time.Time{}
	}
}

func TestBacklogAddSchedulesRetry(t *testing.T) {
	b := NewBacklog()
	b.Add(1, "t", "u")
	if b.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", b.Len())
	}
	got := b.Snapshot()[0]
	if got.Attempts != 1 {
		t.Fatalf("attempts=%d, want 1", got.Attempts)
	}
	if !got.NextRetry.After(time.Now()) {
		t.Fatalf("NextRetry should be in the future, got %v", got.NextRetry)
	}
}

func TestBacklogDrainSuccessRemovesEntry(t *testing.T) {
	b := NewBacklog()
	b.Add(42, "title", "url")
	b.forceDue()

	s := &fakeSender{}
	b.Drain(context.Background(), s)

	if b.Len() != 0 {
		t.Fatalf("expected empty backlog, got %d", b.Len())
	}
	if len(s.sent) != 1 || s.sent[0] != 42 {
		t.Fatalf("expected send of 42, got %v", s.sent)
	}
}

func TestBacklogDrainFailureReschedules(t *testing.T) {
	b := NewBacklog()
	b.Add(1, "t", "u")
	b.forceDue()

	s := &fakeSender{failures: 1}
	b.Drain(context.Background(), s)

	if b.Len() != 1 {
		t.Fatalf("expected entry to remain, got Len=%d", b.Len())
	}
	got := b.Snapshot()[0]
	if got.Attempts != 2 {
		t.Fatalf("attempts=%d, want 2", got.Attempts)
	}
	if !got.NextRetry.After(time.Now()) {
		t.Fatalf("NextRetry should be future, got %v", got.NextRetry)
	}
}

func TestBacklogDropsAfterMaxAttempts(t *testing.T) {
	b := NewBacklog()
	for i := 0; i < backlogMaxAttempts; i++ {
		b.Add(7, "t", "u")
	}
	if b.Len() != 0 {
		t.Fatalf("expected entry dropped after %d attempts, got Len=%d", backlogMaxAttempts, b.Len())
	}
}

func TestBacklogNotDueIsSkipped(t *testing.T) {
	b := NewBacklog()
	b.Add(1, "t", "u") // NextRetry is ~30s from now

	s := &fakeSender{}
	b.Drain(context.Background(), s)

	if len(s.sent) != 0 {
		t.Fatalf("expected no send, got %v", s.sent)
	}
	if b.Len() != 1 {
		t.Fatalf("entry should still be queued, got Len=%d", b.Len())
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	if got := backoff(1); got != backlogBaseBackoff {
		t.Fatalf("backoff(1) = %v, want %v", got, backlogBaseBackoff)
	}
	if got := backoff(2); got != 2*backlogBaseBackoff {
		t.Fatalf("backoff(2) = %v, want %v", got, 2*backlogBaseBackoff)
	}
	if got := backoff(100); got != backlogMaxBackoff {
		t.Fatalf("backoff(100) = %v, want %v (cap)", got, backlogMaxBackoff)
	}
}
