# seccomp design decisions

See the [usage guide](../seccomp.md) for operations and [verification](../verification.md#seccomp-verification) for contributor commands and environments.

## Bound the mechanism before adding a compiler

The package accepts a small constant-operand subset of classic BPF rather than a syscall-name policy language, eBPF framework, or unrestricted instruction set. Absolute word loads, constant masking, forward comparisons/jumps, and constant results express syscall and scalar-argument checks while keeping validation bounded and independent of kernel mutation. Excluding scratch state, indirect reads, computed return actions, and a compiler avoids a verifier or language whose policy semantics would need a separate security contract. Unknown actions, user notification, and ignored input data are rejected explicitly; requested behavior is never masked away.

Construction copies to maintained `x/sys/unix.SockFilter` values, and loading uses its syscall number and `SockFprog` layout. No raw descriptor or callback is necessary. Linux retains an installed program independently of the Go value, so a resource-style Close would misleadingly imply reversibility. The immutable filter can instead be constructed once and shared; each Load remains a new stacking operation with kernel-owned lifetime.

## Guard the ABI outside caller control

Explicit architecture labelling makes the caller's syscall-number convention visible. Accepting only the current amd64 or arm64 ABI avoids silently interpreting another architecture's numbers. A mandatory prefix kills other audit architectures; amd64 additionally rejects x32's syscall bit because audit architecture alone does not distinguish it. Caller jumps stay inside their own forward-only body and cannot reach or bypass that prefix. Resetting the accumulator afterward preserves the usual zero-initialized classic-BPF body semantics.

This deliberately excludes multi-ABI policies and cross-architecture exec. An unrestricted raw-program entrypoint would let callers omit this essential check and make validation claims much weaker. The fixed body bound reserves the largest prefix within the 4096-instruction kernel limit, giving the same public size limit on both supported architectures. ABI fields, actions, and TSYNC behavior are checked against [Linux 7.0 UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/seccomp.h) and its [implementation](https://github.com/torvalds/linux/blob/v7.0/kernel/seccomp.c); [seccomp(2)](https://man7.org/linux/man-pages/man2/seccomp.2.html) explains x32 and argument-width pitfalls.

## Make scope and partial state explicit

A zero synchronization value is invalid. A thread-local default could leave a multithreaded Go caller believing its process was restricted; automatic TSYNC would make otherwise local code change every runtime thread. Explicit CallingThread and AllThreads expose that choice without enumerating Go threads or using private runtime hooks. TSYNC's positive refusing TID has its own error type, preserving information that an errno-only wrapper would lose. Failed synchronization never falls back to local attachment.

No-new-privileges is an explicit loading option rather than an undocumented prerequisite or unconditional side effect. Keeping its prctl separate inside Load lets callers with an existing bit or appropriate capability avoid that operation. It also means a successful prctl followed by failed loading cannot roll back the bit. The API documents this partial state, requires caller-owned thread affinity, and offers no scoped callback or restoration promise. Kernel TSYNC handles process-wide filter consistency; application policy coordination remains external.

The package leaves Go syscall policy, signal disposition, ptrace supervision, and process startup to their existing owners. A complete sandbox or runtime-compatible allow-list would require application-specific decisions far beyond filter loading. Real restrictions therefore run only in bounded child fixtures during verification, including deliberate fatal actions, thread-tree conflicts, and inherited policy after exec.
