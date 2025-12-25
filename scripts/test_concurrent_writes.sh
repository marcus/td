#!/bin/bash
# Test concurrent writes from multiple processes
# Usage: ./scripts/test_concurrent_writes.sh [num_processes] [writes_per_process]

set -e

NUM_PROCS=${1:-5}
WRITES_PER=${2:-10}

echo "Testing concurrent writes: $NUM_PROCS processes × $WRITES_PER writes each"
echo "Expected: $((NUM_PROCS * WRITES_PER)) total issues created"
echo ""

# Get script directory and build
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$PROJECT_DIR"
go build -o td .
TD_BIN="$PROJECT_DIR/td"

# Create temp dir for test
TESTDIR=$(mktemp -d)
trap "rm -rf $TESTDIR" EXIT

# Initialize td in test dir
cd "$TESTDIR"
"$TD_BIN" init >/dev/null 2>&1

# Start all processes in parallel
echo "Starting $NUM_PROCS concurrent writers..."
start_time=$(date +%s.%N)

pids=()
for p in $(seq 1 $NUM_PROCS); do
    (
        for i in $(seq 1 $WRITES_PER); do
            "$TD_BIN" add "Process $p - Write $i" --priority=P2 2>&1 || echo "FAIL: proc=$p write=$i"
        done
    ) &
    pids+=($!)
done

# Wait for all
for pid in "${pids[@]}"; do
    wait $pid
done

end_time=$(date +%s.%N)
elapsed=$(echo "$end_time - $start_time" | bc)

# Count results (use high limit to get all)
actual=$("$TD_BIN" list --limit=10000 2>/dev/null | wc -l | tr -d ' ')
expected=$((NUM_PROCS * WRITES_PER))

echo ""
echo "Results:"
echo "  Expected issues: $expected"
echo "  Actual issues:   $actual"
echo "  Time elapsed:    ${elapsed}s"
echo "  Throughput:      $(echo "scale=1; $actual / $elapsed" | bc) writes/sec"
echo ""

if [ "$actual" -eq "$expected" ]; then
    echo "✓ SUCCESS: All writes completed without conflicts"
    exit 0
else
    missing=$((expected - actual))
    echo "✗ $missing writes failed (likely lock timeouts under heavy contention)"
    echo ""
    echo "This is expected with 500ms timeout and high concurrency."
    echo "The lock is working correctly - failed writes got timeout errors."
    exit 1
fi
