# Vadvirág – Umbrel app store

This repository is an Umbrel community app store with one app, **Vadvirág Rendelések**. It is the restaurant's order program, the same one that used to run on the main PC.

```
umbrel-app-store.yml          store id "vadvirag"
vadvirag-rendelesek/          the app (Umbrel copies this folder onto the mini PC)
  umbrel-app.yml              name, icon, port 4747
  docker-compose.yml          how Umbrel runs it
  Dockerfile                  minimal image, built on the Umbrel itself and needs no internet to start
  bin/                        the compiled program (x86-64 and ARM)
  docker-compose.manual.yml   only for the manual install (B)
source/                       Go source + build.sh for future versions
```

## A) Install from the Umbrel App Store (recommended)

1. **Put this folder on GitHub.** The repository must be **public**, because Umbrel can only download public stores. Create a new repository called `umbrel-vadvirag`, open it, choose *uploading an existing file*, and drag in **everything from this folder** (the files and the `vadvirag-rendelesek` and `source` folders). Then click *Commit changes*.
   *The repository contains no passwords or keys. All of those stay on the mini PC.*
2. **Add the store in Umbrel:** go to App Store, then the **⋯** menu (top right), then **Community App Stores**. Paste `https://github.com/<your-user>/umbrel-vadvirag`, click **Add**, then **Open**.
3. **Install** "Vadvirág Rendelések". The first install takes about a minute.
4. **First setup.** Do this from a computer in the restaurant, on the same network as the mini PC.
   - Open the app from the Umbrel home screen (or go to `http://umbrel.local:4747`).
   - Set a **new password**.
   - Under *Költözés a régi gépről*, select the old `beallitasok.json` and `rendelesek.json` from the folder of the old program. This brings over the Gmail settings, the webhook key and all previous orders.
5. **Make it reachable from the internet (Tailscale Funnel)**, so Wix can deliver orders and phones get notifications.
   - Go to Umbrel **Settings → Advanced settings → Terminal**, choose the **Tailscale** app, and run:
     ```
     tailscale funnel --bg 4747
     ```
   - The command prints the public address, e.g. `https://umbrel.taileff9a4.ts.net/`.
   - If it prints a link instead, open it, allow Funnel for this device, and run the command again.
6. **Point Wix at the new address.**
   - Open the app at that `https://…ts.net` address and click **⚙ Beállítások**, then **Másolás**. This copies the webhook address.
   - In Wix, go to Automations → "Rendeles a programba" → *Send HTTP request*, replace the URL with the copied one, and save the automation.
   - Send a test order from the website.
7. **Phones and the till PC:**
   - Open the `https://…ts.net` address and log in.
   - Tap the bell to turn notifications on again. They are tied to the address, so this has to be redone on every device.
   - On iPhone: Share → Add to Home Screen.
8. **Old PC:** close the old program. Optionally run `tailscale funnel reset` in a command prompt there.

Leave the app's "Umbrel login" setting **off**. The app has its own password, and Wix must be able to reach it without logging in.

## B) Manual install (without GitHub)

1. Upload the `vadvirag-rendelesek` folder to the mini PC using Umbrel's **Files** app.
2. Open **Settings → Advanced settings → Terminal → umbrelOS** and run:
   ```
   cd ~/umbrel/home/<folder you uploaded to>/vadvirag-rendelesek
   sudo docker compose -f docker-compose.manual.yml up -d --build
   ```
   The app then runs on `http://umbrel.local:4747`, but it won't appear on the Umbrel home screen.
3. Continue from step 4 above.

## Backups and updates

- **⚙ Beállítások → Biztonsági mentés letöltése** saves the settings and all orders in one file. **Visszaállítás fájlból** restores them.
- To release a new version:
  1. Run `source/build.sh`.
  2. Raise `version:` in `vadvirag-rendelesek/umbrel-app.yml`.
  3. Upload the changed files to GitHub.

  Umbrel will then offer the update.

## Online rendelőoldal (v2.1)

A program nyilvános rendelőoldala: `https://<a-funnel-címed>/rendeles`
(pl. `https://umbrel.taileff9a4.ts.net/rendeles`). A vendég kattintással rakja össze a
rendelést, a szerver a saját étlapjából számolja az árat, a rendelés bekerül a programba,
és a vendég élőben látja az állapotát. A weboldalon egy gombbal vagy beágyazott kerettel
(Wix: „Beágyazás → Weboldal beágyazása") lehet rá mutatni.
Az étlap a `source/menu.json` fájlban van; módosítás után `source/build.sh`, verzió növelése.

## Frissítés (v2.2-től)

Az Umbrel frissítéskor csak a `docker-compose.yml`, `umbrel-app.yml` és `hooks/` fájlokat
másolja át. A `hooks/pre-start` szkript ezért minden indítás előtt átmásolja a friss
programfájlokat (`bin/`, `Dockerfile`) az áruház példányából, így elég a Frissítés gomb.

## Vadvirág Weboldal (`vadvirag-weboldal`, 4748-as port)

Az étterem új, háromnyelvű (HU / DE / EN) weboldala. Forrás: `web-source/`
(a `site/` mappa a weboldal, a `main.go` a kis kiszolgáló), fordítás: `web-source/build.sh`.

- Az étlapot élőben a Rendelések appból kéri le (`/api/public/menu`); ha az épp
  nem fut, a beépített másolatot mutatja (`menu-fallback.json`, a build készíti).
- A `/rendeles` oldalt és az `/api/public/*` hívásokat továbbítja a Rendelések
  apphoz, így saját domainen is működik az online rendelés.
- A „Kapcsolat” és „Rendezvények” részen küldött üzenetek a Rendelések app
  boríték ikonja alatt jelennek meg (2.4-es verziótól).
- Az étlap fordításai: `web-source/site/data/menu-i18n.json` (étel azonosító szerint),
  az itallap: `web-source/site/data/drinks.json`, a feliratok: `web-source/site/i18n.js`.
- Saját domain: Cloudflare Tunnel `www.vadviragetterem.com` → `http://<umbrel>:4748`.
