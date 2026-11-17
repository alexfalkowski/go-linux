# epoll design decisions

The [usage guide](../epoll.md) owns readiness and registration workflows; [verification](../verification.md) records runtime evidence and limits.

## Registrations own source lifetime

Add privately duplicates a borrowed standard file under lifetime protection. Depending on the caller to keep its original open would allow close/reuse races; consuming it would prevent ordinary I/O. A duplicate supplies independent ownership and permits multiple registrations for one source. The cost is deliberate: registrations can keep pipes or sockets alive and delay peer EOF until removed. Status flags and offsets still belong to the shared open description, so epoll leaves them unchanged.

Opaque eventfd and timerfd owners join a set through their own typed Register methods. They depend on epoll and delegate to Add internally, reusing its lifetime protection, duplication, validation, and cleanup. Epoll continues to accept standard files without importing the facility packages, so the dependency direction stays acyclic. This avoids a universal descriptor interface or callback that would expose syscall plumbing. Pidfd integration remains a separate ownership and readiness decision.

## Stable identity across removal and reuse

Events expose the Registration pointer returned by Add. The kernel carries a nonzero 64-bit token translated through the poller's registry, never a Go pointer or a descriptor number. Tokens are not recycled; exhaustion fails rather than permitting an old batch to identify a replacement. Removed tokens are filtered before translation, while already translated events retain their original identity. This preserves advisory readiness without pretending that removal retracts earlier observations. See [identity translation](../../epoll/poller.go).

## Shutdown must outlive every kernel wait

Closing an epoll descriptor is not the wait-cancellation protocol. A private level-triggered eventfd is signaled and left unread so all waiters wake. Close prevents new admissions, waits for admitted calls to leave, then releases the instance, wake file, and registration duplicates. No lifecycle mutex is held across a kernel wait.

Positive timeout budgets subtract elapsed monotonic time across EINTR and bounded syscall waits. This avoids restarting the budget after interruption and avoids constructing a deadline that can overflow for very large durations. Millisecond rounding belongs to the kernel wait boundary rather than changing the public duration type.

This model starts no background goroutine but can occupy one OS thread per blocked Wait. Timeouts and Close provide interruption; signal-mask control, contexts, and newer wait variants require separate decisions. Registration deletion failure retains ownership, whereas a descriptor-close error after deletion cannot safely be handled by closing that number again.
