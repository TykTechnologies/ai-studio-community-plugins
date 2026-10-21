// Prompt Compressor: policies and bindings editor.
//
// Reads the stored config with getEditor and saves it with saveConfig; the
// plugin validates before it stores, and edges apply the change after the
// next configuration push to them. The DOM is built with createElement and
// textContent only: names of LLMs, Apps, policies and tools never reach
// innerHTML.
//
// Method names start with "_" so none shadows an HTMLElement member.

const LEVELS = [
  { value: 0, label: '0 - Off' },
  { value: 1, label: '1 - Lossless' },
  { value: 2, label: '2 - Noise reduction' },
  { value: 3, label: '3 - Aggressive' },
];
const ROLES = ['tool', 'user', 'assistant', 'system'];
const MODES = [
  { value: 'off', label: 'Off', help: 'Nothing is compressed.' },
  { value: 'audit', label: 'Audit', help: 'Compress and record the savings, but forward the original body.' },
  { value: 'enforce', label: 'Enforce', help: 'Forward the compressed body.' },
];
const POLICY_DEFAULTS = {
  level: 1,
  roles: ['tool'],
  min_unit_tokens: 300,
  protect_recent_turns: 1,
  cache_mode: 'deterministic',
  dedupe_references: false,
  max_unit_tokens: 6000,
  mask_after_turns: 0,
  mask_step_tokens: 20000,
};
const RESERVED = ['off'];
const NAME_PATTERN = /^[a-z0-9]+([-_][a-z0-9]+)*$/;
// A drop_tools entry: a tool name, or a prefix ending in '*' (as the
// plugin's config check reads it).
const DROP_TOOL_PATTERN = /^[A-Za-z0-9_.:-]{1,64}\*?$|^[A-Za-z0-9_.:-]{0,63}\*$/;
const MAX_DROP_TOOLS = 256;
const SUGGEST_RANGE_HOURS = 168;
const CACHE_WARNING =
  'Changing the mode, a policy or a binding changes the compressed prompt for every session it affects. ' +
  'Each of those sessions misses the provider\'s prompt cache once on its next request. Save now?';

// h builds an element. Strings and numbers in children become text nodes;
// "on<event>" props become listeners; everything else is set as a property.
function h(tag, props, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value === undefined || value === null) continue;
    if (key.startsWith('on') && typeof value === 'function') {
      el.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (key === 'className') {
      el.className = value;
    } else if (key.startsWith('data-') || key.startsWith('aria-') || key === 'for' || key === 'role') {
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

function paintWarnings(warnings) {
  if (!warnings || warnings.length === 0) return null;
  return h('div', { className: 'note', role: 'status', 'data-testid': 'pc-status-warnings' },
    warnings.map((w) => h('div', {}, '⚠ ', w)));
}

function clone(v) {
  return JSON.parse(JSON.stringify(v));
}

function intOr(value, fallback) {
  const n = Number.parseInt(value, 10);
  return Number.isFinite(n) ? n : fallback;
}

class PromptCompressorPolicies extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this._view = null; // getEditor response
    this._config = null; // the config being edited
    this._savedJSON = ''; // the config as loaded, to detect changes
    this._editing = null; // { originalName, name, policy } while the policy form is open
    this._messages = { errors: [], info: '' };
    this._busy = false;
  }

  connectedCallback() {
    this._paint();
    this._waitForPluginAPI();
  }

  async _waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) {
        this._load();
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    this._setErrors(['Plugin API initialization timeout - please refresh the page']);
  }

  _canWrite() {
    return !!(this.pluginAPI && (!this.pluginAPI.can || this.pluginAPI.can('policies:write')));
  }

  async _load() {
    this._busy = true;
    this._paint();
    this._warnings = await statusWarnings(this.pluginAPI);
    try {
      const view = await this.pluginAPI.call('getEditor', {});
      this._view = view;
      const cfg = clone(view.config || {});
      cfg.policies = cfg.policies || {};
      cfg.bindings = cfg.bindings || {};
      this._config = cfg;
      this._savedJSON = JSON.stringify(cfg);
      this._editing = null;
      this._messages = { errors: [], info: '' };
    } catch (error) {
      this._messages = { errors: ['Failed to load the policies: ' + (error && error.message ? error.message : error)], info: '' };
    } finally {
      this._busy = false;
      this._paint();
    }
  }

  _dirty() {
    return this._config && JSON.stringify(this._config) !== this._savedJSON;
  }

  // info may be a string or an array of strings and Nodes (a link).
  _setErrors(errors, info) {
    this._messages = { errors: errors || [], info: info || '' };
    this._paint();
  }

  async _save() {
    if (!this._config || !this._canWrite()) return;
    if (this._editing) {
      this._setErrors(['Apply or cancel the policy you are editing first.']);
      return;
    }
    // Check first, so the cache warning is only shown for a config that saves
    try {
      const check = await this.pluginAPI.call('validateConfig', { config: this._config });
      if (!check.valid) {
        this._setErrors(check.errors);
        return;
      }
    } catch (error) {
      this._setErrors(['Validation failed: ' + error.message]);
      return;
    }
    if (!window.confirm(CACHE_WARNING)) return;

    this._busy = true;
    this._paint();
    try {
      const res = await this.pluginAPI.call('saveConfig', { config: this._config });
      if (!res.saved) {
        this._busy = false;
        this._setErrors(res.errors && res.errors.length ? res.errors : ['The configuration was not saved.']);
        return;
      }
      await this._load();
      this._setErrors([], [
        'Saved. Compression on edge gateways does not change yet: they show as pending and apply it after the next configuration push from ',
        this._appLink('/admin/edge-gateways', 'Edge Gateways'),
        '.',
      ]);
    } catch (error) {
      this._busy = false;
      this._setErrors(['Save failed: ' + error.message]);
    }
  }

  // ---- policy form ----

  _startEdit(name) {
    const existing = name ? this._config.policies[name] : null;
    this._editing = {
      originalName: name || null,
      name: name || '',
      policy: Object.assign(clone(POLICY_DEFAULTS), clone(existing || {})),
      originalDrop: JSON.stringify((existing && existing.drop_tools) || []),
    };
    this._messages = { errors: [], info: '' };
    this._paint();
    this._loadDropSuggestions();
  }

  // _loadDropSuggestions reads the tools each App offered but never called
  // in the last week (getSavings), once per page, for users who may see the
  // savings. They are only suggestions: nothing is added unless picked.
  async _loadDropSuggestions() {
    if (this._dropSuggestions !== undefined) return;
    if (!this.pluginAPI || (this.pluginAPI.can && !this.pluginAPI.can('savings:read'))) {
      this._dropSuggestions = null;
      return;
    }
    this._dropSuggestions = 'loading';
    try {
      const view = await this.pluginAPI.call('getSavings', { range_hours: SUGGEST_RANGE_HOURS });
      this._dropSuggestions = (view.tools || [])
        .filter((a) => (a.never_called || []).length)
        .map((a) => ({ app: a.name || `App #${a.id}`, names: a.never_called }));
    } catch (error) {
      this._dropSuggestions = null;
    }
    if (this._editing) this._paint();
  }

  _applyEdit() {
    const ed = this._editing;
    const name = ed.name.trim();
    const errors = [];
    if (!NAME_PATTERN.test(name) || name.length > 64) {
      errors.push('Policy names are lower-case letters, digits, "-" or "_", at most 64 characters.');
    }
    if (RESERVED.includes(name)) errors.push('"off" is reserved: bind "off" to turn compression off.');
    if (name !== ed.originalName && this._config.policies[name]) errors.push(`A policy named "${name}" already exists.`);
    if (!ed.policy.roles || ed.policy.roles.length === 0) errors.push('Select at least one role.');
    const drop = ed.policy.drop_tools || [];
    if (drop.length > MAX_DROP_TOOLS) errors.push(`The drop list has ${drop.length} entries; at most ${MAX_DROP_TOOLS} are allowed.`);
    for (const entry of drop) {
      if (!DROP_TOOL_PATTERN.test(entry)) errors.push(`"${entry}" is not a tool name: letters, digits, "_", ".", ":" or "-", at most 64, optionally ending in "*".`);
    }
    if (errors.length) {
      this._setErrors(errors);
      return;
    }
    const policy = this._compactPolicy(ed.policy);
    if (ed.originalName && ed.originalName !== name) {
      delete this._config.policies[ed.originalName];
      this._renamePolicyReferences(ed.originalName, name);
    }
    this._config.policies[name] = policy;
    this._editing = null;
    this._setErrors([], 'Policy updated. Save to apply it.');
  }

  // _compactPolicy drops settings left at their defaults, so the stored
  // config lists only what a policy changes. The policy hash is the same
  // either way.
  _compactPolicy(policy) {
    const out = {};
    for (const [key, value] of Object.entries(policy)) {
      if (key in POLICY_DEFAULTS && JSON.stringify(POLICY_DEFAULTS[key]) === JSON.stringify(value)) continue;
      if (key === 'protect_patterns' && (!value || value.length === 0)) continue;
      if (key === 'drop_tools' && (!value || value.length === 0)) continue;
      if (key === 'tool_overrides' && (!value || Object.keys(value).length === 0)) continue;
      out[key] = value;
    }
    return out;
  }

  _renamePolicyReferences(from, to) {
    if (this._config.default_policy === from) this._config.default_policy = to;
    for (const [key, value] of Object.entries(this._config.bindings)) {
      if (value === from) this._config.bindings[key] = to;
    }
  }

  _policyReferences(name) {
    const refs = [];
    if ((this._config.default_policy || 'default') === name) refs.push('the default policy');
    for (const [key, value] of Object.entries(this._config.bindings)) {
      if (value === name) refs.push(this._bindingLabel(key));
    }
    return refs;
  }

  _deletePolicy(name) {
    const refs = this._policyReferences(name);
    if (refs.length) {
      this._setErrors([`"${name}" is still used by ${refs.join(', ')}. Change those first.`]);
      return;
    }
    delete this._config.policies[name];
    this._setErrors([], `Policy "${name}" removed. Save to apply it.`);
  }

  // ---- bindings ----

  _bindingLabel(key) {
    const llms = (this._view && this._view.llms) || [];
    const apps = (this._view && this._view.apps) || [];
    if (key.startsWith('llm_id:')) {
      const id = Number(key.slice(7));
      const llm = llms.find((l) => l.id === id);
      return llm ? `LLM ${llm.name}` : `LLM #${id} (not found)`;
    }
    if (key.startsWith('llm:')) {
      const s = key.slice(4);
      const llm = llms.find((l) => l.slug === s);
      return llm ? `LLM ${llm.name} (slug ${s})` : `LLM slug ${s} (not found)`;
    }
    if (key.startsWith('app:')) {
      const id = Number(key.slice(4));
      const app = apps.find((a) => a.id === id);
      return app ? `App ${app.name}` : `App #${id}`;
    }
    return key;
  }

  _llmForKey(key) {
    const llms = (this._view && this._view.llms) || [];
    if (key.startsWith('llm_id:')) return llms.find((l) => l.id === Number(key.slice(7))) || null;
    if (key.startsWith('llm:')) return llms.find((l) => l.slug === key.slice(4)) || null;
    return null;
  }

  // _isLossy reports whether a policy runs level 3 or an engine, which the
  // plugin skips on LLMs with request filters (unless the override is on).
  _isLossy(name) {
    const p = this._config.policies[name];
    return !!p && ((p.level !== undefined && p.level >= 3) || !!p.engine);
  }

  // _llmPolicy is the policy an LLM gets from the LLM bindings and the
  // default (App metadata and App bindings can still override it per App).
  _llmPolicy(llm) {
    const b = this._config.bindings;
    return b[`llm_id:${llm.id}`] || b[`llm:${llm.slug}`] || this._config.default_policy || 'default';
  }

  // _bindingSkipsLossy reports whether an LLM binding names a lossy policy
  // the plugin will skip there because the LLM has request filters.
  _bindingSkipsLossy(key) {
    if (this._config.lossy_with_request_filters) return false;
    const llm = this._llmForKey(key);
    return !!llm && llm.filters_known && llm.has_request_filters && this._isLossy(this._config.bindings[key]);
  }

  // _filterSkipNote explains where a lossy policy is skipped, or null.
  _filterSkipNote(name) {
    if (!this._isLossy(name) || this._config.lossy_with_request_filters) return null;
    const llms = (this._view && this._view.llms) || [];
    if (llms.some((l) => !l.filters_known)) {
      return 'Level 3 and engines are skipped on LLMs with request filters, so the filters still see the original text.';
    }
    const skipped = llms.filter((l) => l.has_request_filters && this._llmPolicy(l) === name).map((l) => l.name);
    if (skipped.length === 0) return null;
    return `Level 3 and engines are skipped on ${skipped.join(', ')}: request filters screen the original text there.`;
  }

  _addBinding(kind, id, policy) {
    if (!id) {
      this._setErrors([kind === 'llm' ? 'Pick an LLM.' : 'Pick or enter an App ID.']);
      return;
    }
    const key = (kind === 'llm' ? 'llm_id:' : 'app:') + id;
    this._config.bindings[key] = policy;
    this._setErrors([], 'Binding added. Save to apply it.');
  }

  // ---- painting ----

  _paint() {
    const root = this.shadowRoot;
    root.textContent = '';
    root.appendChild(h('style', {}, STYLES));
    const page = h('div', { className: 'page' });
    root.appendChild(page);

    page.appendChild(h('h1', {}, 'Compression policies'));
    page.appendChild(
      h('p', { className: 'lead' },
        'Policies set how request bodies are compressed. Bindings choose a policy per LLM or App; ',
        'an App can also choose one with the metadata key ',
        h('code', {}, 'prompt_compressor.policy'),
        '. Precedence: App metadata, App binding, LLM binding, then the default policy.'),
    );

    if (this._busy && !this._config) {
      page.appendChild(h('p', {}, 'Loading…'));
      return;
    }
    const warnings = paintWarnings(this._warnings);
    if (warnings) page.appendChild(warnings);
    page.appendChild(this._paintMessages());
    if (!this._config) return;

    const readOnly = !this._canWrite();
    if (readOnly) {
      page.appendChild(h('div', { className: 'note' }, 'You can view these settings. Changing them needs the Compression policies write permission.'));
    }
    if (this._view && this._view.problems && this._view.problems.length) {
      page.appendChild(
        h('div', { className: 'error' },
          h('strong', {}, 'The stored configuration is invalid, so compression is off: '),
          h('ul', {}, this._view.problems.map((p) => h('li', {}, p)))),
      );
    }

    page.appendChild(this._paintMode(readOnly));
    if (this._editing) {
      page.appendChild(this._paintPolicyForm());
    } else {
      page.appendChild(this._paintPolicies(readOnly));
    }
    page.appendChild(this._paintBindings(readOnly));
    page.appendChild(this._paintAdvanced(readOnly));

    if (!readOnly) {
      page.appendChild(
        h('div', { className: 'actions' },
          h('button', { className: 'primary', disabled: this._busy || !this._dirty(), onClick: () => this._save(), 'data-testid': 'pc-save' }, 'Save'),
          h('button', { disabled: this._busy || !this._dirty(), onClick: () => this._load() }, 'Discard changes'),
          this._dirty() ? h('span', { className: 'muted' }, 'Unsaved changes') : null),
      );
    }
  }

  // _appLink is an in-app link. The host routes on pushState + popstate; a
  // plain navigation would reload the whole admin app.
  _appLink(path, text) {
    return h('a', {
      href: path,
      onClick: (e) => {
        e.preventDefault();
        window.history.pushState({}, '', path);
        window.dispatchEvent(new PopStateEvent('popstate'));
      },
    }, text);
  }

  _paintMessages() {
    const box = h('div', { 'aria-live': 'polite' });
    const { errors, info } = this._messages;
    if (errors && errors.length) {
      box.appendChild(h('div', { className: 'error', 'data-testid': 'pc-errors' }, h('ul', {}, errors.map((e) => h('li', {}, e)))));
    }
    if (info && info.length) box.appendChild(h('div', { className: 'info', 'data-testid': 'pc-info' }, info));
    return box;
  }

  _paintMode(readOnly) {
    const cfg = this._config;
    const mode = cfg.mode || 'audit';
    const section = h('section', {}, h('h2', {}, 'Mode'));
    const group = h('div', { className: 'modes', role: 'radiogroup' });
    for (const m of MODES) {
      group.appendChild(
        h('label', { className: 'mode' + (m.value === mode ? ' selected' : '') },
          h('input', {
            type: 'radio', name: 'mode', value: m.value, checked: m.value === mode, disabled: readOnly,
            onChange: () => { cfg.mode = m.value; this._paint(); },
          }),
          h('span', { className: 'mode-label' }, m.label),
          h('span', { className: 'muted' }, m.help)),
      );
    }
    section.appendChild(group);

    const names = ['default', ...Object.keys(cfg.policies).filter((n) => n !== 'default').sort(), 'off'];
    section.appendChild(
      h('label', { className: 'field' }, 'Default policy',
        h('select', {
          disabled: readOnly,
          onChange: (e) => { cfg.default_policy = e.target.value; this._paint(); },
        }, names.map((n) => h('option', { value: n, selected: (cfg.default_policy || 'default') === n }, n === 'default' && !cfg.policies.default ? 'default (built-in: level 1, tool outputs)' : n)))),
    );
    return section;
  }

  _paintPolicies(readOnly) {
    const cfg = this._config;
    const section = h('section', {}, h('h2', {}, 'Policies'));
    const names = Object.keys(cfg.policies).sort();
    if (names.length === 0) {
      section.appendChild(h('p', { className: 'muted' }, 'No policies yet. The built-in "default" policy applies: level 1 (lossless) on tool outputs.'));
    } else {
      const table = h('table', {},
        h('thead', {}, h('tr', {}, ['Name', 'Level', 'Roles', 'Min unit tokens', 'Notes', ''].map((c) => h('th', {}, c)))));
      const body = h('tbody');
      for (const name of names) {
        const p = Object.assign(clone(POLICY_DEFAULTS), cfg.policies[name]);
        const notes = [];
        const skip = this._filterSkipNote(name);
        if (skip) notes.push(skip);
        if (p.engine) notes.push(`External engine ${p.engine.name} (${p.engine.kind}) runs after levels 1–3; an engine failure follows the failure mode (${cfg.failure_mode || 'open'}).`);
        if (p.tool_overrides && Object.keys(p.tool_overrides).length) notes.push(`${Object.keys(p.tool_overrides).length} tool override(s)`);
        body.appendChild(
          h('tr', { 'data-policy': name },
            h('td', {}, h('strong', {}, name)),
            h('td', {}, String(p.level)),
            h('td', {}, (p.roles || []).join(', ')),
            h('td', {}, String(p.min_unit_tokens)),
            h('td', { className: 'notes' }, notes.map((n) => h('div', {}, n))),
            h('td', { className: 'row-actions' },
              readOnly ? null : h('button', { onClick: () => this._startEdit(name) }, 'Edit'),
              readOnly ? null : h('button', { className: 'danger', onClick: () => this._deletePolicy(name) }, 'Delete'))),
        );
      }
      table.appendChild(body);
      section.appendChild(table);
    }
    if (!readOnly) {
      section.appendChild(h('button', { onClick: () => this._startEdit(null), 'data-testid': 'pc-new-policy' }, 'New policy'));
    }
    return section;
  }

  _paintPolicyForm() {
    const ed = this._editing;
    const p = ed.policy;
    const repaint = () => this._paint();
    const num = (key, label, help) =>
      h('label', { className: 'field' }, label,
        h('input', { type: 'number', min: 0, value: String(p[key]), onChange: (e) => { p[key] = intOr(e.target.value, POLICY_DEFAULTS[key]); } }),
        help ? h('span', { className: 'muted' }, help) : null);

    const form = h('section', { className: 'form' }, h('h2', {}, ed.originalName ? `Edit policy "${ed.originalName}"` : 'New policy'));
    form.appendChild(
      h('label', { className: 'field' }, 'Name',
        h('input', { type: 'text', value: ed.name, maxLength: 64, 'data-testid': 'pc-policy-name', onInput: (e) => { ed.name = e.target.value; } })),
    );
    form.appendChild(
      h('label', { className: 'field' }, 'Level',
        h('select', { 'data-testid': 'pc-policy-level', onChange: (e) => { p.level = Number(e.target.value); repaint(); } },
          LEVELS.map((l) => h('option', { value: String(l.value), selected: p.level === l.value }, l.label)))),
    );
    form.appendChild(
      h('fieldset', {}, h('legend', {}, 'Roles'),
        ROLES.map((r) => h('label', { className: 'inline' },
          h('input', {
            type: 'checkbox', checked: (p.roles || []).includes(r),
            onChange: (e) => {
              const set = new Set(p.roles || []);
              if (e.target.checked) set.add(r); else set.delete(r);
              p.roles = ROLES.filter((x) => set.has(x));
            },
          }), r)),
        h('div', { className: 'muted' }, 'Tool outputs are where agent tokens are. System prompts never go to an external engine.')),
    );
    form.appendChild(num('min_unit_tokens', 'Minimum unit size (tokens)', 'Smaller messages and tool outputs are left alone.'));
    form.appendChild(num('protect_recent_turns', 'Protected recent turns', 'The newest turns stay at level 1 at most.'));
    form.appendChild(
      h('label', { className: 'field' }, 'Cache mode',
        h('select', { onChange: (e) => { p.cache_mode = e.target.value; } },
          h('option', { value: 'deterministic', selected: p.cache_mode === 'deterministic' }, 'Deterministic: compress everything selected, the same way every turn'),
          h('option', { value: 'skip_cached', selected: p.cache_mode === 'skip_cached' }, 'Skip cached: leave everything up to the last cache breakpoint alone'))),
    );
    form.appendChild(
      h('label', { className: 'inline' },
        h('input', { type: 'checkbox', checked: !!p.dedupe_references, onChange: (e) => { p.dedupe_references = e.target.checked; } }),
        'Replace a repeated tool output with a reference to the earlier one'),
    );
    if (p.level >= 3) {
      form.appendChild(num('max_unit_tokens', 'Maximum unit size (tokens)', 'Level 3: longer tool outputs are cut to their head and tail, with a marker.'));
      form.appendChild(num('mask_after_turns', 'Mask after turns (0 = off)', 'Tool results older than this become a placeholder; 0 turns masking off (the default). Each masking step rewrites the vendor\'s prompt cache, so it pays where cached input is not much cheaper than input (OpenAI chat models with a 50% cache discount) and costs more than it saves where cache reads are very cheap (Anthropic, GPT-5).'));
      form.appendChild(num('mask_step_tokens', 'Mask step (tokens)', 'Old turns are masked in steps of at least this many tokens.'));
      if (!this._config.lossy_with_request_filters) {
        form.appendChild(h('div', { className: 'note' }, 'Level 3 is skipped on LLMs with request filters, so the filters still see the original text. See Advanced settings.'));
      }
    }
    form.appendChild(
      h('label', { className: 'field' }, 'Protect patterns (one regular expression per line)',
        h('textarea', {
          rows: 3, value: (p.protect_patterns || []).join('\n'), 'data-testid': 'pc-protect-patterns',
          onChange: (e) => { p.protect_patterns = e.target.value.split('\n').map((s) => s.trim()).filter(Boolean); },
        }),
        h('span', { className: 'muted' }, 'Text matching these (ticket IDs, account numbers) is never removed by lossy levels. RE2 syntax.')),
    );
    form.appendChild(this._paintToolOverrides(p));
    form.appendChild(this._paintDropTools(p, ed.originalDrop));
    if (p.engine) {
      form.appendChild(h('div', { className: 'note' }, `This policy sends selected units to external engine "${p.engine.name}" after levels 1–3. Its credentials are set in the plugin's configuration form; the engine setting is kept as it is here.`));
    }
    form.appendChild(
      h('div', { className: 'actions' },
        h('button', { className: 'primary', onClick: () => this._applyEdit(), 'data-testid': 'pc-apply-policy' }, 'Apply'),
        h('button', { onClick: () => { this._editing = null; this._setErrors([]); } }, 'Cancel')),
    );
    return form;
  }

  _paintToolOverrides(p) {
    const overrides = p.tool_overrides || {};
    const box = h('fieldset', {}, h('legend', {}, 'Per-tool overrides'));
    const names = Object.keys(overrides).sort();
    for (const tool of names) {
      const o = overrides[tool];
      box.appendChild(
        h('div', { className: 'override' },
          h('code', {}, tool),
          h('label', { className: 'inline' },
            h('input', { type: 'checkbox', checked: !!o.exclude, onChange: (e) => { if (e.target.checked) o.exclude = true; else delete o.exclude; } }),
            'exclude'),
          h('label', { className: 'inline' }, 'level',
            h('select', {
              onChange: (e) => { if (e.target.value === '') delete o.level; else o.level = Number(e.target.value); },
            }, h('option', { value: '', selected: o.level === undefined }, 'policy'),
            LEVELS.map((l) => h('option', { value: String(l.value), selected: o.level === l.value }, String(l.value))))),
          h('label', { className: 'inline' }, 'budget (tokens)',
            h('input', {
              type: 'number', min: 0, className: 'small', value: o.max_unit_tokens ? String(o.max_unit_tokens) : '',
              onChange: (e) => { const n = intOr(e.target.value, 0); if (n > 0) o.max_unit_tokens = n; else delete o.max_unit_tokens; },
            })),
          h('button', { className: 'danger', onClick: () => { delete overrides[tool]; p.tool_overrides = overrides; this._paint(); } }, 'Remove')),
      );
    }
    const input = h('input', { type: 'text', placeholder: 'Tool name, as the model calls it', className: 'medium' });
    box.appendChild(
      h('div', { className: 'override' }, input,
        h('button', {
          onClick: () => {
            const tool = input.value.trim();
            if (!tool) return;
            overrides[tool] = overrides[tool] || { exclude: true };
            p.tool_overrides = overrides;
            this._paint();
          },
        }, 'Add override')),
      h('div', { className: 'muted' }, 'Budget: at level 2, this tool\'s long JSON arrays are cut harder (down to one item at each end) until the output fits. Text outputs ignore it.'),
    );
    return box;
  }

  // _paintDropTools edits the policy's drop_tools: tool definitions removed
  // from requests. Suggestions are the tools an App's models never called;
  // tool names come from clients and are text nodes.
  _paintDropTools(p, originalDrop) {
    const list = p.drop_tools || [];
    const box = h('fieldset', { 'data-testid': 'pc-drop-tools-field' }, h('legend', {}, 'Drop tool definitions'),
      h('textarea', {
        rows: 3, value: list.join('\n'), spellcheck: false, 'data-testid': 'pc-drop-tools',
        onChange: (e) => { p.drop_tools = e.target.value.split('\n').map((s) => s.trim()).filter(Boolean); this._paint(); },
      }),
      h('div', { className: 'muted' },
        'One tool name per line, or a prefix ending in "*". Matching definitions are removed from every request on this policy, '
        + 'except a tool that a tool call in the conversation or tool_choice names, and except when removing them would leave no tools while the conversation holds tool calls.'));
    if (JSON.stringify(list) !== originalDrop) {
      box.appendChild(h('div', { className: 'note', role: 'status', 'data-testid': 'pc-drop-tools-warning' },
        'Changing the drop list changes the tools every App on this policy sends, so each of their sessions misses the prompt cache once. '
        + 'Bind a separate policy to an App (app: binding or App metadata) to change only that App.'));
    }

    // undefined or 'loading': not read yet; null: not available to this
    // user; an array: what the savings records hold.
    const suggestions = this._dropSuggestions;
    if (suggestions === undefined || suggestions === 'loading') {
      box.appendChild(h('div', { className: 'muted' }, 'Loading suggestions…'));
    } else if (Array.isArray(suggestions) && suggestions.length) {
      const present = new Set(list);
      const rows = [];
      for (const s of suggestions) {
        const names = s.names.filter((n) => !present.has(n));
        if (!names.length) continue;
        rows.push(h('div', { className: 'suggest-row' },
          h('span', { className: 'suggest-app' }, s.app),
          names.map((n) => h('button', {
            className: 'chip', type: 'button', 'data-testid': 'pc-drop-suggestion',
            onClick: () => { p.drop_tools = [...(p.drop_tools || []), n]; this._paint(); },
          }, '+ ', n))));
      }
      if (rows.length) {
        box.appendChild(h('div', { className: 'suggest', 'data-testid': 'pc-drop-suggestions' },
          h('div', { className: 'muted' }, 'Offered but never called in the last 7 days (from the savings records). Click to add:'), rows));
      }
    } else if (Array.isArray(suggestions)) {
      box.appendChild(h('div', { className: 'muted' }, 'No App offered a tool it never called in the last 7 days.'));
    }
    return box;
  }

  _paintBindings(readOnly) {
    const cfg = this._config;
    const view = this._view || {};
    const section = h('section', {}, h('h2', {}, 'Bindings'));
    const keys = Object.keys(cfg.bindings).sort((a, b) => this._bindingLabel(a).localeCompare(this._bindingLabel(b)));
    const policyNames = [...Object.keys(cfg.policies).sort(), ...(cfg.policies.default ? [] : ['default']), 'off'];

    if (keys.length === 0) {
      section.appendChild(h('p', { className: 'muted' }, 'No bindings: every request uses the default policy.'));
    } else {
      const table = h('table', {}, h('thead', {}, h('tr', {}, ['Target', 'Policy', ''].map((c) => h('th', {}, c)))));
      const body = h('tbody');
      for (const key of keys) {
        body.appendChild(
          h('tr', { 'data-binding': key },
            h('td', {}, this._bindingLabel(key), h('div', { className: 'muted' }, key),
              this._bindingSkipsLossy(key)
                ? h('div', { className: 'skip-marker', 'data-testid': 'pc-filter-skip' }, 'Level 3 and engines skipped: this LLM has request filters')
                : null),
            h('td', {},
              h('select', {
                disabled: readOnly,
                onChange: (e) => { cfg.bindings[key] = e.target.value; this._paint(); },
              }, [...new Set([cfg.bindings[key], ...policyNames])].map((n) => h('option', { value: n, selected: cfg.bindings[key] === n }, policyNames.includes(n) ? n : `${n} (unknown)`)))),
            h('td', { className: 'row-actions' },
              readOnly ? null : h('button', { className: 'danger', onClick: () => { delete cfg.bindings[key]; this._paint(); } }, 'Remove'))),
        );
      }
      table.appendChild(body);
      section.appendChild(table);
    }

    if (!readOnly) {
      const llmSelect = h('select', { 'data-testid': 'pc-bind-llm' },
        h('option', { value: '' }, 'Choose an LLM…'),
        (view.llms || []).map((l) => h('option', { value: String(l.id) }, `${l.name} (${l.vendor}${l.active ? '' : ', inactive'}${l.has_request_filters ? ', request filters' : ''})`)));
      const llmPolicy = h('select', {}, policyNames.map((n) => h('option', { value: n }, n)));
      section.appendChild(
        h('div', { className: 'add-binding' }, 'Bind LLM', llmSelect, 'to', llmPolicy,
          h('button', { onClick: () => this._addBinding('llm', llmSelect.value, llmPolicy.value) }, 'Add')),
      );
      if (view.llms_truncated) section.appendChild(h('div', { className: 'muted' }, 'Only the first 5,000 LLMs are listed.'));

      const appList = 'pc-apps-' + Math.random().toString(36).slice(2);
      const appInput = h('input', { type: 'text', placeholder: 'App name or ID', className: 'medium', 'data-testid': 'pc-bind-app' });
      appInput.setAttribute('list', appList);
      const datalist = h('datalist', { id: appList }, (view.apps || []).map((a) => h('option', { value: `${a.id} - ${a.name}` })));
      const appPolicy = h('select', {}, policyNames.map((n) => h('option', { value: n }, n)));
      section.appendChild(
        h('div', { className: 'add-binding' }, 'Bind App', appInput, datalist, 'to', appPolicy,
          h('button', {
            onClick: () => {
              const m = /^\s*(\d+)/.exec(appInput.value);
              this._addBinding('app', m && Number(m[1]) > 0 ? String(Number(m[1])) : '', appPolicy.value);
            },
          }, 'Add')),
      );
      if (view.apps_truncated) section.appendChild(h('div', { className: 'muted' }, 'Only the first 5,000 Apps are suggested; enter any App ID.'));
    }
    return section;
  }

  _paintAdvanced(readOnly) {
    const cfg = this._config;
    const bool = (key, label, help, dflt) =>
      h('label', { className: 'inline block' },
        h('input', {
          type: 'checkbox', disabled: readOnly, checked: cfg[key] === undefined ? dflt : !!cfg[key],
          onChange: (e) => { cfg[key] = e.target.checked; this._paint(); },
        }),
        h('span', {}, label), h('div', { className: 'muted' }, help));
    return h('details', {},
      h('summary', {}, 'Advanced settings'),
      bool('allow_client_bypass', 'Allow client bypass', 'Honour the request header X-Tyk-Compression: off.', true),
      bool('lossy_with_request_filters', 'Run lossy levels on LLMs with request filters',
        'Off by default: level 3 and engines are skipped there, so the filters screen the original text and not the compressed body.', false),
      h('label', { className: 'field' }, 'Engine failure mode',
        h('select', { disabled: readOnly, onChange: (e) => { cfg.failure_mode = e.target.value; this._paint(); } },
          h('option', { value: 'open', selected: (cfg.failure_mode || 'open') === 'open' }, 'Open: forward the request'),
          h('option', { value: 'closed', selected: cfg.failure_mode === 'closed' }, 'Closed: answer 502'))),
      h('label', { className: 'field' }, 'Maximum body size (bytes)',
        h('input', {
          type: 'number', min: 1, disabled: readOnly, value: String(cfg.max_body_bytes || 4194304),
          onChange: (e) => { cfg.max_body_bytes = intOr(e.target.value, 4194304); this._paint(); },
        }),
        h('span', { className: 'muted' }, 'Larger bodies pass through uncompressed.')),
      h('label', { className: 'field' }, 'Cache-drop warning (fraction)',
        h('input', {
          type: 'number', min: 0, max: 1, step: 0.05, disabled: readOnly,
          value: String(cfg.cache_drop_alert === undefined ? 0.25 : cfg.cache_drop_alert),
          onChange: (e) => {
            const x = Number.parseFloat(e.target.value);
            cfg.cache_drop_alert = Number.isFinite(x) ? x : 0.25;
            this._paint();
          },
        }),
        h('span', { className: 'muted' }, 'The dashboard warns when the cache-read share falls by more than this fraction within an hour of a policy change. 0 turns it off.')),
      h('p', { className: 'muted' }, 'Engine credentials (engine_<name>_url and engine_<name>_token) are kept as they are. Add $SECRET/ references in the plugin\'s configuration form: plugins may not add them.'));
  }
}

const STYLES = `
  :host { display: block; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; color: #1f2937; }
  .page { padding: 24px; max-width: 1100px; }
  h1 { font-size: 22px; font-weight: 600; margin: 0 0 8px; }
  h2 { font-size: 16px; font-weight: 600; margin: 0 0 12px; }
  .lead { color: #4b5563; margin: 0 0 16px; }
  section, details { background: #fff; border: 1px solid #e5e7eb; border-radius: 8px; padding: 16px; margin-bottom: 16px; }
  summary { cursor: pointer; font-weight: 600; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 12px; }
  th, td { text-align: left; padding: 8px; border-bottom: 1px solid #f3f4f6; vertical-align: top; font-size: 14px; }
  th { color: #6b7280; font-weight: 500; }
  .notes div { color: #92400e; font-size: 12px; }
  .skip-marker { color: #92400e; font-size: 12px; margin-top: 2px; }
  .row-actions { white-space: nowrap; text-align: right; }
  button { font: inherit; padding: 6px 12px; border-radius: 6px; border: 1px solid #d1d5db; background: #fff; cursor: pointer; margin-right: 6px; }
  button:disabled { opacity: 0.5; cursor: default; }
  button.primary { background: #4f46e5; border-color: #4f46e5; color: #fff; }
  button.danger { color: #b91c1c; }
  .field { display: flex; flex-direction: column; gap: 4px; margin: 0 0 12px; font-size: 14px; max-width: 520px; }
  .inline { display: inline-flex; align-items: center; gap: 6px; margin-right: 12px; font-size: 14px; }
  .inline.block { display: block; margin: 0 0 12px; }
  input[type=text], input[type=number], select, textarea { font: inherit; padding: 6px 8px; border: 1px solid #d1d5db; border-radius: 6px; }
  input.small { width: 90px; }
  input.medium { width: 260px; }
  fieldset { border: 1px solid #e5e7eb; border-radius: 6px; margin: 0 0 12px; padding: 8px 12px; }
  legend { font-size: 14px; padding: 0 4px; }
  .muted { color: #6b7280; font-size: 12px; }
  .modes { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 12px; }
  .mode { flex: 1 1 200px; border: 1px solid #e5e7eb; border-radius: 6px; padding: 8px; display: flex; flex-direction: column; gap: 2px; cursor: pointer; }
  .mode.selected { border-color: #4f46e5; background: #eef2ff; }
  .mode-label { font-weight: 600; }
  .override, .add-binding { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 6px 0; font-size: 14px; }
  .error { background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; border-radius: 6px; padding: 8px 12px; margin-bottom: 12px; }
  .error ul { margin: 4px 0 0; padding-left: 20px; }
  .info { background: #ecfdf5; color: #065f46; border: 1px solid #a7f3d0; border-radius: 6px; padding: 8px 12px; margin-bottom: 12px; }
  .note { background: #fffbeb; color: #92400e; border: 1px solid #fde68a; border-radius: 6px; padding: 8px 12px; margin: 0 0 12px; font-size: 13px; }
  .actions { margin-top: 8px; display: flex; align-items: center; gap: 4px; }
  code { background: #f3f4f6; padding: 1px 4px; border-radius: 4px; }
  .suggest { margin-top: 8px; }
  .suggest-row { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; margin-top: 4px; }
  .suggest-app { font-size: 12px; color: #374151; margin-right: 4px; }
  button.chip { padding: 1px 8px; font-size: 12px; border-radius: 10px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
`;

if (!customElements.get('prompt-compressor-policies')) {
  customElements.define('prompt-compressor-policies', PromptCompressorPolicies);
}
