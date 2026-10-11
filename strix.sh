#!/bin/bash
set -e

# Get the directory of this script to locate strix-pydantic relative to it
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Run strix-pydantic inside its project using uv run, passing all arguments
if command -v uv >/dev/null 2>&1; then
    exec uv run --project "$SCRIPT_DIR/strix-pydantic" strix-pydantic "$@"
elif [ -f "$SCRIPT_DIR/strix-pydantic/.venv/bin/strix-pydantic" ]; then
    exec "$SCRIPT_DIR/strix-pydantic/.venv/bin/strix-pydantic" "$@"
elif [ -f "$SCRIPT_DIR/strix-pydantic/.venv/bin/python" ]; then
    exec "$SCRIPT_DIR/strix-pydantic/.venv/bin/python" -m strix_pydantic "$@"
else
    echo "Error: 'uv' is not installed and strix-pydantic virtual environment was not found." >&2
    echo "Please install 'uv' or set up the virtual environment in '$SCRIPT_DIR/strix-pydantic/.venv'." >&2
    exit 1
fi
