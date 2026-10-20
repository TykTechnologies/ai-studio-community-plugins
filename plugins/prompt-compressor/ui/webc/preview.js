// Prompt Compressor: preview tool.
//
// Paste a tool output or a whole request body (or pick a synthetic
// sample), choose a saved policy or edit one here, and see what an edge
// would forward and why. The compressor runs in Studio on the same inputs
// as the edge; nothing is stored. Every value is written with textContent,
// never innerHTML: the input is arbitrary text.
//
// Method names start with "_" so none shadows an HTMLElement member.

const MAX_BYTES = 2 * 1024 * 1024;
const MARKER = /\[tyk-compressed:[^\]\n]*\]/g;
const DIFF_LINE_LIMIT = 1500;

const SKIP_REASONS = {
  server_tool: 'a result the vendor produced; replayed as it was',
  undecodable: 'content that cannot be decoded',
  no_text: 'no text to compress',
  role: 'its role is not selected by the policy',
  excluded_tool: 'the tool is excluded by a per-tool override',
  level_0: 'level 0 for this unit',
  below_min_tokens: 'smaller than min_unit_tokens',
  cached: 'before the last cache breakpoint (cache_mode skip_cached)',
};
const CAP_REASONS = {
  native_context_management: 'the request asks the vendor to manage context (level 1 at most)',
  tool_override: 'a per-tool level override',
  protected_turn: 'one of the newest turns (protect_recent_turns)',
  request_filters: 'the LLM has request filters, so level 3 is held at 2 (lossy_with_request_filters is off)',
};
const SAMPLE_TOOL_OUTPUT = `============================= test session starts ==============================
platform linux -- Python 3.12.3, pytest-8.3.2, pluggy-1.5.0
rootdir: /srv/ledger
collected 214 items

tests/test_accounts.py ........................................          [ 18%]
tests/test_ledger.py ......................F.....................        [ 39%]
tests/test_payments.py ..............................................    [ 60%]
tests/test_reports.py ....................F..........................    [ 82%]
tests/test_users.py ......................................               [100%]

=================================== FAILURES ===================================
_____________________ test_ledger_balances_after_refund ______________________

    def test_ledger_balances_after_refund():
>       assert ledger.balance("ACC-1042") == Decimal("120.00")
E       AssertionError: assert Decimal('100.00') == Decimal('120.00')

tests/test_ledger.py:88: AssertionError
_________________________ test_monthly_report_totals __________________________

    def test_monthly_report_totals():
>       assert report.total("2026-09") == 4410
E       assert 4401 == 4410

tests/test_reports.py:57: AssertionError
=========================== short test summary info ============================
FAILED tests/test_ledger.py::test_ledger_balances_after_refund - AssertionError
FAILED tests/test_reports.py::test_monthly_report_totals - assert 4401 == 4410
======================== 2 failed, 212 passed in 9.81s =========================`;

function h(tag, props, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === undefined || value === null) continue;
    if (key.startsWith('on') && typeof value === 'function') {
      el.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (key === 'className') {
      el.className = value;
    } else if (key.startsWith('data-') || key.startsWith('aria-') || key === 'role' || key === 'for') {
      el.setAttribute(key, String(value));
    } else {
      el[key] = value;
    }
  }
  for (const child of children.flat()) {
    if (child === null || child === undefined || child === false) continue;
    el.appendChild(typeof child === 'string' || typeof child === 'number' ? document.createTextNode(String(child)) : child);
  }
  return el;
}

const fmtInt = (n) => Number(n || 0).toLocaleString();
const utf8Length = (s) => new TextEncoder().encode(s).length;

// withMarkers renders text with every compression marker highlighted.
function withMarkers(text) {
  const out = [];
  let last = 0;
  for (const m of text.matchAll(MARKER)) {
    if (m.index > last) out.push(document.createTextNode(text.slice(last, m.index)));
    out.push(h('mark', {}, m[0]));
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(document.createTextNode(text.slice(last)));
  return out;
}

// lineDiff is an LCS line diff: [{op: ' ' | '-' | '+', text}], or null
// when the texts are too long to diff here.
function lineDiff(before, after) {
  const a = before.split('\n');
  const b = after.split('\n');
  if (a.length > DIFF_LINE_LIMIT || b.length > DIFF_LINE_LIMIT) return null;
  const n = a.length, m = b.length;
  const dp = new Uint16Array((n + 1) * (m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i * (m + 1) + j] = a[i] === b[j] ? dp[(i + 1) * (m + 1) + j + 1] + 1 : Math.max(dp[(i + 1) * (m + 1) + j], dp[i * (m + 1) + j + 1]);
    }
  }
  const out = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) { out.push({ op: ' ', text: a[i] }); i++; j++; }
    else if (dp[(i + 1) * (m + 1) + j] >= dp[i * (m + 1) + j + 1]) { out.push({ op: '-', text: a[i] }); i++; }
    else { out.push({ op: '+', text: b[j] }); j++; }
  }
  while (i < n) out.push({ op: '-', text: a[i++] });
  while (j < m) out.push({ op: '+', text: b[j++] });
  return out;
}

class PromptCompressorPreview extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this._options = null;
    this._mode = 'tool_output';
    this._text = SAMPLE_TOOL_OUTPUT;
    this._toolName = 'run_tests';
    this._format = 'openai';
    this._body = '';
    this._vendor = 'openai';
    this._path = '/v1/chat/completions';
    this._sample = '';
    this._policyName = '';
    this._editInline = false;
    this._requestFilters = false;
    this._policyJSON = '';
    this._result = null;
    this._errors = [];
    this._busy = false;
  }

  connectedCallback() {
    this._paint();
    this._waitForPluginAPI();
  }

  async _waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) {
        await this._loadOptions();
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    this._errors = ['Plugin API initialization timeout - please refresh the page'];
    this._paint();
  }

  async _loadOptions() {
    try {
      this._options = await this.pluginAPI.call('getPreviewOptions', {});
      this._policyName = this._options.default_policy || 'default';
      this._sample = (this._options.samples[0] || {}).name || '';
      this._resetInlinePolicy();
    } catch (error) {
      this._errors = ['Failed to load the preview: ' + error.message];
    }
    this._paint();
  }

  _resetInlinePolicy() {
    const p = ((this._options && this._options.policies) || []).find((x) => x.name === this._policyName);
    this._policyJSON = JSON.stringify(p ? p.policy : { level: 1 }, null, 2);
  }

  _request() {
    const req = { mode: this._mode };
    if (this._mode === 'tool_output') Object.assign(req, { text: this._text, tool_name: this._toolName, format: this._format });
    if (this._mode === 'body') Object.assign(req, { vendor: this._vendor, path: this._path });
    if (this._mode === 'sample') req.sample = this._sample;
    req.request_filters = this._requestFilters;
    if (this._editInline) {
      let policy;
      try {
        policy = JSON.parse(this._policyJSON);
      } catch (e) {
        throw new Error('The policy is not valid JSON.');
      }
      req.policy = policy;
    } else {
      req.policy_name = this._policyName;
    }
    if (this._mode === 'body') {
      let body;
      try {
        body = JSON.parse(this._body);
      } catch (e) {
        throw new Error('The request body is not valid JSON.');
      }
      req.body = body;
    }
    return req;
  }

  async _run() {
    this._errors = [];
    const size = this._mode === 'tool_output' ? utf8Length(this._text) : this._mode === 'body' ? utf8Length(this._body) : 0;
    if (size > MAX_BYTES) {
      this._errors = ['The input is larger than 2 MiB; preview a smaller body.'];
      this._paint();
      return;
    }
    let req;
    try {
      req = this._request();
    } catch (error) {
      this._errors = [error.message];
      this._paint();
      return;
    }
    this._busy = true;
    this._paint();
    try {
      this._result = await this.pluginAPI.call('preview', req);
    } catch (error) {
      this._result = null;
      this._errors = [error && error.message ? error.message : String(error)];
    } finally {
      this._busy = false;
      this._paint();
    }
  }

  async _upload(file) {
    if (!file) return;
    if (file.size > MAX_BYTES) {
      this._errors = ['The file is larger than 2 MiB; preview a smaller body.'];
      this._paint();
      return;
    }
    this._body = await file.text();
    this._errors = [];
    this._paint();
  }

  // ---- painting ----

  _paint() {
    const root = this.shadowRoot;
    root.textContent = '';
    root.appendChild(h('style', {}, STYLES));
    const page = h('div', { className: 'page' });
    root.appendChild(page);
    page.appendChild(h('h1', {}, 'Compression preview'));
    page.appendChild(h('p', { className: 'lead' },
      'See what an edge would forward for a body and policy, and why. The compressor runs here on the same inputs as the edge. Nothing you paste is stored or logged.'));
    if (this._errors.length) {
      page.appendChild(h('div', { className: 'error', role: 'alert', 'data-testid': 'pc-preview-errors' }, this._errors.map((e) => h('div', {}, e))));
    }
    if (!this._options) {
      page.appendChild(h('p', { className: 'muted' }, 'Loading…'));
      return;
    }
    page.appendChild(this._paintInput());
    page.appendChild(this._paintPolicy());
    page.appendChild(h('div', { className: 'actions' },
      h('button', { className: 'primary', disabled: this._busy, onClick: () => this._run(), 'data-testid': 'pc-preview-run' }, this._busy ? 'Running…' : 'Preview')));
    if (this._result) page.appendChild(this._paintResult(this._result));
  }

  _paintInput() {
    const tabs = [
      ['tool_output', 'Tool output'],
      ['body', 'Request body'],
      ['sample', 'Sample'],
    ];
    const section = h('section', {}, h('h2', {}, 'Input'),
      h('div', { className: 'tabs', role: 'tablist' }, tabs.map(([mode, label]) =>
        h('button', {
          role: 'tab', 'aria-selected': String(this._mode === mode), className: this._mode === mode ? 'tab active' : 'tab',
          'data-testid': `pc-preview-mode-${mode}`, onClick: () => { this._mode = mode; this._paint(); },
        }, label))));

    if (this._mode === 'tool_output') {
      section.appendChild(h('div', { className: 'row' },
        h('label', { className: 'field' }, 'Tool name',
          h('input', { type: 'text', value: this._toolName, maxLength: 128, onInput: (e) => { this._toolName = e.target.value; } })),
        h('label', { className: 'field' }, 'Request format',
          h('select', { onChange: (e) => { this._format = e.target.value; } },
            this._options.formats.map((f) => h('option', { value: f, selected: f === this._format }, f))))));
      section.appendChild(h('label', { className: 'field' }, 'Tool output',
        h('textarea', { rows: 14, value: this._text, spellcheck: false, 'data-testid': 'pc-preview-text', onInput: (e) => { this._text = e.target.value; } })));
      section.appendChild(h('p', { className: 'muted' }, 'The output is wrapped as the result of one tool call, with a later turn after it so the newest-turn protection does not apply.'));
    } else if (this._mode === 'body') {
      section.appendChild(h('div', { className: 'row' },
        h('label', { className: 'field' }, 'Vendor',
          h('select', { onChange: (e) => { this._vendor = e.target.value; } },
            ['openai', 'anthropic', 'bedrock', 'google_ai', 'vertex', 'ollama'].map((v) => h('option', { value: v, selected: v === this._vendor }, v)))),
        h('label', { className: 'field' }, 'Vendor path',
          h('input', { type: 'text', value: this._path, placeholder: '/v1/messages', onInput: (e) => { this._path = e.target.value; } })),
        h('label', { className: 'field' }, 'Upload a JSON file',
          h('input', { type: 'file', accept: '.json,application/json', onChange: (e) => this._upload(e.target.files[0]) }))));
      section.appendChild(h('label', { className: 'field' }, 'Request body (JSON)',
        h('textarea', { rows: 14, value: this._body, spellcheck: false, 'data-testid': 'pc-preview-body', onInput: (e) => { this._body = e.target.value; } })));
    } else {
      section.appendChild(h('label', { className: 'field' }, 'Synthetic sample',
        h('select', { 'data-testid': 'pc-preview-sample', onChange: (e) => { this._sample = e.target.value; } },
          this._options.samples.map((s) => h('option', { value: s.name, selected: s.name === this._sample }, s.about)))));
    }
    return section;
  }

  _paintPolicy() {
    const policies = this._options.policies || [];
    const section = h('section', {}, h('h2', {}, 'Policy'));
    section.appendChild(h('div', { className: 'row' },
      h('label', { className: 'field' }, 'Saved policy',
        h('select', {
          disabled: this._editInline, 'data-testid': 'pc-preview-policy',
          onChange: (e) => { this._policyName = e.target.value; this._resetInlinePolicy(); this._paint(); },
        }, policies.map((p) => h('option', { value: p.name, selected: p.name === this._policyName }, `${p.name} (level ${p.level}, ${p.short_hash})`)))),
      h('label', { className: 'inline' },
        h('input', { type: 'checkbox', checked: this._editInline, 'data-testid': 'pc-preview-edit-inline', onChange: (e) => { this._editInline = e.target.checked; this._paint(); } }),
        'Edit a copy here (not saved)'),
      h('label', { className: 'inline' },
        h('input', { type: 'checkbox', checked: this._requestFilters, 'data-testid': 'pc-preview-request-filters', onChange: (e) => { this._requestFilters = e.target.checked; } }),
        'The LLM has request filters')));
    if (this._editInline) {
      section.appendChild(h('label', { className: 'field' }, 'Policy (JSON, checked like a saved policy)',
        h('textarea', { rows: 8, value: this._policyJSON, spellcheck: false, 'data-testid': 'pc-preview-policy-json', onInput: (e) => { this._policyJSON = e.target.value; } })));
    }
    return section;
  }

  _paintResult(r) {
    const rep = r.report || {};
    const saved = (rep.tokens_before || 0) - (rep.tokens_after || 0);
    const box = h('div', { 'data-testid': 'pc-preview-result' });
    const facts = [
      ['Outcome', rep.outcome + (rep.reason ? ` (${rep.reason})` : '')],
      ['Format', r.format],
      ['Policy', `${r.policy_name}, level ${r.level}, ${String(r.policy_hash || '').slice(0, 12)}`],
      ['Tokens (estimate)', `${fmtInt(rep.tokens_before)} → ${fmtInt(rep.tokens_after)} in changed text, ${fmtInt(saved)} saved (${rep.estimator || ''})`],
      ['Body bytes', `${fmtInt(rep.bytes_before)} → ${fmtInt(rep.bytes_after)}`],
      ['Units', `${fmtInt(rep.units_seen)} seen, ${fmtInt(rep.units_selected)} selected, ${fmtInt(rep.units_compressed)} changed`],
      ['Post-check', r.post_check],
    ];
    if (rep.native_context_management) facts.push(['Note', 'The request asks the vendor to manage context, so level 1 at most.']);
    if (rep.request_filters_capped) facts.push(['Note', 'Level 3 was held at 2: the LLM has request filters.']);
    if (r.engine_not_run) facts.push(['Note', 'The policy names an external engine. The preview never calls it; an edge runs it after these levels, so its output can differ.']);
    box.appendChild(h('section', {}, h('h2', {}, 'Result'),
      h('dl', { className: 'facts' }, facts.map(([k, v]) => [h('dt', {}, k), h('dd', {}, v)]))));
    if (Object.keys(rep.strategies || {}).length) {
      box.appendChild(h('section', {}, h('h2', {}, 'By strategy'),
        h('table', {}, h('thead', {}, h('tr', {}, ['Strategy', 'Texts changed', 'Bytes saved'].map((c, i) => h('th', { className: i ? 'num' : '' }, c)))),
          h('tbody', {}, Object.entries(rep.strategies).map(([name, s]) =>
            h('tr', {}, h('td', {}, h('code', {}, name)), h('td', { className: 'num' }, fmtInt(s.leaves)), h('td', { className: 'num' }, fmtInt(s.bytes_saved))))))));
    }
    box.appendChild(this._paintUnits(r.units || []));
    box.appendChild(h('section', {}, h('h2', {}, 'Output body'),
      h('p', { className: 'muted' }, r.changed ? 'What an edge in enforce mode would forward. Markers are highlighted.' : 'Unchanged: an edge would forward the body as it came.'),
      h('details', { open: false }, h('summary', {}, 'Show the output body'), h('pre', { className: 'body', 'data-testid': 'pc-preview-output' }, withMarkers(r.output || '')))));
    return box;
  }

  _paintUnits(units) {
    const section = h('section', {}, h('h2', {}, 'Decisions by unit'));
    if (units.length === 0) {
      section.appendChild(h('p', { className: 'muted' }, 'No units: the body was skipped before it was read (see the outcome).'));
      return section;
    }
    const table = h('table', { 'data-testid': 'pc-preview-units' },
      h('thead', {}, h('tr', {}, ['#', 'Role / kind', 'Tool', 'Turn', 'Tokens', 'Decision', 'Strategies', 'Saved'].map((c, i) => h('th', { className: [4, 7].includes(i) ? 'num' : '' }, c)))));
    const body = h('tbody');
    for (const u of units) {
      const decision = [];
      if (!u.selected) {
        decision.push(h('div', {}, h('strong', {}, 'Skipped: '), SKIP_REASONS[u.reason] || u.reason || ''));
      } else {
        decision.push(h('div', {}, h('strong', {}, `Level ${u.level}`), u.changed ? ', changed' : ', nothing to remove'));
        for (const c of u.capped || []) decision.push(h('div', { className: 'muted' }, 'Capped: ', CAP_REASONS[c] || c));
        if (u.budget) decision.push(h('div', { className: 'muted' }, `Tool budget ${fmtInt(u.budget)} tokens`));
        if (u.dedupe) decision.push(h('div', { className: 'muted' }, 'Replaced by a reference to an identical earlier output'));
        if (u.masked) decision.push(h('div', { className: 'muted' }, 'An old tool result, replaced by a placeholder (mask_after_turns)'));
        if ((u.detected || []).length) decision.push(h('div', { className: 'muted' }, `Detected: ${u.detected.join(', ')} (the first one runs)`));
        for (const p of u.protected || []) decision.push(h('div', { className: 'muted' }, `Protect pattern ${p.pattern}: ${p.before} lines matched, ${p.after} kept`));
      }
      body.appendChild(h('tr', { className: u.selected ? '' : 'skipped' },
        h('td', {}, String(u.index)),
        h('td', {}, `${u.role} / ${u.kind}`),
        h('td', {}, u.tool_name || ''),
        h('td', {}, String(u.turn)),
        h('td', { className: 'num' }, fmtInt(u.tokens)),
        h('td', {}, decision),
        h('td', {}, (u.strategies || []).map((s) => h('code', { className: 'chip' }, s))),
        h('td', { className: 'num' }, u.changed ? fmtInt((u.tokens_before || 0) - (u.tokens_after || 0)) : '')));
      if (u.changed && (u.leaves || []).length) {
        body.appendChild(h('tr', {}, h('td', { colSpan: 8 }, u.leaves.map((leaf) => this._paintLeaf(u, leaf)))));
      }
    }
    table.appendChild(body);
    section.appendChild(table);
    return section;
  }

  _paintLeaf(u, leaf) {
    const details = h('details', { className: 'leaf' },
      h('summary', {}, `Diff of unit ${u.index}${leaf.path ? ` (${leaf.path})` : ''}: ${fmtInt(utf8Length(leaf.before))} → ${fmtInt(utf8Length(leaf.after))} bytes`));
    const diff = lineDiff(leaf.before, leaf.after);
    if (!diff) {
      details.appendChild(h('p', { className: 'muted' }, 'Too long to diff here; the output is shown.'));
      details.appendChild(h('pre', { className: 'body' }, withMarkers(leaf.after)));
      return details;
    }
    const pre = h('pre', { className: 'diff', 'data-testid': 'pc-preview-diff' });
    for (const d of diff) {
      const cls = d.op === '-' ? 'del' : d.op === '+' ? 'add' : 'same';
      pre.appendChild(h('div', { className: cls }, h('span', { className: 'op', 'aria-hidden': 'true' }, d.op), withMarkers(d.text)));
    }
    details.appendChild(pre);
    return details;
  }
}

const STYLES = `
  :host { display: block; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; color: #1f2937; }
  .page { padding: 24px; max-width: 1200px; }
  h1 { font-size: 22px; font-weight: 600; margin: 0 0 8px; }
  h2 { font-size: 15px; font-weight: 600; margin: 0 0 10px; }
  .lead { color: #4b5563; margin: 0 0 16px; }
  section { background: #fff; border: 1px solid #e5e7eb; border-radius: 8px; padding: 14px 16px; margin-bottom: 14px; }
  .row { display: flex; gap: 16px; flex-wrap: wrap; }
  .field { display: flex; flex-direction: column; gap: 4px; margin: 0 0 10px; font-size: 14px; flex: 1 1 220px; }
  .inline { display: inline-flex; gap: 6px; align-items: center; font-size: 14px; align-self: center; }
  input[type=text], select, textarea { font: inherit; padding: 6px 8px; border: 1px solid #d1d5db; border-radius: 6px; }
  textarea { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; width: 100%; box-sizing: border-box; }
  button { font: inherit; padding: 6px 14px; border-radius: 6px; border: 1px solid #d1d5db; background: #fff; cursor: pointer; }
  button.primary { background: #4f46e5; border-color: #4f46e5; color: #fff; }
  button:disabled { opacity: 0.5; cursor: default; }
  .tabs { display: flex; gap: 4px; margin-bottom: 12px; }
  .tab.active { background: #eef2ff; border-color: #4f46e5; color: #3730a3; }
  .actions { margin: 4px 0 16px; }
  .muted { color: #6b7280; font-size: 12px; }
  .error { background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; border-radius: 6px; padding: 8px 12px; margin-bottom: 12px; font-size: 14px; }
  .facts { display: grid; grid-template-columns: max-content 1fr; gap: 4px 16px; margin: 0; font-size: 14px; }
  .facts dt { color: #6b7280; }
  .facts dd { margin: 0; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid #f3f4f6; vertical-align: top; }
  th { color: #6b7280; font-weight: 500; }
  .num { text-align: right; font-variant-numeric: tabular-nums; }
  tr.skipped td { color: #6b7280; }
  .chip { display: inline-block; background: #f3f4f6; border-radius: 4px; padding: 1px 6px; margin: 0 4px 4px 0; font-size: 12px; }
  pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; background: #f9fafb; border: 1px solid #e5e7eb; border-radius: 6px; padding: 8px; overflow: auto; max-height: 480px; white-space: pre-wrap; word-break: break-word; }
  .diff div { white-space: pre-wrap; }
  .diff .del { background: #fef2f2; color: #991b1b; }
  .diff .add { background: #ecfdf5; color: #065f46; }
  .diff .op { display: inline-block; width: 1.2em; color: #9ca3af; user-select: none; }
  mark { background: #fde68a; color: #78350f; border-radius: 3px; padding: 0 2px; }
  details.leaf { margin: 4px 0 8px; }
  summary { cursor: pointer; font-size: 13px; }
  code { font-size: 12px; }
`;

if (!customElements.get('prompt-compressor-preview')) {
  customElements.define('prompt-compressor-preview', PromptCompressorPreview);
}
