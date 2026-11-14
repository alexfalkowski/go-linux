//go:build linux

package epoll

import (
	"os"

	"golang.org/x/sys/unix"
)

// Registration owns one entry and a duplicate of its source file. Its pointer
// is the event identity; descriptor numbers and registration IDs are private.
// Do not copy a Registration after first use. A zero registration matches
// os.ErrInvalid; a removed registration or closed poller matches os.ErrClosed.
type Registration struct {
	poller *Poller
	file   *os.File
	id     uint64
}

// Modify replaces the complete interest mask and rearms OneShot delivery,
// including when the mask is unchanged. Unknown bits match os.ErrInvalid before
// state checks. Zero requests only automatic Error and Hangup reporting. An
// already returned event may describe the previous mask. Failure leaves the
// registration owned by its caller and retains the existing kernel settings.
func (r *Registration) Modify(interests Events) error {
	if r.poller == nil || !validInterests(interests) {
		return os.ErrInvalid
	}

	p := r.poller
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed || p.registrations[r.id] != r {
		return os.ErrClosed
	}
	return p.control(unix.EPOLL_CTL_MOD, r, interests)
}

// Close removes the entry with EPOLL_CTL_DEL and closes its owned duplicate.
// It leaves the caller's original file open. A deletion error retains ownership
// and allows retry; after successful deletion, even a descriptor-close error
// leaves the registration closed. Repeated Close matches os.ErrClosed.
func (r *Registration) Close() error {
	if r.poller == nil {
		return os.ErrInvalid
	}

	p := r.poller
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed || p.registrations[r.id] != r {
		return os.ErrClosed
	}
	if err := p.control(unix.EPOLL_CTL_DEL, r, 0); err != nil {
		return err
	}

	delete(p.registrations, r.id)
	return r.file.Close()
}
