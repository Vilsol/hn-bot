package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const hnBase = "https://hacker-news.firebaseio.com/v0"

type HN struct {
	client *http.Client
}

func NewHN() *HN {
	return &HN{client: &http.Client{Timeout: 30 * time.Second}}
}

type Item struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	By    string `json:"by"`
	Title string `json:"title"`
	Score int    `json:"score"`
	Time  int64  `json:"time"`
	URL   string `json:"url"`
}

func (h *HN) TopStories(ctx context.Context) ([]int64, error) {
	var ids []int64
	if err := h.getJSON(ctx, hnBase+"/topstories.json", &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func (h *HN) Item(ctx context.Context, id int64) (*Item, error) {
	var it Item
	if err := h.getJSON(ctx, fmt.Sprintf("%s/item/%d.json", hnBase, id), &it); err != nil {
		return nil, err
	}
	return &it, nil
}

func (h *HN) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
