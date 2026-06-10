#!/bin/bash
# Debug client wrapper to test backend event streaming

cd "$(dirname "$0")/strix-pydantic"

uv run python -m strix_pydantic.service.debug_client \
  --target localhost \
  --instruction "perform a thorough pentest of the target, scanning common ports and enumerating all services" \
  --scan-mode quick
