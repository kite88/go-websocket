[简体中文](README.md) · English

# go-websocket · Targeted WebSocket Chat Service

[![Release](https://img.shields.io/github/v/release/kite88/go-websocket)](https://github.com/kite88/go-websocket/releases/latest)
[![License](https://img.shields.io/github/license/kite88/go-websocket)](LICENSE)

A small service written in Go + Gin: a browser connects with a uid, and the server
**delivers each message only to the user named in `receiver`** (it never broadcasts).
The page template, the static assets and the config templates are all embedded into the
binary with `//go:embed`, so **`go run .` right after cloning is enough** — nothing
external is needed at runtime.

The point is to be a **reading-and-tinkering codebase**: the layering, the concurrency
model, the config loading and the tests are all small enough to read top to bottom and
change directly, with no extra build or runtime dependencies.

## Features

- **Targeted delivery**: a message goes only to the uid in `receiver`, never broadcast;
  if the receiver is offline the sender gets an explicit error back.
- **Unforgeable identity**: `sender` is filled in by the server from the TCP connection —
  whatever the client sends is ignored.
- **uid validation**: letters, digits, underscores and hyphens only, 1–64 characters. An
  invalid uid is rejected with `400` plus a JSON reason **before the upgrade handshake**,
  instead of leaving the frontend with a handshake that fails for no stated reason.
- **Concurrency-safe**: the hub keeps the uid → connection map inside a **single
  goroutine** (no mutex on the map), and every write to a connection is funnelled into
  that connection's one and only write loop, so a WebSocket is never written concurrently.
- **Heartbeat and timeouts**: the server pings on a fixed period and drops the connection
  when no pong (or no read) arrives, so dead connections do not pile up.
- **Slow-client protection**: every connection has its own outbound queue; once it fills
  up the peer is considered gone and gets dropped, so one slow client can neither stall
  the hub nor stop other people from receiving their messages.
- **Duplicate uid takeover**: connecting again with the same uid replaces the old
  connection, so messages never end up on a connection nobody is watching.
- **Graceful shutdown**: `Ctrl+C` / `SIGTERM` stops accepting connections first, then
  closes every WebSocket before exiting.
- **Online list**: `GET /api/online` returns the uids currently online (sorted).
- **Frontend niceties**: light / follow-system / dark themes (applied on the first frame,
  so no dark-mode flash), exponential-backoff auto-reconnect, click an online user to
  pick them as the receiver, and the uid is stored locally (a refresh keeps your identity).
- **Location-aware address**: the frontend builds `ws://` or `wss://` from `location`, so
  changing the port or host, or moving to https, needs no code change.

## Project Layout

```text
.
├── main.go                 Entry point: wires config, router, graceful shutdown
├── config/                 Config loading (embedded env.ini.<env> templates + external env.ini override)
├── common/                 Utilities and protocol: uid validation, message codec, log clipping
├── handle/                 The implementation: hub (hub.go) + WebSocket I/O (handle.go)
├── router/                 Route assembly: pages, static assets, WebSocket, API group
├── test/                   In-process router tests + end-to-end WebSocket tests
├── web/
│   ├── view/index.html     Page template (embedded)
│   └── static/             Bootstrap / jQuery / frontend scripts / icons (embedded)
└── go.mod / go.sum         Dependencies: gin, gorilla/websocket, ini
```

## Quick Start

Requires Go 1.23 or later. If you would rather not install Go, grab the archive for your
platform from [Releases](https://github.com/kite88/go-websocket/releases/latest):
`.tar.gz` for Linux / macOS, `.zip` for Windows. Unpacking gives a ready-to-run directory
(the executable, a `start` launcher, `env.ini`, `LICENSE`) — double-click `start.bat` on
Windows, run `./start.sh` elsewhere, or just run the executable directly. The page and the
static assets are embedded in the executable.

```bash
# Run directly (recommended: rerun after every edit, no files left behind)
go run .

# Or build first
go build -o go-websocket . && ./go-websocket
```

On startup it prints the version, the config source and the accessible URLs:

```text
2026-10-01 13:43:43 go-websocket dev (release)
Config source: embedded env.ini.release
Local access: http://127.0.0.1:8090
LAN access: http://192.168.1.10:8090
WebSocket shares the port above, path /ws?uid=<your-uid>
```

Open two browser windows (or one normal window plus one private window), note each one's
uid, type the other's uid, and start chatting — or simply click the other user under
"Online users" on the left.

### Command-line flags

| Flag | Default | Description |
| --- | --- | --- |
| `-config` | empty | Path to an external config file (ini); when empty, the priority list below is searched |
| `-version` | `false` | Print the version and exit |

### Configuration

The config file is INI (`config/env.ini.<env>` are the templates kept in the repo):

| Key | Default | Description |
| --- | --- | --- |
| `env_mode` | `release` | Run mode: `debug` / `release` / `test`; anything else is treated as `release` |
| `server.protocol` | `http` | Only used to build the address in the startup log; it does not change how the server listens |
| `server.http_port` | `8090` | Listening port |
| `server.shutdown_timeout` | `10s` | Longest time to wait for a graceful shutdown |
| `websocket.read_buffer_size` | `1024` | Read buffer size per connection (bytes) |
| `websocket.write_buffer_size` | `1024` | Write buffer size per connection (bytes) |
| `websocket.max_message_size` | `4096` | Maximum size of a single message (bytes); larger ones are rejected |
| `websocket.ping_period` | `30s` | How often the server sends a ping; `0` disables the heartbeat |
| `websocket.pong_wait` | `60s` | How long to wait for a pong (also used as the read deadline); `0` means no limit |
| `websocket.write_wait` | `10s` | Write timeout; `0` means no limit |
| `websocket.send_queue` | `64` | Per-connection outbound queue length; a full queue drops the connection |
| `websocket.allow_all_origins` | `false` | Allow connections from any origin (cross-site); by default only same-origin is allowed |

Config sources, highest priority first:

1. the file given by `-config` / the `GWS_CONFIG` environment variable (it must exist);
2. `env.ini` in the working directory;
3. `config/env.ini` in the working directory;
4. the embedded `env.ini.<GWS_ENV>` (`GWS_ENV` defaults to `release`);
5. the embedded `env.ini.release`.

Four ways to change the config locally — pick whichever you like:

- **① Copy a template**: copy it to `config/env.ini`; that file is already in `.gitignore`;
- **② Point `-config` at it**: aim straight at a template file and leave the repo untouched;
- **③ Pick an embedded template with `GWS_ENV`**: `debug` / `local` / `release` / `test`;
- **④ Use the `GWS_CONFIG` environment variable**: equivalent to `-config`.

```powershell
Copy-Item config\env.ini.local config\env.ini      # ①
go run . -config config\env.ini.local              # ②
$env:GWS_ENV = 'local'; go run .                   # ③
```

> The pages and assets under `web/` are compiled into the binary by `//go:embed`.
> **After editing them you must rerun `go run .` or restart the process** — reloading the
> browser alone will not pick up the change.

## API

| Method and path | Parameters | Description |
| --- | --- | --- |
| `GET /` | - | The chat page |
| `GET /ws` | `uid` (required) | Upgrade to a WebSocket connection; an invalid uid returns `400` |
| `GET /api/online` | - | Online users, `{"count":N,"users":["alice","bob"]}` |

WebSocket messages (JSON):

Upstream (client → server) — only these two fields matter, `sender` is overwritten by
the server:

```json
{ "receiver": "bob", "content": "你好" }
```

Downstream (server → receiver):

```json
{ "type": "chat", "sender": "alice", "receiver": "bob", "content": "你好", "time": "2026-10-01 13:43:43" }
```

A notice sent to the client that **caused** it (receiver offline, malformed message, …):

```json
{ "type": "error", "content": "接收人 nobody 不在线", "time": "2026-10-01 13:43:43" }
```

## Implementation Notes

- **One goroutine owns the hub map**: `Hub.clients` is only ever mutated inside `run()`.
  Everything else goes through channels (`register` / `unregister` / `deliver` / `online`),
  so the map needs no lock.
- **One writer per connection**: gorilla/websocket does not support concurrent writes to
  the same connection. Each connection has its own `send` queue, only `writePump` writes to
  the socket, and every other goroutine just posts to the queue.
- **No "close the channel" signalling**: once `send` is closed, any further post panics —
  and the poster may be the hub or the read loop, so the ordering cannot be guaranteed.
  `sync.Once` + `close(done)` is used instead, which can never panic a poster; the worst
  case is one wasted post.
- **Slow clients are dropped outright**: a full `send` queue means the peer has stopped
  reading, and piling messages up would only hurt everyone else. The connection is dropped
  and the sender is told why — a deliberate trade-off.
- **Failed delivery talks back**: an offline receiver, or a full send queue, both produce a
  `type=error` message to the sender.
- **uid is validated up front**: validation happens before `Upgrade`, so the failure is a
  `400` plus a JSON reason. Reporting an error after the upgrade would only leave a close
  frame, which tells the frontend nothing.
- **`sender` is overwritten by the server**: it is taken from the connection identity only;
  a client-supplied `sender` is discarded so nobody can impersonate someone else.
- **Read limit**: `SetReadLimit` caps the size of a single message — otherwise one client
  could eat all the memory with an oversized frame.
- **Heartbeat via gorilla's ping/pong**: the server pings periodically and refreshes the
  read deadline in `SetPongHandler`, so a dead connection is cleaned up within `pong_wait`
  at worst.
- **Same-origin check**: only same-origin connections are allowed by default (the page is
  served by this very service, so same-origin is a given). Clients without an `Origin`
  header are let through — that is a debugging tool, not a browser cross-site scenario.
  Set `allow_all_origins` to loosen this.
- **`Recovery` is always on**: `gin.New()` ships with neither Logger nor Recovery, and any
  panic inside a handler would take the process down. `Recovery` is attached explicitly,
  with `Logger` added in non-release modes.
- **Four embedded env templates**: this avoids the classic "there is no `env.ini` in the
  repo, so a fresh clone fails with `no matching files found`" problem; an external
  `env.ini` can still override them.
- **Config access has defaults and never panics**: an invalid `env_mode` falls back to
  `release` (otherwise `gin.SetMode` panics outright), and `GetInt` / `GetBool` /
  `GetDuration` return the default when a key is missing or unparsable.
- **Only the read header timeout is set on HTTP**: WebSockets are long-lived, so a
  `ReadTimeout` / `WriteTimeout` would cut normal sessions and heartbeats alike.
- **Graceful shutdown takes two steps**: `srv.Shutdown` stops accepting new connections
  first, then `h.Close()` disconnects the WebSockets still attached — upgraded connections
  are outside `http.Server`'s bookkeeping, so relying on `Shutdown` alone would miss them.
- **The frontend never builds HTML strings**: all user-controlled text is written into the
  DOM through jQuery's `.text()`. Both the uid and the message body are fully controlled by
  the peer, and pasting them into `innerHTML` is a ready-made XSS.
- **The frontend address follows the page**: `ws(s)://` is built from `location`, so a
  different port, a different host or https all work without touching the code.
- **Dependencies are injected explicitly**: `handle.New(cfg)` takes a config struct, so a
  test can pass an aggressive "heartbeat: one hour" setup without editing a config file;
  `router.R(...)` takes an `fs.FS`, so tests just use `os.DirFS("..")`.

## Known Limitations

1. **No authentication**: anyone who knows a uid can connect, and uids are not reserved
   (a second connection with the same uid simply takes over). Fine on localhost or a LAN.
2. **No persistence**: messages are only forwarded — nothing is replayed when you come back
   online, and no history is stored.
3. **No HTTPS / WSS**: `server.protocol` only affects the address printed in the startup
   log; real TLS has to be terminated by a reverse proxy such as Nginx or Caddy.
4. **Single process**: the hub lives in memory, so instances cannot see each other's uids.
   Horizontal scaling would need cross-process delivery such as Redis.
5. **No rooms or broadcast**: point-to-point only — no groups, no chat rooms, no offline
   messages.
6. **The online list is polled**: the frontend fetches `/api/online` every 5 seconds rather
   than being notified.
7. **The frontend is still jQuery plus a server-side template**; there is no
   frontend/backend split and no asset build pipeline.

## Development

```bash
go vet ./...    # static checks
go test ./...   # unit tests + in-process router tests + end-to-end WebSocket tests
```

Test coverage: the index page and static assets, `/api/online`, 404s, targeted message
forwarding, `sender` being unforgeable, the offline-receiver notice, three kinds of
malformed upstream messages, invalid uids being rejected with 400, duplicate-uid takeover,
and reconnecting after a disconnect.

> The cases in `test/ws_test.go` need a real listening port: a WebSocket handshake requires
> Hijack, which `httptest.NewRecorder` cannot do. They use `httptest.NewServer`, so no
> fixed port is occupied.
>
> Running the race detector needs cgo (and a local gcc): `CGO_ENABLED=1 go test -race ./...`.

## Releasing

Release artifacts are produced by `build.sh`: one archive per platform in `dist/`, plus a
`checksums.txt` with the SHA256 of every archive.

```bash
./build.sh              # version comes from git describe --tags
./build.sh -v v1.1.0    # or pass it explicitly
```

Each archive unpacks to a top-level `go-websocket-<os>-<arch>/` holding the executable,
`start.sh` (or `start.bat` on Windows), `env.ini` and `LICENSE`. Windows gets a `.zip`, the
other platforms get a `.tar.gz` whose executable and launcher carry the `0755` bit.

The script needs bash (Linux / macOS, or Git Bash / WSL on Windows; Git Bash has no `zip`,
so the script falls back to PowerShell's `Compress-Archive`). The version is injected into
`main.version`, so `go-websocket -version` reports it.

Publishing is just a tag: `.github/workflows/release.yml` reruns the same steps
(`go vet` → `go test` → `build.sh` → verify the artifacts), creates the Release and uploads
the artifacts.

```bash
git tag v1.1.0 && git push origin v1.1.0
```

A tag with a suffix (such as `v1.1.0-rc1`) is published as a prerelease and does not take
over Latest; if the tag already has a Release, its notes are updated and the artifacts
overwritten, so re-tagging is a safe way to republish. To write custom release notes, put
them in `docs/release-notes/<tag>.md`.

## License

[MIT](LICENSE) © 2026 kite88
