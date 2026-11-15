# API conventions

These conventions govern the library's public Go APIs. Package design notes explain choices specific to each facility; usage belongs in `docs/<package>.md` and GoDoc. [Verification](../verification.md) owns contributor commands, runner requirements, and the verified environment matrix; [IMPLEMENTATION.md](../../IMPLEMENTATION.md) owns unfinished work and verification gaps. Design notes record decisions, alternatives, and consequences rather than API inventories, implementation history, or test reports.

## Standard-library boundary

Add Linux mechanisms that the standard library does not express; reuse ordinary Go I/O, process startup, and resource management. Prefer the maintained `golang.org/x/sys/unix` bindings over copied syscall numbers or ABI layouts. Packages depend on implemented facilities only; shared internals need demonstrated reuse rather than a planned family of future packages.

Use familiar resource names and operations: `New` creates, `Open` acquires by identity, `Adopt` consumes ownership, and `Close` releases a local resource. Independent flag bits use typed masks; standalone mutually exclusive modes use enums. Use named option fields when they clarify settings of different kinds. Preserve conventional Go/Linux flag combinations rather than replacing masks solely to avoid `|`. Document zero options and reject unknown input bits or contradictory combinations. Preserve unknown output bits so inspection does not silently discard newer kernel information.

Facility owners keep descriptors private. Typed operations perform borrowing, duplication, and polling internally; exported raw callbacks, descriptor accessors, and embedded files would make those implementation details permanent caller responsibilities. Selected standard interfaces remain available where their contracts fit. The `file` package deliberately borrows and returns standard files because it adds individual file operations rather than another resource owner.

## Ownership and lifetime

Creation and adoption return no resource on error and close every resource acquired or consumed. Callers arrange cleanup only after success. `Adopt` consumes its input even when validation fails, avoiding conditional ownership and caller error-path cleanup. Borrowing leaves inputs caller-owned on both success and failure. An operation failing on an existing owner does not transfer its cleanup responsibility.

Duplicate under lifetime protection when independent descriptor ownership is required. Duplicates still share open-file-description state, including status flags and offsets; changing that state can affect another owner. Never give two independent file owners the same live descriptor. Internal `RawConn.Control` calls pin borrowed descriptors against close/reuse; they do not make blocking syscalls cancelable.

Create descriptors atomically close-on-exec. Use explicit Close rather than relying on garbage collection, and never retry a Linux descriptor close after an error because its number may already have been reused. Closing a descriptor releases that reference; it does not necessarily undo the kernel effect that created or used it. Mappings, attached mounts, and process reaping have distinct lifetimes explained in their package guides.

Owned wrapper types are used through pointers and must not be copied after use. Their zero value owns nothing; error-returning operations report `os.ErrInvalid`, with view methods documented separately. Repeated close and valid operations after successful descriptor closure match `os.ErrClosed`. Document validation precedence and exceptional cleanup semantics per operation. Methods may be safe concurrently while sequences still require caller coordination.

## Interfaces, records, and clocks

Expose standard interfaces only when their behavioral contracts fit. Files support ordinary streams; counters and timers preserve one native-endian eight-byte record per read. Validate record sizes before generic file I/O, which can otherwise treat empty buffers or partial writes as stream operations. Typed helpers use `encoding/binary.NativeEndian`; their results are values or counts, not byte lengths. Readiness sets and process handles do not become byte streams merely because Linux represents them with descriptors.

Use synchronous `iter.Seq2[Event, error]` sequences when a facility benefits from individual event iteration. Document read-ahead ownership, early exit and resumption, terminal errors, and concurrent-use limits. Keep scheduling, channel buffering, and session cancellation under caller ownership unless a concrete facility contract requires otherwise.

Use `time.Duration` for relative intervals and `time.Time` for Go I/O deadlines. Absolute Linux timer values carry their clock domain explicitly rather than borrowing Go's wall-clock or private monotonic representation. Keep kernel timer arming separate from the deadline that bounds reading its records.

## Blocking and errors

Counter and timer constructors require nonblocking descriptors and establish Go polling before returning. `os.NewFile` alone does not prove successful poller registration; a zero deadline probe checks the promised integration and failure closes the resource. Do not replace failed polling with blocking I/O or a hidden goroutine. Explicit kernel waits may occupy an OS thread. Add contexts or deadlines only when a real interruption mechanism can release the pending operation.

Preserve errno through `errors.Is` and `errors.As`, using standard error wrappers when they carry sufficient context. Input validation normally matches `os.ErrInvalid`; documented Linux-shaped validation may use errno directly. Do not collapse `EPERM`, `EINVAL`, or `ENOSYS` into a blanket unsupported-feature result. Structured errors are justified when callers need additional information, such as mount diagnostics; keep the primary failure distinguishable from secondary retrieval failures.

Do not emulate atomic kernel operations with multiple racy calls, silently weaken requested restrictions, or promise rollback after a syscall may have applied a change. Retry interruption only where repeating the operation preserves its contract.

## Compatibility

The support floor is Linux 7.0 and the Go version declared in [go.mod](../../go.mod). Check selected operations against versioned Linux sources; runner policy and filesystem support remain separate prerequisites. Newer optional features must not silently raise the floor. Preserve exported names, zero defaults, ownership, error matching, and blocking/close behavior as compatibility surfaces. Record necessary migrations with the affected usage documentation. Runtime results and open acceptance gaps belong to the verification guide and implementation plan, not to parallel status lists here.
