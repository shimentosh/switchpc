'use strict';

// Monitor input codes (MCCS VCP 0x60). Monitors differ; "Other…" accepts any number.
const INPUTS = [
  [15, 'DisplayPort 1'], [16, 'DisplayPort 2'], [17, 'HDMI 1'], [18, 'HDMI 2'],
  [27, 'USB-C'], [3, 'DVI 1'], [4, 'DVI 2'], [1, 'VGA 1'],
];
const inputName = v => (INPUTS.find(i => i[0] === v) || [0, v ? 'Input ' + v : 'Not set'])[1];

const $ = sel => document.querySelector(sel);
const view = $('#view');
const sleep = ms => new Promise(r => setTimeout(r, ms));
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

const ICONS = {
  host: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/><path d="M7 10h4M13 10h4"/></svg>',
  agent: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="5" width="16" height="10" rx="2"/><path d="M2 19h20"/><path d="M12 8v4M10 10h4"/></svg>',
};

let state = null;
let tab = ['home', 'monitors', 'computers', 'settings'].includes(location.hash.slice(1)) ? location.hash.slice(1) : 'home';
let forceWizard = false;
let offline = false;

// ---------- API ----------
async function api(path, opts = {}) {
  const res = await fetch(path, opts);
  const text = await res.text();
  if (!res.ok) throw new Error(text.trim() || res.statusText);
  try { return text ? JSON.parse(text) : {}; } catch { return text; }
}
const post = (path, body) => api(path, {
  method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}),
});

function toast(msg, isError) {
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'toast' + (isError ? ' error' : '');
  t.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => { t.hidden = true; }, isError ? 5000 : 2500);
}

function setOffline(on, text) {
  offline = on;
  $('#offline').hidden = !on;
  if (text) $('#offline-text').textContent = text;
}

async function refresh() {
  try {
    state = await api('/api/state');
    if (offline) setOffline(false);
  } catch (e) {
    setOffline(true, 'Connecting to VKVM…');
    return false;
  }
  render();
  return true;
}

async function waitForApp(text) {
  setOffline(true, text);
  await sleep(1200);
  for (let i = 0; i < 60; i++) {
    try { await api('/api/app/ping'); break; } catch { await sleep(700); }
  }
  await refresh();
}

async function restartApp() {
  try { await post('/api/app/restart'); } catch { /* the app is already going away */ }
  await waitForApp('Restarting VKVM…');
}

// Long poll so the app can bring this window to the front.
// When VKVM has quit (not restarted) the window closes itself after a while.
// If VKVM stopped without restarting, the window starts it again once.
async function eventLoop() {
  let failures = 0;
  for (;;) {
    try {
      const ev = await api('/api/app/events');
      failures = 0;
      if (ev.focus && window.vkvmFocus) window.vkvmFocus();
    } catch {
      failures++;
      if (failures === 6 && window.vkvmStartApp) {
        $('#offline-text').textContent = 'Starting VKVM…';
        window.vkvmStartApp();
      }
      if (failures > 40 && window.vkvmClose) window.vkvmClose();
      await sleep(1500);
      if (offline) await refresh();
    }
  }
}

// ---------- Shell ----------
function render() {
  const s = state;
  const roleLabel = s.role === 'agent' ? 'Second computer' : 'Main computer';
  $('#who').innerHTML = s.name
    ? `<span>${esc(s.name)}</span><span class="pill plain">${roleLabel}</span>`
    : '';
  const wizard = !s.setup_complete || forceWizard;
  $('#tabs').hidden = wizard;
  const banner = $('#banner');
  banner.hidden = wizard || !s.restart_pending;
  $('#banner-text').textContent = 'Some changes take effect after VKVM restarts.';
  $('#banner-action').onclick = restartApp;
  document.querySelectorAll('#tabs button').forEach(b => {
    b.classList.toggle('active', b.dataset.tab === tab);
    b.onclick = () => { tab = b.dataset.tab; render(); };
  });
  if (wizard) renderWizard(); else renderTab();
}

// ---------- Wizard ----------
const STEP_KEY = 'vkvm.step';
const getStep = () => { try { return sessionStorage.getItem(STEP_KEY); } catch { return null; } };
const setStep = s => { try { sessionStorage.setItem(STEP_KEY, s); } catch { /* storage off */ } };

function stepsFor(role) {
  return role === 'agent' ? ['role', 'join', 'finish'] : ['role', 'monitors', 'pair', 'finish'];
}

function renderWizard() {
  let step = getStep();
  const steps = stepsFor(state.role);
  if (!state.name || !steps.includes(step)) step = 'role';
  const idx = steps.indexOf(step);
  const bar = steps.map((_, i) => `<span class="${i <= idx ? 'done' : ''}"></span>`).join('');
  view.innerHTML = `<div class="steps">${bar}</div><div id="step"></div>`;
  const go = next => { setStep(next); render(); };
  ({ role: stepRole, monitors: stepMonitors, pair: stepPair, join: stepJoin, finish: stepFinish })[step]($('#step'), go);
}

function stepRole(el, go) {
  let role = state.name ? state.role : (state.os === 'windows' ? 'host' : 'agent');
  el.innerHTML = `
    <div class="card">
      <h1>Welcome to VKVM</h1>
      <p class="muted">VKVM switches your monitors between two computers with a hotkey, like a KVM switch without the box.</p>
      <div class="notice info">Before you start: connect <b>both computers</b> to the monitor with their own cable, each to a
        <b>different input</b> (for example HDMI 1 and HDMI 2), turn on <b>DDC/CI</b> in the monitor's menu,
        and install VKVM on both computers.</div>
      <label class="field"><span>Name of this computer</span>
        <input type="text" id="name" maxlength="32" value="${esc(state.name || state.default_name)}"></label>
      <b>What is this computer?</b>
      <div class="choice-grid">
        <button class="choice" data-role="host">${ICONS.host}
          <h3>Main computer</h3>
          <p>Your keyboard and mouse are plugged in here. Set this one up first.</p></button>
        <button class="choice" data-role="agent">${ICONS.agent}
          <h3>Second computer</h3>
          <p>Joins the main computer with a 6-digit code.</p></button>
      </div>
      <div class="row end"><button class="btn primary" id="next">Continue</button></div>
    </div>`;
  const mark = () => el.querySelectorAll('.choice').forEach(c => c.classList.toggle('selected', c.dataset.role === role));
  el.querySelectorAll('.choice').forEach(c => c.onclick = () => { role = c.dataset.role; mark(); });
  mark();
  $('#next').onclick = async () => {
    const btn = $('#next');
    btn.disabled = true;
    try {
      const res = await post('/api/setup/role', { role, name: $('#name').value });
      setStep(stepsFor(role)[1]);
      if (res.restart_required) await restartApp(); else await refresh();
    } catch (e) { toast(e.message, true); btn.disabled = false; }
  };
}

function inputSelect(value, attrs = '') {
  const known = INPUTS.some(i => i[0] === value);
  const opts = INPUTS.map(([v, n]) => `<option value="${v}" ${v === value ? 'selected' : ''}>${n}</option>`).join('');
  const other = !known && value ? `<option value="${value}" selected>Input ${value}</option>` : '';
  return `<select ${attrs}><option value="0" ${!value ? 'selected' : ''}>Not set</option>${opts}${other}<option value="other">Other…</option></select>`;
}

function readSelect(sel) {
  if (sel.value !== 'other') return Number(sel.value);
  const v = Number(prompt('Input number (decimal VCP 0x60 value):', ''));
  if (!v || v < 1 || v > 255) { sel.value = '0'; return 0; }
  const opt = new Option('Input ' + v, String(v), true, true);
  sel.add(opt, sel.options.length - 1);
  return v;
}

async function stepMonitors(el, go) {
  el.innerHTML = `<div class="card"><h1>Your monitors</h1><p class="muted"><span class="spinner inline"></span> Looking for monitors…</p></div>`;
  let monitors = [];
  let error = '';
  try { monitors = await api('/api/monitors/detect'); } catch (e) { error = e.message; }
  const rows = monitors.map(m => `
    <tr data-id="${esc(m.id)}">
      <td><b>${esc(m.name || 'Monitor')}</b><div class="muted small">${m.ddc_supported ? 'DDC/CI ready' : 'DDC/CI not responding'}</div></td>
      <td>${inputSelect(m.current_input, 'class="in"')}</td>
      <td><button class="btn small test">Test</button></td>
    </tr>`).join('');
  el.innerHTML = `
    <div class="card">
      <h1>Which input is ${esc(state.name)} on?</h1>
      <p class="muted">Your monitors are showing this computer right now, so VKVM read the input from each monitor.
        Fix it here if it's wrong.</p>
      ${error ? `<div class="notice bad">${esc(error)}</div>` : ''}
      ${monitors.length ? `<table><thead><tr><th>Monitor</th><th>${esc(state.name)} is on</th><th></th></tr></thead><tbody>${rows}</tbody></table>`
        : `<div class="notice warn">No monitors with DDC/CI were found. Turn on DDC/CI in the monitor's on-screen menu, then search again.
           Some USB-C/Thunderbolt adapters and docks block DDC/CI.</div>`}
      <div class="row end" style="margin-top:16px">
        <button class="btn ghost" id="again">Search again</button>
        <button class="btn primary" id="next">Continue</button>
      </div>
    </div>`;
  el.querySelectorAll('select.in').forEach(s => s.onchange = () => readSelect(s));
  el.querySelectorAll('button.test').forEach(b => b.onclick = async () => {
    const tr = b.closest('tr');
    const v = readSelect(tr.querySelector('select'));
    if (!v) return;
    try {
      await api(`/api/test?monitor=${encodeURIComponent(tr.dataset.id)}&input=${v}`, { method: 'POST' });
      toast('Sent ' + inputName(v) + ' to the monitor');
    } catch (e) { toast(e.message, true); }
  });
  $('#again').onclick = () => stepMonitors(el, go);
  $('#next').onclick = async () => {
    const inputs = {};
    el.querySelectorAll('tr[data-id]').forEach(tr => {
      const v = Number(tr.querySelector('select').value);
      if (v > 0) inputs[tr.dataset.id] = v;
    });
    try {
      await post('/api/profile', { name: state.this_profile, monitor_inputs: inputs });
      go('pair');
    } catch (e) { toast(e.message, true); }
  };
}

function pairingPanel(el, onPaired, onSkip) {
  let openedAt = Date.now();
  let timer = null;
  const ips = (state.local_ips || []).join(', ');
  const draw = st => {
    if (!el.isConnected) { clearInterval(timer); return; }
    if (st.last && new Date(st.last.at).getTime() >= openedAt - 2000) {
      clearInterval(timer);
      onPaired(st.last);
      return;
    }
    const left = st.open ? Math.max(0, Math.round((new Date(st.expires_at) - Date.now()) / 60000)) : 0;
    el.querySelector('#code-area').innerHTML = st.open
      ? `<div class="code">${st.code.slice(0, 3)} ${st.code.slice(3)}</div>
         <p class="muted small" style="text-align:center"><span class="spinner inline"></span> Waiting for the second computer… code valid for ${left || '<1'} min</p>`
      : `<div class="notice warn">The code expired or was entered wrong too often.</div>
         <div class="row"><button class="btn" id="new-code">Get a new code</button></div>`;
    const nc = el.querySelector('#new-code');
    if (nc) nc.onclick = open;
  };
  const poll = async () => { try { draw(await api('/api/pair/status')); } catch { /* retry next tick */ } };
  const open = async () => {
    openedAt = Date.now();
    try { draw(await post('/api/pair/open')); } catch (e) {
      el.querySelector('#code-area').innerHTML = `<div class="notice bad">${esc(e.message)}</div>`;
      return;
    }
    clearInterval(timer);
    timer = setInterval(poll, 1500);
  };
  el.innerHTML = `
    <ol class="muted" style="padding-left:18px;margin:0 0 6px">
      <li>On the second computer, open VKVM and choose <b>Second computer</b>.</li>
      <li>Pick <b>${esc(state.name)}</b> from the list (or type ${esc(ips || 'this computer\'s IP')}).</li>
      <li>Enter this code:</li>
    </ol>
    <div id="code-area"><p class="muted"><span class="spinner inline"></span> Getting a code…</p></div>
    ${onSkip ? '<div class="row end"><button class="btn ghost" id="skip">Skip for now</button></div>' : ''}`;
  if (onSkip) el.querySelector('#skip').onclick = () => { clearInterval(timer); post('/api/pair/close').catch(() => {}); onSkip(); };
  open();
}

function stepPair(el, go) {
  el.innerHTML = `<div class="card"><h1>Connect your second computer</h1><div id="pp"></div></div>`;
  pairingPanel($('#pp'), last => {
    el.innerHTML = `<div class="card"><h1>Paired with ${esc(last.name)} 🎉</h1>
      ${last.unmatched_monitors ? `<div class="notice warn">VKVM couldn't tell which input ${esc(last.name)} uses on
        ${last.unmatched_monitors} monitor(s). You can set it on the Monitors tab later.</div>`
        : `<p class="muted">VKVM learned which input ${esc(last.name)} is plugged into.</p>`}
      <div class="row end"><button class="btn primary" id="next">Continue</button></div></div>`;
    $('#next').onclick = () => go('finish');
  }, () => go('finish'));
}

function joinPanel(el, onJoined) {
  let selected = '';
  el.innerHTML = `
    <p class="muted">Make sure VKVM on the main computer shows a pairing code, and both computers are on the same Wi-Fi or network.</p>
    <b>Main computers found</b>
    <div class="list" id="hosts"><p class="muted"><span class="spinner inline"></span> Searching the network…</p></div>
    <div class="row" style="margin-bottom:14px">
      <button class="btn small" id="rescan">Search again</button>
      <span class="muted small">Not listed? Type its IP address:</span>
      <input type="text" id="manual" placeholder="192.168.1.20" style="width:160px">
    </div>
    <label class="field"><span>Pairing code</span>
      <input type="text" id="code" class="code-input" inputmode="numeric" maxlength="7" placeholder="000000" autocomplete="off"></label>
    <label class="field"><span>This computer is plugged into the monitor's</span>
      <select id="my-input"><option value="0">Detect automatically</option>
        ${INPUTS.map(([v, n]) => `<option value="${v}">${n}</option>`).join('')}</select>
      <span class="muted small" style="display:block;margin-top:6px">Pick it if you know it (for example HDMI 2). Not every monitor reports it.</span></label>
    <div class="row end"><button class="btn primary" id="join">Pair</button></div>`;
  const hostsEl = el.querySelector('#hosts');
  const scan = async () => {
    hostsEl.innerHTML = '<p class="muted"><span class="spinner inline"></span> Searching the network…</p>';
    let hosts = [];
    try { hosts = await api('/api/pair/discover'); } catch { /* shown as empty */ }
    if (!hosts.length) {
      hostsEl.innerHTML = '<div class="notice warn">No main computer found yet. Check that VKVM is set up as the main computer there, that both are on the same network, and that the firewall allows VKVM.</div>';
      return;
    }
    hostsEl.innerHTML = hosts.map(h => {
      const addr = h.ip.includes(':') ? `[${h.ip}]:${h.port}` : `${h.ip}:${h.port}`;
      return `<div class="list-item" data-addr="${esc(addr)}">${ICONS.host.replace('<svg', '<svg width="26" height="26" style="color:var(--accent)"')}
        <div class="grow"><b>${esc((h.profiles && h.profiles[0]) || 'VKVM main computer')}</b><span class="muted small">${esc(addr)}</span></div></div>`;
    }).join('');
    hostsEl.querySelectorAll('.list-item').forEach(it => it.onclick = () => {
      selected = it.dataset.addr;
      el.querySelector('#manual').value = '';
      hostsEl.querySelectorAll('.list-item').forEach(x => x.classList.toggle('selected', x === it));
    });
    if (hosts.length === 1) hostsEl.querySelector('.list-item').click();
  };
  el.querySelector('#rescan').onclick = scan;
  el.querySelector('#manual').oninput = () => { selected = ''; hostsEl.querySelectorAll('.list-item').forEach(x => x.classList.remove('selected')); };
  el.querySelector('#join').onclick = async () => {
    const address = el.querySelector('#manual').value.trim() || selected;
    const code = el.querySelector('#code').value.replace(/\D/g, '');
    if (!address) return toast('Pick the main computer or type its IP address', true);
    if (code.length !== 6) return toast('Enter the 6-digit code shown on the main computer', true);
    const btn = el.querySelector('#join');
    btn.disabled = true;
    try {
      btn.innerHTML = '<span class="spinner inline"></span> Pairing…';
      const input = Number(el.querySelector('#my-input').value) || 0;
      const res = await post('/api/pair/join', { address, code, input });
      await onJoined(res);
    } catch (e) { toast(e.message, true); btn.disabled = false; btn.textContent = 'Pair'; }
  };
  scan();
}

function stepJoin(el, go) {
  el.innerHTML = `<div class="card"><h1>Join the main computer</h1><div id="jp"></div></div>`;
  joinPanel($('#jp'), async res => {
    toast(`Paired with ${res.host}`);
    setStep('finish');
    await restartApp();
  });
}

function permissionsCard(s) {
  if (s.os !== 'darwin') return '';
  const p = s.permissions || {};
  const row = (kind, ok, title, why) => `
    <div class="setting"><div class="grow"><b>${title}</b><span class="muted small">${why}</span></div>
      ${ok ? '<span class="pill ok">Allowed</span>' : `<button class="btn small perm" data-kind="${kind}">Allow…</button>`}</div>`;
  return `<div class="card"><h2>macOS permissions</h2>
    <p class="muted small">macOS asks once. After switching VKVM on in System Settings, restart VKVM.</p>
    ${row('accessibility', p.accessibility, 'Accessibility', 'Needed for hotkeys and for keyboard/mouse sharing.')}
    ${row('input_monitoring', p.input_monitoring, 'Input Monitoring', 'Needed only if this Mac shares its own keyboard and mouse.')}
  </div>`;
}

function bindPermissions(root) {
  root.querySelectorAll('button.perm').forEach(b => b.onclick = async () => {
    try { await post('/api/permissions/request', { kind: b.dataset.kind }); toast('Switch VKVM on in System Settings, then restart VKVM'); }
    catch (e) { toast(e.message, true); }
  });
}

function hotkeyList(s) {
  return (s.profiles || []).filter(p => p.hotkey).map(p =>
    `<div class="setting"><div class="grow"><b>Show ${esc(p.name)}</b></div>${keys(p.hotkey)}</div>`).join('');
}
const keys = hk => hk ? hk.split('+').map(k => `<kbd>${esc(k)}</kbd>`).join(' + ') : '<span class="muted">None</span>';

// Choices on the last step survive the periodic refresh while the second
// computer is still connecting.
const finishPrefs = { boot: true, share: false };

function stepFinish(el, go) {
  const s = state;
  const agent = s.role === 'agent';
  const conn = s.connection || {};
  el.innerHTML = `
    <div class="card">
      <h1>You're all set</h1>
      ${agent ? `<p>${conn.connected ? `<span class="pill ok">Connected to ${esc(s.peer_profile)}</span>`
        : `<span class="pill warn">Connecting to ${esc(s.peer_profile || 'the main computer')}…</span>`}</p>` : ''}
      <p class="muted">Use these hotkeys on either computer, or the VKVM icon in the ${s.os === 'darwin' ? 'menu bar' : 'taskbar'}:</p>
      ${hotkeyList(s) || '<p class="muted small">Hotkeys appear once the computers are paired.</p>'}
    </div>
    ${permissionsCard(s)}
    <div class="card">
      <div class="setting"><div class="grow"><b>Start VKVM when I log in</b><span class="muted small">Recommended, so switching always works.</span></div>
        <label class="toggle"><input type="checkbox" id="boot" ${finishPrefs.boot ? 'checked' : ''}><span></span></label></div>
      ${!agent && s.peer_profile ? `<div class="setting"><div class="grow"><b>Share keyboard and mouse</b>
        <span class="muted small">Control ${esc(s.peer_profile)} with this computer's keyboard and mouse while the monitors show it. Optional; you can turn it on later in Settings.</span></div>
        <label class="toggle"><input type="checkbox" id="share" ${finishPrefs.share ? 'checked' : ''}><span></span></label></div>` : ''}
    </div>
    <div class="row end"><button class="btn primary" id="done">Finish</button></div>`;
  bindPermissions(el);
  $('#boot').onchange = e => { finishPrefs.boot = e.target.checked; };
  if ($('#share')) $('#share').onchange = e => { finishPrefs.share = e.target.checked; };
  if (agent && !conn.connected) setTimeout(() => { if (el.isConnected) refresh(); }, 3000);
  $('#done').onclick = async () => {
    try {
      const share = $('#share');
      if (share && share.checked) await post('/api/sharing', { enabled: true });
      const res = await post('/api/setup/finish', { start_on_boot: $('#boot').checked });
      setStep('');
      forceWizard = false;
      tab = 'home';
      if (res.restart_required) await restartApp(); else await refresh();
    } catch (e) { toast(e.message, true); }
  };
}

// ---------- Dashboard ----------
function renderTab() {
  ({ home: tabHome, monitors: tabMonitors, computers: tabComputers, settings: tabSettings })[tab]();
}

function connectionPill(s) {
  if (s.role === 'agent') {
    const c = s.connection || {};
    return c.connected ? `<span class="pill ok">Connected to ${esc(s.peer_profile || 'main computer')}</span>`
      : `<span class="pill warn">Looking for ${esc(s.peer_profile || 'the main computer')}…</span>`;
  }
  return s.peers.length ? `<span class="pill ok">${s.peers.length === 1 ? '1 computer paired' : s.peers.length + ' computers paired'}</span>`
    : '<span class="pill warn">No second computer yet</span>';
}

// Warn about profiles that would not switch any monitor.
function missingInputs(s) {
  if (s.role === 'agent') return '';
  const empty = (s.profiles || []).filter(p => !Object.keys(p.monitor_inputs || {}).length).map(p => esc(p.name));
  if (!empty.length) return '';
  const opts = INPUTS.map(([v, n]) => `<option value="${v}">${n}</option>`).join('');
  return (s.profiles || []).filter(p => !Object.keys(p.monitor_inputs || {}).length).map(p => `
    <div class="notice warn row">
      <span>Which input is <b>${esc(p.name)}</b> plugged into?</span>
      <select class="quick-input" data-profile="${esc(p.name)}"><option value="0">Choose…</option>${opts}</select>
      <a href="#monitors" class="fix-inputs small">More options</a>
    </div>`).join('');
}

// Sets one input for every monitor of a profile (the common one-input-per-computer case).
async function quickSetInput(sel) {
  const v = Number(sel.value);
  if (!v) return;
  sel.disabled = true;
  try {
    const monitors = await api('/api/monitors/detect');
    if (!monitors.length) throw new Error('No monitors found. Turn on DDC/CI in the monitor menu.');
    const set = {};
    monitors.forEach(m => { set[m.id] = v; });
    await post('/api/profile', { name: sel.dataset.profile, set_inputs: set });
    toast(`${sel.dataset.profile} uses ${inputName(v)}`);
    await refresh();
  } catch (e) { toast(e.message, true); sel.disabled = false; }
}

function tabHome() {
  const s = state;
  const cards = (s.profiles || []).map(p => {
    const mine = p.name === s.this_profile;
    const active = p.name === s.current_profile;
    return `<div class="target ${active ? 'active' : ''}">
      <span class="label">${mine ? 'This computer' : 'Other computer'}</span>
      <span class="name">${esc(p.name)}</span>
      <span class="hk">${p.hotkey ? keys(p.hotkey) : 'No hotkey'}</span>
      <button class="btn ${active ? '' : 'primary'} sw" data-name="${esc(p.name)}">${active ? 'Showing now' : 'Switch monitors'}</button>
    </div>`;
  }).join('');
  view.innerHTML = `
    <div class="card-head"><div><h1>Switch</h1><p class="muted">Pick which computer your monitors show.</p></div><div>${connectionPill(s)}</div></div>
    ${!s.ddc_available ? '<div class="notice bad">Monitor control is not available on this computer.</div>' : ''}
    ${missingInputs(s)}
    <div class="switch-grid">${cards || '<p class="muted">No profiles yet.</p>'}</div>
    ${s.role === 'host' && !s.peers.length ? `<div class="card"><h2>Add your second computer</h2>
      <p class="muted">Install VKVM on the other computer and pair it to switch to it.</p>
      <button class="btn primary" id="go-pair">Pair a computer</button></div>` : ''}
    <div class="card"><div class="setting"><div class="grow"><b>Turn off displays</b><span class="muted small">Puts the monitors to sleep.${s.sleep_hotkey ? ' Hotkey: ' + keys(s.sleep_hotkey) : ''}</span></div>
      <button class="btn small" id="sleep">Sleep displays</button></div></div>`;
  view.querySelectorAll('button.sw').forEach(b => b.onclick = async () => {
    b.disabled = true;
    try { await api('/api/switch?profile=' + encodeURIComponent(b.dataset.name), { method: 'POST' }); toast('Switched to ' + b.dataset.name); }
    catch (e) { toast(e.message, true); }
    await refresh();
  });
  view.querySelectorAll('.fix-inputs').forEach(a => a.onclick = e => { e.preventDefault(); tab = 'monitors'; render(); });
  view.querySelectorAll('select.quick-input').forEach(sel => sel.onchange = () => quickSetInput(sel));
  const gp = $('#go-pair');
  if (gp) gp.onclick = () => { tab = 'computers'; render(); };
  $('#sleep').onclick = () => api('/api/sleep-display', { method: 'POST' }).catch(e => toast(e.message, true));
}

async function tabMonitors() {
  const s = state;
  const host = s.role !== 'agent';
  view.innerHTML = `<h1>Monitors</h1><p class="muted"><span class="spinner inline"></span> Reading monitors…</p>`;
  let monitors = [];
  let error = '';
  try { monitors = await api('/api/monitors/detect'); } catch (e) { error = e.message; }
  if (tab !== 'monitors') return;
  const profiles = s.profiles || [];
  const head = profiles.map(p => `<th>${esc(p.name)}</th>`).join('');
  const rows = monitors.map(m => `<tr data-id="${esc(m.id)}">
      <td><b>${esc(m.name || 'Monitor')}</b><div class="muted small">Now: ${esc(inputName(m.current_input))}${m.ddc_supported ? '' : ' · DDC/CI not responding'}</div></td>
      ${profiles.map(p => {
        const v = (p.monitor_inputs || {})[m.id] || 0;
        return `<td><div class="row">${inputSelect(v, `data-profile="${esc(p.name)}" ${host ? '' : 'disabled'}`)}
          <button class="btn small test" ${v ? '' : 'disabled'}>Test</button></div></td>`;
      }).join('')}
    </tr>`).join('');
  view.innerHTML = `
    <div class="card-head"><div><h1>Monitors</h1>
      <p class="muted">Which input each computer is plugged into, per monitor.${host ? '' : ' Change these on the main computer.'}</p></div>
      <button class="btn small" id="reload">Refresh</button></div>
    ${error ? `<div class="notice bad">${esc(error)}</div>` : ''}
    <div class="card">${monitors.length ? `<table><thead><tr><th>Monitor</th>${head}</tr></thead><tbody>${rows}</tbody></table>`
      : '<p class="muted">No monitors with DDC/CI found. Turn on DDC/CI in the monitor menu and refresh.</p>'}</div>
    <p class="muted small">Tip: switch the monitor to a computer, then click Refresh — "Now" shows the input it is on.</p>`;
  $('#reload').onclick = tabMonitors;
  view.querySelectorAll('tr[data-id] select').forEach(sel => sel.onchange = async () => {
    const v = readSelect(sel);
    const id = sel.closest('tr').dataset.id;
    sel.closest('td').querySelector('button.test').disabled = !v;
    try {
      await post('/api/profile', { name: sel.dataset.profile, set_inputs: { [id]: v } });
      toast('Saved');
      state = await api('/api/state');
    } catch (e) { toast(e.message, true); }
  });
  view.querySelectorAll('button.test').forEach(b => b.onclick = async () => {
    const sel = b.closest('td').querySelector('select');
    const id = b.closest('tr').dataset.id;
    try { await api(`/api/test?monitor=${encodeURIComponent(id)}&input=${Number(sel.value)}`, { method: 'POST' }); toast('Sent ' + inputName(Number(sel.value))); }
    catch (e) { toast(e.message, true); }
  });
}

function tabComputers() {
  const s = state;
  const host = s.role !== 'agent';
  const peers = s.peers.map(p => `<div class="list-item" style="cursor:default">
      ${ICONS[p.role === 'host' ? 'host' : 'agent'].replace('<svg', '<svg width="26" height="26" style="color:var(--accent)"')}
      <div class="grow"><b>${esc(p.role === 'host' ? (s.peer_profile || 'Main computer') : (s.peer_profile || 'Second computer'))}</b>
        <span class="muted small">${p.role === 'host' ? 'Main computer' : 'Second computer'}${p.endpoint ? ' · ' + esc(p.endpoint) : ''}</span></div>
      <button class="btn small danger forget" data-id="${esc(p.device_id)}">Unpair</button></div>`).join('');
  view.innerHTML = `
    <div class="card-head"><div><h1>Computers</h1><p class="muted">The computers that share these monitors.</p></div>${connectionPill(s)}</div>
    <div class="card">
      <h2>This computer</h2>
      <p class="muted small selectable">${esc(s.name)} · ${host ? 'Main computer' : 'Second computer'} · ${esc((s.local_ips || []).join(', '))}${host ? ' · port ' + s.api_port : ''}</p>
      <div class="list">${peers || '<p class="muted">Not paired with another computer yet.</p>'}</div>
    </div>
    <div class="card" id="pair-card">
      <h2>${host ? 'Pair a computer' : 'Pair with a main computer'}</h2>
      <p class="muted">${host ? 'Show a code to connect another computer.' : 'Connect to a different main computer.'}</p>
      <button class="btn primary" id="start-pair">${host ? 'Show pairing code' : 'Find main computer'}</button>
    </div>
    <div class="card"><div class="setting"><div class="grow"><b>Run setup again</b>
      <span class="muted small">Change this computer's name or whether it's the main or second computer.</span></div>
      <button class="btn small" id="rerun">Run setup</button></div></div>`;
  view.querySelectorAll('button.forget').forEach(b => b.onclick = async () => {
    if (!confirm('Unpair this computer? You can pair it again with a new code.')) return;
    try {
      const res = await post('/api/pair/forget', { device_id: b.dataset.id });
      if (res.restart_required) await restartApp(); else await refresh();
    } catch (e) { toast(e.message, true); }
  });
  $('#start-pair').onclick = () => {
    const card = $('#pair-card');
    if (host) {
      card.innerHTML = '<h2>Pair a computer</h2><div id="pp"></div>';
      pairingPanel($('#pp'), last => { toast('Paired with ' + last.name); refresh(); }, () => refresh());
    } else {
      card.innerHTML = '<h2>Pair with a main computer</h2><div id="jp"></div>';
      joinPanel($('#jp'), async res => { toast('Paired with ' + res.host); await restartApp(); });
    }
  };
  $('#rerun').onclick = () => { forceWizard = true; setStep('role'); render(); };
}

function hotkeyButton(value, onSave) {
  const btn = document.createElement('button');
  btn.className = 'btn small hotkey-btn';
  const show = v => { btn.textContent = v || 'Set hotkey'; };
  show(value);
  btn.onclick = () => {
    if (btn.classList.contains('recording')) return;
    btn.classList.add('recording');
    btn.textContent = 'Press keys… (Esc cancels)';
    const finish = async v => {
      window.removeEventListener('keydown', onKey, true);
      btn.classList.remove('recording');
      show(v === null ? value : v);
      if (v !== null && v !== value) { value = v; await onSave(v); }
    };
    const onKey = e => {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === 'Escape') return finish(null);
      if (e.key === 'Backspace' || e.key === 'Delete') return finish('');
      if (['Control', 'Alt', 'Shift', 'Meta'].includes(e.key)) return;
      const mods = [];
      if (e.ctrlKey) mods.push('Ctrl');
      if (e.altKey) mods.push('Alt');
      if (e.shiftKey) mods.push('Shift');
      if (e.metaKey) mods.push('Cmd');
      if (!mods.length) { btn.textContent = 'Add Ctrl, Alt or Cmd'; return; }
      let k = e.code;
      if (/^Key[A-Z]$/.test(k)) k = k.slice(3);
      else if (/^Digit\d$/.test(k)) k = k.slice(5);
      else if (/^Numpad\d$/.test(k)) k = k.slice(6);
      else if (/^F\d{1,2}$/.test(k)) { /* F1..F24 as is */ }
      else if (k === 'Space') k = 'Space';
      else k = (e.key.length === 1 ? e.key.toUpperCase() : e.key);
      finish(mods.concat(k).join('+'));
    };
    window.addEventListener('keydown', onKey, true);
  };
  return btn;
}

function tabSettings() {
  const s = state;
  const host = s.role !== 'agent';
  view.innerHTML = `
    <h1>Settings</h1>
    <div class="card"><h2>Hotkeys</h2>
      <p class="muted small">${host ? 'Click a hotkey and press the new combination. Backspace removes it.' : 'Profile hotkeys are set on the main computer.'}
        ${s.os === 'darwin' ? ' On a Mac, Ctrl hotkeys also work with Cmd.' : ''}</p>
      <div id="hk-profiles"></div>
      <div class="setting"><div class="grow"><b>Open VKVM</b></div><span id="hk-settings"></span></div>
      <div class="setting"><div class="grow"><b>Turn off displays</b></div><span id="hk-sleep"></span></div>
    </div>
    <div class="card">
      <div class="setting"><div class="grow"><b>Start VKVM when I log in</b></div>
        <label class="toggle"><input type="checkbox" id="boot" ${s.start_on_boot ? 'checked' : ''}><span></span></label></div>
      ${host ? `<div class="setting"><div class="grow"><b>Share keyboard and mouse</b>
        <span class="muted small">${s.peer_profile
          ? `Control ${esc(s.peer_profile)} with this computer's keyboard and mouse while the monitors show it.
             Emergency release: ${keys('Ctrl+Alt+Shift+Esc')}.${''}`
          : 'Pair a second computer first.'}</span></div>
        <label class="toggle"><input type="checkbox" id="share" ${s.sharing_enabled ? 'checked' : ''} ${s.peer_profile ? '' : 'disabled'}><span></span></label></div>` : ''}
    </div>
    ${permissionsCard(s)}
    <div class="card"><h2>Advanced</h2>
      <div class="setting"><div class="grow"><b>Classic settings</b><span class="muted small">All options, including network and calibration.</span></div>
        <a class="btn small" href="/classic">Open</a></div>
      <div class="setting"><div class="grow"><b>Restart VKVM</b></div><button class="btn small" id="restart">Restart</button></div>
      <div class="setting"><div class="grow"><b>Quit VKVM</b><span class="muted small">Hotkeys stop working until VKVM is opened again.</span></div>
        <button class="btn small danger" id="quit">Quit</button></div>
      <p class="muted small" style="margin-top:12px">VKVM ${esc(s.version)}</p>
    </div>`;
  const list = $('#hk-profiles');
  (s.profiles || []).forEach(p => {
    const row = document.createElement('div');
    row.className = 'setting';
    row.innerHTML = `<div class="grow"><b>Show ${esc(p.name)}</b></div>`;
    if (host) {
      row.appendChild(hotkeyButton(p.hotkey, async v => {
        try { await post('/api/profile', { name: p.name, hotkey: v }); toast('Hotkey saved'); state = await api('/api/state'); }
        catch (e) { toast(e.message, true); }
      }));
    } else {
      row.insertAdjacentHTML('beforeend', `<span>${keys(p.hotkey)}</span>`);
    }
    list.appendChild(row);
  });
  const saveGeneral = field => async v => {
    try { await post('/api/hotkeys', { [field]: v }); toast('Hotkey saved'); state = await api('/api/state'); }
    catch (e) { toast(e.message, true); }
  };
  $('#hk-settings').appendChild(hotkeyButton(s.settings_hotkey, saveGeneral('settings_hotkey')));
  $('#hk-sleep').appendChild(hotkeyButton(s.sleep_hotkey, saveGeneral('sleep_hotkey')));
  $('#boot').onchange = async e => {
    try { await post('/api/autostart', { enabled: e.target.checked }); toast(e.target.checked ? 'VKVM will start at login' : 'VKVM won\'t start at login'); }
    catch (err) { toast(err.message, true); e.target.checked = !e.target.checked; }
  };
  const share = $('#share');
  if (share) share.onchange = async e => {
    try { await post('/api/sharing', { enabled: e.target.checked }); toast(e.target.checked ? 'Keyboard and mouse sharing on' : 'Keyboard and mouse sharing off'); await refresh(); }
    catch (err) { toast(err.message, true); e.target.checked = !e.target.checked; }
  };
  bindPermissions(view);
  $('#restart').onclick = restartApp;
  $('#quit').onclick = async () => {
    if (!confirm('Quit VKVM? Hotkeys stop working until you open it again.')) return;
    try { await post('/api/app/quit'); } catch { /* exiting */ }
    if (window.vkvmClose) window.vkvmClose(); else setOffline(true, 'VKVM has quit. You can close this window.');
  };
}

// Keep the connection status fresh on Home without disturbing other tabs.
setInterval(() => {
  if (!offline && state && state.setup_complete && !forceWizard && tab === 'home' && !document.hidden) refresh();
}, 5000);

refresh();
// ?noevents is for headless screenshots, which wait for the network to go idle.
if (!location.search.includes('noevents')) eventLoop();
