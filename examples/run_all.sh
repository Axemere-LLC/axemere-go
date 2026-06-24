#!/usr/bin/env bash
# run_all.sh — run all Go SDK examples against a local gateway.
#
# Usage:
#   ./examples/run_all.sh              # run all examples
#   ./examples/run_all.sh --skip 3,5   # skip examples 03 and 05
#   ./examples/run_all.sh --only 1,2   # run only examples 01 and 02
#
# Must be run from the sdk/go/ directory:
#   cd sdk/go && ./examples/run_all.sh

set -euo pipefail

EXAMPLES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SDK_DIR="$(dirname "$EXAMPLES_DIR")"

SKIP_LIST=""
ONLY_LIST=""

# Parse arguments.
while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip)
            SKIP_LIST="$2"
            shift 2
            ;;
        --only)
            ONLY_LIST="$2"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1" >&2
            echo "Usage: $0 [--skip N,M] [--only N,M]" >&2
            exit 1
            ;;
    esac
done

# Build the list of numbers from a comma-separated string.
contains_number() {
    local list="$1"
    local num="$2"
    [[ ",$list," == *",$num,"* ]]
}

# Discover all example directories in order.
mapfile -t EXAMPLE_DIRS < <(find "$EXAMPLES_DIR" -maxdepth 1 -mindepth 1 -type d | sort)

PASS=0
FAIL=0
SKIP=0

for dir in "${EXAMPLE_DIRS[@]}"; do
    name="$(basename "$dir")"
    # Extract the numeric prefix (01, 02, ...).
    num="${name%%_*}"

    # Apply --only / --skip filters.
    if [[ -n "$ONLY_LIST" ]] && ! contains_number "$ONLY_LIST" "$num"; then
        echo "SKIP  $name"
        (( SKIP++ )) || true
        continue
    fi
    if [[ -n "$SKIP_LIST" ]] && contains_number "$SKIP_LIST" "$num"; then
        echo "SKIP  $name"
        (( SKIP++ )) || true
        continue
    fi

    printf "RUN   %s ...\n" "$name"

    # Capture output; only print on failure.
    output_file="$(mktemp)"
    if ( cd "$SDK_DIR" && go run "./examples/$name/" ) >"$output_file" 2>&1; then
        echo "PASS  $name"
        (( PASS++ )) || true
    else
        exit_code=$?
        echo "FAIL  $name (exit $exit_code)"
        echo "--- output ---"
        cat "$output_file"
        echo "--- end output ---"
        (( FAIL++ )) || true
    fi
    rm -f "$output_file"
done

echo ""
echo "Summary: $PASS passed / $FAIL failed / $SKIP skipped"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
