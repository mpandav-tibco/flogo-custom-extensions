#!/usr/bin/env bash
# =============================================================================
# run-tests.sh — Live integration tests for VectorDB connector
# Tests all 4 providers: Qdrant, Weaviate, Chroma, Milvus
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINDIR="$SCRIPT_DIR/bin"
LOGDIR="/tmp/vdb-test-logs"
PASS=0; FAIL=0; SKIP=0

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; RESET='\033[0m'
ok()   { echo -e "  ${GREEN}✓ PASS${RESET}  $*"; ((PASS++)); }
fail() { echo -e "  ${RED}✗ FAIL${RESET}  $*"; ((FAIL++)); }
skip() { echo -e "  ${YELLOW}⚠ SKIP${RESET}  $*"; ((SKIP++)); }
info() { echo -e "  ───  $*"; }

mkdir -p "$LOGDIR"
APP_PIDS=()

cleanup() {
  echo ""
  info "Stopping test apps…"
  for pid in "${APP_PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup EXIT

# ─── 1. Start Docker containers ───────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║   VectorDB Connector – Live Integration Test  ║"
echo "╚══════════════════════════════════════════════╝"
echo ""
echo "► Starting Docker containers…"

if docker compose -f "$SCRIPT_DIR/docker-compose.yml" up -d 2>&1 | grep -q "error\|Error"; then
  echo "  WARNING: docker compose had errors. Continuing with whatever is running."
else
  info "Docker compose started"
fi

# Wait for healthchecks
info "Waiting 15s for containers to be ready…"
sleep 15

# ─── 2. Start Flogo test apps ─────────────────────────────────────────────────
echo ""
echo "► Starting Flogo test apps…"

declare -A APP_PORTS=(
  ["qdrant"]=18081
  ["weaviate"]=18082
  ["chroma"]=18083
  ["milvus"]=18084
)

for db in qdrant weaviate chroma milvus; do
  bin="$BINDIR/${db}-connector-test"
  log="$LOGDIR/${db}.log"
  port="${APP_PORTS[$db]}"

  if [[ ! -x "$bin" ]]; then
    skip "$db binary not found at $bin — run the build first"
    continue
  fi

  "$bin" > "$log" 2>&1 &
  APP_PIDS+=($!)
  info "Started $db-connector-test (PID $!, port $port, log $log)"
done

echo ""
info "Waiting 8s for apps to initialize…"
sleep 8

# ─── 3. Test helper ───────────────────────────────────────────────────────────
run_test() {
  local db=$1 port=$2 endpoint=$3 label=$4
  local url="http://localhost:${port}/api/test/${endpoint}"
  local resp
  resp=$(curl -s --max-time 30 "$url" 2>&1) || { fail "$db [$label] — curl error"; return; }

  local http_err
  http_err=$(echo "$resp" | python3 -c "import sys,json
d=json.load(sys.stdin)
# accept if no 'error' field or error is empty
err=d.get('error','')
if err:
    print(err)
else:
    # check nested result fields
    for v in d.values():
        if isinstance(v,dict) and v.get('error'):
            print(v['error'])
            break
" 2>/dev/null) || true

  if [[ -z "$http_err" ]]; then
    ok "$db [$label]"
    echo "     $(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); [print(f'       {k}: {str(v)[:80]}') for k,v in d.items()]" 2>/dev/null || echo "     $resp" | head -2)"
  else
    fail "$db [$label] — $http_err"
    echo "     Response: $(echo "$resp" | head -c 200)"
  fi
}

# ─── 4. Run per-provider tests ────────────────────────────────────────────────
echo ""
echo "─────────────────────────────────────────────────"
echo " QDRANT (port 18081)"
echo "─────────────────────────────────────────────────"
run_test qdrant 18081 crud   "CRUD cycle"
run_test qdrant 18081 rag    "RAG cycle"
run_test qdrant 18081 embed  "Embeddings"
run_test qdrant 18081 rerank "Rerank"

echo ""
echo "─────────────────────────────────────────────────"
echo " WEAVIATE (port 18082)"
echo "─────────────────────────────────────────────────"
run_test weaviate 18082 crud   "CRUD + Hybrid"
run_test weaviate 18082 rag    "RAG + HybridRAG"
run_test weaviate 18082 embed  "Embeddings"
run_test weaviate 18082 rerank "Rerank"

echo ""
echo "─────────────────────────────────────────────────"
echo " CHROMA (port 18083)"
echo "─────────────────────────────────────────────────"
run_test chroma 18083 crud   "CRUD cycle"
run_test chroma 18083 rag    "RAG cycle"
run_test chroma 18083 embed  "Embeddings"
run_test chroma 18083 rerank "Rerank"

echo ""
echo "─────────────────────────────────────────────────"
echo " MILVUS (port 18084)"
echo "─────────────────────────────────────────────────"
run_test milvus 18084 crud   "CRUD cycle"
run_test milvus 18084 rag    "RAG cycle"
run_test milvus 18084 embed  "Embeddings"
run_test milvus 18084 rerank "Rerank"

# ─── 5. Summary ───────────────────────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════╗"
echo "║               TEST SUMMARY                    ║"
printf "║  %-44s║\n" "PASS: $PASS   FAIL: $FAIL   SKIP: $SKIP"
echo "╚══════════════════════════════════════════════╝"
echo ""
echo "Logs: $LOGDIR/"
if [[ $FAIL -gt 0 ]]; then
  exit 1
fi
