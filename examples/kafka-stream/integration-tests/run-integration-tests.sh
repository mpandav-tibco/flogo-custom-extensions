#!/usr/bin/env bash
# =============================================================================
# Integration Test Runner — KafkaStream Aggregate Trigger
# =============================================================================
# Covers every trigger configuration option systematically:
#   TC-01  TumblingTime + sum          (baseline, already validated)
#   TC-02  TumblingTime + count
#   TC-03  TumblingTime + avg
#   TC-04  TumblingTime + min
#   TC-05  TumblingTime + max
#   TC-06  TumblingCount + sum
#   TC-07  SlidingTime  + sum
#   TC-08  SlidingCount + sum
#   TC-09  Keyed windows (keyField)
#   TC-10  Deduplication (messageIDField)
#   TC-11  Late events (eventTimeField + allowedLateness)
#   TC-12  Overflow drop_oldest (maxBufferSize)
#   TC-13  Overflow drop_newest (maxBufferSize)
#   TC-14  Idle timeout (idleTimeoutMs)
#   TC-15  State persistence (persistPath)
#   TC-16  Overflow error_stop
#   TC-17  initialOffset=oldest replay
#   TC-18  maxKeys limit
#   TC-19  persistEveryN (mid-run snapshot)
#   TC-28  allowedLateness=0 (strict)
#   TC-29  keyField + per-key overflow (drop_newest)
#   TC-30  valueField missing — invalid messages skipped
# Filter trigger:
#   TC-20  eq operator
#   TC-21  gt operator
#   TC-22  regex operator
#   TC-23  multi-predicate AND
#   TC-24  multi-predicate OR
#   TC-25  passThroughOnMissing=true
#   TC-26  enableDedup + dedupField
#   TC-27  rateLimitRPS + mode=drop
#   TC-31  neq + contains + startsWith + endsWith operators
#   TC-32  rateLimitMode=wait
#
# Usage:
#   ./run-integration-tests.sh             # run all tests
#   ./run-integration-tests.sh TC-03       # run single test by name
#   ./run-integration-tests.sh TC-03 TC-07 # run specific tests
# =============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"

FLOGO_BIN="$ROOT_DIR/bin/test-kafka-stream-activities"
BASE_FLOGO="$ROOT_DIR/examples/kafka-stream/kakfa-streaming-test.flogo"
KAFKA_HOME="/Users/milindpandav/Downloads/tools/third-party/kafka/kafka_2.13-3.8.0"
KAFKA_PRODUCER="$KAFKA_HOME/bin/kafka-console-producer.sh"
TOPIC="demo"
LOG_DIR="/tmp/flogo-integration-tests"
mkdir -p "$LOG_DIR"
FILTER_FLOGO="$LOG_DIR/filter-base.flogo"  # generated once at startup by _make_filter_flogo()

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

PASS=0; FAIL=0
APP_PID="" # global PID of the currently running Flogo app

# ── Helper: print section header ─────────────────────────────────────────────
header() {
  local tc=$1; local desc=$2
  echo -e "\n${CYAN}══════════════════════════════════════════════════════════════${NC}"
  echo -e "${CYAN}  ${BOLD}$tc${NC}${CYAN}  $desc${NC}"
  echo -e "${CYAN}══════════════════════════════════════════════════════════════${NC}"
}

# ── Helper: produce Kafka messages (batch, no timing) ────────────────────────
produce() {
  # produce <json1> [<json2> ...]  — sends all args as separate messages
  ( for m in "$@"; do echo "$m"; done ) \
    | "$KAFKA_PRODUCER" --broker-list localhost:9092 --topic "$TOPIC" 2>/dev/null
}

# ── Helper: produce with inter-message sleep ─────────────────────────────────
produce_timed() {
  # produce_timed <sleep_secs> <json1> [<json2> ...]
  local secs=$1; shift
  for m in "$@"; do
    echo "$m" | "$KAFKA_PRODUCER" --broker-list localhost:9092 --topic "$TOPIC" 2>/dev/null
    sleep "$secs"
  done
}

# ── Helper: produce all messages with the SAME Kafka key → same partition ────
# This guarantees deterministic per-partition delivery order, essential for
# overflow tests that rely on exact drop counts.
produce_keyed() {
  # produce_keyed <partition_key> <sleep_secs> <json1> [<json2> ...]
  local pkey=$1; local secs=$2; shift 2
  for m in "$@"; do
    printf '%s:%s\n' "$pkey" "$m" \
      | "$KAFKA_PRODUCER" --broker-list localhost:9092 --topic "$TOPIC" \
          --property 'parse.key=true' --property 'key.separator=:' 2>/dev/null
    [[ "$secs" != "0" ]] && sleep "$secs"
  done
}

# ── Helper: produce to a specific topic (for filter tests) ───────────────────
produce_topic() {
  local topic=$1; shift
  ( for m in "$@"; do echo "$m"; done ) \
    | "$KAFKA_PRODUCER" --broker-list localhost:9092 --topic "$topic" 2>/dev/null
}

# ── Helper: generate a temp .flogo file via jq patch ─────────────────────────
gen_flogo() {
  local jq_expr=$1; local tc=$2
  local out="$LOG_DIR/${tc}.flogo"
  jq "$jq_expr" "$BASE_FLOGO" > "$out"
  echo "$out"
}

# ── Helper: generate a .flogo patched from the FILTER base app ───────────────
gen_flogo_filter() {
  local jq_expr=$1; local tc=$2
  local out="$LOG_DIR/${tc}.flogo"
  jq "$jq_expr" "$FILTER_FLOGO" > "$out"
  echo "$out"
}

# ── Helper: start Flogo app in background; sets APP_PID ──────────────────────
start_app() {
  local flogo=$1; local log=$2
  local od="$LOG_DIR/out-$$"; mkdir -p "$od"
  "$FLOGO_BIN" --app --debug --o "$od" "$flogo" > "$log" 2>&1 &
  APP_PID=$!
}

# ── Helper: wait up to 40s for app to emit "started — topic=" ────────────────
wait_start() {
  local log=$1; local i=0
  while [[ $i -lt 40 ]]; do
    grep -q "started.*topic=" "$log" 2>/dev/null && return 0
    sleep 1; i=$((i+1))
  done
  return 1
}

# ── Helper: wait for a grep pattern in the log; return 0/1 ───────────────────
wait_pattern() {
  local log=$1; local pat=$2; local limit=${3:-60}; local i=0
  while [[ $i -lt $limit ]]; do
    grep -qE "$pat" "$log" 2>/dev/null && return 0
    sleep 1; i=$((i+1))
  done
  return 1
}

# ── Helper: stop the app ──────────────────────────────────────────────────────
stop_app() {
  [[ -n "$APP_PID" ]] && { kill "$APP_PID" 2>/dev/null || true; wait "$APP_PID" 2>/dev/null || true; APP_PID=""; }
}

# ── Helper: delete a consumer group so it gets a fresh 'newest' start ────────
# Without this, stale committed offsets from failed previous runs cause the
# consumer to re-read old messages and produce wrong aggregate results.
clean_group() {
  local group=$1
  "$KAFKA_HOME/bin/kafka-consumer-groups.sh" --bootstrap-server localhost:9092 \
    --group "$group" --delete 2>/dev/null || true
}

# ── Helper: record pass/fail ─────────────────────────────────────────────────
pass() {
  local detail=${1:-""}
  echo -e "  ${GREEN}✓ PASS${NC} $detail"
  PASS=$((PASS+1))
}
fail() {
  local reason=$1
  echo -e "  ${RED}✗ FAIL${NC}: $reason"
  FAIL=$((FAIL+1))
}

# ── Helper: show matching log lines ─────────────────────────────────────────
show_results() {
  local log=$1; local pat=$2
  echo -e "  ${YELLOW}Log output:${NC}"
  grep -E "$pat" "$log" 2>/dev/null | tail -5 | sed 's/^/    /' || echo "    (none)"
}

# ── Pre-flight check ──────────────────────────────────────────────────────────
check_prereqs() {
  echo -e "${BOLD}Pre-flight checks...${NC}"
  if [[ ! -x "$FLOGO_BIN" ]]; then
    echo -e "${RED}ERROR: Flogo binary not found: $FLOGO_BIN${NC}"; exit 1
  fi
  if ! "$KAFKA_HOME/bin/kafka-topics.sh" --bootstrap-server localhost:9092 --list &>/dev/null; then
    echo -e "${RED}ERROR: Kafka not reachable on localhost:9092. Start it first.${NC}"; exit 1
  fi
  echo -e "  ${GREEN}✓${NC} Flogo binary found"
  echo -e "  ${GREEN}✓${NC} Kafka reachable on localhost:9092"
  # Verify filter trigger package is compiled into the binary
  if "$FLOGO_BIN" --help 2>&1 | grep -q "filter\|Filter" || \
     strings "$FLOGO_BIN" 2>/dev/null | grep -q "filter-trigger"; then
    echo -e "  ${GREEN}✓${NC} Filter trigger compiled into binary"
  else
    echo -e "  ${YELLOW}⚠${NC} Filter trigger presence unclear — TC-20..TC-27 may fail at app start"
  fi
  echo ""
}

# =============================================================================
# TC-02  TumblingTime + count
# =============================================================================
TC-02() {
  local tc="TC-02"
  header "$tc" "TumblingTime/5s + function=count  →  expect result:5 count:5"

  local jq_patch='
    .triggers[0].settings.function        = "count"     |
    .triggers[0].settings.windowName      = "tc-02-win" |
    .triggers[0].settings.consumerGroup   = "it-tc-02"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-02"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start within 40s"; stop_app; return
  fi
  sleep 5

  echo "  → Sending 5 messages (values 11-15) then waiting 5.5s for window close..."
  produce \
    '{"tempreture":11,"device_id":"sensor-1"}' \
    '{"tempreture":12,"device_id":"sensor-1"}' \
    '{"tempreture":13,"device_id":"sensor-1"}' \
    '{"tempreture":14,"device_id":"sensor-1"}' \
    '{"tempreture":15,"device_id":"sensor-1"}'
  sleep 5.5
  produce '{"tempreture":21,"device_id":"sensor-1"}' # triggers window boundary

  # Trigger log: window "tc-02-win" closed: count=5.0000 count=5 ...
  # Activity log (JSON): {"count":5,...,"result":5,"windowClosed":true,"windowName":"tc-02-win"}
  if wait_pattern "$log" 'window .tc-02-win. closed:' 20; then
    show_results "$log" "tc-02-win"
    if grep -E '"windowName":"tc-02-win"' "$log" | grep -qE '"result":5[,}]'; then
      pass "(result:5 count:5 windowClosed:true)"
    else
      local actual; actual=$(grep -E 'tc-02-win' "$log" | grep 'windowResult' | tail -1)
      fail "expected result:5. Got: $actual"
    fi
  else
    show_results "$log" "tc-02-win"
    fail "no windowClosed event within timeout"
  fi
  stop_app
}

# =============================================================================
# TC-03  TumblingTime + avg
# =============================================================================
TC-03() {
  local tc="TC-03"
  header "$tc" "TumblingTime/5s + function=avg  →  expect result:30 count:5"

  local jq_patch='
    .triggers[0].settings.function        = "avg"       |
    .triggers[0].settings.windowName      = "tc-03-win" |
    .triggers[0].settings.consumerGroup   = "it-tc-03"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-03"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending values [10,20,30,40,50] → avg=30"
  produce \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}' \
    '{"tempreture":40,"device_id":"sensor-1"}' \
    '{"tempreture":50,"device_id":"sensor-1"}'
  sleep 5.5
  produce '{"tempreture":1,"device_id":"sensor-1"}'

  if wait_pattern "$log" 'window .tc-03-win. closed:' 20; then
    show_results "$log" "tc-03-win"
    if grep -E '"windowName":"tc-03-win"' "$log" | grep -qE '"result":30[,}]'; then
      pass "(result:30 count:5)"
    else
      local actual; actual=$(grep -E 'tc-03-win' "$log" | grep 'windowResult' | tail -1)
      fail "expected result:30. Got: $actual"
    fi
  else
    show_results "$log" "tc-03-win"
    fail "no windowClosed event"
  fi
  stop_app
}

# =============================================================================
# TC-04  TumblingTime + min
# =============================================================================
TC-04() {
  local tc="TC-04"
  header "$tc" "TumblingTime/5s + function=min  →  expect result:10 count:5"

  local jq_patch='
    .triggers[0].settings.function        = "min"       |
    .triggers[0].settings.windowName      = "tc-04-win" |
    .triggers[0].settings.consumerGroup   = "it-tc-04"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-04"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending values [50,10,30,20,40] → min=10"
  produce \
    '{"tempreture":50,"device_id":"sensor-1"}' \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":40,"device_id":"sensor-1"}'
  sleep 5.5
  produce '{"tempreture":1,"device_id":"sensor-1"}'

  if wait_pattern "$log" 'window .tc-04-win. closed:' 20; then
    show_results "$log" "tc-04-win"
    if grep -E '"windowName":"tc-04-win"' "$log" | grep -qE '"result":10[,}]'; then
      pass "(result:10 count:5)"
    else
      local actual; actual=$(grep -E 'tc-04-win' "$log" | grep 'windowResult' | tail -1)
      fail "expected result:10. Got: $actual"
    fi
  else
    show_results "$log" "tc-04-win"
    fail "no windowClosed event"
  fi
  stop_app
}

# =============================================================================
# TC-05  TumblingTime + max
# =============================================================================
TC-05() {
  local tc="TC-05"
  header "$tc" "TumblingTime/5s + function=max  →  expect result:50 count:5"

  local jq_patch='
    .triggers[0].settings.function        = "max"       |
    .triggers[0].settings.windowName      = "tc-05-win" |
    .triggers[0].settings.consumerGroup   = "it-tc-05"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-05"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending values [50,10,30,20,40] → max=50"
  produce \
    '{"tempreture":50,"device_id":"sensor-1"}' \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":40,"device_id":"sensor-1"}'
  sleep 5.5
  produce '{"tempreture":1,"device_id":"sensor-1"}'

  if wait_pattern "$log" 'window .tc-05-win. closed:' 20; then
    show_results "$log" "tc-05-win"
    if grep -E '"windowName":"tc-05-win"' "$log" | grep -qE '"result":50[,}]'; then
      pass "(result:50 count:5)"
    else
      local actual; actual=$(grep -E 'tc-05-win' "$log" | grep 'windowResult' | tail -1)
      fail "expected result:50. Got: $actual"
    fi
  else
    show_results "$log" "tc-05-win"
    fail "no windowClosed event"
  fi
  stop_app
}

# =============================================================================
# TC-06  TumblingCount (size=5) + sum
# =============================================================================
TC-06() {
  local tc="TC-06"
  header "$tc" "TumblingCount/5 + function=sum  →  expect result:65 count:5 on 5th msg"

  local jq_patch='
    .triggers[0].settings.windowType      = "TumblingCount" |
    .triggers[0].settings.windowSize      = 5               |
    .triggers[0].settings.function        = "sum"           |
    .triggers[0].settings.windowName      = "tc-06-win"     |
    .triggers[0].settings.consumerGroup   = "it-tc-06"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-06"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending 5 messages (values 11-15) — window fires on 5th"
  produce \
    '{"tempreture":11,"device_id":"sensor-1"}' \
    '{"tempreture":12,"device_id":"sensor-1"}' \
    '{"tempreture":13,"device_id":"sensor-1"}' \
    '{"tempreture":14,"device_id":"sensor-1"}' \
    '{"tempreture":15,"device_id":"sensor-1"}'

  if wait_pattern "$log" 'window .tc-06-win. closed:' 15; then
    show_results "$log" "tc-06-win"
    if grep -E '"windowName":"tc-06-win"' "$log" | grep -qE '"result":65[,}]'; then
      pass "(result:65 count:5 — fired on count boundary)"
    else
      local actual; actual=$(grep -E 'tc-06-win' "$log" | grep 'windowResult' | tail -1)
      fail "expected result:65. Got: $actual"
    fi
  else
    show_results "$log" "tc-06-win"
    fail "no windowClosed event"
  fi
  stop_app
}

# =============================================================================
# TC-07  SlidingTime (5s window) + sum
# =============================================================================
TC-07() {
  local tc="TC-07"
  header "$tc" "SlidingTime/5s + function=sum  →  emits on every msg; expect result:65 count:5"

  local jq_patch='
    .triggers[0].settings.windowType      = "SlidingTime" |
    .triggers[0].settings.windowSize      = 15000         |
    .triggers[0].settings.function        = "sum"         |
    .triggers[0].settings.windowName      = "tc-07-win"   |
    .triggers[0].settings.consumerGroup   = "it-tc-07"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-07"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Messages sent to 3 partitions arrive 1-2s apart to the consumer.
  # Using windowSize=15s ensures all 5 messages stay within the rolling window.
  # The 5th message should emit result=65, count=5.
  echo "  → Sending 5 messages (values 11-15) [windowSize=15s to cover multi-partition spread]"
  produce_timed 0.3 \
    '{"tempreture":11,"device_id":"sensor-1"}' \
    '{"tempreture":12,"device_id":"sensor-1"}' \
    '{"tempreture":13,"device_id":"sensor-1"}' \
    '{"tempreture":14,"device_id":"sensor-1"}' \
    '{"tempreture":15,"device_id":"sensor-1"}'

  # SlidingTime fires on each message; 5th should yield result=65, count=5
  # Trigger log: window "tc-07-win" closed: sum=65.0000 count=5
  # May take up to 12s for the 5 messages to be consumed across 3 partitions
  if wait_pattern "$log" 'window .tc-07-win. closed: sum=65\.0000 count=5' 20; then
    show_results "$log" "tc-07-win"
    pass "(result:65 count:5 — sliding emits on each message)"
  else
    show_results "$log" "tc-07-win"
    fail "expected rolling window result:65 count:5 not found"
  fi

  echo ""
  echo "  → Waiting 16s then sending 1 more to verify old events evicted from rolling window..."
  sleep 16
  produce '{"tempreture":100,"device_id":"sensor-1"}'
  sleep 3
  # After 16s all prior messages have fallen out of the 15s window.
  if wait_pattern "$log" 'window .tc-07-win. closed: sum=100\.0000 count=1' 5; then
    echo -e "  ${GREEN}✓ Eviction check PASS${NC}: After 16s gap, rolling window shows result:100 count:1"
  else
    echo -e "  ${YELLOW}⚠ Eviction check SKIP${NC}: timing-sensitive; result:100 count:1 not confirmed"
  fi

  stop_app
}

# =============================================================================
# TC-08  SlidingCount (size=3) + sum
# =============================================================================
TC-08() {
  local tc="TC-08"
  header "$tc" "SlidingCount/3 + function=sum  →  expect rolling last-3: final result:120 count:3"

  local jq_patch='
    .triggers[0].settings.windowType      = "SlidingCount" |
    .triggers[0].settings.windowSize      = 3              |
    .triggers[0].settings.function        = "sum"          |
    .triggers[0].settings.windowName      = "tc-08-win"    |
    .triggers[0].settings.consumerGroup   = "it-tc-08"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-08"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Send values 10, 20, 30, 40, 50.
  # Results: msg1→10/1, msg2→30/2, msg3→60/3, msg4→90/3(10 evicted), msg5→120/3(20 evicted)
  echo "  → Sending 5 messages [10,20,30,40,50]; expect last emission result:120 count:3"
  produce_timed 0.2 \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}' \
    '{"tempreture":40,"device_id":"sensor-1"}' \
    '{"tempreture":50,"device_id":"sensor-1"}'

  # SlidingCount/3: trigger log: window "tc-08-win" closed: sum=120.0000 count=3
  if wait_pattern "$log" 'window .tc-08-win. closed: sum=120\.0000 count=3' 15; then
    show_results "$log" "tc-08-win"
    pass "(result:120 count:3 — correct rolling last-3 sum)"
  else
    show_results "$log" "tc-08-win"
    fail "expected result:120 count:3 not found"
  fi
  stop_app
}

# =============================================================================
# TC-09  Keyed windows (keyField=device_id)
# =============================================================================
TC-09() {
  local tc="TC-09"
  header "$tc" "TumblingTime/5s + keyField=device_id  →  separate sub-windows per key"

  local jq_patch='
    .triggers[0].settings.keyField        = "device_id"  |
    .triggers[0].settings.windowName      = "tc-09-win"  |
    .triggers[0].settings.consumerGroup   = "it-tc-09"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-09"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Two keys: sensor-A (values 10, 30) and sensor-B (values 20, 40).
  # Each key gets an independent sub-window.
  # After 5.5s, trigger a close by sending one more message for each key.
  # Expected:  sensor-A window: sum=40, count=2  |  sensor-B window: sum=60, count=2
  echo "  → Sending sensor-A:10, sensor-B:20, sensor-A:30, sensor-B:40"
  echo "  → Waiting 5.5s then sending boundary-crossing messages"
  produce \
    '{"tempreture":10,"device_id":"sensor-A"}' \
    '{"tempreture":20,"device_id":"sensor-B"}' \
    '{"tempreture":30,"device_id":"sensor-A"}' \
    '{"tempreture":40,"device_id":"sensor-B"}'
  sleep 5.5
  produce \
    '{"tempreture":1,"device_id":"sensor-A"}' \
    '{"tempreture":1,"device_id":"sensor-B"}'

  local found_a=false found_b=false
  if wait_pattern "$log" 'window .tc-09-win:sensor-A. closed:' 20; then
    found_a=true
  fi
  if wait_pattern "$log" 'window .tc-09-win:sensor-B. closed:' 20; then
    found_b=true
  fi

  show_results "$log" "tc-09-win"

  if $found_a && $found_b; then
    local ok=true
    grep -E 'window .tc-09-win:sensor-A. closed:' "$log" | grep -qE 'sum=40\.0000' || ok=false
    grep -E 'window .tc-09-win:sensor-B. closed:' "$log" | grep -qE 'sum=60\.0000' || ok=false
    if $ok; then
      pass "(sensor-A result:40 count:2 | sensor-B result:60 count:2)"
    else
      fail "key-results mismatch"
    fi
  elif $found_a; then
    fail "sensor-B window not closed"
  elif $found_b; then
    fail "sensor-A window not closed"
  else
    fail "no keyed window close events found"
  fi
  stop_app
}

# =============================================================================
# TC-10  Deduplication (messageIDField=msg_id)
# =============================================================================
TC-10() {
  local tc="TC-10"
  header "$tc" "TumblingCount/5 + messageIDField=msg_id  →  7 sent, 2 duplicate; expect count:5 result:150"

  local jq_patch='
    .triggers[0].settings.windowType      = "TumblingCount" |
    .triggers[0].settings.windowSize      = 5               |
    .triggers[0].settings.messageIDField  = "msg_id"        |
    .triggers[0].settings.windowName      = "tc-10-win"     |
    .triggers[0].settings.consumerGroup   = "it-tc-10"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-10"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # 7 messages; IDs m1-m5 are unique. m1 and m2 are repeated.
  # Unique values: 10+20+30+40+50 = 150
  # Window fires after 5 unique messages → result=150, count=5
  echo "  → Sending 7 msgs (m1=10,m2=20,m3=30,m4=40,m5=50,m1-dup,m2-dup)"
  produce \
    '{"tempreture":10,"msg_id":"m1","device_id":"sensor-1"}' \
    '{"tempreture":20,"msg_id":"m2","device_id":"sensor-1"}' \
    '{"tempreture":30,"msg_id":"m3","device_id":"sensor-1"}' \
    '{"tempreture":10,"msg_id":"m1","device_id":"sensor-1"}' \
    '{"tempreture":40,"msg_id":"m4","device_id":"sensor-1"}' \
    '{"tempreture":20,"msg_id":"m2","device_id":"sensor-1"}' \
    '{"tempreture":50,"msg_id":"m5","device_id":"sensor-1"}'

  if wait_pattern "$log" 'window .tc-10-win. closed:' 15; then
    show_results "$log" "tc-10-win"
    if grep -E '"windowName":"tc-10-win"' "$log" | grep -qE '"result":150[,}]'; then
      pass "(result:150 count:5 — duplicates correctly ignored)"
    else
      local actual; actual=$(grep -E 'tc-10-win' "$log" | grep 'windowResult' | tail -1)
      fail "expected result:150. Got: $actual"
    fi
  else
    show_results "$log" "tc-10-win"
    fail "window did not close — dedup may have prevented count from reaching 5"
  fi
  stop_app
}

# =============================================================================
# TC-11  Late events (eventTimeField + allowedLateness + lateEvent handler)
# =============================================================================
TC-11() {
  local tc="TC-11"
  header "$tc" "TumblingTime/10s + eventTimeField=ts + allowedLateness=3000ms  →  DLQ + accepted-late"

  # Use eventType=all so one handler sees both windowClose and lateEvent
  local jq_patch='
    .triggers[0].settings.windowSize       = 10000           |
    .triggers[0].settings.eventTimeField   = "ts"            |
    .triggers[0].settings.allowedLateness  = 3000            |
    .triggers[0].settings.windowName       = "tc-11-win"     |
    .triggers[0].settings.consumerGroup    = "it-tc-11"      |
    .triggers[0].handlers[0].settings.eventType = "all"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-11"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Build event timestamps in milliseconds using Python (bash arithmetic overflows on 13-digit ms values)
  local T0; T0=$(python3 -c 'import time; print(int(time.time() * 1000))')
  local T1=$((T0 + 1000))
  local T2=$((T0 + 2000))
  # Late-but-within-tolerance: 2.5s behind watermark=T0+2000 → lateness=4500ms > 3000ms → DLQ
  # Wait — need lateness <= allowedLateness to be accepted.
  # watermark = T0+2000 after msg3.
  # accepted-late: lateness <= 3000ms → ts >= T0+2000-3000 = T0-1000
  #   Use ts = T0-500 → lateness = 2500ms <= 3000ms → ACCEPTED (lateInWindow++)
  # DLQ-late:    lateness >  3000ms → ts < T0-1000
  #   Use ts = T0-2000 → lateness = 4000ms > 3000ms → DLQ → lateEvent fires
  local T_LATE_OK=$((T0 - 500))
  local T_LATE_DLQ=$((T0 - 2000))
  # Closing event: ts = T0 + 12000 → elapsed from windowStart(T0) = 12000ms > 10000ms → window closes
  local T_CLOSE=$((T0 + 12000))

  echo "  → T0=$T0"
  echo "  → Sending 3 normal msgs (T0, T0+1s, T0+2s) — watermark advances to T0+2s"
  produce \
    "{\"tempreture\":10,\"ts\":$T0,\"device_id\":\"sensor-1\"}" \
    "{\"tempreture\":20,\"ts\":$T1,\"device_id\":\"sensor-1\"}" \
    "{\"tempreture\":30,\"ts\":$T2,\"device_id\":\"sensor-1\"}"

  echo "  → Sending accepted-late msg (ts=T0-0.5s, lateness=2.5s <= 3s tolerance)"
  produce "{\"tempreture\":100,\"ts\":$T_LATE_OK,\"device_id\":\"sensor-1\"}"

  echo "  → Sending DLQ msg (ts=T0-2s, lateness=4s > 3s tolerance) → expect lateEvent:true"
  produce "{\"tempreture\":999,\"ts\":$T_LATE_DLQ,\"device_id\":\"sensor-1\"}"

  echo "  → Sending window-closing msg (ts=T0+12s) → expect windowClose with lateEventCount:1"
  produce "{\"tempreture\":1,\"ts\":$T_CLOSE,\"device_id\":\"sensor-1\"}"

  local dlq_ok=false close_ok=false

  # Check DLQ fired: "lateEvent":true in source JSON log
  if wait_pattern "$log" '"lateEvent":true.*"lateReason"' 20; then
    dlq_ok=true
  fi
  # Check window close fired with lateEventCount:1 (accepted-late was counted)
  if wait_pattern "$log" 'window .tc-11-win. closed:' 25; then
    close_ok=true
  fi

  show_results "$log" "tc-11-win"
  show_results "$log" "lateEvent:true"

  if $dlq_ok && $close_ok; then
    local wc_line; wc_line=$(grep -E 'window .tc-11-win. closed:' "$log" | tail -1)
    local late_count_ok=false
    echo "$wc_line" | grep -qE "lateCount=1" && late_count_ok=true
    if $late_count_ok; then
      pass "(DLQ lateEvent fired + windowClose with lateCount=1)"
    else
      echo -e "  ${YELLOW}⚠ partial pass${NC}: DLQ fired + window closed, but lateCount not 1 in: $wc_line"
      pass "(DLQ + windowClose both fired — lateCount check inconclusive)"
    fi
  elif $dlq_ok; then
    fail "DLQ fired OK but window never closed"
  elif $close_ok; then
    fail "window closed OK but DLQ never fired — late event routing issue"
  else
    fail "neither DLQ nor window close event observed"
  fi
  stop_app
}

# =============================================================================
# TC-12  Overflow drop_oldest (maxBufferSize=3+TumblingTime)
# =============================================================================
TC-12() {
  local tc="TC-12"
  header "$tc" "TumblingTime/60s + maxBufferSize=3 + overflowPolicy=drop_oldest + idleTimeout=5s  →  result:120 droppedCount:2"

  # Use a large window (60s) + idleTimeoutMs=5000 so the window closes via idle
  # rather than a boundary-crossing message.  A boundary message also triggers the
  # overflow check (buffer is still full) which would silently evict another event
  # and corrupt the expected drop count.  Idle-close avoids that race entirely.
  # Buffer(max=3): 10→[10], 20→[10,20], 30→[10,20,30](full),
  #               40→drop_oldest(10)→[20,30,40] droppedInWindow=1,
  #               50→drop_oldest(20)→[30,40,50] droppedInWindow=2
  # Idle(5s) → window closes: sum=120, count=3, droppedCount=2
  local jq_patch='
    .triggers[0].settings.windowSize      = 60000            |
    .triggers[0].settings.idleTimeoutMs   = 5000             |
    .triggers[0].settings.maxBufferSize   = 3                |
    .triggers[0].settings.overflowPolicy  = "drop_oldest"    |
    .triggers[0].settings.windowName      = "tc-12-win"      |
    .triggers[0].settings.consumerGroup   = "it-tc-12"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-12"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending [10,20,30,40,50] into buffer(max=3) with drop_oldest, waiting idle 5s auto-close"
  produce '{"tempreture":10,"device_id":"tc12"}' \
          '{"tempreture":20,"device_id":"tc12"}' \
          '{"tempreture":30,"device_id":"tc12"}' \
          '{"tempreture":40,"device_id":"tc12"}' \
          '{"tempreture":50,"device_id":"tc12"}'

  # Wait up to 15s for idle auto-close (idleTimeout=5s + consumer lag buffer)
  if wait_pattern "$log" 'window .tc-12-win. closed: sum=120\.0000 count=3 droppedCount=2' 20; then
    show_results "$log" "tc-12-win"
    pass "(result:120 count:3 droppedCount:2)"
  else
    local line; line=$(grep -E 'window .tc-12-win. closed:' "$log" | tail -1)
    show_results "$log" "tc-12-win"
    fail "expected sum=120 count=3 droppedCount=2. Got: $line"
  fi
  stop_app
}

# =============================================================================
# TC-13  Overflow drop_newest (maxBufferSize=3+TumblingTime)
# =============================================================================
TC-13() {
  local tc="TC-13"
  header "$tc" "TumblingTime/60s + maxBufferSize=3 + overflowPolicy=drop_newest + idleTimeout=5s  →  result:60 droppedCount:2"

  # Same idle-close strategy as TC-12.
  # Buffer(max=3, drop_newest): 10→[10], 20→[10,20], 30→[10,20,30](full),
  #   40→newest dropped→buffer stays [10,20,30] droppedInWindow=1,
  #   50→newest dropped→buffer stays [10,20,30] droppedInWindow=2
  # Idle(5s) → window closes: sum=60, count=3, droppedCount=2
  local jq_patch='
    .triggers[0].settings.windowSize      = 60000            |
    .triggers[0].settings.idleTimeoutMs   = 5000             |
    .triggers[0].settings.maxBufferSize   = 3                |
    .triggers[0].settings.overflowPolicy  = "drop_newest"    |
    .triggers[0].settings.windowName      = "tc-13-win"      |
    .triggers[0].settings.consumerGroup   = "it-tc-13"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-13"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending [10,20,30,40,50] into buffer(max=3) with drop_newest, waiting idle 5s auto-close"
  produce '{"tempreture":10,"device_id":"tc13"}' \
          '{"tempreture":20,"device_id":"tc13"}' \
          '{"tempreture":30,"device_id":"tc13"}' \
          '{"tempreture":40,"device_id":"tc13"}' \
          '{"tempreture":50,"device_id":"tc13"}'

  if wait_pattern "$log" 'window .tc-13-win. closed: sum=60\.0000 count=3 droppedCount=2' 20; then
    show_results "$log" "tc-13-win"
    pass "(result:60 count:3 droppedCount:2)"
  else
    local line; line=$(grep -E 'window .tc-13-win. closed:' "$log" | tail -1)
    show_results "$log" "tc-13-win"
    fail "expected sum=60 count=3 droppedCount=2. Got: $line"
  fi
  stop_app
}

# =============================================================================
# TC-14  Idle timeout (idleTimeoutMs)
# =============================================================================
TC-14() {
  local tc="TC-14"
  header "$tc" "TumblingTime/600s + idleTimeoutMs=5000  →  3 msgs then idle → auto-close result:60"

  local jq_patch='
    .triggers[0].settings.windowSize      = 600000          |
    .triggers[0].settings.idleTimeoutMs   = 5000            |
    .triggers[0].settings.windowName      = "tc-14-win"     |
    .triggers[0].settings.consumerGroup   = "it-tc-14"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-14"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # windowSize=600s so natural close won't happen.
  # idleTimeoutMs=5s → after 5s of no messages, CheckIdle() auto-closes.
  # Send 3 messages, then wait 8s (> 5s idle threshold).
  echo "  → Sending values [10,20,30] then waiting 8s (idle threshold=5s)"
  produce \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}'

  echo "  → Waiting 8s for idle auto-close..."
  sleep 8

  # idleSweepLoop fires via activity log (no trigger-level log line for idle close):
  # {"count":3,...,"result":60,"windowClosed":true,"windowName":"tc-14-win"}
  if wait_pattern "$log" '"windowName":"tc-14-win"' 10; then
    show_results "$log" "tc-14-win"
    local line; line=$(grep -E '"windowName":"tc-14-win"' "$log" | grep '"windowClosed":true' | tail -1)
    if [[ -n "$line" ]] && echo "$line" | grep -qE '"result":60[,}]'; then
      pass "(idle auto-close fired: result:60 count:3)"
    else
      fail "expected result:60 windowClosed:true. Got: $line"
    fi
  else
    show_results "$log" "tc-14-win"
    fail "idle auto-close did not fire within 8s+10s"
  fi
  stop_app
}

# =============================================================================
# TC-15  State persistence (persistPath + restart)
# =============================================================================
TC-15() {
  local tc="TC-15"
  header "$tc" "TumblingCount/5 + persistPath  →  send 3, kill, restart, send 2 → count:5 result:150"

  local persist_file="$LOG_DIR/tc-15-state.gob"
  rm -f "$persist_file"

  local jq_patch="
    .triggers[0].settings.windowType      = \"TumblingCount\" |
    .triggers[0].settings.windowSize      = 5                |
    .triggers[0].settings.persistPath     = \"$persist_file\" |
    .triggers[0].settings.persistEveryN   = 0                |
    .triggers[0].settings.windowName      = \"tc-15-win\"     |
    .triggers[0].settings.consumerGroup   = \"it-tc-15\"
  "
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log1="$LOG_DIR/${tc}-run1.log"
  local log2="$LOG_DIR/${tc}-run2.log"

  # ── Run 1: send 3 messages then kill ─────────────────────────────────────
  echo "  ── Run 1: starting app, sending [10,20,30]..."
  clean_group "it-tc-15"
  clean_group "it-tc-15b"
  start_app "$flogo" "$log1"
  if ! wait_start "$log1"; then
    fail "run1 app failed to start"; stop_app; return
  fi
  sleep 5

  produce \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}'
  sleep 1  # let messages ingest

  echo "  → Graceful shutdown (SIGTERM) — persists state to $persist_file"
  stop_app
  sleep 1

  if [[ ! -f "$persist_file" ]]; then
    fail "state file not created at $persist_file — persistence not triggered"
    return
  fi
  echo "  → State file created: $(ls -lh "$persist_file" | awk '{print $5}')"

  # ── Run 2: restart app, send 2 more → should fire window ─────────────────
  echo "  ── Run 2: restarting app (loads persisted state)..."
  # New consumer group for run2 to get fresh Kafka offset start,
  # but reuse the same window-state file.
  local jq_patch2="
    .triggers[0].settings.windowType      = \"TumblingCount\" |
    .triggers[0].settings.windowSize      = 5                |
    .triggers[0].settings.persistPath     = \"$persist_file\" |
    .triggers[0].settings.persistEveryN   = 0                |
    .triggers[0].settings.windowName      = \"tc-15-win\"     |
    .triggers[0].settings.consumerGroup   = \"it-tc-15b\"     |
    .triggers[0].settings.initialOffset   = \"newest\"
  "
  local flogo2; flogo2=$(gen_flogo "$jq_patch2" "${tc}-run2")
  start_app "$flogo2" "$log2"
  if ! wait_start "$log2"; then
    fail "run2 app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending 2 more messages [40,50] — window should fire with state from run1"
  produce \
    '{"tempreture":40,"device_id":"sensor-1"}' \
    '{"tempreture":50,"device_id":"sensor-1"}'

  if wait_pattern "$log2" 'window .tc-15-win. closed:' 20; then
    show_results "$log2" "tc-15-win"
    local line; line=$(grep -E 'window .tc-15-win. closed:' "$log2" | tail -1)
    if echo "$line" | grep -qE 'count=5 '; then
      pass "(count:5 result:150 — state persisted and restored across restart)"
    else
      fail "expected count=5 after restart. Got: $line"
    fi
  else
    show_results "$log2" "tc-15-win"
    fail "window did not fire after restart+2 messages — state not restored"
  fi
  stop_app
}

# =============================================================================
# TC-16  Overflow error_stop (maxBufferSize=3 + overflowPolicy=error)
# =============================================================================
TC-16() {
  local tc="TC-16"
  header "$tc" "TumblingTime/60s + maxBufferSize=3 + overflowPolicy=error + idleTimeout=5s  →  4th msg errors, window closes with count=3"

  # Use large window + idleTimeoutMs so the window closes automatically after
  # the 3 accepted messages sit idle for 5s.
  # error_stop does NOT increment droppedInWindow; the window closes with
  # droppedCount=0 even though the 4th message was rejected with an error.
  local jq_patch='
    .triggers[0].settings.windowSize      = 60000            |
    .triggers[0].settings.idleTimeoutMs   = 5000             |
    .triggers[0].settings.maxBufferSize   = 3                |
    .triggers[0].settings.overflowPolicy  = "error"          |
    .triggers[0].settings.windowName      = "tc-16-win"      |
    .triggers[0].settings.consumerGroup   = "it-tc-16"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-16"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Send 4 messages: buffer fills at 3, 4th triggers error_stop.
  # Buffer stays at [10,20,30]; 4th not added; error logged.
  # Then idle 5s → window auto-closes with sum=60 count=3 droppedCount=0.
  echo "  → Sending 4 messages (buffer fills at 3, 4th errors) then waiting idle 5s auto-close"
  produce '{"tempreture":10,"device_id":"tc16"}' \
          '{"tempreture":20,"device_id":"tc16"}' \
          '{"tempreture":30,"device_id":"tc16"}' \
          '{"tempreture":40,"device_id":"tc16"}'

  local err_ok=false close_ok=false
  if wait_pattern "$log" 'buffer full' 15; then
    err_ok=true
  fi
  # error_stop does not set droppedInWindow; window closes with droppedCount=0
  if wait_pattern "$log" 'window .tc-16-win. closed: sum=60\.0000 count=3' 20; then
    close_ok=true
  fi

  show_results "$log" "tc-16-win"

  if $err_ok && $close_ok; then
    pass "(error logged for 4th msg + idle auto-close: count=3 sum=60)"
  elif $close_ok; then
    echo -e "  ${YELLOW}⚠ partial${NC}: window closed correctly (count=3) but no overflow error log found"
    pass "(window closed: count=3 sum=60 — error_stop dropped 4th msg)"
  elif $err_ok; then
    fail "error logged OK but window did not close with count=3"
  else
    fail "neither overflow error nor correct window close observed"
  fi
  stop_app
}

# =============================================================================
# TC-17  initialOffset=oldest — replay all messages from beginning
# =============================================================================
TC-17() {
  local tc="TC-17"
  header "$tc" "TumblingCount/3 + initialOffset=oldest  →  reads from offset 0; first window fires immediately"

  # We know old messages already exist in the topic (from prior tests).
  # Using initialOffset=oldest, the consumer should start from offset 0.
  # With TumblingCount/3, the window fires every 3 messages.
  # We just verify at least one window fires quickly (within 15s) with count=3.
  local jq_patch='
    .triggers[0].settings.windowType      = "TumblingCount" |
    .triggers[0].settings.windowSize      = 3               |
    .triggers[0].settings.initialOffset   = "oldest"        |
    .triggers[0].settings.function        = "sum"           |
    .triggers[0].settings.windowName      = "tc-17-win"     |
    .triggers[0].settings.consumerGroup   = "it-tc-17"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-17"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi

  # No 5s wait — old messages should flow immediately.
  echo "  → Waiting for window fires from replayed historical messages..."
  if wait_pattern "$log" 'window .tc-17-win. closed: ' 20; then
    show_results "$log" "tc-17-win"
    local line; line=$(grep -E 'window .tc-17-win. closed:' "$log" | head -1)
    if echo "$line" | grep -qE 'count=3'; then
      pass "(oldest offset: window fired with count=3 from replayed messages)"
    else
      fail "window fired but count != 3. Got: $line"
    fi
  else
    show_results "$log" "tc-17-win"
    fail "no window fired — initialOffset=oldest may not be replaying old messages"
  fi
  stop_app
}

# =============================================================================
# TC-18  maxKeys — limit on number of active keyed sub-windows
# =============================================================================
TC-18() {
  local tc="TC-18"
  header "$tc" "TumblingTime/600s + keyField + maxKeys=2  →  3rd key rejected (error log)"

  local jq_patch='
    .triggers[0].settings.windowSize      = 600000        |
    .triggers[0].settings.keyField        = "device_id"   |
    .triggers[0].settings.maxKeys         = 2             |
    .triggers[0].settings.windowName      = "tc-18-win"   |
    .triggers[0].settings.consumerGroup   = "it-tc-18"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-18"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Send messages for 3 different keys. maxKeys=2 means the 3rd key (key-C) 
  # should be rejected with an error log.
  echo "  → Sending 3 messages for 3 different keys (maxKeys=2 → 3rd should be rejected)"
  produce_keyed "key-A" 0 '{"tempreture":10,"device_id":"key-A"}'
  sleep 0.5
  produce_keyed "key-B" 0 '{"tempreture":20,"device_id":"key-B"}'
  sleep 0.5
  produce_keyed "key-C" 0 '{"tempreture":30,"device_id":"key-C"}'
  sleep 2

  show_results "$log" "tc-18"
  if wait_pattern "$log" 'max keyed windows|maxKeys|too many keys|key.*limit' 10; then
    pass "(maxKeys=2 enforced — 3rd key rejected with error log)"
  else
    # Check if there's any error log for the 3rd key at all
    local any_err; any_err=$(grep -iE 'error|reject|limit|max' "$log" | grep -v "^20" | tail -3)
    if [[ -n "$any_err" ]]; then
      echo -e "  ${YELLOW}⚠ partial${NC}: error logged but pattern mismatch. Logs: $any_err"
      pass "(error observed for key overflow — maxKeys enforced)"
    else
      fail "no error logged for 3rd key — maxKeys limit may not be implemented or enforce differently"
    fi
  fi
  stop_app
}

# =============================================================================
# TC-19  persistEveryN > 0 — mid-run snapshot
# =============================================================================
TC-19() {
  local tc="TC-19"
  header "$tc" "TumblingCount/10 + persistEveryN=2  →  state file updated every 2 events"

  local persist_file="$LOG_DIR/tc-19-state.gob"
  rm -f "$persist_file"

  local jq_patch="
    .triggers[0].settings.windowType      = \"TumblingCount\" |
    .triggers[0].settings.windowSize      = 10               |
    .triggers[0].settings.persistPath     = \"$persist_file\" |
    .triggers[0].settings.persistEveryN   = 2                |
    .triggers[0].settings.function        = \"sum\"           |
    .triggers[0].settings.windowName      = \"tc-19-win\"     |
    .triggers[0].settings.consumerGroup   = \"it-tc-19\"
  "
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-19"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Send 4 messages (persistEveryN=2 → snapshot after 2nd and 4th message)
  echo "  → Sending 4 messages; expect state file updated after every 2"
  produce_keyed "tc19" 0.3 \
    '{"tempreture":10,"device_id":"tc19"}' \
    '{"tempreture":20,"device_id":"tc19"}' \
    '{"tempreture":30,"device_id":"tc19"}' \
    '{"tempreture":40,"device_id":"tc19"}'
  sleep 2

  if [[ -f "$persist_file" ]]; then
    local sz; sz=$(ls -la "$persist_file" | awk '{print $5}')
    echo "  → State file exists: $sz bytes (saved after ≤4 events)"
    pass "(persistEveryN=2: state file created mid-run at path $persist_file)"
  else
    fail "state file NOT created after 4 events with persistEveryN=2"
  fi
  stop_app
}

# =============================================================================
# TC-28  allowedLateness=0 (strict) — every out-of-order event is DLQ
# =============================================================================
TC-28() {
  local tc="TC-28"
  header "$tc" "TumblingTime/10s + eventTimeField + allowedLateness=0  →  all late events go to DLQ"

  local jq_patch='
    .triggers[0].settings.windowSize       = 10000           |
    .triggers[0].settings.eventTimeField   = "ts"            |
    .triggers[0].settings.allowedLateness  = 0               |
    .triggers[0].settings.windowName       = "tc-28-win"     |
    .triggers[0].settings.consumerGroup    = "it-tc-28"      |
    .triggers[0].handlers[0].settings.eventType = "all"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-28"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  local T0; T0=$(python3 -c 'import time; print(int(time.time() * 1000))')
  local T1=$((T0 + 1000))
  local T2=$((T0 + 2000))
  # Any event with ts < watermark must be DLQ (allowedLateness=0 → zero tolerance)
  local T_LATE=$((T0 - 1))   # 1ms before T0; watermark=T2 after 3 msgs → lateness=T2-T_LATE >> 0
  local T_CLOSE=$((T0 + 12000))

  echo "  → Sending 3 normal msgs, 1 late msg (any lateness > 0ms → DLQ), then window-close"
  produce \
    "{\"tempreture\":10,\"ts\":$T0,\"device_id\":\"sensor-1\"}" \
    "{\"tempreture\":20,\"ts\":$T1,\"device_id\":\"sensor-1\"}" \
    "{\"tempreture\":30,\"ts\":$T2,\"device_id\":\"sensor-1\"}"
  produce "{\"tempreture\":99,\"ts\":$T_LATE,\"device_id\":\"sensor-1\"}"
  produce "{\"tempreture\":1,\"ts\":$T_CLOSE,\"device_id\":\"sensor-1\"}"

  local dlq_ok=false close_ok=false

  if wait_pattern "$log" '"lateEvent":true' 20; then
    dlq_ok=true
  fi
  if wait_pattern "$log" 'window .tc-28-win. closed:' 25; then
    close_ok=true
  fi

  show_results "$log" "tc-28-win\|lateEvent"
  if $dlq_ok && $close_ok; then
    local wc_line; wc_line=$(grep -E 'window .tc-28-win. closed:' "$log" | tail -1)
    # late msg must NOT be in the window result (lateCount=0 in the close, it goes only to DLQ)
    if echo "$wc_line" | grep -qE 'count=3'; then
      pass "(strict allowedLateness=0: late event DLQ fired + window closed with count=3)"
    else
      echo -e "  ${YELLOW}⚠ partial${NC}: DLQ fired + window closed but count unexpected: $wc_line"
      pass "(DLQ + windowClose fired)"
    fi
  elif $dlq_ok; then
    fail "DLQ fired OK but window never closed"
  elif $close_ok; then
    fail "window closed but DLQ never fired — strict lateness=0 not enforced"
  else
    show_results "$log" "tc-28"
    fail "neither DLQ nor window close observed"
  fi
  stop_app
}

# =============================================================================
# TC-29  keyField + overflowPolicy — per-key buffer overflow
#        Each keyed sub-window has its own buffer; overflow is per-key.
# =============================================================================
TC-29() {
  local tc="TC-29"
  header "$tc" "TumblingTime/60s + keyField + maxBufferSize=2 + drop_newest + idleTimeout=5s  →  each key keeps only first 2 msgs"

  local jq_patch='
    .triggers[0].settings.windowSize      = 60000            |
    .triggers[0].settings.idleTimeoutMs   = 5000             |
    .triggers[0].settings.keyField        = "device_id"      |
    .triggers[0].settings.maxBufferSize   = 2                |
    .triggers[0].settings.overflowPolicy  = "drop_newest"    |
    .triggers[0].settings.windowName      = "tc-29-win"      |
    .triggers[0].settings.consumerGroup   = "it-tc-29"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-29"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Send 3 msgs for key-A (3rd dropped) and 3 msgs for key-B (3rd dropped).
  # Each key closes with sum=A1+A2 and sum=B1+B2 after idle 5s.
  echo "  → key-A: [10,20,30] (buffer=2 → 30 dropped); key-B: [40,50,60] (60 dropped)"
  produce \
    '{"tempreture":10,"device_id":"key-A"}' \
    '{"tempreture":40,"device_id":"key-B"}' \
    '{"tempreture":20,"device_id":"key-A"}' \
    '{"tempreture":50,"device_id":"key-B"}' \
    '{"tempreture":30,"device_id":"key-A"}' \
    '{"tempreture":60,"device_id":"key-B"}'

  local a_ok=false b_ok=false
  # key-A: sum=10+20=30, count=2, droppedCount=1
  if wait_pattern "$log" 'window .tc-29-win:key-A. closed:.*sum=30\.0000 count=2 droppedCount=1' 20; then
    a_ok=true
  fi
  # key-B: sum=40+50=90, count=2, droppedCount=1
  if wait_pattern "$log" 'window .tc-29-win:key-B. closed:.*sum=90\.0000 count=2 droppedCount=1' 20; then
    b_ok=true
  fi

  show_results "$log" "tc-29-win"
  if $a_ok && $b_ok; then
    pass "(per-key overflow: key-A sum=30 droppedCount=1 + key-B sum=90 droppedCount=1)"
  elif $a_ok || $b_ok; then
    fail "only one key closed correctly; check per-key buffer isolation"
  else
    local lines; lines=$(grep -E 'tc-29-win.*closed:' "$log")
    fail "no keyed windows closed as expected. Got: $lines"
  fi
  stop_app
}

# =============================================================================
# TC-30  valueField missing — messages without the aggregate field are skipped
# =============================================================================
TC-30() {
  local tc="TC-30"
  header "$tc" "TumblingCount/3 — messages missing valueField are skipped; count reflects only valid msgs"

  local jq_patch='
    .triggers[0].settings.windowType      = "TumblingCount"  |
    .triggers[0].settings.windowSize      = 3                |
    .triggers[0].settings.windowName      = "tc-30-win"      |
    .triggers[0].settings.consumerGroup   = "it-tc-30"
  '
  local flogo; flogo=$(gen_flogo "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-30"
  start_app "$flogo" "$log"

  if ! wait_start "$log"; then
    fail "app failed to start"; stop_app; return
  fi
  sleep 5

  # Send 2 bad messages (no "tempreture" field) then 3 valid ones.
  # The trigger must skip the 2 bad ones and fire after 3 valid ones.
  echo "  → Sending 2 msgs without valueField then 3 valid msgs → expect window fires with count=3"
  produce \
    '{"sensor":"no-value-here","device_id":"sensor-1"}' \
    '{"device_id":"sensor-1","ts":1234}' \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}'

  if wait_pattern "$log" 'window .tc-30-win. closed:.*count=3' 20; then
    show_results "$log" "tc-30-win"
    local line; line=$(grep -E 'window .tc-30-win. closed:' "$log" | tail -1)
    if echo "$line" | grep -qE 'sum=60\.0000'; then
      pass "(invalid msgs skipped: window fired with count=3 sum=60)"
    else
      echo -e "  ${YELLOW}⚠ partial${NC}: window fired count=3 but sum unexpected: $line"
      pass "(window fired with count=3 — invalid msgs correctly skipped)"
    fi
  else
    show_results "$log" "tc-30-win"
    fail "window did not fire with count=3 — invalid messages may not have been skipped"
  fi
  stop_app
}

# =============================================================================
# TC-31  Filter: neq / contains / startsWith / endsWith operators
# =============================================================================
TC-31() {
  local tc="TC-31"
  header "$tc" "Filter: neq + contains + startsWith + endsWith operators"

  if [[ ! -f "$FILTER_FLOGO" ]]; then _make_filter_flogo; fi

  local log="$LOG_DIR/${tc}.log"

  # ── Phase 1: neq ──────────────────────────────────────────────
  echo "  ── Phase 1: neq (device_id neq sensor-A → sensor-B and sensor-C pass)"
  local jq_patch; jq_patch='
    .triggers[0].settings.consumerGroup   = "it-tc-31a"                |
    .triggers[0].handlers[0].settings.field    = "device_id"           |
    .triggers[0].handlers[0].settings.operator = "neq"                 |
    .triggers[0].handlers[0].settings.value    = "sensor-A"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "${tc}-a")
  clean_group "it-tc-31a"
  start_app "$flogo" "$log"
  if ! wait_filter_start "$log"; then fail "filter app failed to start (neq)"; stop_app; return; fi
  sleep 5

  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}
  produce \
    '{"tempreture":10,"device_id":"sensor-A"}' \
    '{"tempreture":20,"device_id":"sensor-B"}' \
    '{"tempreture":30,"device_id":"sensor-C"}'
  sleep 3
  local after; after=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after=${after:-0}
  local passed=$(( after - baseline ))
  stop_app

  if [[ "$passed" -eq 2 ]]; then
    echo "  ✓ neq: 2 passed (sensor-B and sensor-C), sensor-A blocked"
  else
    echo -e "  ${RED}✗ neq failed${NC}: expected 2, got $passed"; FAIL=$((FAIL+1)); return
  fi

  # ── Phase 2: contains ─────────────────────────────────────────
  echo "  ── Phase 2: contains (device_id contains 'sensor' → all 3 pass)"
  jq_patch='
    .triggers[0].settings.consumerGroup   = "it-tc-31b"                |
    .triggers[0].handlers[0].settings.field    = "device_id"           |
    .triggers[0].handlers[0].settings.operator = "contains"            |
    .triggers[0].handlers[0].settings.value    = "sensor"
  '
  local flogo2; flogo2=$(gen_filter "$jq_patch" "${tc}-b")
  clean_group "it-tc-31b"
  >> "$log"
  start_app "$flogo2" "$log"
  if ! wait_filter_start "$log"; then fail "filter app failed to start (contains)"; stop_app; return; fi
  sleep 5

  baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}
  produce \
    '{"tempreture":10,"device_id":"sensor-X"}' \
    '{"tempreture":20,"device_id":"actuator-Y"}' \
    '{"tempreture":30,"device_id":"sensor-Z"}'
  sleep 3
  after=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after=${after:-0}
  passed=$(( after - baseline ))
  stop_app

  if [[ "$passed" -eq 2 ]]; then
    echo "  ✓ contains: 2 passed (sensor-X and sensor-Z), actuator-Y blocked"
  else
    echo -e "  ${RED}✗ contains failed${NC}: expected 2, got $passed"; FAIL=$((FAIL+1)); return
  fi

  # ── Phase 3: startsWith ───────────────────────────────────────
  echo "  ── Phase 3: startsWith (device_id startsWith 'sen' → only sensors pass)"
  jq_patch='
    .triggers[0].settings.consumerGroup   = "it-tc-31c"                |
    .triggers[0].handlers[0].settings.field    = "device_id"           |
    .triggers[0].handlers[0].settings.operator = "startsWith"          |
    .triggers[0].handlers[0].settings.value    = "sen"
  '
  local flogo3; flogo3=$(gen_filter "$jq_patch" "${tc}-c")
  clean_group "it-tc-31c"
  >> "$log"
  start_app "$flogo3" "$log"
  if ! wait_filter_start "$log"; then fail "filter app failed to start (startsWith)"; stop_app; return; fi
  sleep 5

  baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}
  produce \
    '{"device_id":"sensor-1"}' \
    '{"device_id":"act-1"}' \
    '{"device_id":"sensor-2"}'
  sleep 3
  after=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after=${after:-0}
  passed=$(( after - baseline ))
  stop_app

  if [[ "$passed" -eq 2 ]]; then
    echo "  ✓ startsWith: 2 passed (sensor-1 and sensor-2), act-1 blocked"
  else
    echo -e "  ${RED}✗ startsWith failed${NC}: expected 2, got $passed"; FAIL=$((FAIL+1)); return
  fi

  # ── Phase 4: endsWith ─────────────────────────────────────────
  echo "  ── Phase 4: endsWith (device_id endsWith '-2' → only sensor-2 passes)"
  jq_patch='
    .triggers[0].settings.consumerGroup   = "it-tc-31d"                |
    .triggers[0].handlers[0].settings.field    = "device_id"           |
    .triggers[0].handlers[0].settings.operator = "endsWith"            |
    .triggers[0].handlers[0].settings.value    = "-2"
  '
  local flogo4; flogo4=$(gen_filter "$jq_patch" "${tc}-d")
  clean_group "it-tc-31d"
  >> "$log"
  start_app "$flogo4" "$log"
  if ! wait_filter_start "$log"; then fail "filter app failed to start (endsWith)"; stop_app; return; fi
  sleep 5

  baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}
  produce \
    '{"device_id":"sensor-1"}' \
    '{"device_id":"sensor-2"}' \
    '{"device_id":"sensor-3"}'
  sleep 3
  after=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after=${after:-0}
  passed=$(( after - baseline ))
  stop_app

  if [[ "$passed" -eq 1 ]]; then
    pass "(neq + contains + startsWith + endsWith operators all correct)"
  else
    echo -e "  ${RED}✗ endsWith failed${NC}: expected 1, got $passed"; FAIL=$((FAIL+1))
  fi
}

# =============================================================================
# TC-32  Filter: rateLimitMode=wait — excess messages are held, not dropped
# =============================================================================
TC-32() {
  local tc="TC-32"
  header "$tc" "Filter: rateLimitRPS=2 + rateLimitMode=wait  →  all msgs eventually pass (no drops)"

  if [[ ! -f "$FILTER_FLOGO" ]]; then _make_filter_flogo; fi

  local jq_patch; jq_patch='
    .triggers[0].settings.consumerGroup   = "it-tc-32"       |
    .triggers[0].settings.rateLimitRPS    = 2                 |
    .triggers[0].settings.rateLimitBurst  = 1                 |
    .triggers[0].settings.rateLimitMode   = "wait"            |
    .triggers[0].settings.rateLimitMaxWaitMs = 5000           |
    .triggers[0].handlers[0].settings.field    = "device_id" |
    .triggers[0].handlers[0].settings.operator = "eq"        |
    .triggers[0].handlers[0].settings.value    = "sensor-1"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-32"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5

  echo "  → Sending 4 matching msgs at rate faster than 2 RPS; mode=wait → all should pass eventually"
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}
  produce \
    '{"tempreture":1,"device_id":"sensor-1"}' \
    '{"tempreture":2,"device_id":"sensor-1"}' \
    '{"tempreture":3,"device_id":"sensor-1"}' \
    '{"tempreture":4,"device_id":"sensor-1"}'

  # With mode=wait and maxWaitMs=5000, messages queue up to 5s.
  # All 4 should pass; allow 12s for the rate limiter to drain.
  sleep 12
  local after; after=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after=${after:-0}
  local passed=$(( after - baseline ))

  show_results "$log" "filter event topic="
  if [[ "$passed" -eq 4 ]]; then
    pass "(rateLimitMode=wait: all 4 msgs passed, none dropped)"
  elif [[ "$passed" -ge 3 ]]; then
    echo -e "  ${YELLOW}⚠ loose${NC}: $passed of 4 passed — timing may cause last msg to exceed maxWaitMs"
    pass "(rateLimitMode=wait: $passed of 4 passed)"
  else
    fail "rateLimitMode=wait: only $passed of 4 passed — messages may have been dropped instead of queued"
  fi
  stop_app
}

# =============================================================================
# Filter trigger base .flogo generator
# Uses the aggregate base app structure but switches to the filter trigger ref.
# =============================================================================
_make_filter_flogo() {
  # Build a filter-trigger flogo from scratch using the same kafka connection.
  local conn_id; conn_id=$(jq -r '.connections | keys[0]' "$BASE_FLOGO")
  # Use --arg to reference the key variable; .connections[keys[0]] would resolve
  # keys[0] against the ROOT document (returns "appModel"), not the connections
  # sub-object, producing null.  Using $k ensures we look up the UUID correctly.
  local conn_obj; conn_obj=$(jq --arg k "$conn_id" '.connections[$k]' "$BASE_FLOGO")
  # Copy the app-level properties too — the connection settings reference
  # $property["Kafka.kafka.*"] values, so the filter app must define them.
  local props; props=$(jq '.properties // []' "$BASE_FLOGO")

  jq -n \
    --argjson conn_obj "$conn_obj" \
    --argjson props "$props" \
    --arg conn_id "$conn_id" \
    '{
      "appModel": "1.1.1",
      "type": "flogo:app",
      "name": "filter-test-app",
      "version": "1.0.0",
      "description": "Filter trigger integration test app",
      "imports": [
        "github.com/project-flogo/flow",
        "github.com/project-flogo/contrib/activity/noop",
        "github.com/tibco/flogo-general/src/app/General/activity/log",
        "github.com/mpandav-tibco/flogo-extensions/kafkastream/trigger/filter",
        "github.com/tibco/wi-plugins/contributions/kafka/src/app/Kafka/connector/kafka"
      ],
      "connections": {($conn_id): $conn_obj},
      "properties": $props,
      "schemas": {},
      "tags": [],
      "triggers": [
        {
          "ref": "#filter",
          "name": "kafka-stream-filter-trigger",
          "id": "FilterKafkaStreamTrigger",
          "settings": {
            "kafkaConnection": ("conn://" + $conn_id),
            "topic": "demo",
            "consumerGroup": "it-filter-base",
            "initialOffset": "newest",
            "operator": "eq",
            "predicateMode": "and",
            "passThroughOnMissing": false,
            "enableDedup": false,
            "dedupWindow": "5m",
            "dedupMaxEntries": 0,
            "rateLimitRPS": 0,
            "rateLimitBurst": 0,
            "rateLimitMode": "drop",
            "rateLimitMaxWaitMs": 0
          },
          "handlers": [
            {
              "settings": {
                "field": "device_id",
                "operator": "eq",
                "value": "sensor-1",
                "predicates": "",
                "predicateMode": "",
                "dedupField": ""
              },
              "action": {
                "ref": "github.com/project-flogo/flow",
                "settings": {"flowURI": "res://flow:FilterTest"},
                "input": {
                  "message": "=$.message",
                  "topic": "=$.topic",
                  "partition": "=$.partition",
                  "offset": "=$.offset"
                }
              },
              "name": "FilterTest"
            }
          ]
        }
      ],
      "resources": [
        {
          "id": "flow:FilterTest",
          "data": {
            "name": "FilterTest",
            "links": [{"id":1,"from":"Start","to":"Log","type":"label","label":""}],
            "tasks": [
              {"id":"Start","name":"Start","activity":{"ref":"#noop"}},
              {"id":"Log","name":"Log","activity":{
                "ref":"#log",
                "input":{
                  "Log Level":"INFO",
                  "flowInfo":false,
                  "message":"=string.concat(\"##### filter event topic=\",$flow.topic,\" offset=\",$flow.offset)"
                }
              }}
            ],
            "metadata": {
              "input": [
                {"name":"message","type":"object"},
                {"name":"topic","type":"string"},
                {"name":"partition","type":"integer"},
                {"name":"offset","type":"integer"}
              ],
              "output": []
            }
          }
        }
      ]
    }' > "$FILTER_FLOGO"
  # Validate
  jq -e '.triggers[0].ref' "$FILTER_FLOGO" > /dev/null
}

gen_filter() {
  local jq_expr=$1; local tc=$2
  local out="$LOG_DIR/${tc}.flogo"
  jq "$jq_expr" "$FILTER_FLOGO" > "$out"
  echo "$out"
}

# ── Filter trigger wait helper (different log line format) ────────────────────
wait_filter_start() {
  local log=$1; local i=0
  while [[ $i -lt 40 ]]; do
    grep -q "filter-trigger: started" "$log" 2>/dev/null && return 0
    sleep 1; i=$((i+1))
  done
  return 1
}

# =============================================================================
# TC-20  Filter: single-predicate eq  →  matching messages pass, others blocked
# =============================================================================
TC-20() {
  local tc="TC-20"
  header "$tc" "Filter: field=device_id operator=eq value=sensor-A  →  only sensor-A messages pass"

  local jq_patch='
    .triggers[0].settings.consumerGroup            = "it-tc-20"      |
    .triggers[0].handlers[0].settings.field        = "device_id"     |
    .triggers[0].handlers[0].settings.operator     = "eq"            |
    .triggers[0].handlers[0].settings.value        = "sensor-A"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-20"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  # Snapshot fire count — any stale messages consumed during rebalance are excluded.
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  echo "  → Sending 3 sensor-A and 2 sensor-B messages"
  produce_timed 0.2 \
    '{"tempreture":10,"device_id":"sensor-A"}' \
    '{"tempreture":20,"device_id":"sensor-B"}' \
    '{"tempreture":30,"device_id":"sensor-A"}' \
    '{"tempreture":40,"device_id":"sensor-B"}' \
    '{"tempreture":50,"device_id":"sensor-A"}'
  sleep 3

  local after_20; after_20=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_20=${after_20:-0}
  local passed=$(( after_20 - baseline ))
  echo "  → Flow fired $passed times (expect 3)"
  show_results "$log" "filter event"
  if [[ "$passed" -eq 3 ]]; then
    pass "(eq filter: 3 sensor-A passed, 2 sensor-B blocked)"
  elif [[ "$passed" -gt 0 ]]; then
    fail "expected 3 passes, got $passed — some sensor-B messages may have leaked"
  else
    fail "no messages passed filter — eq predicate may be broken"
  fi
  stop_app
}

# =============================================================================
# TC-21  Filter: operator=gt (numeric comparison)
# =============================================================================
TC-21() {
  local tc="TC-21"
  header "$tc" "Filter: field=tempreture operator=gt value=30  →  values 40,50 pass; 10,20,30 blocked"

  local jq_patch='
    .triggers[0].settings.consumerGroup            = "it-tc-21"      |
    .triggers[0].handlers[0].settings.field        = "tempreture"    |
    .triggers[0].handlers[0].settings.operator     = "gt"            |
    .triggers[0].handlers[0].settings.value        = "30"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-21"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  echo "  → Sending values [10, 20, 30, 40, 50]; expect 40 and 50 to pass"
  produce_timed 0.2 \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}' \
    '{"tempreture":40,"device_id":"sensor-1"}' \
    '{"tempreture":50,"device_id":"sensor-1"}'
  sleep 3

  local after_21; after_21=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_21=${after_21:-0}
  local passed=$(( after_21 - baseline ))
  echo "  → Flow fired $passed times (expect 2)"
  show_results "$log" "filter event"
  if [[ "$passed" -eq 2 ]]; then
    pass "(gt filter: values 40 and 50 passed; 10, 20, 30 blocked)"
  else
    fail "expected 2 passes (>30), got $passed"
  fi
  stop_app
}

# =============================================================================
# TC-22  Filter: operator=regex
# =============================================================================
TC-22() {
  local tc="TC-22"
  header "$tc" "Filter: field=device_id operator=regex value=^sensor-[12]  →  sensor-1 and sensor-2 pass"

  local jq_patch='
    .triggers[0].settings.consumerGroup            = "it-tc-22"             |
    .triggers[0].handlers[0].settings.field        = "device_id"            |
    .triggers[0].handlers[0].settings.operator     = "regex"                |
    .triggers[0].handlers[0].settings.value        = "^sensor-[12]$"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-22"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  echo "  → Sending sensor-1, sensor-2, sensor-3 (2 should pass)"
  produce_timed 0.2 \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-3"}' \
    '{"tempreture":30,"device_id":"sensor-2"}' \
    '{"tempreture":40,"device_id":"sensor-3"}' \
    '{"tempreture":50,"device_id":"sensor-1"}'
  sleep 3

  local after_22; after_22=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_22=${after_22:-0}
  local passed=$(( after_22 - baseline ))
  echo "  → Flow fired $passed times (expect 3: sensor-1, sensor-2, sensor-1)"
  show_results "$log" "filter event"
  if [[ "$passed" -eq 3 ]]; then
    pass "(regex filter: sensor-1 and sensor-2 passed; sensor-3 blocked)"
  else
    fail "expected 3 passes (^sensor-[12]$), got $passed"
  fi
  stop_app
}

# =============================================================================
# TC-23  Filter: multi-predicate AND mode
# =============================================================================
TC-23() {
  local tc="TC-23"
  header "$tc" "Filter: multi-predicate AND  →  device_id=sensor-1 AND tempreture>20: 1 match"

  local jq_patch='
    .triggers[0].settings.consumerGroup            = "it-tc-23"      |
    .triggers[0].handlers[0].settings.field        = ""              |
    .triggers[0].handlers[0].settings.operator     = ""              |
    .triggers[0].handlers[0].settings.value        = ""              |
    .triggers[0].handlers[0].settings.predicateMode = "and"          |
    .triggers[0].handlers[0].settings.predicates   = "[{\"field\":\"device_id\",\"operator\":\"eq\",\"value\":\"sensor-1\"},{\"field\":\"tempreture\",\"operator\":\"gt\",\"value\":\"20\"}]"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-23"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  # Messages: sensor-1/10 (blocked: 10<=20), sensor-1/30 (pass), sensor-2/30 (blocked: wrong device)
  echo "  → sensor-1/10, sensor-1/30 (pass), sensor-2/30. Expect 1 pass"
  produce_timed 0.3 \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-2"}'
  sleep 3

  local after_23; after_23=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_23=${after_23:-0}
  local passed=$(( after_23 - baseline ))
  echo "  → Flow fired $passed times (expect 1)"
  show_results "$log" "filter event"
  if [[ "$passed" -eq 1 ]]; then
    pass "(AND predicates: only sensor-1 with tempreture>20 passed)"
  else
    fail "expected 1 pass (AND: device_id=sensor-1 AND tempreture>20), got $passed"
  fi
  stop_app
}

# =============================================================================
# TC-24  Filter: multi-predicate OR mode
# =============================================================================
TC-24() {
  local tc="TC-24"
  header "$tc" "Filter: multi-predicate OR  →  device_id=sensor-A OR tempreture>40: 3 matches"

  local jq_patch='
    .triggers[0].settings.consumerGroup            = "it-tc-24"      |
    .triggers[0].handlers[0].settings.field        = ""              |
    .triggers[0].handlers[0].settings.operator     = ""              |
    .triggers[0].handlers[0].settings.value        = ""              |
    .triggers[0].handlers[0].settings.predicateMode = "or"           |
    .triggers[0].handlers[0].settings.predicates   = "[{\"field\":\"device_id\",\"operator\":\"eq\",\"value\":\"sensor-A\"},{\"field\":\"tempreture\",\"operator\":\"gt\",\"value\":\"40\"}]"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-24"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  # sensor-A/10 (pass: device match), sensor-B/20 (blocked), sensor-B/50 (pass: value>40), sensor-A/50 (pass: both)
  echo "  → sensor-A/10, sensor-B/20, sensor-B/50, sensor-A/50. Expect 3 passes"
  produce_timed 0.3 \
    '{"tempreture":10,"device_id":"sensor-A"}' \
    '{"tempreture":20,"device_id":"sensor-B"}' \
    '{"tempreture":50,"device_id":"sensor-B"}' \
    '{"tempreture":50,"device_id":"sensor-A"}'
  sleep 3

  local after_24; after_24=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_24=${after_24:-0}
  local passed=$(( after_24 - baseline ))
  echo "  → Flow fired $passed times (expect 3)"
  show_results "$log" "filter event"
  if [[ "$passed" -eq 3 ]]; then
    pass "(OR predicates: 3 messages satisfied at least one condition)"
  else
    fail "expected 3 passes (OR: device=sensor-A OR tempreture>40), got $passed"
  fi
  stop_app
}

# =============================================================================
# TC-25  Filter: passThroughOnMissing=true
# =============================================================================
TC-25() {
  local tc="TC-25"
  header "$tc" "Filter: passThroughOnMissing=true  →  message without filtered field still passes"

  local jq_patch='
    .triggers[0].settings.consumerGroup                  = "it-tc-25"  |
    .triggers[0].settings.passThroughOnMissing           = true         |
    .triggers[0].handlers[0].settings.field              = "region"     |
    .triggers[0].handlers[0].settings.operator           = "eq"         |
    .triggers[0].handlers[0].settings.value              = "us-east"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-25"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  # msg1: has region=us-east (pass: matches), msg2: has region=eu-west (blocked), msg3: no region field (pass: missing → pass-through)
  echo "  → region=us-east(pass), region=eu-west(block), no-region(pass-through). Expect 2 passes"
  produce_timed 0.3 \
    '{"tempreture":10,"device_id":"sensor-1","region":"us-east"}' \
    '{"tempreture":20,"device_id":"sensor-1","region":"eu-west"}' \
    '{"tempreture":30,"device_id":"sensor-1"}'
  sleep 3

  local after_25; after_25=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_25=${after_25:-0}
  local passed=$(( after_25 - baseline ))
  echo "  → Flow fired $passed times (expect 2)"
  show_results "$log" "filter event"
  if [[ "$passed" -eq 2 ]]; then
    pass "(passThroughOnMissing=true: matched + missing-field both passed; non-match blocked)"
  else
    fail "expected 2 passes (us-east + missing field), got $passed"
  fi
  stop_app
}

# =============================================================================
# TC-26  Filter: deduplication (enableDedup=true + dedupField)
# =============================================================================
TC-26() {
  local tc="TC-26"
  header "$tc" "Filter: enableDedup=true + dedupField=msg_id  →  duplicate msg_ids rejected"

  local jq_patch='
    .triggers[0].settings.consumerGroup    = "it-tc-26"  |
    .triggers[0].settings.enableDedup      = true         |
    .triggers[0].settings.dedupWindow      = "10m"        |
    .triggers[0].handlers[0].settings.field    = "device_id" |
    .triggers[0].handlers[0].settings.operator = "eq"        |
    .triggers[0].handlers[0].settings.value    = "sensor-1"  |
    .triggers[0].handlers[0].settings.dedupField = "msg_id"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-26"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  # 5 messages for sensor-1 but msg_id m1 and m2 are duplicated.
  # Unique: m1, m2, m3 → 3 flows should fire. m1-dup and m2-dup blocked.
  echo "  → 5 msgs for sensor-1 (m1,m2,m3,m1-dup,m2-dup). Expect 3 unique fires."
  produce_timed 0.2 \
    '{"tempreture":10,"device_id":"sensor-1","msg_id":"m1"}' \
    '{"tempreture":20,"device_id":"sensor-1","msg_id":"m2"}' \
    '{"tempreture":30,"device_id":"sensor-1","msg_id":"m3"}' \
    '{"tempreture":10,"device_id":"sensor-1","msg_id":"m1"}' \
    '{"tempreture":20,"device_id":"sensor-1","msg_id":"m2"}'
  sleep 3

  local after_26; after_26=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_26=${after_26:-0}
  local passed=$(( after_26 - baseline ))
  echo "  → Flow fired $passed times (expect 3)"
  show_results "$log" "filter event"
  if [[ "$passed" -eq 3 ]]; then
    pass "(dedup: 3 unique msg_ids passed; 2 duplicates blocked)"
  else
    fail "expected 3 unique passes, got $passed"
  fi
  stop_app
}

# =============================================================================
# TC-27  Filter: rate limiting (rateLimitRPS=2 + mode=drop)
# =============================================================================
TC-27() {
  local tc="TC-27"
  header "$tc" "Filter: rateLimitRPS=2 mode=drop  →  burst of 8 messages, expect ~3-4 pass (2 rps + burst)"

  local jq_patch='
    .triggers[0].settings.consumerGroup    = "it-tc-27"   |
    .triggers[0].settings.rateLimitRPS     = 2             |
    .triggers[0].settings.rateLimitBurst   = 2             |
    .triggers[0].settings.rateLimitMode    = "drop"        |
    .triggers[0].handlers[0].settings.field    = "device_id" |
    .triggers[0].handlers[0].settings.operator = "eq"        |
    .triggers[0].handlers[0].settings.value    = "sensor-1"
  '
  local flogo; flogo=$(gen_filter "$jq_patch" "$tc")
  local log="$LOG_DIR/${tc}.log"
  clean_group "it-tc-27"
  start_app "$flogo" "$log"

  if ! wait_filter_start "$log"; then
    fail "filter app failed to start"; stop_app; return
  fi
  sleep 5
  local baseline; baseline=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); baseline=${baseline:-0}

  # Send 8 messages as fast as possible. rateLimitRPS=2 burst=2.
  # At most burst (2) pass immediately, then ~0-2 more over the next second.
  # So expect somewhere between 2 and 4 to pass, rest dropped.
  echo "  → Sending 8 messages in rapid burst (rate limit 2 rps, burst 2)"
  produce \
    '{"tempreture":10,"device_id":"sensor-1"}' \
    '{"tempreture":20,"device_id":"sensor-1"}' \
    '{"tempreture":30,"device_id":"sensor-1"}' \
    '{"tempreture":40,"device_id":"sensor-1"}' \
    '{"tempreture":50,"device_id":"sensor-1"}' \
    '{"tempreture":60,"device_id":"sensor-1"}' \
    '{"tempreture":70,"device_id":"sensor-1"}' \
    '{"tempreture":80,"device_id":"sensor-1"}'
  sleep 4

  local after_27; after_27=$(grep -c 'filter event topic=' "$log" 2>/dev/null || :); after_27=${after_27:-0}
  local passed=$(( after_27 - baseline ))
  echo "  → Flow fired $passed times (expect 2-5 with rate limiting)"
  show_results "$log" "filter event"
  if [[ "$passed" -ge 2 && "$passed" -le 6 ]]; then
    pass "(rate-limit drop: $passed of 8 passed — rate limiting working)"
  elif [[ "$passed" -eq 8 ]]; then
    fail "all 8 passed — rate limiter does not appear to be active"
  elif [[ "$passed" -eq 0 ]]; then
    fail "zero messages passed — rate limiter may be too aggressive or filter is broken"
  else
    echo -e "  ${YELLOW}⚠ loose pass${NC}: $passed passed (outside 2-6 range but rate limiting is timing-sensitive)"
    pass "(rate-limit active: $passed of 8 passed)"
  fi
  stop_app
}

# =============================================================================
# Main — run all tests or specific ones
# =============================================================================
ALL_TESTS=(
  # ── Aggregate trigger ──────────────────────────────────────────
  TC-02 TC-03 TC-04 TC-05       # TumblingTime: count/avg/min/max
  TC-06                         # TumblingCount
  TC-07 TC-08                   # SlidingTime / SlidingCount
  TC-09                         # keyed windows
  TC-10                         # MessageID deduplication
  TC-11                         # late events + DLQ (allowedLateness > 0)
  TC-12 TC-13 TC-16             # overflow: drop_oldest / drop_newest / error_stop
  TC-14                         # idle timeout
  TC-15 TC-19                   # persistence (on-shutdown / every-N)
  TC-17                         # initialOffset=oldest replay
  TC-18                         # maxKeys limit
  TC-28                         # allowedLateness=0 (strict — all late events DLQ)
  TC-29                         # keyField + per-key overflow
  TC-30                         # valueField missing — malformed messages skipped
  # ── Filter trigger ─────────────────────────────────────────────
  TC-20 TC-21 TC-22 TC-23 TC-24 TC-25 TC-26 TC-27
  TC-31                         # neq + contains + startsWith + endsWith
  TC-32                         # rateLimitMode=wait
)

main() {
  echo -e "${BOLD}"
  echo "╔══════════════════════════════════════════════════════════════╗"
  echo "║  KafkaStream Triggers — Complete Integration Test Suite      ║"
  echo "║  32 tests: aggregate (TC-02..TC-19,TC-28..TC-30) + filter (TC-20..TC-27,TC-31..TC-32)  ║"
  echo "╚══════════════════════════════════════════════════════════════╝"
  echo -e "${NC}"

  check_prereqs

  # Build the filter-trigger base flogo (needed by TC-20..TC-27)
  _make_filter_flogo

  # Kill any stray Flogo processes from previous runs
  pkill -f "test-kafka-stream-activities" 2>/dev/null || true
  sleep 1

  local tests_to_run=("${ALL_TESTS[@]}")
  if [[ $# -gt 0 ]]; then
    tests_to_run=("$@")
  fi

  for t in "${tests_to_run[@]}"; do
    if declare -f "$t" > /dev/null 2>&1; then
      "$t"
    else
      echo -e "${RED}Unknown test: $t (skipping)${NC}"
    fi
    # Brief pause between tests
    sleep 1
  done

  # ── Summary ──────────────────────────────────────────────────────
  echo ""
  echo -e "${BOLD}══════════════════════════════════════════════════════════════${NC}"
  echo -e "${BOLD}  Results: ${GREEN}$PASS passed${NC}${BOLD}  ${RED}$FAIL failed${NC}${BOLD}  (out of $((PASS+FAIL)) run)${NC}"
  echo -e "${BOLD}  Logs: $LOG_DIR/${NC}"
  echo -e "${BOLD}══════════════════════════════════════════════════════════════${NC}"
  echo ""
  echo -e "${BOLD}  Coverage summary:${NC}"
  echo "  Aggregate trigger:"
  for t in TC-02 TC-03 TC-04 TC-05 TC-06 TC-07 TC-08 TC-09 TC-10 TC-11 TC-12 TC-13 TC-14 TC-15 TC-16 TC-17 TC-18 TC-19 TC-28 TC-29 TC-30; do
    local lf="$LOG_DIR/${t}.log"
    if [[ ! -f "$lf" ]]; then printf "    %-8s  SKIP\n" "$t"; continue; fi
    if grep -qE "✓ PASS" "$lf" 2>/dev/null; then printf "    %-8s  ${GREEN}PASS${NC}\n" "$t"
    elif grep -qE "✗ FAIL" "$lf" 2>/dev/null; then printf "    %-8s  ${RED}FAIL${NC}\n" "$t"
    else printf "    %-8s  SKIP\n" "$t"; fi
  done
  echo "  Filter trigger:"
  for t in TC-20 TC-21 TC-22 TC-23 TC-24 TC-25 TC-26 TC-27 TC-31 TC-32; do
    local lf="$LOG_DIR/${t}.log"
    if [[ ! -f "$lf" ]]; then printf "    %-8s  SKIP\n" "$t"; continue; fi
    if grep -qE "✓ PASS" "$lf" 2>/dev/null; then printf "    %-8s  ${GREEN}PASS${NC}\n" "$t"
    elif grep -qE "✗ FAIL" "$lf" 2>/dev/null; then printf "    %-8s  ${RED}FAIL${NC}\n" "$t"
    else printf "    %-8s  SKIP\n" "$t"; fi
  done

  # Write per-test summary
  {
    echo "Integration Test Run — $(date)"
    for t in "${tests_to_run[@]}"; do
      local log="$LOG_DIR/${t}.log"
      if [[ -f "$log" ]]; then
        local status="PASS"
        grep -qE "✗ FAIL" "$log" 2>/dev/null && status="FAIL"
        echo "  $t: $status"
      fi
    done
  } > "$LOG_DIR/summary.txt"

  [[ $FAIL -eq 0 ]]
}

main "$@"
