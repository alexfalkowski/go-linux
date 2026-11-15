//go:build linux

package file

import (
	"os"

	"golang.org/x/sys/unix"
)

// OpenFlags selects the supported openat2 access and creation flags.
// Combine flags with |, using exactly one access mode (ReadOnly is zero).
type OpenFlags uint64

// ReadOnly opens for reading (O_RDONLY); this is the default access mode.
const ReadOnly OpenFlags = unix.O_RDONLY

// WriteOnly opens for writing (O_WRONLY).
const WriteOnly OpenFlags = unix.O_WRONLY

// ReadWrite opens for reading and writing (O_RDWR).
const ReadWrite OpenFlags = unix.O_RDWR

// Append makes each write append at end of file (O_APPEND).
const Append OpenFlags = unix.O_APPEND

// Create creates the file if absent (O_CREAT); Perm controls its initial mode.
const Create OpenFlags = unix.O_CREAT

// Exclusive with Create requires a new file (O_EXCL).
const Exclusive OpenFlags = unix.O_EXCL

// Truncate truncates an existing writable file (O_TRUNC).
const Truncate OpenFlags = unix.O_TRUNC

// Directory requires the opened object to be a directory (O_DIRECTORY).
const Directory OpenFlags = unix.O_DIRECTORY

// NoFollow rejects a final symlink, except that Path|NoFollow opens the link
// itself (O_NOFOLLOW). It does not restrict intermediate symlinks.
const NoFollow OpenFlags = unix.O_NOFOLLOW

// Path opens a path-only handle (O_PATH), without data I/O access. It can be
// passed to Stat or used as a directory anchor. Only Directory and NoFollow
// may be combined with Path; the access mode must be ReadOnly.
const Path OpenFlags = unix.O_PATH

// Resolve selects openat2 lookup restrictions. Zero uses ordinary resolution;
// combine restrictions with | except that Beneath and InRoot are incompatible.
type Resolve uint64

// NoXDev rejects mount-point crossings, including bind mounts (RESOLVE_NO_XDEV).
const NoXDev Resolve = unix.RESOLVE_NO_XDEV

// NoMagicLinks rejects procfs-style magic-link traversal (RESOLVE_NO_MAGICLINKS).
const NoMagicLinks Resolve = unix.RESOLVE_NO_MAGICLINKS

// NoSymlinks rejects symlink traversal in every component and implies
// NoMagicLinks (RESOLVE_NO_SYMLINKS). Path|NoFollow can still open a final link.
const NoSymlinks Resolve = unix.RESOLVE_NO_SYMLINKS

// Beneath rejects absolute paths and escapes above the directory anchor,
// including through symlinks (RESOLVE_BENEATH). Use NoMagicLinks explicitly
// when that guarantee is also needed.
const Beneath Resolve = unix.RESOLVE_BENEATH

// InRoot treats the directory anchor as / for this lookup, including absolute
// paths, absolute symlinks, and .. (RESOLVE_IN_ROOT). It does not change the
// process root. Use NoMagicLinks explicitly when that guarantee is needed.
const InRoot Resolve = unix.RESOLVE_IN_ROOT

// Cached permits only cached resolution (RESOLVE_CACHED). EAGAIN is returned
// when lookup needs uncached work; Open never retries without this restriction.
// The pinned x/sys omits this constant; Linux 7.0 defines it in
// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/openat2.h.
const Cached Resolve = 0x20

// OpenOptions controls Open. The zero value requests ordinary read-only access.
type OpenOptions struct {
	// Flags selects access and creation behavior. Close-on-exec is unconditional.
	Flags OpenFlags

	// Perm supplies Go permission, setuid, setgid, and sticky bits for Create,
	// before umask and filesystem policy. It must be zero without Create.
	Perm os.FileMode

	// Resolve restricts this lookup; zero does not provide confinement.
	Resolve Resolve
}

// Open opens name relative to dir using [openat2(2)] and returns an owned
// standard file. The caller must close the result. No resource is returned or
// retained on failure. The descriptor is atomically close-on-exec.
//
// A live, nonnil dir is required even for absolute names. Without InRoot,
// absolute names ignore the anchor; ordinary relative names can escape it.
// Names are passed unchanged, preserving trailing slashes and symlink/.. rules.
// Unknown bits, invalid access modes or Perm, conflicting Beneath/InRoot, and
// incompatible Path flags match os.ErrInvalid before borrowing dir. Remaining
// constraints are checked by Linux. Kernel failures retain errno, including
// EAGAIN for Cached or a confinement race, EXDEV for escapes/crossings, and
// ELOOP for prohibited traversal. No retry or weaker fallback is performed.
//
// Blocking special-file opens can occupy an OS thread and cannot be interrupted
// by closing dir. Returned files retain os.File's I/O and deadline contracts.
// Name returns the supplied name, which need not identify the file from cwd.
//
// [openat2(2)]: https://man7.org/linux/man-pages/man2/openat2.2.html
func Open(dir *os.File, name string, options OpenOptions) (*os.File, error) {
	mode, err := openMode(options)
	if err != nil {
		return nil, &os.PathError{Op: "openat2", Path: name, Err: err}
	}

	var fd int
	err = control(dir, func(anchor int) error {
		var callErr error
		fd, callErr = unix.Openat2(anchor, name, &unix.OpenHow{
			Flags: uint64(options.Flags) | unix.O_CLOEXEC, Mode: mode, Resolve: uint64(options.Resolve),
		})
		return callErr
	})
	if err != nil {
		return nil, &os.PathError{Op: "openat2", Path: name, Err: err}
	}

	return os.NewFile(uintptr(fd), name), nil
}

func openMode(options OpenOptions) (uint64, error) {
	const (
		flags       = WriteOnly | ReadWrite | Append | Create | Exclusive | Truncate | Directory | NoFollow | Path
		resolve     = NoXDev | NoMagicLinks | NoSymlinks | Beneath | InRoot | Cached
		permissions = os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	)

	unknownFlags := options.Flags & ^flags != 0
	invalidAccess := options.Flags&(WriteOnly|ReadWrite) == WriteOnly|ReadWrite
	invalidPathFlags := options.Flags&Path != 0 && options.Flags & ^(Path|Directory|NoFollow) != 0
	unknownResolve := options.Resolve & ^resolve != 0
	conflictingResolve := options.Resolve&(Beneath|InRoot) == Beneath|InRoot
	unknownPermissions := options.Perm & ^permissions != 0
	permissionsWithoutCreate := options.Flags&Create == 0 && options.Perm != 0
	if unknownFlags || invalidAccess || invalidPathFlags ||
		unknownResolve || conflictingResolve ||
		unknownPermissions || permissionsWithoutCreate {
		return 0, os.ErrInvalid
	}

	return linuxMode(options.Perm), nil
}

func linuxMode(perm os.FileMode) uint64 {
	mode := uint64(perm.Perm())
	if perm&os.ModeSetuid != 0 {
		mode |= unix.S_ISUID
	}
	if perm&os.ModeSetgid != 0 {
		mode |= unix.S_ISGID
	}
	if perm&os.ModeSticky != 0 {
		mode |= unix.S_ISVTX
	}
	return mode
}
