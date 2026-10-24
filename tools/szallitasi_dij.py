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
A böngészőben megnyílik egy egyszerű ablak: ott állíthatod a díjakat, a településlistát,
és onnan töltheted le a táblázatot. (Ablak nélkül: ... szallitasi_dij.py --parancssor)
Csak a Python beépített részeit használja, nem kell semmit telepíteni.
Internet kell hozzá. Az első számolás kb. 2 perc (a térképkereső másodpercenként 1 kérést enged).
"""

import json, math, os, sys, time, urllib.error, urllib.parse, urllib.request, zipfile
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


_SSL = None


def _ssl_ctx():
    """A python.org-os Mac Python nem bízik meg egy weboldalban sem, amíg nem futtatják az
    „Install Certificates” lépést. Ilyenkor a Mac saját (Kulcskarika) tanúsítványait használjuk."""
    import ssl, subprocess, tempfile
    try:
        import certifi  # ha telepítve van
        return ssl.create_default_context(cafile=certifi.where())
    except Exception:
        pass
    pem = ""
    for kc in ("/System/Library/Keychains/SystemRootCertificates.keychain", "/Library/Keychains/System.keychain"):
        try:
            pem += subprocess.run(["security", "find-certificate", "-a", "-p", kc],
                                  capture_output=True, text=True, timeout=20).stdout
        except Exception:
            pass
    if not pem:
        return None
    f = tempfile.NamedTemporaryFile("w", suffix=".pem", delete=False)
    f.write(pem)
    f.close()
    return ssl.create_default_context(cafile=f.name)


def get(url):
    global _SSL
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=40, context=_SSL) as r:
            return json.load(r)
    except urllib.error.URLError as e:
        if _SSL is None and "CERTIFICATE_VERIFY_FAILED" in str(e):
            _SSL = _ssl_ctx()
            if _SSL is not None:
                with urllib.request.urlopen(req, timeout=40, context=_SSL) as r:
                    return json.load(r)
        raise


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


# ---------------------------------------------------------------- számolás

def dij_k(km, alap, kmdij, ker):
    ker = int(ker) or 100
    return int(math.floor((alap + kmdij * km) / ker + 0.5)) * ker


def szamol(telepulesek, etterem, log, allj=lambda: False):
    """Megkeresi a településeket, lekéri az autós távokat. Visszaad: (start, [{nev, gemini, km, perc, legvonal, lat, lon}])."""
    cache = {}
    try:
        with open(CACHE, encoding="utf-8") as f:
            cache = json.load(f)
    except Exception:
        pass
    helyek, hibak = {}, []
    nevek = [n for n, _ in telepulesek]
    if not etterem and "Zalaudvarnok" not in nevek:
        nevek = ["Zalaudvarnok"] + nevek
    for i, nev in enumerate(nevek, 1):
        if allj():
            raise RuntimeError("leállítva")
        try:
            helyek[nev] = geocode(nev, cache)
        except Exception as e:
            hibak.append(f"{nev}: {e}")
            log(f"! {nev}: hiba a keresésnél ({e})", i, len(nevek))
            helyek[nev] = None
            if len(hibak) >= 3 and not cache:
                raise RuntimeError("Nem érem el a térképet. Az első hibák: " + " | ".join(hibak[:3]))
            continue
        h = helyek[nev]
        log(f"{nev}: " + (f"{h['lat']:.4f}, {h['lon']:.4f}" if h else "NEM TALÁLHATÓ a térképen"), i, len(nevek))
        with open(CACHE, "w", encoding="utf-8") as f:
            json.dump(cache, f, ensure_ascii=False, indent=1)
    z = helyek.get("Zalaudvarnok")
    start = tuple(etterem) if etterem else (z and (z["lat"], z["lon"]))
    if not start:
        raise RuntimeError("Az étterem helyét nem találtam – add meg a koordinátáit.")
    talalt = [(n, (helyek[n]["lat"], helyek[n]["lon"])) for n, _ in telepulesek if helyek.get(n)]
    log("Autós távolságok lekérése…", len(nevek), len(nevek))
    utak = dict(zip([n for n, _ in talalt], utvonalak(start, [p for _, p in talalt]))) if talalt else {}
    out = []
    for nev, gem in telepulesek:
        h = helyek.get(nev)
        km, perc = utak.get(nev, (None, None))
        out.append({"nev": nev, "gemini": gem, "km": None if km is None else round(km, 1),
                    "perc": None if perc is None else round(perc),
                    "legvonal": round(legvonal(start, (h["lat"], h["lon"])), 1) if h else None,
                    "lat": h and round(h["lat"], 5), "lon": h and round(h["lon"], 5)})
    return start, out


def tablazat(path, start, sorok_in, alap, kmdij, ker):
    fejlec = ["Település", "Autós táv (km)", "Menetidő (perc)", "Légvonal (km)", "Gemini km", "Eltérés (km)",
              "Új díj (valódi km)", "Díj a Gemini km-rel", "Különbség", "Megjegyzés", "Útvonal a térképen"]
    sorok = []
    for r in sorok_in:
        gem, km = r["gemini"], r["km"]
        if km is None:
            sorok.append([r["nev"], "", "", "", (gem, 2) if gem is not None else "", "", "",
                          (dij_k(gem, alap, kmdij, ker), 3) if gem is not None else "", "",
                          ("nem találtam a térképen – ellenőrizd kézzel", 6), ""])
            continue
        elt = None if gem is None else km - gem
        nagy = elt is not None and abs(elt) >= 2
        megj = f"a Gemini {abs(elt):.1f} km-rel {'kevesebbet' if elt > 0 else 'többet'} írt" if nagy else ""
        uj = dij_k(km, alap, kmdij, ker)
        regi = None if gem is None else dij_k(gem, alap, kmdij, ker)
        link = f"https://www.google.com/maps/dir/{start[0]:.5f},{start[1]:.5f}/{r['lat']:.5f},{r['lon']:.5f}"
        sorok.append([r["nev"], (km, 4 if nagy else 2), (r["perc"], 0), (r["legvonal"], 2),
                      (gem, 2) if gem is not None else "", (round(elt, 1), 4 if nagy else 2) if elt is not None else "",
                      (uj, 5), (regi, 3) if regi is not None else "", (uj - regi, 3) if regi is not None else "",
                      (megj, 6) if nagy else "", link])
    sorok.sort(key=lambda r: r[1][0] if isinstance(r[1], tuple) else 999)
    xlsx(path, [fejlec] + sorok, [20, 14, 15, 13, 11, 12, 18, 19, 11, 36, 70])


# ---------------------------------------------------------------- ablak (a böngészőben nyílik meg)

OLDAL = r"""<!doctype html><html lang="hu"><meta charset="utf-8"><title>Szállítási díj kalkulátor</title>
<style>
body{font:15px -apple-system,system-ui,sans-serif;margin:20px;max-width:1150px;color:#222}
h1{font-size:22px;margin:0 0 14px}
.row{display:flex;gap:18px;flex-wrap:wrap;align-items:flex-end;margin-bottom:12px}
label{display:flex;flex-direction:column;font-size:13px;color:#555;gap:4px}
input{font:inherit;padding:6px 8px;border:1px solid #bbb;border-radius:6px;width:110px}
#coords{width:220px}
textarea{width:100%;height:150px;font:13px ui-monospace,Menlo,monospace;border:1px solid #bbb;border-radius:6px;padding:8px;box-sizing:border-box}
button{font:inherit;padding:8px 16px;border:0;border-radius:8px;background:#c4140a;color:#fff;cursor:pointer}
button.sec{background:#e9e9ee;color:#222}
button:disabled{opacity:.5;cursor:default}
#bar{height:8px;background:#eee;border-radius:4px;overflow:hidden;margin:10px 0}
#bar div{height:100%;width:0;background:#c4140a;transition:width .3s}
#log{font:12px ui-monospace,Menlo,monospace;color:#666;height:1.4em;overflow:hidden;white-space:nowrap}
table{border-collapse:collapse;width:100%;margin-top:14px;font-size:14px}
th,td{border-bottom:1px solid #e3e3e3;padding:5px 8px;text-align:right;white-space:nowrap}
th{position:sticky;top:0;background:#f7e3df;cursor:pointer}
td:first-child,th:first-child{text-align:left}
td.m{text-align:left;white-space:normal;color:#8a5a00}
tr.warn td{background:#fff2cc}
.sum{margin-top:10px;color:#444}
small{color:#777}
</style>
<h1>Vadvirág – szállítási díj kalkulátor</h1>
<div class="row">
 <label>Alapdíj (Ft)<input id="alap" type="number" value="__ALAP__"></label>
 <label>Kilométerdíj (Ft/km)<input id="kmdij" type="number" value="__KMDIJ__"></label>
 <label>Kerekítés (Ft)<input id="ker" type="number" value="__KER__"></label>
 <label>Étterem helye (szélesség, hosszúság)<input id="coords" placeholder="üres = Zalaudvarnok közepe" value="__COORDS__"></label>
</div>
<label>Települések – soronként: <b>név, régi km</b> (a régi km elhagyható)
<textarea id="list">__LIST__</textarea></label>
<div class="row" style="margin-top:10px">
 <button id="go">Kiszámolás</button>
 <button id="dl" class="sec" disabled>Táblázat letöltése (.xlsx)</button>
 <small>A díjak módosításához nem kell újra számolni – a táblázat azonnal frissül.</small>
</div>
<div id="bar"><div></div></div><div id="log"></div>
<div class="sum" id="sum"></div>
<table id="t"></table>
<script>
const $=id=>document.getElementById(id);
let data=null,sortKey='km',sortDir=1,timer=0;
const fee=km=>{const a=+$('alap').value,k=+$('kmdij').value,r=+$('ker').value||100;return Math.floor((a+k*km)/r+0.5)*r};
const ft=n=>n==null?'':n.toLocaleString('hu-HU')+' Ft';
const n1=n=>n==null?'':n.toFixed(1).replace('.',',');
function rows(){return (data?data.rows:[]).map(r=>{const elt=r.km!=null&&r.gemini!=null?r.km-r.gemini:null;
  return {...r,elt,uj:r.km!=null?fee(r.km):null,regi:r.gemini!=null?fee(r.gemini):null,warn:r.km==null||(elt!=null&&Math.abs(elt)>=2)}})}
function render(){if(!data)return;const rs=rows();
  rs.sort((a,b)=>{const x=a[sortKey],y=b[sortKey];if(x==null)return 1;if(y==null)return -1;return (typeof x==='string'?x.localeCompare(y,'hu'):x-y)*sortDir});
  const cols=[['nev','Település'],['km','Autós km'],['perc','Perc'],['legvonal','Légvonal km'],['gemini','Régi km'],['elt','Eltérés'],['uj','Új díj'],['regi','Díj a régi km-rel'],['m','Megjegyzés'],['map','']];
  $('t').innerHTML='<tr>'+cols.map(c=>`<th data-k="${c[0]}">${c[1]}${sortKey===c[0]?(sortDir>0?' ▲':' ▼'):''}</th>`).join('')+'</tr>'+
   rs.map(r=>`<tr class="${r.warn?'warn':''}"><td>${r.nev}</td><td>${n1(r.km)}</td><td>${r.perc??''}</td><td>${n1(r.legvonal)}</td><td>${n1(r.gemini)}</td><td>${r.elt==null?'':(r.elt>0?'+':'')+n1(r.elt)}</td><td><b>${ft(r.uj)}</b></td><td>${ft(r.regi)}</td><td class="m">${r.km==null?'nem találtam a térképen':r.warn?'nagy eltérés':''}</td><td>${r.lat?`<a target="_blank" href="https://www.google.com/maps/dir/${data.start[0]},${data.start[1]}/${r.lat},${r.lon}">térkép</a>`:''}</td></tr>`).join('');
  const ok=rs.filter(r=>r.uj!=null),w=rs.filter(r=>r.warn).length;
  $('sum').textContent=`${ok.length} település · díjak ${ft(Math.min(...ok.map(r=>r.uj)))} – ${ft(Math.max(...ok.map(r=>r.uj)))} · ${w} sárga sor (2 km-nél nagyobb eltérés vagy nem található)`;
  document.querySelectorAll('th').forEach(th=>th.onclick=()=>{const k=th.dataset.k;if(k==='map'||k==='m')return;sortDir=sortKey===k?-sortDir:1;sortKey=k;render()})}
['alap','kmdij','ker'].forEach(id=>$(id).oninput=render);
async function poll(){const s=await (await fetch('/status')).json();
  $('bar').firstElementChild.style.width=(s.total?100*s.done/s.total:0)+'%';$('log').textContent=s.msg||'';
  if(s.running){timer=setTimeout(poll,700);return}
  $('go').disabled=false;
  if(s.error){$('log').textContent='Hiba: '+s.error;return}
  if(s.result){data=s.result;$('dl').disabled=false;render()}}
$('go').onclick=async()=>{$('go').disabled=true;$('dl').disabled=true;
  const r=await fetch('/start',{method:'POST',body:JSON.stringify({list:$('list').value,coords:$('coords').value})});
  const j=await r.json();if(j.error){$('log').textContent='Hiba: '+j.error;$('go').disabled=false;return}poll()};
$('dl').onclick=()=>{location.href=`/xlsx?alap=${$('alap').value}&kmdij=${$('kmdij').value}&ker=${$('ker').value}`};
poll();
</script></html>"""


def ablak(port=8765):
    import http.server, threading, webbrowser

    allapot = {"running": False, "done": 0, "total": 0, "msg": "", "error": None, "result": None}
    zar = threading.Lock()

    def feldolgoz(lista, coords):
        def log(msg, i, n):
            with zar:
                allapot.update(msg=msg, done=i, total=n)
        try:
            start, sorok = szamol(lista, coords, log)
            with zar:
                allapot.update(result={"start": start, "rows": sorok}, msg="Kész.", running=False)
        except Exception as e:
            with zar:
                allapot.update(error=str(e), running=False)

    def olvas_lista(szoveg):
        out = []
        for sor in szoveg.splitlines():
            sor = sor.strip()
            if not sor or sor.startswith("#"):
                continue
            nev, _, km = sor.partition(",")
            km = km.strip().replace(",", ".")
            try:
                km = float(km) if km else None
            except ValueError:
                km = None
            out.append((nev.strip(), km))
        return out

    class H(http.server.BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def kuld(self, kod, tipus, adat, extra=None):
            self.send_response(kod)
            self.send_header("Content-Type", tipus)
            for k, v in (extra or {}).items():
                self.send_header(k, v)
            self.end_headers()
            self.wfile.write(adat)

        def do_GET(self):
            u = urllib.parse.urlparse(self.path)
            if u.path == "/":
                lista = "\n".join(f"{n}, {k:g}" for n, k in TELEPULESEK)
                coords = f"{ETTEREM[0]}, {ETTEREM[1]}" if ETTEREM else ""
                html = (OLDAL.replace("__ALAP__", str(ALAPDIJ)).replace("__KMDIJ__", str(KM_DIJ))
                        .replace("__KER__", str(KEREKITES)).replace("__COORDS__", coords).replace("__LIST__", escape(lista)))
                self.kuld(200, "text/html; charset=utf-8", html.encode())
            elif u.path == "/status":
                with zar:
                    self.kuld(200, "application/json", json.dumps(allapot, ensure_ascii=False).encode())
            elif u.path == "/xlsx":
                q = urllib.parse.parse_qs(u.query)
                with zar:
                    res = allapot["result"]
                if not res:
                    self.kuld(404, "text/plain; charset=utf-8", "Előbb számolj!".encode())
                    return
                num = lambda k, d: int(round(float(q.get(k, [d])[0] or d)))
                path = os.path.join(MAPPA, "szallitasi_dijak.xlsx")
                tablazat(path, res["start"], res["rows"], num("alap", ALAPDIJ), num("kmdij", KM_DIJ), num("ker", KEREKITES) or 100)
                with open(path, "rb") as f:
                    self.kuld(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", f.read(),
                              {"Content-Disposition": 'attachment; filename="szallitasi_dijak.xlsx"'})
            else:
                self.kuld(404, "text/plain", b"")

        def do_POST(self):
            if self.path != "/start":
                return self.kuld(404, "text/plain", b"")
            body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
            lista = olvas_lista(body.get("list", ""))
            coords = None
            c = (body.get("coords") or "").replace(";", ",").split(",")
            if len(c) == 2 and all(x.strip() for x in c):
                try:
                    coords = (float(c[0]), float(c[1]))
                except ValueError:
                    return self.kuld(200, "application/json", json.dumps({"error": "Az étterem koordinátái hibásak (pl. 46.91289, 17.10685)"}).encode())
            if not lista:
                return self.kuld(200, "application/json", json.dumps({"error": "Üres a településlista"}).encode())
            with zar:
                if allapot["running"]:
                    return self.kuld(200, "application/json", b'{"ok":true}')
                allapot.update(running=True, done=0, total=len(lista), msg="Indul…", error=None, result=None)
            threading.Thread(target=feldolgoz, args=(lista, coords), daemon=True).start()
            self.kuld(200, "application/json", b'{"ok":true}')

    srv = None
    for p in range(port, port + 20):
        try:
            srv = http.server.ThreadingHTTPServer(("127.0.0.1", p), H)
            break
        except OSError:
            continue
    if not srv:
        sys.exit("Nem találtam szabad portot.")
    url = f"http://127.0.0.1:{srv.server_address[1]}/"
    print(f"A kalkulátor megnyílt a böngészőben: {url}\nBezáráshoz: Ctrl+C ebben az ablakban.")
    threading.Timer(0.5, lambda: webbrowser.open(url)).start()
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        print("\nBezárva.")


def parancssor():
    def log(msg, i, n):
        print(f"  {i:2}/{n}  {msg}")
    start, sorok = szamol(TELEPULESEK, ETTEREM, log)
    path = os.path.join(MAPPA, "szallitasi_dijak.xlsx")
    tablazat(path, start, sorok, ALAPDIJ, KM_DIJ, KEREKITES)
    print(f"\nKész: {path}")


if __name__ == "__main__":
    if "--parancssor" in sys.argv:
        parancssor()
    else:
        ablak()
