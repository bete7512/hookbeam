// Package cache provides shared in-process cached values and bounded, non-evicting counters.
package cache

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

// Config sizes the cache.
type Config struct {
	MaxItems    int64 // entries kept before the least valuable are evicted
	MaxCounters int64 // live counter capacity; zero uses MaxItems
}

// Cache holds values of any type under string keys. It is safe for concurrent use.
type Cache struct {
	c           *ristretto.Cache[string, any]
	mu          sync.Mutex
	counters    map[windowKey]*windowCounter
	expires     counterHeap
	maxCounters int64
	closed      bool
}

// New returns an empty Cache; Close releases its goroutines.
func New(cfg Config) (*Cache, error) {
	if cfg.MaxItems <= 0 {
		return nil, errors.New("cache: MaxItems must be positive")
	}
	if cfg.MaxCounters < 0 {
		return nil, errors.New("cache: MaxCounters must not be negative")
	}
	if cfg.MaxCounters == 0 {
		cfg.MaxCounters = cfg.MaxItems
	}
	c, err := ristretto.NewCache(&ristretto.Config[string, any]{
		NumCounters:        cfg.MaxItems * 10,
		MaxCost:            cfg.MaxItems,
		BufferItems:        64,
		IgnoreInternalCost: true,
	})
	if err != nil {
		return nil, fmt.Errorf("cache: %w", err)
	}
	return &Cache{c: c, counters: make(map[windowKey]*windowCounter), maxCounters: cfg.MaxCounters}, nil
}

// Get returns the value under key, if present and not expired.
func (c *Cache) Get(key string) (any, bool) {
	return c.c.Get(key)
}

// Set stores value under key for ttl. Writes are applied asynchronously, so a Get may miss them until Wait returns;
// false means the write was dropped under contention or refused by the eviction policy.
func (c *Cache) Set(key string, value any, ttl time.Duration) bool {
	return c.c.SetWithTTL(key, value, 1, ttl)
}

// Wait waits for pending sets; admission and eviction can still make values absent.
func (c *Cache) Wait() {
	c.c.Wait()
}

// Close stops the cache; later reads miss and writes are ignored.
func (c *Cache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.counters = nil
	c.expires = nil
	c.c.Close()
}
