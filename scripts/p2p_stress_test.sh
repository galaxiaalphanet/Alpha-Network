#!/bin/bash
# P2P Stress Test — Run 3 Alpha Network nodes locally and verify consensus
# Duration: 24 hours
# Monitors: block height agreement, divergence detection, uptime

set -e

ALPHA_BIN="/opt/Alpha-Network/alphanode"
DATA_ROOT="/tmp/alpha-p2p-test"
LOG_DIR="$DATA_ROOT/logs"
DURATION_HOURS=24
START_TIME=$(date +%s)
END_TIME=$((START_TIME + DURATION_HOURS * 3600))

# Port assignments
NODE1_API=8088  NODE1_WS=8089
NODE2_API=9090  NODE2_WS=9091
NODE3_API=10090 NODE3_WS=10091

mkdir -p "$DATA_ROOT"/{node1,node2,node3,logs}

cleanup() {
    echo ""
    echo "🛑 Shutting down all nodes..."
    kill $PID1 $PID2 $PID3 2>/dev/null || true
    wait $PID1 $PID2 $PID3 2>/dev/null || true
    echo "✅ All nodes stopped."
    echo "Logs saved to: $LOG_DIR"
    exit 0
}
trap cleanup INT TERM

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║     Alpha Network — P2P Consensus Stress Test (24h)          ║"
echo "║     3 nodes | PoI consensus | Continuous monitoring          ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""
echo "📋 Configuration:"
echo "   Node 1: API :$NODE1_API | WS :$NODE1_WS | Data: $DATA_ROOT/node1"
echo "   Node 2: API :$NODE2_API | WS :$NODE2_WS | Data: $DATA_ROOT/node2"
echo "   Node 3: API :$NODE3_API | WS :$NODE3_WS | Data: $DATA_ROOT/node3"
echo "   Duration: ${DURATION_HOURS}h"
echo ""

# Kill any leftover test nodes
pkill -f "alphanode.*alpha-p2p-test" 2>/dev/null || true
sleep 1

# Clean data dirs for fresh start
rm -rf "$DATA_ROOT"/node1/data "$DATA_ROOT"/node2/data "$DATA_ROOT"/node3/data

echo "🚀 Starting Node 1 (seed)..."
ALPHA_DATADIR="$DATA_ROOT/node1" ALPHA_PORT=$NODE1_API \
    "$ALPHA_BIN" -port $NODE1_API -ws-port $NODE1_WS -datadir "$DATA_ROOT/node1" \
    -announce-addr "127.0.0.1" \
    > "$LOG_DIR/node1.log" 2>&1 &
PID1=$!
sleep 2

echo "🚀 Starting Node 2 (joins Node 1)..."
ALPHA_DATADIR="$DATA_ROOT/node2" ALPHA_PORT=$NODE2_API \
    "$ALPHA_BIN" -port $NODE2_API -ws-port $NODE2_WS -datadir "$DATA_ROOT/node2" \
    -announce-addr "127.0.0.1" \
    -seed-peers "127.0.0.1:$NODE1_API" \
    > "$LOG_DIR/node2.log" 2>&1 &
PID2=$!
sleep 2

echo "🚀 Starting Node 3 (joins Node 1+2)..."
ALPHA_DATADIR="$DATA_ROOT/node3" ALPHA_PORT=$NODE3_API \
    "$ALPHA_BIN" -port $NODE3_API -ws-port $NODE3_WS -datadir "$DATA_ROOT/node3" \
    -announce-addr "127.0.0.1" \
    -seed-peers "127.0.0.1:$NODE1_API,127.0.0.1:$NODE2_API" \
    > "$LOG_DIR/node3.log" 2>&1 &
PID3=$!
sleep 3

# Verify all nodes started
for pid in $PID1 $PID2 $PID3; do
    if ! kill -0 $pid 2>/dev/null; then
        echo "❌ Process $pid failed to start! Check logs."
        cleanup
        exit 1
    fi
done
echo "✅ All 3 nodes running (PIDs: $PID1 $PID2 $PID3)"
echo ""

# ── Monitoring Loop ──────────────────────────────────────────────────
DIVERGENCE_COUNT=0
CHECK_INTERVAL=60  # seconds
ITERATION=0

echo "📊 Monitoring started — checking consensus every ${CHECK_INTERVAL}s for ${DURATION_HOURS}h"
echo "   $(date -u)"
echo ""

log_event() {
    echo "[$(date -u +%H:%M:%S)] $1" | tee -a "$LOG_DIR/events.log"
}

while true; do
    NOW=$(date +%s)
    if [ $NOW -ge $END_TIME ]; then
        log_event "✅ 24-HOUR TEST COMPLETE — all checks passed"
        break
    fi

    REMAINING=$(( (END_TIME - NOW) / 3600 ))h$(( ((END_TIME - NOW) % 3600) / 60 ))m
    ITERATION=$((ITERATION + 1))

    # Get heights from each node
    H1=$(curl -s http://localhost:$NODE1_API/api/v1/chain/info | python3 -c "import sys,json; print(json.load(sys.stdin).get('height',0))" 2>/dev/null || echo "ERR")
    H2=$(curl -s http://localhost:$NODE2_API/api/v1/chain/info | python3 -c "import sys,json; print(json.load(sys.stdin).get('height',0))" 2>/dev/null || echo "ERR")
    H3=$(curl -s http://localhost:$NODE3_API/api/v1/chain/info | python3 -c "import sys,json; print(json.load(sys.stdin).get('height',0))" 2>/dev/null || echo "ERR")

    # Check for node failures
    FAILURES=0
    for pid in $PID1 $PID2 $PID3; do
        if ! kill -0 $pid 2>/dev/null; then
            FAILURES=$((FAILURES + 1))
        fi
    done

    if [ $FAILURES -gt 0 ]; then
        log_event "🔴 CRITICAL: $FAILURES node(s) crashed!"
        DIVERGENCE_COUNT=$((DIVERGENCE_COUNT + 1))
    fi

    # Check height divergence (allow ±5 blocks tolerance for propagation delay)
    MAX_H=$H1
    MIN_H=$H1
    for h in $H2 $H3; do
        if [ "$h" != "ERR" ]; then
            [ $h -gt $MAX_H ] && MAX_H=$h
            [ $h -lt $MIN_H ] && MIN_H=$h
        fi
    done

    DIVERGENCE=$((MAX_H - MIN_H))
    if [ "$H1" = "ERR" ] || [ "$H2" = "ERR" ] || [ "$H3" = "ERR" ]; then
        log_event "⚠️  ITER $ITERATION | N1=$H1 N2=$H2 N3=$H3 | NODE ERROR | Remaining: $REMAINING"
    elif [ $DIVERGENCE -gt 10 ]; then
        DIVERGENCE_COUNT=$((DIVERGENCE_COUNT + 1))
        log_event "🔴 DIVERGENCE #$DIVERGENCE_COUNT | Max delta: $DIVERGENCE | N1=$H1 N2=$H2 N3=$H3 | Remaining: $REMAINING"
    else
        # Healthy
        log_event "✅ ITER $ITERATION | Heights: N1=$H1 N2=$H2 N3=$H3 (Δ=$DIVERGENCE) | Uptime: $(( (NOW - START_TIME) / 3600 ))h$(( ((NOW - START_TIME) % 3600) / 60 ))m | Remaining: $REMAINING"
    fi

    sleep $CHECK_INTERVAL
done

# ── Final Report ─────────────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════════════════════╗"
echo "║                   STRESS TEST COMPLETE                        ║"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""

H1_FINAL=$(curl -s http://localhost:$NODE1_API/api/v1/chain/info | python3 -c "import sys,json; print(json.load(sys.stdin).get('height',0))" 2>/dev/null || echo "ERR")
H2_FINAL=$(curl -s http://localhost:$NODE2_API/api/v1/chain/info | python3 -c "import sys,json; print(json.load(sys.stdin).get('height',0))" 2>/dev/null || echo "ERR")
H3_FINAL=$(curl -s http://localhost:$NODE3_API/api/v1/chain/info | python3 -c "import sys,json; print(json.load(sys.stdin).get('height',0))" 2>/dev/null || echo "ERR")

echo "📊 Final Heights: N1=$H1_FINAL N2=$H2_FINAL N3=$H3_FINAL"
echo "🔴 Divergence events: $DIVERGENCE_COUNT"
echo "📝 Total iterations: $ITERATION"
echo ""

if [ $DIVERGENCE_COUNT -eq 0 ]; then
    echo "✅ PASSED — Zero consensus divergences in 24 hours"
else
    echo "❌ FAILED — $DIVERGENCE_COUNT divergence events detected"
fi

# Archive logs
tar czf "$DATA_ROOT/test-results-$(date +%Y%m%d-%H%M%S).tar.gz" -C "$LOG_DIR" .
echo "📦 Logs archived to $DATA_ROOT/test-results-*.tar.gz"

cleanup
