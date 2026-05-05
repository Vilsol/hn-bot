package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

const (
	pollInterval = time.Minute
	topN         = 10
	minScore     = 50
	maxAge       = 12 * time.Hour
	fetchWorkers = 8
)

// hotness implements HN's own ranking formula: (score - 1) / (age_hours + 2)^1.8.
// Logged for now; not (yet) used as a filter.
func hotness(score int, age time.Duration) float64 {
	return (float64(score) - 1) / math.Pow(age.Hours()+2, 1.8)
}

type Sender interface {
	Send(ctx context.Context, id int64, title, url string) error
}

type HNClient interface {
	TopStories(ctx context.Context) ([]int64, error)
	Item(ctx context.Context, id int64) (*Item, error)
}

type App struct {
	State   *State
	HN      HNClient
	TG      Sender
	Backlog *Backlog
	DryRun  bool
	Now     func() time.Time // overridable for tests
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *App) Run(ctx context.Context) {
	if a.DryRun {
		a.scan(ctx)
		return
	}

	a.scan(ctx)

	pollT := time.NewTicker(pollInterval)
	defer pollT.Stop()
	drainT := time.NewTicker(backlogDrainTick)
	defer drainT.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollT.C:
			a.scan(ctx)
		case <-drainT.C:
			a.Backlog.Drain(ctx, a.TG)
		}
	}
}

func (a *App) scan(ctx context.Context) {
	ids, err := a.HN.TopStories(ctx)
	if err != nil {
		log.Printf("topstories: %v", err)
		return
	}
	// No truncation: a story is eligible if it's in the top N OR has score
	// above the floor (see checkItem). Both branches need item data, so we
	// can't filter by position alone.

	sem := make(chan struct{}, fetchWorkers)
	var wg sync.WaitGroup
	for pos, id := range ids {
		if a.State.Has(id) {
			continue
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(id int64, pos int) {
			defer wg.Done()
			defer func() { <-sem }()
			a.checkItem(ctx, id, pos)
		}(id, pos)
	}
	wg.Wait()
}

func (a *App) checkItem(ctx context.Context, id int64, pos int) {
	if a.State.Has(id) {
		return
	}
	item, err := a.HN.Item(ctx, id)
	if err != nil {
		log.Printf("item %d: %v", id, err)
		return
	}
	if item.Type != "story" {
		return
	}
	// Eligible if in the top N or above the score floor. Stories failing both
	// are not claimed — they may rise in score or rank on a later scan.
	if pos >= topN && item.Score <= minScore {
		return
	}

	age := a.now().Sub(time.Unix(item.Time, 0))
	stale := age > maxAge
	hot := hotness(item.Score, age)
	articleURL := item.URL
	if articleURL == "" {
		articleURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", id)
	}

	tag := fmt.Sprintf("[%d] pos=%d score=%d hotness=%.2f age=%v", id, pos+1, item.Score, hot, age.Round(time.Minute))

	if a.DryRun {
		if stale {
			log.Printf("[DRY] would skip stale %s %s", tag, item.Title)
		} else {
			log.Printf("[DRY] would send %s %s", tag, item.Title)
		}
		return
	}

	if !a.State.AddIfMissing(id) {
		return
	}

	if stale {
		log.Printf("Skipping stale %s %s", tag, item.Title)
		if err := a.State.Save(); err != nil {
			log.Printf("save: %v", err)
		}
		return
	}

	log.Printf("Sending %s %s", tag, item.Title)
	if err := a.TG.Send(ctx, id, item.Title, articleURL); err != nil {
		log.Printf("send %d failed, queueing: %v", id, err)
		a.Backlog.Add(id, item.Title, articleURL)
	}
	if err := a.State.Save(); err != nil {
		log.Printf("save: %v", err)
	}
}
