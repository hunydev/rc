# rc — Remote Control

A lightweight Go server that runs **any CLI command** in a pseudo-terminal (PTY) and streams it to a web browser in real-time via WebSocket. Close the browser, come back later — output is preserved and the process keeps running.

## Why

- **Remote access** — Control CLI tools (AI agents, build systems, REPLs) from any device with a browser.
- **Session persistence** — Process survives browser disconnection. Reconnect and see full history.
- **Mobile friendly** — Floating helper keyboard for touch devices (arrow keys, Ctrl combos, Tab, Esc).
- **Restart on exit** — When the command finishes, a restart button appears. One click to rerun.

## Architecture

```
Browser (xterm.js)
    ↕ WebSocket (JSON)
Go HTTP Server (:8000)
    ↕ PTY (pseudo-terminal)
Any CLI Command
```

### Components

| File | Role |
|------|------|
| `main.go` | HTTP server, routing, signal handling, startup |
| `hub.go` | WebSocket hub — manages client connections, broadcasts PTY output, handles input/resize/restart messages |
| `pty_manager.go` | PTY lifecycle — spawns command, reads output, writes input, resize, restart |
| `output_buffer.go` | Ring buffer (default 10 MB) — stores output for session replay on reconnect |
| `static/index.html` | Single-page frontend — xterm.js terminal, WebSocket client, restart overlay, mobile helper |
| `service.sh` | systemd service management — install/uninstall/start/stop/build (stop→build→restart) |

### WebSocket Protocol

All messages are JSON with `{ type, data?, cols?, rows? }`.

| Direction | Type | Description |
|-----------|------|-------------|
| Client → Server | `input` | Keyboard input (`data`: string) |
| Client → Server | `resize` | Terminal size change (`cols`, `rows`: uint16) |
| Client → Server | `restart` | Restart the PTY command |
| Server → Client | `output` | Terminal output (`data`: string) |
| Server → Client | `status` | Process status (`data`: `"running"` / `"exited"` / `"restarted"`) |
| Server → Client | `error` | Error message (`data`: string) |

### Data Flow

1. **Startup**: Server spawns command in PTY → `readLoop` goroutine reads PTY output → sends to `outputCh` channel → `Hub.StartOutputPump` broadcasts to all WebSocket clients + stores in `OutputBuffer`.
2. **New client**: Connects → receives `OutputBuffer.Snapshot()` (full history) + current `status`.
3. **Input**: Browser keypress → `input` message → `Hub.readPump` → `PTYManager.Write` → PTY stdin.
4. **Resize**: Browser window resize → `resize` message → `PTYManager.Resize` → `pty.Setsize`.
5. **Process exit**: `readLoop` closes `outputCh` → `StartOutputPump` broadcasts `"exited"` status → browser shows restart button.
6. **Restart**: Browser sends `restart` → `PTYManager.Restart()` kills old process, spawns new one, resets buffer → broadcasts `"restarted"` → browser clears terminal.

## Quick Start

```bash
# Build
go build -o rc .

# Run (defaults to 'copilot' if available, otherwise 'bash')
./rc

# Run with specific command
./rc --command "python3 -i"

# Custom port
./rc --port 9000 --command "htop"
```

Open `http://localhost:8000` in your browser.

## CLI Options

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8000` | HTTP server port |
| `--command` | `copilot` or `bash` | Command to run in PTY (supports arguments, e.g. `"copilot --yolo"`) |
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

- **xterm.js** terminal with Catppuccin Mocha theme, 50K scrollback
- **Session replay** — reconnecting replays all buffered output
- **Restart overlay** — appears when command exits; click to restart
- **Disconnect overlay** — appears on WebSocket disconnect; auto-reconnects in 3s
- **Floating helper button** (mobile/touch) — bottom-right ⌨️ button opens panel:
  - Arrow keys: ← ↑ ↓ →
  - Special: Tab, Esc, Enter, Space
  - Ctrl+C (interrupt)
  - Ctrl toggle (activate → type a letter → sends Ctrl+letter)

## Dependencies

- [creack/pty](https://github.com/creack/pty) — PTY management
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket server
- [xterm.js](https://xtermjs.org/) — Browser terminal emulator (loaded via CDN)

## License

MIT
