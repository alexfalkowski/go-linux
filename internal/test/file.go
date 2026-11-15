//go:build linux

package test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// FileStatx independently observes Linux metadata for focused ABI comparisons.
// The file remains open and no offset or flags are changed.
func FileStatx(t *testing.T, file *os.File, mask uint32) unix.Statx_t {
	t.Helper()
	var stat unix.Statx_t
	var err error
	FileControl(t, file, func(fd uintptr) {
		err = unix.Statx(int(fd), "", unix.AT_EMPTY_PATH, int(mask), &stat)
	})
	require.NoError(t, err)
	return stat
}
