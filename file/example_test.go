//go:build linux

package file_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alexfalkowski/go-linux/file"
)

func ExampleOpen() {
	path, err := os.MkdirTemp("", "file-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	dir, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer dir.Close()

	if err := os.WriteFile(filepath.Join(path, "config"), []byte("hello"), 0o600); err != nil {
		panic(err)
	}

	opened, err := file.Open(dir, "config", file.OpenOptions{
		Resolve: file.Beneath | file.NoSymlinks | file.NoXDev,
	})
	if err != nil {
		panic(err)
	}
	defer opened.Close()

	contents, err := io.ReadAll(opened)
	if err != nil {
		panic(err)
	}

	metadata, err := file.Stat(opened, file.MaskSize|file.MaskBirthTime)
	if err != nil {
		panic(err)
	}

	if metadata.Mask&file.MaskSize == 0 {
		panic("size unavailable")
	}

	fmt.Println(string(contents), metadata.Size)

	// Output: hello 5
}

func ExampleStat() {
	status, err := os.Open("/proc/self/status")
	if err != nil {
		panic(err)
	}
	defer status.Close()

	mask := file.MaskDirectIOAlignment | file.MaskDirectIOReadAlignment
	metadata, err := file.Stat(status, mask)
	if err != nil {
		panic(err)
	}
	if metadata.Mask&file.MaskDirectIOAlignment == 0 {
		fmt.Println("Direct I/O alignment unavailable")
		return
	}
	if metadata.DirectIOMemoryAlignment == 0 {
		fmt.Println("Direct I/O unsupported")
		return
	}
	readAlignment := metadata.DirectIOOffsetAlignment
	if metadata.Mask&file.MaskDirectIOReadAlignment != 0 && metadata.DirectIOReadOffsetAlignment != 0 {
		readAlignment = metadata.DirectIOReadOffsetAlignment
	}
	fmt.Printf("Read offset alignment: %d bytes\n", readAlignment)

	// Output: Direct I/O alignment unavailable
}

func ExampleRename() {
	path, err := os.MkdirTemp("", "rename-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	dir, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer dir.Close()

	if err := os.WriteFile(filepath.Join(path, "ready"), []byte("new"), 0o600); err != nil {
		panic(err)
	}

	if err := os.WriteFile(filepath.Join(path, "active"), []byte("old"), 0o600); err != nil {
		panic(err)
	}

	if err := file.Rename(dir, "ready", dir, "active", file.Exchange); err != nil {
		panic(err)
	}

	active, err := os.ReadFile(filepath.Join(path, "active"))
	if err != nil {
		panic(err)
	}

	previous, err := os.ReadFile(filepath.Join(path, "ready"))
	if err != nil {
		panic(err)
	}

	fmt.Println(string(active), string(previous))

	// Output: new old
}

func ExampleFallocate() {
	path, err := os.MkdirTemp("", "allocation-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	segment, err := os.Create(filepath.Join(path, "segment"))
	if err != nil {
		panic(err)
	}
	defer segment.Close()

	// Reserve space for the next log segment before appending any records.
	if err := file.Fallocate(segment, file.Allocate|file.KeepSize, 0, 64*1024); err != nil {
		panic(err)
	}
	if _, err := segment.WriteString("record\n"); err != nil {
		panic(err)
	}

	info, err := segment.Stat()
	if err != nil {
		panic(err)
	}
	fmt.Println(info.Size())

	// Output: 7
}

func ExampleReadahead() {
	path, err := os.MkdirTemp("", "scan-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	name := filepath.Join(path, "segment")
	if err := os.WriteFile(name, []byte("record\n"), 0o600); err != nil {
		panic(err)
	}
	segment, err := os.Open(name)
	if err != nil {
		panic(err)
	}
	defer segment.Close()

	if err := file.Fadvise(segment, file.Sequential, 0, 0); err != nil {
		panic(err)
	}
	if err := file.Readahead(segment, 0, 64*1024); err != nil {
		panic(err)
	}
	record, err := io.ReadAll(segment)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(record))

	// Output: record
}

func ExampleFdatasync() {
	log, err := os.CreateTemp("", "wal-example-")
	if err != nil {
		panic(err)
	}
	defer os.Remove(log.Name())
	defer log.Close()

	if _, err := log.WriteString("record\n"); err != nil {
		panic(err)
	}
	if err := file.Fdatasync(log); err != nil {
		panic(err)
	}
	fmt.Println("log data synchronized")

	// Output: log data synchronized
}

func ExampleFlock() {
	path, err := os.MkdirTemp("", "locked-segment-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(path)

	name := filepath.Join(path, "segment")
	if err := os.WriteFile(name, []byte("first record\n"), 0o600); err != nil {
		panic(err)
	}

	// Each worker opens the same file in append mode before taking a lock.
	segment, err := os.OpenFile(name, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		panic(err)
	}
	defer segment.Close()

	if err := file.Flock(segment, file.LockExclusive|file.LockNonblock); err != nil {
		panic(err)
	}
	if _, err := segment.WriteString("second record\n"); err != nil {
		panic(err)
	}
	if err := file.Flock(segment, file.Unlock); err != nil {
		panic(err)
	}

	contents, err := os.ReadFile(name)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(contents))

	// Output:
	// first record
	// second record
}

func ExampleSetRecordLock() {
	store, err := os.CreateTemp("", "record-store-*")
	if err != nil {
		panic(err)
	}
	defer os.Remove(store.Name())
	defer store.Close()
	if _, err := store.WriteString("one\ntwo\n"); err != nil {
		panic(err)
	}

	// Each worker opens the store independently and locks the record it updates.
	lock := file.RecordLock{Type: file.LockWrite, Range: file.LockRange{Start: 4, Length: 4}}
	if err := file.SetRecordLock(store, file.OFDLocks, lock); err != nil {
		panic(err)
	}
	if _, err := store.WriteAt([]byte("new\n"), lock.Range.Start); err != nil {
		panic(err)
	}
	lock.Type = file.LockRelease
	if err := file.SetRecordLock(store, file.OFDLocks, lock); err != nil {
		panic(err)
	}

	contents, err := os.ReadFile(store.Name())
	if err != nil {
		panic(err)
	}
	fmt.Print(string(contents))

	// Output:
	// one
	// new
}

func ExampleFallocate_unshareRange() {
	segment, err := os.CreateTemp("", "working-copy-*.db")
	if err != nil {
		panic(err)
	}
	defer os.Remove(segment.Name())
	defer segment.Close()
	if _, err := segment.WriteString("record\n"); err != nil {
		panic(err)
	}

	// For a reflinked working copy, make this segment private before writing.
	// This caller treats the reservation as optional on unsupported filesystems.
	const segmentSize = 64 * 1024
	if err := file.Fallocate(segment, file.UnshareRange|file.KeepSize, 0, segmentSize); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		panic(err)
	}

	contents, err := os.ReadFile(segment.Name())
	if err != nil {
		panic(err)
	}
	fmt.Print(string(contents))

	// Output: record
}

func ExampleFallocate_writeZeroes() {
	segment, err := os.CreateTemp("", "database-segment-*")
	if err != nil {
		panic(err)
	}
	defer os.Remove(segment.Name())
	defer segment.Close()

	// Prepare a new segment for overwrites when the storage supports it.
	const segmentSize = 64 * 1024
	if err := file.Fallocate(segment, file.WriteZeroes, 0, segmentSize); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		panic(err)
	}
	record := []byte("record")
	recordOffset := int64(segmentSize - len(record))
	if _, err := segment.WriteAt(record, recordOffset); err != nil {
		panic(err)
	}

	info, err := segment.Stat()
	if err != nil {
		panic(err)
	}
	fmt.Println(info.Size())

	// Output: 65536
}
