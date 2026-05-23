#!/bin/bash
set -e

echo "=== Docker Cleanup Test ==="
echo "Step 1: Check initial state"
INITIAL_COUNT=$(docker ps -q | wc -l)
echo "Initial containers: $INITIAL_COUNT"

echo ""
echo "Step 2: Starting strix scan in background..."
cd /home/ubuntu/strix
./strix-go/bin/strix -t http://approval.bespinglobal.us/ --instruction "test cleanup" -n > /tmp/strix-test.log 2>&1 &
STRIX_PID=$!
echo "Strix PID: $STRIX_PID"

echo ""
echo "Step 3: Waiting 20 seconds for container to be created..."
sleep 20

CONTAINER_COUNT=$(docker ps -q | wc -l)
echo "Containers running: $CONTAINER_COUNT"

if [ "$CONTAINER_COUNT" -gt "$INITIAL_COUNT" ]; then
    echo "✓ Container was created"
    docker ps --format "table {{.ID}}\t{{.Names}}\t{{.Status}}"
else
    echo "✗ No container was created (scan may have failed early)"
    echo "Strix log:"
    tail -20 /tmp/strix-test.log
    exit 1
fi

echo ""
echo "Step 4: Killing strix process..."
kill $STRIX_PID 2>/dev/null || true

echo "Waiting 10 seconds for cleanup..."
sleep 10

FINAL_COUNT=$(docker ps -q | wc -l)
echo "Final containers: $FINAL_COUNT"

if [ "$FINAL_COUNT" -eq "$INITIAL_COUNT" ]; then
    echo ""
    echo "✅ SUCCESS: Docker cleanup working! Container was removed on process exit."
    exit 0
else
    echo ""
    echo "❌ FAILURE: Containers still running after process kill:"
    docker ps --format "table {{.ID}}\t{{.Names}}\t{{.Status}}"
    echo ""
    echo "Attempting manual cleanup..."
    docker ps -q | xargs -r docker stop
    docker ps -aq | xargs -r docker rm
    exit 1
fi
