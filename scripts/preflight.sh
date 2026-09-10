#!/usr/bin/env bash
# T2F deployment preflight — the smallest possible check that catches an
# obviously broken deployment BEFORE pushing to GitHub/Vercel. Runs no
# external providers and makes no network calls beyond what the existing
# build/test commands already do locally.
#
# Usage: bash scripts/preflight.sh [--env-file path/to/.env]
#
# Checks, in order:
#   1. Web compiles (npm run build in apps/web)
#   2. Go backend builds (go build ./...)
#   3. Python AI service imports/starts structurally (py_compile + FastAPI
#      app construction against the mock/dev Settings, never a real network
#      call)
#   4. If --env-file is given: every name it defines is checked against the
#      three .env.example files (root/Go, ai-service, apps/web) for obvious
#      drift — names present in the real env file but undocumented in any
#      .env.example, which usually means either a typo or an undocumented
#      new variable.
#
# This is deliberately NOT a deployment framework: no provisioning, no
# secret handling beyond reading local files, no calls to Vercel/GitHub.

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

ENV_FILE=""
if [[ "${1:-}" == "--env-file" ]]; then
  ENV_FILE="${2:?--env-file requires a path}"
fi

echo "== T2F preflight =="

echo "-- [1/4] Web build (apps/web) --"
(cd apps/web && npm run build)

echo "-- [2/4] Go backend build --"
(cd backend && go build ./...)

echo "-- [3/4] Python AI service structural check --"
(cd ai-service && python -c "
import py_compile, pathlib
for f in pathlib.Path('app').rglob('*.py'):
    py_compile.compile(str(f), doraise=True)
from app.config import Settings
from app.main import create_app
create_app(Settings(INTERNAL_API_TOKEN='preflight-check-token'))
print('ai-service: imports and app construction OK (mock providers, no network calls)')
")

if [[ -n "$ENV_FILE" ]]; then
  echo "-- [4/4] Env name check against .env.example --"
  python3 - "$ENV_FILE" <<'PYEOF'
import re
import sys

env_file = sys.argv[1]

def names_in(path):
    names = set()
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.lstrip("# ").strip()
                m = re.match(r"^([A-Z][A-Z0-9_]*)=", line)
                if m:
                    names.add(m.group(1))
    except FileNotFoundError:
        pass
    return names

documented = names_in(".env.example") | names_in("ai-service/.env.example") | names_in("apps/web/.env.local.example")
actual = names_in(env_file)

undocumented = sorted(actual - documented)
if undocumented:
    print("WARNING: variables present in", env_file, "but not in any .env.example:")
    for name in undocumented:
        print("  -", name)
    sys.exit(1)
print("env-file check OK: every variable name is documented in a .env.example")
PYEOF
else
  echo "-- [4/4] Env name check skipped (no --env-file given) --"
fi

echo "== preflight OK =="
