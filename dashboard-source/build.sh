#!/bin/sh
# Az irányítópult fordítása az Umbrel apphoz (Go 1.22+ kell hozzá).
set -e
cd "$(dirname "$0")"
for a in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$a go build -trimpath -ldflags "-s -w" -o ../vadvirag-iranyitopult/bin/iranyitopult-linux-$a .
done
echo "Kész. Ne felejtsd el növelni a verziót a vadvirag-iranyitopult/umbrel-app.yml fájlban."
