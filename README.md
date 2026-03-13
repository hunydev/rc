# rc — Remote Control

A lightweight server that runs **any CLI command** in a pseudo-terminal (PTY) and streams it to a web browser in real-time via WebSocket. Close the browser, come back later — output is preserved and the process keeps running.

## Why

- **Remote access** — Control CLI tools (AI agents, build systems, REPLs) from any device with a browser.
- **Multi-command tabs** — Run multiple commands in one session with `-c` flag. Switch between them with browser tabs.
- **Distributed terminals** — Attach remote servers to a central hub with `--attach`. Monitor everything from one browser.
- **Session persistence** — Process survives browser disconnection. Reconnect and see full history.
- **Mobile friendly** — Floating helper keyboard for touch devices (arrow keys, Ctrl combos, Tab, Esc).
- **Restart on exit** — When the command finishes, a restart button appears. One click to rerun.

## Architecture

```
Browser (xterm.js + Tab UI)
    ↕ WebSocket /ws (JSON, tab-aware)
Hub Server (:8000)
    ├─ PTY × N (local commands)
    ├─ HTTP API (/info, /health)
    └─ WebSocket /attach (agent protocol)
          ↕
Agent on Server B ──── PTY × M (remote commands)
Agent on Server C ──── PTY × K (remote commands)
```

### Components

| File | Role |
|------|------|
| `main.go` | Cobra CLI, HTTP server, routing, auth middleware, `-c` multi-command, `-a` agent mode, `-d` daemon |
| `hub.go` | WebSocket hub — browser clients, local PTYs, remote agents, dynamic tab management |
| `auth.go` | Bearer token authentication for HTTP and WebSocket endpoints |
| `agent.go` | Agent mode — spawns local PTYs, connects to remote hub (with auth), streams I/O, auto-reconnect |
| `pty_manager.go` | PTY lifecycle — spawns command, reads output, writes input, resize, restart |
| `output_buffer.go` | Ring buffer (configurable, default 10 MB) — stores output for session replay on reconnect |
| `static/index.html` | Single-page frontend — xterm.js terminals, tab bar, WebSocket client, mobile helper |
| `service.sh` | systemd service management — install/uninstall/start/stop/build (stop→build→restart) |

### WebSocket Protocol

All messages are JSON with `{ type, data?, cols?, rows?, tab, tabs?, remote? }`.

**Browser ↔ Hub:**

| Direction | Type | Description |
|-----------|------|-------------|
| Server → Client | `tabs` | Tab list on connect (`tabs`: array of `{name, remote}`) |
| Server → Client | `tab_added` | New remote tab added dynamically (`data`: name, `tab`: index, `remote`: true) |
| Client → Server | `input` | Keyboard input (`data`: string, `tab`: int) |
| Client → Server | `resize` | Terminal size change (`cols`, `rows`: uint16, `tab`: int) |
| Client → Server | `restart` | Restart the PTY command (`tab`: int) |
| Server → Client | `output` | Terminal output (`data`: string, `tab`: int) |
| Server → Client | `status` | Process status (`data`: `"running"` / `"exited"` / `"restarted"` / `"disconnected"`, `tab`: int) |
| Server → Client | `error` | Error message (`data`: string, `tab`: int) |

**Agent → Hub:**

| Direction | Type | Description |
|-----------|------|-------------|
| Agent → Hub | `register` | Registration with tab list (`tabs`: array of `{name}`) |
| Agent → Hub | `output` | PTY output from agent command (`data`: string, `tab`: agent-relative index) |
| Agent → Hub | `status` | Status update (`data`: `"running"` / `"exited"` / `"restarted"`, `tab`: agent-relative index) |
| Hub → Agent | `input` | Forwarded keyboard input (`data`: string, `tab`: agent-relative index) |
| Hub → Agent | `resize` | Forwarded terminal resize (`cols`, `rows`: uint16, `tab`: agent-relative index) |
| Hub → Agent | `restart` | Forwarded restart command (`tab`: agent-relative index) |

### HTTP API

| Endpoint | Description |
|----------|-------------|
| `GET /` | Web UI (embedded static files) |
| `GET /info` | JSON: `{hostname, workspace, commands}` |
| `GET /health` | JSON: `{status, tabs, running}` — tab count and running process count |
| `WS /ws` | Browser WebSocket endpoint |
| `WS /attach` | Agent WebSocket endpoint |

## Quick Start

```bash
# Build
go build -o rc .

# Run (defaults to bash)
./rc

# Multiple commands with tabs
./rc -c "htop" -c "bash" -c "python3 -i"

# Custom port and bind address
./rc -p 9000 --bind 127.0.0.1 -c "bash"

# Password-protected server
./rc --password mysecret -c "bash"

# Run as background daemon (logs to /tmp/rc-<pid>.log)
./rc -d -c "bash"
```

Open `http://localhost:8000` in your browser.

### Password Authentication

When `--password` is set, all API and WebSocket endpoints require a Bearer token. The frontend shows a login page automatically.

```bash
# Server with password
./rc --password mysecret -c "bash"

# Password via environment variable (recommended, avoids ps visibility)
RC_PASSWORD=mysecret ./rc -c "bash"
```

### Remote Attach (Agent Mode)

Run commands on server B and monitor them from server A's browser:

```bash
# Server A (hub) — running normally
./rc -c "bash"

# Server B (agent) — attach to server A
./rc -a serverA:8000 -c "htop" -c "tail -f /var/log/syslog"

# With password-protected hub
./rc -a serverA:8000 --password mysecret -c "htop"
```

Server B spawns the commands locally and streams them to server A. The browser on server A automatically gets new tabs for server B's commands (`serverB: htop`, `serverB: tail -f ...`). You can type, resize, and restart — all routed back to server B.

Multiple agents can attach to the same hub simultaneously. If an agent disconnects, its tabs show a "disconnected" status and reconnect automatically (retry every 3 seconds).

The scheme is auto-detected: `wss://` for port 443, `ws://` otherwise. You can also use explicit URLs (`ws://`, `wss://`, `http://`, `https://`).

## CLI Options

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8000` | HTTP server port |
| `--command` | `-c` | `bash` | Command to run (repeatable for multi-tab, e.g. `-c "bash" -c "htop"`) |
| `--attach` | `-a` | — | Attach to a remote hub (e.g. `-a serverA:8000`). Runs in agent mode. |
| `--password` | | — | Password for server access (Bearer token). Env: `RC_PASSWORD` |
| `--daemon` | `-d` | `false` | Run as background daemon (logs to `/tmp/rc-<pid>.log`) |
| `--bind` | | `0.0.0.0` | Bind address (use `127.0.0.1` for local-only access) |
| `--buffer-size` | | `10` | Output buffer size in MB |
| `--cols` | | `120` | Initial terminal columns |
| `--rows` | | `30` | Initial terminal rows |
| `--version` | `-v` | | Print version and exit |

## systemd Service

`service.sh` manages rc as a systemd service with zero-downtime rebuilds.

```bash
# Register as systemd service
./service.sh install

# Start / stop / restart
./service.sh start
./service.sh stop
./service.sh restart

# Status with health check
./service.sh status

# Rebuild: stops service → go build → restarts service
./service.sh build

# View logs
./service.sh logs 100
./service.sh logs-follow

# Remove service entirely
./service.sh uninstall
```

Environment variables for `service.sh install`:

| Variable | Default | Description |
|----------|---------|-------------|
| `RC_PORT` | `8000` | Server port |
| `RC_COMMAND` | `bash` | Command to run |
| `RC_COLS` | `120` | Terminal columns |
| `RC_ROWS` | `30` | Terminal rows |
| `RC_BIND` | `0.0.0.0` | Bind address |
| `RC_PASSWORD` | — | Access password (set via env to avoid ps visibility) |

Example: `RC_PORT=9000 RC_PASSWORD=secret ./service.sh install`

## Frontend Features

- **Tab bar** — Multiple commands shown as tabs; click to switch. Hidden with single tab.
  - 🟢 Green dot — running
  - 🔴 Red dot — exited
  - 🟡 Yellow pulsing dot — awaiting input (idle for 3+ seconds)
  - ⚫ Gray dot — agent disconnected
  - **REMOTE** badge — italic purple styling for remote agent tabs
- **xterm.js** terminal with Catppuccin Mocha theme, 50K scrollback
- **Session replay** — reconnecting replays all buffered output per tab
- **Login page** — automatic login overlay when password is set; token stored in session
- **Header** — Shows logo, hostname, working directory, and command list
- **Restart bar** — appears when active tab's command exits; click to restart
- **Disconnect overlay** — appears on WebSocket disconnect; auto-reconnects in 3s
- **Floating helper button** (mobile/touch) — bottom-right button opens panel:
  - Arrow keys
  - Special: Tab, Esc, Enter, Space
  - Ctrl+C (interrupt)
  - Ctrl toggle (activate, type a letter, sends Ctrl+letter)

## Releases

Pre-built binaries are available on the [Releases](https://github.com/hunydev/rc/releases) page for:

- Linux (amd64, arm64)
- macOS (amd64, arm64)

## Dependencies

- [spf13/cobra](https://github.com/spf13/cobra) — CLI framework
- [creack/pty](https://github.com/creack/pty) — PTY management
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket server
- [xterm.js](https://xtermjs.org/) — Browser terminal emulator (loaded via CDN)

## License

MIT
