package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStateExisting(t *testing.T) {
	// Smoke test against the live parsed.json — schema only, no specific ids
	// (production rewrites this file).
	s, err := LoadState("parsed.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Posts) == 0 {
		t.Fatal("parsed.json should have at least one entry")
	}
	for id, p := range s.Posts {
		if p.ID != id {
			t.Fatalf("inconsistent id: key=%d post.ID=%d", id, p.ID)
		}
		if p.Up == nil || p.Down == nil {
			t.Fatalf("nil up/down for %d", id)
		}
	}
}

func TestLoadStateLegacyArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")
	if err := os.WriteFile(path, []byte(`[1,2,3]`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Posts) != 3 {
		t.Fatalf("got %d", len(s.Posts))
	}
}

func TestSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")

	s := &State{path: path, Posts: map[int64]*PostData{}}
	s.Posts[42] = newPost(42)
	s.Posts[42].Up[100] = struct{}{}
	s.Posts[42].Up[101] = struct{}{}
	s.Posts[42].Down[200] = struct{}{}

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]postJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got["42"].Up) != 2 || got["42"].Up[0] != 100 || got["42"].Up[1] != 101 {
		t.Fatalf("up: %+v", got["42"].Up)
	}
	if len(got["42"].Down) != 1 || got["42"].Down[0] != 200 {
		t.Fatalf("down: %+v", got["42"].Down)
	}

	s2, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Posts[42].Up[100]; !ok {
		t.Fatalf("round-trip missing 100")
	}
}

func TestAddIfMissing(t *testing.T) {
	s := &State{path: "/dev/null", Posts: map[int64]*PostData{}}
	if !s.AddIfMissing(1) {
		t.Fatal("first add should succeed")
	}
	if s.AddIfMissing(1) {
		t.Fatal("second add should fail")
	}
}

func TestRemoveReleasesClaim(t *testing.T) {
	s := &State{path: "/dev/null", Posts: map[int64]*PostData{}}
	if !s.AddIfMissing(1) {
		t.Fatal("first add should succeed")
	}
	s.Remove(1)
	if !s.AddIfMissing(1) {
		t.Fatal("re-add after Remove should succeed")
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.json")

	s := &State{path: path, Posts: map[int64]*PostData{}}
	s.Posts[1] = newPost(1)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should not remain after Save: %v", err)
	}
}
