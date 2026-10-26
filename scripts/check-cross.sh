#!/usr/bin/env bash
# Cross-compile mcli for every platform it claims to support.
#
# The maintainer runs mcli from Git Bash on Windows, so a build that only compiles
# on Linux is not good enough. Compiling is not the same as behaving — that is what
# docs/status.md is for — but it is the part that can be checked from anywhere, and
# it has already caught one real bug (XSTS error codes larger than a 32-bit int
# broke every 32-bit target).
#
# Run from the repository root:  ./scripts/check-cross.sh
set -u

targets=(
  "linux amd64"
  "linux arm64"
  "linux 386"
  "windows amd64"
  "windows arm64"
  "darwin amd64"
  "darwin arm64"
  "freebsd amd64"
)

fail=0
for target in "${targets[@]}"; do
  read -r goos goarch <<<"$target"
  printf '%-16s ' "$goos/$goarch"
  if out=$(GOOS="$goos" GOARCH="$goarch" go build -o /dev/null . 2>&1); then
    echo "ok"
  else
    echo "FAILED"
    echo "$out" | sed 's/^/    /'
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo
  echo "a target that does not compile is a bug: fix it, or drop the claim from README/docs."
  exit 1
fi
echo
echo "all targets compile."