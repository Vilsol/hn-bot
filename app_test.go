package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"
)

type fakeHN struct {
	ids   []int64
	items map[int64]*Item
}

func (f *fakeHN) TopStories(_ context.Context) ([]int64, error) { return f.ids, nil }

func (f *fakeHN) Item(_ context.Context, id int64) (*Item, error) {
	it, ok := f.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return it, nil
}

func newAppFixture(t *testing.T, dryRun bool) (*App, *fakeHN, *fakeSender, *State) {
	t.Helper()
	state := &State{path: filepath.Join(t.TempDir(), "p.json"), Posts: map[int64]*PostData{}}
	hn := &fakeHN{items: map[int64]*Item{}}
	tg := &fakeSender{}
	a := &App{
		State:   state,
		HN:      hn,
		TG:      tg,
		Backlog: NewBacklog(),
		DryRun:  dryRun,
		Now:     func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
	return a, hn, tg, state
}

func TestCheckItemSendsFreshStory(t *testing.T) {
	a, hn, tg, state := newAppFixture(t, false)
	hn.items[1] = &Item{ID: 1, Type: "story", Score: 100, Title: "fresh", URL: "https://example.com", Time: a.now().Add(-1 * time.Hour).Unix()}

	a.checkItem(context.Background(), 1, 0)

	if len(tg.sent) != 1 || tg.sent[0] != 1 {
		t.Fatalf("expected send of 1, got %v", tg.sent)
	}
	if !state.Has(1) {
		t.Fatal("post should be claimed")
	}
}

func TestCheckItemSkipsStaleStory(t *testing.T) {
	a, hn, tg, state := newAppFixture(t, false)
	hn.items[2] = &Item{ID: 2, Type: "story", Score: 100, Title: "stale", URL: "https://example.com", Time: a.now().Add(-24 * time.Hour).Unix()}

	a.checkItem(context.Background(), 2, 0)

	if len(tg.sent) != 0 {
		t.Fatalf("stale story should not be sent, got %v", tg.sent)
	}
	if !state.Has(2) {
		t.Fatal("stale post should still be claimed (so we don't re-fetch it)")
	}
}

func TestCheckItemEligibilityOR(t *testing.T) {
	cases := []struct {
		name     string
		pos      int
		score    int
		wantSend bool
	}{
		{"top10+highScore", 0, 100, true},
		{"top10+lowScore", 5, 20, true},
		{"beyondTop10+highScore", 50, 87, true},
		{"beyondTop10+exactlyFloor", 50, 50, false},
		{"beyondTop10+belowFloor", 50, 30, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, hn, tg, state := newAppFixture(t, false)
			hn.items[1] = &Item{ID: 1, Type: "story", Score: tc.score, Title: tc.name, URL: "https://example.com", Time: a.now().Unix()}

			a.checkItem(context.Background(), 1, tc.pos)

			sent := len(tg.sent) == 1
			if sent != tc.wantSend {
				t.Fatalf("send=%v, want %v (pos=%d score=%d)", sent, tc.wantSend, tc.pos, tc.score)
			}
			if !tc.wantSend && state.Has(1) {
				t.Fatal("ineligible story must not be claimed (score/rank may rise later)")
			}
		})
	}
}

func TestCheckItemSkipsNonStoryTypes(t *testing.T) {
	a, hn, tg, state := newAppFixture(t, false)
	hn.items[3] = &Item{ID: 3, Type: "job", Score: 100, Title: "hiring", URL: "https://example.com", Time: a.now().Unix()}

	a.checkItem(context.Background(), 3, 0)

	if len(tg.sent) != 0 {
		t.Fatalf("non-story should not send, got %v", tg.sent)
	}
	if state.Has(3) {
		t.Fatal("non-story should not be claimed")
	}
}

func TestCheckItemSendFailureGoesToBacklog(t *testing.T) {
	a, hn, _, state := newAppFixture(t, false)
	a.TG = &fakeSender{failures: 1}
	hn.items[4] = &Item{ID: 4, Type: "story", Score: 100, Title: "boom", URL: "", Time: a.now().Unix()}

	a.checkItem(context.Background(), 4, 0)

	if a.Backlog.Len() != 1 {
		t.Fatalf("expected backlog entry, got Len=%d", a.Backlog.Len())
	}
	if !state.Has(4) {
		t.Fatal("post should remain claimed after queueing")
	}
	got := a.Backlog.Snapshot()[0]
	want := "https://news.ycombinator.com/item?id=4"
	if got.URL != want {
		t.Fatalf("self-post URL fallback: got %q, want %q", got.URL, want)
	}
}

func TestCheckItemDryRunMutatesNothing(t *testing.T) {
	a, hn, tg, state := newAppFixture(t, true)
	hn.items[5] = &Item{ID: 5, Type: "story", Score: 100, Title: "fresh", URL: "https://example.com", Time: a.now().Unix()}
	hn.items[6] = &Item{ID: 6, Type: "story", Score: 100, Title: "stale", URL: "https://example.com", Time: a.now().Add(-48 * time.Hour).Unix()}

	a.checkItem(context.Background(), 5, 0)
	a.checkItem(context.Background(), 6, 1)

	if len(tg.sent) != 0 {
		t.Fatalf("dry-run must not send, got %v", tg.sent)
	}
	if state.Has(5) || state.Has(6) {
		t.Fatal("dry-run must not claim posts")
	}
	if a.Backlog.Len() != 0 {
		t.Fatal("dry-run must not enqueue")
	}
}

func TestScanORSemantics(t *testing.T) {
	a, hn, tg, _ := newAppFixture(t, false)
	// Three buckets:
	//   ids 1..topN        — top N, score below floor (eligible by rank)
	//   ids topN+1..topN+5 — beyond top N, high score (eligible by score)
	//   ids topN+6..topN+10 — beyond top N, low score (NOT eligible)
	for i := int64(1); i <= int64(topN); i++ {
		hn.ids = append(hn.ids, i)
		hn.items[i] = &Item{ID: i, Type: "story", Score: 20, Title: fmt.Sprintf("rank-%d", i), URL: "https://example.com", Time: a.now().Unix()}
	}
	for i := int64(topN + 1); i <= int64(topN+5); i++ {
		hn.ids = append(hn.ids, i)
		hn.items[i] = &Item{ID: i, Type: "story", Score: 100, Title: fmt.Sprintf("score-%d", i), URL: "https://example.com", Time: a.now().Unix()}
	}
	for i := int64(topN + 6); i <= int64(topN+10); i++ {
		hn.ids = append(hn.ids, i)
		hn.items[i] = &Item{ID: i, Type: "story", Score: 20, Title: fmt.Sprintf("ineligible-%d", i), URL: "https://example.com", Time: a.now().Unix()}
	}

	a.scan(context.Background())

	if len(tg.sent) != topN+5 {
		t.Fatalf("expected %d sends (rank+score), got %d: %v", topN+5, len(tg.sent), tg.sent)
	}
	for _, id := range tg.sent {
		if id > int64(topN+5) {
			t.Fatalf("sent ineligible id %d", id)
		}
	}
}

func TestHotnessFormula(t *testing.T) {
	// (100 - 1) / (1 + 2)^1.8 = 99 / 7.224... ≈ 13.7
	got := hotness(100, time.Hour)
	if math.Abs(got-13.7) > 0.1 {
		t.Fatalf("hotness(100, 1h) = %f, want ~13.7", got)
	}
	// older story, same score, should be lower
	older := hotness(100, 6*time.Hour)
	if older >= got {
		t.Fatalf("expected older to have lower hotness: 1h=%f vs 6h=%f", got, older)
	}
}
