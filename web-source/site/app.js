/* Vadvirág Étterem – weboldal */
(() => {
'use strict';
document.documentElement.classList.add('js');
const $ = id => document.getElementById(id);
// Az esemény célpontja elemként (szöveg csomópontnál a szülője; ablak/dokumentum esetén null)
const tgt = e => { let t = e && e.target; if (t && t.nodeType === 3) t = t.parentNode; return t && t.nodeType === 1 ? t : null; };
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const huf = n => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
const norm = s => String(s || '').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
const store = { get(k){ try { return localStorage.getItem(k) } catch(e){ return null } }, set(k,v){ try { localStorage.setItem(k,v) } catch(e){} } };
const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
const HOURS = { open: 11, close: 22 };                 // minden nap 11:00–22:00
const LANGS = ['hu', 'en', 'de'];

let lang = 'hu', M = null, TR = { sections:{}, items:{} }, DR = [], tab = 'food', kind = 'asztal';
/* látogatottság: sütik nélkül, csak névtelen jelzések a saját szerverünknek */
const track = (t, o = {}) => { try {
  const body = JSON.stringify(Object.assign({ t, p: location.pathname, l: lang, s: window.vvSec || '', d: window.vvScroll || 0 }, o));
  if (navigator.sendBeacon) navigator.sendBeacon('/api/t', new Blob([body], { type: 'application/json' }));
  else fetch('/api/t', { method: 'POST', body, keepalive: true, headers: { 'Content-Type': 'application/json' } });
} catch (e) {} };
let errSent = 0;
// csak a saját fájljaink hibáit jelentjük: a Facebook/Instagram és más alkalmazások beépített böngészője saját kódot fűz az oldalba, annak a hibái nem a mieink
const ownErr = e => { try { const u = new URL(e.filename || '', location.href); return u.origin === location.origin && /\/(app|i18n)\.js$/.test(u.pathname); } catch (x) { return false; } };
addEventListener('error', e => { if (ownErr(e) && errSent++ < 5) track('err', { msg: String(e.message || 'hiba'), src: (e.filename || '') + ':' + (e.lineno || 0) }); });
addEventListener('unhandledrejection', e => { const st = String(e.reason && e.reason.stack || ''); if (/\/(app|i18n)\.js/.test(st) && errSent++ < 5) track('err', { msg: 'Promise: ' + String(e.reason && e.reason.message || e.reason || '') }); });
const T = (k, vars) => { let s = (I18N[lang] && I18N[lang][k]) ?? I18N.hu[k] ?? k; if (vars) for (const v in vars) s = s.replaceAll('{' + v + '}', vars[v]); return s; };

/* ---------------------------------------------------------------- nyelv */
function pickLang() {
  const p = location.pathname.replace(/\/+$/, '').slice(1).toLowerCase();
  if (LANGS.includes(p)) return p;
  const q = new URLSearchParams(location.search).get('lang');
  if (LANGS.includes(q)) return q;
  return 'hu'; // alapból mindig magyar; angol/német csak a /en, /de címen vagy a nyelvváltóval
}
function setLang(l, push) {
  lang = LANGS.includes(l) ? l : 'hu';
  document.documentElement.lang = lang;
  document.title = T('title');
  document.querySelector('meta[name=description]').setAttribute('content', T('desc'));
  $$('[data-t]').forEach(e => { e.textContent = T(e.dataset.t); });
  $$('[data-tp]').forEach(e => { e.placeholder = T(e.dataset.tp); });
  $$('[data-ta]').forEach(e => { if (e.tagName === 'IMG') e.alt = T(e.dataset.ta); else e.setAttribute('aria-label', T(e.dataset.ta)); });
  $$('[data-lang]').forEach(b => b.setAttribute('aria-pressed', String(b.dataset.lang === lang)));
  if (push) { const path = lang === 'hu' ? '/' : '/' + lang; history.replaceState(null, '', path + location.hash); }
  renderStatus(); renderHours(); renderFacts(); setKind(kind);
  if (M) { renderChips(); renderFood(); renderFees(); checkTown(); }
  renderDrinks(); renderAlg(); renderOffers(); renderPre(); renderGw(); renderSeason();
}
$$('[data-lang]').forEach(b => b.addEventListener('click', () => { setLang(b.dataset.lang, true); closeDrawer(); hideLangHint(); track('ev', { n: 'lang', v: b.dataset.lang }); }));

// Ha a böngésző nyelve német vagy angol, de az oldal magyarul nyílt meg, egyszer felajánljuk a nyelvét
// (az oldal alapból magyar marad; a döntést a munkamenet végéig megjegyezzük, sütit nem használunk).
const LH = {
  de: { t: 'Diese Seite gibt es auch auf Deutsch.', go: 'Auf Deutsch anzeigen', x: 'Schließen' },
  en: { t: 'This page is also available in English.', go: 'Switch to English', x: 'Close' },
};
function hideLangHint() { const h = document.getElementById('langHint'); if (h) h.remove(); try { sessionStorage.setItem('vvLangHint', '1'); } catch (e) {} }
function offerLang() {
  if (lang !== 'hu') return;
  try { if (sessionStorage.getItem('vvLangHint')) return; } catch (e) {}
  const prefs = (navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || '']).map(x => String(x).slice(0, 2).toLowerCase());
  if (prefs.includes('hu')) return; // magyar böngészőnek nem kell
  const want = prefs.find(x => x === 'de' || x === 'en');
  if (!want) return;
  const c = LH[want], h = document.createElement('div');
  h.className = 'lang-hint'; h.id = 'langHint'; h.setAttribute('role', 'dialog'); h.setAttribute('aria-live', 'polite'); h.lang = want;
  h.innerHTML = `<p>${c.t}</p><button class="lh-go" type="button">${c.go}</button><button class="lh-x" type="button" aria-label="${c.x}">×</button>`;
  h.querySelector('.lh-go').onclick = () => { setLang(want, true); hideLangHint(); track('ev', { n: 'lang', v: want }); };
  h.querySelector('.lh-x').onclick = hideLangHint;
  document.body.appendChild(h);
}

/* ---------------------------------------------------------------- fejléc, menü */
const top = $('top');
let lastY = scrollY, typing = false;
const onScroll = () => {
  const y = scrollY, down = y > lastY + 4, up = y < lastY - 4;
  top.classList.toggle('scrolled', y > 40);
  const dock = $('dock');
  if (y < innerHeight * 0.7 || typing) dock.classList.remove('show');
  else if (up) dock.classList.add('show');
  else if (down) dock.classList.remove('show');
  if (down || up) lastY = y;
};
document.addEventListener('focusin', e => { if (tgt(e)?.matches('input,textarea,select')) { typing = true; $('dock').classList.remove('show'); } });
document.addEventListener('focusout', () => { typing = false; });
addEventListener('scroll', onScroll, { passive: true }); onScroll();

const burger = $('burger'), drawer = $('drawer');
function closeDrawer() { drawer.hidden = true; top.classList.remove('menu-open'); burger.setAttribute('aria-expanded', 'false'); document.body.style.overflow = ''; }
burger.addEventListener('click', () => {
  const open = drawer.hidden; drawer.hidden = !open; top.classList.toggle('menu-open', open); burger.setAttribute('aria-expanded', String(open));
  document.body.style.overflow = open ? 'hidden' : '';
});
$$('#drawer a').forEach(a => a.addEventListener('click', closeDrawer));
addEventListener('keydown', e => { if (e.key === 'Escape' && !drawer.hidden) closeDrawer(); });

// melyik szakasznál járunk – kiemelés a menüben
const navLinks = $$('.nav a');
const spy = new IntersectionObserver(es => es.forEach(en => {
  if (en.isIntersecting) navLinks.forEach(a => a.classList.toggle('on', a.getAttribute('href') === '#' + en.target.id));
}), { rootMargin: '-45% 0px -50% 0px' });
['ajanlat', 'etlap', 'rendezvenyek', 'kiszallitas', 'galeria', 'kapcsolat'].forEach(id => spy.observe($(id)));

/* ---------------------------------------------------------------- nyitvatartás */
function budapestNow() {
  const p = Object.fromEntries(new Intl.DateTimeFormat('en-GB', { timeZone: 'Europe/Budapest', weekday: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date()).map(x => [x.type, x.value]));
  const wd = ['Mon','Tue','Wed','Thu','Fri','Sat','Sun'].indexOf(p.weekday);
  return { wd, min: (+p.hour) * 60 + (+p.minute) };
}
const hh = h => String(h).padStart(2, '0') + ':00';
function renderStatus() {
  const el = $('status'), tx = $('statusText'); if (!el) return;
  const { min } = budapestNow(), o = HOURS.open * 60, c = HOURS.close * 60;
  el.classList.remove('open', 'closed');
  if (M && M.closed_today) { el.classList.add('closed'); tx.textContent = T('closedEarly', { t: hh(HOURS.open) }); }
  else if (min >= o && min < c) { el.classList.add('open'); tx.textContent = c - min <= 45 ? T('closingSoon', { t: hh(HOURS.close) }) : T('openNow', { t: hh(HOURS.close) }); }
  else { el.classList.add('closed'); tx.textContent = min < o ? T('closedNow', { t: hh(HOURS.open) }) : T('closedTomorrow', { t: hh(HOURS.open) }); }
  if (M && M.open === false && el.classList.contains('open')) tx.textContent += ' · ' + T('orderPaused');
  if ($('hoursToday')) $('hoursToday').textContent = tx.textContent;
}
function renderHours() {
  const { wd } = budapestNow();
  $('hoursList').innerHTML = T('days').map((d, i) => `<li class="${i === wd ? 'today' : ''}"><span data-today="${esc(T('today'))}">${esc(d)}</span><span>${hh(HOURS.open)}–${hh(HOURS.close)}</span></li>`).join('');
}
setInterval(renderStatus, 60000);
// a nyitva/zárva állapotot (szünetel, korábban zártunk) 3 percenként frissítjük, ha az oldal nyitva marad
setInterval(async () => { if (document.hidden || !M) return; try { const m = await (await fetch('/api/menu', { cache: 'no-store' })).json(); if (m && m.sections) { const ch = m.closed_today !== M.closed_today || m.open !== M.open; M.open = m.open; M.closed_today = m.closed_today; if (ch) { renderStatus(); renderWeekly(); renderFood(); } } } catch (e) {} }, 180000);

/* ---------------------------------------------------------------- étlap */
const trSec = s => (lang !== 'hu' && TR.sections[s.id] && TR.sections[s.id][lang]) || s.name;
const trSecShort = s => (lang !== 'hu' && TR.sections[s.id] && TR.sections[s.id][lang]) || s.short || s.name;
const trVar = l => (lang !== 'hu' && TR.variants && TR.variants[l] && TR.variants[l][lang]) || l;
const trItem = it => { const t = lang !== 'hu' && TR.items[it.id] && TR.items[it.id][lang]; return { name: (t && t[0]) || it.name, desc: (t && t[0]) ? (t[1] || '') : (it.desc || '') }; };
const variants = it => it.variants && it.variants.length ? it.variants : [{ label: '', price: it.price }];
const orderHref = (id, v) => '/rendeles?add=' + encodeURIComponent(id) + (v ? '&v=' + encodeURIComponent(v) : '');
const plus = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>';

/* ---------------------------------------------------------------- ajánlatok
   A szerver (/api/ajanlat) csak azt küldi, ami ma érvényes: a hétvégi ajánlat hétfőtől
   magától eltűnik, a heti menü a saját hetén (és az előtte lévő hétvégén) látszik. */
let offersSettled = false, OFF = null, wkDay = null, wkCourses = 2, wkSoup = 0, wkMain = 0, wkSwap = 0;
const L10 = o => (o && (o[lang] || o.hu)) || '';
const LOC = () => (lang === 'hu' ? 'hu-HU' : lang === 'de' ? 'de-DE' : 'en-GB');
const isoDay = iso => new Date(iso + 'T12:00:00');
const cap = s => s.charAt(0).toUpperCase() + s.slice(1);
const fmtDay = (iso, opts, loc) => cap(isoDay(iso).toLocaleDateString(loc || LOC(), opts));
const quickHref = text => '/rendeles?leiras=' + encodeURIComponent(text);
async function loadOffers() {
  try { const r = await fetch('/api/ajanlat', { cache: 'no-store' }); if (!r.ok) throw 0; OFF = await r.json(); OFF._t = Date.now(); } catch (e) { OFF = {}; }
  renderOffers();
}
function renderOffers() {
  if (!OFF) return;
  const w = OFF.weekend, el = $('offWeekend');
  if (w) {
    const tag = w.days_left <= 0 ? T('wkendToday') : w.days_left === 1 ? T('wkendTomorrow') : T('wkendSunday');
    const dz = w.dessert;
    const base = 'Hétvégi ajánlat: ' + w.courses.map(c => c.name.hu).join(' + ') + ' (' + huf(w.price) + ' Ft)';
    const course = (k, c, extra) => `<div class="course${k === 'cDessert' ? ' dessert' : ''}"><span class="k">${esc(T(k))}</span><span class="n">${esc(L10(c.name))}</span>${L10(c.desc) ? `<span class="d">${esc(L10(c.desc))}</span>` : ''}${extra || ''}</div>`;
    el.innerHTML = `<span class="off-tag hot">${esc(tag)}</span><h3>${esc(L10(w.title))}</h3>` +
      w.courses.map(c => course(c.kind === 'soup' ? 'cSoup' : 'cMain', c)).join('') +
      `<div class="off-price"><span>${esc(T('wkendBoth'))}</span><b>${huf(w.price)} Ft</b></div>` +
      (dz ? course('cDessert', dz, `<span class="p">${huf(dz.price)} Ft</span>`) : '') +
      `<div class="off-btns"><a class="btn btn-red btn-lg" href="${esc(quickHref(base))}">${esc(T('wkendOrder', { p: huf(w.price) }))}</a>` +
      (dz ? `<a class="btn btn-ghost btn-lg" href="${esc(quickHref(base + '\n+ desszert: ' + dz.name.hu + ' (' + huf(dz.price) + ' Ft)'))}">${esc(T('wkendOrderDessert', { p: huf(w.price + dz.price) }))}</a>` : '') + '</div>';
  }
  el.hidden = !w;
  renderWeekly(); syncOffers();
}

// ---- Nyereményjáték a weboldalas rendelők között (a rendeléskezelő nyeremenyjatek.json-ja alapján).
// A szabályt a rendeléskezelő ellenőrzi; itt csak bemutatjuk. A játék után a sorsolás napjáig a „lezárult” szöveg látszik.
let GW = null;
async function loadGw() {
  try { const r = await fetch('/api/public/giveaway', { cache: 'no-store' }); if (!r.ok) throw 0; const d = await r.json(); GW = d.active ? d : null; } catch (e) { GW = null; }
  renderGw();
}
function renderGw() {
  const el = $('nyeremeny'), hc = $('heroGw');
  el.hidden = hc.hidden = !GW;
  if (!GW) return syncOffers();
  const end = isoDay(GW.until.slice(0, 10)), draw = isoDay(GW.draw.slice(0, 10));
  const dayFmt = d => lang === 'hu' ? d.toLocaleDateString('hu-HU', { weekday: 'long' }) + ' (' + d.toLocaleDateString('hu-HU', { month: 'long', day: 'numeric' }) + ')' : d.toLocaleDateString(LOC(), { weekday: 'long', day: 'numeric', month: 'long' });
  const p = { n: GW.winners, min: huf(GW.min), date: dayFmt(end), time: GW.until.slice(11, 16) || '23:59', draw: dayFmt(draw), dtime: GW.draw.slice(11, 16) || '21:00' };
  el.innerHTML = `<div class="gw-pic"><img src="/img/pizzakoszoru.webp" alt="${esc(T('gwAlt'))}" width="564" height="564" loading="lazy"></div>
    <div class="gw-body"><span class="off-tag">${esc(T('gwTag'))}</span><h3>${esc(T('gwTitle', p))}</h3>
    <p>${esc(T(GW.running ? 'gwText' : 'gwEnded', p))}</p>
    ${GW.running ? `<div class="gw-act"><a class="btn btn-red btn-lg" href="/rendeles">${esc(T('gwBtn'))}</a><small>${esc(T('gwNote', p))}</small></div>` : ''}</div>`;
  hc.hidden = !GW.running;
  hc.innerHTML = `<span class="gi"><img src="/img/pizzakoszoru-xs.webp" alt="" width="44" height="44"></span><span><b>${esc(T('gwTitle', p))}</b><small>${esc(T('gwHero', p))}</small></span><span class="go">›</span>`;
  syncOffers();
}

// ---- Előrendelhető alkalmi ajánlat (most: Márton-nap). Az ajánlat és az árak a rendeléskezelőből
// jönnek (/api/public/preorder); ha ott „Elfogyott”-ra kapcsolják, innen is eltűnik. A vendég választ
// ételeket, majd helyben (asztalfoglalás), elvitelre vagy kiszállítással kéri egy későbbi napra.
let PRE = null, preQty = {}, preMode = 'helyben', prePay = 'Készpénz', preBuilt = false;
async function loadPre() {
  try { const r = await fetch('/api/public/preorder', { cache: 'no-store' }); if (!r.ok) throw 0; const d = await r.json(); PRE = d.open ? d : null; } catch (e) { PRE = null; }
  renderPre();
}
// „november 10-től” / „10 November” / „10. November”
function fromDate(iso) {
  const d = isoDay(iso), n = d.getDate();
  if (lang === 'hu') { const u = n % 10, tol = u ? [3, 6, 8].includes(u) : n !== 10; return d.toLocaleDateString('hu-HU', { month: 'long' }) + ' ' + n + '-' + (tol ? 'tól' : 'től'); }
  return d.toLocaleDateString(LOC(), { day: 'numeric', month: 'long' });
}
function preItems() { const o = PRE.offer; return [...o.mains.map((d, i) => ({ k: 'm' + i, d, p: o.main_price })), ...(o.platters || []).map((d, i) => ({ k: 't' + i, d, p: o.platter_price })), ...o.desserts.map((d, i) => ({ k: 'd' + i, d, p: o.dessert_price }))]; }
function preTotals() {
  let n = 0, mains = 0, food = 0;
  let pack = 0;
  for (const it of preItems()) { const q = preQty[it.k] || 0; n += q; food += q * it.p; if (it.k[0] === 'm') mains += q; if (it.k[0] === 't') mains += 2 * q; pack += q * (it.k[0] === 't' ? (PRE.pack_platter || PRE.pack) : PRE.pack); }
  if (preMode === 'helyben') pack = 0;
  let fee = 0; if (preMode === 'kiszallitas') { const f = PRE.fees.find(x => x.name === $('preCity')?.value); fee = f ? f.fee : null; }
  return { n, mains, food, pack, fee, total: food + pack + (fee || 0) };
}
// egyszerű, saját rajzolású liba (díszítés)
const GOOSE = '<svg viewBox="0 0 200 160" aria-hidden="true"><path d="M150 28c-11-1-19 7-19 17 0 9 6 15 6 25 0 9-8 15-19 15H78c-30 0-55 18-61 44 14 14 37 22 64 22h40c34 0 58-21 58-49 0-15-6-26-15-35-5-5-7-10-7-15 0-5 4-8 9-8h17c-4-10-15-16-27-16z"/><path d="M168 46h20M60 112c18 10 40 13 64 9M88 132c14 3 30 3 44-1"/><circle cx="152" cy="40" r="1.5"/></svg>';
function renderPre() {
  const el = $('offPre'), hp = $('heroPre');
  el.hidden = !PRE; hp.hidden = !PRE; syncOffers();
  if (!PRE) return;
  hp.innerHTML = `<span class="gi">${GOOSE}</span><span><b>${esc(L10(PRE.offer.title))}</b><small>${esc(T('preServed', { date: fromDate(PRE.offer.serve_from) }))} · ${esc(T('preHeroSub'))}</small></span><span class="go">›</span>`;
  const o = PRE.offer, t = preTotals();
  const dish = it => { const q = preQty[it.k] || 0; return `<div class="mt-dish${q ? ' on' : ''}"><div class="tx"><div class="n">${esc(L10(it.d.name))}</div>${L10(it.d.desc) ? `<div class="d">${esc(L10(it.d.desc))}</div>` : ''}</div>
    <div class="mt-step"><button type="button" data-pq="${it.k}" data-d="-1" aria-label="${esc(T('preMinus') + ': ' + L10(it.d.name))}"${q ? '' : ' disabled'}>−</button><b aria-live="polite">${q}</b><button type="button" class="plus" data-pq="${it.k}" data-d="1" aria-label="${esc(T('prePlus') + ': ' + L10(it.d.name))}">+</button></div></div>`; };
  const items = preItems();
  el.innerHTML = GOOSE.replace('<svg ', '<svg class="mt-goose" ') + `<div class="mt-head"><div><span class="off-tag hot">${esc(T('preTag'))}</span><h3>${esc(L10(o.title))}</h3></div><div class="mt-dates">${esc(T('preServed', { date: fromDate(o.serve_from) }))}</div></div>
    <p class="mt-lead">${esc(T('preLead'))}</p>
    <div class="mt-cols">
      <div class="mt-col"><h4><span>${esc(T('preMains'))}</span><b>${esc(T('prePer', { p: huf(o.main_price) }))}</b></h4>${items.filter(i => i.k[0] === 'm').map(dish).join('')}
        ${(o.platters || []).length ? `<h4 class="mt-h2"><span>${esc(T('prePlatters'))}</span><b>${esc(T('prePerPlatter', { p: huf(o.platter_price) }))}</b></h4>${items.filter(i => i.k[0] === 't').map(dish).join('')}` : ''}</div>
      <div class="mt-col"><h4><span>${esc(T('preDesserts'))}</span><b>${esc(T('prePer', { p: huf(o.dessert_price) }))}</b></h4>${items.filter(i => i.k[0] === 'd').map(dish).join('')}
        <div class="mt-sum"><div class="t"><span>${esc(T('preSum', { n: t.n }))}</span><b>${huf(t.food)} Ft</b></div>
          <button type="button" class="btn btn-red btn-lg" id="preNext"${t.n ? '' : ' disabled'}>${esc(T(t.n ? 'preNext' : 'prePick'))}</button>
          <small>${esc(T('preModes'))}</small></div></div>
    </div>`;
}
$('offPre').addEventListener('click', e => {
  const t = tgt(e); if (!t) return;
  const s = t.closest('[data-pq]');
  if (s) {
    const k = s.dataset.pq; preQty[k] = Math.max(0, Math.min(50, (preQty[k] || 0) + +s.dataset.d)); renderPre();
    const btn = $('offPre').querySelector(`[data-pq="${k}"][data-d="${s.dataset.d}"]`); btn?.parentElement.querySelector('b')?.classList.add('bump');
    if (btn && !btn.disabled) btn.focus();
  }
  else if (t.closest('#preNext')) openPre();
});

// --- az előrendelés ablaka
const PRE_ICON = {
  helyben: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3v8a3 3 0 0 0 3 3v7M8 3v6M11 3v8a3 3 0 0 1-3 3M17 21V3c-2 1-3 4-3 8h3"/></svg>',
  elvitel: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 8h14l-1 13H6L5 8zM9 8V6a3 3 0 0 1 6 0v2"/></svg>',
  kiszallitas: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h11v10H3zM14 9h4l3 3v4h-7"/><circle cx="7" cy="18" r="2"/><circle cx="17" cy="18" r="2"/></svg>'
};
function preMinDate() { const a = PRE.offer.serve_from, b = PRE.today || a; return a > b ? a : b; }
function buildPreDlg() {
  const modes = [['helyben', 'preDine', 'preDineSub'], ['elvitel', 'preTake', 'preTakeSub'], ['kiszallitas', 'preDel', 'preDelSub']];
  const towns = [...PRE.fees].sort((a, b) => a.name.localeCompare(b.name, 'hu')).map(f => `<option value="${esc(f.name)}">${esc(f.name)} · ${huf(f.fee)} Ft</option>`).join('');
  $('preBody').innerHTML = `<h3 id="preTitle">${esc(T('preDlgTitle'))}</h3><p class="pre-sub">${esc(L10(PRE.offer.title))} · ${esc(T('preServed', { date: fromDate(PRE.offer.serve_from) }))}</p>
    <div class="pre-lines" id="preLines"></div>
    <form id="preForm" novalidate>
      <div class="pre-lbl">${esc(T('preHow'))}</div>
      <div class="pre-modes" role="radiogroup" aria-label="${esc(T('preHow'))}">${modes.map(([v, l, sub]) => `<button type="button" role="radio" data-pm="${v}" aria-checked="${preMode === v}">${PRE_ICON[v]}<b>${esc(T(l))}</b><small>${esc(T(sub))}</small></button>`).join('')}</div>
      <div class="row2"><label class="f"><span>${esc(T('preDay'))}</span><input name="date" type="date" min="${preMinDate()}" required></label>
        <label class="f"><span>${esc(T('preTime'))}</span><input name="time" type="time" min="09:30" max="21:30" step="900" required></label></div>
      <div class="pre-g" data-for="helyben"><label class="f"><span>${esc(T('preGuests'))}</span><input name="guests" type="number" min="1" max="200" inputmode="numeric"></label></div>
      <div class="pre-g" data-for="kiszallitas"><label class="f"><span>${esc(T('preTown'))}</span><select name="city" id="preCity"><option value="">${esc(T('preTownPick'))}</option>${towns}</select></label>
        <label class="f"><span>${esc(T('preStreet'))}</span><input name="street" autocomplete="street-address"></label></div>
      <div class="pre-g" data-for="elvitel kiszallitas"><div class="pre-lbl">${esc(T('prePay'))}</div><div class="seg seg-pay" role="radiogroup">${['Készpénz', 'Kártya'].map(v => `<button type="button" role="radio" data-pp="${v}" aria-checked="${prePay === v}">${esc(T(v === 'Kártya' ? 'prePayCard' : 'prePayCash'))}</button>`).join('')}</div></div>
      <div class="row2"><label class="f"><span>${esc(T('preName'))}</span><input name="name" autocomplete="name" required></label>
        <label class="f"><span>${esc(T('prePhone'))}</span><input name="phone" type="tel" autocomplete="tel" inputmode="tel" required></label></div>
      <label class="f"><span>${esc(T('preEmail'))}</span><input name="email" type="email" autocomplete="email"></label>
      <label class="f"><span>${esc(T('preNote'))}</span><textarea name="note" rows="2"></textarea></label>
      <input class="hp" name="website" tabindex="-1" autocomplete="off" aria-hidden="true">
      <div class="pre-sum" id="preSum"></div>
      <p class="form-err" id="preErr" role="alert"></p>
      <button class="btn btn-red btn-lg btn-wide" type="submit" id="preSend"></button>
      <p class="pre-note" id="preNote"></p>
    </form>`;
  preBuilt = lang;
}
function syncPre() {
  const f = $('preForm'); if (!f) return;
  $$('[data-pm]', f).forEach(b => b.setAttribute('aria-checked', String(b.dataset.pm === preMode)));
  $$('[data-pp]', f).forEach(b => b.setAttribute('aria-checked', String(b.dataset.pp === prePay)));
  $$('.pre-g', f).forEach(g => { g.hidden = !g.dataset.for.split(' ').includes(preMode); });
  const t = preTotals();
  $('preLines').innerHTML = preItems().filter(it => preQty[it.k]).map(it => `<div><span>${preQty[it.k]}× ${esc(L10(it.d.name))}</span><b>${huf(preQty[it.k] * it.p)} Ft</b></div>`).join('') +
    `<button type="button" class="linkish" data-close>${esc(T('preEdit'))}</button>`;
  const row = (k, v) => `<div><span>${esc(k)}</span><span>${v}</span></div>`;
  $('preSum').innerHTML = row(T('preFood'), huf(t.food) + ' Ft') + (t.pack ? row(T('preBoxes', { n: t.n }), huf(t.pack) + ' Ft') : '') +
    (preMode === 'kiszallitas' ? row(T('preFee'), t.fee === null ? '–' : huf(t.fee) + ' Ft') : '') +
    `<div class="tot"><span>${esc(T('preTotal'))}</span><span>${huf(t.total)} Ft</span></div>`;
  $('preSend').textContent = T('preSend', { p: huf(t.total) });
  $('preNote').textContent = T(preMode === 'helyben' ? 'prePayDine' : preMode === 'elvitel' ? 'prePayTake' : 'prePayDel') + ' ' + T('preConfirm');
}
function openPre() {
  if (!PRE || !preTotals().n) return;
  if (preBuilt !== lang || !$('preForm')) buildPreDlg();
  const f = $('preForm'), t = preTotals();
  if (!f.elements.date.value) f.elements.date.value = preMinDate();
  if (!+f.elements.guests.value && t.mains) f.elements.guests.value = t.mains;
  syncPre(); $('preErr').textContent = '';
  openModal($('preDlg')); track('ev', { n: 'pre_open' });
}
$('preDlg').addEventListener('click', e => {
  const t = tgt(e); if (!t) return;
  const m = t.closest('[data-pm]'), pp = t.closest('[data-pp]');
  if (m) { preMode = m.dataset.pm; syncPre(); }
  else if (pp) { prePay = pp.dataset.pp; syncPre(); }
});
$('preDlg').addEventListener('change', e => { if (tgt(e)?.name === 'city') syncPre(); });
$('preDlg').addEventListener('input', e => tgt(e)?.closest('.f')?.classList.remove('bad'));
$('preDlg').addEventListener('submit', async e => {
  e.preventDefault();
  const f = $('preForm'), v = Object.fromEntries(new FormData(f)), err = $('preErr');
  const bad = (name, msg) => { const el = f.elements[name]; el.closest('.f')?.classList.add('bad'); el.focus(); err.textContent = msg; };
  err.textContent = '';
  const min = preMinDate();
  if (!v.date || v.date < min) return bad('date', T('preErrDate', { date: fromDate(min) }));
  if (!v.time || v.time < '09:30' || v.time > '21:30') return bad('time', T('preErrTime'));
  if (preMode === 'helyben' && !(+v.guests >= 1 && +v.guests <= 200)) return bad('guests', T('preErrGuests'));
  if (preMode === 'kiszallitas' && !v.city) return bad('city', T('preErrTown'));
  if (preMode === 'kiszallitas' && !v.street.trim()) return bad('street', T('preErrStreet'));
  if (!v.name.trim()) return bad('name', T('errName'));
  if (v.phone.replace(/\D/g, '').length < 6) return bad('phone', T('preErrPhone'));
  if (v.email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v.email.trim())) return bad('email', T('errEmail'));
  const btn = $('preSend'); btn.disabled = true; btn.textContent = T('preSending');
  const items = {}; for (const k in preQty) if (preQty[k]) items[k] = preQty[k];
  try {
    const r = await fetch('/api/public/preorder', { method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ items, mode: preMode, date: v.date, time: v.time, guests: +v.guests || 0, city: preMode === 'kiszallitas' ? v.city : '', street: preMode === 'kiszallitas' ? v.street : '',
        name: v.name, phone: v.phone, email: v.email, note: v.note, payment: prePay, lang, website: v.website }) });
    const d = await r.json().catch(() => ({}));
    if (!r.ok || !d.ok) { btn.disabled = false; syncPre(); err.textContent = (lang === 'hu' && d.error) || (r.status >= 500 ? T('preErrNet') : T('preErrCheck')); return; }
    track('ev', { n: 'pre_sent', v: preMode });
    const when = fmtDay(v.date, { month: 'long', day: 'numeric', weekday: 'long' }) + ', ' + v.time;
    $('preBody').innerHTML = `<div class="pre-done"><div class="ic">${SVG_CHECK}</div><h3>${esc(T('preDone'))}</h3><p>${esc(T(preMode === 'helyben' ? 'preDine' : preMode === 'elvitel' ? 'preTake' : 'preDel'))} · ${esc(when)}</p><p>${esc(T('preDoneText'))}</p><button type="button" class="btn btn-red btn-lg" data-close>${esc(T('preClose'))}</button></div>`;
    preQty = {}; preBuilt = false; renderPre();
  } catch (x) { btn.disabled = false; syncPre(); err.textContent = T('preErrNet'); }
});
const SVG_CHECK = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg>';
function weekRange(m) {
  try { return new Intl.DateTimeFormat(LOC(), { month: 'long', day: 'numeric' }).formatRange(isoDay(m.from), isoDay(m.to)); }
  catch (e) { return fmtDay(m.from, { month: 'long', day: 'numeric' }) + ' – ' + fmtDay(m.to, { month: 'long', day: 'numeric' }); }
}
function renderWeekly() {
  const m = OFF && OFF.weekly, el = $('offWeekly');
  el.hidden = !m;
  if (!m) return;
  if (!wkDay || !m.days.some(d => d.date === wkDay)) wkDay = (m.days.find(d => d.date === OFF.today) || m.days[0]).date;
  const d = m.days.find(x => x.date === wkDay);
  if (wkSoup >= d.soups.length) wkSoup = 0;
  if (wkMain >= d.mains.length) wkMain = 0;
  const letter = i => String.fromCharCode(65 + i);
  const price = (n, p, sub) => `<button class="wk-price" data-courses="${n}" aria-pressed="${wkCourses === n}"><small class="k">${esc(T(n === 2 ? 'weeklyTwo' : 'weeklyThree'))}</small><b>${huf(p)} Ft</b><small>${esc(T(sub))}</small></button>`;
  const opt = (name, i, mk, txt) => `<label class="wk-opt"><input type="radio" name="${name}" value="${i}"${(name === 'wkSoup' ? wkSoup : wkMain) === i ? ' checked' : ''}><span class="mk">${mk}</span><span>${esc(txt)}</span></label>`;
  el.innerHTML = `<div class="wk-head"><div><h3>${esc(T(m.upcoming ? 'weeklyNext' : 'weeklyTitle'))}</h3><div class="dates">${esc(weekRange(m))}</div></div>
    <div class="wk-prices" role="group">${price(2, m.two, 'weeklyTwoSub')}${price(3, m.three, 'weeklyThreeSub')}</div></div>
    <div class="wk-days" role="tablist">${m.days.map(x => `<button class="wk-day" role="tab" data-day="${x.date}" aria-selected="${x.date === wkDay}">${esc(fmtDay(x.date, { weekday: 'long' }))}<small>${esc(isoDay(x.date).toLocaleDateString(LOC(), { month: 'short', day: 'numeric' }))}</small>${x.date === OFF.today ? `<small class="today">${esc(T('weeklyToday'))}</small>` : ''}</button>`).join('')}</div>
    <div class="wk-cols${wkSwap ? ' swap' : ''}" role="tabpanel" style="--dir:${wkSwap * 14}px">
      <fieldset class="wk-col" style="border:0;padding:0;margin:0"><legend><h4>${esc(T('weeklySoups'))}</h4></legend>${d.soups.map((s, i) => opt('wkSoup', i, '•', L10(s))).join('')}</fieldset>
      <fieldset class="wk-col" style="border:0;padding:0;margin:0"><legend><h4>${esc(T('weeklyMains'))}</h4></legend>${d.mains.map((s, i) => opt('wkMain', i, letter(i), L10(s))).join('')}</fieldset>
    </div>
    ${d.dessert ? `<p class="wk-dessert${wkCourses === 3 ? ' on' : ''}"><span class="k">${esc(T('weeklyDessert'))}</span><span>${esc(L10(d.dessert))}</span></p>` : ''}
    <div class="wk-foot">${wkFoot(m, d)}</div>`;
  if ($('wkOrder')) $('wkOrder').href = quickHref(weeklyText(m, d));
  wkSwap = 0;
}
// Napi ajánlat: hétköznap 9:30–14:00 rendelhető, mindig az aznapi. 9:30 és 11:00 között előrendelés,
// ilyenkor Zalaszentgrótra, Zalaudvarnokra és Zalaszentlászlóra nincs szállítási díj (a rendeléskezelő
// ellenőrzi és számolja; itt csak megmutatjuk). Az idő a szerver budapesti órájából jön.
const DAILY = { from: 9 * 60 + 30, free: 11 * 60, to: 14 * 60 };
const nowMin = () => (OFF && OFF.now_min != null) ? OFF.now_min + Math.floor((Date.now() - (OFF._t || Date.now())) / 60000) : 12 * 60;
function dailyState() { if (M && M.closed_today) return 'closed'; const n = nowMin(); return n < DAILY.from ? 'before' : n < DAILY.free ? 'pre' : n < DAILY.to ? 'open' : 'closed'; }
function wkFoot(m, d) {
  const price = huf(wkCourses === 3 ? m.three : m.two);
  const free = `<p class="wk-free">${esc(T('weeklyFree'))}</p>`;
  if (d.date === OFF.today) {
    const st = dailyState();
    if (st === 'pre' || st === 'open') return `<p>${esc(T('weeklyHint'))}</p>${st === 'pre' ? free : ''}<a class="btn btn-red btn-lg" id="wkOrder" href="#">${esc(T(st === 'pre' ? 'weeklyPre' : 'weeklyOrder'))} · ${price} Ft</a>`;
    if (st === 'before') return `<p>${esc(T('weeklyRule'))}</p>${free}<span class="btn btn-lg wk-later" aria-disabled="true">${esc(T('weeklyLater'))}</span>`;
    return `<p>${esc(T('weeklyRule'))}</p><span class="btn btn-lg wk-later" aria-disabled="true">${esc(T('weeklyClosed'))}</span>`;
  }
  const day = isoDay(d.date).toLocaleDateString(LOC(), { weekday: 'long' });
  const past = d.date < OFF.today;
  return `<p>${esc(T('weeklyRule'))}</p>${past ? '' : free}<span class="btn btn-lg wk-later" aria-disabled="true">${esc(past ? T('weeklyPast') : T('weeklyOnDay', { day }))}</span>`;
}
// percenként: ha közben 9:30, 11:00 vagy 14:00 lett, frissítjük a gombot
let lastDaily = null;
setInterval(() => { if (!OFF || !OFF.weekly) return; const s = dailyState(); if (lastDaily !== null && s !== lastDaily) renderWeekly(); lastDaily = s; }, 30000);
function weeklyText(m, d) {
  const three = wkCourses === 3;
  const dd = isoDay(d.date);
  return 'Napi ajánlat – ' + dd.toLocaleDateString('hu-HU', { weekday: 'long' }) + ' (' + dd.toLocaleDateString('hu-HU', { month: 'short', day: 'numeric' }) + '), ' +
    (three ? 'háromfogásos (' + huf(m.three) + ' Ft)' : 'kétfogásos (' + huf(m.two) + ' Ft)') +
    '\nLeves: ' + d.soups[wkSoup].hu + '\nFőétel: ' + String.fromCharCode(65 + wkMain) + ') ' + d.mains[wkMain].hu +
    (three ? '\nDesszert: ' + (d.dessert ? d.dessert.hu : 'a háromfogásos menü desszertje') : '');
}
$('offWeekly').addEventListener('click', e => {
  const t = tgt(e); if (!t) return;
  const dayB = t.closest('[data-day]'), pr = t.closest('[data-courses]');
  if (dayB) { wkSwap = dayB.dataset.day > wkDay ? 1 : dayB.dataset.day < wkDay ? -1 : 0; wkDay = dayB.dataset.day; wkSoup = 0; wkMain = 0; renderWeekly(); $('offWeekly').querySelector(`[data-day="${wkDay}"]`)?.focus(); }
  else if (pr) { wkCourses = +pr.dataset.courses; renderWeekly(); }
  else if (t.closest('#wkOrder')) track('ev', { n: 'weekly_order' });
});
$('offWeekly').addEventListener('change', e => {
  const t = tgt(e); if (!t || t.type !== 'radio') return;
  if (t.name === 'wkSoup') wkSoup = +t.value; else wkMain = +t.value;
  const m = OFF.weekly; if ($('wkOrder')) $('wkOrder').href = quickHref(weeklyText(m, m.days.find(x => x.date === wkDay)));
});
$('offWeekend').addEventListener('click', e => { if (tgt(e)?.closest('a')) track('ev', { n: 'weekend_order' }); });

async function loadMenu() {
  try {
    const [m, tr, dr] = await Promise.all([
      fetch('/api/menu', { cache: 'no-store' }).then(r => { if (!r.ok) throw 0; return r.json(); }),
      fetch('/data/menu-i18n.json').then(r => r.json()).catch(() => ({ sections:{}, items:{} })),
      fetch('/data/drinks.json').then(r => r.json()).catch(() => [])
    ]);
    if (!m.sections) throw 0;
    M = m; TR = tr; DR = dr;
    $('kocsonya').hidden = !M.sections.some(s => s.id === 'kocsonya'); // csak ha a rendeléskezelőben be van kapcsolva (nem fogyott el)
  } catch (e) { $('food').innerHTML = `<p class="empty">${esc(T('menuError'))}</p>`; return; }
  renderChips(); renderFood(); renderDrinks(); renderFees(); renderFacts(); renderStatus(); buildWheelFilters();
  const opts = [...M.fees].sort((a, b) => a.name.localeCompare(b.name, 'hu')).map(f => `<option value="${esc(f.name)}">`).join(''); $('towns').innerHTML = opts;
}

function renderChips() {
  const chips = $('chips');
  if (tab === 'drinks') { chips.innerHTML = DR.map(s => `<button class="chip" data-go="d-${s.id}">${esc(s[lang] || s.hu)}</button>`).join(''); }
  else if (M) chips.innerHTML = M.sections.map(s => `<button class="chip" data-go="s-${s.id}">${esc(trSecShort(s))}</button>`).join('');
}
function goSection(go) {
  if (go.startsWith('s-')) secFood = go.slice(2); else secDrink = go.slice(2);
  $('q').value = '';
  tab === 'food' ? renderFood() : renderDrinks();
  const t = $('etlap').querySelector('.chips-bar'), y = t.getBoundingClientRect().top + scrollY - top.offsetHeight;
  if (scrollY > y) scrollTo({ top: y, behavior: reduced ? 'auto' : 'smooth' });
}
$('chips').addEventListener('click', e => { const b = tgt(e)?.closest('[data-go]'); if (b) goSection(b.dataset.go); });
$('etlap').addEventListener('click', e => { const b = tgt(e)?.closest('.pager [data-go]'); if (b) goSection(b.dataset.go); });

function itemHTML(it, i = 0) {
  const st = ` style="--i:${Math.min(i, 14)}"`;
  const { name, desc } = trItem(it);
  const vs = variants(it), paused = M && M.open === false;
  const al = it.alg ? `<div class="al">${esc(T('allergens'))}: ${it.alg.split(/\s*,\s*/).map(n => `<button data-alg="${esc(n)}">${esc(n)}</button>`).join(', ')}</div>` : '';
  const ds = desc ? `<div class="ds">${esc(desc)}</div>` : '';
  const sup = it.alg ? `<span class="als">${it.alg.split(/\s*,\s*/).map(n => `<button data-alg="${esc(n)}" aria-label="${esc(T('allergens'))} ${esc(n)}">${esc(n)}</button>`).join('')}</span>` : '';
  if (vs.length > 1) {
    const pills = vs.map(v => paused
      ? `<span class="var">${esc(trVar(v.label))} <b>${huf(v.price)} Ft</b></span>`
      : `<a class="var add-v" href="${orderHref(it.id, v.label)}" aria-label="${esc(T('addToCart', { n: name + ' ' + trVar(v.label) }))}">${esc(trVar(v.label))} <b>${huf(v.price)} Ft</b>${plus}</a>`).join('');
    return `<div class="mi mv" id="i-${esc(it.id)}"${st}><div class="nm"><span>${esc(name)}${sup}</span></div>${ds}<div class="vars">${pills}</div>${al}</div>`;
  }
  const btn = paused ? '' : `<a class="add" href="${orderHref(it.id, vs[0].label)}" aria-label="${esc(T('addToCart', { n: name }))}">${plus}</a>`;
  return `<div class="mi" id="i-${esc(it.id)}"${st}><div class="nm"><span>${esc(name)}${sup}</span></div><div class="pr">${huf(vs[0].price)} Ft ${btn}</div>${ds}${al}</div>`;
}
let secFood = null, secDrink = null;
function pager(list, cur, nameOf, prefix) {
  const i = list.findIndex(x => x.id === cur), nx = list[i + 1], pv = list[i - 1];
  return `<div class="pager">${pv ? `<button class="btn btn-ghost" data-go="${prefix}${pv.id}"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m15 18-6-6 6-6"/></svg>${esc(nameOf(pv))}</button>` : '<span></span>'}${nx ? `<button class="btn btn-ghost" data-go="${prefix}${nx.id}">${esc(nameOf(nx))}<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 18 6-6-6-6"/></svg></button>` : ''}</div>`;
}
function renderFood() {
  if (!M) return;
  const q = norm($('q').value.trim());
  if (!secFood || !M.sections.some(s => s.id === secFood)) secFood = M.sections[0].id;
  let html = '', n = 0;
  for (const s of M.sections) {
    if (!q && s.id !== secFood) continue;
    const items = s.items.filter(it => { if (!q) return true; const t = trItem(it); return norm(t.name + ' ' + t.desc + ' ' + it.name + ' ' + (it.desc || '')).includes(q); });
    if (!items.length) continue; n += items.length;
    html += `<div class="msec${q ? '' : ' one'}" id="s-${esc(s.id)}"><h3>${esc(trSec(s))}</h3><div class="items">${items.map((it, i) => itemHTML(it, i)).join('')}</div></div>`;
  }
  if (!q) html += pager(M.sections, secFood, trSecShort, 's-');
  $('food').innerHTML = n ? html : `<p class="empty">${esc(T('noResults', { q: $('q').value.trim() }))}</p>`;
  markChips();
}
function renderDrinks() {
  if (!DR.length) return;
  const q = norm($('q').value.trim());
  if (!secDrink) secDrink = DR[0].id;
  const L = o => (o && (o[lang] || o.hu)) || '';
  let html = `<p class="drink-note">${esc(T('drinksNote'))}</p>`, n = 0;
  for (const s of DR) {
    if (!q && s.id !== secDrink) continue;
    const items = s.items.filter(it => !q || norm((it[lang] || it.hu) + ' ' + it.hu + ' ' + L(it.sub)).includes(q));
    if (!items.length) continue; n += items.length;
    html += `<div class="msec${q ? '' : ' one'}" id="d-${s.id}"><h3>${esc(s[lang] || s.hu)}</h3>${s.note ? `<p class="note">${esc(L(s.note))}</p>` : ''}<div class="items">` +
      items.map(it => { const pr = (lang === 'en' && it.priceEn) || (lang === 'de' && it.priceDe) || it.price; return `<div class="mi"><div class="nm">${esc(it[lang] || it.hu)}</div><div class="pr">${esc(pr.replace(/\d{4,}/g, m => huf(m)))} Ft</div>${it.sub ? `<div class="ds">${esc(L(it.sub))}</div>` : ''}</div>`; }).join('') + '</div></div>';
  }
  if (!q) html += pager(DR, secDrink, s => s[lang] || s.hu, 'd-');
  $('drinks').innerHTML = n ? html : `<p class="empty">${esc(T('noResults', { q: $('q').value.trim() }))}</p>`;
  markChips();
}
function markChips() {
  const q = $('q').value.trim(), cur = tab === 'food' ? 's-' + secFood : 'd-' + secDrink;
  $$('#chips .chip').forEach(c => { const on = !q && c.dataset.go === cur; c.classList.toggle('on', on); if (on) { const ch = $('chips'); ch.scrollTo({ left: c.offsetLeft - ch.clientWidth / 2 + c.offsetWidth / 2, behavior: reduced ? 'auto' : 'smooth' }); } });
}
let qT; $('q').addEventListener('input', () => { clearTimeout(qT); qT = setTimeout(() => tab === 'food' ? renderFood() : renderDrinks(), 120); });
$$('[data-tab]').forEach(b => b.addEventListener('click', () => {
  tab = b.dataset.tab;
  $$('[data-tab]').forEach(x => x.setAttribute('aria-selected', String(x === b)));
  $('food').hidden = tab !== 'food'; $('drinks').hidden = tab !== 'drinks';
  $('wheelBtn').hidden = tab !== 'food';
  renderChips(); tab === 'food' ? renderFood() : renderDrinks();
}));
// ragadós kategóriasáv árnyéka
const chipsBar = $('chipsBar');
new IntersectionObserver(([e]) => chipsBar.classList.toggle('stuck', e.intersectionRatio < 1), { rootMargin: `-${top.offsetHeight + 1}px 0px 0px 0px`, threshold: [1] }).observe(chipsBar);

// kosárba gomb: rövid visszajelzés, aztán megyünk a rendelési oldalra
document.addEventListener('click', e => {
  const a = tgt(e)?.closest('a.add, a.add-v'); if (!a) return;
  e.preventDefault(); toast(T('added'));
  try { track('ev', { n: 'add', v: new URL(a.href, location.href).searchParams.get('add') }); } catch (er) {}
  a.closest('.mi')?.classList.add('hit');
  setTimeout(() => { location.href = a.href; }, flyToCart(a));
});

/* ---------------------------------------------------------------- allergének */
function renderAlg(hl) { $('algList').innerHTML = T('alg').map((a, i) => `<li class="${hl && hl.includes(String(i + 1)) ? 'hl' : ''}">${esc(a)}</li>`).join(''); }
$('algBtn').addEventListener('click', () => { renderAlg(); openModal($('alg')); });
document.addEventListener('click', e => {
  const b = tgt(e)?.closest('[data-alg]'); if (!b) return;
  const all = [...b.parentElement.querySelectorAll('[data-alg]')].map(x => x.dataset.alg);
  e.preventDefault();
  renderAlg(all); openModal($('alg')); track('ev', { n: 'alg' });
});

/* ---------------------------------------------------------------- modális ablakok */
$$('dialog').forEach(d => {
  d.addEventListener('click', e => { if (e.target === d || tgt(e)?.closest('[data-close]')) d.close(); });
  d.addEventListener('close', () => { document.body.style.overflow = ''; });
});
const openModal = d => { d.showModal(); document.body.style.overflow = 'hidden'; };

/* ---------------------------------------------------------------- Mit egyek ma? */
const WHEEL_SKIP = ['koretek', 'savanyusagok', 'szoszok-es-martogatosok', 'burger-extrak-es-udito'];
let wheelSec = 'all', spinning = false;
function wheelPool() {
  if (!M) return [];
  return M.sections.filter(s => !WHEEL_SKIP.includes(s.id) && (wheelSec === 'all' || s.id === wheelSec)).flatMap(s => s.items);
}
function buildWheelFilters() {
  if (!M) return;
  const secs = M.sections.filter(s => !WHEEL_SKIP.includes(s.id));
  $('wheelFilters').innerHTML = `<button class="chip${wheelSec === 'all' ? ' on' : ''}" data-ws="all">${esc(T('all'))}</button>` + secs.map(s => `<button class="chip${wheelSec === s.id ? ' on' : ''}" data-ws="${s.id}">${esc(trSecShort(s))}</button>`).join('');
}
$('wheelFilters').addEventListener('click', e => { const b = tgt(e)?.closest('[data-ws]'); if (!b || spinning) return; wheelSec = b.dataset.ws; buildWheelFilters(); });
$('wheelBtn').addEventListener('click', () => {
  buildWheelFilters(); $('reelName').textContent = '?'; $('reelName').className = 'reel-name'; $('reelMeta').textContent = '';
  $('wheelOrder').hidden = true; $('spin').textContent = T('spin'); openModal($('wheel'));
});
$('spin').addEventListener('click', () => {
  const pool = wheelPool(); if (!pool.length || spinning) return;
  spinning = true; $('wheelOrder').hidden = true; $('reelMeta').textContent = '';
  const pick = pool[Math.floor(Math.random() * pool.length)];
  const name = $('reelName');
  let i = 0; const steps = reduced ? 1 : 26;
  const step = () => {
    i++;
    const it = i >= steps ? pick : pool[Math.floor(Math.random() * pool.length)];
    name.textContent = trItem(it).name;
    name.className = 'reel-name'; void name.offsetWidth;
    if (i < steps) { name.classList.add('tick'); setTimeout(step, 45 + Math.pow(i / steps, 3) * 320); }
    else {
      name.classList.add('landed'); spinning = false;
      const vs = variants(pick), t = trItem(pick);
      $('reelMeta').textContent = [vs.map(v => (v.label ? v.label + ' ' : '') + huf(v.price) + ' Ft').join(' · '), t.desc].filter(Boolean).join(' — ');
      const o = $('wheelOrder'); o.href = orderHref(pick.id, vs[0].label); o.hidden = M.open === false; o.dataset.id = pick.id;
      $('spin').textContent = T('spinAgain');
      const r = $('reel').getBoundingClientRect(); burst(r.left + r.width / 2, r.top - 10, 8);
    }
  };
  step();
});

/* ---------------------------------------------------------------- kiszállítás */
function renderFacts() {
  if (!M || !M.fees || !M.fees.length) return;
  const n = M.fees.length, min = Math.min(...M.fees.map(f => f.fee));
  $('factDelivery').textContent = T('factDeliveryN', { n, min: huf(min) });
  $('heroDelivery').textContent = T('heroDeliveryN', { n });
}
function renderFees() {
  if (!M) return;
  const fees = [...M.fees].sort((a, b) => a.name.localeCompare(b.name, 'hu'));
  $('feeTable').innerHTML = fees.map(f => `<div class="fee-row"><span>${esc(f.name)}</span><b>${huf(f.fee)} Ft</b></div>`).join('');
}
const van = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7h11v9H3zM14 10h4l3 3v3h-7"/><circle cx="7" cy="17.5" r="1.6"/><circle cx="17" cy="17.5" r="1.6"/></svg>';
let lastTown = '';
function checkTown() {
  if (!M) return;
  const raw = $('town').value.trim(), q = norm(raw), out = $('delResult');
  if (q.length < 2) { out.innerHTML = ''; lastTown = ''; return; }
  const exact = M.fees.find(f => norm(f.name) === q);
  const starts = M.fees.filter(f => norm(f.name).startsWith(q));
  const f = exact || (starts.length === 1 ? starts[0] : null);
  if (f) {
    if (lastTown === f.name + lang) return; lastTown = f.name + lang;
    track('ev', { n: 'town', v: f.name, ok: true });
    out.innerHTML = `<div class="del-card yes"><span class="van">${van}</span><div class="tx"><b>${esc(T('delYes'))}</b><div class="fee">${esc(f.name)}: ${esc(T('delFee', { fee: huf(f.fee) }))}</div></div>${M.open === false ? '' : `<a class="btn btn-red" href="/rendeles?city=${encodeURIComponent(f.name)}">${esc(T('delOrder'))}</a>`}</div>`;
  } else if (!starts.length && q.length >= 3) {
    if (lastTown === '!' + q + lang) return; lastTown = '!' + q + lang;
    clearTimeout(checkTown.t); checkTown.t = setTimeout(() => { if ($('town').value.trim() === raw) track('ev', { n: 'town', v: raw, ok: false }); }, 1500);
    out.innerHTML = `<div class="del-card no"><div class="tx"><b>${esc(T('delNo'))}</b><div class="fee">${esc(T('delNoSub'))}</div></div><a class="btn btn-ghost" href="tel:+36203395099">${esc(T('call'))}</a></div>`;
  } else { out.innerHTML = ''; lastTown = ''; }
}
$('town').addEventListener('input', checkTown);
$('town').addEventListener('change', checkTown);

/* ---------------------------------------------------------------- galéria */
const gal = $$('#gal .g'); let gi = 0;
function showImg(i) {
  gi = (i + gal.length) % gal.length; const b = gal[gi], img = $('lbImg');
  img.style.animation = 'none'; void img.offsetWidth; img.style.animation = '';
  img.src = b.dataset.full; img.alt = b.querySelector('img').alt; $('lbCap').textContent = img.alt;
}
gal.forEach((b, i) => b.addEventListener('click', () => { showImg(i); openModal($('lb')); track('ev', { n: 'gallery' }); }));
$('lbPrev').addEventListener('click', () => showImg(gi - 1));
$('lbNext').addEventListener('click', () => showImg(gi + 1));
$('lb').addEventListener('keydown', e => { if (e.key === 'ArrowLeft') showImg(gi - 1); if (e.key === 'ArrowRight') showImg(gi + 1); });
let tx0 = null;
$('lbImg').addEventListener('touchstart', e => { tx0 = e.touches[0].clientX; }, { passive: true });
$('lbImg').addEventListener('touchend', e => { if (tx0 === null) return; const d = e.changedTouches[0].clientX - tx0; if (Math.abs(d) > 50) showImg(gi + (d < 0 ? 1 : -1)); tx0 = null; });
// rendezvényes képsor: kattintásra is nagyítható
$$('#strip img').forEach(im => im.addEventListener('click', () => {
  const name = im.getAttribute('src').replace('-s.webp', '.webp'); const i = gal.findIndex(g => g.dataset.full === name);
  if (i >= 0) { showImg(i); openModal($('lb')); }
}));

/* ---------------------------------------------------------------- üzenet / foglalás */
const form = $('form');
function setKind(k) {
  kind = k;
  if (!$('whenRow')) return;          // az űrlap már el lett küldve
  $$('[data-kind]', form).forEach(b => b.setAttribute('aria-checked', String(b.dataset.kind === k)));
  $('whenRow').hidden = k === 'egyeb';
  $('msgLabel').textContent = T(k === 'asztal' ? 'fMsgTable' : k === 'rendezveny' ? 'fMsgEvent' : 'fMsgOther');
}
$$('[data-kind]', form).forEach(b => b.addEventListener('click', () => setKind(b.dataset.kind)));
$$('a[data-kind]').forEach(a => a.addEventListener('click', () => setKind(a.dataset.kind)));
// ma előtti napot ne lehessen választani
(() => { const d = new Date(); const iso = new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10); form.elements.date.min = iso; })();
form.addEventListener('input', e => tgt(e)?.closest('.f')?.classList.remove('bad'));
form.addEventListener('submit', async e => {
  e.preventDefault();
  const v = Object.fromEntries(new FormData(form));
  const bad = (name, msg) => { const el = form.elements[name]; el.closest('.f').classList.add('bad'); el.focus(); $('formErr').textContent = T(msg); };
  $('formErr').textContent = '';
  if (!v.name.trim()) return bad('name', 'errName');
  if (v.phone.replace(/\D/g, '').length < 6 && !/@/.test(v.email)) return bad('phone', 'errContact');
  if (v.email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v.email.trim())) return bad('email', 'errEmail');
  if (kind !== 'egyeb' && !v.date) return bad('date', 'errDate');
  if (kind !== 'egyeb' && v.date < form.elements.date.min) return bad('date', 'errPast');
  if (kind !== 'egyeb' && v.time && (v.time < '11:00' || v.time > '21:30')) return bad('time', 'errTime');
  if (kind !== 'egyeb' && v.guests && !(+v.guests >= 1 && +v.guests <= 500)) return bad('guests', 'errGuests');
  if (kind === 'egyeb' && !v.text.trim()) return bad('text', 'errText');
  const btn = $('sendBtn'); btn.disabled = true; btn.textContent = T('sending');
  try {
    const body = { kind, name: v.name, phone: v.phone, email: v.email, text: v.text, lang, website: v.website };
    if (kind !== 'egyeb') Object.assign(body, { date: v.date, time: v.time, guests: parseInt(v.guests, 10) || 0 });
    const r = await fetch('/api/public/message', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const d = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error((lang === 'hu' && d.error) || T('errNet'));
    form.classList.add('sent'); track('ev', { n: 'form', v: kind });
    form.dataset.html = form.innerHTML;
    form.innerHTML = `<div class="sent-ic"><svg viewBox="0 0 24 24"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg></div><h3>${esc(T('sentTitle'))}</h3><p class="form-note">${esc(T('sentText'))}</p><button type="button" class="btn btn-ghost" id="again">${esc(T('sentAgain'))}</button>`;
    const r2 = form.getBoundingClientRect(); burst(r2.left + r2.width / 2, r2.top + 60, 10);
    $('again').addEventListener('click', () => location.reload());
  } catch (err) {
    $('formErr').textContent = err.message && err.message !== 'Failed to fetch' ? err.message : T('errNet');
    btn.disabled = false; btn.textContent = T('send');
  }
});

/* ---------------------------------------------------------------- toast */
function toast(t) { const e = $('toast'); e.textContent = t; e.classList.add('show'); clearTimeout(e._h); e._h = setTimeout(() => e.classList.remove('show'), 1800); }

/* ---------------------------------------------------------------- évszakok: hulló díszek, füzér, tűzijáték
   A téma a <html data-season> értéke: a /season.js állítja be a szerver órája szerint (az irányítópultról
   felülbírálható). base = pipacsok (tavasz, nyár), autumn = ősz, winter = tél, christmas = karácsony,
   christmas2 = dec. 23–25., newyear = szilveszter és újév. Előnézet: ?tema=tel, ?tema=karacsony2 stb. */
const SEASON = document.documentElement.dataset.season || 'base';
const SEASONS = {
  base:       { n: [6, 3],   mix: { petal: 1 },                          burst: { petal: 1 } },
  autumn:     { n: [16, 9],  mix: { leaf: .7, petal: .3 },             burst: { leaf: .5, petal: .5 } },
  winter:     { n: [46, 26], mix: { snow: .88, petal: .12 },             burst: { snow: .7, petal: .3 } },
  christmas:  { n: [44, 24], mix: { snow: .84, star: .06, petal: .1 },   burst: { star: .45, snow: .4, petal: .15 } },
  christmas2: { n: [84, 46], mix: { snow: .88, star: .1, petal: .02 },   burst: { star: .6, snow: .4 } },
  newyear:    { n: [26, 14], mix: { conf: .75, star: .25 },              burst: { conf: .6, star: .4 } },
};
const SZ = SEASONS[SEASON] || SEASONS.base;
const PAL = {
  petal: ['#c4140a', '#d9261b', '#a5100a', '#e0412f', '#b81d12'],
  leaf: ['#c8641e', '#d98a2b', '#a8501a', '#b8862f', '#8f3f12', '#c9a03a'],
  conf: ['#d4a52a', '#e8c25a', '#c4140a', '#2a8c8c', '#b9b9c9', '#f0d68a'],
  star: ['#e7b53c', '#f2cb5d', '#d9a12a'],
  snow: ['#fff'],
};
const pickOf = mix => { let r = Math.random(); for (const k in mix) if ((r -= mix[k]) <= 0) return k; return Object.keys(mix)[0]; };
function particle(kind, x, y, burstV) {
  const R = Math.random, pal = PAL[kind];
  const s = kind === 'snow' ? 1.3 + R() * (SEASON === 'christmas2' ? 3.2 : 2.6) : kind === 'star' ? 4 + R() * 4 : kind === 'conf' ? 5 + R() * 4 : kind === 'leaf' ? 9 + R() * 9 : 7 + R() * 9;
  const fall = kind === 'snow' ? .4 + R() * .7 + s * .08 : kind === 'conf' ? .6 + R() * .7 : .35 + R() * .6;
  return { kind, c: pal[(R() * pal.length) | 0], x, y, s, r: R() * 6.28, vr: (R() - .5) * (kind === 'conf' ? .14 : .05),
    vx: burstV ? (R() - .5) * burstV : (R() - .3) * .5, vy: burstV ? -R() * burstV * .8 : fall, sw: R() * 6.28, life: burstV ? 1 : -1, flip: R() * 6.28 };
}
function star5(g, r) { g.beginPath(); for (let i = 0; i < 10; i++) { const a = -Math.PI / 2 + i * Math.PI / 5, q = i % 2 ? r * .45 : r; g.lineTo(Math.cos(a) * q, Math.sin(a) * q); } g.closePath(); }
function drawP(g, p) {
  g.save(); g.translate(p.x, p.y);
  g.globalAlpha = p.life > 0 ? Math.min(1, p.life * 1.5) : .92;
  if (p.kind === 'snow') {
    g.beginPath(); g.arc(0, 0, p.s, 0, 6.29); g.fillStyle = '#fff'; g.fill();
    g.lineWidth = .9; g.strokeStyle = 'rgba(100,128,160,.55)'; g.stroke(); g.restore(); return;
  }
  g.rotate(p.r);
  if (p.kind === 'star') {
    g.globalAlpha *= .6 + .4 * Math.sin(p.sw * 3); star5(g, p.s); g.fillStyle = p.c; g.fill();
    g.lineWidth = .8; g.strokeStyle = '#a87d16'; g.stroke(); g.restore(); return;
  }
  g.scale(1, Math.abs(Math.cos(p.flip)) * .7 + .3);
  g.fillStyle = p.c; g.beginPath();
  if (p.kind === 'conf') { g.fillRect(-p.s / 2, -p.s * .22, p.s, p.s * .44); g.restore(); return; }
  if (p.kind === 'leaf') {
    g.moveTo(0, -p.s); g.quadraticCurveTo(p.s * .8, -p.s * .3, 0, p.s); g.quadraticCurveTo(-p.s * .8, -p.s * .3, 0, -p.s); g.fill();
    g.globalAlpha *= .45; g.strokeStyle = '#4a2208'; g.lineWidth = 1; g.beginPath(); g.moveTo(0, -p.s * .8); g.lineTo(0, p.s * 1.3); g.stroke(); g.restore(); return;
  }
  g.moveTo(0, -p.s); g.bezierCurveTo(p.s * .9, -p.s * .8, p.s * .9, p.s * .6, 0, p.s * .55); g.bezierCurveTo(-p.s * .9, p.s * .6, -p.s * .9, -p.s * .8, 0, -p.s);
  g.fill(); g.globalAlpha *= .25; g.fillStyle = '#3a0904'; g.beginPath(); g.ellipse(0, p.s * .3, p.s * .18, p.s * .12, 0, 0, 6.28); g.fill();
  g.restore();
}
// egy részecske mozgatása; false, ha eltűnt
function stepP(p, dt, sway) {
  p.sw += .02 * dt; p.flip += .03 * dt; p.r += p.vr * dt;
  if (p.life > 0) { p.vy += (p.kind === 'snow' ? .03 : .06) * dt; p.vx *= .985; p.life -= .009 * dt; }
  p.x += (p.vx + Math.sin(p.sw) * sway) * dt; p.y += p.vy * dt;
}
const cv = $('petals'), ctx = cv.getContext('2d'), hero = $('hero'), heroArt = hero.querySelector('.hero-art'), heroIn = hero.querySelector('.hero-in');
let W = 0, H = 0, FH = 0, ART = { x: 0, y: 0, w: 0, h: 0 }, parts = [], sparks = [], nextFw = performance.now() + 700, running = false, visible = true, last = 0;
// FH: meddig hullanak a díszek. Telefonon a kép van felül, a szöveg alatta – ott csak a képen hullanak, hogy ne takarják a szöveget.
function resize() {
  const dpr = Math.min(devicePixelRatio || 1, 2); W = hero.clientWidth; H = hero.clientHeight; cv.width = W * dpr; cv.height = H * dpr; ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ART = { x: heroArt.offsetLeft + heroIn.offsetLeft, y: heroArt.offsetTop + heroIn.offsetTop, w: heroArt.offsetWidth, h: heroArt.offsetHeight };
  FH = innerWidth < 760 ? ART.y + ART.h : H;
}
// tűzijáték (szilveszter): rakéta a kép egén, majd szikraeső
const FW = ['#ffd25a', '#ff5a4e', '#ffffff', '#5ad1c8', '#ff9f43', '#c38bff'];
function explode(x, y, n = 70) {
  const c = FW[(Math.random() * FW.length) | 0];
  for (let i = 0; i < n; i++) { const a = Math.random() * 6.28, v = 1.6 + Math.random() * 3.4; sparks.push({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v, life: 1, c: Math.random() < .2 ? '#fff' : c }); }
}
function rocket() { sparks.push({ rk: 1, x: ART.x + ART.w * (.2 + Math.random() * .6), y: ART.y + ART.h * .8, vx: (Math.random() - .5) * .5, vy: -(5.2 + Math.random() * 1.8), ty: ART.y + ART.h * (.1 + Math.random() * .28), life: 1, c: '#ffe7a8' }); }
function drawSparks(dt) {
  for (let i = sparks.length - 1; i >= 0; i--) {
    const k = sparks[i];
    if (k.rk) { k.vy += .05 * dt; k.x += k.vx * dt; k.y += k.vy * dt; if (k.y <= k.ty || k.vy > -1) { sparks.splice(i, 1); explode(k.x, k.y); continue; } }
    else { k.vx *= .975; k.vy = k.vy * .975 + .04 * dt; k.x += k.vx * dt; k.y += k.vy * dt; k.life -= .0105 * dt; if (k.life <= 0) { sparks.splice(i, 1); continue; } }
    const a = k.rk ? 1 : Math.max(0, k.life), r = k.rk ? 2.2 : 1 + 1.8 * k.life;
    ctx.strokeStyle = ctx.fillStyle = k.c; ctx.lineWidth = k.rk ? 2 : 1.8;
    ctx.globalAlpha = a * .22; ctx.beginPath(); ctx.arc(k.x, k.y, r * 3.2, 0, 6.29); ctx.fill();
    ctx.globalAlpha = a; ctx.beginPath(); ctx.moveTo(k.x - k.vx * 4.5, k.y - k.vy * 4.5); ctx.lineTo(k.x, k.y); ctx.stroke();
    ctx.beginPath(); ctx.arc(k.x, k.y, r, 0, 6.29); ctx.fill();
  }
  ctx.globalAlpha = 1;
}
function frame(t) {
  if (!running) return;
  const dt = Math.min(3, (t - last) / 16.7 || 1); last = t;
  ctx.clearRect(0, 0, W, H);
  if (SEASON === 'newyear') { if (t > nextFw) { rocket(); if (Math.random() < .35) setTimeout(rocket, 260); nextFw = t + 1200 + Math.random() * 2000; } drawSparks(dt); }
  for (let i = parts.length - 1; i >= 0; i--) {
    const p = parts[i];
    stepP(p, dt, p.kind === 'snow' ? .3 : .45);
    if (p.life > 0 ? p.life <= 0 || p.y > H + 20 : p.y > (FH < H ? FH - 10 : H + 20)) {
      if (p.life > 0) { parts.splice(i, 1); continue; }
      Object.assign(p, particle(pickOf(SZ.mix), Math.random() * W, -20));
    }
    drawP(ctx, p);
  }
  requestAnimationFrame(frame);
}
function start() { if (running || reduced || !visible || document.hidden) return; running = true; last = performance.now(); requestAnimationFrame(frame); }
function stop() { running = false; }
function burst(x, y, n) {
  if (reduced) return;
  const r = hero.getBoundingClientRect();
  if (y < r.top || y > r.bottom) { miniBurst(x, y, n); return; }
  for (let i = 0; i < n; i++) parts.push(particle(pickOf(SZ.burst), x - r.left, y - r.top, 7));
  if (SEASON === 'newyear') explode(x - r.left, y - r.top, 36);
  start();
}
// a díszek a hősön kívül is kirobbanhatnak (pl. sorsolás, elküldött üzenet) – ehhez egy ideiglenes vászon kell
function miniBurst(x, y, n) {
  const c = document.createElement('canvas'); c.style.cssText = 'position:fixed;inset:0;width:100%;height:100%;pointer-events:none;z-index:70';
  (document.querySelector('dialog[open]') || document.body).appendChild(c);
  const dpr = Math.min(devicePixelRatio || 1, 2), w = innerWidth, h = innerHeight; c.width = w * dpr; c.height = h * dpr;
  const g = c.getContext('2d'); g.setTransform(dpr, 0, 0, dpr, 0, 0);
  const ps = Array.from({ length: n }, () => particle(pickOf(SZ.burst), x, y, 8)); let lt = performance.now();
  (function loop(t) {
    const dt = Math.min(3, (t - lt) / 16.7 || 1); lt = t; g.clearRect(0, 0, w, h); let alive = 0;
    for (const p of ps) { if (p.life <= 0) continue; alive++; stepP(p, dt, 0); p.life -= .003 * dt; drawP(g, p); }
    if (alive) requestAnimationFrame(loop); else c.remove();
  })(lt);
}
// karácsonyi fényfüzér a fejléc alján (pointer-events nélkül, semmit nem takar le)
const BULBS = ['#e0312b', '#f4c542', '#2f9e58', '#3d8bd9', '#f08a24'];
const bulb = (x, y, rot, k, big) => `<g class="bulb" transform="translate(${x.toFixed(1)} ${y.toFixed(1)}) rotate(${rot.toFixed(1)})" style="color:${BULBS[k % BULBS.length]};--d:${(1.6 + Math.random() * 1.8).toFixed(2)}s;--dl:-${(Math.random() * 3).toFixed(2)}s"><rect x="-1.7" y="-.6" width="3.4" height="3" rx=".8" fill="#34463a"/><ellipse cy="${big ? 6.2 : 5.4}" rx="${big ? 3.3 : 2.8}" ry="${big ? 4.5 : 3.9}" fill="currentColor"/></g>`;
function garland() {
  const g = document.createElement('div'); g.className = 'garland'; g.setAttribute('aria-hidden', 'true'); top.appendChild(g);
  const big = SEASON === 'christmas2';
  const draw = () => {
    const w = top.clientWidth, sag = big ? 12 : 9, n = Math.max(3, Math.round(w / (big ? 66 : 96))), sw = w / n, hh = sag * 2 + 16;
    let d = 'M0 1', b = '', k = 0;
    for (let i = 0; i < n; i++) {
      const x0 = i * sw, cx = x0 + sw / 2, x1 = x0 + sw, cy = 1 + sag * 2; d += ` Q${cx.toFixed(1)} ${cy} ${x1.toFixed(1)} 1`;
      for (const t of big ? [.22, .5, .78] : [.3, .7]) {
        const x = (1 - t) * (1 - t) * x0 + 2 * (1 - t) * t * cx + t * t * x1, y = (1 - t) * (1 - t) + 2 * (1 - t) * t * cy + t * t;
        const ang = Math.atan2(2 * (1 - t) * (cy - 1) + 2 * t * (1 - cy), sw) * 180 / Math.PI;
        b += bulb(x, y, ang * .5, k++, big);
      }
    }
    g.innerHTML = `<svg width="${w}" height="${hh}" viewBox="0 0 ${w} ${hh}"><path d="${d}"/>${b}</svg>`;
  };
  draw(); addEventListener('resize', draw);
}
// dec. 23–25.: fényfüzér a pipacsos kép íve körül is
function archLights() {
  const box = document.createElement('div'); box.className = 'arch-lights'; box.setAttribute('aria-hidden', 'true'); hero.appendChild(box);
  const draw = () => {
    const w = heroArt.offsetWidth, h = heroArt.offsetHeight; if (!w) return;
    const cs = getComputedStyle(heroArt), parts2 = cs.borderTopLeftRadius.split(' ');
    const val = (v, ref) => v && v.endsWith('%') ? parseFloat(v) / 100 * ref : parseFloat(v);
    const rx = val(parts2[0], w) || w / 2, ry = val(parts2[1] || parts2[0], h) || h * .3, pad = 8, bottom = h - 26;
    const pts = [];
    for (let y = bottom; y > ry; y -= 3) pts.push([-pad, y]);
    for (let a = Math.PI; a <= 2 * Math.PI + .001; a += .008) pts.push([w / 2 + (rx + pad) * Math.cos(a), ry + (ry + pad) * Math.sin(a)]);
    for (let y = ry; y < bottom; y += 3) pts.push([w + pad, y]);
    let path = '', b = '', acc = 0, k = 0, step = innerWidth < 760 ? 26 : 30;
    pts.forEach(([x, y], i) => {
      path += (i ? 'L' : 'M') + (x + 20).toFixed(1) + ' ' + (y + 20).toFixed(1);
      if (i) acc += Math.hypot(x - pts[i - 1][0], y - pts[i - 1][1]);
      if (i === 0 || acc >= step) { acc = 0; b += bulb(x + 20, y + 20, 0, k++, false); }
    });
    box.style.cssText = `left:${ART.x - 20}px;top:${ART.y - 20}px;width:${w + 40}px;height:${h + 40}px`;
    box.innerHTML = `<svg width="${w + 40}" height="${h + 40}"><path d="${path}"/>${b}</svg>`;
  };
  draw(); addEventListener('resize', draw); addEventListener('load', draw);
}
resize();
if (SEASON === 'christmas' || SEASON === 'christmas2') garland();
if (SEASON === 'christmas2') archLights();
addEventListener('resize', resize); addEventListener('load', resize);
if (!reduced) {
  const n = SZ.n[innerWidth < 760 ? 1 : 0];
  for (let i = 0; i < n; i++) parts.push(particle(pickOf(SZ.mix), Math.random() * W, Math.random() * FH));
  new IntersectionObserver(([e]) => { visible = e.isIntersecting; visible ? start() : stop(); }).observe(hero);
  document.addEventListener('visibilitychange', () => document.hidden ? stop() : start());
  hero.addEventListener('pointerdown', e => { if (tgt(e)?.closest('a,button')) return; burst(e.clientX, e.clientY, 8); $('heroHint').classList.remove('show'); });
  setTimeout(() => $('heroHint').classList.add('show'), 3500);
  setTimeout(() => $('heroHint').classList.remove('show'), 11000);
  start();
}
// a téma köszöntője a hős részben, és a koppintásra hívó felirat
function renderSeason() {
  const k = { christmas: 'seasonXmas', christmas2: 'seasonXmas2', newyear: 'seasonNY' }[SEASON], el = $('seasonNote');
  const d = new Date(); el.hidden = !k; if (k) el.textContent = T(k, { y: d.getFullYear() + (d.getMonth() >= 6 ? 1 : 0) });
  const hk = { autumn: 'hintAutumn', winter: 'hintSnow', christmas: 'hintXmas', christmas2: 'hintXmas', newyear: 'hintNY' }[SEASON];
  if (hk) $('heroHint').textContent = T(hk);
}
{ const m = document.querySelector('meta[name=theme-color]'), c = getComputedStyle(document.documentElement).getPropertyValue('--paper').trim(); if (m && c) m.content = c; }

/* ---------------------------------------------------------------- finom megjelenések
   A címsorok évszakos dísze és a képek csak akkor „érkeznek meg”, amikor a képernyőre görgetsz. */
const seenIO = 'IntersectionObserver' in window ? new IntersectionObserver(es => es.forEach(e => { if (e.isIntersecting) { e.target.classList.add(e.target.matches('h2') ? 'seen' : 'in'); seenIO.unobserve(e.target); } }), { rootMargin: '0px 0px -10% 0px' }) : null;
$$('main h2, .unveil').forEach((el, i) => seenIO ? seenIO.observe(el) : el.classList.add('seen', 'in'));
$$('.unveil').forEach((el, i) => el.style.setProperty('--i', i % 6));
// kosárba: a gomb „átrepül” a rendelés gombhoz, aztán megyünk a rendelési oldalra
function flyToCart(from) {
  if (reduced || !from.animate) return 0;
  const target = [document.querySelector('.dock.show .btn-red'), document.querySelector('.btn-order')].find(e => e && e.getBoundingClientRect().width);
  const a = from.getBoundingClientRect(), b = target ? target.getBoundingClientRect() : { left: innerWidth / 2 - 15, top: innerHeight - 46, width: 30, height: 30 };
  const dot = document.createElement('div'); dot.className = 'fly'; dot.innerHTML = plus; document.body.appendChild(dot);
  const sx = a.left + a.width / 2 - 15, sy = a.top + a.height / 2 - 15, ex = b.left + b.width / 2 - 15, ey = b.top + b.height / 2 - 15;
  const mx = sx + (ex - sx) * .45, my = Math.min(sy, ey) - 90;
  dot.animate([{ transform: `translate(${sx}px,${sy}px) scale(.6)` }, { transform: `translate(${sx}px,${sy}px) scale(1.15)`, offset: .12 },
    { transform: `translate(${mx}px,${my}px) scale(1)`, offset: .55 }, { transform: `translate(${ex}px,${ey}px) scale(.45)`, opacity: .7 }],
    { duration: 640, easing: 'cubic-bezier(.4,0,.3,1)' }).onfinish = () => { dot.remove(); target?.animate([{ transform: 'scale(1)' }, { transform: 'scale(1.1)' }, { transform: 'scale(1)' }], { duration: 280 }); };
  return 660;
}

/* ---------------------------------------------------------------- süti hozzájárulás */
// vv_consent: a döntés (1/0) – ez maga szükséges süti, enélkül minden alkalommal kérdeznénk.
// vv_id: véletlen kód, csak elfogadás után; a rendelési oldal is ezt használja.
const getCookie = n => (document.cookie.split('; ').find(c => c.startsWith(n + '=')) || '').split('=')[1] || '';
const secureAttr = location.protocol === 'https:' ? '; Secure' : '';
const setCookie = (n, v, days) => { document.cookie = `${n}=${v}; Max-Age=${days * 86400}; Path=/; SameSite=Lax${secureAttr}`; };
function setConsent(yes) {
  setCookie('vv_consent', yes ? '1' : '0', 390);
  if (yes) { if (!/^[0-9a-f]{32}$/.test(getCookie('vv_id'))) { const b = crypto.getRandomValues(new Uint8Array(16)); setCookie('vv_id', [...b].map(x => x.toString(16).padStart(2, '0')).join(''), 390); } }
  else { setCookie('vv_id', '', 0); try { localStorage.removeItem('vv_me'); } catch (e) {} }   // a rendelésnél megjegyzett adatokat is töröljük
  $('consent').hidden = true; document.body.classList.remove('ck-open');
  if ($('ckInfo').open) $('ckInfo').close();
  track('ev', { n: yes ? 'consent_yes' : 'consent_no' });
  toast(T(yes ? 'ckSavedYes' : 'ckSavedNo'));
}
function showConsent() { $('consent').hidden = false; document.body.classList.add('ck-open'); }
$('ckYes').addEventListener('click', () => setConsent(true));
$('ckNo').addEventListener('click', () => setConsent(false));
$$('#ckInfo [data-ck]').forEach(b => b.addEventListener('click', () => setConsent(b.dataset.ck === '1')));
$('ckMore').addEventListener('click', () => openModal($('ckInfo')));
$('cookieSettings').addEventListener('click', () => openModal($('ckInfo')));
if (!getCookie('vv_consent')) setTimeout(showConsent, 1200);   // a nyitó animáció után

/* ---------------------------------------------------------------- indulás */
$('year').textContent = new Date().getFullYear();
setLang(pickLang(), false);
setTimeout(offerLang, 1200);
// oldal megnyitása, életjel (élő látogatók), megnézett szakaszok, kattintások
// minden jelzés magával viszi, melyik részt nézi éppen, és meddig görgetett le
window.vvSec = 'hero'; window.vvScroll = 0;
const scrollPct = () => { const h = document.documentElement.scrollHeight - innerHeight; return h > 0 ? Math.min(100, Math.round(scrollY / h * 100)) : 100; };
addEventListener('scroll', () => { const p = scrollPct(); if (p > window.vvScroll) window.vvScroll = p; }, { passive: true });
const pingNow = () => track('ping');
track('view', { r: document.referrer, q: location.search, w: screen.width, h: screen.height, dk: matchMedia('(prefers-color-scheme: dark)').matches,
  c: (navigator.connection && navigator.connection.effectiveType) || '', bl: navigator.language || '' });
setInterval(() => { if (!document.hidden) pingNow(); }, 20000);
document.addEventListener('visibilitychange', pingNow);   // eltűnéskor is: így pontos, meddig volt az oldalon
addEventListener('pagehide', pingNow);
const seenSec = new Set();
// a képernyő közepén áthaladó szakasz számít (a hosszú étlap is); ez az "éppen nézett" rész is
const secObs = new IntersectionObserver(es => es.forEach(en => { if (!en.isIntersecting) return; const id = en.target.id; window.vvSec = id; window.vvScroll = Math.max(window.vvScroll, scrollPct()); if (!seenSec.has(id)) { seenSec.add(id); if (id !== 'hero') track('sec', { n: id }); } }), { rootMargin: '-45% 0px -45% 0px' });
['hero', 'ajanlat', 'rolunk', 'etlap', 'rendezvenyek', 'kiszallitas', 'galeria', 'kapcsolat'].forEach(id => $(id) && secObs.observe($(id)));
document.addEventListener('click', e => {
  const a = tgt(e)?.closest('a,button'); if (!a) return;
  const h = a.getAttribute('href') || '';
  if (a.id === 'wheelOrder') track('ev', { n: 'wheel_order', v: a.dataset.id || '' });
  else if (a.id === 'spin') track('ev', { n: 'wheel' });
  else if (h.startsWith('tel:')) track('ev', { n: 'call' });
  else if (h.includes('google.com/maps')) track('ev', { n: 'route' });
  else if (h.includes('facebook.com')) track('ev', { n: 'fb' });
  else if (h.startsWith('/rendeles') && !a.matches('.add,.add-v') && !h.includes('city=')) track('ev', { n: 'order_btn' });
  else if (a.dataset.tab === 'drinks') track('ev', { n: 'drinks' });
  if (a.closest('.quick')) track('ev', { n: 'quick', v: h });
}, true);
// ha egy ajánlat sincs (se napi, se hétvégi, se alkalmi), az Ajánlataink rész és a menüpontja eltűnik
function syncOffers() {
  const any = ['offPre', 'nyeremeny', 'offWeekend', 'kocsonya', 'offWeekly'].some(id => !$(id).hidden);
  if (!any && !offersSettled) return;
  $('ajanlat').hidden = !any; $$('a[href="#ajanlat"]:not(.hero-pre)').forEach(a => { a.hidden = !any; });
}
Promise.allSettled([loadMenu(), loadOffers(), loadPre(), loadGw()]).then(() => { offersSettled = true; syncOffers(); });
requestAnimationFrame(() => requestAnimationFrame(() => document.documentElement.classList.add('ready')));
})();
