#!/bin/sh
# Checks every program (src/*) without touching bin/: gofmt, go vet, go test and a test build.
# Run it before every commit: ./check.sh
cd "$(dirname "$0")" || exit 1
fail=0
check() { # check <dir>: prints problems, returns non-zero on any
  ( cd "$1" || exit 1
    f=$(gofmt -l .); [ -n "$f" ] && { echo "not gofmt-ed: $f"; exit 1; }
    go vet ./... && go test ./... >/tmp/check-test.log 2>&1 || { cat /tmp/check-test.log; exit 1; }
    go build -o /dev/null . )
}
for d in src/*/; do
  name=$(basename "$d")
  if out=$(check "$d" 2>&1); then echo "ok   $name"; else echo "FAIL $name"; echo "$out"; fail=1; fi
done
if [ "$fail" = 0 ]; then echo "All good."; else echo "Something failed."; exit 1; fi
