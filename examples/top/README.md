# 📊 Process monitor

A small [htop-inspired](https://htop.dev/) process table using [procfs](../../docs/procfs.md). It samples process CPU counters, converts resident pages to MiB, and shows system RAM. It uses the standard Go ticker for ordinary refresh scheduling.

```sh
make example-top
make example-top args='["-once","-sort","memory","-limit","5"]'
make example-top args='["-count","3","-interval","500ms","-sort","pid"]'
```

Frames contain `PID`, `STATE`, `CPU%`, `RSS MiB`, `THREADS`, and `NAME`. The first frame appears after one sampling interval, so its CPU column describes an actual delta. CPU is a percentage of one logical CPU and can exceed 100 for a multithreaded process. `-sort` accepts `cpu`, `memory`, or `pid`, with PID breaking ties. `-once` overrides `-count` to one frame; zero count runs until interrupted.

`-proc` selects a trusted proc root and defaults to `/proc`. PID and start ticks must both match before two samples contribute to a process's CPU delta. New/replaced processes and regressed counters report zero for that interval. Processes that disappear or deny access are counted as skipped; other read failures stop the command. Names and states are quoted to keep embedded newlines and terminal control bytes out of the table layout.

Observations are approximate and do not form an atomic snapshot. RAM and CPU visibility follow the selected proc mount; system RAM is not a cgroup memory limit. Frames are ordinary text, so redirected output stays useful. Interactive cursor navigation, process signaling, command-line capture, and terminal widgets are outside this example.
