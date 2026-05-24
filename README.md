# Strix (Go Port)

### Go implementation of the Strix security framework

> This is the **Go rewrite** of the Strix security framework. For the production Python implementation, see [usestrix/strix](https://github.com/usestrix/strix).

---

## About This Port

This repository contains the Go port of [Strix](https://github.com/usestrix/strix), an autonomous multi-agent security pentesting framework. The original Python-based Strix established the core architecture and capabilities. This Go implementation aims for feature parity with advantages in:

- **Concurrency**: Go routines and channels for multi-agent orchestration
- **Distribution**: Static binaries without runtime dependencies
- **Performance**: Optimized agent loop and tool execution
- **UI**: Terminal interface with real-time logging and state persistence

### What Changed & What Stayed

**Retained from Python:**
- Backend execution model using containerized sandboxes for tool isolation
- Agent reasoning framework and LLM integration patterns
- Security tool suite (HTTP proxy, browser automation, shell, Python runtime)

**Evolved in Go:**
- Terminal UI rewritten for native Go performance and real-time responsiveness
- CLI architecture simplified and restructured around Go idioms
- Tool execution pipeline optimized for Go concurrency patterns

**Credit & Lineage**

The core concepts, agent reasoning loop, skill-based learning system, and security tool integrations originate from the [original Python implementation](https://github.com/usestrix/strix). This Go port aims to achieve feature parity with potential benefits for performance and distribution.

---

## Current Status

Active development is ongoing on the `goport` branch. Current status:

✅ **Completed:**
- Core agent framework and agentic loop
- Context-aware tool filtering (Sandbox vs. Parent context)
- Robust XML schema parsing for tool definitions
- Per-agent logging and state persistence
- High-performance Terminal UI (scrolling, log sanitization, real-time tailing)
- Essential security tools (HTTP proxy, browser automation, terminal shells, Python runtime)
- LLM provider integrations (OpenAI, Anthropic, Google, Azure, AWS Bedrock)
- Graceful shutdown and sandbox cleanup

🚧 **In Progress:**
- Skill system parity with Python implementation
- Extended tool coverage (OSINT, code analysis, reconnaissance)
- CI/CD integration and automated reporting

---

## Overview

Strix is an autonomous multi-agent security framework that uses AI agents to run code, discover vulnerabilities, and generate proof-of-concepts. It supports multiple LLM providers and executes tools in isolated sandboxes.

**Key Capabilities:**

- **Agent reasoning loop** — Agents coordinate and adapt based on discoveries
- **Sandboxed execution** — Tools run in containers without host impact
- **Proof-of-concept generation** — Validates findings through execution
- **Multi-LLM support** — OpenAI, Anthropic, Google, Azure, Bedrock, local models
- **CLI interface** — Real-time results with logging


## Use Cases

- **Application Security Testing** — Detect and validate vulnerabilities in code
- **Dynamic Testing** — Run code to discover runtime vulnerabilities
- **Proof-of-Concept Generation** — Generate PoCs for validated findings
- **CI/CD Integration** — Run security tests in automated pipelines

## 🚀 Quick Start

**Prerequisites:**
- Go 1.21+ (to build) or a compiled binary
- Docker (running, for sandboxed tool execution)
- An LLM API key from any [supported provider](#supported-llm-providers) (OpenAI, Anthropic, Google, etc.)

### Building from Source

```bash
# Clone and navigate to the Go port
git clone https://github.com/usestrix/strix.git
cd strix/strix-go

# Build the binary
make build

# Configure your LLM provider
export STRIX_LLM="openai/gpt-5.4"
export LLM_API_KEY="your-api-key"

# Run your first security assessment
./strix --target ./path/to/app
```

> [!NOTE]
> First run automatically pulls the sandbox Docker image. Results are saved to `strix_runs/<run-name>/`

## ✨ Key Features

### Agentic Architecture

- **Agent reasoning loop** — Agents discover findings and coordinate execution
- **Multi-agent orchestration** — Multiple agents work on different tasks in parallel
- **Agent coordination** — Agents share context through the framework
- **State logging** — Logs reasoning, tool calls, and results per agent

### Security Testing Capabilities

**Implemented:**
- **HTTP Proxy** — Request/response interception and manipulation
- **Browser Automation** — Browser for XSS, CSRF, and authentication testing
- **Terminal Shell** — Command execution in sandboxed environment
- **Python Runtime** — Python code execution for exploits
- **LLM Integration** — Agent loop using Claude, GPT, Gemini, and other models

**Planned:**
- **Reconnaissance** — OSINT and attack surface discovery
- **Code Analysis** — Vulnerability detection in source code
- **Tool Integration** — Integration with existing security tools
- **Skills** — Framework-specific testing (Rails, Django, NestJS, etc.)

### Supported Vulnerability Classes

- **Access Control** — IDOR, privilege escalation, auth bypass
- **Injection** — SQL, NoSQL, command injection, template injection
- **Server-side** — SSRF, XXE, deserialization
- **Client-side** — XSS, DOM vulnerabilities, prototype pollution
- **Business Logic** — Race conditions, workflow manipulation
- **Authentication** — JWT flaws, session management issues
- **Configuration** — Exposed services, misconfigurations

---

## Supported LLM Providers

- **OpenAI** — GPT-4o, GPT-4 Turbo
- **Anthropic** — Claude Sonnet 4.6, Claude 3.5
- **Google** — Gemini 2 Pro, Vertex AI
- **Azure** — OpenAI models via Azure endpoint
- **AWS Bedrock** — Claude, Llama, Mistral
- **Local Models** — Ollama, LM Studio, other OpenAI-compatible endpoints

## Usage Examples

### Basic Testing

```bash
# Local codebase assessment
./strix --target ./app

# CLI application testing
./strix --target ~/project/cli-app

# Web service assessment
./strix --target http://localhost:3000
```

### With Custom Instructions

```bash
# Focused vulnerability hunt
./strix --target ./app --instruction "Look for IDOR and privilege escalation issues"

# With rules of engagement
./strix --target ./app --instruction-file ./engagement-rules.md
```

### Non-Interactive Mode

For use in CI/CD pipelines:

```bash
# Run without UI, output findings and exit with status
./strix -n --target ./app
```

### Configuration

```bash
# Set LLM provider
export STRIX_LLM="openai/gpt-4o"
export LLM_API_KEY="sk-..."

# Optional: Custom API endpoint (for local models)
export LLM_API_BASE="http://localhost:11434/v1"

# Optional: Control reasoning effort
export STRIX_REASONING_EFFORT="high"
```

Configuration is auto-saved to `~/.strix/cli-config.json` for convenience.

---

## Architecture & Development

For technical details on the Go port architecture, agent design, and tool integration, see:

- [ARCHITECTURE.md](./ARCHITECTURE.md) — System design and component overview
- [PROJECT.md](./PROJECT.md) — Project evolution and roadmap
- [strix-go/](./strix-go/) — Go implementation source code
- [strix/](./strix/) — Original Python implementation (for reference)

## Development Status

Development is ongoing on the `goport` branch. The goal is achieving feature parity with the Python implementation.

**Current priorities:**
1. Skill system implementation (framework-specific testing)
2. Reconnaissance and discovery tools
3. Code analysis capabilities
4. CI/CD pipeline integration
5. Stability and performance optimization

## Getting Involved

- **Found a bug?** [Open an issue](https://github.com/usestrix/strix/issues)
- **Want to contribute?** See [CONTRIBUTING.md](./CONTRIBUTING.md) and start with the `strix-go/` directory
- **Questions?** [Join the Discord](https://discord.gg/strix-ai) or open a discussion

## Acknowledgements

- **Strix Python** — Original architecture and design
- **Go Libraries** — Built on [http-mitm-proxy](https://github.com/owasp-amass/http-mitm-proxy), [PlayWright](https://playwright.dev/go/), [Anthropic SDK](https://github.com/anthropics/anthropic-sdk-go)

---

> [!WARNING]
> **Legal Notice**: Only test applications you own or have explicit permission to test. Unauthorized security testing is illegal. Use Strix responsibly and ethically.

</div>
