#!/bin/sh
set -eu
cd "$(dirname "$0")/../.."
umask 077
python3 -m venv .harness/agenttrace-venv
.harness/agenttrace-venv/bin/python -m pip install --no-cache-dir -r integrations/agenttrace/requirements.txt
printf '%s\n' 'AgentTrace export environment ready in .harness/agenttrace-venv'
