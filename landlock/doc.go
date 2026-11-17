//go:build linux

// Package landlock builds Linux access rulesets and explicitly applies them to
// the calling thread or all threads of the calling process. See
// https://docs.kernel.org/7.0/userspace-api/landlock.html.
//
// New and the Add methods do not restrict the caller. Restrict is irreversible:
// subsequent layers can only constrain the caller further. Close releases the
// ruleset descriptor without undoing an applied policy. Files passed to AddFile
// remain caller-owned; the kernel retains the rule's object independently.
//
// Lock the applying goroutine with runtime.LockOSThread before Restrict and
// leave it locked until it exits, including on failure and recovered panic.
// CallingThread covers only that thread and its future children, including
// across exec. It does not restrict other existing threads or arbitrary Go
// goroutines. AllThreads uses kernel TSYNC (ABI 8, Linux 7.0) without fallback;
// it replaces sibling threads' Landlock domains with the caller's resulting
// domain, even when siblings had different restrictions. Coordinate all policy
// changes and apply from the intended domain. Existing separate processes are
// unaffected. Test policies in disposable subprocesses: restricting Go runtime
// access can prevent the program from making progress.
//
// Handled rights are denied unless rules allow them. Unhandled rights remain
// unaffected, except that cross-directory Refer is denied even when unhandled.
// Already-open files, unhandled metadata operations, and network operations
// outside the selected rights are not a complete sandbox. Requested rights are
// never silently removed. ABI discovery preserves kernel and policy errors;
// known rights unavailable in that ABI match EOPNOTSUPP. Unknown inputs match
// os.ErrInvalid. Kernel errno remains accessible through standard error wrappers.
package landlock
