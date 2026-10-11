# Project Evolution: Strix

Strix is an autonomous, multi-agent security pentesting framework designed to discover, validate, and remediate application vulnerabilities through agentic reasoning and sandboxed tool execution.

## Core Milestones & Evolution

### 1. Foundation & Python Architecture
The project originated as a Python-based framework, establishing the core concepts of:
- **Agentic Security Loop**: Iterative reasoning using LLMs (GPT-4/5, Claude, Gemini) to drive security tools.
- **Isolated Sandbox**: Execution of offensive tools (Nmap, Sqlmap, Playwright) within Docker containers to ensure host safety.
- **Multi-Agent Coordination**: A "Graph of Agents" architecture allowing specialized agents (e.g., fuzzer, researcher) to collaborate.
- **Skill-Based Learning**: A dynamic system using Markdown playbooks to inject specialized security knowledge into agent prompts.

### 2. Operational Maturity (v0.8.x)
Recent updates focused on performance, observability, and developer experience:
- **Dependency Management**: Migrated from Poetry to `uv` for significantly faster environment setup and execution.
- **Observability**: Integrated OpenTelemetry for local tracing of agent reasoning and tool execution.
- **Performance**: Optimized the agent loop to "wake on state change" rather than relying on fixed polling intervals.
- **Vulnerability Coverage**: Expanded the "Skills" library to include Kubernetes security, NoSQL injection, HTTP request smuggling, and specialized framework testing (NestJS).

### 3. The Go Transition (`goport` branch)
The project is currently undergoing a strategic port of the core architecture to Go (`strix-go`). This evolution aims to improve performance, concurrency handling, and binary distribution.

**Key achievements in the Go port:**
- **Core Framework Port**: Re-implementation of the agent framework and core architecture in Go.
- **Persistence & Logging**: Implementation of robust per-agent logging and state persistence.
- **Advanced TUI**: Development of a high-performance Terminal User Interface with support for scrolling, log sanitization, and real-time tailing.
- **Core Tooling**: Porting of essential security tools to the Go runtime.

## Technical Architecture Evolution

| Feature | Python Implementation (Original) | Go Implementation (`strix-go`) |
| :--- | :--- | :--- |
| **Core Loop** | Asyncio-based loop in `base_agent.py` | Native Go routines and channels |
| **Tool Execution** | FastAPI-based `tool_server` in Docker | Optimized Go tool runner |
| **State Management** | Pydantic-based state objects | Type-safe Go structs with persistence |
| **User Interface** | Textual (Python TUI) | High-performance Go TUI |
| **Distribution** | PyPI / Docker | Static binaries / Docker |

## Current Strategic Focus
The immediate focus is the completion of the `strix-go` port, ensuring parity with the Python implementation's extensive skill set and tool integrations while leveraging Go's performance for more complex multi-agent simulations.
