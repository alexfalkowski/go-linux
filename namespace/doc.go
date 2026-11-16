//go:build linux

// Package namespace owns Linux namespace handles and exposes explicit setns
// and unshare operations. Handles pin namespaces independently of process
// lifetime; closing a handle never restores a thread's namespace membership.
//
// Enter and Unshare require a dedicated goroutine locked with
// runtime.LockOSThread before observing or changing thread state. Leave it
// locked until the goroutine exits, including on failure or recovered panic;
// Go then retires the thread. Newly started goroutines do not inherit that
// goroutine's namespaces. No scoped callback or automatic restoration is offered.
//
// Namespace transitions are subject to capabilities, ownership, shared-state,
// and multithreading restrictions. In particular, user namespace transitions
// cannot succeed in an ordinary multithreaded Go process. Use os/exec with
// syscall.SysProcAttr for child namespace creation and UID/GID mappings.
// PID entry and PID/time creation affect future children; time entry requires
// a single-threaded process and can change its clock domain. Unshare initializes
// Go's process support before PID creation so its lazy probe cannot become the
// new namespace's PID 1. See
// https://man7.org/linux/man-pages/man2/setns.2.html and
// https://man7.org/linux/man-pages/man2/unshare.2.html.
package namespace
