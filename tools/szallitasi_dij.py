#!/usr/bin/env python3
"""
Vadvirág Étterem – szállítási díj kalkulátor
============================================

Megkeresi a térképen (OpenStreetMap) az összes kiszállítási települést, lekéri a
valódi autós távolságot az étteremtől, és kiszámolja a díjat:

    díj = 250 Ft + 110 Ft × km, 100 Ft-ra kerekítve

Az eredményt táblázatba menti (szallitasi_dijak.xlsx) a program mellé, és
összeveti a korábbi (Gemini által becsült) kilométerekkel.

Futtatás Macen:  Terminál  →  python3 ~/Downloads/szallitasi_dij.py
Csak a Python beépített részeit használja, nem kell semmit telepíteni.
Internet kell hozzá. Kb. 2 perc (a térképkereső másodpercenként 1 kérést enged).
"""

import json, math, os, sys, time, urllib.parse, urllib.request, zipfile
from xml.sax.saxutils import escape

# ---------------------------------------------------------------- beállítások

ALAPDIJ = 250        # Ft
KM_DIJ = 110         # Ft / km
KEREKITES = 100      # Ft
# Az étterem helye (szélesség, hosszúság). Ha None, a program a "Zalaudvarnok" pontot használja.
# Pontosabbhoz: Google Térkép → jobb klikk az étteremre → a koordinátákra kattintva kimásolod,
# és ide írod, pl.  ETTEREM = (46.91289, 17.10685)
ETTEREM = None

# Település, és a korábbi (Gemini) távolság km-ben
TELEPULESEK = [
    ("Zalaudvarnok", 0),
    ("Tüskeszentpéter", 2),
    ("Zalaszentlászló", 2.5),
    ("Zalaszentgrót", 3.5),
    ("Aranyod", 4.5),
    ("Barátsziget", 5),
    ("Gyülevész", 5),
    ("Csáford", 5.5),
    ("Sénye", 5.5),
    ("Tekenye", 5.5),
    ("Tilaj", 5.5),
    ("Kehida", 6),
    ("Kisszentgrót", 6),
    ("Kehidakustány", 6.5),
    ("Zalakoppány", 7),
    ("Zalacsány", 8),
    ("Zalabér", 8.5),
    ("Dötk", 9),
    ("Türje", 9),
    ("Kallósd", 10),
    ("Ligetfalva", 10),
    ("Batyk", 10.5),
    ("Zalaköveskút", 10.5),
    ("Almásháza", 11),
    ("Pakod", 11),
    ("Bókaháza", 11.5),
    ("Zalavég", 12),
    ("Nemesbük", 12.5),
    ("Szalapa", 12.5),
    ("Padár", 13),
    ("Óhíd", 13),
    ("Vindornyafok", 13.5),
    ("Bezeréd", 14),
    ("Gétye", 14),
    ("Karmacs", 14),
    ("Zalaapáti", 14),
    ("Bérbaltavár", 14.5),
    ("Mihályfa", 14.5),
    ("Felsőpáhok", 15),
    ("Kisgörbő", 15),
    ("Pókaszepetk", 15),
    ("Szentpéterúr", 15),
    ("Nagykapornak", 15.5),
    ("Nagytilaj", 15.5),
    ("Szentgyörgyvár", 15.5),
    ("Vindornyaszőlős", 15.5),
    ("Gyűrűs", 16),
    ("Hévíz", 16),
    ("Orbányosfa", 16),
    ("Dabronc", 16.5),
    ("Kisvásárhely", 16.5),
    ("Zalaerdőd", 16.5),
    ("Zalaszántó", 16.5),
    ("Alsópáhok", 17),
    ("Mikosszéplak", 17),
    ("Nagygörbő", 17),
    ("Zalaistvánd", 17),
    ("Esztergályhorváti", 17.5),
    ("Kemendollár", 17.5),
    ("Misefa", 17.5),
    ("Vindornyalak", 17.5),
    ("Zalaigrice", 17.5),
    ("Egeraracsa", 18),
    ("Hetyefő", 18),
    ("Nemesrádó", 18),
    ("Rezi", 18),
    ("Sümegcsehi", 18),
    ("Csehi", 18.5),
    ("Döbröce", 18.5),
    ("Pacsa", 18.5),
    ("Oszkó", 19),
    ("Zalaszentmárton", 19),
    ("Csehimindszent", 19.5),
    ("Cserszegtomaj", 19.5),
    ("Dióskál", 19.5),
    ("Gógánfa", 19.5),
    ("Sármellék", 19.5),
    ("Vöckönd", 19.5),
    ("Bazsi", 20),
    ("Sümegprága", 20),
    ("Zalagyömörő", 20),
    ("Zalaszentlőrinc", 20),
]

# Településrészek: így a térképkereső biztosan a jót találja meg
KERESES = {
    "Kehida": "Kehida, Kehidakustány",
    "Barátsziget": "Barátsziget, Kehidakustány",
    "Gyülevész": "Gyülevész, Zalaszentlászló",
    "Aranyod": "Aranyod, Zalaszentgrót",
    "Csáford": "Csáford, Zalaszentgrót",
    "Kisszentgrót": "Kisszentgrót, Zalaszentgrót",
    "Tüskeszentpéter": "Tüskeszentpéter, Zalaszentgrót",
    "Zalaudvarnok": "Zalaudvarnok, Zalaszentgrót",
}

UA = "VadviragEtterem-szallitasi-dij/1.0"
HELYEK = ("village", "town", "city", "hamlet", "suburb", "neighbourhood", "quarter", "isolated_dwelling", "municipality")
MAPPA = os.path.dirname(os.path.abspath(__file__))
CACHE = os.path.join(MAPPA, "szallitasi_dij_cache.json")


def get(url):
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=40) as r:
        return json.load(r)


def geocode(nev, cache):
    if nev in cache:
        return cache[nev]
    params = {"q": KERESES.get(nev, nev), "countrycodes": "hu", "viewbox": "16.6,47.25,17.6,46.5",
              "bounded": 1, "format": "jsonv2", "limit": 5}
    res = get("https://nominatim.openstreetmap.org/search?" + urllib.parse.urlencode(params))
    time.sleep(1.1)  # a térképkereső szabálya: legfeljebb 1 kérés másodpercenként
    jo = [r for r in res if r.get("addresstype") in HELYEK] or res
    if not jo:
        return None  # nem mentjük, legközelebb újra próbálja
    r = jo[0]
    cache[nev] = {"lat": float(r["lat"]), "lon": float(r["lon"]), "hol": r.get("display_name", "")}
    return cache[nev]


def legvonal(a, b):
    p1, p2 = math.radians(a[0]), math.radians(b[0])
    dp, dl = p2 - p1, math.radians(b[1] - a[1])
    h = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * 6371.0 * math.asin(math.sqrt(h))


def utvonalak(start, celok):
    """Autós távolság (km) és menetidő (perc) a starttól minden célig, egyetlen kéréssel."""
    coords = ";".join(f"{lon:.5f},{lat:.5f}" for lat, lon in [start] + celok)
    j = get(f"https://router.project-osrm.org/table/v1/driving/{coords}?sources=0&annotations=distance,duration")
    if j.get("code") != "Ok":
        raise RuntimeError(j)
    d, t = j["distances"][0][1:], j["durations"][0][1:]
    return [(None if x is None else x / 1000, None if y is None else y / 60) for x, y in zip(d, t)]


def dij(km):
    return int(math.floor((ALAPDIJ + KM_DIJ * km) / KEREKITES + 0.5)) * KEREKITES


# ---------------------------------------------------------------- egyszerű .xlsx író (csak beépített modulokkal)
# stílusok: 0 sima, 1 fejléc, 2 km, 3 Ft, 4 km sárga, 5 Ft félkövér, 6 szöveg sárga

def xlsx(path, sorok, szelessegek):
    def col(i):
        s, i = "", i + 1
        while i:
            i, m = divmod(i - 1, 26)
            s = chr(65 + m) + s
        return s

    def cell(ref, v, s):
        if isinstance(v, (int, float)) and not isinstance(v, bool):
            return f'<c r="{ref}" s="{s}"><v>{v}</v></c>'
        return f'<c r="{ref}" t="inlineStr" s="{s}"><is><t xml:space="preserve">{escape(str(v))}</t></is></c>'

    rows = []
    for r, sor in enumerate(sorok, 1):
        cs = []
        for c, v in enumerate(sor):
            if v is None or v == "":
                continue
            val, st = v if isinstance(v, tuple) else (v, 1 if r == 1 else 0)
            cs.append(cell(f"{col(c)}{r}", val, st))
        rows.append(f'<row r="{r}">{"".join(cs)}</row>')
    last = f"{col(len(sorok[0]) - 1)}{len(sorok)}"
    cols = "".join(f'<col min="{i+1}" max="{i+1}" width="{w}" customWidth="1"/>' for i, w in enumerate(szelessegek))
    X = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    M = 'xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"'
    R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
    sheet = (f'{X}<worksheet {M}><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" '
             f'activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols>{cols}</cols>'
             f'<sheetData>{"".join(rows)}</sheetData><autoFilter ref="A1:{last}"/></worksheet>')
    styles = (f'{X}<styleSheet {M}><numFmts count="2"><numFmt numFmtId="164" formatCode="0.0"/>'
              '<numFmt numFmtId="165" formatCode="#,##0 &quot;Ft&quot;"/></numFmts>'
              '<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>'
              '<fills count="4"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>'
              '<fill><patternFill patternType="solid"><fgColor rgb="FFF7E3DF"/></patternFill></fill>'
              '<fill><patternFill patternType="solid"><fgColor rgb="FFFFF2CC"/></patternFill></fill></fills>'
              '<borders count="1"><border/></borders><cellStyleXfs count="1"><xf/></cellStyleXfs><cellXfs count="7"><xf/>'
              '<xf fontId="1" fillId="2" applyFont="1" applyFill="1"/>'
              '<xf numFmtId="164" applyNumberFormat="1"/>'
              '<xf numFmtId="165" applyNumberFormat="1"/>'
              '<xf numFmtId="164" fillId="3" applyNumberFormat="1" applyFill="1"/>'
              '<xf numFmtId="165" fontId="1" applyNumberFormat="1" applyFont="1"/>'
              '<xf fillId="3" applyFill="1"/></cellXfs>'
              '<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>')
    wb = (f'{X}<workbook {M} xmlns:r="{R}"><sheets><sheet name="Szállítási díjak" sheetId="1" r:id="rId1"/></sheets>'
          f'<definedNames><definedName name="_xlnm._FilterDatabase" localSheetId="0" hidden="1">'
          f"'Szállítási díjak'!$A$1:${col(len(sorok[0]) - 1)}${len(sorok)}</definedName></definedNames></workbook>")
    ct = (f'{X}<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
          '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
          '<Default Extension="xml" ContentType="application/xml"/>'
          '<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>'
          '<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>'
          '<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>')
    P = "http://schemas.openxmlformats.org/package/2006/relationships"
    rels = (f'{X}<Relationships xmlns="{P}"><Relationship Id="rId1" Type="{R}/officeDocument" '
            'Target="xl/workbook.xml"/></Relationships>')
    wbrels = (f'{X}<Relationships xmlns="{P}"><Relationship Id="rId1" Type="{R}/worksheet" Target="worksheets/sheet1.xml"/>'
              f'<Relationship Id="rId2" Type="{R}/styles" Target="styles.xml"/></Relationships>')
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr("[Content_Types].xml", ct)
        z.writestr("_rels/.rels", rels)
        z.writestr("xl/_rels/workbook.xml.rels", wbrels)
        z.writestr("xl/workbook.xml", wb)
        z.writestr("xl/styles.xml", styles)
        z.writestr("xl/worksheets/sheet1.xml", sheet)


# ---------------------------------------------------------------- futás

def main():
    cache = {}
    try:
        cache = json.load(open(CACHE, encoding="utf-8"))
    except Exception:
        pass

    uj_keres = sum(1 for n, _ in TELEPULESEK if n not in cache)
    print(f"Települések keresése a térképen ({len(TELEPULESEK)} db, kb. {uj_keres * 1.2 / 60:.0f}–{uj_keres * 1.2 / 60 + 1:.0f} perc)...")
    helyek = {}
    for i, (nev, _) in enumerate(TELEPULESEK, 1):
        try:
            helyek[nev] = geocode(nev, cache)
        except Exception as e:
            print(f"  ! {nev}: hiba a keresésnél ({e})")
            helyek[nev] = None
        h = helyek[nev]
        print(f"  {i:2}/{len(TELEPULESEK)}  {nev:<20} " + (f"{h['lat']:.4f}, {h['lon']:.4f}" if h else "NEM TALÁLHATÓ"))
        json.dump(cache, open(CACHE, "w", encoding="utf-8"), ensure_ascii=False, indent=1)

    z = helyek.get("Zalaudvarnok")
    start = ETTEREM or (z and (z["lat"], z["lon"]))
    if not start:
        sys.exit("Az étterem helyét nem találtam – írd be az ETTEREM sorba a koordinátákat.")

    talalt = [(n, (helyek[n]["lat"], helyek[n]["lon"])) for n, _ in TELEPULESEK if helyek.get(n)]
    print("Autós távolságok lekérése...")
    utak = dict(zip([n for n, _ in talalt], utvonalak(start, [p for _, p in talalt])))

    fejlec = ["Település", "Autós táv (km)", "Menetidő (perc)", "Légvonal (km)", "Gemini km", "Eltérés (km)",
              "Új díj (valódi km)", "Díj a Gemini km-rel", "Különbség", "Megjegyzés", "Útvonal a térképen"]
    sorok, eredmeny, rossz = [], [], 0
    for nev, gem in TELEPULESEK:
        h = helyek.get(nev)
        km, perc = utak.get(nev, (None, None))
        if not h or km is None:
            sorok.append([nev, "", "", "", (gem, 2), "", "", (dij(gem), 3), "",
                          ("nem találtam a térképen – ellenőrizd kézzel", 6), ""])
            rossz += 1
            continue
        elt = km - gem
        nagy = abs(elt) >= 2
        rossz += nagy
        megj = (f"a Gemini {abs(elt):.1f} km-rel {'kevesebbet' if elt > 0 else 'többet'} írt" if nagy else "")
        uj, regi = dij(km), dij(gem)
        link = f"https://www.google.com/maps/dir/{start[0]:.5f},{start[1]:.5f}/{h['lat']:.5f},{h['lon']:.5f}"
        sorok.append([nev, (round(km, 1), 4 if nagy else 2), (round(perc), 0), (round(legvonal(start, (h["lat"], h["lon"])), 1), 2),
                      (gem, 2), (round(elt, 1), 4 if nagy else 2), (uj, 5), (regi, 3), (uj - regi, 3),
                      (megj, 6) if nagy else "", link])
        eredmeny.append({"nev": nev, "km": round(km, 1), "perc": round(perc), "dij": uj,
                         "lat": round(h["lat"], 5), "lon": round(h["lon"], 5)})

    sorok.sort(key=lambda r: r[1][0] if isinstance(r[1], tuple) else 999)
    out = os.path.join(MAPPA, "szallitasi_dijak.xlsx")
    xlsx(out, [fejlec] + sorok, [20, 14, 15, 13, 11, 12, 18, 19, 11, 36, 70])
    json.dump({"szabaly": f"{ALAPDIJ} Ft + {KM_DIJ} Ft/km, {KEREKITES} Ft-ra kerekítve", "etterem": start,
               "telepulesek": eredmeny}, open(os.path.join(MAPPA, "szallitasi_dijak.json"), "w", encoding="utf-8"),
              ensure_ascii=False, indent=1)
    print(f"\nKész: {out}")
    print(f"{len(eredmeny)} település kiszámolva; {rossz} helyen tér el legalább 2 km-rel a Gemini adata vagy hiányzik (sárga).")
    print(f"Szabály: {ALAPDIJ} Ft + {KM_DIJ} Ft/km, {KEREKITES} Ft-ra kerekítve.")


if __name__ == "__main__":
    main()
