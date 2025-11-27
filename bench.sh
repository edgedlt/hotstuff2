#!/bin/bash

# bench.sh - Run HotStuff-2 benchmarks and generate HTML report
#
# Usage:
#   ./bench.sh          # Run all benchmarks and generate report
#   ./bench.sh quick    # Run quick benchmarks only (crypto + QC)
#   ./bench.sh crypto   # Run only crypto benchmarks
#   ./bench.sh full     # Run all benchmarks including slow ones
#
# Output: benchmark-YYYYMMDD-HHMMSS.html

set -e

# Configuration
TIMESTAMP=$(date +"%Y%m%d-%H%M%S")
OUTPUT_FILE="benchmark-${TIMESTAMP}.html"
TEMPLATE_FILE="bench/template.html"
BENCH_OUTPUT="/tmp/bench_output_${TIMESTAMP}.txt"
BENCH_TIME="500ms"  # Duration per benchmark

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo ""
echo -e "${BLUE}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║${NC}    ${GREEN}HotStuff-2 Benchmark Suite${NC}                                ${BLUE}║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Check if template exists
if [ ! -f "$TEMPLATE_FILE" ]; then
    echo -e "${RED}Error: Template file not found: $TEMPLATE_FILE${NC}"
    exit 1
fi

# Parse arguments
MODE=${1:-"default"}

case $MODE in
    "quick")
        echo -e "${YELLOW}Running quick benchmarks (crypto + QC)...${NC}"
        ;;
    "crypto")
        echo -e "${YELLOW}Running crypto benchmarks only...${NC}"
        ;;
    "full")
        echo -e "${YELLOW}Running full benchmark suite (this may take a while)...${NC}"
        BENCH_TIME="3s"
        ;;
    *)
        echo -e "${YELLOW}Running standard benchmarks...${NC}"
        ;;
esac
echo ""

# Initialize output file
> "$BENCH_OUTPUT"

# ==============================================================================
# Crypto Benchmarks
# ==============================================================================
echo -e "${GREEN}[1/5]${NC} Cryptographic operations..."
echo "  • Ed25519 (sign, verify, keygen)"
echo "  • BLS12-381 (sign, verify, aggregate)"

go test -bench="^Benchmark(Ed25519|BLS)" -benchmem -benchtime=$BENCH_TIME -run=^$ ./internal/crypto >> "$BENCH_OUTPUT" 2>&1 || true
echo -e "  ${GREEN}✓${NC} Crypto benchmarks complete"
echo ""

if [ "$MODE" == "crypto" ]; then
    echo -e "${GREEN}Crypto benchmarks complete.${NC}"
else

# ==============================================================================
# QC Operation Benchmarks
# ==============================================================================
echo -e "${GREEN}[2/5]${NC} QC (Quorum Certificate) operations..."
echo "  • Formation (4, 7, 10, 22 nodes)"
echo "  • Validation (4, 7, 10, 22 nodes)"
echo "  • Serialization"

go test -bench="^BenchmarkQC" -benchmem -benchtime=$BENCH_TIME -run=^$ . >> "$BENCH_OUTPUT" 2>&1 || true
echo -e "  ${GREEN}✓${NC} QC benchmarks complete"
echo ""

# ==============================================================================
# Vote Benchmarks
# ==============================================================================
echo -e "${GREEN}[3/5]${NC} Vote operations..."
echo "  • Creation, verification, serialization"

go test -bench="^BenchmarkVote" -benchmem -benchtime=$BENCH_TIME -run=^$ . >> "$BENCH_OUTPUT" 2>&1 || true
echo -e "  ${GREEN}✓${NC} Vote benchmarks complete"
echo ""

# ==============================================================================
# Consensus Benchmarks (hotstuff2_bench_test.go)
# ==============================================================================
echo -e "${GREEN}[4/5]${NC} Consensus operations..."
echo "  • Vote aggregation"
echo "  • Block validation"

go test -bench="^Benchmark(VoteAggregation|BlockValidation)" -benchmem -benchtime=$BENCH_TIME -run=^$ . >> "$BENCH_OUTPUT" 2>&1 || true
echo -e "  ${GREEN}✓${NC} Consensus benchmarks complete"
echo ""

# ==============================================================================
# Timer Benchmarks (optional in full mode)
# ==============================================================================
if [ "$MODE" == "full" ]; then
    echo -e "${GREEN}[5/5]${NC} Timer operations..."
    go test -bench="^Benchmark" -benchmem -benchtime=$BENCH_TIME -run=^$ ./timer >> "$BENCH_OUTPUT" 2>&1 || true
    echo -e "  ${GREEN}✓${NC} Timer benchmarks complete"
    echo ""
else
    echo -e "${YELLOW}[5/5]${NC} Timer benchmarks skipped (use './bench.sh full' to include)"
    echo ""
fi

fi # end crypto-only check

# ==============================================================================
# Generate HTML Report
# ==============================================================================
echo -e "${GREEN}Generating HTML report...${NC}"

# Build and run the report generator
go run ./cmd/benchgen "$TEMPLATE_FILE" "$BENCH_OUTPUT" "$OUTPUT_FILE"

echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║${NC}    Benchmark Complete!                                        ${GREEN}║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${BLUE}Report:${NC}  $OUTPUT_FILE"
echo -e "  ${BLUE}Open:${NC}    file://$(pwd)/$OUTPUT_FILE"
echo ""

# Cleanup
rm -f "$BENCH_OUTPUT"

# Summary of results
echo -e "${YELLOW}Quick Summary:${NC}"
echo "  Run 'cat $OUTPUT_FILE | grep -A1 \"ops/sec\"' for key metrics"
echo ""

# Open in browser if possible
if command -v xdg-open > /dev/null 2>&1; then
    echo -e "${GREEN}Opening report in browser...${NC}"
    xdg-open "$OUTPUT_FILE" 2>/dev/null &
elif command -v open > /dev/null 2>&1; then
    echo -e "${GREEN}Opening report in browser...${NC}"
    open "$OUTPUT_FILE" 2>/dev/null &
elif command -v start > /dev/null 2>&1; then
    echo -e "${GREEN}Opening report in browser...${NC}"
    start "$OUTPUT_FILE" 2>/dev/null &
fi
