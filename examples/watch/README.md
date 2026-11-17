# 👀 Directory watcher

A small [Watchman-inspired](https://github.com/facebook/watchman) command runner using [inotify](../../docs/inotify.md) and [pidfd](../../docs/pidfd.md).

Create a directory and start the watcher:

```sh
mkdir -p /tmp/go-linux-watch
make example-watch args='["-dir","/tmp/go-linux-watch","--","/bin/echo","rebuild requested"]'
```

In another terminal:

```sh
touch /tmp/go-linux-watch/hello
```

The command prints `rebuild requested` once at startup and again after a quiet period following changes. `-initial=false` disables the startup run. `-debounce` defaults to `150ms`. The command's working directory is the watched directory, and its arguments are passed directly to `os/exec`.

The watch covers the directory and its immediate children. It observes creation, deletion, renames, and completed writable opens. Subdirectories are not watched recursively. A notification overflow triggers a fresh directory listing and a full command run; the command should rebuild from current filesystem state. Moving/removing/unmounting the watched directory stops the watcher with an error so the caller can choose its new path.

After the debounce interval, any previous command receives TERM and is reaped before the replacement starts. `-grace` defaults to one second; a child still running after that receives KILL. Ctrl-C follows the same shutdown path. The pidfd observes and signals the direct child, while `exec.Cmd.Wait` owns reaping. Commands that spawn descendants own their cleanup; use `exec` in shell wrappers when the actual workload should replace the shell. Command failures are printed and watching continues; startup/watch failures stop the watcher.

Keep generated output outside the watched directory when it would trigger another rebuild. This example is a rerun loop, not a lossless filesystem audit log or a persistent watch service.
