import { useState, useEffect, useRef, useContext, createContext } from 'react'
import { ArrowLeft, Menu, X, Copy, Check, ChevronRight } from 'lucide-react'
import './DocsPage.css'

const LangCtx = createContext()

function CodeBlock({ children, lang = 'bash' }) {
  const [copied, setCopied] = useState(false)
  const text = typeof children === 'string' ? children.trim() : ''
  const handleCopy = () => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <div className="doc-code">
      <div className="doc-code-header">
        <span className="doc-code-lang">{lang}</span>
        <button className="doc-code-copy" onClick={handleCopy}>
          {copied ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
        </button>
      </div>
      <pre><code>{text}</code></pre>
    </div>
  )
}

function Tip({ children, type = 'tip' }) {
  const labels = { tip: '💡 Tip', warning: '⚠️ Warning', note: '📝 Note', important: '❗ Important' }
  return (
    <div className={`doc-callout doc-callout-${type}`}>
      <div className="doc-callout-title">{labels[type]}</div>
      <div>{children}</div>
    </div>
  )
}

function OptTable({ rows }) {
  return (
    <div className="doc-table-wrap">
      <table className="doc-table">
        <thead>
          <tr><th>Flag</th><th>Short</th><th>Default</th><th>Description</th></tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              <td><code>{r[0]}</code></td>
              <td>{r[1] ? <code>{r[1]}</code> : '—'}</td>
              <td>{r[2] ? <code>{r[2]}</code> : '—'}</td>
              <td>{r[3]}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ────────────────────────── Sections ──────────────────────────

function Introduction() {
  return (
    <section id="introduction">
      <h2>Introduction</h2>
      <p>
        <strong>rc</strong> (Remote Control) is a lightweight, single-binary server that runs any CLI command in a pseudo-terminal (PTY) and streams it to a web browser in real-time via WebSocket.
      </p>
      <p>
        Close the browser, come back later — the output is preserved and the process keeps running. It's designed for developers and sysadmins who need to access terminal sessions from any device.
      </p>
      <h3>Key Features</h3>
      <ul>
        <li><strong>Remote access</strong> — Control CLI tools from any device with a browser</li>
        <li><strong>Multi-command tabs</strong> — Run multiple commands in one session with browser tabs</li>
        <li><strong>Split pane</strong> — Monitor multiple terminals side-by-side</li>
        <li><strong>Distributed terminals</strong> — Attach remote servers to a central hub</li>
        <li><strong>Session persistence</strong> — Process survives browser disconnection</li>
        <li><strong>Password auth</strong> — SHA-256 hashed token with progressive rate limiting</li>
        <li><strong>TLS/HTTPS</strong> — Serve over HTTPS with custom certificates</li>
        <li><strong>File upload</strong> — Upload files from browser to server</li>
        <li><strong>Mobile friendly</strong> — Floating helper keyboard for touch devices</li>
        <li><strong>Cross-platform</strong> — Linux, macOS, and Windows support</li>
      </ul>

      <h3>Architecture</h3>
      <CodeBlock>{`Browser (xterm.js + Tab UI)
    ↕ WebSocket (JSON, tab-aware)
Hub Server (:8000)
    ├─ PTY × N (local commands)
    └─ WebSocket /attach (agent protocol)
          ↕
Agent on Server B ── PTY × M (remote commands)
Agent on Server C ── PTY × K (remote commands)`}</CodeBlock>
      <p>
        The hub server manages local PTY processes and accepts connections from remote agents. Browsers connect via WebSocket to interact with all terminals — both local and remote.
      </p>
    </section>
  )
}

function Installation() {
  return (
    <section id="installation">
      <h2>Installation</h2>

      <h3>Quick Install (Recommended)</h3>
      <p>Install the latest release with a single command:</p>

      <h4>macOS / Linux</h4>
      <CodeBlock>{`curl -fsSL https://rc.huny.dev/install.sh | bash`}</CodeBlock>

      <h4>Windows (PowerShell)</h4>
      <CodeBlock lang="powershell">{`powershell -c "irm https://rc.huny.dev/install.ps1 | iex"`}</CodeBlock>

      <h3>Build from Source</h3>
      <p>Requires <strong>Go 1.22+</strong>:</p>
      <CodeBlock>{`git clone https://github.com/hunydev/rc.git
cd rc
go build -o rc .
./rc --version`}</CodeBlock>

      <h3>Pre-built Binaries</h3>
      <p>
        Download pre-built binaries from the <a href="https://github.com/hunydev/rc/releases" target="_blank" rel="noopener noreferrer">Releases</a> page. Available for:
      </p>
      <ul>
        <li>Linux (amd64, arm64)</li>
        <li>macOS (amd64, arm64)</li>
        <li>Windows (amd64, arm64)</li>
      </ul>
      <Tip>
        Release binaries include the version tag. Run <code>rc -v</code> to verify your installation.
      </Tip>
    </section>
  )
}

function QuickStart() {
  return (
    <section id="quick-start">
      <h2>Quick Start</h2>

      <h3>Basic Usage</h3>
      <p>Run rc with no arguments to start a default bash session:</p>
      <CodeBlock>{`./rc`}</CodeBlock>
      <p>Open <code>http://localhost:8000</code> in your browser — you'll see a fully interactive terminal.</p>

      <h3>Running a Specific Command</h3>
      <p>Use <code>-c</code> to specify the command to run:</p>
      <CodeBlock>{`./rc -c "htop"
./rc -c "python3 -i"
./rc -c "tail -f /var/log/syslog"`}</CodeBlock>

      <h3>Multiple Commands (Tabs)</h3>
      <p>Use multiple <code>-c</code> flags to run several commands as separate browser tabs:</p>
      <CodeBlock>{`./rc -c "bash" -c "htop" -c "python3 -i"`}</CodeBlock>

      <h3>Custom Tab Labels</h3>
      <p>Pair <code>-l</code> with <code>-c</code> to give tabs meaningful names:</p>
      <CodeBlock>{`./rc -c "bash" -l "dev" -c "htop" -l "monitor" -c "tail -f app.log" -l "logs"`}</CodeBlock>

      <h3>Custom Port and Bind Address</h3>
      <CodeBlock>{`# Listen on port 9000
./rc -p 9000

# Local-only access (not exposed to network)
./rc --bind 127.0.0.1

# Both
./rc -p 9000 --bind 127.0.0.1 -c "bash"`}</CodeBlock>
      <Tip>
        Default bind is <code>0.0.0.0</code> (all interfaces). Use <code>127.0.0.1</code> for local-only access.
      </Tip>
    </section>
  )
}

function CLIReference() {
  return (
    <section id="cli-reference">
      <h2>CLI Reference</h2>
      <p>Complete reference of all available command-line options.</p>

      <h3>Core Options</h3>
      <OptTable rows={[
        ['--command', '-c', 'bash', 'Command to run (repeatable for multi-tab)'],
        ['--label', '-l', '—', 'Tab label (repeatable, paired with -c)'],
        ['--port', '-p', '8000', 'HTTP server port'],
        ['--bind', '', '0.0.0.0', 'Bind address'],
        ['--shell', '', '—', 'Default shell when no -c is given (default: bash on Unix, cmd.exe on Windows)'],
      ]} />

      <h3>Security Options</h3>
      <OptTable rows={[
        ['--password', '', '—', 'Password for server access. Env: RC_PASSWORD'],
        ['--trusted-proxy', '', 'false', 'Trust X-Forwarded-For / X-Real-Ip headers (enable when behind reverse proxy)'],
        ['--tls-cert', '', '—', 'TLS certificate file path (enables HTTPS)'],
        ['--tls-key', '', '—', 'TLS private key file path'],
        ['--route', '', '—', 'URL route prefix (e.g. --route /myapp)'],
      ]} />

      <h3>Agent Mode</h3>
      <OptTable rows={[
        ['--attach', '-a', '—', 'Attach to a remote hub (e.g. -a serverA:8000)'],
      ]} />

      <h3>Process Options</h3>
      <OptTable rows={[
        ['--no-restart', '', 'false', 'Disable command restart after exit'],
        ['--readonly', '', 'false', 'Disable stdin input (output-only terminals)'],
        ['--working-dir', '-w', '—', 'Working directory for PTY processes'],
        ['--env', '-e', '—', 'Environment variable (repeatable, KEY=VALUE format)'],
        ['--title', '', '—', 'Custom title in browser header and page title'],
      ]} />

      <h3>Server Options</h3>
      <OptTable rows={[
        ['--upload', '', 'false', 'Enable file upload to working directory'],
        ['--max-connections', '', '0', 'Maximum concurrent WebSocket clients (0 = unlimited)'],
        ['--timeout', '', '—', 'Auto-shutdown after idle duration (e.g. 30m, 2h)'],
        ['--log', '', '—', 'Log file path (default: stderr)'],
        ['--daemon', '-d', 'false', 'Run as background daemon'],
        ['--buffer-size', '', '10', 'Output buffer size in MB'],
        ['--cols', '', '120', 'Initial terminal columns'],
        ['--rows', '', '30', 'Initial terminal rows'],
      ]} />

      <h3>Information</h3>
      <OptTable rows={[
        ['--update', '', '', 'Check for updates and install the latest version'],
        ['--version', '-v', '', 'Print version and exit'],
        ['--help', '-h', '', 'Show help message'],
      ]} />

      <Tip type="note">
        Flags can be combined freely. For example: <code>./rc -p 9000 --password secret -c "bash" -l "dev" --upload --tls-cert cert.pem --tls-key key.pem</code>
      </Tip>
    </section>
  )
}

function TabManagement() {
  return (
    <section id="tab-management">
      <h2>Tab Management</h2>
      <p>rc presents each command as a browser tab, similar to a terminal multiplexer but in your browser.</p>

      <h3>Multiple Commands</h3>
      <p>Each <code>-c</code> flag creates a separate tab:</p>
      <CodeBlock>{`./rc -c "bash" -c "htop" -c "python3 -i"`}</CodeBlock>

      <h3>Tab Labels</h3>
      <p>Use <code>-l</code> paired with <code>-c</code> to set custom labels:</p>
      <CodeBlock>{`./rc -c "bash" -l "development" -c "npm run dev" -l "frontend"`}</CodeBlock>
      <p>Without labels, tabs show the command name (e.g., "bash", "htop").</p>

      <h3>Status Indicators</h3>
      <p>Each tab shows a colored status dot:</p>
      <ul>
        <li><strong>🟢 Green</strong> — Command is running</li>
        <li><strong>🔴 Red</strong> — Command has exited</li>
        <li><strong>🟡 Yellow (pulsing)</strong> — Awaiting input (idle for 3+ seconds)</li>
        <li><strong>⚫ Gray</strong> — Remote agent disconnected</li>
        <li><strong>Purple ring</strong> — Remote agent tab indicator</li>
      </ul>

      <h3>Keyboard Shortcuts</h3>
      <p>Switch between tabs with <strong>Alt+1</strong> through <strong>Alt+9</strong> (by position), or <strong>Alt+←/→</strong> for adjacent tabs.</p>

      <h3>Drag and Drop</h3>
      <p>Reorder tabs by dragging them, including to the rightmost position. The order is saved to localStorage and persists across page reloads per browser.</p>

      <h3>Horizontal Scroll</h3>
      <p>When tabs overflow, use the mouse wheel on the tab bar to scroll horizontally (no Shift needed).</p>

      <h3>Double-Click Rename</h3>
      <p>Double-click any tab label to rename it. Custom names are saved to localStorage and restored on reload. <strong>Reset all tabs</strong> from the menu restores original names.</p>

      <h3>Tab Menu (☰)</h3>
      <p>The sticky menu button at the right end of the tab bar provides:</p>
      <ul>
        <li><strong>Attach token</strong> — Generate a temporary one-time-use token for agent <code>--attach</code> (shown when password is set; 5-minute expiry)</li>
        <li><strong>Close disconnected tabs</strong> — Remove all disconnected agent tabs at once</li>
        <li><strong>Reset all tabs</strong> — Clear saved order, custom names, and restore the original layout</li>
        <li><strong>Check for Updates</strong> — Check for and apply updates from the UI. The new binary is verified before restarting; if the new process fails to start, the server recovers automatically.</li>
        <li><strong>Help &amp; Docs</strong> — Quick guide with tab statuses, split pane, upload, shortcuts</li>
        <li><strong>About &amp; Licenses</strong> — Version info, author, GitHub link, open-source licenses</li>
        <li><strong>Logout</strong> — Clear authentication token and reload (shown only when logged in)</li>
      </ul>

      <h3>Hover Tooltip</h3>
      <p>Hover over any tab to see details: user, PID, and address. Remote agent tabs show <code>user@ip, pid: 1234</code>.</p>

      <h3>Close Button</h3>
      <p>Disconnected remote agent tabs show an <strong>×</strong> button. Click to remove them from the tab bar. When the agent reconnects, fresh tabs are created.</p>

      <h3>Restart on Exit</h3>
      <p>When a command exits, a restart bar appears at the bottom. Click to restart the command in the same tab. This can be disabled with <code>--no-restart</code>.</p>
    </section>
  )
}

function SplitPane() {
  return (
    <section id="split-pane">
      <h2>Split Pane</h2>
      <p>Monitor multiple terminals side-by-side by sending tabs to a split view panel.</p>

      <h3>How to Split</h3>
      <p>Click the split icon (<strong>⧉</strong>) on any non-active tab to send it to the right-side split panel. Multiple tabs can be stacked vertically.</p>

      <h3>Split Tab Header</h3>
      <p>Each split tab has a header bar with:</p>
      <ul>
        <li><strong>Status dot</strong> — Same color coding as main tabs (🟢/🔴/🟡/⚫)</li>
        <li><strong>✕ Unsplit</strong> — Move the tab back to the main tab bar</li>
        <li><strong>📤 Upload</strong> — Upload files (when <code>--upload</code> is enabled and tab is connected)</li>
        <li><strong>↑↓ Reorder arrows</strong> — Move split tabs up or down in the stack</li>
        <li><strong>↻ Restart</strong> — Appears inline when the command exits</li>
      </ul>

      <h3>Focus Indicator</h3>
      <p>The currently focused split terminal has a subtle highlight effect, making it easy to identify which terminal is receiving keyboard input.</p>

      <h3>Reorder Constraints</h3>
      <p>The top tab's up arrow and the bottom tab's down arrow are dimmed and disabled — you can't move beyond the boundary.</p>

      <h3>Mobile</h3>
      <p>On narrow screens (≤ 768px), the split pane becomes a slide-out drawer toggled by a floating button. This keeps the interface usable on mobile devices.</p>

      <h3>Persistence</h3>
      <p>Split state is saved to localStorage. Refreshing the page restores tabs to their split positions.</p>
    </section>
  )
}

function AgentMode() {
  return (
    <section id="agent-mode">
      <h2>Agent Mode (Remote Attach)</h2>
      <p>Run commands on remote servers and monitor them from a central hub's browser. This is one of rc's most powerful features for managing distributed infrastructure.</p>

      <h3>How It Works</h3>
      <CodeBlock>{`# Server A (hub) — the central dashboard
./rc -c "bash"

# Server B (agent) — attaches to Server A
./rc -a serverA:8000 -c "htop" -c "tail -f /var/log/syslog"

# Server C (another agent)
./rc -a serverA:8000 -c "docker stats" -l "containers"`}</CodeBlock>
      <p>
        The agent spawns commands locally on its machine and streams them to the hub. The hub's browser automatically gets new tabs labeled with the agent's hostname (e.g., <code>serverB: htop</code>).
      </p>

      <h3>Full Interactivity</h3>
      <p>Agent tabs support everything local tabs do: typing, resizing, and restarting — all routed back to the agent's machine.</p>

      <h3>With Password Protection</h3>
      <CodeBlock>{`# Hub with password
./rc --password mysecret -c "bash"

# Agent must use the same password
./rc -a serverA:8000 --password mysecret -c "htop"`}</CodeBlock>
      <Tip type="important">
        The agent's <code>--password</code> must match the hub's password exactly. The password is hashed with SHA-256 before being sent as a Bearer token.
      </Tip>

      <h3>Attach Token</h3>
      <p>Instead of sharing the hub's actual password, you can generate a temporary <strong>attach token</strong> from the hub's web UI:</p>
      <ol>
        <li>Open the tab menu (☰) in the hub frontend and click <strong>🔑 Attach token</strong></li>
        <li>A one-time-use token is generated with a <strong>5-minute expiry</strong></li>
        <li>Copy the token and use it as the agent's <code>--password</code> value</li>
      </ol>
      <CodeBlock>{`# On the agent machine, use the generated token instead of the real password
./rc -a hub.example.com:8000 --password <token> -c "htop"`}</CodeBlock>
      <Tip type="note">
        Attach tokens are single-use: once an agent authenticates with a token, the token is immediately invalidated. The menu item is only visible when the hub is running with <code>--password</code>.
      </Tip>

      <h3>Auto-Reconnect</h3>
      <p>If an agent disconnects (network issue, restart, etc.):</p>
      <ul>
        <li>Agent tabs show a gray "disconnected" status dot</li>
        <li>The agent automatically retries connection every 3 seconds</li>
        <li>On reconnect, fresh tabs are created with restored sessions</li>
        <li>Disconnected tabs can be closed with the × button</li>
      </ul>

      <h3>Scheme Auto-Detection</h3>
      <p>The connection scheme is automatically detected:</p>
      <ul>
        <li>Port 443 → <code>wss://</code> (secure WebSocket)</li>
        <li>Other ports → <code>ws://</code></li>
        <li>Explicit URLs are also supported: <code>ws://</code>, <code>wss://</code>, <code>http://</code>, <code>https://</code></li>
      </ul>
      <CodeBlock>{`# Auto-detect (wss for 443)
./rc -a hub.example.com:443 -c "bash"

# Explicit scheme
./rc -a wss://hub.example.com -c "bash"
./rc -a https://hub.example.com -c "bash"`}</CodeBlock>

      <h3>Agent with Route Prefix</h3>
      <p>If the hub uses <code>--route</code>, include the path in the attach URL:</p>
      <CodeBlock>{`# Hub
./rc --route /terminal -c "bash"

# Agent
./rc -a serverA:8000/terminal -c "htop"`}</CodeBlock>

      <h3>Agent-Specific Flags</h3>
      <p>These flags work independently on hub and agent:</p>
      <ul>
        <li><code>--readonly</code> — Only restricts the agent's own tabs, not the hub's</li>
        <li><code>--no-restart</code> — Only affects the agent's commands</li>
        <li><code>--upload</code> — Enables upload for the agent's tabs (proxied to agent machine)</li>
        <li><code>--working-dir</code> — Sets the agent's PTY working directory</li>
        <li><code>--env</code> — Sets environment variables for the agent's commands</li>
      </ul>

      <h3>Max Connections</h3>
      <p>The hub's <code>--max-connections</code> limit only affects browser WebSocket clients. Agent connections are never throttled.</p>
    </section>
  )
}

function Security() {
  return (
    <section id="security">
      <h2>Security</h2>
      <p>rc provides several layers of security for protecting terminal access.</p>

      <h3>Password Authentication</h3>
      <p>Set a password to require authentication for all access:</p>
      <CodeBlock>{`# Via CLI flag
./rc --password mysecret -c "bash"

# Via environment variable (recommended — avoids ps visibility)
RC_PASSWORD=mysecret ./rc -c "bash"`}</CodeBlock>
      <p>When password is set:</p>
      <ul>
        <li>All API endpoints (<code>/info</code>, <code>/ws</code>, <code>/upload</code>) require a Bearer token</li>
        <li>The browser shows a login page automatically</li>
        <li>Tokens are SHA-256 hashes — the raw password is never transmitted</li>
        <li>Agent <code>--attach</code> uses the same hashed token for authentication</li>
      </ul>

      <h3>Login Security</h3>
      <p>The login endpoint (<code>/login</code>) has built-in brute-force protection:</p>
      <ul>
        <li><strong>500ms delay</strong> — Every login attempt has a server-side delay to slow down attacks</li>
        <li><strong>Progressive IP lockout</strong>:</li>
      </ul>
      <div className="doc-table-wrap">
        <table className="doc-table">
          <thead><tr><th>Failed Attempts</th><th>Lockout Duration</th></tr></thead>
          <tbody>
            <tr><td>5</td><td>5 minutes</td></tr>
            <tr><td>10</td><td>1 hour</td></tr>
            <tr><td>20</td><td>24 hours</td></tr>
          </tbody>
        </table>
      </div>
      <p>Lockout is IP-based (supports <code>X-Forwarded-For</code> and <code>X-Real-Ip</code> for reverse proxy setups). Bearer token authentication is never affected by rate limiting.</p>

      <h3>Attach Tokens</h3>
      <p>Generate temporary one-time-use tokens for agent authentication — avoid sharing the actual hub password:</p>
      <ul>
        <li>Open the <strong>☰ menu</strong> and click <strong>🔑 Attach token</strong></li>
        <li>A 40-character hex token is generated using <code>crypto/rand</code></li>
        <li>The token is stored as a SHA-256 hash (the raw token is never stored server-side)</li>
        <li>Tokens expire after <strong>5 minutes</strong> and are <strong>single-use</strong> — once an agent authenticates, the token is immediately invalidated</li>
        <li>The agent uses the token as <code>--password</code>; it is hashed client-side just like a normal password</li>
      </ul>

      <h3>Trusted Proxy</h3>
      <p>When running behind a reverse proxy (e.g., nginx), enable <code>--trusted-proxy</code> to read the real client IP from <code>X-Forwarded-For</code> / <code>X-Real-Ip</code> headers:</p>
      <CodeBlock>{`./rc --trusted-proxy --password secret -c "bash"`}</CodeBlock>
      <p>This affects rate limiting, tab tooltips (agent IP display), and logging. Without this flag, all clients appear as <code>127.0.0.1</code> when behind a proxy.</p>

      <Tip type="warning">
        Use <code>RC_PASSWORD</code> environment variable instead of <code>--password</code> flag when possible. CLI flags are visible in <code>ps</code> output.
      </Tip>

      <h3>TLS / HTTPS</h3>
      <p>Serve rc over HTTPS with TLS certificates:</p>
      <CodeBlock>{`./rc --tls-cert /path/to/cert.pem --tls-key /path/to/key.pem -c "bash"`}</CodeBlock>
      <p>Both <code>--tls-cert</code> and <code>--tls-key</code> must be specified together. When enabled, the server logs the <code>https://</code> URL.</p>

      <h3>Route Prefix</h3>
      <p>Use <code>--route</code> to serve rc under a sub-path, adding security-by-obscurity on top of password auth:</p>
      <CodeBlock>{`./rc --route /secret/terminal -c "bash"
# Access at http://localhost:8000/secret/terminal/`}</CodeBlock>
      <p>All endpoints are prefixed: <code>/secret/terminal/ws</code>, <code>/secret/terminal/info</code>, etc.</p>

      <h3>Security Headers</h3>
      <p>rc automatically sets security headers on all HTTP responses:</p>
      <ul>
        <li><code>X-Content-Type-Options: nosniff</code></li>
        <li><code>X-Frame-Options: DENY</code></li>
        <li><code>Referrer-Policy: strict-origin-when-cross-origin</code></li>
      </ul>
    </section>
  )
}

function FileUpload() {
  return (
    <section id="file-upload">
      <h2>File Upload</h2>
      <p>Upload files from your browser directly to the server's working directory.</p>

      <h3>Enable Upload</h3>
      <CodeBlock>{`./rc --upload -c "bash"`}</CodeBlock>
      <p>When enabled, an upload icon (📤) appears next to the workspace path in the header.</p>

      <h3>Usage</h3>
      <ul>
        <li>Click the upload icon or drag-and-drop a file onto the upload modal</li>
        <li>Files are uploaded to the server's current working directory</li>
        <li>A progress bar shows upload status</li>
        <li>Duplicate filenames are rejected (no overwrite)</li>
        <li>Single file upload at a time</li>
      </ul>

      <h3>Agent Upload</h3>
      <p>When an agent runs with <code>--upload</code>, its tabs also show the upload icon. Files are proxied through the hub to the agent's machine:</p>
      <CodeBlock>{`# Hub
./rc --upload -c "bash"

# Agent (also with --upload)
./rc -a hub:8000 --upload -c "bash"`}</CodeBlock>
      <Tip>
        Upload is disabled for disconnected tabs — the upload icon is hidden when the tab's agent is disconnected.
      </Tip>

      <h3>Working Directory</h3>
      <p>Files are uploaded to the working directory, which defaults to where rc was started. Use <code>--working-dir</code> to change it:</p>
      <CodeBlock>{`./rc --upload -w /tmp/uploads -c "bash"`}</CodeBlock>
    </section>
  )
}

function Deployment() {
  return (
    <section id="deployment">
      <h2>Deployment</h2>

      <h3>Daemon Mode</h3>
      <p>Run rc as a background daemon:</p>
      <CodeBlock>{`# Basic daemon
./rc -d -c "bash"

# Daemon with custom log file
./rc -d --log /var/log/rc.log -c "bash"

# Without --log, daemon logs to /tmp/rc-<pid>.log
./rc -d -c "bash"`}</CodeBlock>
      <p>The daemon re-executes itself without the <code>-d</code> flag, detaching from the terminal. Log output goes to the <code>--log</code> path if set, otherwise to <code>/tmp/rc-&lt;pid&gt;.log</code>.</p>

      <h3>systemd Service</h3>
      <p>The included <code>service.sh</code> script manages rc as a systemd service:</p>
      <CodeBlock>{`# Install as systemd service
./service.sh install

# Start / stop / restart
./service.sh start
./service.sh stop
./service.sh restart

# Status with health check
./service.sh status

# Rebuild: stops → go build → restarts
./service.sh build

# View logs
./service.sh logs 100
./service.sh logs-follow

# Remove service entirely
./service.sh uninstall`}</CodeBlock>

      <h4>Service Environment Variables</h4>
      <div className="doc-table-wrap">
        <table className="doc-table">
          <thead><tr><th>Variable</th><th>Default</th><th>Description</th></tr></thead>
          <tbody>
            <tr><td><code>RC_PORT</code></td><td><code>8000</code></td><td>Server port</td></tr>
            <tr><td><code>RC_COMMAND</code></td><td><code>bash</code></td><td>Command to run</td></tr>
            <tr><td><code>RC_COLS</code></td><td><code>120</code></td><td>Terminal columns</td></tr>
            <tr><td><code>RC_ROWS</code></td><td><code>30</code></td><td>Terminal rows</td></tr>
            <tr><td><code>RC_BIND</code></td><td><code>0.0.0.0</code></td><td>Bind address</td></tr>
            <tr><td><code>RC_PASSWORD</code></td><td>—</td><td>Access password</td></tr>
          </tbody>
        </table>
      </div>
      <CodeBlock>{`RC_PORT=9000 RC_PASSWORD=secret ./service.sh install`}</CodeBlock>

      <h3>Reverse Proxy (nginx)</h3>
      <p>Serve rc behind nginx with WebSocket proxy:</p>
      <CodeBlock lang="nginx">{`server {
    listen 443 ssl;
    server_name terminal.example.com;

    ssl_certificate     /etc/ssl/certs/terminal.pem;
    ssl_certificate_key /etc/ssl/private/terminal.key;

    location / {
        proxy_pass http://127.0.0.1:8000;
        proxy_http_version 1.1;

        # WebSocket support
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # Forward client IP for rate limiting
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Host $host;

        # Disable buffering for real-time streaming
        proxy_buffering off;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }
}`}</CodeBlock>

      <h4>With Route Prefix</h4>
      <CodeBlock lang="nginx">{`location /terminal/ {
    proxy_pass http://127.0.0.1:8000/terminal/;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_buffering off;
    proxy_read_timeout 86400s;
}`}</CodeBlock>
      <p>Start rc with a matching route:</p>
      <CodeBlock>{`./rc --route /terminal -c "bash"`}</CodeBlock>

      <Tip type="important">
        Always include <code>proxy_set_header Upgrade</code> and <code>proxy_set_header Connection "upgrade"</code> for WebSocket support. Without these, terminal connections will fail.
      </Tip>

      <h3>Idle Timeout</h3>
      <p>Auto-shutdown the server when no clients are connected for a duration:</p>
      <CodeBlock>{`# Shutdown after 30 minutes idle
./rc --timeout 30m -c "bash"

# Shutdown after 2 hours idle
./rc --timeout 2h -c "bash"`}</CodeBlock>
      <p>The server checks every 5 seconds. As soon as a client connects, the idle timer resets. Useful for ephemeral environments or cost savings.</p>
    </section>
  )
}

function AdvancedConfig() {
  return (
    <section id="advanced-config">
      <h2>Advanced Configuration</h2>

      <h3>Working Directory</h3>
      <p>Set the working directory for all PTY processes:</p>
      <CodeBlock>{`./rc -w /opt/myproject -c "bash"
./rc --working-dir /var/log -c "bash"`}</CodeBlock>
      <p>The directory must exist. The header in the browser shows the current working directory.</p>

      <h3>Environment Variables</h3>
      <p>Pass environment variables to PTY processes with <code>-e</code>:</p>
      <CodeBlock>{`./rc -e "NODE_ENV=production" -e "DEBUG=true" -c "npm start"
./rc -e "TERM=xterm-256color" -e "LANG=en_US.UTF-8" -c "bash"`}</CodeBlock>
      <p>Variables must be in <code>KEY=VALUE</code> format. They're added on top of the system's existing environment.</p>

      <h3>Custom Title</h3>
      <p>Set a custom title that appears in the browser header and page title:</p>
      <CodeBlock>{`./rc --title "Production Server" -c "bash"
./rc --title "Dev Environment" -c "npm run dev"`}</CodeBlock>
      <p>When viewing remote agent tabs, the hostname is still shown instead of the title.</p>

      <h3>Shell Override</h3>
      <p>When no <code>-c</code> is given, rc uses the default shell (<code>bash</code> on Unix, <code>cmd.exe</code> on Windows). Override with <code>--shell</code>:</p>
      <CodeBlock>{`./rc --shell zsh
./rc --shell fish
./rc --shell /bin/sh`}</CodeBlock>
      <Tip type="note">
        <code>--shell</code> only affects the default command. If <code>-c</code> is specified, <code>--shell</code> is ignored.
      </Tip>

      <h3>Max Connections</h3>
      <p>Limit the number of concurrent browser WebSocket connections:</p>
      <CodeBlock>{`./rc --max-connections 5 -c "bash"`}</CodeBlock>
      <p>When the limit is reached, new browser connections get HTTP 503. Agent connections are never affected by this limit.</p>

      <h3>Buffer Size</h3>
      <p>Control how much terminal output is buffered for replay on reconnect:</p>
      <CodeBlock>{`# Default: 10 MB per tab
./rc --buffer-size 50 -c "bash"   # 50 MB buffer`}</CodeBlock>

      <h3>Initial Terminal Size</h3>
      <p>Set the initial PTY dimensions (before the browser sends a resize):</p>
      <CodeBlock>{`./rc --cols 200 --rows 50 -c "bash"`}</CodeBlock>

      <h3>Logging</h3>
      <p>Redirect server logs to a file:</p>
      <CodeBlock>{`./rc --log /var/log/rc.log -c "bash"

# With daemon mode
./rc -d --log /var/log/rc.log -c "bash"`}</CodeBlock>
      <p>Without <code>--log</code>, logs go to stderr. In daemon mode without <code>--log</code>, logs go to <code>/tmp/rc-&lt;pid&gt;.log</code>. Log files are opened in append mode.</p>

      <h3>Readonly &amp; No-Restart</h3>
      <p>Create view-only terminals:</p>
      <CodeBlock>{`# No input, no restart — pure monitoring
./rc --readonly --no-restart -c "tail -f /var/log/syslog"

# Readonly hub, interactive agent
./rc --readonly -c "bash"                    # Hub (view-only)
./rc -a hub:8000 -c "bash"                   # Agent (interactive)`}</CodeBlock>
      <p>These flags work independently on hub and agent — a hub's <code>--readonly</code> only restricts its local tabs.</p>
    </section>
  )
}

function Frontend() {
  return (
    <section id="frontend">
      <h2>Frontend Features</h2>
      <p>The browser interface includes several features beyond basic terminal emulation.</p>

      <h3>Terminal</h3>
      <ul>
        <li><strong>xterm.js</strong> with Catppuccin Mocha theme</li>
        <li><strong>50,000 line scrollback</strong> buffer</li>
        <li><strong>Copy on select</strong> — selecting text automatically copies to clipboard</li>
        <li><strong>Session replay</strong> — reconnecting replays all buffered output</li>
      </ul>

      <h3>Dynamic Header</h3>
      <p>The header shows:</p>
      <ul>
        <li>Logo and hostname (or custom <code>--title</code>)</li>
        <li>Working directory (left-truncated on narrow screens)</li>
        <li>Upload icon (when <code>--upload</code> is enabled)</li>
        <li>When viewing a remote agent tab, header switches to the agent's hostname and workspace</li>
      </ul>

      <h3>Login Page</h3>
      <p>When <code>--password</code> is set, the browser shows a login overlay. After successful login, the token is stored in sessionStorage (cleared when the browser tab is closed).</p>
      <p>The login page has no visual flash of terminal content — the app is hidden until authentication resolves.</p>

      <h3>Disconnect Overlay</h3>
      <p>When the WebSocket disconnects, rc applies a <strong>grace retry</strong>: the first disconnect after a successful connection triggers one automatic reconnection attempt before showing the overlay. If the retry also fails, or if the page has never been connected, the full-screen overlay appears immediately with a "Reconnect" button.</p>

      <h3>Leave Confirmation</h3>
      <p>Closing or navigating away from the hub page triggers a browser <code>beforeunload</code> confirmation dialog to prevent accidental disconnection from active sessions.</p>

      <h3>Mobile Touch Keyboard</h3>
      <p>On touch devices, a floating keyboard icon (bottom-right) opens a helper panel:</p>
      <ul>
        <li><strong>Arrow keys</strong> — Up, Down, Left, Right</li>
        <li><strong>Special keys</strong> — Tab, Esc, Enter, Space</li>
        <li><strong>Ctrl+C</strong> — Send interrupt signal</li>
        <li><strong>Ctrl toggle</strong> — Activate, type a letter → sends Ctrl+letter</li>
        <li><strong>Clipboard paste</strong> — Read clipboard and send as terminal input</li>
      </ul>
    </section>
  )
}

function PlatformNotes() {
  return (
    <section id="platform-notes">
      <h2>Platform Notes</h2>

      <div className="doc-table-wrap">
        <table className="doc-table">
          <thead><tr><th>Platform</th><th>PTY Backend</th><th>Default Shell</th><th>Daemon</th></tr></thead>
          <tbody>
            <tr><td>Linux</td><td>creack/pty</td><td><code>bash</code></td><td>✅ Setsid</td></tr>
            <tr><td>macOS</td><td>creack/pty</td><td><code>bash</code></td><td>✅ Setsid</td></tr>
            <tr><td>Windows</td><td>ConPTY</td><td><code>cmd.exe</code></td><td>⚠️ Best-effort</td></tr>
          </tbody>
        </table>
      </div>

      <h3>Linux &amp; macOS</h3>
      <ul>
        <li>Uses <a href="https://github.com/creack/pty" target="_blank" rel="noopener noreferrer">creack/pty</a> for PTY management</li>
        <li>Full daemon support with <code>Setsid</code> process group isolation</li>
        <li>All features fully supported</li>
      </ul>

      <h3>Windows</h3>
      <ul>
        <li>Uses <a href="https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session" target="_blank" rel="noopener noreferrer">ConPTY</a> (Windows Pseudo Console)</li>
        <li>Requires <strong>Windows 10 1809+</strong></li>
        <li>Environment variables are passed via <code>CreateProcessW</code> with a UTF-16 environment block</li>
        <li>Daemon mode is best-effort (no true <code>Setsid</code> equivalent)</li>
        <li>All core features work: browser remote, multi-tab, auth, agent mode</li>
      </ul>
    </section>
  )
}

function Troubleshooting() {
  return (
    <section id="troubleshooting">
      <h2>Troubleshooting</h2>

      <h3>WebSocket connection fails behind reverse proxy</h3>
      <p>Ensure your reverse proxy forwards WebSocket upgrade headers:</p>
      <CodeBlock lang="nginx">{`proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";`}</CodeBlock>

      <h3>Terminal size is wrong on initial load</h3>
      <p>rc automatically fits the terminal to the browser window 200ms after connection. If the size still looks wrong, try resizing the browser window — this triggers a re-fit.</p>

      <h3>Agent can't connect to password-protected hub</h3>
      <p>Ensure the agent's <code>--password</code> matches the hub's password exactly:</p>
      <CodeBlock>{`# Hub
./rc --password "my secret" -c "bash"

# Agent (same password)
./rc -a hub:8000 --password "my secret" -c "htop"`}</CodeBlock>
      <p>If using a reverse proxy, ensure WebSocket paths (<code>/attach</code>) are also proxied correctly.</p>

      <h3>Login locked out after too many attempts</h3>
      <p>Progressive lockout is IP-based. Wait for the lockout to expire (5 min → 1 hr → 24 hr) or restart the rc server to reset all lockouts.</p>
      <Tip>
        Bearer token authentication (saved session) is never affected by login lockout. Only the password login form is rate-limited.
      </Tip>

      <h3>Port already in use</h3>
      <CodeBlock>{`# Use a different port
./rc -p 9000 -c "bash"

# Find what's using the port
lsof -i :8000
# or
ss -tlnp | grep 8000`}</CodeBlock>

      <h3>Process died but rc shows "running"</h3>
      <p>rc monitors the PTY process. If the process exits, the tab status changes to "exited" (🔴). If you see "running" but the process seems stuck, try sending Ctrl+C or check the process list.</p>

      <h3>Upload fails with "file exists"</h3>
      <p>rc rejects uploads that would overwrite existing files. Rename the file or delete the existing one via terminal before uploading.</p>

      <h3>Daemon log location</h3>
      <CodeBlock>{`# Default daemon log
ls /tmp/rc-*.log

# Custom log location
./rc -d --log /var/log/rc.log -c "bash"
tail -f /var/log/rc.log`}</CodeBlock>
    </section>
  )
}

function Examples() {
  return (
    <section id="examples">
      <h2>Examples &amp; Recipes</h2>

      <h3>Development Environment</h3>
      <CodeBlock>{`# Full dev setup with labeled tabs
./rc \\
  -c "bash" -l "shell" \\
  -c "npm run dev" -l "frontend" \\
  -c "npm run server" -l "backend" \\
  -c "docker logs -f myapp" -l "logs" \\
  --title "My Project" \\
  -w /home/dev/myproject \\
  --upload`}</CodeBlock>

      <h3>Server Monitoring Dashboard</h3>
      <CodeBlock>{`# Hub on monitoring server
./rc --password monitoring123 -c "htop" -l "local" --title "Infra Monitor"

# Attach production servers
./rc -a monitor:8000 --password monitoring123 \\
  -c "htop" -l "cpu" \\
  -c "tail -f /var/log/nginx/access.log" -l "nginx" \\
  -c "docker stats" -l "containers"

# Attach database server
./rc -a monitor:8000 --password monitoring123 \\
  -c "watch -n 5 'mysql -e \"SHOW PROCESSLIST\"'" -l "mysql"`}</CodeBlock>

      <h3>Ephemeral Terminal</h3>
      <CodeBlock>{`# Auto-shutdown after 1 hour of inactivity
./rc --timeout 1h --password temp123 -c "bash" -d`}</CodeBlock>

      <h3>Secure Production Access</h3>
      <CodeBlock>{`# HTTPS + password + route prefix + logging
RC_PASSWORD=strongpassword ./rc \\
  --tls-cert /etc/ssl/cert.pem \\
  --tls-key /etc/ssl/key.pem \\
  --route /admin/terminal \\
  --log /var/log/rc.log \\
  --max-connections 3 \\
  -c "bash"`}</CodeBlock>

      <h3>Read-Only Log Viewer</h3>
      <CodeBlock>{`# No input, no restart, multiple log streams
./rc --readonly --no-restart \\
  -c "tail -f /var/log/syslog" -l "syslog" \\
  -c "tail -f /var/log/auth.log" -l "auth" \\
  -c "journalctl -f" -l "journal"`}</CodeBlock>

      <h3>CI/CD Build Monitor</h3>
      <CodeBlock>{`# Watch a build with auto-shutdown
./rc --timeout 30m --readonly --no-restart \\
  -c "tail -f /tmp/build.log" -l "build" \\
  --title "Build #1234"`}</CodeBlock>
    </section>
  )
}

// ────────────────────────── Sidebar & Layout ──────────────────────────

const sections = [
  { id: 'introduction', title: 'Introduction' },
  { id: 'installation', title: 'Installation' },
  { id: 'quick-start', title: 'Quick Start' },
  { id: 'cli-reference', title: 'CLI Reference' },
  { id: 'tab-management', title: 'Tab Management' },
  { id: 'split-pane', title: 'Split Pane' },
  { id: 'agent-mode', title: 'Agent Mode' },
  { id: 'security', title: 'Security' },
  { id: 'file-upload', title: 'File Upload' },
  { id: 'deployment', title: 'Deployment' },
  { id: 'advanced-config', title: 'Advanced Config' },
  { id: 'frontend', title: 'Frontend Features' },
  { id: 'platform-notes', title: 'Platform Notes' },
  { id: 'troubleshooting', title: 'Troubleshooting' },
  { id: 'examples', title: 'Examples & Recipes' },
]

export default function DocsPage({ onBack }) {
  const [activeSection, setActiveSection] = useState('introduction')
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const contentRef = useRef(null)

  // Scroll spy
  useEffect(() => {
    const content = contentRef.current
    if (!content) return
    const handleScroll = () => {
      const scrollTop = content.scrollTop + 100
      let current = 'introduction'
      for (const s of sections) {
        const el = document.getElementById(s.id)
        if (el && el.offsetTop <= scrollTop) current = s.id
      }
      setActiveSection(current)
    }
    content.addEventListener('scroll', handleScroll)
    return () => content.removeEventListener('scroll', handleScroll)
  }, [])

  // Handle hash navigation
  useEffect(() => {
    const hash = window.location.hash.replace('#/docs/', '').replace('#/docs', '')
    if (hash && sections.find(s => s.id === hash)) {
      setTimeout(() => {
        const el = document.getElementById(hash)
        if (el) el.scrollIntoView({ behavior: 'smooth' })
      }, 100)
    }
  }, [])

  const scrollTo = (id) => {
    const el = document.getElementById(id)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth' })
      window.history.replaceState(null, '', `#/docs/${id}`)
    }
    setSidebarOpen(false)
  }

  return (
    <div className="docs-layout">
      {/* Mobile sidebar toggle */}
      <button className="docs-sidebar-toggle" onClick={() => setSidebarOpen(!sidebarOpen)}>
        {sidebarOpen ? <X size={20} /> : <Menu size={20} />}
      </button>

      {/* Sidebar */}
      <aside className={`docs-sidebar ${sidebarOpen ? 'open' : ''}`}>
        <div className="docs-sidebar-header">
          <button className="docs-back" onClick={onBack}>
            <ArrowLeft size={14} />
            <img src="/logo.svg" alt="rc" className="docs-logo" />
            <span>rc</span>
          </button>
        </div>
        <nav className="docs-nav">
          <div className="docs-nav-title">Documentation</div>
          {sections.map(s => (
            <button
              key={s.id}
              className={`docs-nav-item ${activeSection === s.id ? 'active' : ''}`}
              onClick={() => scrollTo(s.id)}
            >
              <ChevronRight size={12} className="docs-nav-chevron" />
              {s.title}
            </button>
          ))}
        </nav>
      </aside>

      {/* Overlay for mobile */}
      {sidebarOpen && <div className="docs-overlay" onClick={() => setSidebarOpen(false)} />}

      {/* Content */}
      <main className="docs-content" ref={contentRef}>
        <div className="docs-content-inner">
          <Introduction />
          <Installation />
          <QuickStart />
          <CLIReference />
          <TabManagement />
          <SplitPane />
          <AgentMode />
          <Security />
          <FileUpload />
          <Deployment />
          <AdvancedConfig />
          <Frontend />
          <PlatformNotes />
          <Troubleshooting />
          <Examples />
          <div className="docs-footer">
            <p>rc — MIT License — <a href="https://github.com/hunydev/rc" target="_blank" rel="noopener noreferrer">GitHub</a></p>
          </div>
        </div>
      </main>
    </div>
  )
}
