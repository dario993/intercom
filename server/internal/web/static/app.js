const $ = (id) => document.getElementById(id);
let cfg = { settings: {}, channels: [], users: [] };
let editing = null; // user in the dialog: { id, channels: [{channelId, talk, listen}] }

async function api(method, path, body) {
  const res = await fetch('/api/' + path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (res.status === 401 && path !== 'login') {
    showAuth(false);
    throw new Error('Sessione scaduta');
  }
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

// guard runs fn and shows its error; returns undefined on failure.
async function guard(fn) {
  try { return await fn(); } catch (err) { alert(err.message); }
}

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const chanName = (id) => (cfg.channels.find((c) => c.id === id) || {}).name || '?';
const userName = (id) => (cfg.users.find((u) => u.id === id) || {}).name || '?';

// --- login ---
function showAuth(needsSetup) {
  $('app').hidden = true;
  $('auth').hidden = false;
  $('auth-form').dataset.setup = needsSetup ? '1' : '';
  $('auth-hint').textContent = needsSetup ? 'Primo avvio: crea la password amministratore.' : 'Accedi con la password amministratore.';
  $('auth-pass').autocomplete = needsSetup ? 'new-password' : 'current-password';
}

$('auth-form').onsubmit = async (e) => {
  e.preventDefault();
  $('auth-err').textContent = '';
  try {
    await api('POST', e.target.dataset.setup ? 'setup' : 'login', { password: $('auth-pass').value });
    $('auth-pass').value = '';
    await showApp();
  } catch (err) {
    $('auth-err').textContent = err.message;
  }
};

async function showApp() {
  $('auth').hidden = true;
  $('app').hidden = false;
  await loadConfig();
  showTab('overview');
}

async function loadConfig() {
  cfg = await api('GET', 'config');
  renderChannels();
  renderUsers();
  renderSettings();
}

function showTab(name) {
  for (const s of document.querySelectorAll('section[id^=tab-]')) s.hidden = s.id !== 'tab-' + name;
  for (const b of document.querySelectorAll('nav button')) b.classList.toggle('active', b.dataset.tab === name);
  refreshLive();
}
for (const b of document.querySelectorAll('nav button')) b.onclick = () => showTab(b.dataset.tab);
$('logout').onclick = async () => { await api('POST', 'logout'); showAuth(false); };

// --- overview, refreshed every second while visible ---
function talkLabel(u) {
  if (!u.target || u.target.kind === 'channels') return u.talk.map(chanName).join(', ');
  if (u.target.kind === 'all') return 'Chiamata a tutti';
  return 'Privata → ' + userName(u.target.userId);
}

async function refreshLive() {
  if ($('app').hidden || $('tab-overview').hidden) return;
  const users = await api('GET', 'live').catch(() => null);
  if (!users) return;
  $('live').innerHTML = users.map((u) => `<tr>
    <td>${esc(u.name)}</td>
    <td><span class="dot ${u.online ? 'on' : ''}"></span>${u.online ? 'Online' : 'Offline'}</td>
    <td>${u.talking ? esc(talkLabel(u)) : ''}</td>
    <td>${u.online ? `<button data-kick="${u.id}">Disconnetti</button>` : ''}</td>
  </tr>`).join('');
}
$('live').onclick = async (e) => {
  const id = e.target.dataset.kick;
  if (id) { await guard(() => api('POST', 'kick/' + id)); refreshLive(); }
};
setInterval(refreshLive, 1000);

// --- channels ---
function renderChannels() {
  const last = cfg.channels.length - 1;
  $('channels').innerHTML = cfg.channels.map((c, i) => `<tr data-id="${c.id}">
    <td><input type="color" value="${esc(c.color)}" data-field="color"></td>
    <td><input value="${esc(c.name)}" data-field="name"></td>
    <td>
      <button data-move="-1" ${i === 0 ? 'disabled' : ''}>↑</button>
      <button data-move="1" ${i === last ? 'disabled' : ''}>↓</button>
      <button data-del>Elimina</button>
    </td>
  </tr>`).join('');
}

$('channel-form').onsubmit = async (e) => {
  e.preventDefault();
  if (await guard(() => api('POST', 'channels', { name: $('ch-name').value, color: $('ch-color').value }))) $('ch-name').value = '';
  await loadConfig();
};

$('channels').onchange = async (e) => {
  const tr = e.target.closest('tr');
  const name = tr.querySelector('[data-field=name]').value;
  const color = tr.querySelector('[data-field=color]').value;
  await guard(() => api('PUT', 'channels/' + tr.dataset.id, { name, color }));
  await loadConfig();
};

$('channels').onclick = async (e) => {
  const tr = e.target.closest('tr');
  if (!tr) return;
  const id = +tr.dataset.id;
  if (e.target.dataset.del !== undefined) {
    if (!confirm(`Eliminare il canale "${chanName(id)}"? Verrà tolto anche a tutti gli utenti.`)) return;
    await guard(() => api('DELETE', 'channels/' + id));
  } else if (e.target.dataset.move) {
    const ids = cfg.channels.map((c) => c.id);
    const i = ids.indexOf(id), j = i + Number(e.target.dataset.move);
    [ids[i], ids[j]] = [ids[j], ids[i]];
    await guard(() => api('PUT', 'channels/order', ids));
  } else return;
  await loadConfig();
};

// --- users ---
function renderUsers() {
  $('users').innerHTML = cfg.users.map((u) => `<tr data-id="${u.id}">
    <td>${esc(u.name)}</td>
    <td>${u.enabled ? 'Abilitato' : 'Disabilitato'}</td>
    <td>${(u.channels || []).map((a) => esc(chanName(a.channelId))).join(', ')}</td>
    <td><button data-edit>Modifica</button> <button data-del>Elimina</button></td>
  </tr>`).join('');
}

$('users').onclick = async (e) => {
  const tr = e.target.closest('tr');
  if (!tr) return;
  const u = cfg.users.find((x) => x.id === +tr.dataset.id);
  if (e.target.dataset.edit !== undefined) openUser(u);
  if (e.target.dataset.del !== undefined && confirm(`Eliminare l'utente "${u.name}"?`)) {
    await guard(() => api('DELETE', 'users/' + u.id));
    await loadConfig();
  }
};
$('user-new').onclick = () => openUser(null);

function openUser(u) {
  editing = { id: u ? u.id : null, channels: u ? (u.channels || []).map((a) => ({ ...a })) : [] };
  $('user-title').textContent = u ? 'Modifica ' + u.name : 'Nuovo utente';
  $('u-name').value = u ? u.name : '';
  $('u-pass').value = '';
  $('u-pass').required = !u;
  $('u-pass').placeholder = u ? 'Lascia vuoto per non cambiarla' : '';
  $('u-enabled').checked = u ? u.enabled : true;
  $('u-allcall').checked = u ? u.allCall : false;
  $('u-private').checked = u ? u.privateCall : true;
  $('user-err').textContent = '';
  renderUserChannels();
  $('user-dialog').showModal();
}

// Assigned channels first (in the user's order), then the unassigned ones.
function renderUserChannels() {
  const assigned = editing.channels.map((a) => a.channelId);
  const rows = [
    ...editing.channels,
    ...cfg.channels.filter((c) => !assigned.includes(c.id)).map((c) => ({ channelId: c.id, unassigned: true })),
  ];
  const last = editing.channels.length - 1;
  $('u-channels').innerHTML = rows.map((a, i) => `<tr data-id="${a.channelId}">
    <td>${esc(chanName(a.channelId))}</td>
    <td><input type="checkbox" data-f="assigned" ${a.unassigned ? '' : 'checked'}></td>
    <td><input type="checkbox" data-f="talk" ${a.talk ? 'checked' : ''} ${a.unassigned ? 'disabled' : ''}></td>
    <td><input type="checkbox" data-f="listen" ${a.listen ? 'checked' : ''} ${a.unassigned ? 'disabled' : ''}></td>
    <td>${a.unassigned ? '' : `<button type="button" data-move="-1" ${i === 0 ? 'disabled' : ''}>↑</button>
      <button type="button" data-move="1" ${i === last ? 'disabled' : ''}>↓</button>`}</td>
  </tr>`).join('');
}

$('u-channels').onchange = (e) => {
  const id = +e.target.closest('tr').dataset.id, f = e.target.dataset.f;
  if (f === 'assigned') {
    if (e.target.checked) editing.channels.push({ channelId: id, talk: true, listen: true });
    else editing.channels = editing.channels.filter((a) => a.channelId !== id);
  } else {
    editing.channels.find((a) => a.channelId === id)[f] = e.target.checked;
  }
  renderUserChannels();
};

$('u-channels').onclick = (e) => {
  if (!e.target.dataset.move) return;
  const id = +e.target.closest('tr').dataset.id, ch = editing.channels;
  const i = ch.findIndex((a) => a.channelId === id), j = i + Number(e.target.dataset.move);
  [ch[i], ch[j]] = [ch[j], ch[i]];
  renderUserChannels();
};

$('user-cancel').onclick = () => $('user-dialog').close();

$('user-form').onsubmit = async (e) => {
  e.preventDefault();
  const body = {
    name: $('u-name').value,
    password: $('u-pass').value,
    enabled: $('u-enabled').checked,
    allCall: $('u-allcall').checked,
    privateCall: $('u-private').checked,
    channels: editing.channels,
  };
  try {
    await api(editing.id ? 'PUT' : 'POST', editing.id ? 'users/' + editing.id : 'users', body);
    $('user-dialog').close();
    await loadConfig();
  } catch (err) {
    $('user-err').textContent = err.message;
  }
};

// --- settings ---
function renderSettings() {
  const s = cfg.settings;
  $('s-name').value = s.serverName;
  $('s-http').value = s.httpPort;
  $('s-udp').value = s.udpPort;
}

$('settings-form').onsubmit = async (e) => {
  e.preventDefault();
  const ok = await guard(() => api('PUT', 'settings', {
    serverName: $('s-name').value, httpPort: +$('s-http').value, udpPort: +$('s-udp').value,
  }));
  $('s-msg').textContent = ok ? 'Salvato' : '';
  await loadConfig();
};

$('password-form').onsubmit = async (e) => {
  e.preventDefault();
  const ok = await guard(() => api('POST', 'password', { password: $('p-new').value }));
  $('p-msg').textContent = ok ? 'Password cambiata' : '';
  $('p-new').value = '';
};

(async () => {
  const st = await api('GET', 'status');
  if (st.loggedIn) await showApp();
  else showAuth(st.needsSetup);
})();
