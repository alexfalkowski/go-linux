//go:build linux

package test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Value returns a successful observation. Use it when the value is the behavior
// under test; keep failure and partial-result assertions explicit in scenarios.
func Value[T any](t *testing.T, operation func() (T, error)) T {
	t.Helper()
	value, err := operation()
	require.NoError(t, err)
	return value
}

// NoResource checks the shared acquisition-failure contract: the expected error
// and no resource requiring caller cleanup.
func NoResource(t *testing.T, resource any, err, expected error) {
	t.Helper()
	require.ErrorIs(t, err, expected)
	require.Nil(t, resource)
}

// PathError checks the standard operation/path context without prescribing the
// wrapped cause. Keep errno assertions beside the failing operation.
func PathError(t *testing.T, err error, operation, path string) {
	t.Helper()
	var actual *os.PathError
	require.ErrorAs(t, err, &actual)
	require.Equal(t, [2]string{operation, path}, [2]string{actual.Op, actual.Path})
}
