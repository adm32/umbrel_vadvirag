#!/bin/sh
# Vadvirág Rendelések (rendeléskezelő) fordítása.
# Kimenet: ../../vadvirag-rendelesek/bin/rendeleskezelo-linux-amd64 és -arm64 (ezt futtatja az Umbrel),
# valamint ../../Vadvirag_Rendeleskezelo.exe (a régi, Windowsos tartalék változat).
# Utána: verzió növelése a vadvirag-rendelesek/umbrel-app.yml-ben, release note, commit, push.
set -e
cd "$(dirname "$0")"
go vet ./...
go test ./... >/dev/null
for a in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$a go build -trimpath -ldflags "-s -w" -o ../../vadvirag-rendelesek/bin/rendeleskezelo-linux-$a .
done
GOOS=windows GOARCH=amd64 go build -o ../../Vadvirag_Rendeleskezelo.exe .
echo "Kész. Ne felejtsd el növelni a verziót a vadvirag-rendelesek/umbrel-app.yml fájlban."
