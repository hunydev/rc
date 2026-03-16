<p align="center">
  <img src="logo.svg" width="80" alt="rc" />
</p>

# rc — Remote Control

A lightweight server that runs **any CLI command** in a pseudo-terminal (PTY) and streams it to a web browser in real-time via WebSocket. Close the browser, come back later — output is preserved and the process keeps running.

🌐 **Website**: [rc.huny.dev](https://rc.huny.dev)

## Install

```bash
# macOS / Linux
curl -fsSL https://rc.huny.dev/install.sh | bash

# Windows (PowerShell)
powershell -c "irm https://rc.huny.dev/install.ps1 | iex"
```

Or download pre-built binaries from the [Releases](https://github.com/hunydev/rc/releases) page.

## Why

- **Remote access** — Control CLI tools (AI agents, build systems, REPLs) from any device with a browser.
- **Multi-command tabs** — Run multiple commands in one session with `-c` flag. Switch between them with browser tabs.
- **Split pane** — Send any tab to a right-side split view. Monitor multiple terminals side-by-side.
- **Distributed terminals** — Attach remote servers to a central hub with `--attach`. Monitor everything from one browser.
- **Session persistence** — Process survives browser disconnection. Reconnect and see full history.
- **Mobile friendly** — Floating helper keyboard for touch devices (arrow keys, Ctrl combos, Tab, Esc).
- **File upload** — Upload files to the server's working directory from the browser (opt-in with `--upload`).
- **Restart on exit** — When the command finishes, a restart button appears. One click to rerun.

## Architecture

```
Browser (xterm.js + Tab UI)
    ↕ WebSocket (JSON, tab-aware)
Hub Server (:8000)
    ├─ PTY × N (local commands)
    └─ WebSocket /attach (agent protocol)
          ↕
Agent on Server B ──── PTY × M (remote commands)
Agent on Server C ──── PTY × K (remote commands)
```

## Quick Start

```bash
# Build
go build -o rc .

# Run (defaults to bash)
./rc

# Multiple commands with tabs
./rc -c "htop" -c "bash" -c "python3 -i"

# Custom tab labels
./rc -c "bash" -l "dev" -c "htop" -l "monitor"

# Custom port and bind address
./rc -p 9000 --bind 127.0.0.1 -c "bash"

# Password-protected server
./rc --password mysecret -c "bash"

# Run as background daemon (logs to /tmp/rc-<pid>.log)
./rc -d -c "bash"

# Output-only terminal (no stdin, no restart)
./rc --readonly --no-restart -c "tail -f /var/log/syslog"

# URL route prefix (for reverse proxy / security)
./rc --route /myapp -c "bash"

# Enable file upload to working directory
./rc --upload -c "bash"

# Custom title, working directory, and environment variables
./rc --title "Production" -w /opt/app -e "NODE_ENV=production" -c "npm start"

# HTTPS with TLS certificate
./rc --tls-cert cert.pem --tls-key key.pem -c "bash"

# Override default shell
./rc --shell zsh

# Limit concurrent browser connections
./rc --max-connections 5 -c "bash"

# Log to file instead of stderr
./rc --log /var/log/rc.log -c "bash"

# Auto-shutdown after 30 minutes with no connected clients
./rc --timeout 30m -c "bash"
```

Open `http://localhost:8000` (or `http://localhost:8000/myapp/` with `--route`) in your browser.

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

Multiple agents can attach to the same hub simultaneously. If an agent disconnects, its tabs show a "disconnected" status and reconnect automatically (retry every 3 seconds). Disconnected agent tabs can be closed by clicking the **×** button; when the agent reconnects it creates fresh tabs.

The scheme is auto-detected: `wss://` for port 443, `ws://` otherwise. You can also use explicit URLs (`ws://`, `wss://`, `http://`, `https://`).

### Readonly & No-Restart

These flags work **independently** on hub and agent. A hub with `--readonly` only restricts its own local tabs — agent tabs are unaffected (and vice versa).

```bash
# View-only log monitor (no input, no restart)
./rc --readonly --no-restart -c "tail -f /var/log/syslog"

# Hub allows input, but agent is readonly
./rc -c "bash"                                        # Hub (interactive)
./rc -a hub:8000 --readonly -c "tail -f app.log"      # Agent (view-only)
```

### Route Prefix

Use `--route` to serve rc under a sub-path — useful for reverse proxies or security-by-obscurity.

```bash
# Hub with route prefix
./rc --route /secret/terminal -c "bash"
# → http://localhost:8000/secret/terminal/

# Agent must include the route in the target URL
./rc -a hub:8000/secret/terminal -c "htop"
```

All endpoints (`/ws`, `/attach`, `/info`, `/health`) are prefixed with the route. Static assets (logo, favicon) use relative paths and work automatically.

## CLI Options

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `8000` | HTTP server port |
| `--command` | `-c` | `bash` | Command to run (repeatable for multi-tab, e.g. `-c "bash" -c "htop"`) |
| `--label` | `-l` | — | Tab label (repeatable, paired with `-c`; e.g. `-c "bash" -l "dev"`) |
| `--attach` | `-a` | — | Attach to a remote hub (e.g. `-a serverA:8000`). Runs in agent mode. |
| `--password` | | — | Password for server access (Bearer token). Env: `RC_PASSWORD` |
| `--title` | | — | Custom title displayed in browser header and page title |
| `--working-dir` | `-w` | — | Working directory for PTY processes (default: current directory) |
| `--env` | `-e` | — | Environment variable for PTY processes (repeatable, e.g. `-e KEY=VALUE`) |
| `--no-restart` | | `false` | Disable command restart after exit (no restart bar shown) |
| `--readonly` | | `false` | Disable stdin input (output only, view-only terminals) |
| `--route` | | — | URL route prefix (e.g. `--route /myapp` → all endpoints under `/myapp/`) |
| `--upload` | | `false` | Enable file upload to working directory (single file, no overwrite) |
| `--tls-cert` | | — | TLS certificate file path (enables HTTPS; requires `--tls-key`) |
| `--tls-key` | | — | TLS private key file path (requires `--tls-cert`) |
| `--shell` | | — | Default shell when no `-c` is given (default: `bash` on Unix, `cmd.exe` on Windows) |
| `--max-connections` | | `0` | Maximum concurrent WebSocket clients (0 = unlimited; agents not affected) |
| `--log` | | — | Log file path (default: stderr). With `--daemon`, overrides `/tmp/` default. |
| `--timeout` | | — | Auto-shutdown after idle duration with no clients (e.g. `30m`, `2h`) |
| `--daemon` | `-d` | `false` | Run as background daemon (logs to `--log` path or `/tmp/rc-<pid>.log`) |
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

- **Tab bar** — Commands shown as tabs; always visible even with a single tab. Click to switch.
  - 🟢 Green dot — running
  - 🔴 Red dot — exited
  - 🟡 Yellow pulsing dot — awaiting input (idle for 3+ seconds)
  - ⚫ Gray dot — agent disconnected
  - Purple ring — remote agent tab indicator
  - **×** close button — appears on disconnected remote tabs to remove them
  - **Alt+1~9** — keyboard shortcut to switch tabs by position
  - **Hover tooltip** — shows user, PID, and address (remote tabs: `user@ip, pid: 1234`)
  - **Drag-and-drop** — reorder tabs by dragging; order saved to localStorage per browser
  - **☰ Menu** — sticky button at right end of tab bar; tabable action menu:
    - **Close disconnected tabs** — removes all disconnected agent tabs at once
    - **Reset tab order** — clears saved tab order (localStorage) and restores original order
    - **About & Licenses** — opens modal with version, author, GitHub link, and open-source licenses
    - **Logout** — clears authentication token and reloads (shown only when logged in)
- **Split pane** — Click the split icon (⧉) on a non-active tab to send it to a right-side panel.
  - Multiple tabs can be stacked vertically in the split area
  - Split tab headers show status dot (🟢/🔴/🟡/⚫), **✕** unsplit, **📤** upload (when enabled & connected), and **↑↓** reorder arrows
  - **Restart** button (↻) appears inline when a split tab's command exits
  - **Focus indicator** — subtle highlight on the focused split terminal
  - **Reorder** — move split tabs up/down; boundary arrows are dimmed and disabled
  - Split state persists across page reloads (saved to localStorage)
  - On narrow screens (≤ 768px), the split pane becomes a slide-out drawer toggled by a floating button
- **Copy on select** — selecting text in the terminal automatically copies to clipboard
- **xterm.js** terminal with Catppuccin Mocha theme, 50K scrollback
- **Session replay** — reconnecting replays all buffered output per tab
- **Login page** — automatic login overlay when password is set; token stored in session
- **Dynamic header** — Shows logo, hostname, working directory (left-truncated on narrow screens); switches to remote agent info when viewing remote tabs
- **Restart bar** — appears when active tab's command exits; click to restart (hidden with `--no-restart`)
- **Disconnect overlay** — appears on WebSocket disconnect; click Reconnect to retry (all input blocked while disconnected)
- **Upload modal** — when `--upload` is enabled, an upload icon appears next to the workspace path for tabs that support it (per-tab); supports drag-and-drop, progress bar, and duplicate file rejection. Agent tabs also show upload when the agent runs with `--upload`.
- **Floating helper button** (mobile/touch) — bottom-right SVG keyboard icon opens panel without triggering virtual keyboard:
  - Arrow keys
  - Special: Tab, Esc, Enter, Space
  - Ctrl+C (interrupt)
  - Ctrl toggle (activate, type a letter, sends Ctrl+letter)
  - **Clipboard paste** — reads clipboard and sends as terminal input

## Releases

Pre-built binaries are available on the [Releases](https://github.com/hunydev/rc/releases) page for:

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64, arm64)

Release binaries include the version tag (e.g. `rc -v` → `rc version v0.5.2`).

## Platform Notes

| Platform | PTY Backend | Default Shell | Daemon |
|----------|-------------|---------------|--------|
| Linux | [creack/pty](https://github.com/creack/pty) | `bash` | ✅ `Setsid` |
| macOS | [creack/pty](https://github.com/creack/pty) | `bash` | ✅ `Setsid` |
| Windows | [ConPTY](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session) | `cmd.exe` | ⚠️ Best-effort |

Windows requires **Windows 10 1809+** for ConPTY support. All core features (browser remote, multi-tab, auth, agent mode) work across all platforms.

## Dependencies

- [spf13/cobra](https://github.com/spf13/cobra) — CLI framework
- [creack/pty](https://github.com/creack/pty) — PTY management
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket server
- [xterm.js](https://xtermjs.org/) — Browser terminal emulator (loaded via CDN)

## License

[MIT](LICENSE)
