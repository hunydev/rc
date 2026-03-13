# rc — Remote Control

A lightweight server that runs **any CLI command** in a pseudo-terminal (PTY) and streams it to a web browser in real-time via WebSocket. Close the browser, come back later — output is preserved and the process keeps running.

## Why

- **Remote access** — Control CLI tools (AI agents, build systems, REPLs) from any device with a browser.
- **Multi-command tabs** — Run multiple commands in one session with `-c` flag. Switch between them with browser tabs.
- **Session persistence** — Process survives browser disconnection. Reconnect and see full history.
- **Mobile friendly** — Floating helper keyboard for touch devices (arrow keys, Ctrl combos, Tab, Esc).
- **Restart on exit** — When the command finishes, a restart button appears. One click to rerun.

## Architecture

```
Browser (xterm.js + Tab UI)
    ↕ WebSocket (JSON, tab-aware)
HTTP Server (:8000)
    ↕ PTY × N (one per command)
Command 1, Command 2, ...
```

### Components

| File | Role |
|------|------|
| `main.go` | HTTP server, routing, `-c` multi-command flag, signal handling |
| `hub.go` | WebSocket hub — manages clients, routes messages per tab, broadcasts PTY output |
| `pty_manager.go` | PTY lifecycle — spawns command, reads output, writes input, resize, restart |
| `output_buffer.go` | Ring buffer (default 10 MB) — stores output for session replay on reconnect |
| `static/index.html` | Single-page frontend — xterm.js terminals, tab bar, WebSocket client, mobile helper |
| `service.sh` | systemd service management — install/uninstall/start/stop/build (stop→build→restart) |

### WebSocket Protocol

All messages are JSON with `{ type, data?, cols?, rows?, tab }`.

| Direction | Type | Description |
|-----------|------|-------------|
| Server → Client | `tabs` | Tab list sent on connect (`tabs`: string array of command names) |
| Client → Server | `input` | Keyboard input (`data`: string, `tab`: int) |
| Client → Server | `resize` | Terminal size change (`cols`, `rows`: uint16, `tab`: int) |
| Client → Server | `restart` | Restart the PTY command (`tab`: int) |
| Server → Client | `output` | Terminal output (`data`: string, `tab`: int) |
| Server → Client | `status` | Process status (`data`: `"running"` / `"exited"` / `"restarted"`, `tab`: int) |
| Server → Client | `error` | Error message (`data`: string, `tab`: int) |

## Quick Start

```bash
# Build
go build -o rc .

# Run (defaults to 'copilot' if available, otherwise 'bash')
./rc

# Run with specific command (legacy flag, still works)
./rc --command "python3 -i"

# Multiple commands with tabs
./rc -c "copilot" -c "bash" -c "htop"

# Custom port
./rc --port 9000 -c "copilot --yolo" -c "bash"
```

Open `http://localhost:8000` in your browser.

## CLI Options

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8000` | HTTP server port |
| `-c` | `copilot` or `bash` | Command to run (repeatable for multi-tab, e.g. `-c "bash" -c "htop"`) |
| `--command` | — | Legacy single-command flag (use `-c` instead) |
| `--cols` | `120` | Initial terminal columns |
| `--rows` | `30` | Initial terminal rows |

## systemd Service

`service.sh` manages rc as a systemd service with zero-downtime rebuilds.

```bash
# Register as systemd service
./service.sh install

# Start / stop / restart
./service.sh start
./service.sh stop
./service.sh restart

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
| `RC_COMMAND` | `copilot --yolo` | Command to run |
| `RC_COLS` | `120` | Terminal columns |
| `RC_ROWS` | `30` | Terminal rows |

Example: `RC_PORT=9000 RC_COMMAND="bash" ./service.sh install`

## Frontend Features

- **Tab bar** — Multiple commands shown as tabs; click to switch. Status dot per tab (green=running, red=exited).
- **xterm.js** terminal with Catppuccin Mocha theme, 50K scrollback
- **Session replay** — reconnecting replays all buffered output per tab
- **Restart overlay** — appears when active tab's command exits; click to restart
- **Disconnect overlay** — appears on WebSocket disconnect; auto-reconnects in 3s
- **Floating helper button** (mobile/touch) — bottom-right button opens panel:
  - Arrow keys
  - Special: Tab, Esc, Enter, Space
  - Ctrl+C (interrupt)
  - Ctrl toggle (activate, type a letter, sends Ctrl+letter)

## Dependencies

- [creack/pty](https://github.com/creack/pty) — PTY management
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket server
- [xterm.js](https://xtermjs.org/) — Browser terminal emulator (loaded via CDN)

## License

MIT
