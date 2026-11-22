# Vadvirág Étterem – Umbrel apps

An Umbrel community app store (store id `vadvirag`) with three apps for Vadvirág Étterem
(Zalaudvarnok, vadviragetterem.com). All three are small Go programs. Umbrel installs the
`vadvirag-*` folders, and the source code is in `src/`.

```
vadvirag-weboldal/        the website, vadviragetterem.com (port 4748)       ← built from src/weboldal
vadvirag-rendelesek/      Rendelések: online orders + the kitchen's app (4747) ← built from src/rendelesek
vadvirag-iranyitopult/    Irányítópult: monitoring, statistics, ntfy (4749)  ← built from src/iranyitopult
src/                      Go source, one folder per program
tools/szallitasi_dij.py   delivery-fee calculator (OpenStreetMap distances → fee table), runs on a Mac
check.sh                  builds and tests everything without touching bin/
```

How the three fit together:

- The **website** serves `src/weboldal/site/`. It asks Rendelések for the live menu. If Rendelések
  can't be reached, it shows `menu-fallback.json`, which is built into the program. It also proxies
  `/rendeles` (the order page) to Rendelések, so guests stay on vadviragetterem.com.
- **Rendelések** prices every order from its own `menu.json`, shows orders in the kitchen's app,
  and sends web push and ntfy notifications and e-mails.
- The **Irányítópult** reads both over internal ports (website :9090, Rendelések :9091). These
  ports are never published.

## Common jobs

| Job | Edit | Then |
|---|---|---|
| Weekly menu, weekend offer, events (bál) | `src/weboldal/ajanlatok.json` (tests: `ajanlat_test.go`) | website release |
| Menu items, prices, sizes | `src/rendelesek/menu.json` (translations: `src/weboldal/site/data/menu-i18n.json`) | Rendelések **and** website release (the website's fallback menu is built from it) |
| Drinks list | `src/weboldal/site/data/drinks.json` | website release |
| Delivery fees per village | `defaultFees()` in `src/rendelesek/fees.go` (calculator: `tools/szallitasi_dij.py`) | Rendelések + website release |
| Weekly promotion (gift drink) | `src/rendelesek/akcio.json` | Rendelések release |
| Pre-order offer (e.g. Márton-nap) | `src/rendelesek/elorendeles.json` | Rendelések release |
| Giveaway | `src/rendelesek/nyeremenyjatek.json` | Rendelések release |
| Website texts (HU/DE/EN) | `src/weboldal/site/i18n.js` and `index.html` (`data-t="key"`) | website release |

**Release** = run `sh src/<app>/build.sh`, raise the version in **two** places, add a Hungarian
release note, run `./check.sh`, commit and push. Then press Update on both Umbrels.

| App | Version lives in |
|---|---|
| website | `vadvirag-weboldal/umbrel-app.yml` + `const version` in `src/weboldal/main.go` |
| Rendelések | `vadvirag-rendelesek/umbrel-app.yml` + `const appVersion` in `src/rendelesek/settings.go` |
| Irányítópult | `vadvirag-iranyitopult/umbrel-app.yml` + `const version` in `src/iranyitopult/main.go` |

## Where to look

### Website – `src/weboldal`

| File | What it does |
|---|---|
| `main.go` | Server, live menu from Rendelések, the `/rendeles` proxy, caching, security headers |
| `ajanlat.go` + `ajanlatok.json` | Weekly menu, weekend offer, events. What shows is decided by the Budapest date. |
| `schedule.go` | Automatic changes (seasonal theme, offers) and their manual overrides |
| `stats.go` | Visitor statistics without cookies, and the internal :9090 port for the Irányítópult |
| `cleanup.go` | Cleaning the statistics (bots, own devices) |
| `menu-fallback.json` | **Generated** by `build.sh` from `../rendelesek/menu.json`. Don't edit it. |
| `site/index.html` | The page: hero, offers, menu, events, gallery, delivery, contact |
| `site/app.js` | Everything interactive. Sections are marked `/* ---- name */`: nyelv, fejléc, nyitvatartás, étlap, ajánlatok (giveaway, events, weekly promotion, pre-order, daily offer, new burgers), allergének, „Mit egyek ma?”, kiszállítás, galéria, üzenet/foglalás, évszakok (falling decorations, fireworks, Christmas lights). |
| `site/i18n.js` | All UI texts in three languages |
| `site/style.css` | All styles (fonts first, then the page in order) |
| `site/data/` | Menu translations and the drinks list |

### Rendelések – `src/rendelesek`

| File | What it does |
|---|---|
| `main.go` | `main()`: loads settings and orders, starts the servers and background jobs |
| `config.go` | `config.json`: the settings structure and defaults |
| `store.go` | `orders.json`: the Order type and storage |
| `app.go` | The running app and taking in a new order |
| `public.go` + `web/rendeles.html` | The guest's order page `/rendeles` and its API (menu, cart, live status) |
| `pricing.go`, `packaging.go`, `fees.go` | Order price, take-away boxes, delivery fees and travel times |
| `akcio.go`, `preorder.go`, `giveaway.go`, `raffle.go` | Weekly promotion, pre-orders, giveaway, raffle |
| `messages.go` | Table booking, event inquiries and questions from the website |
| `http.go` + `web/index.html` | The kitchen's app: login, order list, accept/reject, API routes |
| `settings.go` + `web/setup.html` | First setup, settings menu, backup and migration |
| `push.go`, `ntfy.go`, `mail.go` | Web push, ntfy and e-mail notifications |
| `dash.go` | Internal :9091 data for the Irányítópult |
| `cleanup.go`, `total.go` | Test-order filtering, setting an order's exact total |
| `fields.go`, `wix.go` | Old Wix-form orders (field recognition, polling and webhook) |
| `platform.go`, `restart_*.go`, `console_windows.go` | Runs both on Umbrel and as the old Windows program |

### Irányítópult – `src/iranyitopult`

| File | What it does |
|---|---|
| `main.go` | Version, `main()` |
| `config.go` | `settings.json` (ntfy topic, thresholds) |
| `fetch.go` | Reading data from the website and Rendelések |
| `monitor.go`, `checks.go` | Status, alerts and the checks that run every minute |
| `automation.go` | The "Automatikus" view and analysis export |
| `cleanup.go` | Marking test orders and own devices |
| `auth.go` + `authweb/` | Password lock |
| `http.go` + `web/index.html` | The dashboard (UI and its JavaScript are in `index.html`) |

## Rules

- Never commit anything from `data/` (orders, passwords, keys).
- No external scripts, fonts or trackers. Fonts and images are served by the website itself.
- UI work follows the 20 UX & animation principles (snappy, eased, big tap targets, few choices).
