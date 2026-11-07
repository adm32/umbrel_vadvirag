// Vadvirág online rendelés – a vendégek telefonjára tehető alkalmazás háttérszála.
// Mindig a hálózatról tölt (az étlap és az árak így sosem régiek); ha nincs internet,
// egy rövid üzenetet mutat a telefonszámmal.
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));
self.addEventListener('fetch', e => {
  if (e.request.mode !== 'navigate') return;
  e.respondWith(fetch(e.request).catch(() => new Response(
    '<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Vadvirág</title>' +
    '<body style="margin:0;min-height:100vh;display:grid;place-items:center;background:#f4efe4;font:17px/1.45 system-ui,sans-serif;color:#2a2823;text-align:center;padding:24px">' +
    '<div><h1 style="font:italic 600 28px Georgia,serif;color:#c4140a;margin:0 0 10px">Nincs internetkapcsolat</h1>' +
    '<p>Ha visszajön a net, próbáld újra.<br>Addig telefonon is rendelhetsz:</p>' +
    '<p><a href="tel:+36203395099" style="display:inline-block;margin-top:6px;padding:14px 22px;border-radius:14px;background:#c4140a;color:#fff;font-weight:700;text-decoration:none">06 20 339 5099</a></p>' +
    '<p><button onclick="location.reload()" style="margin-top:8px;padding:10px 18px;border-radius:12px;border:1.5px solid #c4140a;background:none;color:#c4140a;font:inherit;font-weight:600">Újrapróbálom</button></p></div>',
    { headers: { 'Content-Type': 'text/html; charset=utf-8' } })));
});
