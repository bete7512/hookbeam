package cache

import (
	"container/heap"
	"context"
	"errors"
	"math"
	"time"
)

// Counter storage errors require callers to fail closed rather than reset a live counter.
var (
	ErrCounterCapacity = errors.New("cache: live counter capacity exhausted")
	ErrClosed          = errors.New("cache: closed")
)

type windowKey struct {
	key    string
	window time.Duration
}

type windowCounter struct {
	key   windowKey
	end   time.Time
	count int
}

type counterHeap []*windowCounter

func (h counterHeap) Len() int           { return len(h) }
func (h counterHeap) Less(i, j int) bool { return h[i].end.Before(h[j].end) }
func (h counterHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *counterHeap) Push(v any)        { *h = append(*h, v.(*windowCounter)) }
func (h *counterHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	return last
}

// IncrementWindow atomically samples time and increments a fixed-window counter.
// Live counters never evict; new keys fail at capacity until expired counters can be removed.
// now must return promptly and must not call back into the cache; nil uses time.Now.
func (c *Cache) IncrementWindow(ctx context.Context, key string, window time.Duration, now func() time.Time) (int, time.Duration, error) {
	if window <= 0 || len(key) > 1024 {
		return 0, 0, errors.New("cache: positive window and key of at most 1024 bytes required")
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if c.closed {
		return 0, 0, ErrClosed
	}
	if now == nil {
		now = time.Now
	}
	current := now().UTC()
	for len(c.expires) > 0 && !c.expires[0].end.After(current) {
		expired := heap.Pop(&c.expires).(*windowCounter)
		delete(c.counters, expired.key)
	}
	k := windowKey{key: key, window: window}
	entry, ok := c.counters[k]
	if !ok {
		if int64(len(c.counters)) >= c.maxCounters {
			return 0, 0, ErrCounterCapacity
		}
		entry = &windowCounter{key: k, end: current.Truncate(window).Add(window)}
		c.counters[k] = entry
		heap.Push(&c.expires, entry)
	}
	if entry.count < math.MaxInt {
		entry.count++
	}
	return entry.count, entry.end.Sub(current), nil
}
