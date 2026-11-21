#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then
  echo "usage: integration/test.sh /path/to/orchestrator-at-aea5eb0" >&2
  exit 2
fi
repo=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
orch=$(CDPATH='' cd -- "$1" && pwd)
version=aea5eb03fdb97b77d936d37633e99aab62792c08
if head=$(git -C "$orch" rev-parse HEAD 2>/dev/null) && [ "$head" != "$version" ]; then
  echo "Expected orchestrator $version, found $head" >&2
  exit 1
fi
diff -r "$orch/health/schemas/v1" "$repo/internal/wire/schemas"
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT
if [ -n "${BRIDGE_HEALTH_SERVICE_BIN:-}" ]; then
  cp "$BRIDGE_HEALTH_SERVICE_BIN/bridge-health-collector" "$BRIDGE_HEALTH_SERVICE_BIN/bridge-health-reporter" "$bin/"
else
  (cd "$repo" && go build -buildvcs=false -o "$bin/" ./cmd/...)
fi
(cd "$orch" && go build -buildvcs=false -ldflags='-extldflags=-Wl,--allow-multiple-definition' -o "$bin/orchestrator-socket" "$repo/testdata/orchestrator-socket/main.go")
cd "$repo"
BRIDGE_HEALTH_TEST_BIN="$bin" go test -race -count=1 -v -timeout=90s ./integration
