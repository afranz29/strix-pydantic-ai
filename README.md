# Strix

### Autonomous Multi-Agent Security Framework

Strix is an AI-powered security pentesting framework that coordinates multiple agents to discover, validate, and exploit vulnerabilities in an isolated sandbox.

---

## 🚀 Current Status

Strix is actively being developed with two implementations:

| Implementation | Status | Path | Description |
|---|---|---|---|
| **Strix Pydantic** | In Development | `strix-pydantic/` | Python implementation using Pydantic AI v1.102.0 and Pydantic Graph. Core orchestration complete; tool integration in progress. |
| **Strix Go** | Maintenance | `strix-go/` | Go port for high-concurrency and binary distribution. Currently in stabilization phase. |
| **Strix Legacy** | Legacy | `strix/` | Original Python prototype. Maintained for reference and skill library source. |

---

## ✨ Key Features (Pydantic Implementation)

### Implemented ✅
- **Graph-Based Orchestration** — Unified multi-step orchestrator using Pydantic Graph, coordinating 3 agent roles (reconnaissance, exploitation, post_exploitation).
- **Multi-LLM Provider Support** — String-based model selection with automatic fallback:
  - **Anthropic** (Primary): `claude-haiku-4-5` (when `ANTHROPIC_API_KEY` is set)
  - **OpenAI**: `gpt-4o-mini` (when `OPENAI_API_KEY` is set)
  - **Google Gemini**: `gemini-3.5-flash` (fallback when Google API key is set)
- **Skill-Based Agent Instructions** — Dynamically loads capabilities from `strix/skills/` markdown files.
- **Type-Safe Definitions** — Structured agent outputs using Pydantic models (vulnerabilities, notes, findings).
- **CLI Interface** — Non-interactive scanner with `--target`, `--scan-mode`, `--model` override, and testing modes.
- **Docker Sandbox Support** — Automatic Docker sandbox lifecycle with configurable timeout and cleanup.

### In Progress 🚧
- **Tool Execution Dispatch** — Sandbox client interface ready; endpoint routing and actual execution integration pending.
- **Message History Continuity** — Framework support present; multi-turn agent reasoning optimization in progress.

---

## 🚀 Quick Start (Strix Pydantic)

**Prerequisites:**
- Python 3.11+
- [uv](https://github.com/astral-sh/uv) (recommended for dependency management)
- Docker (for sandboxed tool execution)
- LLM API Key (Anthropic, OpenAI, or Google)

### Setup & Run

```bash
# Clone the repository
git clone https://github.com/usestrix/strix.git
cd strix

# Install dependencies
cd strix-pydantic
uv sync
cd ..

# Configure your API key (Anthropic recommended for default model)
export ANTHROPIC_API_KEY="your-key-here"

# Run a scan
python -m strix_pydantic --target http://example.com --scan-mode quick

# Or use the installed CLI command
strix-pydantic --target http://example.com --scan-mode standard --verbose
```

**What to expect:**
- Logs are written to the current directory: `./strix_<run_id>.log`
- Scan modes: `quick` (fast), `standard` (balanced), `deep` (thorough)
- Docker sandbox is created automatically and cleaned up after the scan

### Common Options

```bash
# Override model selection
strix-pydantic --target http://example.com --model gpt-4o

# Use mock tools for testing (no Docker required)
strix-pydantic --target http://example.com --mock-tools --verbose

# Interactive phase gates between agent roles
strix-pydantic --target http://example.com --confirm

# Custom agent instruction
strix-pydantic --target http://example.com --instruction "Focus on API authentication"
```

See `python -m strix_pydantic --help` for all available options.

---

## 🛡️ Supported LLM Providers & Model Selection

Strix Pydantic uses automatic model selection based on available API keys, with a clear fallback priority:

### Provider Priority & Default Models

| Provider | API Key | Default Model | Command Override |
|---|---|---|---|
| **Anthropic** | `ANTHROPIC_API_KEY` | `claude-haiku-4-5` | `--model claude-3-5-sonnet` |
| **OpenAI** | `OPENAI_API_KEY` | `gpt-4o-mini` | `--model gpt-4o` |
| **Google** | `GEMINI_API_KEY` or `GOOGLE_API_KEY` | `gemini-3.5-flash` | `--model gemini-2.0-flash` |

### Model Selection Logic

1. **Environment Override**: If `STRIX_LLM` is set, use that exact model string
2. **Key Detection**: Check for API keys in order: Anthropic → OpenAI → Google
3. **Default**: Return the appropriate model for the first available key
4. **Error**: If no API keys are found, the scan will fail with a clear error message

### Specifying Models

The `--model` flag accepts model names with or without provider prefixes:

```bash
# With provider prefix (explicit)
strix-pydantic --target http://example.com --model "anthropic:claude-3-5-sonnet"

# Without prefix (auto-detected from model name pattern)
strix-pydantic --target http://example.com --model "claude-3-5-sonnet"
strix-pydantic --target http://example.com --model "gpt-4o"
strix-pydantic --target http://example.com --model "gemini-2.0-flash"
```

Model name patterns are recognized:
- **Claude models**: Start with `claude-` or contain "claude" → mapped to Anthropic
- **Gemini models**: Contain "gemini" or "flash" → mapped to Google
- **GPT models**: Contain `gpt-`, `gpt4`, `o1-`, `o3-` → mapped to OpenAI

---

## 🏗️ Architecture & Implementation Details

### Pydantic Implementation (Primary Development)
- **Technology Stack**: Pydantic AI v1.102.0 + Pydantic Graph v1.0+ (stable APIs)
- **Core Orchestrator**: Unified `StepContext`-based graph with 3 agent roles
- **State Management**: Immutable `StrixRunState` flowing through graph steps
- **Tool Integration**: Docker sandbox client with context-aware tool filtering (in progress)
- **Skills System**: Markdown-based capability loading from `strix/skills/`

**Key Files**:
- `strix-pydantic/strix_pydantic/interface/cli.py` — CLI entry point with all scan options
- `strix-pydantic/strix_pydantic/agents/pydantic_orchestrator.py` — Graph orchestration logic
- `strix-pydantic/strix_pydantic/config/model_config.py` — Provider detection and model resolution

**Implementation Status**: See [IMPLEMENTATION_STATUS.md](./IMPLEMENTATION_STATUS.md) for detailed progress on core framework, tool integration, and remaining work.

### Go Port
- **Location**: `strix-go/`
- **Status**: Maintenance phase; LLM integration under evaluation
- **Purpose**: High-concurrency binary distribution and performance testing

### Legacy Python
- **Location**: `strix/`
- **Purpose**: Original prototype; skills library is actively used by Pydantic implementation
- **Status**: Reference and skill source only

For detailed architecture, see [ARCHITECTURE.md](./ARCHITECTURE.md).

---

## 🤝 Contributing

We welcome contributions! The project is actively being developed with a focus on the Pydantic implementation.

### Getting Started with Development

```bash
# Install dev dependencies
cd strix-pydantic
uv sync  # Installs all dependencies including dev tools

# Run tests
uv run pytest -v

# Format and lint
uv run ruff format .
uv run ruff check . --fix
```

### Areas for Contribution

- **Tool Integration** (High Priority): Wire sandbox client tool dispatch and implement tool result parsing
- **Testing**: Add unit and integration tests for orchestrator and tool execution
- **Documentation**: Expand skill library documentation and architecture details
- **Go Port Stabilization**: LLM integration improvements and tool binding

See `CONTRIBUTING.md` for detailed workflow guidelines and `IMPLEMENTATION_STATUS.md` for current priorities.

---

> [!WARNING]
> **Legal Notice**: Only test applications you own or have explicit permission to test. Unauthorized security testing is illegal. Use Strix responsibly and ethically.
