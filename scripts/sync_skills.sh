#!/usr/bin/env bash
# Resync strix/skills/ Markdown content from upstream usestrix/strix.
#
# Ownership boundary:
#   - Upstream-managed: every *.md file under strix/skills/ (including README.md).
#   - Locally managed: strix/skills/__init__.py and strix-pydantic's own loader
#     (strix_pydantic/skills/skill_capability_factory.py). Never touched by this script.
#
# Usage:
#   scripts/sync_skills.sh [ref]
#
# [ref] is any usestrix/strix git ref (branch, tag, or commit SHA). Defaults to
# the SHA this repo is currently pinned to (see below). After running, review
# the diff, update PINNED_SHA in this script, and run the strix-pydantic test
# suite before committing.

set -euo pipefail

REPO="usestrix/strix"
PINNED_SHA="f7a32ae62d94872631bc8415073868d02a6ffdaf"
REF="${1:-$PINNED_SHA}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEST_ROOT="$REPO_ROOT/strix/skills"

command -v gh >/dev/null || { echo "gh CLI is required" >&2; exit 1; }

echo "Syncing strix/skills/ from ${REPO}@${REF}..."

paths=$(gh api "repos/${REPO}/git/trees/${REF}?recursive=1" \
  --jq '.tree[] | select(.path | startswith("strix/skills/")) | select(.type=="blob") | .path')

while IFS= read -r path; do
  case "$path" in
    *__init__.py|*.gitkeep|"") continue ;;
  esac
  dest="$REPO_ROOT/${path#strix/}"
  mkdir -p "$(dirname "$dest")"
  gh api "repos/${REPO}/contents/${path}?ref=${REF}" --jq '.content' | base64 -d > "$dest"
  echo "  wrote ${path#strix/}"
done <<< "$paths"

echo
echo "Done. Review 'git status'/'git diff' under strix/skills/, then update PINNED_SHA in"
echo "this script to ${REF} and run 'uv run pytest' from strix-pydantic/ before committing."
