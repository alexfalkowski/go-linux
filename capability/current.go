//go:build linux

package capability

import (
	"os"

	"golang.org/x/sys/unix"
)

// Current reads the calling thread's three capability sets in one capget
// operation using the version-3, 64-bit ABI. It returns zero sets on error.
// Lock the goroutine to its OS thread when subsequent operations depend on
// this observation. Bounding and ambient membership have separate queries.
func Current() (Sets, error) {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&header, &data[0]); err != nil {
		return Sets{}, os.NewSyscallError("capget", err)
	}
	return Sets{
		Permitted:   Set(data[0].Permitted) | Set(data[1].Permitted)<<32,
		Effective:   Set(data[0].Effective) | Set(data[1].Effective)<<32,
		Inheritable: Set(data[0].Inheritable) | Set(data[1].Inheritable)<<32,
	}, nil
}

// SetCurrent replaces the calling thread's permitted, effective, and inheritable
// sets in one capset operation. Zero clears all three. Unknown bits match
// os.ErrInvalid; Linux validates transitions and preserves errors such as EPERM
// for an effective set outside the proposed permitted set, attempts to regain
// dropped permitted capabilities, or forbidden inheritable additions.
//
// Use only on a dedicated locked goroutine that exits without unlocking, even
// after failure. Other threads are unchanged. Dropping permitted capabilities
// cannot be undone with capset; lowering permitted or inheritable capabilities
// also lowers corresponding ambient capabilities.
// Bounding membership is unchanged. No multi-operation rollback is attempted.
func SetCurrent(sets Sets) error {
	unknown := (sets.Permitted | sets.Effective | sets.Inheritable) &^ supported
	if unknown != 0 {
		return os.NewSyscallError("capset", os.ErrInvalid)
	}

	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{
		{Permitted: uint32(sets.Permitted & 0xffffffff), Effective: uint32(sets.Effective & 0xffffffff), Inheritable: uint32(sets.Inheritable & 0xffffffff)},
		{Permitted: uint32(sets.Permitted >> 32), Effective: uint32(sets.Effective >> 32), Inheritable: uint32(sets.Inheritable >> 32)},
	}
	return os.NewSyscallError("capset", unix.Capset(&header, &data[0]))
}

// Sets is a value snapshot of the three capget/capset capability masks. It owns
// no resources. Its zero value requests clearing all three with SetCurrent.
// Reading, modifying, then applying a snapshot requires one locked OS thread;
// the package does not synchronize credentials across threads.
type Sets struct {
	// Permitted is the ceiling for this thread's effective capabilities.
	Permitted Set
	// Effective contains the capabilities used for kernel permission checks.
	Effective Set
	// Inheritable contributes to capabilities obtained across exec according
	// to file and ambient capability rules; it does not itself grant authority.
	Inheritable Set
}
