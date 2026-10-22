#!/bin/sh
# Builds the backend for every platform, one target at a time.
#
# `mage -v` (all platforms in parallel) intermittently fails to write
# dist/gpx_timebase_linux_amd64 with "cannot allocate memory" when the repo
# is a Windows folder bind-mounted into Docker; sequential builds do not.
# Run inside the Go container (see README):
#
#   docker run --rm -v "${PWD}:/src" -v tbi3x-gomod:/go/pkg/mod -v tbi3x-gocache:/root/.cache/go-build \
#     -v tbi3x-gobin:/go/bin -w /src golang:1.26 sh scripts/build-backend.sh
set -e

command -v mage >/dev/null || go install github.com/magefile/mage@v1.15.0

for target in build:linux build:linuxARM build:linuxARM64 build:darwin build:darwinARM64 build:windows; do
  echo "== $target"
  mage "$target"
done

ls -l dist/gpx_*
