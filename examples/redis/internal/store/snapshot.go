//go:build linux

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/alexfalkowski/go-linux/file"
	"github.com/alexfalkowski/go-linux/mmap"
)

const maxValueBytes = 64 << 10

// snapshotFile reads mapped snapshots and replaces complete snapshots atomically.
// The caller serializes saving with cache mutations.
type snapshotFile struct {
	path string
}

func (storage snapshotFile) load(cache *Cache) error {
	if storage.path == "" {
		return nil
	}
	source, err := os.Open(storage.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer source.Close()
	saved, err := decodeSnapshot(source)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, record := range saved.Entries {
		if err := cache.restoreEntry(record, now); err != nil {
			return err
		}
	}
	cache.expireLocked(now)
	return nil
}

func (storage snapshotFile) save(cache *Cache) error {
	saved := snapshot{Version: 1, Entries: make([]snapshotEntry, 0, len(cache.entries))}
	for key, value := range cache.entries {
		saved.Entries = append(saved.Entries, snapshotEntry{Key: []byte(key), Value: value.Value, Expires: value.Expires})
	}
	slices.SortFunc(saved.Entries, func(a, b snapshotEntry) int {
		return slices.Compare(a.Key, b.Key)
	})
	directory, err := os.Open(filepath.Dir(storage.path))
	if err != nil {
		return err
	}
	defer directory.Close()
	temporary, err := os.CreateTemp(filepath.Dir(storage.path), ".go-linux-cache-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := json.NewEncoder(temporary).Encode(saved); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := file.Rename(directory, filepath.Base(temporary.Name()), directory, filepath.Base(storage.path), file.Replace); err != nil {
		return err
	}
	return directory.Sync()
}

type snapshot struct {
	// Entries contains binary-safe values and optional absolute expiry times.
	Entries []snapshotEntry `json:"entries"`
	// Version identifies this example's JSON format, independently of Redis RDB.
	Version int `json:"version"`
}

type snapshotEntry struct {
	// Key contains the raw key bytes, encoded as base64 by encoding/json.
	Key []byte `json:"key"`
	// Value contains the raw value bytes, encoded as base64 by encoding/json.
	Value []byte `json:"value"`
	// Expires is Unix milliseconds; zero means no expiry.
	Expires int64 `json:"expires"`
}

func (cache *Cache) restoreEntry(record snapshotEntry, now int64) error {
	validSizes := len(record.Key) <= maxKey && len(record.Value) <= maxValueBytes
	validExpiry := record.Expires >= 0 && record.Expires <= now+MaxTTLSeconds*1000
	key := string(record.Key)
	_, duplicate := cache.entries[key]
	if !validSizes || !validExpiry || duplicate {
		return errors.New("invalid snapshot entry")
	}
	cache.bytes += len(key) + len(record.Value)
	if cache.bytes > maxBytes {
		return errors.New("snapshot exceeds 8 MiB key/value capacity")
	}
	cache.entries[key] = Entry{Value: record.Value, Expires: record.Expires}
	return nil
}

func decodeSnapshot(source *os.File) (snapshot, error) {
	stat, err := source.Stat()
	if err != nil {
		return snapshot{}, err
	}
	validFile := stat.Mode().IsRegular() && stat.Size() > 0 && stat.Size() <= 16<<20
	if !validFile {
		return snapshot{}, errors.New("snapshot must be a nonempty regular file <= 16 MiB")
	}
	region, err := mmap.Map(source, 0, int(stat.Size()), mmap.Options{Protection: mmap.Read})
	if err != nil {
		return snapshot{}, err
	}
	defer region.Close()
	var saved snapshot
	if err := json.Unmarshal(region.Bytes(), &saved); err != nil {
		return snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	if saved.Version != 1 || len(saved.Entries) > maxKeys {
		return snapshot{}, errors.New("unsupported snapshot version or key count")
	}
	return saved, nil
}
