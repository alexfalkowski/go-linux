//go:build linux

package store

import (
	"bytes"
	"errors"
	"sync"
)

// MaxTTLSeconds is the maximum positive expiry accepted by the example.
const MaxTTLSeconds = 365 * 24 * 60 * 60

const (
	maxKeys  = 1024
	maxKey   = 1024
	maxBytes = 8 << 20
)

// ErrSnapshotsDisabled reports that no snapshot path was configured.
var ErrSnapshotsDisabled = errors.New("start with -snapshot to enable SAVE")

// Open creates an in-memory cache and restores an optional trusted snapshot path.
func Open(path string) (*Cache, error) {
	snapshots := snapshotFile{path: path}
	cache := &Cache{entries: make(map[string]Entry), snapshots: snapshots}
	if err := snapshots.load(cache); err != nil {
		return nil, err
	}
	return cache, nil
}

// Cache owns value lifetimes, expiry, capacity, and synchronization. Values are
// immutable after insertion, so a completed lookup can safely outlive its lock.
type Cache struct {
	entries   map[string]Entry
	snapshots snapshotFile
	bytes     int
	mu        sync.Mutex
}

// Entry describes one immutable stored value.
type Entry struct {
	// Value is borrowed immutable data and remains valid after later cache changes.
	Value []byte
	// Expires is an absolute Unix-millisecond deadline; zero means no expiry.
	Expires int64
}

// Set copies a value and atomically applies capacity limits; timestamps are Unix milliseconds.
func (cache *Cache) Set(key string, value []byte, expires, now int64) error {
	if len(key) > maxKey {
		return errors.New("key exceeds 1024 bytes")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.expireLocked(now)

	previous, exists := cache.entries[key]
	if !exists && len(cache.entries) == maxKeys {
		return errors.New("1024-key capacity reached")
	}
	var total int
	if exists {
		total = cache.bytes - len(previous.Value) + len(value)
	} else {
		total = cache.bytes + len(key) + len(value)
	}
	if total > maxBytes {
		return errors.New("8 MiB key/value capacity reached")
	}
	cache.entries[key] = Entry{Value: bytes.Clone(value), Expires: expires}
	cache.bytes = total
	return nil
}

// Get returns a live entry at now, in Unix milliseconds; returned bytes must not be mutated.
func (cache *Cache) Get(key string, now int64) (Entry, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.lookupLocked(key, now)
}

// Delete removes live keys at now, in Unix milliseconds, counting each key once.
func (cache *Cache) Delete(keys []string, now int64) int {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	var deleted int
	for _, key := range keys {
		if _, exists := cache.lookupLocked(key, now); exists {
			cache.removeLocked(key)
			deleted++
		}
	}
	return deleted
}

// ExpireAt sets a Unix-millisecond deadline, deleting at once if the deadline has passed.
func (cache *Cache) ExpireAt(key string, when, now int64) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	value, exists := cache.lookupLocked(key, now)
	if !exists {
		return false
	}
	if when <= now {
		cache.removeLocked(key)
	} else {
		value.Expires = when
		cache.entries[key] = value
	}
	return true
}

// Sweep removes entries expired at the supplied Unix-millisecond timestamp.
func (cache *Cache) Sweep(now int64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.expireLocked(now)
}

// Save serializes a complete snapshot while holding mutations until persistence returns.
func (cache *Cache) Save() error {
	if cache.snapshots.path == "" {
		return ErrSnapshotsDisabled
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.snapshots.save(cache)
}

// Count reports retained entries; Sweep removes expired entries before counting when needed.
func (cache *Cache) Count() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.entries)
}

func (cache *Cache) lookupLocked(key string, now int64) (Entry, bool) {
	value, exists := cache.entries[key]
	if exists && value.Expires != 0 && value.Expires <= now {
		cache.removeLocked(key)
		return Entry{}, false
	}
	return value, exists
}

func (cache *Cache) removeLocked(key string) {
	value := cache.entries[key]
	cache.bytes -= len(key) + len(value.Value)
	delete(cache.entries, key)
}

func (cache *Cache) expireLocked(now int64) {
	for key, value := range cache.entries {
		if value.Expires != 0 && value.Expires <= now {
			cache.removeLocked(key)
		}
	}
}
