//go:build linux

package mmap

import (
	"os"

	"golang.org/x/sys/unix"
)

// Advice selects one whole-region [madvise(2)] operation, not a bitmask.
// Advice can change mapped contents, access, and inheritance.
//
// [madvise(2)]: https://man7.org/linux/man-pages/man2/madvise.2.html
type Advice uint8

// Normal restores the default access-pattern advice (MADV_NORMAL).
const Normal Advice = unix.MADV_NORMAL

// Random suggests that accesses will be random (MADV_RANDOM).
const Random Advice = unix.MADV_RANDOM

// Sequential suggests that accesses will be sequential (MADV_SEQUENTIAL).
const Sequential Advice = unix.MADV_SEQUENTIAL

// WillNeed suggests bringing pages into memory (MADV_WILLNEED). Success does
// not promise that all pages are resident or that later access cannot block.
const WillNeed Advice = unix.MADV_WILLNEED

// DontNeed discards this mapping's resident pages (MADV_DONTNEED). Subsequent
// access to private anonymous pages sees zeros; private file changes are lost
// and pages are reloaded from the file. Shared pages are repopulated from their
// backing object, which need not be freed immediately. This does not truncate
// the file or promise writeback, durability, or zeroing of shared contents.
const DontNeed Advice = unix.MADV_DONTNEED

// Free marks private anonymous pages for lazy discard (MADV_FREE).
// Access sees the old contents until reclaim discards them. Writing a page
// before reclaim cancels the discard for that page.
const Free Advice = unix.MADV_FREE

// Remove punches a hole in the backing store of an unlocked shared mapping
// (MADV_REMOVE). Linux must permit changing the backing object, which must
// support hole punching. A mapping from a writable file can punch a hole even
// with read-only page protection. Other users of that range observe the change.
// The backing object's size is preserved.
const Remove Advice = unix.MADV_REMOVE

// DontFork omits the mapping from children created by fork (MADV_DONTFORK).
const DontFork Advice = unix.MADV_DONTFORK

// DoFork restores inheritance of the mapping across fork (MADV_DOFORK).
const DoFork Advice = unix.MADV_DOFORK

// WipeOnFork gives children zero-filled pages for a private anonymous
// mapping (MADV_WIPEONFORK). The parent keeps its contents.
const WipeOnFork Advice = unix.MADV_WIPEONFORK

// KeepOnFork cancels WipeOnFork (MADV_KEEPONFORK).
const KeepOnFork Advice = unix.MADV_KEEPONFORK

// DontDump excludes the mapping from core dumps (MADV_DONTDUMP).
const DontDump Advice = unix.MADV_DONTDUMP

// DoDump makes the mapping subject to normal core dump policy (MADV_DODUMP).
const DoDump Advice = unix.MADV_DODUMP

// Mergeable makes eligible pages candidates for kernel same-page merging
// (MADV_MERGEABLE). This requires kernel KSM support.
const Mergeable Advice = unix.MADV_MERGEABLE

// Unmergeable opts out of kernel same-page merging and breaks existing
// sharing created by KSM (MADV_UNMERGEABLE). This requires kernel KSM support.
const Unmergeable Advice = unix.MADV_UNMERGEABLE

// HugePage marks a mapping as eligible for transparent huge pages
// (MADV_HUGEPAGE). Allocation depends on kernel support and policy.
const HugePage Advice = unix.MADV_HUGEPAGE

// NoHugePage disables transparent huge pages for the mapping
// (MADV_NOHUGEPAGE). It does not promise to split existing huge pages.
const NoHugePage Advice = unix.MADV_NOHUGEPAGE

// Collapse requests synchronous best-effort collapse into transparent huge
// pages (MADV_COLLAPSE). Unsupported mappings and ranges without an eligible
// aligned huge page can fail. This advice does not set future page policy.
const Collapse Advice = unix.MADV_COLLAPSE

// Cold makes eligible pages more likely to be reclaimed (MADV_COLD).
const Cold Advice = unix.MADV_COLD

// PageOut asks Linux to reclaim eligible pages without discarding their
// contents (MADV_PAGEOUT). Reclaim and writeback depend on kernel policy.
const PageOut Advice = unix.MADV_PAGEOUT

// PopulateRead prefaults readable pages (MADV_POPULATE_READ). The mapping
// must permit reading. Access errors are returned instead of raising SIGBUS.
const PopulateRead Advice = unix.MADV_POPULATE_READ

// PopulateWrite prefaults writable pages, including breaking copy-on-write
// (MADV_POPULATE_WRITE). The mapping must permit writing.
const PopulateWrite Advice = unix.MADV_POPULATE_WRITE

// DontNeedLocked discards pages as DontNeed does, including pages in a locked
// mapping (MADV_DONTNEED_LOCKED). The mapping's lock policy remains in effect.
const DontNeedLocked Advice = unix.MADV_DONTNEED_LOCKED

// GuardInstall discards private contents and installs guards that fault on
// read or write (MADV_GUARD_INSTALL). The mapping and its backing object
// remain. Linux rejects installation on locked mappings. Protection changes
// leave guards in place.
const GuardInstall Advice = unix.MADV_GUARD_INSTALL

// GuardRemove removes guards (MADV_GUARD_REMOVE). Later access follows the
// mapping's protection and repopulates former guards from the backing object
// or as zero-filled anonymous pages. Discarded private contents are lost.
const GuardRemove Advice = unix.MADV_GUARD_REMOVE

// Advise applies one supported advice to all pages intersecting the region
// through [madvise(2)]. Unknown values match os.ErrInvalid without a syscall;
// owner-state errors take precedence. Kernel errors preserve errno and may
// follow partial application. Advice is synchronous as a syscall and may block;
// no context, deadline, or residency guarantee is offered.
//
// [madvise(2)]: https://man7.org/linux/man-pages/man2/madvise.2.html
func (r *Region) Advise(advice Advice) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.stateError(); err != nil {
		return &os.PathError{Op: "madvise", Path: "mmap", Err: err}
	}
	switch advice {
	case Normal, Random, Sequential, WillNeed, DontNeed,
		Free, Remove, DontFork, DoFork, WipeOnFork, KeepOnFork, DontDump, DoDump,
		Mergeable, Unmergeable, HugePage, NoHugePage, Collapse, Cold, PageOut,
		PopulateRead, PopulateWrite, DontNeedLocked, GuardInstall, GuardRemove:
	default:
		return &os.PathError{Op: "madvise", Path: "mmap", Err: os.ErrInvalid}
	}
	if err := unix.Madvise(r.data, int(advice)); err != nil {
		return &os.PathError{Op: "madvise", Path: "mmap", Err: err}
	}

	return nil
}
