#!/bin/zsh
set -e
PROOFLAB_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$PROOFLAB_ROOT"
export PATH="$HOME/.local/bin:/opt/homebrew/bin:$PATH"
if curl -fsS http://127.0.0.1:8765/api/health 2>/dev/null | python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("app")=="ProofLab" else 1)' 2>/dev/null; then
  open http://127.0.0.1:8765
  exit 0
fi
make bootstrap
make agent-bootstrap
make build
PYTHONPATH=agents .venv-agent/bin/python -m portfolio_agent.web --open --codex-bin /Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex
