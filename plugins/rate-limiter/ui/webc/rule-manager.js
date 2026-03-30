class RateLimitRuleManager extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.rules = [];
    this.loading = true;
  }

  connectedCallback() {
    this.render();
    this.waitForPluginAPI();
  }

  async waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) {
        this.loadRules();
        return;
      }
      await new Promise(r => setTimeout(r, 100));
    }
    this.showMessage('Plugin API initialization timeout', 'error');
  }

  async loadRules() {
    this.setLoading(true);
    try {
      const result = await this.pluginAPI.call('listRules', {});
      this.rules = result.rules || [];
      this.renderRules();
    } catch (err) {
      this.showMessage('Failed to load rules: ' + err.message, 'error');
    } finally {
      this.setLoading(false);
    }
  }

  render() {
    this.shadowRoot.innerHTML = `
      <style>
        :host {
          display: block;
          padding: 24px;
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
          color: #1a1a1a;
        }
        .header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          margin-bottom: 24px;
        }
        h1 { margin: 0; font-size: 24px; font-weight: 600; }
        .subtitle { color: #666; margin-top: 4px; font-size: 14px; }
        .btn {
          padding: 8px 16px;
          border: none;
          border-radius: 6px;
          cursor: pointer;
          font-size: 14px;
          font-weight: 500;
          transition: background 0.2s;
        }
        .btn-primary {
          background: #2563eb;
          color: white;
        }
        .btn-primary:hover { background: #1d4ed8; }
        .btn-secondary {
          background: #f1f5f9;
          color: #475569;
          border: 1px solid #e2e8f0;
        }
        .btn-secondary:hover { background: #e2e8f0; }
        .btn-danger {
          background: #fee2e2;
          color: #dc2626;
          border: 1px solid #fecaca;
        }
        .btn-danger:hover { background: #fecaca; }
        .btn-sm { padding: 4px 10px; font-size: 12px; }
        table {
          width: 100%;
          border-collapse: collapse;
          background: white;
          border-radius: 8px;
          overflow: hidden;
          box-shadow: 0 1px 3px rgba(0,0,0,0.1);
        }
        th, td {
          padding: 12px 16px;
          text-align: left;
          border-bottom: 1px solid #f1f5f9;
        }
        th {
          background: #f8fafc;
          font-weight: 600;
          font-size: 12px;
          text-transform: uppercase;
          color: #64748b;
          letter-spacing: 0.05em;
        }
        td { font-size: 14px; }
        .badge {
          display: inline-block;
          padding: 2px 8px;
          border-radius: 12px;
          font-size: 11px;
          font-weight: 600;
          margin: 1px 2px;
        }
        .badge-dim {
          background: #e0e7ff;
          color: #3730a3;
        }
        .badge-enforce {
          background: #fee2e2;
          color: #dc2626;
        }
        .badge-log {
          background: #fef3c7;
          color: #92400e;
        }
        .badge-type {
          background: #f0fdf4;
          color: #166534;
        }
        .toggle {
          position: relative;
          width: 40px;
          height: 22px;
          display: inline-block;
        }
        .toggle input {
          opacity: 0;
          width: 0;
          height: 0;
        }
        .toggle-slider {
          position: absolute;
          inset: 0;
          background: #cbd5e1;
          border-radius: 22px;
          cursor: pointer;
          transition: background 0.2s;
        }
        .toggle-slider::before {
          content: '';
          position: absolute;
          width: 18px;
          height: 18px;
          left: 2px;
          top: 2px;
          background: white;
          border-radius: 50%;
          transition: transform 0.2s;
        }
        .toggle input:checked + .toggle-slider {
          background: #2563eb;
        }
        .toggle input:checked + .toggle-slider::before {
          transform: translateX(18px);
        }
        .actions {
          display: flex;
          gap: 6px;
          align-items: center;
        }
        .priority-btns {
          display: flex;
          flex-direction: column;
          gap: 2px;
        }
        .priority-btn {
          background: none;
          border: 1px solid #e2e8f0;
          border-radius: 3px;
          cursor: pointer;
          padding: 0 4px;
          font-size: 10px;
          line-height: 14px;
          color: #64748b;
        }
        .priority-btn:hover { background: #f1f5f9; }
        .empty-state {
          text-align: center;
          padding: 48px 24px;
          color: #94a3b8;
        }
        .empty-state h3 { color: #64748b; }
        #message {
          display: none;
          padding: 12px 16px;
          border-radius: 6px;
          margin-bottom: 16px;
          font-size: 14px;
        }
        #loading {
          text-align: center;
          padding: 48px;
          color: #94a3b8;
        }
        .dialog-overlay {
          position: fixed;
          inset: 0;
          background: rgba(0,0,0,0.5);
          display: flex;
          align-items: center;
          justify-content: center;
          z-index: 1000;
        }
        .dialog {
          background: white;
          border-radius: 12px;
          padding: 24px;
          width: 520px;
          max-height: 80vh;
          overflow-y: auto;
          box-shadow: 0 20px 60px rgba(0,0,0,0.3);
        }
        .dialog h2 { margin-top: 0; font-size: 18px; }
        .form-group {
          margin-bottom: 16px;
        }
        .form-group label {
          display: block;
          margin-bottom: 4px;
          font-size: 13px;
          font-weight: 600;
          color: #374151;
        }
        .form-group input[type="text"],
        .form-group input[type="number"],
        .form-group select {
          width: 100%;
          padding: 8px 12px;
          border: 1px solid #d1d5db;
          border-radius: 6px;
          font-size: 14px;
          box-sizing: border-box;
        }
        .form-group input:focus,
        .form-group select:focus {
          outline: none;
          border-color: #2563eb;
          box-shadow: 0 0 0 3px rgba(37,99,235,0.1);
        }
        .checkbox-group {
          display: flex;
          flex-wrap: wrap;
          gap: 8px;
        }
        .checkbox-group label {
          display: flex;
          align-items: center;
          gap: 4px;
          font-weight: 400;
          background: #f8fafc;
          padding: 4px 10px;
          border-radius: 6px;
          border: 1px solid #e2e8f0;
          cursor: pointer;
          font-size: 13px;
        }
        .checkbox-group label:has(input:checked) {
          background: #e0e7ff;
          border-color: #818cf8;
        }
        .radio-group {
          display: flex;
          gap: 12px;
        }
        .radio-group label {
          display: flex;
          align-items: center;
          gap: 4px;
          font-weight: 400;
          cursor: pointer;
          font-size: 13px;
        }
        .dialog-actions {
          display: flex;
          justify-content: flex-end;
          gap: 8px;
          margin-top: 20px;
        }
        .limit-value { font-weight: 600; font-variant-numeric: tabular-nums; }
      </style>

      <div id="message"></div>

      <div class="header">
        <div>
          <h1>Rate Limit Rules</h1>
          <div class="subtitle">Manage sliding-window rate limits with composable key dimensions</div>
        </div>
        <button class="btn btn-primary" id="btn-create">+ New Rule</button>
      </div>

      <div id="loading">Loading rules...</div>
      <div id="content" style="display:none">
        <table>
          <thead>
            <tr>
              <th style="width:30px"></th>
              <th>Name</th>
              <th>Dimensions</th>
              <th>Limit</th>
              <th>Action</th>
              <th>Enabled</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="rules-body"></tbody>
        </table>
        <div id="empty-state" class="empty-state" style="display:none">
          <h3>No rate limit rules configured</h3>
          <p>Create your first rule to start limiting LLM requests by app, user, model, or globally.</p>
        </div>
      </div>
    `;

    this.shadowRoot.querySelector('#btn-create').addEventListener('click', () => this.showDialog());
  }

  setLoading(loading) {
    const el = this.shadowRoot.querySelector('#loading');
    const content = this.shadowRoot.querySelector('#content');
    if (el && content) {
      el.style.display = loading ? 'block' : 'none';
      content.style.display = loading ? 'none' : 'block';
    }
  }

  renderRules() {
    const tbody = this.shadowRoot.querySelector('#rules-body');
    const empty = this.shadowRoot.querySelector('#empty-state');
    if (!tbody) return;

    if (this.rules.length === 0) {
      tbody.innerHTML = '';
      if (empty) empty.style.display = 'block';
      return;
    }
    if (empty) empty.style.display = 'none';

    tbody.innerHTML = this.rules.map((rule, idx) => `
      <tr data-id="${rule.id}">
        <td>
          <div class="priority-btns">
            ${idx > 0 ? '<button class="priority-btn btn-up" title="Move up">\u25B2</button>' : ''}
            ${idx < this.rules.length - 1 ? '<button class="priority-btn btn-down" title="Move down">\u25BC</button>' : ''}
          </div>
        </td>
        <td><strong>${this.esc(rule.name)}</strong></td>
        <td>${rule.dimensions.map(d => `<span class="badge badge-dim">${d}</span>`).join('')}</td>
        <td>
          <span class="badge badge-type">${rule.limit.type}</span>
          <span class="limit-value">${rule.limit.value.toLocaleString()}</span>
        </td>
        <td><span class="badge ${rule.action === 'enforce' ? 'badge-enforce' : 'badge-log'}">${rule.action || 'enforce'}</span></td>
        <td>
          <label class="toggle">
            <input type="checkbox" class="toggle-enabled" ${rule.enabled ? 'checked' : ''}>
            <span class="toggle-slider"></span>
          </label>
        </td>
        <td>
          <div class="actions">
            <button class="btn btn-secondary btn-sm btn-edit">Edit</button>
            <button class="btn btn-danger btn-sm btn-delete">Delete</button>
          </div>
        </td>
      </tr>
    `).join('');

    // Wire events
    tbody.querySelectorAll('tr').forEach(tr => {
      const id = tr.dataset.id;
      const rule = this.rules.find(r => r.id === id);

      tr.querySelector('.btn-edit')?.addEventListener('click', () => this.showDialog(rule));
      tr.querySelector('.btn-delete')?.addEventListener('click', () => this.deleteRule(id));

      const toggle = tr.querySelector('.toggle-enabled');
      if (toggle) {
        toggle.addEventListener('change', () => this.toggleEnabled(rule, toggle.checked));
      }

      tr.querySelector('.btn-up')?.addEventListener('click', () => this.moveRule(id, -1));
      tr.querySelector('.btn-down')?.addEventListener('click', () => this.moveRule(id, 1));
    });
  }

  showDialog(editRule) {
    const isEdit = !!editRule;
    const dims = ['app_id', 'user_id', 'model', 'llm_id', 'api_key', 'global'];
    const selectedDims = isEdit ? editRule.dimensions : [];

    const overlay = document.createElement('div');
    overlay.className = 'dialog-overlay';
    overlay.innerHTML = `
      <div class="dialog">
        <h2>${isEdit ? 'Edit Rule' : 'Create Rule'}</h2>

        <div class="form-group">
          <label>Rule Name</label>
          <input type="text" id="d-name" value="${isEdit ? this.esc(editRule.name) : ''}" placeholder="e.g., Per-app request limit">
        </div>

        <div class="form-group">
          <label>Key Dimensions</label>
          <div class="checkbox-group">
            ${dims.map(d => `
              <label><input type="checkbox" value="${d}" ${selectedDims.includes(d) ? 'checked' : ''}> ${d}</label>
            `).join('')}
          </div>
        </div>

        <div class="form-group">
          <label>Limit Type</label>
          <select id="d-limit-type">
            <option value="requests" ${isEdit && editRule.limit.type === 'requests' ? 'selected' : ''}>Requests per window</option>
            <option value="tokens" ${isEdit && editRule.limit.type === 'tokens' ? 'selected' : ''}>Tokens per window</option>
            <option value="concurrent" ${isEdit && editRule.limit.type === 'concurrent' ? 'selected' : ''}>Concurrent requests</option>
          </select>
        </div>

        <div class="form-group">
          <label>Limit Value</label>
          <input type="number" id="d-limit-value" min="1" value="${isEdit ? editRule.limit.value : '100'}" placeholder="100">
        </div>

        <div class="form-group">
          <label>Action</label>
          <div class="radio-group">
            <label><input type="radio" name="d-action" value="enforce" ${!isEdit || editRule.action === 'enforce' ? 'checked' : ''}> Enforce (block on breach)</label>
            <label><input type="radio" name="d-action" value="log" ${isEdit && editRule.action === 'log' ? 'checked' : ''}> Shadow (log only)</label>
          </div>
        </div>

        <div class="dialog-actions">
          <button class="btn btn-secondary btn-cancel">Cancel</button>
          <button class="btn btn-primary btn-submit">${isEdit ? 'Save Changes' : 'Create Rule'}</button>
        </div>
      </div>
    `;

    this.shadowRoot.appendChild(overlay);

    overlay.querySelector('.btn-cancel').addEventListener('click', () => overlay.remove());
    overlay.addEventListener('click', (e) => {
      if (e.target === overlay) overlay.remove();
    });

    overlay.querySelector('.btn-submit').addEventListener('click', async () => {
      const name = overlay.querySelector('#d-name').value.trim();
      const dimensions = [...overlay.querySelectorAll('.checkbox-group input:checked')].map(cb => cb.value);
      const limitType = overlay.querySelector('#d-limit-type').value;
      const limitValue = parseInt(overlay.querySelector('#d-limit-value').value, 10);
      const action = overlay.querySelector('input[name="d-action"]:checked').value;

      if (!name) { this.showMessage('Rule name is required', 'error'); return; }
      if (dimensions.length === 0) { this.showMessage('Select at least one dimension', 'error'); return; }
      if (!limitValue || limitValue < 1) { this.showMessage('Limit value must be > 0', 'error'); return; }

      const btn = overlay.querySelector('.btn-submit');
      btn.disabled = true;
      btn.textContent = 'Saving...';

      try {
        if (isEdit) {
          await this.pluginAPI.call('updateRule', {
            id: editRule.id,
            name,
            dimensions,
            limit: { type: limitType, value: limitValue },
            action,
            enabled: editRule.enabled,
          });
        } else {
          await this.pluginAPI.call('createRule', {
            name,
            dimensions,
            limit: { type: limitType, value: limitValue },
            action,
          });
        }
        overlay.remove();
        this.showMessage(isEdit ? 'Rule updated' : 'Rule created', 'success');
        this.loadRules();
      } catch (err) {
        this.showMessage(err.message, 'error');
        btn.disabled = false;
        btn.textContent = isEdit ? 'Save Changes' : 'Create Rule';
      }
    });
  }

  async deleteRule(id) {
    if (!confirm('Delete this rule? This cannot be undone.')) return;
    try {
      await this.pluginAPI.call('deleteRule', { id });
      this.showMessage('Rule deleted', 'success');
      this.loadRules();
    } catch (err) {
      this.showMessage('Delete failed: ' + err.message, 'error');
    }
  }

  async toggleEnabled(rule, enabled) {
    try {
      await this.pluginAPI.call('updateRule', {
        id: rule.id,
        name: rule.name,
        dimensions: rule.dimensions,
        limit: rule.limit,
        action: rule.action,
        enabled,
      });
    } catch (err) {
      this.showMessage('Toggle failed: ' + err.message, 'error');
      this.loadRules();
    }
  }

  async moveRule(id, direction) {
    const ids = this.rules.map(r => r.id);
    const idx = ids.indexOf(id);
    if (idx < 0) return;

    const newIdx = idx + direction;
    if (newIdx < 0 || newIdx >= ids.length) return;

    [ids[idx], ids[newIdx]] = [ids[newIdx], ids[idx]];

    try {
      await this.pluginAPI.call('reorderRules', { rule_ids: ids });
      this.loadRules();
    } catch (err) {
      this.showMessage('Reorder failed: ' + err.message, 'error');
    }
  }

  showMessage(text, type) {
    const el = this.shadowRoot.querySelector('#message');
    if (!el) return;
    el.style.display = 'block';
    el.textContent = text;
    el.style.background = type === 'success' ? '#f0fdf4' : '#fef2f2';
    el.style.color = type === 'success' ? '#166534' : '#991b1b';
    el.style.border = '1px solid ' + (type === 'success' ? '#bbf7d0' : '#fecaca');
    clearTimeout(this._msgTimeout);
    this._msgTimeout = setTimeout(() => { el.style.display = 'none'; }, 5000);
  }

  esc(str) {
    const d = document.createElement('div');
    d.textContent = str || '';
    return d.innerHTML;
  }
}

customElements.define('rate-limit-rule-manager', RateLimitRuleManager);
