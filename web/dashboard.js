/*
 * Kiro-Go dashboard pages: overview widgets, live model catalog, playground.
 * Relies on window.KiroAdmin exposed by app.js.
 */
(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const K = () => window.KiroAdmin;
  const t = (k, ...a) => (K() ? K().t(k, ...a) : k);
  const esc = (s) => (K() ? K().escapeHtml(s) : String(s == null ? '' : s));

  // ---------- shared ----------
  let modelsCache = null; // { models, cachedAt, ttlSeconds, thinkingSuffix }

  async function fetchModels(refresh) {
    const res = await K().api('/models' + (refresh ? '?refresh=1' : ''));
    if (!res.ok) throw new Error('HTTP ' + res.status);
    modelsCache = await res.json();
    return modelsCache;
  }

  function fmtNum(n) {
    if (n == null || n === 0) return '–';
    if (n >= 1e6) return (n / 1e6).toFixed(n % 1e6 === 0 ? 0 : 1) + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(n % 1e3 === 0 ? 0 : 1) + 'K';
    return String(n);
  }

  function timeAgo(unix) {
    if (!unix) return '–';
    const s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
    if (s < 60) return t('time.secondsAgo', s);
    if (s < 3600) return t('time.minutesAgo', Math.floor(s / 60));
    return t('time.hoursAgo', Math.floor(s / 3600));
  }

  // ---------- overview ----------
  async function loadOverview() {
    const base = K().baseUrl;
    const baseEl = $('ovBaseUrl');
    if (baseEl) baseEl.textContent = base + '/v1';
    const curl = $('ovCurl');
    if (curl) {
      curl.textContent =
        'curl ' + base + '/v1/chat/completions \\\n' +
        '  -H "Authorization: Bearer $KIRO_API_KEY" \\\n' +
        '  -H "Content-Type: application/json" \\\n' +
        "  -d '{\"model\":\"claude-sonnet-4.5\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}]}'";
    }
    let logs = [];
    try {
      const res = await K().api('/logs');
      if (res.ok) logs = (await res.json()).logs || [];
    } catch (e) { /* leave empty */ }
    renderChart(logs);
    renderRecent(logs);
  }

  function renderChart(logs) {
    const el = $('ovChart');
    if (!el) return;
    const buckets = 30; // last 30 minutes, one bar per minute
    const now = Math.floor(Date.now() / 1000 / 60);
    const ok = new Array(buckets).fill(0);
    const bad = new Array(buckets).fill(0);
    logs.forEach((l) => {
      const idx = buckets - 1 - (now - Math.floor((l.time || 0) / 60));
      if (idx < 0 || idx >= buckets) return;
      if (l.status === 'success') ok[idx]++; else bad[idx]++;
    });
    const max = Math.max(1, ...ok.map((v, i) => v + bad[i]));
    const total = ok.reduce((a, b) => a + b, 0) + bad.reduce((a, b) => a + b, 0);
    if (!total) {
      el.innerHTML = '<div class="empty-state"><i class="fa-solid fa-chart-column"></i><p>' + esc(t('overview.noTraffic')) + '</p></div>';
      return;
    }
    const w = 100 / buckets;
    let bars = '';
    for (let i = 0; i < buckets; i++) {
      const hOk = (ok[i] / max) * 100;
      const hBad = (bad[i] / max) * 100;
      const x = i * w + w * 0.18;
      const bw = w * 0.64;
      const label = esc(t('overview.barTitle', buckets - 1 - i, ok[i], bad[i]));
      bars += '<g><title>' + label + '</title>' +
        '<rect class="bar-ok" x="' + x + '%" y="' + (100 - hOk) + '%" width="' + bw + '%" height="' + hOk + '%" rx="2"></rect>' +
        (bad[i] ? '<rect class="bar-bad" x="' + x + '%" y="' + (100 - hOk - hBad) + '%" width="' + bw + '%" height="' + hBad + '%" rx="2"></rect>' : '') +
        '</g>';
    }
    el.innerHTML =
      '<svg class="ov-svg" preserveAspectRatio="none" role="img" aria-label="' + esc(t('overview.traffic')) + '">' +
      '<line class="grid" x1="0" x2="100%" y1="50%" y2="50%"></line>' + bars + '</svg>' +
      '<div class="ov-legend"><span><i class="dot dot-ok"></i>' + esc(t('stats.success')) + '</span>' +
      '<span><i class="dot dot-bad"></i>' + esc(t('stats.failed')) + '</span>' +
      '<span class="ov-legend-axis">' + esc(t('overview.axis')) + '</span></div>';
  }

  function renderRecent(logs) {
    const el = $('ovRecent');
    if (!el) return;
    const rows = logs.slice().sort((a, b) => (b.time || 0) - (a.time || 0)).slice(0, 8);
    if (!rows.length) {
      el.innerHTML = '<div class="empty-state"><i class="fa-regular fa-clock"></i><p>' + esc(t('logs.empty')) + '</p></div>';
      return;
    }
    el.innerHTML = '<table class="data-table"><thead><tr>' +
      '<th>' + esc(t('table.time')) + '</th><th>' + esc(t('table.endpoint')) + '</th><th>' + esc(t('table.model')) +
      '</th><th>' + esc(t('table.status')) + '</th><th class="num">' + esc(t('table.tokens')) + '</th><th class="num">' + esc(t('table.latency')) +
      '</th></tr></thead><tbody>' +
      rows.map((l) => '<tr>' +
        '<td class="muted-text">' + esc(timeAgo(l.time)) + '</td>' +
        '<td><span class="chip">' + esc(l.endpoint || '-') + '</span></td>' +
        '<td class="mono">' + esc(l.model || '-') + '</td>' +
        '<td>' + (l.status === 'success'
          ? '<span class="badge badge-ok">' + esc(t('stats.success')) + '</span>'
          : '<span class="badge badge-bad" title="' + esc(l.error || '') + '">' + esc(t('stats.failed')) + '</span>') + '</td>' +
        '<td class="num">' + esc(fmtNum(l.tokens)) + '</td>' +
        '<td class="num">' + esc(l.duration ? (l.duration / 1000).toFixed(1) + 's' : '–') + '</td>' +
        '</tr>').join('') +
      '</tbody></table>';
  }

  // ---------- models ----------
  async function loadModels(refresh) {
    const table = $('modelsTable');
    const btn = $('modelsRefreshBtn');
    if (btn) btn.disabled = true;
    if (table && (!modelsCache || refresh)) table.innerHTML = '<div class="empty-state"><i class="fa-solid fa-spinner fa-spin"></i></div>';
    try {
      await fetchModels(refresh);
      renderModels();
      if (refresh) K().toast(t('models.refreshed'), 'primary');
    } catch (e) {
      if (table) table.innerHTML = '<div class="empty-state"><i class="fa-solid fa-triangle-exclamation"></i><p>' + esc(t('models.loadFailed')) + ' ' + esc(e.message) + '</p></div>';
    } finally {
      if (btn) btn.disabled = false;
    }
  }

  function renderModels() {
    const table = $('modelsTable');
    const meta = $('modelsMeta');
    if (!table || !modelsCache) return;
    const q = ($('modelsSearch') && $('modelsSearch').value || '').trim().toLowerCase();
    const list = (modelsCache.models || []).filter((m) => !q || (m.id + ' ' + (m.name || '')).toLowerCase().includes(q));
    if (meta) {
      meta.textContent = t('models.meta', (modelsCache.models || []).length, timeAgo(modelsCache.cachedAt), Math.round((modelsCache.ttlSeconds || 600) / 60));
    }
    if (!list.length) {
      table.innerHTML = '<div class="empty-state"><i class="fa-solid fa-cubes"></i><p>' + esc(t(modelsCache.models && modelsCache.models.length ? 'models.noMatch' : 'models.empty')) + '</p></div>';
      return;
    }
    table.innerHTML = '<table class="data-table"><thead><tr>' +
      '<th>' + esc(t('table.model')) + '</th><th class="num">' + esc(t('models.context')) + '</th><th class="num">' + esc(t('models.maxOutput')) +
      '</th><th>' + esc(t('models.inputs')) + '</th><th class="num">' + esc(t('models.multiplier')) + '</th><th class="num">' + esc(t('models.accounts')) +
      '</th><th></th></tr></thead><tbody>' +
      list.map((m) => '<tr>' +
        '<td><div class="model-cell"><span class="mono strong">' + esc(m.id) + '</span>' +
        (m.name && m.name !== m.id ? '<span class="muted-text text-xs">' + esc(m.name) + '</span>' : '') + '</div></td>' +
        '<td class="num">' + esc(fmtNum(m.contextLength)) + '</td>' +
        '<td class="num">' + esc(fmtNum(m.maxOutputTokens)) + '</td>' +
        '<td><span class="chip">' + esc(t('models.text')) + '</span>' + (m.vision ? ' <span class="chip chip-accent">' + esc(t('models.vision')) + '</span>' : '') + '</td>' +
        '<td class="num">' + esc(m.rateMultiplier ? m.rateMultiplier + '×' : '–') + '</td>' +
        '<td class="num">' + esc(String(m.accounts || 0)) + '</td>' +
        '<td class="row-actions">' +
        '<button class="btn btn-ghost btn-xs" data-copy-model="' + esc(m.id) + '" title="' + esc(t('common.copy')) + '"><i class="fa-regular fa-copy"></i></button>' +
        '<button class="btn btn-ghost btn-xs" data-try-model="' + esc(m.id) + '" title="' + esc(t('models.try')) + '"><i class="fa-solid fa-flask"></i></button>' +
        '</td></tr>').join('') +
      '</tbody></table>';
  }

  // ---------- playground ----------
  const pg = { messages: [], controller: null };

  async function initPlaygroundModels() {
    const sel = $('pgModel');
    if (!sel) return;
    if (!modelsCache) {
      try { await fetchModels(false); } catch (e) { /* fallback below */ }
    }
    const prev = sel.value || localStorage.getItem('kiro_pg_model') || '';
    const ids = (modelsCache && modelsCache.models || []).map((m) => m.id);
    const suffix = (modelsCache && modelsCache.thinkingSuffix) || '-thinking';
    const options = ids.length ? ids : ['claude-sonnet-4.5', 'claude-haiku-4.5'];
    let html = '<option value="auto">auto</option>';
    options.forEach((id) => {
      html += '<option value="' + esc(id) + '">' + esc(id) + '</option>';
      html += '<option value="' + esc(id + suffix) + '">' + esc(id + suffix) + '</option>';
    });
    sel.innerHTML = html;
    if (prev && Array.from(sel.options).some((o) => o.value === prev)) sel.value = prev;
    else if (options.length) sel.value = options[0];
  }

  function renderMarkdown(text) {
    // Minimal, XSS-safe rendering: escape first, then format.
    let s = esc(text);
    s = s.replace(/```([\w-]*)\n([\s\S]*?)```/g, (_, lang, code) => '<pre class="code-block"><code>' + code + '</code></pre>');
    s = s.replace(/`([^`\n]+)`/g, '<code class="code-inline-sm">$1</code>');
    s = s.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
    s = s.split(/(<pre[\s\S]*?<\/pre>)/g).map((part) => part.startsWith('<pre') ? part : part.replace(/\n/g, '<br>')).join('');
    return s;
  }

  function renderPgMessages() {
    const box = $('pgMessages');
    if (!box) return;
    if (!pg.messages.length) {
      box.innerHTML = '<div class="empty-state pg-empty"><i class="fa-regular fa-comments"></i><p>' + esc(t('playground.empty')) + '</p></div>';
      return;
    }
    box.innerHTML = pg.messages.map((m) => {
      const thinking = m.thinking ? '<details class="pg-thinking"><summary>' + esc(t('playground.thinking')) + '</summary><div>' + renderMarkdown(m.thinking) + '</div></details>' : '';
      const body = m.error ? '<div class="pg-error">' + esc(m.error) + '</div>' : renderMarkdown(m.content || (m.pending ? '…' : ''));
      return '<div class="pg-msg pg-msg--' + m.role + '"><div class="pg-avatar">' +
        (m.role === 'user' ? '<i class="fa-solid fa-user"></i>' : '<i class="fa-solid fa-robot"></i>') +
        '</div><div class="pg-bubble">' + thinking + body + '</div></div>';
    }).join('');
    box.scrollTop = box.scrollHeight;
  }

  function pgRequest() {
    const protocol = $('pgProtocol').value;
    const model = $('pgModel').value;
    const system = $('pgSystem').value.trim();
    const temperature = parseFloat($('pgTemp').value);
    const maxTokens = parseInt($('pgMaxTokens').value, 10) || 1024;
    const stream = $('pgStream').checked;
    const history = pg.messages.filter((m) => !m.error && !m.pending && m.content).map((m) => ({ role: m.role, content: m.content }));
    if (protocol === 'anthropic') {
      const body = { model, max_tokens: maxTokens, temperature, stream, messages: history };
      if (system) body.system = system;
      return { path: '/playground/messages', publicPath: '/v1/messages', body };
    }
    const messages = system ? [{ role: 'system', content: system }].concat(history) : history;
    return { path: '/playground/chat/completions', publicPath: '/v1/chat/completions', body: { model, temperature, max_tokens: maxTokens, stream, messages } };
  }

  async function pgSend() {
    const input = $('pgInput');
    const text = input.value.trim();
    if (!text || pg.controller) return;
    input.value = '';
    pg.messages.push({ role: 'user', content: text });
    const reply = { role: 'assistant', content: '', thinking: '', pending: true };
    pg.messages.push(reply);
    renderPgMessages();
    localStorage.setItem('kiro_pg_model', $('pgModel').value);

    const req = pgRequest();
    const protocol = $('pgProtocol').value;
    pg.controller = new AbortController();
    $('pgSendBtn').classList.add('hidden');
    $('pgStopBtn').classList.remove('hidden');
    const started = performance.now();
    let firstToken = 0;
    let usage = null;
    try {
      const res = await K().api(req.path, { method: 'POST', body: JSON.stringify(req.body), signal: pg.controller.signal });
      if (!res.ok) {
        let msg = 'HTTP ' + res.status;
        try { const j = await res.json(); msg = (j.error && (j.error.message || j.error)) || msg; } catch (e) { /* not JSON */ }
        throw new Error(msg);
      }
      if (!req.body.stream) {
        const j = await res.json();
        if (protocol === 'anthropic') {
          (j.content || []).forEach((b) => { if (b.type === 'text') reply.content += b.text; if (b.type === 'thinking') reply.thinking += b.thinking || ''; });
          usage = j.usage && { in: j.usage.input_tokens, out: j.usage.output_tokens };
        } else {
          const msg = (j.choices && j.choices[0] && j.choices[0].message) || {};
          reply.content = msg.content || '';
          reply.thinking = msg.reasoning_content || '';
          usage = j.usage && { in: j.usage.prompt_tokens, out: j.usage.completion_tokens };
        }
        firstToken = performance.now();
      } else {
        const reader = res.body.getReader();
        const dec = new TextDecoder();
        let buf = '';
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += dec.decode(value, { stream: true });
          let idx;
          while ((idx = buf.indexOf('\n')) >= 0) {
            const line = buf.slice(0, idx).trim();
            buf = buf.slice(idx + 1);
            if (!line.startsWith('data:')) continue;
            const data = line.slice(5).trim();
            if (!data || data === '[DONE]') continue;
            let ev;
            try { ev = JSON.parse(data); } catch (e) { continue; }
            if (protocol === 'anthropic') {
              if (ev.type === 'content_block_delta' && ev.delta) {
                if (ev.delta.type === 'text_delta') reply.content += ev.delta.text || '';
                if (ev.delta.type === 'thinking_delta') reply.thinking += ev.delta.thinking || '';
              } else if (ev.type === 'message_delta' && ev.usage) {
                usage = Object.assign(usage || {}, { out: ev.usage.output_tokens });
              } else if (ev.type === 'message_start' && ev.message && ev.message.usage) {
                usage = { in: ev.message.usage.input_tokens };
              } else if (ev.type === 'error') {
                throw new Error((ev.error && ev.error.message) || 'stream error');
              }
            } else {
              const d = ev.choices && ev.choices[0] && ev.choices[0].delta;
              if (d) {
                reply.content += d.content || '';
                reply.thinking += d.reasoning_content || '';
              }
              if (ev.usage) usage = { in: ev.usage.prompt_tokens, out: ev.usage.completion_tokens };
              if (ev.error) throw new Error(ev.error.message || 'stream error');
            }
            if (!firstToken && (reply.content || reply.thinking)) firstToken = performance.now();
            reply.pending = false;
            renderPgMessages();
          }
        }
      }
    } catch (e) {
      if (e.name === 'AbortError') {
        if (!reply.content) reply.error = t('playground.stopped');
      } else {
        reply.error = e.message || String(e);
      }
    } finally {
      reply.pending = false;
      pg.controller = null;
      $('pgSendBtn').classList.remove('hidden');
      $('pgStopBtn').classList.add('hidden');
      const total = (performance.now() - started) / 1000;
      const ttft = firstToken ? ((firstToken - started) / 1000).toFixed(2) + 's' : '–';
      const parts = [t('playground.ttft') + ' ' + ttft, t('playground.total') + ' ' + total.toFixed(2) + 's'];
      if (usage && (usage.in || usage.out)) parts.push((usage.in || 0) + ' → ' + (usage.out || 0) + ' ' + t('stats.tokens'));
      $('pgMetrics').textContent = parts.join(' · ');
      renderPgMessages();
    }
  }

  function pgCurl() {
    const req = pgRequest();
    const body = Object.assign({}, req.body);
    if (!body.messages || !body.messages.length) body.messages = [{ role: 'user', content: 'Hello' }];
    const isAnthropic = $('pgProtocol').value === 'anthropic';
    const cmd = 'curl ' + K().baseUrl + req.publicPath + ' \\\n' +
      (isAnthropic ? '  -H "x-api-key: $KIRO_API_KEY" \\\n  -H "anthropic-version: 2023-06-01" \\\n' : '  -H "Authorization: Bearer $KIRO_API_KEY" \\\n') +
      '  -H "Content-Type: application/json" \\\n' +
      "  -d '" + JSON.stringify(body).replace(/'/g, "'\\''") + "'";
    K().copyText(cmd).then(() => K().toast(t('common.copied'), 'primary'));
  }

  // ---------- wiring ----------
  function onTab(tab) {
    if (tab === 'overview') loadOverview();
    if (tab === 'models') loadModels(false);
    if (tab === 'playground') { initPlaygroundModels(); renderPgMessages(); }
  }

  function wire() {
    document.addEventListener('kiro:tab', (e) => onTab(e.detail.tab));
    document.addEventListener('kiro:lang', () => {
      renderModels();
      renderPgMessages();
      const active = document.querySelector('#tabBar .tab.active');
      if (active && active.dataset.tab === 'overview') loadOverview();
    });
    document.body.addEventListener('click', (e) => {
      const go = e.target.closest('[data-goto]');
      if (go) K().switchTab(go.dataset.goto);
      const cp = e.target.closest('[data-copy-model]');
      if (cp) K().copyText(cp.dataset.copyModel).then(() => K().toast(t('common.copied'), 'primary'));
      const tr = e.target.closest('[data-try-model]');
      if (tr) {
        localStorage.setItem('kiro_pg_model', tr.dataset.tryModel);
        K().switchTab('playground');
      }
    });
    const on = (id, ev, fn) => { const el = $(id); if (el) el.addEventListener(ev, fn); };
    on('modelsRefreshBtn', 'click', () => loadModels(true));
    on('modelsSearch', 'input', renderModels);
    on('pgSendBtn', 'click', pgSend);
    on('pgStopBtn', 'click', () => pg.controller && pg.controller.abort());
    on('pgClearBtn', 'click', () => { pg.messages = []; $('pgMetrics').textContent = ''; renderPgMessages(); });
    on('pgCurlBtn', 'click', pgCurl);
    on('pgTemp', 'input', () => { $('pgTempVal').textContent = $('pgTemp').value; });
    on('pgInput', 'keydown', (e) => {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); pgSend(); }
    });
    on('sidebarToggle', 'click', () => document.body.classList.toggle('sidebar-open'));
    on('sidebarBackdrop', 'click', () => document.body.classList.remove('sidebar-open'));
    // Refresh overview widgets periodically while visible.
    setInterval(() => {
      const main = $('mainPage');
      const active = document.querySelector('#tabBar .tab.active');
      if (main && !main.classList.contains('hidden') && active && active.dataset.tab === 'overview') loadOverview();
    }, 15000);
  }

  function start() {
    wire();
    // app.js may already have shown the main page before this script loaded.
    const active = document.querySelector('#tabBar .tab.active');
    const main = $('mainPage');
    if (K() && main && !main.classList.contains('hidden') && active) onTab(active.dataset.tab);
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', start);
  else start();
})();
