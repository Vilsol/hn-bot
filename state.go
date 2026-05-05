package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"sync"
)

type PostData struct {
	ID   int64
	Up   map[int64]struct{}
	Down map[int64]struct{}
}

type State struct {
	mu    sync.Mutex
	path  string
	Posts map[int64]*PostData
}

type postJSON struct {
	Up   []int64 `json:"up"`
	Down []int64 `json:"down"`
}

func LoadState(path string) (*State, error) {
	s := &State{path: path, Posts: map[int64]*PostData{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}

	// Legacy format: top-level array of IDs.
	if data[0] == '[' {
		var ids []int64
		if err := json.Unmarshal(data, &ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			s.Posts[id] = newPost(id)
		}
		return s, nil
	}

	var raw map[string]postJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	for k, v := range raw {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			return nil, err
		}
		p := newPost(id)
		for _, u := range v.Up {
			p.Up[u] = struct{}{}
		}
		for _, d := range v.Down {
			p.Down[d] = struct{}{}
		}
		s.Posts[id] = p
	}
	return s, nil
}

func newPost(id int64) *PostData {
	return &PostData{ID: id, Up: map[int64]struct{}{}, Down: map[int64]struct{}{}}
}

// AddIfMissing returns true if the post was newly inserted. Used for atomic
// claim-and-send when concurrent scans race on the same id.
func (s *State) AddIfMissing(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Posts[id]; ok {
		return false
	}
	s.Posts[id] = newPost(id)
	return true
}

// Remove releases a claim made via AddIfMissing. Used when the side effect
// that the claim was guarding (e.g. a Telegram send) failed and we want the
// next scan to retry the post.
func (s *State) Remove(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Posts, id)
}

func (s *State) Has(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.Posts[id]
	return ok
}

func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]postJSON, len(s.Posts))
	for id, p := range s.Posts {
		up := setToSortedSlice(p.Up)
		down := setToSortedSlice(p.Down)
		out[strconv.FormatInt(id, 10)] = postJSON{Up: up, Down: down}
	}

	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func setToSortedSlice(m map[int64]struct{}) []int64 {
	s := make([]int64, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s
}
