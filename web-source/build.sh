#!/bin/sh
# A weboldal fordítása az Umbrel apphoz (Go 1.22+ és python3 kell hozzá).
# A tartalék étlap (ha a rendeléskezelő épp nem elérhető) a ../source/menu.json-ból készül.
set -e
cd "$(dirname "$0")"
python3 - <<'PY'
import json, re
menu = json.load(open('../source/menu.json'))
src = open('../source/main.go', encoding='utf-8').read()
block = src[src.index('Dijak: map[string]int{'):]
block = block[:block.index('},')]
fees = sorted(({"name": n, "fee": int(f)} for n, f in re.findall(r'"([^"]+)":\s*(\d+)', block)), key=lambda x: x["name"])
json.dump({"live": False, "restaurant": "Vadvirág Étterem", "phone": "06 20 339 5099", "open": True, "notice": "",
           "sections": menu["sections"], "fees": fees}, open('menu-fallback.json', 'w'), ensure_ascii=False)
print("tartalék étlap:", sum(len(s["items"]) for s in menu["sections"]), "étel,", len(fees), "település")
PY
for a in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$a go build -trimpath -ldflags "-s -w" -o ../vadvirag-weboldal/bin/weboldal-linux-$a .
done
echo "Kész. Ne felejtsd el növelni a verziót a vadvirag-weboldal/umbrel-app.yml fájlban."
