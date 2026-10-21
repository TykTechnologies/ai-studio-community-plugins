// Prompt Compressor: savings dashboard.
//
// Reads hourly rollups with getSavings. Applied savings (enforce) and
// projected ones (audit) are kept apart; estimated cost is shown at the
// input price, which is an upper bound, next to the cache-read share and,
// when the data allows, the cache-adjusted figure. Every value from the
// plugin (App, LLM, policy and strategy names come from edges) is written
// with textContent or as SVG text, never innerHTML.
//
// Method names start with "_" so none shadows an HTMLElement member.

const RANGES = [
  { hours: 24, label: 'Last 24 hours' },
  { hours: 168, label: 'Last 7 days' },
  { hours: 720, label: 'Last 30 days' },
  { hours: 2160, label: 'Last 90 days' },
];
const SVG_NS = 'http://www.w3.org/2000/svg';
const REFRESH_MS = 60000;

function h(tag, props, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === undefined || value === null) continue;
    if (key.startsWith('on') && typeof value === 'function') {
      el.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (key === 'className') {
      el.className = value;
    } else if (key.startsWith('data-') || key.startsWith('aria-') || key === 'role') {
      el.setAttribute(key, String(value));
    } else {
      el[key] = value;
    }
  }
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    el.appendChild(typeof child === 'string' || typeof child === 'number' ? document.createTextNode(String(child)) : child);
  }
  return el;
}

function svg(tag, attrs, ...children) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [key, value] of Object.entries(attrs || {})) {
    if (value !== undefined && value !== null) el.setAttribute(key, String(value));
  }
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    el.appendChild(typeof child === 'string' || typeof child === 'number' ? document.createTextNode(String(child)) : child);
  }
  return el;
}

const fmtInt = (n) => Number(n || 0).toLocaleString();
const fmtPct = (x) => `${(x * 100).toFixed(1)}%`;
const hourLabel = (hour) => new Date(hour * 3600 * 1000).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
const fmtMicros = (us) => (us < 0 ? '> 100 ms' : us >= 1000 ? `≤ ${(us / 1000).toFixed(us % 1000 ? 1 : 0)} ms` : `≤ ${us} µs`);

function fmtMoney(map) {
  const entries = Object.entries(map || {}).filter(([, v]) => v > 0);
  if (entries.length === 0) return '—';
  return entries
    .map(([currency, v]) => {
      const digits = v < 1 ? 4 : 2;
      return currency === 'XXX' ? `${v.toFixed(digits)} (other currencies)` : `${v.toFixed(digits)} ${currency}`;
    })
    .join(' + ');
}

// statusWarnings reads what the plugin says is wrong right now (getStatus):
// its warnings and a rejected configuration. A user who may not call
// getStatus gets none.
async function statusWarnings(api) {
  try {
    const status = await api.call('getStatus', {});
    const out = [...(status.warnings || [])];
    if (status.config_error) out.push(`Configuration invalid, compression is off: ${status.config_error}`);
    return out;
  } catch (error) {
    return [];
  }
}

function share(cached, input) {
  return input > 0 ? fmtPct(cached / input) : '—';
}

class PromptCompressorDashboard extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this._range = 24;
    this._appId = 0;
    this._llmId = 0;
    this._view = null;
    this._error = '';
    this._loading = true;
    this._timer = null;
    this._appChoices = [];
    this._llmChoices = [];
    this._choicesRange = 0;
  }

  connectedCallback() {
    this._paint();
    this._waitForPluginAPI();
  }

  disconnectedCallback() {
    if (this._timer) clearInterval(this._timer);
    this._timer = null;
  }

  async _waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) {
        await this._load();
        this._timer = setInterval(() => this._load(), REFRESH_MS);
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    this._error = 'Plugin API initialization timeout - please refresh the page';
    this._loading = false;
    this._paint();
  }

  async _load() {
    this._warnings = await statusWarnings(this.pluginAPI);
    try {
      const view = await this.pluginAPI.call('getSavings', { range_hours: this._range, app_id: this._appId, llm_id: this._llmId });
      this._view = view;
      this._error = '';
      // Filter choices come from the unfiltered view of the range, so they
      // neither shrink to the one picked nor miss Apps a new range adds
      let all = view;
      if ((this._appId || this._llmId) && this._choicesRange !== this._range) {
        all = await this.pluginAPI.call('getSavings', { range_hours: this._range });
      }
      if (!this._appId || all !== view) this._appChoices = (all.by_app || []).filter((g) => !g.overflow);
      if (!this._llmId || all !== view) this._llmChoices = (all.by_llm || []).filter((g) => !g.overflow);
      if ((!this._appId && !this._llmId) || all !== view) this._choicesRange = this._range;
    } catch (error) {
      this._error = 'Failed to load savings: ' + (error && error.message ? error.message : error);
    } finally {
      this._loading = false;
      this._paint();
    }
  }

  _setFilter(changes) {
    Object.assign(this, changes);
    this._loading = true;
    this._paint();
    this._load();
  }

  // ---- painting ----

  _paint() {
    const root = this.shadowRoot;
    root.textContent = '';
    root.appendChild(h('style', {}, STYLES));
    const page = h('div', { className: 'page' });
    root.appendChild(page);
    page.appendChild(h('h1', {}, 'Compression savings'));
    if (this._warnings && this._warnings.length) {
      page.appendChild(h('div', { className: 'banner warning', role: 'status', 'data-testid': 'pc-status-warnings' },
        this._warnings.map((w) => h('div', {}, h('span', { className: 'icon', 'aria-hidden': 'true' }, '⚠'), w))));
    }
    page.appendChild(this._paintControls());

    if (this._error) page.appendChild(h('div', { className: 'error', role: 'alert' }, this._error));
    if (this._loading && !this._view) {
      page.appendChild(h('p', { className: 'muted' }, 'Loading…'));
      return;
    }
    const v = this._view;
    if (!v) return;

    for (const a of v.alerts || []) {
      page.appendChild(
        h('div', { className: 'banner warning', role: 'alert', 'data-testid': 'pc-cache-alert' },
          h('span', { className: 'icon', 'aria-hidden': 'true' }, '⚠'),
          h('strong', {}, 'Cache-read share dropped. '),
          `It fell from ${fmtPct(a.before)} to ${fmtPct(a.after)} within an hour of policy "${a.policy || 'unnamed'}" taking effect (${hourLabel(a.hour)}). `,
          'A policy change makes every affected session miss the prompt cache once; a lasting drop means the compressed prefix is not stable.'),
      );
    }
    if (v.empty) {
      page.appendChild(
        h('div', { className: 'empty', 'data-testid': 'pc-empty' },
          h('strong', {}, 'No savings recorded in this range yet.'),
          h('p', {}, 'Edges send their records about once a minute, and Studio receives them with the edge\'s next heartbeat. Check that the plugin is attached to an LLM and its mode is audit or enforce.')),
      );
      return;
    }
    const scopeMissing = (v.by_reason || {}).app_scope_missing || 0;
    if (scopeMissing > 0) {
      page.appendChild(
        h('div', { className: 'banner warning', role: 'alert', 'data-testid': 'pc-app-scope-missing' },
          h('span', { className: 'icon', 'aria-hidden': 'true' }, '⚠'),
          h('strong', {}, 'App traffic is not compressed. '),
          `${fmtInt(scopeMissing)} requests were forwarded uncompressed because the plugin could not read their App's settings: its apps.read scope is not approved. `,
          'Approve the plugin\'s scopes on the Plugins page.'),
      );
    }
    if (v.partial) {
      page.appendChild(
        h('div', { className: 'banner info', 'data-testid': 'pc-partial' },
          h('span', { className: 'icon', 'aria-hidden': 'true' }, 'ℹ'),
          h('strong', {}, 'Partial view: '),
          `this range holds more data than one view reads, so it covers ${hourLabel(v.partial_from)} onwards. Pick a shorter range, or an App or LLM, for the rest.`),
      );
    }
    if (v.audit_only) {
      page.appendChild(
        h('div', { className: 'banner info', 'data-testid': 'pc-audit-only' },
          h('span', { className: 'icon', 'aria-hidden': 'true' }, 'ℹ'),
          h('strong', {}, 'Audit mode: '),
          'every saving below is projected. Requests were forwarded unchanged; switch the mode to enforce on the Policies page to save them.'),
      );
    }
    page.appendChild(this._paintTiles(v));
    page.appendChild(this._paintSavingsChart(v));
    page.appendChild(this._paintShareChart(v));
    page.appendChild(this._paintGroups('By App', v.by_app, 'App', 'pc-by-app'));
    page.appendChild(this._paintGroups('By LLM', v.by_llm, 'LLM', 'pc-by-llm'));
    page.appendChild(this._paintTools(v.tools));
    page.appendChild(this._paintStrategies(v.strategies));
    page.appendChild(this._paintReasons(v.by_reason));
    if (v.overflow_excluded > 0) {
      page.appendChild(h('p', { className: 'muted' }, `${fmtInt(v.overflow_excluded)} requests in overflow rows cannot be attributed to an App or LLM and are left out of this filter.`));
    }
    page.appendChild(h('p', { className: 'muted' }, 'Token counts are estimates (tiktoken for OpenAI models, a per-vendor ratio otherwise). Updated every minute.'));
  }

  _paintControls() {
    const option = (value, label, selected) => h('option', { value: String(value), selected }, label);
    return h('div', { className: 'controls' },
      h('label', {}, 'Range ',
        h('select', { 'data-testid': 'pc-range', onChange: (e) => this._setFilter({ _range: Number(e.target.value) }) },
          RANGES.map((r) => option(r.hours, r.label, r.hours === this._range)))),
      h('label', {}, 'App ',
        h('select', { onChange: (e) => this._setFilter({ _appId: Number(e.target.value) }) },
          option(0, 'All Apps', !this._appId),
          this._appChoices.map((g) => option(g.id, g.name || `App #${g.id}`, g.id === this._appId)))),
      h('label', {}, 'LLM ',
        h('select', { onChange: (e) => this._setFilter({ _llmId: Number(e.target.value) }) },
          option(0, 'All LLMs', !this._llmId),
          this._llmChoices.map((g) => option(g.id, g.name || `LLM #${g.id}`, g.id === this._llmId)))),
      h('button', { onClick: () => this._setFilter({}) }, 'Refresh'),
      this._loading ? h('span', { className: 'muted' }, 'Updating…') : null);
  }

  _paintTiles(v) {
    const t = v.totals;
    const tile = (label, value, sub, testid) =>
      h('div', { className: 'tile', 'data-testid': testid },
        h('div', { className: 'tile-label' }, label),
        h('div', { className: 'tile-value' }, value),
        sub ? h('div', { className: 'tile-sub' }, sub) : null);
    const outcomes = Object.entries(v.by_outcome || {}).sort((a, b) => b[1] - a[1]).map(([k, n]) => `${k} ${fmtInt(n)}`).join(' · ');

    const tiles = [
      tile('Tokens saved (estimate)', fmtInt(t.tokens_saved), 'Enforce mode: removed from the requests sent', 'pc-tokens-saved'),
    ];
    if (t.projected_tokens_saved > 0) {
      tiles.push(tile('Projected tokens saved', fmtInt(t.projected_tokens_saved), `Audit mode: would have been removed · ${fmtMoney(t.projected_cost_at_input)} at input price`, 'pc-projected'));
    }
    tiles.push(
      tile('Cost saved at input price', fmtMoney(t.cost_at_input),
        'Upper bound: saved tokens that would have been cache reads were worth less' + (t.unpriced_tokens_saved > 0 ? ` · ${fmtInt(t.unpriced_tokens_saved)} tokens without a price` : ''), 'pc-cost-upper'),
      tile('Cost saved, cache-adjusted', t.cost_cache_adjusted ? fmtMoney(t.cost_cache_adjusted) : '—',
        t.cost_cache_adjusted ? 'Saved tokens priced at the cache-read price in proportion to the cache-read share' : (t.cache_adjusted_note || 'Nothing saved in this range'), 'pc-cost-adjusted'),
      tile('Cache-read share', t.share_known ? fmtPct(t.cache_read_share) : '—',
        t.share_known ? `${fmtInt(t.cache_read_tokens)} of ${fmtInt(t.input_tokens)} prompt tokens were cache reads` : 'No usage reported by the vendor', 'pc-cache-share'),
      tile('Requests', fmtInt(t.requests), outcomes + (t.usage_missing > 0 ? ` · ${fmtInt(t.usage_missing)} without usage` : ''), 'pc-requests'),
      tile('Added latency', `${fmtMicros(t.latency_p50_micros)} p50`, `${fmtMicros(t.latency_p95_micros)} p95 (compression time)`, 'pc-latency'),
      tile('Rejected / failed open', `${fmtInt(t.rejected)} / ${fmtInt(t.failed_open)}`, 'Forwarded unchanged: a rewrite failed its check, or the plugin hit an error', 'pc-failures'),
    );
    if (t.failed_closed > 0) {
      tiles.push(tile('Refused (engine failed, closed)', fmtInt(t.failed_closed), 'Answered 502: the external engine failed and failure_mode is closed', 'pc-failed-closed'));
    }
    return h('div', { className: 'tiles' }, tiles);
  }

  // _hours lists every hour of the range, so gaps show as zero.
  // _hours lists the start hour of every series point (buckets of
  // bucket_hours), so gaps show as zero.
  _hours(v) {
    const step = Math.max(1, v.bucket_hours || 1);
    const out = [];
    for (let hr = v.from; hr <= v.to; hr += step) out.push(hr);
    return out;
  }

  _per(v) {
    const b = v.bucket_hours || 1;
    return b === 1 ? 'per hour' : b === 24 ? 'per day' : `per ${b} hours`;
  }

  _chartFrame(title, legend) {
    return h('section', { className: 'chart' },
      h('h2', {}, title),
      legend ? h('div', { className: 'legend' }, legend) : null);
  }

  _tooltip(section) {
    const tip = h('div', { className: 'tooltip', role: 'status' });
    tip.style.display = 'none';
    section.appendChild(tip);
    return {
      show: (x, lines) => {
        tip.textContent = '';
        lines.forEach((line, i) => tip.appendChild(h(i === 0 ? 'strong' : 'div', {}, line)));
        tip.style.display = 'block';
        tip.style.left = `${Math.max(0, x)}px`;
      },
      hide: () => { tip.style.display = 'none'; },
    };
  }

  _paintSavingsChart(v) {
    const hasProjected = (v.series || []).some((p) => p.projected_tokens_saved > 0);
    const legend = [
      h('span', { className: 'key' }, h('span', { className: 'swatch s1' }), 'Saved (enforce)'),
      hasProjected ? h('span', { className: 'key' }, h('span', { className: 'swatch s2' }), 'Projected (audit)') : null,
      h('span', { className: 'key' }, h('span', { className: 'swatch marker' }), 'Policy change'),
    ];
    const section = this._chartFrame(`Tokens saved ${this._per(v)}`, legend);
    const byHour = new Map((v.series || []).map((p) => [p.hour, p]));
    const hours = this._hours(v);
    const W = 900, H = 200, L = 56, R = 8, T = 8, B = 24;
    const max = Math.max(1, ...hours.map((hr) => { const p = byHour.get(hr); return p ? p.tokens_saved + p.projected_tokens_saved : 0; }));
    const step = (W - L - R) / hours.length;
    const bw = Math.max(1, step - 2);
    const y = (n) => T + (H - T - B) * (1 - n / max);
    const chart = svg('svg', { viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': `Tokens saved ${this._per(v)}`, 'data-testid': 'pc-savings-chart' });
    // Recessive grid: baseline and top value
    chart.appendChild(svg('line', { x1: L, x2: W - R, y1: y(0), y2: y(0), class: 'axis' }));
    chart.appendChild(svg('text', { x: L - 6, y: y(max) + 4, class: 'tick', 'text-anchor': 'end' }, fmtInt(max)));
    chart.appendChild(svg('text', { x: L - 6, y: y(0) + 4, class: 'tick', 'text-anchor': 'end' }, '0'));
    const tip = this._tooltip(section);

    hours.forEach((hr, i) => {
      const p = byHour.get(hr) || { tokens_saved: 0, projected_tokens_saved: 0, requests: 0 };
      const x = L + i * step + 1;
      const g = svg('g', {});
      if (p.tokens_saved > 0) g.appendChild(svg('rect', { x, y: y(p.tokens_saved), width: bw, height: Math.max(0, y(0) - y(p.tokens_saved)), class: 'bar s1', rx: Math.min(2, bw / 2) }));
      if (p.projected_tokens_saved > 0) {
        const top = p.tokens_saved + p.projected_tokens_saved;
        // A 2px surface gap separates the stacked segments
        g.appendChild(svg('rect', { x, y: y(top), width: bw, height: Math.max(0, y(p.tokens_saved) - y(top) - (p.tokens_saved > 0 ? 2 : 0)), class: 'bar s2', rx: Math.min(2, bw / 2) }));
      }
      // Hit target: the whole column, wider than the mark
      const hit = svg('rect', { x: L + i * step, y: T, width: step, height: H - T - B, class: 'hit' });
      hit.addEventListener('mouseenter', () => {
        const lines = [hourLabel(hr), `Saved: ${fmtInt(p.tokens_saved)} tokens`];
        if (hasProjected) lines.push(`Projected: ${fmtInt(p.projected_tokens_saved)} tokens`);
        lines.push(`Requests: ${fmtInt(p.requests)}`);
        tip.show(((L + i * step) / W) * section.clientWidth, lines);
      });
      hit.addEventListener('mouseleave', tip.hide);
      g.appendChild(hit);
      chart.appendChild(g);
    });
    this._paintMarkers(chart, v, hours, L, step, T, H - B);
    this._paintTimeAxis(chart, hours, L, step, H);
    section.appendChild(chart);
    return section;
  }

  _paintShareChart(v) {
    const section = this._chartFrame(`Cache-read share ${this._per(v)}`, [
      h('span', { className: 'key' }, h('span', { className: 'swatch s1 line' }), 'Cache reads as a share of prompt tokens'),
      h('span', { className: 'key' }, h('span', { className: 'swatch marker' }), 'Policy change'),
    ]);
    const byHour = new Map((v.series || []).map((p) => [p.hour, p]));
    const hours = this._hours(v);
    const W = 900, H = 160, L = 56, R = 8, T = 8, B = 24;
    const step = (W - L - R) / hours.length;
    const y = (f) => T + (H - T - B) * (1 - f);
    const chart = svg('svg', { viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': `Cache-read share ${this._per(v)}`, 'data-testid': 'pc-share-chart' });
    for (const f of [0, 0.5, 1]) {
      chart.appendChild(svg('line', { x1: L, x2: W - R, y1: y(f), y2: y(f), class: f === 0 ? 'axis' : 'grid' }));
      chart.appendChild(svg('text', { x: L - 6, y: y(f) + 4, class: 'tick', 'text-anchor': 'end' }, `${f * 100}%`));
    }
    const tip = this._tooltip(section);
    // Hours without usage break the line rather than read as 0%
    let path = '';
    let pen = false;
    hours.forEach((hr, i) => {
      const p = byHour.get(hr);
      const cx = L + i * step + step / 2;
      if (p && p.input_tokens > 0) {
        const f = p.cache_read_tokens / p.input_tokens;
        path += `${pen ? 'L' : 'M'}${cx.toFixed(1)},${y(f).toFixed(1)} `;
        pen = true;
        if (hours.length <= 48) chart.appendChild(svg('circle', { cx, cy: y(f), r: 4, class: 'dot s1' }));
      } else {
        pen = false;
      }
      const hit = svg('rect', { x: L + i * step, y: T, width: step, height: H - T - B, class: 'hit' });
      hit.addEventListener('mouseenter', () => {
        tip.show(((L + i * step) / W) * section.clientWidth, [hourLabel(hr), p && p.input_tokens > 0
          ? `${share(p.cache_read_tokens, p.input_tokens)} (${fmtInt(p.cache_read_tokens)} of ${fmtInt(p.input_tokens)} tokens)`
          : 'No usage reported']);
      });
      hit.addEventListener('mouseleave', tip.hide);
      chart.appendChild(hit);
    });
    if (path) chart.insertBefore(svg('path', { d: path.trim(), class: 'line s1' }), chart.querySelector('.hit'));
    this._paintMarkers(chart, v, hours, L, step, T, H - B);
    this._paintTimeAxis(chart, hours, L, step, H);
    section.appendChild(chart);
    return section;
  }

  _paintMarkers(chart, v, hours, L, step, top, bottom) {
    for (const m of v.markers || []) {
      // Within its bucket, the marker sits at the hour it was first seen
      const bucket = Math.max(1, v.bucket_hours || 1);
      const offset = (m.hour - v.from) / bucket;
      if (offset < 0 || offset >= hours.length) continue;
      const x = L + offset * step;
      const g = svg('g', { class: 'marker', 'data-testid': 'pc-policy-marker' },
        svg('line', { x1: x, x2: x, y1: top, y2: bottom }),
        svg('title', {}, `Policy ${m.name || m.short_hash} (${m.short_hash}, level ${m.level}) first seen ${hourLabel(m.hour)}`));
      chart.appendChild(g);
    }
  }

  _paintTimeAxis(chart, hours, L, step, H) {
    const ticks = Math.min(6, hours.length);
    for (let k = 0; k < ticks; k++) {
      const i = Math.round((k * (hours.length - 1)) / Math.max(1, ticks - 1));
      chart.appendChild(svg('text', { x: L + i * step + step / 2, y: H - 6, class: 'tick', 'text-anchor': k === 0 ? 'start' : k === ticks - 1 ? 'end' : 'middle' }, hourLabel(hours[i])));
    }
  }

  _paintGroups(title, groups, kind, testid) {
    const section = h('section', {}, h('h2', {}, title));
    if (!groups || groups.length === 0) {
      section.appendChild(h('p', { className: 'muted' }, 'Nothing in this range.'));
      return section;
    }
    const table = h('table', { 'data-testid': testid },
      h('thead', {}, h('tr', {}, [kind, 'Requests', 'Tokens saved', 'Projected (audit)', 'Cost at input price', 'Cache-read share'].map((c, i) => h('th', { className: i ? 'num' : '' }, c)))));
    const body = h('tbody');
    for (const g of groups) {
      const name = g.overflow ? 'Other (row limit reached)' : (g.name || `${kind} #${g.id}`);
      body.appendChild(h('tr', {},
        h('td', {}, name),
        h('td', { className: 'num' }, fmtInt(g.requests)),
        h('td', { className: 'num' }, fmtInt(g.tokens_saved)),
        h('td', { className: 'num' }, fmtInt(g.projected_tokens_saved)),
        h('td', { className: 'num' }, fmtMoney(g.cost_at_input)),
        h('td', { className: 'num' }, share(g.cache_read_tokens, g.input_tokens))));
    }
    table.appendChild(body);
    section.appendChild(table);
    return section;
  }

  _paintReasons(reasons) {
    const section = h('section', {}, h('h2', {}, 'Not compressed, by reason'));
    const entries = Object.entries(reasons || {}).sort((a, b) => b[1] - a[1]);
    if (entries.length === 0) {
      section.appendChild(h('p', { className: 'muted' }, 'Every request in this range was compressed or had nothing to compress.'));
      return section;
    }
    const table = h('table', { 'data-testid': 'pc-reasons' },
      h('thead', {}, h('tr', {}, ['Reason', 'Requests'].map((c, i) => h('th', { className: i ? 'num' : '' }, c)))));
    const body = h('tbody');
    for (const [name, n] of entries) {
      body.appendChild(h('tr', {}, h('td', {}, h('code', {}, name)), h('td', { className: 'num' }, fmtInt(n))));
    }
    table.appendChild(body);
    section.appendChild(table);
    return section;
  }

  // _paintTools shows what each App's tool definitions cost and which
  // offered tools its models never called. Tool names come from clients,
  // so they are text nodes like every other name here.
  _paintTools(apps) {
    const section = h('section', { 'data-testid': 'pc-tools' }, h('h2', {}, 'Tool definitions'),
      h('p', { className: 'muted' },
        'Every request carries the definitions of the tools it offers. Tools that were offered but never called are candidates to drop; '
        + 'the saving is estimated at the input price and at the cache-read price, since definitions usually sit in the cached prefix. '
        + 'Changing a tool list makes the prompt cache miss once.'));
    if (!apps || apps.length === 0) {
      section.appendChild(h('p', { className: 'muted' }, 'No request in this range offered tools.'));
      return section;
    }
    const table = h('table', { 'data-testid': 'pc-tools-table' },
      h('thead', {}, h('tr', {}, ['App', 'Requests with tools', 'Definition tokens', 'Share of prompt tokens', 'Tool lists', 'Never called', 'Estimated saving']
        .map((c, i) => h('th', { className: i ? 'num' : '' }, c)))));
    const body = h('tbody');
    for (const a of apps) {
      const never = a.never_called || [];
      const saving = fmtMoney(a.estimated_saving);
      const atCache = fmtMoney(a.estimated_saving_at_cache_read);
      body.appendChild(h('tr', { 'data-testid': 'pc-tools-app' },
        h('td', {}, a.name || `App #${a.id}`),
        h('td', { className: 'num' }, fmtInt(a.requests)),
        h('td', { className: 'num', title: `${fmtInt(a.tool_defs)} definitions in all` }, fmtInt(a.tool_def_tokens)),
        h('td', { className: 'num' }, a.share_known ? fmtPct(a.share) : '—'),
        h('td', { className: 'num', title: 'Distinct tool lists offered; each change misses the prompt cache once' }, fmtInt(a.tool_lists)),
        h('td', { className: 'num' }, never.length ? `${fmtInt(never.length)} (${fmtInt(a.never_called_def_tokens)} tokens)` : '0'),
        h('td', { className: 'num' }, never.length ? `${saving} at input · ${atCache} at cache read` : '—')));
      body.appendChild(h('tr', { className: 'detail' }, h('td', { colSpan: 7 }, this._paintToolList(a))));
    }
    table.appendChild(body);
    section.appendChild(table);
    return section;
  }

  _paintToolList(a) {
    const never = new Set(a.never_called || []);
    const tools = a.tools || [];
    const details = h('details', { 'data-testid': 'pc-tools-list' },
      h('summary', {}, never.size
        ? `${fmtInt(tools.length)} tools · never called: ${[...never].slice(0, 5).join(', ')}${never.size > 5 ? ` and ${fmtInt(never.size - 5)} more` : ''}`
        : `${fmtInt(tools.length)} tools, all called`));
    const table = h('table', {},
      h('thead', {}, h('tr', {}, ['Tool', 'Offered', 'Called', 'Definition tokens'].map((c, i) => h('th', { className: i ? 'num' : '' }, c)))));
    const body = h('tbody');
    for (const t of tools) {
      body.appendChild(h('tr', never.has(t.name) ? { 'data-testid': 'pc-tool-never-called' } : {},
        h('td', {}, t.name === 'other' ? 'Other (names not listed)' : h('code', {}, t.name), never.has(t.name) ? h('span', { className: 'tag' }, 'never called') : null),
        h('td', { className: 'num' }, fmtInt(t.offered)),
        h('td', { className: 'num' }, fmtInt(t.called)),
        h('td', { className: 'num' }, fmtInt(t.def_tokens))));
    }
    table.appendChild(body);
    details.appendChild(table);
    return details;
  }

  _paintStrategies(strategies) {
    const section = h('section', {}, h('h2', {}, 'By strategy'));
    const entries = Object.entries(strategies || {}).sort((a, b) => b[1].bytes_saved - a[1].bytes_saved);
    if (entries.length === 0) {
      section.appendChild(h('p', { className: 'muted' }, 'No strategy has removed anything in this range.'));
      return section;
    }
    const total = entries.reduce((n, [, s]) => n + s.bytes_saved, 0) || 1;
    const table = h('table', { 'data-testid': 'pc-strategies' },
      h('thead', {}, h('tr', {}, ['Strategy', 'Texts changed', 'Bytes saved', 'Share'].map((c, i) => h('th', { className: i ? 'num' : '' }, c)))));
    const body = h('tbody');
    for (const [name, s] of entries) {
      body.appendChild(h('tr', {},
        h('td', {}, h('code', {}, name)),
        h('td', { className: 'num' }, fmtInt(s.leaves)),
        h('td', { className: 'num' }, fmtInt(s.bytes_saved)),
        h('td', { className: 'num' }, fmtPct(s.bytes_saved / total))));
    }
    table.appendChild(body);
    section.appendChild(table);
    return section;
  }
}

const STYLES = `
  :host {
    display: block;
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    --surface: #ffffff;
    --surface-2: #f9fafb;
    --text-primary: #111827;
    --text-secondary: #4b5563;
    --text-muted: #6b7280;
    --border: #e5e7eb;
    --grid: #eef0f3;
    --series-1: #2a78d6;
    --series-2: #eb6834;
    --marker: #6b7280;
    --warning-bg: #fffbeb; --warning-fg: #92400e; --warning-border: #fde68a;
    --info-bg: #eff6ff; --info-fg: #1e40af; --info-border: #bfdbfe;
    color: var(--text-primary);
  }
  @media (prefers-color-scheme: dark) {
    :host-context(:root:not([data-theme="light"])) {
      --surface: #1a1a19; --surface-2: #222220; --text-primary: #f0efec; --text-secondary: #c3c2b7;
      --text-muted: #a3a29a; --border: #33332f; --grid: #2a2a27; --series-1: #3987e5; --series-2: #d95926;
      --marker: #a3a29a;
      --warning-bg: #2b2310; --warning-fg: #f5d48a; --warning-border: #5c4614;
      --info-bg: #102035; --info-fg: #a9c8f5; --info-border: #1e3a5f;
    }
  }
  .page { padding: 24px; max-width: 1200px; background: var(--surface); }
  h1 { font-size: 22px; font-weight: 600; margin: 0 0 12px; }
  h2 { font-size: 15px; font-weight: 600; margin: 0 0 8px; color: var(--text-primary); }
  .controls { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; margin-bottom: 16px; font-size: 14px; color: var(--text-secondary); }
  select, button { font: inherit; padding: 5px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface); color: var(--text-primary); }
  button { cursor: pointer; }
  .muted { color: var(--text-muted); font-size: 12px; }
  .error { background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; border-radius: 6px; padding: 8px 12px; margin-bottom: 12px; }
  .banner { border-radius: 6px; padding: 10px 12px; margin-bottom: 12px; font-size: 14px; border: 1px solid; }
  .banner .icon { margin-right: 6px; }
  .banner.warning { background: var(--warning-bg); color: var(--warning-fg); border-color: var(--warning-border); }
  .banner.info { background: var(--info-bg); color: var(--info-fg); border-color: var(--info-border); }
  .empty { border: 1px dashed var(--border); border-radius: 8px; padding: 24px; text-align: center; color: var(--text-secondary); }
  .tiles { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 12px; margin-bottom: 16px; }
  .tile { border: 1px solid var(--border); border-radius: 8px; padding: 12px; background: var(--surface); }
  .tile-label { font-size: 12px; color: var(--text-secondary); }
  .tile-value { font-size: 22px; font-weight: 600; margin: 4px 0; font-variant-numeric: tabular-nums; }
  .tile-sub { font-size: 12px; color: var(--text-muted); }
  section { border: 1px solid var(--border); border-radius: 8px; padding: 12px 16px; margin-bottom: 16px; position: relative; background: var(--surface); }
  .legend { display: flex; flex-wrap: wrap; gap: 16px; font-size: 12px; color: var(--text-secondary); margin-bottom: 4px; }
  .key { display: inline-flex; align-items: center; gap: 6px; }
  .swatch { width: 12px; height: 12px; border-radius: 3px; display: inline-block; }
  .swatch.s1 { background: var(--series-1); }
  .swatch.s2 { background: var(--series-2); }
  .swatch.line { height: 2px; border-radius: 1px; }
  .swatch.marker { width: 0; height: 12px; border-left: 2px dashed var(--marker); border-radius: 0; }
  svg { width: 100%; height: auto; display: block; overflow: visible; }
  .axis { stroke: var(--text-muted); stroke-width: 1; }
  .grid { stroke: var(--grid); stroke-width: 1; }
  .tick { fill: var(--text-muted); font-size: 11px; }
  .bar.s1 { fill: var(--series-1); }
  .bar.s2 { fill: var(--series-2); }
  .line.s1 { fill: none; stroke: var(--series-1); stroke-width: 2; stroke-linejoin: round; }
  .dot.s1 { fill: var(--series-1); stroke: var(--surface); stroke-width: 2; }
  .hit { fill: transparent; }
  .hit:hover { fill: var(--grid); fill-opacity: 0.5; }
  .marker line { stroke: var(--marker); stroke-width: 2; stroke-dasharray: 4 3; }
  .tooltip { position: absolute; top: 40px; background: var(--surface); border: 1px solid var(--border); border-radius: 6px; padding: 6px 8px; font-size: 12px; box-shadow: 0 2px 8px rgba(0,0,0,0.12); pointer-events: none; z-index: 2; color: var(--text-primary); }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--grid); }
  th { color: var(--text-muted); font-weight: 500; }
  .num { text-align: right; font-variant-numeric: tabular-nums; }
  code { font-size: 12px; }
  tr.detail > td { padding-top: 0; }
  details summary { cursor: pointer; color: var(--text-secondary); font-size: 12px; }
  details table { margin-top: 6px; }
  .tag { margin-left: 8px; font-size: 11px; padding: 1px 6px; border-radius: 9px; border: 1px solid var(--warning-border); background: var(--warning-bg); color: var(--warning-fg); }
`;

if (!customElements.get('prompt-compressor-dashboard')) {
  customElements.define('prompt-compressor-dashboard', PromptCompressorDashboard);
}
