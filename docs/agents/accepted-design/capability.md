# capability design decisions

Part of the [accepted-design index](../accepted-design.md).

See the [usage guide](../../capability.md) for contracts and [verification](../../verification.md#capability-verification) for contributor commands and environments.

## Express masks and individual operations directly

The public `Set` type uses typed masks so combinations and removals read like other Go/Linux flags. A separate numeric capability enum would require conversion at every set operation. Bounding and ambient operations instead validate one bit and convert internally to the kernel index. Zero, combined, and unknown masks cannot accidentally select a different capability. `Sets` keeps permitted, effective, and inheritable values together because the kernel replaces them together; it is an ordinary value without descriptor ownership or hidden policy.

The maintained `x/sys/unix` version-3 bindings already express the two-word capability ABI. Explicit word splitting and joining preserve its 64 bits without cgo, copied syscall tables, unsafe pointers, or a second native dependency. The baseline capability list and operation semantics follow [Linux 7.0 UAPI](https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/capability.h) and [capability checks](https://github.com/torvalds/linux/blob/v7.0/security/commoncap.c). Inspection retains unknown output bits, while mutation rejects unknown inputs: Linux may otherwise mask unknown capset bits, which could silently discard requested policy.

## Keep state changes separate from exec policy

Linux credentials are thread state. A process-wide setter would need coordination with existing and future Go threads; a locked goroutine supplies affinity without supplying that coordination. Explicit operations and mandatory thread retirement make the boundary visible, matching namespace transitions. Automatic save/restore would hide irreversible permitted or bounding drops and could return a changed thread to unrelated Go work after failure.

Bounding membership and ambient membership remain separate mechanisms. Combining them into a single apply operation would suggest atomicity and rollback that the kernel does not provide. A bounding drop does not remove current authority, and permitted/effective sets are recomputed by exec. The library therefore leaves restriction policy, securebits, UID transitions, executable attributes, and no-new-privileges to callers rather than claiming that clearing a few masks creates a sandbox.

Standard Go process creation already owns fork/exec safety and child-start ambient configuration. Replacing it would duplicate lifecycle and runtime obligations. This package instead permits synchronous standard child startup on the locked thread, with the kernel's exec transformation left intact. Tests disable UID-0 restoration only inside disposable fixtures to distinguish ambient inheritance from root privilege restoration; the public API never makes that policy choice.
