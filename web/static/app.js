// Tapis phone client. No dependencies, no network access other than the
// WebSocket to the computer that served this page.
'use strict';

const $ = (s) => document.querySelector(s);
const $$ = (s) => [...document.querySelectorAll(s)];

// ---------- i18n ----------
const STR = {
  en: {
    connecting: 'Connecting…', settings: 'Settings', speed: 'Pointer speed', accel: 'Acceleration',
    scroll: 'Scroll speed', natural: 'Natural scrolling', inertia: 'Scroll inertia', tap: 'Tap to click',
    haptic: 'Vibration feedback', fullscreen: 'Full screen', cert: 'Download certificate',
    language: 'Language', langAuto: 'Automatic (phone language)',
    autokbd: 'Open the keyboard when a text field is selected on the computer',
    forget: 'Forget this computer', done: 'Done', retry: 'Retry', about: 'Tapis · free software (GPL-3.0)',
    hint1: 'One finger: move · Tap: click', hint2: 'Two fingers: scroll · Two-finger tap: right click',
    hint3: 'Tap then hold and move: drag',
    notPairedT: 'Not paired', notPaired: 'Scan the QR code shown by Tapis on your computer.\nIn the Tapis window, type p then Enter to show a new code.',
    badPairT: 'Pairing failed', badPair: 'This code was already used or has expired.\nIn the Tapis window, type p then Enter, and scan the new code.',
    revokedT: 'Phone not paired anymore', revoked: 'This phone was unpaired from the computer.\nScan a new pairing QR code to reconnect.',
    offlineT: 'Computer unreachable', offline: 'Check that Tapis is running and that both devices are on the same Wi-Fi network.\nIf a VPN is active on this phone, allow local network access in its settings or turn it off.',
    forgetQ: 'Forget this computer? You will need to scan a QR code again.',
    tabPad: 'Trackpad', tabMedia: 'Media', tabSystem: 'Computer', holdHint: 'Hold a button to confirm',
    lock: 'Lock the computer', sleep: 'Put to sleep', autofs: 'Full screen automatically',
    lockDone: 'Computer locked', sleepDone: 'Computer going to sleep',
    confirmT: 'Confirm on the computer', confirm: 'Check that the computer shows the same code, then allow this phone there.',
    refusedT: 'Pairing refused', refused: 'The pairing was refused on the computer (or nobody answered within a minute).\nType p in the Tapis window to try again.',
  },
  fr: {
    connecting: 'Connexion…', settings: 'Réglages', speed: 'Vitesse du pointeur', accel: 'Accélération',
    scroll: 'Vitesse de défilement', natural: 'Défilement naturel', inertia: 'Inertie du défilement',
    tap: 'Toucher pour cliquer', haptic: 'Vibrations', fullscreen: 'Plein écran', cert: 'Télécharger le certificat',
    language: 'Langue', langAuto: 'Automatique (langue du téléphone)',
    autokbd: 'Ouvrir le clavier quand un champ de texte est sélectionné sur l’ordinateur',
    forget: 'Oublier cet ordinateur', done: 'OK', retry: 'Réessayer', about: 'Tapis · logiciel libre (GPL-3.0)',
    hint1: 'Un doigt : déplacer · Toucher : clic', hint2: 'Deux doigts : défiler · Toucher à deux doigts : clic droit',
    hint3: 'Toucher puis maintenir et glisser : glisser-déposer',
    notPairedT: 'Pas encore appairé', notPaired: 'Scanne le QR code affiché par Tapis sur ton ordinateur.\nDans la fenêtre Tapis, tape p puis Entrée pour afficher un nouveau code.',
    badPairT: 'Appairage refusé', badPair: 'Ce code a déjà servi ou a expiré.\nDans la fenêtre Tapis, tape p puis Entrée et scanne le nouveau code.',
    revokedT: 'Téléphone désappairé', revoked: 'Ce téléphone a été retiré de l’ordinateur.\nScanne un nouveau QR code pour te reconnecter.',
    offlineT: 'Ordinateur injoignable', offline: 'Vérifie que Tapis tourne et que les deux appareils sont sur le même Wi-Fi.\nSi un VPN est actif sur ce téléphone, autorise l’accès au réseau local dans ses réglages ou désactive-le.',
    forgetQ: 'Oublier cet ordinateur ? Il faudra rescanner un QR code.',
    tabPad: 'Trackpad', tabMedia: 'Médias', tabSystem: 'Ordinateur', holdHint: 'Maintiens un bouton pour confirmer',
    lock: 'Verrouiller l’ordinateur', sleep: 'Mettre en veille', autofs: 'Plein écran automatique',
    lockDone: 'Ordinateur verrouillé', sleepDone: 'Mise en veille…',
    confirmT: 'Confirme sur l’ordinateur', confirm: 'Vérifie que l’ordinateur affiche le même code, puis autorise ce téléphone là-bas.',
    refusedT: 'Appairage refusé', refused: 'L’appairage a été refusé sur l’ordinateur (ou personne n’a répondu en une minute).\nTape p dans la fenêtre Tapis pour réessayer.',
  },
};
const systemLang = () => ((navigator.language || 'en').toLowerCase().startsWith('fr') ? 'fr' : 'en');
let LANG = 'en';
const t = (k) => STR[LANG][k] ?? STR.en[k] ?? k;
let shownOverlay = null; // [titleKey, textKey] of the overlay on screen, to re-translate it
function applyLanguage(choice) {
  LANG = choice === 'en' || choice === 'fr' ? choice : systemLang();
  document.documentElement.lang = LANG;
  $$('[data-i18n]').forEach((el) => { el.textContent = t(el.dataset.i18n); });
  if (shownOverlay && !$('#overlay').hidden) {
    $('#ov-title').textContent = t(shownOverlay[0]);
    $('#ov-text').textContent = t(shownOverlay[1]);
  }
}
applyLanguage((() => { try { return localStorage.getItem('tapis.lang'); } catch { return null; } })());

// ---------- settings ----------
const DEFAULTS = { speed: 1.6, accel: 1, scroll: 1, natural: true, inertia: true, tap: true, autofs: true, autokbd: true, haptic: true };
const settings = { ...DEFAULTS, ...load('tapis.settings', {}) };
function load(k, d) { try { return JSON.parse(localStorage.getItem(k)) ?? d; } catch { return d; } }
function save(k, v) { try { localStorage.setItem(k, JSON.stringify(v)); } catch { /* private mode */ } }

for (const [k, v] of Object.entries(DEFAULTS)) {
  const el = $('#s-' + k);
  if (typeof v === 'boolean') el.checked = settings[k];
  else el.value = settings[k];
  el.addEventListener('input', () => {
    settings[k] = typeof v === 'boolean' ? el.checked : parseFloat(el.value);
    save('tapis.settings', settings);
  });
}

const langSelect = $('#s-lang');
try { langSelect.value = localStorage.getItem('tapis.lang') || 'auto'; } catch { langSelect.value = 'auto'; }
langSelect.addEventListener('change', () => {
  try { localStorage.setItem('tapis.lang', langSelect.value); } catch { /* private mode */ }
  applyLanguage(langSelect.value);
  if (!connected) $('#host').textContent = t('connecting');
});

const haptic = (ms = 8) => { if (settings.haptic && navigator.vibrate) navigator.vibrate(ms); };

// ---------- connection ----------
let ws = null;
let connected = false;
let fatal = false;
let retryDelay = 500;
let retryTimer = null;
let pairToken = null;

const m = location.hash.match(/pair=([A-Za-z0-9_-]+)/);
if (m) {
  pairToken = m[1];
  history.replaceState(null, '', location.pathname); // keep the token out of history
}

function overlay(titleKey, textKey, retry = true, code = '') {
  $('#ov-code').hidden = !code;
  $('#ov-code').textContent = code ? code.slice(0, 3) + ' ' + code.slice(3) : '';
  shownOverlay = titleKey ? [titleKey, textKey] : null;
  if (!titleKey) { $('#overlay').hidden = true; return; }
  $('#ov-title').textContent = t(titleKey);
  $('#ov-text').textContent = t(textKey);
  $('#ov-retry').hidden = !retry;
  $('#overlay').hidden = false;
}

let toastTimer = 0;
function toast(text) {
  const el = $('#toast');
  el.textContent = text;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 2500);
}

function setStatus(state, label) {
  $('#status').className = 'dot ' + (state === 'ok' ? 'ok' : state === 'bad' ? 'bad' : '');
  $('#host').textContent = label;
}

async function deviceName() {
  try {
    if (navigator.userAgentData?.getHighEntropyValues) {
      const h = await navigator.userAgentData.getHighEntropyValues(['model', 'platform']);
      if (h.model) return h.model;
      if (h.platform) return h.platform + ' phone';
    }
  } catch { /* ignore */ }
  const ua = navigator.userAgent;
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua)) return 'iPad';
  if (/Android/.test(ua)) return 'Android phone';
  return 'Browser';
}

function connect() {
  clearTimeout(retryTimer);
  if (ws && ws.readyState <= 1) return;
  const device = load('tapis.device', null);
  if (!pairToken && !device) {
    fatal = true;
    setStatus('bad', 'Tapis');
    overlay('notPairedT', 'notPaired');
    return;
  }
  setStatus('wait', t('connecting'));
  ws = new WebSocket(`wss://${location.host}/ws`);
  ws.onopen = async () => {
    retryDelay = 500;
    if (pairToken) {
      ws.send(JSON.stringify({ t: 'pair', token: pairToken, name: await deviceName() }));
      pairToken = null;
    } else {
      ws.send(JSON.stringify({ t: 'auth', id: device.id, key: device.key }));
    }
  };
  ws.onmessage = (ev) => {
    let msg;
    try { msg = JSON.parse(ev.data); } catch { return; }
    switch (msg.t) {
      case 'confirm':
        overlay('confirmT', 'confirm', false, /^\d{6}$/.test(msg.code || '') ? msg.code : '');
        break;
      case 'paired':
        save('tapis.device', { id: msg.id, key: msg.key });
        break;
      case 'media':
        showNowPlaying(msg);
        break;
      case 'focus':
        computerFocus(!!msg.edit);
        break;
      case 'sys':
        toast(t(msg.a === 'lock' ? 'lockDone' : 'sleepDone'));
        break;
      case 'ok':
        connected = true;
        fatal = false;
        overlay(null);
        setStatus('ok', msg.host || 'Tapis');
        requestWakeLock();
        sendPrefs();
        break;
      case 'error':
        fatal = true;
        if (msg.code === 'pairing') overlay('badPairT', 'badPair');
        else if (msg.code === 'refused') overlay('refusedT', 'refused');
        else if (msg.code === 'auth') { localStorage.removeItem('tapis.device'); overlay('revokedT', 'revoked'); }
        setStatus('bad', 'Tapis');
        break;
    }
  };
  ws.onclose = () => {
    connected = false;
    showNowPlaying(null);
    ws = null;
    if (fatal) return;
    setStatus('bad', t('connecting'));
    if (retryDelay > 4000) overlay('offlineT', 'offline');
    retryTimer = setTimeout(connect, retryDelay);
    retryDelay = Math.min(retryDelay * 2, 8000);
  };
}

function send(obj) {
  if (connected && ws && ws.readyState === 1) ws.send(JSON.stringify(obj));
}

$('#ov-retry').addEventListener('click', () => {
  fatal = false;
  retryDelay = 500;
  overlay(null);
  connect();
});
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') {
    if (!ws && !fatal) { retryDelay = 500; connect(); }
    requestWakeLock();
  } else {
    releaseAll();
  }
});

let wakeLock = null;
async function requestWakeLock() {
  try {
    if ('wakeLock' in navigator && !wakeLock && document.visibilityState === 'visible') {
      wakeLock = await navigator.wakeLock.request('screen');
      wakeLock.addEventListener('release', () => { wakeLock = null; });
    }
  } catch { /* not available without a trusted certificate */ }
}

// ---------- now playing ----------
function showNowPlaying(m) {
  const has = !!(m && m.title);
  $('#now').hidden = !has;
  $('#now-title').textContent = has ? m.title : '';
  $('#now-sub').textContent = has ? [m.artist, m.app].filter(Boolean).join(' · ') : '';
  const play = $('#view-media .tile.play');
  if (has) play.dataset.state = m.playing ? 'playing' : 'paused';
  else delete play.dataset.state;
}

// ---------- modifiers (one-shot on tap, locked on double tap) ----------
const mods = { ctrl: 0, alt: 0, meta: 0, shift: 0 }; // 0 off, 1 armed, 2 locked
const modLastTap = {};

function renderMods() {
  $$('[data-mod]').forEach((b) => {
    const s = mods[b.dataset.mod];
    b.classList.toggle('armed', s === 1);
    b.classList.toggle('locked', s === 2);
  });
}
function toggleMod(name) {
  const now = performance.now();
  const dbl = now - (modLastTap[name] || 0) < 350;
  modLastTap[name] = now;
  const prev = mods[name];
  if (prev === 2) { mods[name] = 0; send({ t: 'keyup', k: name }); }
  else if (prev === 1 && dbl) { mods[name] = 2; send({ t: 'keydown', k: name }); }
  else if (prev === 1) mods[name] = 0;
  else mods[name] = 1;
  renderMods();
  haptic();
}
const armedMods = () => Object.keys(mods).filter((k) => mods[k] === 1);
const anyMod = () => Object.values(mods).some((v) => v > 0);
function clearArmed() {
  let changed = false;
  for (const k in mods) if (mods[k] === 1) { mods[k] = 0; changed = true; }
  if (changed) renderMods();
}
function releaseAll() {
  for (const k in mods) {
    if (mods[k] === 2) send({ t: 'keyup', k });
    mods[k] = 0;
  }
  renderMods();
  if (dragging) { send({ t: 'btn', b: 'left', a: 'up' }); dragging = false; }
}

function sendKey(k, n = 1) {
  send({ t: 'key', k, m: armedMods(), n });
  clearArmed();
}

// ---------- trackpad ----------
const pad = $('#pad');
const touches = new Map(); // id -> {x, y}
let g = null; // current gesture
let lastTapEnd = -Infinity;
let dragging = false;
let rem = { x: 0, y: 0, sx: 0, sy: 0 };
let scrollVel = { x: 0, y: 0, t: 0 };
let inertiaFrame = 0;
let hintShown = true;

function stopInertia() { cancelAnimationFrame(inertiaFrame); inertiaFrame = 0; }

function movePointer(dx, dy, dt) {
  const v = Math.hypot(dx, dy) / Math.max(dt, 4); // px per ms
  const gain = settings.speed * (1 + settings.accel * Math.min(Math.max(v - 0.2, 0), 2.5) * 0.9);
  rem.x += dx * gain;
  rem.y += dy * gain;
  const ix = Math.trunc(rem.x), iy = Math.trunc(rem.y);
  rem.x -= ix; rem.y -= iy;
  if ((ix || iy) && ws && ws.bufferedAmount < 32768) send({ t: 'move', x: ix, y: iy });
}

function scrollBy(fdx, fdy) {
  // 120 units = one wheel notch, about 40 px of finger travel.
  const k = 3 * settings.scroll;
  const dir = settings.natural ? 1 : -1;
  rem.sx += -fdx * k * dir;
  rem.sy += fdy * k * dir;
  const ix = Math.trunc(rem.sx), iy = Math.trunc(rem.sy);
  rem.sx -= ix; rem.sy -= iy;
  if (ix || iy) send({ t: 'scroll', x: ix, y: iy });
}

function click(button) {
  const held = armedMods();
  held.forEach((k) => send({ t: 'keydown', k }));
  send({ t: 'btn', b: button, a: 'click' });
  held.forEach((k) => send({ t: 'keyup', k }));
  clearArmed();
  haptic();
}

pad.addEventListener('touchstart', (e) => {
  e.preventDefault();
  stopInertia();
  const now = performance.now();
  for (const tc of e.changedTouches) touches.set(tc.identifier, { x: tc.clientX, y: tc.clientY });
  if (!g) {
    g = { t0: now, last: now, max: touches.size, moved: 0, nearTap: now - lastTapEnd < 300 };
    pad.classList.add('active');
  } else {
    g.max = Math.max(g.max, touches.size);
  }
  if (hintShown) { $('#hint').classList.add('gone'); hintShown = false; }
}, { passive: false });

pad.addEventListener('touchmove', (e) => {
  e.preventDefault();
  if (!g) return;
  const now = performance.now();
  const dt = now - g.last;
  g.last = now;
  let sx = 0, sy = 0, n = 0;
  for (const tc of e.changedTouches) {
    const p = touches.get(tc.identifier);
    if (!p) continue;
    sx += tc.clientX - p.x;
    sy += tc.clientY - p.y;
    p.x = tc.clientX; p.y = tc.clientY;
    n++;
  }
  if (!n) return;
  const fingers = touches.size;
  // With several fingers, use the average movement of the moving ones.
  const dx = sx / n, dy = sy / n;
  g.moved += Math.abs(dx) + Math.abs(dy);
  if (fingers === 1) {
    if (g.nearTap && !dragging && g.moved > 6) {
      dragging = true;
      send({ t: 'btn', b: 'left', a: 'down' });
      haptic(12);
    }
    movePointer(dx, dy, dt);
  } else if (fingers === 2) {
    scrollBy(dx, dy);
    const a = 0.3; // smoothed velocity, finger px/ms
    scrollVel = { x: (1 - a) * scrollVel.x + a * dx / Math.max(dt, 4), y: (1 - a) * scrollVel.y + a * dy / Math.max(dt, 4), t: now };
  }
}, { passive: false });

function touchEnd(e) {
  e.preventDefault();
  for (const tc of e.changedTouches) touches.delete(tc.identifier);
  if (touches.size > 0 || !g) return;
  const now = performance.now();
  const dur = now - g.t0;
  pad.classList.remove('active');
  if (dragging) {
    dragging = false;
    send({ t: 'btn', b: 'left', a: 'up' });
    lastTapEnd = -Infinity;
  } else if (g.moved < 10 && dur < 260 && settings.tap) {
    click(g.max >= 3 ? 'middle' : g.max === 2 ? 'right' : 'left');
    lastTapEnd = g.max === 1 ? now : -Infinity;
  } else {
    lastTapEnd = -Infinity;
    if (g.max === 2 && settings.inertia && now - scrollVel.t < 60) startInertia();
  }
  scrollVel = { x: 0, y: 0, t: 0 };
  g = null;
}
pad.addEventListener('touchend', touchEnd, { passive: false });
pad.addEventListener('touchcancel', touchEnd, { passive: false });

function startInertia() {
  let { x: vx, y: vy } = scrollVel;
  if (Math.hypot(vx, vy) < 0.3) return;
  let last = performance.now();
  const step = (now) => {
    const dt = now - last;
    last = now;
    const decay = Math.pow(0.995, dt);
    vx *= decay; vy *= decay;
    scrollBy(vx * dt, vy * dt);
    if (Math.hypot(vx, vy) > 0.02) inertiaFrame = requestAnimationFrame(step);
    else inertiaFrame = 0;
  };
  inertiaFrame = requestAnimationFrame(step);
}

document.addEventListener('contextmenu', (e) => e.preventDefault());

// ---------- special keys bar ----------
for (const b of $$('#keys button, #view-media button')) {
  let repeatTimer = 0;
  const stopRepeat = () => { clearTimeout(repeatTimer); clearInterval(repeatTimer); repeatTimer = 0; };
  b.addEventListener('pointerdown', (e) => {
    e.preventDefault(); // keep the text field focused (keyboard stays open)
    if (b.dataset.mod) { toggleMod(b.dataset.mod); return; }
    if (b.dataset.combo) {
      const parts = b.dataset.combo.split('+');
      send({ t: 'key', k: parts.pop(), m: [...new Set([...parts, ...armedMods()])] });
      clearArmed();
      haptic();
      return;
    }
    const key = b.dataset.key;
    sendKey(key);
    haptic();
    if (b.hasAttribute('data-repeat')) {
      repeatTimer = setTimeout(() => { repeatTimer = setInterval(() => send({ t: 'key', k: key }), 60); }, 400);
    }
  });
  b.addEventListener('pointerup', stopRepeat);
  b.addEventListener('pointercancel', stopRepeat);
  b.addEventListener('pointerleave', stopRepeat);
}

// ---------- phone keyboard ----------
// The phone's own keyboard types into a hidden text field. We keep the
// field filled with invisible sentinel characters on both sides of the
// caret and translate every change into key presses:
//   - text inserted  -> typed text
//   - text removed   -> Backspace (or Delete if after the caret)
//   - caret moved    -> arrow keys (e.g. Gboard's space-bar swipe)
// Diffing the whole value is what makes predictive / swipe keyboards work.
const kbd = $('#kbd');
const SENT = '​'.repeat(16);
let prevValue = '';
let caret = 0;
let composing = false;

function resetField() {
  kbd.value = SENT + SENT;
  prevValue = kbd.value;
  caret = SENT.length;
  try { kbd.setSelectionRange(caret, caret); } catch { /* not focused */ }
}
resetField();

function emitText(s) {
  if (!s) return;
  const parts = s.split('\n');
  parts.forEach((part, i) => {
    if (i > 0) sendKey('Enter');
    if (!part) return;
    if (anyMod()) {
      // With modifiers held, send real key presses so shortcuts work.
      for (const ch of part) sendKey(ch);
    } else {
      send({ t: 'text', s: part });
    }
  });
}

function syncField() {
  const cur = kbd.value;
  if (cur === prevValue) return;
  const a = [...prevValue], b = [...cur];
  let p = 0;
  while (p < a.length && p < b.length && a[p] === b[p]) p++;
  let s = 0;
  while (s < a.length - p && s < b.length - p && a[a.length - 1 - s] === b[b.length - 1 - s]) s++;
  const removed = a.length - p - s;
  const inserted = b.slice(p, b.length - s).join('').replace(/​/g, '');
  const prevCaretCp = [...prevValue.slice(0, caret)].length;
  if (removed > 0) {
    if (p >= prevCaretCp) sendKey('Delete', removed);
    else sendKey('Backspace', removed);
  }
  emitText(inserted);
  prevValue = cur;
  caret = kbd.selectionStart ?? cur.length;
  if (!composing) maybeReset();
}

function maybeReset() {
  const v = kbd.value;
  const before = v.slice(0, kbd.selectionStart);
  const after = v.slice(kbd.selectionEnd);
  const lead = before.length - before.replace(/^​+/, '').length;
  const trail = after.length - after.replace(/​+$/, '').length;
  if (v.length > 64 || lead < 4 || trail < 4) resetField();
}

kbd.addEventListener('compositionstart', () => { composing = true; });
kbd.addEventListener('compositionend', () => { composing = false; setTimeout(syncField, 0); });
kbd.addEventListener('input', syncField);

kbd.addEventListener('keydown', (e) => {
  if (e.isComposing || e.keyCode === 229) return; // handled by the input diff
  const named = ['Enter', 'Backspace', 'Tab', 'Escape', 'ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown',
    'Delete', 'Home', 'End', 'PageUp', 'PageDown'];
  if (named.includes(e.key)) {
    e.preventDefault();
    sendKey(e.key);
  } else if ((e.ctrlKey || e.altKey || e.metaKey) && e.key.length === 1) {
    // Hardware keyboard attached to the phone.
    e.preventDefault();
    const m2 = [e.ctrlKey && 'ctrl', e.altKey && 'alt', e.metaKey && 'meta', e.shiftKey && 'shift'].filter(Boolean);
    send({ t: 'key', k: e.key.toLowerCase(), m: m2 });
  }
});

document.addEventListener('selectionchange', () => {
  if (document.activeElement !== kbd || composing || kbd.value !== prevValue) return;
  const st = kbd.selectionStart, en = kbd.selectionEnd;
  if (st !== en) { kbd.setSelectionRange(caret, caret); return; }
  if (st === caret) return;
  const delta = [...kbd.value.slice(0, st)].length - [...kbd.value.slice(0, caret)].length;
  sendKey(delta < 0 ? 'ArrowLeft' : 'ArrowRight', Math.abs(delta));
  resetField();
});

function setKeyboard(open) {
  document.body.classList.toggle('kbd-open', open);
  $('#keys').hidden = !open;
  $('#btn-kbd').classList.toggle('on', open);
}
kbd.addEventListener('focus', () => { resetField(); setKeyboard(true); });
kbd.addEventListener('blur', () => setKeyboard(false));

$('#btn-kbd').addEventListener('pointerdown', (e) => e.preventDefault()); // don't steal focus
$('#btn-kbd').addEventListener('click', () => {
  autoOpened = false;
  if (document.activeElement === kbd) kbd.blur();
  else { showTab('pad'); kbd.focus(); resetField(); }
});

// ---------- open the keyboard when a text field is focused on the computer ----------
// The computer reports whether its focused element accepts text. Browsers
// only let a page show the keyboard shortly after a touch, which is the
// case here: the field was just clicked from the trackpad.
let autoOpened = false;
function sendPrefs() { send({ t: 'prefs', focus: !!settings.autokbd }); }
$('#s-autokbd').addEventListener('change', sendPrefs);
let lastTouch = 0;
document.addEventListener('pointerup', () => { lastTouch = performance.now(); }, { capture: true });
function computerFocus(edit) {
  if (!settings.autokbd || $('#settings').open) return;
  // Only right after a touch on the phone: the field was just clicked.
  if (edit && document.activeElement !== kbd && performance.now() - lastTouch < 4000) {
    showTab('pad');
    kbd.focus({ preventScroll: true });
    resetField();
    autoOpened = true;
  } else if (!edit && autoOpened && document.activeElement === kbd) {
    kbd.blur();
    autoOpened = false;
  }
}

// ---------- tabs ----------
function showTab(name) {
  for (const b of $$('#tabs [data-tab]')) b.setAttribute('aria-selected', String(b.dataset.tab === name));
  for (const v of $$('.view')) v.hidden = v.id !== 'view-' + name;
  save('tapis.tab', name);
}
for (const b of $$('#tabs [data-tab]')) b.addEventListener('click', () => { showTab(b.dataset.tab); haptic(); });
showTab(load('tapis.tab', 'pad'));

// ---------- computer actions (hold to confirm) ----------
const HOLD_MS = 800;
for (const b of $$('[data-sys]')) {
  let start = 0, frame = 0;
  const reset = () => { cancelAnimationFrame(frame); start = 0; b.style.setProperty('--p', 0); b.classList.remove('done'); };
  const tick = (now) => {
    const p = Math.min((now - start) / HOLD_MS, 1);
    b.style.setProperty('--p', p);
    if (p < 1) { frame = requestAnimationFrame(tick); return; }
    b.classList.add('done');
    haptic(30);
    send({ t: 'sys', a: b.dataset.sys });
    start = 0;
    setTimeout(reset, 600);
  };
  b.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    b.setPointerCapture(e.pointerId);
    start = performance.now();
    haptic();
    frame = requestAnimationFrame(tick);
  });
  for (const ev of ['pointerup', 'pointercancel']) b.addEventListener(ev, () => { if (start) reset(); });
}

// ---------- full screen ----------
async function toggleFullscreen(on = !document.fullscreenElement) {
  try {
    if (!on && document.fullscreenElement) await document.exitFullscreen();
    else if (on && !document.fullscreenElement) await document.documentElement.requestFullscreen({ navigationUI: 'hide' });
  } catch { /* unsupported (e.g. iPhone Safari) */ }
}
$('#btn-fs').addEventListener('pointerdown', (e) => e.preventDefault());
$('#btn-fs').addEventListener('click', () => toggleFullscreen());
if (!document.documentElement.requestFullscreen) $('#btn-fs').hidden = true;
// Browsers only allow full screen right after a touch ends (pointerup
// grants "user activation" for touch input): do it on the first one.
let autoFsDone = false;
document.addEventListener('pointerup', () => {
  if (autoFsDone) return;
  autoFsDone = true;
  if (settings.autofs) toggleFullscreen(true);
}, { capture: true });

// When Android's back gesture hides the keyboard the field keeps focus;
// detect the viewport growing back and drop focus so the button toggles.
let fullHeight = window.innerHeight;
window.addEventListener('resize', () => {
  const h = window.visualViewport ? window.visualViewport.height : window.innerHeight;
  if (document.activeElement !== kbd) { fullHeight = Math.max(fullHeight, h); return; }
  if (h >= fullHeight * 0.92 && kbd.dataset.wasOpen === '1') { kbd.dataset.wasOpen = ''; kbd.blur(); }
  else if (h < fullHeight * 0.8) kbd.dataset.wasOpen = '1';
});
window.addEventListener('orientationchange', () => { fullHeight = 0; });

// ---------- settings dialog ----------
$('#btn-settings').addEventListener('click', () => { kbd.blur(); $('#settings').showModal(); });
$('#btn-forget').addEventListener('click', () => {
  if (!confirm(t('forgetQ'))) return;
  send({ t: 'forget' }); // also remove this phone's key from the computer
  localStorage.removeItem('tapis.device');
  fatal = true;
  if (ws) setTimeout(() => ws && ws.close(), 300);
  $('#settings').close();
  overlay('notPairedT', 'notPaired');
});

connect();
