//go:build linux

package capability

import (
	"math/bits"
	"os"

	"golang.org/x/sys/unix"
)

// Bounding reports whether the calling thread's bounding set contains mask,
// using PR_CAPBSET_READ. mask must contain exactly one supported capability;
// zero, multiple, or unknown bits match os.ErrInvalid. False is returned on
// error. Lock the goroutine when combining this with another observation.
func Bounding(mask Set) (bool, error) {
	if !singleCapability(mask) {
		return false, os.NewSyscallError("prctl(PR_CAPBSET_READ)", os.ErrInvalid)
	}

	value, err := unix.PrctlRetInt(unix.PR_CAPBSET_READ, uintptr(bits.TrailingZeros64(uint64(mask))), 0, 0, 0)
	if err != nil {
		return false, os.NewSyscallError("prctl(PR_CAPBSET_READ)", err)
	}
	return value != 0, nil
}

// DropBounding irreversibly removes mask from the calling thread's bounding set
// using PR_CAPBSET_DROP. It takes exactly one supported capability, with the
// same validation as Bounding. Linux requires effective CAP_SETPCAP, even when
// the bit is already absent. Current permitted, effective, inheritable, and
// ambient capabilities are unchanged; this is not an immediate privilege drop.
// Use a dedicated locked goroutine that exits without unlocking. The bounding
// set cannot be raised and is inherited across process creation and exec.
func DropBounding(mask Set) error {
	if !singleCapability(mask) {
		return os.NewSyscallError("prctl(PR_CAPBSET_DROP)", os.ErrInvalid)
	}
	return os.NewSyscallError("prctl(PR_CAPBSET_DROP)", unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(bits.TrailingZeros64(uint64(mask))), 0, 0, 0))
}

// Ambient reports whether the calling thread's ambient set contains mask,
// using PR_CAP_AMBIENT_IS_SET. It takes exactly one supported capability, with
// the same validation as Bounding. False is returned on error. Independent
// membership queries do not form an atomic snapshot.
func Ambient(mask Set) (bool, error) {
	if !singleCapability(mask) {
		return false, os.NewSyscallError("prctl(PR_CAP_AMBIENT_IS_SET)", os.ErrInvalid)
	}

	value, err := unix.PrctlRetInt(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_IS_SET, uintptr(bits.TrailingZeros64(uint64(mask))), 0, 0)
	if err != nil {
		return false, os.NewSyscallError("prctl(PR_CAP_AMBIENT_IS_SET)", err)
	}
	return value != 0, nil
}

// RaiseAmbient adds mask to the calling thread's ambient set using
// PR_CAP_AMBIENT_RAISE. It takes exactly one supported capability, with the same
// validation as Bounding. Linux requires mask in both permitted and inheritable
// sets and refuses raising when SECBIT_NO_CAP_AMBIENT_RAISE is set. Ambient
// capabilities affect subsequent exec; raising does not change Effective now.
// Use a dedicated locked goroutine that exits without unlocking.
func RaiseAmbient(mask Set) error {
	return changeAmbient(mask, unix.PR_CAP_AMBIENT_RAISE, "prctl(PR_CAP_AMBIENT_RAISE)")
}

// LowerAmbient removes mask from the calling thread's ambient set using
// PR_CAP_AMBIENT_LOWER. An already absent bit succeeds. It takes exactly one
// supported capability, with the same validation as Bounding. Other capability
// sets are unchanged. Use a dedicated locked goroutine that exits without
// unlocking; this does not restrict other threads or change executable files.
func LowerAmbient(mask Set) error {
	return changeAmbient(mask, unix.PR_CAP_AMBIENT_LOWER, "prctl(PR_CAP_AMBIENT_LOWER)")
}

// ClearAmbient clears the calling thread's entire ambient set using
// PR_CAP_AMBIENT_CLEAR_ALL, including when already empty. Other capability sets
// are unchanged. Use a dedicated locked goroutine that exits without unlocking.
func ClearAmbient() error {
	return os.NewSyscallError("prctl(PR_CAP_AMBIENT_CLEAR_ALL)", unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0))
}

func singleCapability(mask Set) bool {
	supportedBits := mask&^supported == 0
	singleBit := mask != 0 && mask&(mask-1) == 0
	return supportedBits && singleBit
}

func changeAmbient(mask Set, operation uintptr, name string) error {
	if !singleCapability(mask) {
		return os.NewSyscallError(name, os.ErrInvalid)
	}
	return os.NewSyscallError(name, unix.Prctl(unix.PR_CAP_AMBIENT, operation, uintptr(bits.TrailingZeros64(uint64(mask))), 0, 0))
}
