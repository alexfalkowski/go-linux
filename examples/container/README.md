# 📦 Container runner

A small [Docker-inspired](https://docs.docker.com/engine/security/) process runner. It uses an existing root filesystem, private namespaces, a read-only root mount, private tmpfs, capability bounding drops, Landlock, seccomp, and pidfd shutdown. It demonstrates the library's mechanisms and is not a complete security boundary for untrusted workloads.

## 🚀 Try the supplied guest

```sh
make example-container
```

The Make launcher builds a static Go guest and a temporary root filesystem containing `/bin/hello` and `/tmp`. Expected output includes:

```text
private user/mount/network/UTS/IPC/PID namespaces; read-only root; 16 MiB tmpfs
hello from pid=1 uid=0; effective capabilities=0x0
/tmp/hello-<suffix>: scratch space belongs to this run
```

The guest writes and reads its private scratch file. Exiting destroys the PID namespace and its remaining processes. The helper's private mount namespace is released at process exit, and the parent removes its empty staging directory. The launcher removes the demo root filesystem afterward.

This requires permitted mapped user/network/mount/UTS/IPC/PID namespaces, the modern mount API, capability operations, Landlock, and seccomp. A root UID or successful build does not establish those permissions. Sandboxes can refuse user namespace creation with `EPERM`. The same demo can run on a native Linux Docker host that permits privileged containers:

```sh
make example-container-docker image=alexfalkowski/go:5.23
```

The image matches [.circleci/config.yml](../../.circleci/config.yml). Docker may acquire it from the registry. This target mounts the checkout read-only and changes only the disposable container/child namespaces. It does not refresh dependencies. Existing host/daemon policy can still refuse an operation; failures remain visible without a weaker fallback.

## 🔧 Supply a root filesystem

Use a trusted directory containing the executable, any interpreter/shared libraries it needs, and a real nonsymlink `/tmp` directory:

```sh
make example-container args='["-rootfs","/path/to/rootfs","--","/bin/sh","-c","echo hello; echo private > /tmp/message; cat /tmp/message"]'
```

The executable path must be absolute inside that root. The root is cloned without child mounts and made read-only with NoSUID/NoDev; a fresh 16 MiB NoSUID/NoDev tmpfs covers `/tmp`. The environment contains only `PATH=/bin:/usr/bin`, `HOME=/tmp`, and `TMPDIR=/tmp`. Stdin is EOF; stdout/stderr remain connected to the caller. There is no terminal setup, hostname change, `/proc` or `/dev` population, image download, network configuration, or volume management. The private network namespace starts without external connectivity. An arbitrary root filesystem may require those omitted facilities.

The helper stays outside the payload's PID namespace and starts the payload synchronously on a locked thread after `namespace.Unshare(PID)`. The payload becomes PID 1 and owns init/reaping duties while it runs. Go's standard `SysProcAttr` performs namespace mappings and chroot; there is no raw fork wrapper. Parent mount identity is captured before startup to verify that the helper's mount namespace differs, without requiring the mapped child to inspect an ancestor's protected proc links.

Before exec, the helper clears ambient/inheritable capabilities and drops bounding capabilities. It keeps its current chroot authority until Go completes child startup; exec then produces an empty capability set. Landlock handles file reads/execution and ordinary writes, allows reads/execution within the staged root and writes within its `/tmp`. A small native-ABI seccomp deny list refuses mount configuration, namespace transitions, ptrace, and BPF syscalls. Other syscalls remain allowed. These deliberately limited policies do not constitute a general sandbox policy.

Ctrl-C/TERM requests payload shutdown through pidfd, with one second before KILL. The outer parent bounds helper shutdown too. Parent-death signals terminate the helper/payload if their creating thread disappears; those threads stay locked for the child's lifetime. PID 1's exit terminates its remaining namespace processes.

## 📏 Optional cgroup limits

For a native run, supply an existing writable cgroup v2 delegation with memory and PIDs controllers already enabled:

```sh
make example-container args='["-cgroup","/path/to/delegation","-memory","67108864","-pids","32"]'
```

The parent creates `example-<parent-pid>`, sets `memory.max` and `pids.max`, moves the waiting helper into it, and only then releases the payload startup gate. Helper and payload tasks belong to that group; memory allocated before migration can remain charged to the previous group under Linux accounting rules. Defaults are 64 MiB and 32 tasks when a delegation is supplied; there are no cgroup limits without one. After shutdown the parent closes and removes its child group. The application never mounts a hierarchy, enables ancestor controllers, or moves unrelated processes. Native delegation setup belongs to the operator; [cgroup usage](../../docs/cgroup.md) and [verification](../../docs/verification.md#cgroup-verification) explain prerequisites.

The Docker launcher can prepare a disposable delegation inside its private cgroup namespace explicitly:

```sh
make example-container-docker image=alexfalkowski/go:5.23 limits=true
make example-container-docker image=alexfalkowski/go:5.23 limits=true args='["-memory","100663296","-pids","48"]'
```

This setup follows the existing cgroup verification launcher: it mounts a new v2 view rooted at the disposable container, moves that container's setup processes into a runner leaf, and enables memory/PIDs controllers for its own delegation. It does not bind or modify a host-wide hierarchy. Existing parent limits still apply. Without `limits=true`, the Docker launcher does not prepare a delegation.
