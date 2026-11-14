//go:build linux

package test

import (
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/alexfalkowski/go-linux/memfd"
	"github.com/alexfalkowski/go-linux/mmap"
	"github.com/stretchr/testify/require"
)

// MappingPermissions finds the live mapping containing view without reading its
// contents. The caller must keep the view mapped throughout the inspection.
func MappingPermissions(t *testing.T, view []byte) string {
	t.Helper()

	mappings, err := os.ReadFile("/proc/self/maps")
	require.NoError(t, err)

	address := uint64(reflect.ValueOf(view).Pointer())
	for line := range strings.SplitSeq(string(mappings), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		start, end, found := strings.Cut(fields[0], "-")
		require.True(t, found, "procfs mapping must supply an address range")

		low, err := strconv.ParseUint(start, 16, 64)
		require.NoError(t, err)

		high, err := strconv.ParseUint(end, 16, 64)
		require.NoError(t, err)

		if address >= low && address < high {
			return fields[1]
		}
	}

	require.FailNow(t, "live region absent from procfs mappings")
	return ""
}

// WritableMapping creates a shared writable page with independent cleanup.
// Kernel errors are returned for behavior assertions at the call site.
func WritableMapping(t *testing.T, file *memfd.File) (*mmap.Region, error) {
	t.Helper()

	region, err := file.Map(0, os.Getpagesize(), mmap.Options{Mode: mmap.Shared, Protection: mmap.Read | mmap.Write})
	if err == nil {
		Cleanup(t, region)
	}
	return region, err
}
