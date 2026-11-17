# 🔁 Process supervisor

A small [systemd-inspired](https://freedesktop.org/software/systemd/man/latest/systemd.service.html) supervisor for one command, using [pidfd](../../docs/pidfd.md) and [timerfd](../../docs/timerfd.md).

```sh
make example-supervise
make example-supervise args='["--","/bin/echo","supervised successfully"]'
make example-supervise args='["-restarts","2","-backoff","100ms","-max-backoff","1s","--","/bin/sh","-c","exit 7"]'
```

With no command, the example supervises `/bin/sleep 30`. A successful child exit ends the supervisor successfully. Failed exits use exponential backoff, beginning at `-backoff` (one second) and capped by `-max-backoff` (ten seconds). `-restarts` defaults to three and counts retries after the initial attempt; the final command above deliberately fails after three total attempts. Failure to start or acquire a process handle stops immediately. A successful start prints its PID and attempt number to stderr.

Backoff uses `timerfd.Boottime`: suspended time counts toward the restart delay, and discontinuous wall-clock changes do not alter it. The example creates a new one-shot timer for each delay, and closing it interrupts waiting during shutdown.

Ctrl-C or TERM signals the direct child through its pidfd. `-grace` defaults to two seconds before KILL. Exit observation leaves status consumption and pipe cleanup to `exec.Cmd.Wait`; the supervisor joins both before returning. Its own exit status is zero for successful completion/operator shutdown and one for errors or an exhausted restart budget.

Arguments are literal and no shell is added. Descendant process management, service units, daemonization, dependency ordering, and process-group signaling are outside this example. When using a shell wrapper for a long-running program, use shell `exec` so the workload becomes the supervised child.
