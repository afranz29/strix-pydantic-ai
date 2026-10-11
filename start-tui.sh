#!/bin/bash
# Start TUI with backend orchestrator
# Usage: ./start-tui.sh [--mock-tools]

cd "$(dirname "$0")/strix-pydantic"

uv run python -m strix_pydantic.service.tui_client \
  --target localhost \
  --instruction "perform a thorough pentest of the target, scanning common ports and enumerating all services" \
  --scan-mode quick \
  "$@"
