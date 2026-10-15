#!/bin/sh
# Új verzió fordítása az Umbrel apphoz (Go 1.22+ kell hozzá).
set -e
cd "$(dirname "$0")"
for a in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$a go build -trimpath -ldflags "-s -w" -o ../vadvirag-rendelesek/bin/rendeleskezelo-linux-$a .
done
GOOS=windows GOARCH=amd64 go build -o ../Vadvirag_Rendeleskezelo.exe .
echo "Kész. Ne felejtsd el növelni a verziót a vadvirag-rendelesek/umbrel-app.yml fájlban."
