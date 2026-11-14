//go:build linux

// Package epoll provides explicit Linux readiness sets for standard files.
// It requires Linux 7.0 or later and permitted epoll, eventfd, and descriptor
// duplication operations; ordinary use needs no extra capabilities.
//
// New creates a Poller. Poller.Add borrows an os.File and returns an owned
// Registration with an independent descriptor lifetime. Wait returns readiness
// with stable registration identities. Registrations support level-triggered,
// edge-triggered, and one-shot notifications. Readiness is advisory: coordinate
// competing I/O and do not assume an event reserves data or buffer space.
//
// Close registrations to remove them, and close the poller to release all
// remaining registrations and interrupt every pending Wait. Wait uses a kernel
// wait that can occupy an OS thread; it does not replace Go's network poller.
// File I/O and its deadlines remain the caller's responsibility. No raw
// descriptors, callbacks, or underlying files are exposed.
package epoll
