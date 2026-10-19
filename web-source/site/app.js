/* Vadvirág Étterem – weboldal */
(() => {
'use strict';
document.documentElement.classList.add('js');
const $ = id => document.getElementById(id);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const huf = n => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
const norm = s => String(s || '').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
const store = { get(k){ try { return localStorage.getItem(k) } catch(e){ return null } }, set(k,v){ try { localStorage.setItem(k,v) } catch(e){} } };
const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
const PHONE = '06 20 339 5099';
const HOURS = { open: 11, close: 22 };                 // minden nap 11:00–22:00
const LANGS = ['hu', 'en', 'de'];

let lang = 'hu', M = null, TR = { sections:{}, items:{} }, DR = [], tab = 'food', kind = 'asztal';
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
  renderDrinks(); renderAlg();
}
$$('[data-lang]').forEach(b => b.addEventListener('click', () => { setLang(b.dataset.lang, true); closeDrawer(); }));

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
document.addEventListener('focusin', e => { if (e.target.matches('input,textarea,select')) { typing = true; $('dock').classList.remove('show'); } });
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
['etlap', 'rendezvenyek', 'kiszallitas', 'galeria', 'kapcsolat'].forEach(id => spy.observe($(id)));

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
  if (min >= o && min < c) { el.classList.add('open'); tx.textContent = c - min <= 45 ? T('closingSoon', { t: hh(HOURS.close) }) : T('openNow', { t: hh(HOURS.close) }); }
  else { el.classList.add('closed'); tx.textContent = min < o ? T('closedNow', { t: hh(HOURS.open) }) : T('closedTomorrow', { t: hh(HOURS.open) }); }
  if (M && M.open === false && el.classList.contains('open')) tx.textContent += ' · ' + T('orderPaused');
  if ($('hoursToday')) $('hoursToday').textContent = tx.textContent;
}
function renderHours() {
  const { wd } = budapestNow();
  $('hoursList').innerHTML = T('days').map((d, i) => `<li class="${i === wd ? 'today' : ''}"><span data-today="${esc(T('today'))}">${esc(d)}</span><span>${hh(HOURS.open)}–${hh(HOURS.close)}</span></li>`).join('');
}
setInterval(renderStatus, 60000);

/* ---------------------------------------------------------------- étlap */
const trSec = s => (lang !== 'hu' && TR.sections[s.id] && TR.sections[s.id][lang]) || s.name;
const trSecShort = s => (lang !== 'hu' && TR.sections[s.id] && TR.sections[s.id][lang]) || s.short || s.name;
const trItem = it => { const t = lang !== 'hu' && TR.items[it.id] && TR.items[it.id][lang]; return { name: (t && t[0]) || it.name, desc: (t && t[0]) ? (t[1] || '') : (it.desc || '') }; };
const variants = it => it.variants && it.variants.length ? it.variants : [{ label: '', price: it.price }];
const orderHref = (id, v) => '/rendeles?add=' + encodeURIComponent(id) + (v ? '&v=' + encodeURIComponent(v) : '');
const plus = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>';

async function loadMenu() {
  try {
    const [m, tr, dr] = await Promise.all([
      fetch('/api/menu', { cache: 'no-store' }).then(r => { if (!r.ok) throw 0; return r.json(); }),
      fetch('/data/menu-i18n.json').then(r => r.json()).catch(() => ({ sections:{}, items:{} })),
      fetch('/data/drinks.json').then(r => r.json()).catch(() => [])
    ]);
    if (!m.sections) throw 0;
    M = m; TR = tr; DR = dr;
  } catch (e) { $('food').innerHTML = `<p class="empty">${esc(T('menuError'))}</p>`; return; }
  renderChips(); renderFood(); renderDrinks(); renderFees(); renderFacts(); renderStatus(); buildWheelFilters();
  const opts = M.fees.map(f => `<option value="${esc(f.name)}">`).join(''); $('towns').innerHTML = opts;
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
$('chips').addEventListener('click', e => { const b = e.target.closest('[data-go]'); if (b) goSection(b.dataset.go); });
$('etlap').addEventListener('click', e => { const b = e.target.closest('.pager [data-go]'); if (b) goSection(b.dataset.go); });

function itemHTML(it) {
  const { name, desc } = trItem(it);
  const vs = variants(it), paused = M && M.open === false;
  const al = it.alg ? `<div class="al">${esc(T('allergens'))}: ${it.alg.split(/\s*,\s*/).map(n => `<button data-alg="${esc(n)}">${esc(n)}</button>`).join(', ')}</div>` : '';
  const ds = desc ? `<div class="ds">${esc(desc)}</div>` : '';
  const sup = it.alg ? `<span class="als">${it.alg.split(/\s*,\s*/).map(n => `<button data-alg="${esc(n)}" aria-label="${esc(T('allergens'))} ${esc(n)}">${esc(n)}</button>`).join('')}</span>` : '';
  if (vs.length > 1) {
    const pills = vs.map(v => paused
      ? `<span class="var">${esc(v.label)} <b>${huf(v.price)} Ft</b></span>`
      : `<a class="var add-v" href="${orderHref(it.id, v.label)}" aria-label="${esc(T('addToCart', { n: name + ' ' + v.label }))}">${esc(v.label)} <b>${huf(v.price)} Ft</b>${plus}</a>`).join('');
    return `<div class="mi mv" id="i-${esc(it.id)}"><div class="nm"><span>${esc(name)}${sup}</span></div>${ds}<div class="vars">${pills}</div>${al}</div>`;
  }
  const btn = paused ? '' : `<a class="add" href="${orderHref(it.id, vs[0].label)}" aria-label="${esc(T('addToCart', { n: name }))}">${plus}</a>`;
  return `<div class="mi" id="i-${esc(it.id)}"><div class="nm"><span>${esc(name)}${sup}</span></div><div class="pr">${huf(vs[0].price)} Ft ${btn}</div>${ds}${al}</div>`;
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
    html += `<div class="msec${q ? '' : ' one'}" id="s-${esc(s.id)}"><h3>${esc(trSec(s))}</h3><div class="items">${items.map(itemHTML).join('')}</div></div>`;
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
  const a = e.target.closest('a.add, a.add-v'); if (!a) return;
  e.preventDefault(); toast(T('added'));
  a.closest('.mi')?.classList.add('hit');
  burst(e.clientX, e.clientY, 6);
  setTimeout(() => { location.href = a.href; }, reduced ? 0 : 380);
});

/* ---------------------------------------------------------------- allergének */
function renderAlg(hl) { $('algList').innerHTML = T('alg').map((a, i) => `<li class="${hl && hl.includes(String(i + 1)) ? 'hl' : ''}">${esc(a)}</li>`).join(''); }
$('algBtn').addEventListener('click', () => { renderAlg(); openModal($('alg')); });
document.addEventListener('click', e => {
  const b = e.target.closest('[data-alg]'); if (!b) return;
  const all = [...b.parentElement.querySelectorAll('[data-alg]')].map(x => x.dataset.alg);
  e.preventDefault();
  renderAlg(all); openModal($('alg'));
});

/* ---------------------------------------------------------------- modális ablakok */
$$('dialog').forEach(d => {
  d.addEventListener('click', e => { if (e.target === d || e.target.closest('[data-close]')) d.close(); });
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
$('wheelFilters').addEventListener('click', e => { const b = e.target.closest('[data-ws]'); if (!b || spinning) return; wheelSec = b.dataset.ws; buildWheelFilters(); });
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
      const o = $('wheelOrder'); o.href = orderHref(pick.id, vs[0].label); o.hidden = M.open === false;
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
  const fees = [...M.fees].sort((a, b) => a.fee - b.fee || a.name.localeCompare(b.name, 'hu'));
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
    out.innerHTML = `<div class="del-card yes"><span class="van">${van}</span><div class="tx"><b>${esc(T('delYes'))}</b><div class="fee">${esc(f.name)}: ${esc(T('delFee', { fee: huf(f.fee) }))}</div></div>${M.open === false ? '' : `<a class="btn btn-red" href="/rendeles?city=${encodeURIComponent(f.name)}">${esc(T('delOrder'))}</a>`}</div>`;
  } else if (!starts.length && q.length >= 3) {
    if (lastTown === '!' + q + lang) return; lastTown = '!' + q + lang;
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
gal.forEach((b, i) => b.addEventListener('click', () => { showImg(i); openModal($('lb')); }));
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
form.addEventListener('input', e => e.target.closest('.f')?.classList.remove('bad'));
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
    form.classList.add('sent');
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

/* ---------------------------------------------------------------- hulló pipacsszirmok */
const cv = $('petals'), ctx = cv.getContext('2d'), hero = $('hero');
let W = 0, H = 0, petals = [], running = false, visible = true, last = 0;
const COLORS = ['#c4140a', '#d9261b', '#a5100a', '#e0412f', '#b81d12'];
function resize() { const dpr = Math.min(devicePixelRatio || 1, 2); W = hero.clientWidth; H = hero.clientHeight; cv.width = W * dpr; cv.height = H * dpr; ctx.setTransform(dpr, 0, 0, dpr, 0, 0); }
function petal(x, y, burstV) {
  const s = 7 + Math.random() * 9;
  return { x, y, s, r: Math.random() * 6.28, vr: (Math.random() - .5) * .05, vx: burstV ? (Math.random() - .5) * burstV : (Math.random() - .3) * .5, vy: burstV ? -Math.random() * burstV * .8 : .35 + Math.random() * .6,
    sw: Math.random() * 6.28, c: COLORS[(Math.random() * COLORS.length) | 0], life: burstV ? 1 : -1, flip: Math.random() * 6.28 };
}
function drawPetal(p) {
  ctx.save(); ctx.translate(p.x, p.y); ctx.rotate(p.r); ctx.scale(1, Math.abs(Math.cos(p.flip)) * .7 + .3);
  ctx.globalAlpha = p.life > 0 ? Math.min(1, p.life * 1.5) : .9;
  ctx.fillStyle = p.c; ctx.beginPath();
  ctx.moveTo(0, -p.s); ctx.bezierCurveTo(p.s * .9, -p.s * .8, p.s * .9, p.s * .6, 0, p.s * .55); ctx.bezierCurveTo(-p.s * .9, p.s * .6, -p.s * .9, -p.s * .8, 0, -p.s);
  ctx.fill(); ctx.globalAlpha *= .25; ctx.fillStyle = '#3a0904'; ctx.beginPath(); ctx.ellipse(0, p.s * .3, p.s * .18, p.s * .12, 0, 0, 6.28); ctx.fill();
  ctx.restore();
}
function frame(t) {
  if (!running) return;
  const dt = Math.min(3, (t - last) / 16.7 || 1); last = t;
  ctx.clearRect(0, 0, W, H);
  for (let i = petals.length - 1; i >= 0; i--) {
    const p = petals[i];
    p.sw += .02 * dt; p.flip += .03 * dt; p.r += p.vr * dt;
    if (p.life > 0) { p.vy += .06 * dt; p.vx *= .985; p.life -= .009 * dt; }
    p.x += (p.vx + Math.sin(p.sw) * .45) * dt; p.y += p.vy * dt;
    if (p.life > 0 ? p.life <= 0 || p.y > H + 20 : p.y > H + 20) {
      if (p.life > 0) { petals.splice(i, 1); continue; }
      Object.assign(p, petal(Math.random() * W, -20));
    }
    drawPetal(p);
  }
  requestAnimationFrame(frame);
}
function start() { if (running || reduced || !visible || document.hidden) return; running = true; last = performance.now(); requestAnimationFrame(frame); }
function stop() { running = false; }
function burst(x, y, n) {
  if (reduced) return;
  const r = hero.getBoundingClientRect();
  if (y < r.top || y > r.bottom) { miniBurst(x, y, n); return; }
  for (let i = 0; i < n; i++) petals.push(petal(x - r.left, y - r.top, 7));
  start();
}
// a szirmok a hősön kívül is kirobbanhatnak (pl. sorsolás, elküldött üzenet) – ehhez egy ideiglenes vászon kell
function miniBurst(x, y, n) {
  const c = document.createElement('canvas'); c.style.cssText = 'position:fixed;inset:0;width:100%;height:100%;pointer-events:none;z-index:70';
  const host = document.querySelector('dialog[open]') || document.body; host.appendChild(c);
  const dpr = Math.min(devicePixelRatio || 1, 2), w = innerWidth, h = innerHeight; c.width = w * dpr; c.height = h * dpr;
  const g = c.getContext('2d'); g.setTransform(dpr, 0, 0, dpr, 0, 0);
  const ps = Array.from({ length: n }, () => petal(x, y, 8)); let lt = performance.now();
  (function loop(t) {
    const dt = Math.min(3, (t - lt) / 16.7 || 1); lt = t; g.clearRect(0, 0, w, h); let alive = 0;
    for (const p of ps) { if (p.life <= 0) continue; alive++; p.vy += .06 * dt; p.vx *= .985; p.life -= .012 * dt; p.flip += .05 * dt; p.r += p.vr * dt; p.x += p.vx * dt; p.y += p.vy * dt;
      g.save(); g.translate(p.x, p.y); g.rotate(p.r); g.scale(1, Math.abs(Math.cos(p.flip)) * .7 + .3); g.globalAlpha = Math.min(1, p.life * 1.5); g.fillStyle = p.c; g.beginPath();
      g.moveTo(0, -p.s); g.bezierCurveTo(p.s * .9, -p.s * .8, p.s * .9, p.s * .6, 0, p.s * .55); g.bezierCurveTo(-p.s * .9, p.s * .6, -p.s * .9, -p.s * .8, 0, -p.s); g.fill(); g.restore(); }
    if (alive) requestAnimationFrame(loop); else c.remove();
  })(lt);
}
if (!reduced) {
  resize();
  const n = innerWidth < 760 ? 3 : 6;
  for (let i = 0; i < n; i++) petals.push(petal(Math.random() * W, Math.random() * H));
  addEventListener('resize', resize);
  new IntersectionObserver(([e]) => { visible = e.isIntersecting; visible ? start() : stop(); }).observe(hero);
  document.addEventListener('visibilitychange', () => document.hidden ? stop() : start());
  hero.addEventListener('pointerdown', e => { if (e.target.closest('a,button')) return; burst(e.clientX, e.clientY, 8); document.querySelector('.hero-hint').classList.remove('show'); });
  setTimeout(() => document.querySelector('.hero-hint').classList.add('show'), 3500);
  setTimeout(() => document.querySelector('.hero-hint').classList.remove('show'), 11000);
  start();
}

/* ---------------------------------------------------------------- indulás */
$('year').textContent = new Date().getFullYear();
setLang(pickLang(), false);
loadMenu();
requestAnimationFrame(() => requestAnimationFrame(() => document.documentElement.classList.add('ready')));
})();
