#!/usr/bin/env bash
# Reruns the framework benchmarks and saves the raw output under results/.
# Usage: bash bench/run.sh [count]   (default 8 runs per benchmark)
set -euo pipefail
cd "$(dirname "$0")"
count=${1:-8}
mkdir -p results
out="results/$(date +%Y-%m-%d)-$(go env GOOS)-$(go env GOARCH).txt"

cpu() {
	case "$(go env GOOS)" in
	linux) lscpu 2>/dev/null | sed -n 's/^Model name:[[:space:]]*//p' ;;
	darwin) sysctl -n machdep.cpu.brand_string ;;
	windows) powershell -NoProfile -Command "(Get-CimInstance Win32_Processor).Name" | tr -d '\r' ;;
	*) echo unknown ;;
	esac
}

# Hybrid CPUs (performance and efficiency cores) make a single-threaded
# benchmark bimodal unless it is pinned to one core, so the test binary is
# built once and run with an affinity mask where the OS offers one.
go test -c -o bench.test .
trap 'rm -f bench.test' EXIT
args=(-test.run '^$' -test.bench . -test.benchmem -test.cpu 1 -test.count "$count")
run() {
	case "$(go env GOOS)" in
	linux) taskset -c 2 ./bench.test "${args[@]}" ;;
	windows) cmd //c "start /b /wait /affinity 4 bench.test ${args[*]}" ;;
	*) ./bench.test "${args[@]}" ;;
	esac
}

{
	echo "# $(go version)"
	echo "# $(go env GOOS)/$(go env GOARCH), $(cpu), pinned to one core"
	echo "# go test -run '^\$' -bench . -benchmem -cpu 1 -count $count .  (via bench/run.sh)"
	run
} | tee "$out"

echo "saved $out"
if command -v benchstat >/dev/null 2>&1; then
	benchstat "$out"
else
	echo "install benchstat for medians: go install golang.org/x/perf/cmd/benchstat@latest"
fi
