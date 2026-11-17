//go:build linux

package landlock_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/alexfalkowski/go-linux/internal/test"
	"github.com/alexfalkowski/go-linux/landlock"
)

func ExampleNew() { //nolint:testableexamples // Executed by TestConstructionExample with explicit Landlock prerequisites.
	ruleset, err := landlock.New(landlock.Config{
		HandledFS:  landlock.ReadFile | landlock.ReadDir,
		HandledNet: landlock.ConnectTCP,
	})
	if err != nil {
		panic(err)
	}
	defer ruleset.Close()

	if err := ruleset.AddPath("/usr", landlock.ReadFile|landlock.ReadDir); err != nil {
		panic(err)
	}
	if err := ruleset.AddPort(443, landlock.ConnectTCP); err != nil {
		panic(err)
	}
	// Building a ruleset does not restrict this process. Restrict is explicit.
}

func ExampleRuleset_Restrict() { //nolint:testableexamples // Executed by TestRestrictionExample in a bounded subprocess.
	// Execute on a dedicated goroutine in a disposable child during development.
	runtime.LockOSThread() // Intentionally never unlocked, even on failure.
	directory, err := os.MkdirTemp("", "landlock-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "read-only")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		panic(err)
	}
	ruleset, err := landlock.New(landlock.Config{HandledFS: landlock.WriteFile})
	if err != nil {
		panic(err)
	}
	defer ruleset.Close()

	if err := ruleset.Restrict(landlock.Options{
		Synchronization: landlock.CallingThread,
		NoNewPrivileges: true,
	}); err != nil {
		panic(err)
	}

	// Opening for writing is denied on this thread. Reads stay unhandled.
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if !errors.Is(err, os.ErrPermission) {
			panic(err)
		}
		return
	}
	writer.Close()
	panic("expected Landlock write denial")
}

func TestConstructionExample(t *testing.T) {
	test.LandlockAvailable(t, 8)
	ExampleNew()
}

func TestRestrictionExample(t *testing.T) {
	test.LandlockAvailable(t, 8)
	if !test.LandlockProcess(t) {
		return
	}
	ExampleRuleset_Restrict()
}
