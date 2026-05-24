# Strix

### Autonomous Multi-Agent Security Framework

Strix is an AI-powered security pentesting framework that coordinates multiple agents to discover, validate, and exploit vulnerabilities in an isolated sandbox.

---

## 🚀 Project Evolution: The Pydantic AI Pivot

Strix is transitioning its core orchestration to a new flagship implementation based on **[Pydantic AI](https://github.com/pydantic/pydantic-ai)**. This version leverages the robust type safety of Pydantic and the sophisticated orchestration of `pydantic-graph` to provide the most reliable and extensible version of the framework to date.

### Current Implementations

| Implementation | Status | Path | Description |
|---|---|---|---|
| **Strix Pydantic** | **Flagship (Active)** | `strix-pydantic/` | Next-gen Python implementation. Features graph-based orchestration, structured extraction, and strict type safety. |
| **Strix Go** | Maintenance | `strix-go/` | High-concurrency port. Focused on binary distribution and performance. |
| **Strix Legacy** | Legacy | `strix/` | The original Python prototype and research codebase. |

---

## ✨ Key Features (Pydantic Implementation)

- **Graph-Based Orchestration** — Sophisticated multi-agent workflows using `pydantic-graph` for resilient and traceable scan logic.
- **Structured Findings** — Native Pydantic model extraction for vulnerabilities, notes, and evidence.
- **Type-Safe Tooling** — End-to-end validation of tool arguments and results.
- **Sandbox Isolation** — Automatic Docker sandbox lifecycle management for safe tool execution.
- **Context-Aware Tools** — Intelligent tool filtering based on agent role and execution environment (Sandbox vs. Host).
- **Multi-LLM Support** — Native integration with Anthropic, OpenAI, Google Gemini, and more.

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

# Install flagship dependencies
cd strix-pydantic
uv sync
cd ..

# Configure your API key
export ANTHROPIC_API_KEY="your-key-here"

# Run a scan using the root wrapper
./strix.sh --target http://example.com --scan-mode quick
```

> [!TIP]
> Use `./strix.sh --help` to see all available options including `--confirm` for interactive phase gates and `--verbose` for detailed agent reasoning.

---

## 🛡️ Supported LLM Providers

Strix Pydantic supports a wide range of providers through Pydantic AI's unified interface:

- **Anthropic** (Preferred) — Claude 3.5 Sonnet, Claude 3 Opus
- **Google** — Gemini 1.5 Pro/Flash, Gemini 2.0
- **OpenAI** — GPT-4o, GPT-4 Turbo
- **Local Models** — Support via Ollama or vLLM (OpenAI-compatible endpoints)

The system automatically selects the best available model based on your environment keys (defaulting to `claude-haiku-4-5` if available, or `gemini-2.0-flash`).

---

## 🏗️ Architecture

For technical details on the various implementations, see:

- **[Pydantic Implementation (Flagship)](./strix-pydantic/README.md)** — Current focus and architecture.
- **[Go Port](./strix-go/README.md)** — High-concurrency design and performance notes.
- **[Legacy Python](./strix/README.md)** — Original concepts and skill system.

---

## 🤝 Contributing

We welcome contributions across all implementations! 

- **Bug Reports**: Open an issue labeled with the relevant implementation (e.g., `area/pydantic`, `area/go`).
- **Feature Requests**: Use the feature request template.
- **Development**: See `CONTRIBUTING.md` for workflow guidelines.

---

> [!WARNING]
> **Legal Notice**: Only test applications you own or have explicit permission to test. Unauthorized security testing is illegal. Use Strix responsibly and ethically.
