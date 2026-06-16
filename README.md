# ORYX — General Purpose AI Agent

**A single-binary AI agent for your Linux VPS — monitor, chat, fix, automate.**

ORYX runs as a lightweight daemon (~9 MB RAM) that checks your server every minute, detects anomalies, attempts automatic fixes, and alerts you via Telegram when it's stuck. It also has an interactive chat TUI, a Telegram bot gateway, MCP tool support, and a Plan-and-Execute agent mode.

## Features

### 🛡️ 24/7 Autonomous Monitoring
- **Runs as a systemd service** — auto-starts on boot, auto-restarts on crash
- **1-minute check interval** — disk usage, memory pressure, load average, auth failures
- **Trend tracking** — alerts on sudden growth (disk +5%, memory +10%) not just fixed thresholds
- **SSH bruteforce detection** — counts failed auth attempts, alerts on spikes
- **Process anomaly detection** — flags new unknown processes or memory-doubling

### 🔧 Self-Healing
- **Canned auto-fixes** — disk cleanup (journal vacuum, apt autoremove, docker prune), service restart, warp-svc reset
- **No API cost for routine fixes** — shell commands only, zero LLM calls
- **Escalation to LLM** — if canned fix fails, spawns `ORYX fix` with a 50-iteration agent budget
- **Safety-ruled prompt** — LLM cannot run destructive commands, install packages, or modify system files

### 💬 Interactive TUI
- **Real-time streaming** — see LLM responses as they generate
- **Conversation history** — context persists across messages within a session
- **Markdown rendering** — LLM responses render as proper formatted text via glamour
- **Plan mode** (Ctrl+P) — read-only analysis mode, blocks destructive actions
- **Reasoning toggle** (Ctrl+R) — expand/collapse LLM reasoning blocks
- **Sidebar** (Ctrl+S) — server status at a glance (CPU, RAM, disk, uptime)
- **Command palette** — `/help`, `/scan`, `/config`, `/memory`, `/save`, `/update`, `/model`
- **Session resume** — `--resume` flag to continue previous conversations

### 🤖 Agent Capabilities
- **Plan-and-Execute** — goal decomposition with step-by-step execution and failure recovery
- **Reflexion** — self-critique loop that reviews tool outputs before responding
- **Budget tracking** — token, cost, and iteration limits per session
- **Tool registry** — JSON Schema-defined tools with call-count tracking
- **MCP support** — connect to any MCP server for extended tool capabilities

### 📱 Telegram Bot Gateway
- **Long-polling** — runs as a Telegram bot, no public URL needed
- **Full agent access** — chat with ORYX from your phone
- **Streaming responses** — see responses as they generate
- **Chat splitting** — automatic message splitting for Telegram's 4096 char limit

### 📡 Alert Delivery
- **Telegram** — direct messages when auto-fix fails
- **Webhook** — Slack-compatible webhook endpoint
- **Fallback to stdout** — always logs to journald

### ⚡ Minimal Footprint
- **Single binary** — ~22 MB, zero dependencies, no runtime required
- **Daemon mode** — ~9 MB RSS continuous
- **Chat mode** — ~17 MB RSS during interactive use
- **Fix mode** — ~17 MB RSS temporarily, exits when done

## Quick Start

```bash
# Install (one-liner)
curl -fsSL https://raw.githubusercontent.com/syawalqi/ORYX-AI/master/install.sh | bash

# Or manual download
curl -fsSL https://github.com/syawalqi/ORYX-AI/releases/download/v1.2.0/oryx-linux-amd64 -o oryx
chmod +x oryx && sudo mv oryx /usr/local/bin/

# Configure
oryx setup

# Start interactive chat
oryx

# Start 24/7 daemon
sudo systemctl enable --now oryx-daemon

# Or run daemon directly
oryx daemon

# Start Telegram bot
oryx telegram

# Check for updates
oryx --update
```

## Screenshots

<!-- TODO: Add TUI screenshot -->
<!-- ![ORYX Chat TUI](screenshots/chat.png) -->
<!-- ![ORYX Daemon](screenshots/daemon.png) -->

## Usage

### Subcommands

| Command | Description |
|---------|-------------|
| `oryx` | Interactive chat TUI (default) |
| `oryx setup` | Step-by-step configuration wizard |
| `oryx daemon` | Background monitoring daemon |
| `oryx fix --latest` | Fix the most recent unresolved anomaly |
| `oryx fix --stdin` | Fix from piped ticket JSON (used by daemon) |
| `oryx alert` | Send an alert via configured channels |
| `oryx telegram` | Start Telegram bot gateway |
| `oryx --resume` | Resume last conversation |
| `oryx --update` | Check for and apply updates |

### TUI Commands

| Command | Action |
|---------|--------|
| `/help` | Show all commands and keybindings |
| `/scan` | Refresh server context (memory.md) |
| `/config` | Show current configuration |
| `/config edit` | Edit config in nano/vim |
| `/memory` | Show server context |
| `/memory edit` | Edit memory.md |
| `/model <id>` | Change LLM model |
| `/plan` | Toggle plan mode (read-only) |
| `/save` | Save conversation to /tmp/ |
| `/update` | Check for and apply updates |
| `/clear` | Clear chat history |
| `/quit` | Exit |

### Keybindings

| Key | Action |
|-----|--------|
| `↑/↓` | Scroll viewport |
| `PgUp/PgDn` | Page scroll |
| `Enter` | Send message |
| `Ctrl+R` | Toggle reasoning expand/collapse |
| `Ctrl+T` | Toggle tool output expand/collapse |
| `Ctrl+S` | Toggle sidebar |
| `Ctrl+P` | Toggle plan mode |
| `Ctrl+C` | Cancel streaming / quit |

## Configuration

Generated by `oryx setup`. Stored at `~/.config/oryx/config.yaml`:

```yaml
provider: opencode-go           # LLM provider (opencode-go, openrouter, anthropic, ollama)
api_key: sk-...                 # Your API key
model: deepseek-v4-flash        # Model for chat and fix agent
daemon_model: deepseek-v4-flash # Model for daemon auto-fix

checks:
    interval: 1m                # Check frequency
    anomaly_window: 5m          # Window for auth fail counting
    disk_threshold: 85          # Warning at 85% disk
    mem_warning_threshold: 85   # Warning at 85% RAM
    mem_critical_threshold: 95  # Critical at 95% RAM
    disk_growth_threshold: 5    # Alert on +5% disk jump
    mem_growth_threshold: 10    # Alert on +10% RAM jump
    auth_fail_threshold: 10     # Alert on 10+ fails in window

alerts:
    enabled: false              # Enable alert delivery
    delivery: stdout            # stdout, telegram, webhook, or all
    telegram_token: ""          # Bot token for Telegram alerts
    telegram_chat: ""           # Chat ID for Telegram alerts

agent:
    max_iterations: 100         # Max agent loops per request
    temperature: 0.3
    max_tokens: 4096
    max_cost: 0.50              # Cost ceiling per session (USD)

telegram:
    enabled: false              # Enable Telegram bot gateway
    token: ""                   # Bot token from @BotFather
```

## Architecture

```
┌─ oryx chat ────────────────────┐
│  TUI (Bubbletea)                │
│  ├─ Agent loop (LLM + tools)    │
│  ├─ Plan-and-Execute engine     │
│  ├─ Reflexion self-critique     │
│  ├─ Budget tracker              │
│  ├─ Glamour Markdown rendering  │
│  └─ Conversation history        │
└─────────────────────────────────┘

┌─ oryx telegram ────────────────┐
│  Telegram Bot Gateway           │
│  ├─ Long-polling updates        │
│  ├─ Full agent access           │
│  ├─ Streaming responses         │
│  └─ Chat splitting (4096 char)  │
└─────────────────────────────────┘

┌─ oryx daemon ──────────────────┐
│  1-minute ticker                │
│  ├─ Threshold checks            │
│  ├─ Anomaly detection           │
│  ├─ Canned auto-fix             │
│  └─ Escalate → oryx fix        │
└─────────────────────────────────┘

┌─ oryx fix ─────────────────────┐
│  Headless LLM agent             │
│  ├─ 50-iteration budget         │
│  ├─ Tool-based remediation      │
│  └─ Alert on failure            │
└─────────────────────────────────┘

┌─ MCP Client ───────────────────┐
│  JSON-RPC 2.0 over stdio        │
│  ├─ Server connection           │
│  ├─ Tool discovery              │
│  └─ Tool execution              │
└─────────────────────────────────┘
```

## Supported Providers

- **OpenCode Go** — `opencode.ai/zen/go/v1` (default)
- **OpenRouter** — `openrouter.ai/api/v1` (200+ models)
- **Anthropic** — Native Claude API
- **Ollama** — Local models (zero-cost offline inference)
- **Custom** — Any OpenAI-compatible API

## Building from Source

```bash
git clone https://github.com/syawalqi/ORYX-AI.git
cd ORYX-AI
go build -ldflags="-s -w -X main.version=$(git describe --tags)" -o oryx .
```

Requires Go 1.22+.

## Requirements

- Linux (amd64 or arm64) or macOS
- An API key from a supported LLM provider
- systemd (optional, for daemon auto-start)

## License

MIT
