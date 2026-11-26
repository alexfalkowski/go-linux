# Procfs design

Part of the [accepted-design index](../accepted-design.md).

## Own the caller contract while reusing readers

The package keeps callers on `go-linux/procfs` and delegates ordinary process/system statistics, descriptor data, enumeration, and mountinfo parsing to [Prometheus procfs](https://github.com/prometheus/procfs/tree/v0.23.0). Its existing Linux format knowledge avoids maintaining another general proc parser library. Pure Go and the dependency's Go floor fit this module without adding a monitoring registry, server, or cgo requirement.

Private upstream values and explicit local operations bound the supported surface. Type aliases or embedding FS/Proc would expose upstream's complete method sets and make its evolution part of this library's compatibility contract. Local result types permit selected fields, stable units, checked numeric conversion, and error conventions without exporting dependency types. The cost is deliberate mapping code and continued responsibility to verify dependency upgrades. Upstream's pre-v1 compatibility warning does not become permission to break this package.

## Keep observation distinct from ownership

An FS or Process contains a path-based view and no persistent descriptor. An artificial Close would imply a lifetime or stable identity that these readers do not possess. Resolving the supplied root to an absolute path avoids later working-directory changes without claiming confinement or pinning its inode. PID values belong to the proc mount's namespace; resolving its self link is more appropriate than substituting os.Getpid for an alternate root.

Pinning a process directory could prevent operations from switching to a reused PID, but the upstream reader API accepts filesystem paths rather than borrowed directory handles. Wrapping it with pidfds would not repair subsequent numeric-PID lookups. The initial API therefore documents the actual race instead of promising stable identity. [Linux 7.0 procfs documentation](https://github.com/torvalds/linux/blob/v7.0/Documentation/filesystems/proc.rst) supplies the baseline formats and directory-lifetime distinction.

Descriptor enumeration and individual inspection are separate operations because the dependency's aggregate descriptor-info reader silently skips failed entries. Returning listed numbers leaves decisions about disappearance and access denial with the caller, including enumeration's own temporary descriptor in self inspection. Numbers and symlink text are observations, never owned descriptor accessors.

## Translate only what can be preserved

The wrapper retains native process ticks/pages and converts meminfo's selected quantities to checked byte counts. CPU aggregates remain cumulative floating-point seconds: converting very large multi-CPU totals to nanosecond durations would introduce avoidable range limits. Kernel flags retain unknown bits and process states retain unknown characters. Optional mount propagation fields and facility-specific fdinfo extensions are omitted altogether rather than presenting the dependency's filtered subset as a complete record.

Upstream process-stat parsing can panic on malformed delimiters. A small private adapter confines dependency parser calls and turns those failures into a local path-labelled parse error. Native filesystem path errors pass through with errno intact. Returned results are discarded on failure, and dependency types do not escape through wrapped parse errors. This adapter is a data-parsing boundary, not panic recovery around the rest of the package.

The maps reader is the narrow local parsing exception: upstream v0.23.0 normalizes pathname whitespace and fails to report scanner errors. Parsing the fixed leading fields while retaining the pathname remainder preserves Linux names and supports reliable failure reporting without creating a second general parser framework. It retains textual newline escapes because procfs cannot distinguish them from literal backslash sequences. Prefer an upstream implementation if a future public reader preserves these contracts.

Mountinfo uses upstream's full-file method rather than its capped process method; the wrapper decodes pathname escapes once. The remaining upstream readers retain a 1-MiB input cap and permissive handling of some missing fields. The usage guide states those limits instead of claiming exhaustive fixture validation or complete observations for arbitrary-size system-stat sources. The selected meminfo/fdinfo fields receive explicit presence/range checks because their public contract distinguishes missing values from zero.
