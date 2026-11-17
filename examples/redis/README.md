# 🗃️ Redis-inspired cache

A small in-memory cache with a bounded [RESP2](https://redis.io/docs/latest/develop/reference/protocol-spec/) command subset. Standard `net` supplies TCP; [timerfd](../../docs/timerfd.md) sweeps expired entries, [mmap](../../docs/mmap.md) loads snapshots, and [file.Rename](../../docs/file.md) replaces them atomically. No Redis client/server dependency is added.

Start the server:

```sh
make example-redis
```

Use the bundled client in another terminal:

```sh
make example-redis-cli
make example-redis-cli args='["SET","hello","hello world"]'
make example-redis-cli args='["GET","hello"]'
make example-redis-cli args='["SET","temporary","value","EX","5"]'
make example-redis-cli args='["TTL","temporary"]'
make example-redis-cli args='["EXPIRE","hello","10"]'
make example-redis-cli args='["DEL","hello"]'
```

The first command prints `PONG`; `SET` prints `OK`; `GET hello` prints `hello world`. The default address is `127.0.0.1:6380`. Use `-addr` before the command in client arguments to choose another address. An installed `redis-cli -h 127.0.0.1 -p 6380 GET hello` also works; clients that require other Redis commands or RESP3 are unsupported.

## 💾 Explicit snapshots

Start with a trusted snapshot path whose parent directory already exists:

```sh
make example-redis args='["-snapshot","/tmp/go-linux-cache.json"]'
```

Then write and save:

```sh
make example-redis-cli args='["SET","hello","saved value"]'
make example-redis-cli args='["SAVE"]'
```

Stop and restart with the same server arguments. `GET hello` returns `saved value`. Startup reads a nonempty snapshot through a read-only mapping, decodes owned values, and unmaps before accepting clients. A missing file starts an empty cache; an invalid snapshot stops startup. Do not truncate or modify a snapshot in place while it is being mapped. This server's SAVE operation replaces the file instead of truncating it.

SAVE serializes the cache, writes a mode-0600 temporary file in the same directory, syncs and closes it, atomically renames it over the destination, then syncs the directory. A failure after rename can mean the new snapshot is visible despite an error. Saving blocks other cache commands until it finishes. There is no automatic save on shutdown or append-only log. The version-1 JSON format uses base64 keys/values and absolute Unix-millisecond expiry times; it is specific to this example and is not a Redis RDB file.

## 🔧 Supported commands and limits

| Command | Behavior |
| --- | --- |
| `PING [message]` | `PONG`, or the supplied bulk string |
| `SET key value [EX seconds]` | Replace a binary-safe value; clear old expiry unless EX is supplied |
| `GET key` | Bulk value, or a null response displayed as `(nil)` |
| `DEL key [key ...]` | Number of existing keys removed |
| `EXPIRE key seconds` | Set expiry; a nonpositive value deletes immediately; return 1 if present, otherwise 0 |
| `TTL key` | Remaining seconds rounded to the nearest second; -1 for no expiry, -2 for absent |
| `SAVE` | Persist explicitly when `-snapshot` is configured |
| `QUIT` | Reply `OK` and close this connection |

Commands are case-insensitive. The server accepts arrays of bulk strings, including pipelined requests; inline commands, RESP3, authentication, transactions, replication, collections, and eviction are omitted. Unsupported commands/options return an error. Malformed or oversized frames close their connection after an error response.

Limits are 1,024 keys, 1,024 bytes per key, 64 KiB per bulk argument/value, 128 KiB of argument bytes per request, 16 arguments, and 8 MiB total key/value bytes. Capacity errors preserve an existing value. Up to 32 concurrent clients are accepted; excess connections close immediately. Connections have a 30-second per-command I/O deadline. Snapshot inputs are limited to 16 MiB. Positive expiry is limited to one year (31,536,000 seconds).

Expiry uses wall-clock timestamps so time continues across restarts. Requests remove expired entries before execution, and a one-second boottime timer performs background sweeps. Kernel expiration counts are consumed as counts, with one reconciliation sweep sufficient after missed intervals. Wall-clock changes affect absolute key expiry. The server cancels clients and joins the sweep on shutdown.

The server defaults to loopback and has no authentication or encryption. Keep it local for demonstrations. Snapshot durability and the small protocol subset are educational; this is not a substitute for Redis.
