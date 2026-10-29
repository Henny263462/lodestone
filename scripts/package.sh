#!/usr/bin/env sh
# Baut die Linux-Binaries (amd64) und legt sie samt Dockerfiles in dist/ ab. Danach:
#   docker build -f Dockerfile.panel.prebuilt -t lodestone-panel:local .
#   docker build -f Dockerfile.node.prebuilt  -t lodestone-node:local  .
set -eu
cd "$(dirname "$0")/.."
sh scripts/build-web.sh
rm -rf dist && mkdir -p dist
export CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}"
# VERSION (zum Beispiel 0.3.0) steht für die Update-Prüfung im Binary; ohne Angabe „dev“.
LDFLAGS="-s -w -X main.version=${VERSION:-dev}"
go build -trimpath -ldflags="$LDFLAGS" -o dist/lodestone-panel ./cmd/lodestone-panel
go build -trimpath -ldflags="$LDFLAGS" -o dist/lodestone-node ./cmd/lodestone-node
cp deploy/Dockerfile.panel.prebuilt deploy/Dockerfile.node.prebuilt dist/
ls -la dist
