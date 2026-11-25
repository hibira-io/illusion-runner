#!/usr/bin/env bash
# Cross-compiles the runner alone (no engine) for every platform into dist/:
# illusion-runner-<version>-<os>-<arch>.zip with LICENSE, NOTICE and README.md.
# These are for people who run their own engine (ComfyUI, or their own sd-cli
# build); the engine bundles are built by .github/workflows/build.yml.
#   scripts/build.sh v0.1.0
set -euo pipefail
version="${1:-dev}"
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
mkdir -p dist stage
for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64 windows/arm64; do
  goos="${t%/*}" goarch="${t#*/}"
  os="$goos"; [ "$os" = darwin ] && os=macos
  arch="$goarch"; [ "$arch" = amd64 ] && arch=x64
  name="illusion-runner-${version}-${os}-${arch}"
  stage="stage/$name"
  rm -rf "$stage" && mkdir -p "$stage"
  exe="illusion-runner"; [ "$goos" = windows ] && exe="illusion-runner.exe"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "$stage/$exe" ./cmd/illusion-runner
  cp LICENSE NOTICE README.md "$stage/"
  rm -f "dist/$name.zip"
  (cd stage && zip -qry "$root/dist/$name.zip" "$name")
  echo "Packaged dist/$name.zip"
done
