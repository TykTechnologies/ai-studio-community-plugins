class PriceSyncDashboard extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.pluginAPI = null;
    this.status = null;
    this.history = [];
    this.syncing = false;
  }

  connectedCallback() {
    this.render();
    this.waitForAPI();
  }

  async waitForAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) {
        this.loadData();
        return;
      }
      await new Promise(r => setTimeout(r, 100));
    }
    this.showError('Plugin API not available');
  }

  async loadData() {
    try {
      const [status, history] = await Promise.all([
        this.pluginAPI.call('getStatus', {}),
        this.pluginAPI.call('getSyncHistory', {}),
      ]);
      this.status = status;
      this.history = Array.isArray(history) ? history : [];
      this.render();
    } catch (e) {
      this.showError('Failed to load data: ' + e.message);
    }
  }

  async triggerSync() {
    if (this.syncing) return;
    this.syncing = true;
    this.render();

    try {
      const result = await this.pluginAPI.call('triggerSync', {});
      this.syncing = false;
      await this.loadData();
    } catch (e) {
      this.syncing = false;
      this.showError('Sync failed: ' + e.message);
    }
  }

  showError(msg) {
    this.shadowRoot.querySelector('.error-banner')?.remove();
    const banner = document.createElement('div');
    banner.className = 'error-banner';
    banner.textContent = msg;
    this.shadowRoot.querySelector('.container')?.prepend(banner);
  }

  formatDate(dateStr) {
    if (!dateStr) return 'Never';
    const d = new Date(dateStr);
    return d.toLocaleString();
  }

  render() {
    const s = this.status;
    const hasSync = s && s.status === 'ok' && s.last_sync;
    const ls = hasSync ? s.last_sync : null;
    const cfg = s?.config || {};

    this.shadowRoot.innerHTML = `
      <style>
        :host { display: block; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; color: #1a1a2e; }
        .container { max-width: 900px; margin: 0 auto; padding: 24px; }
        h2 { font-size: 20px; font-weight: 600; margin: 0 0 20px 0; }
        h3 { font-size: 16px; font-weight: 600; margin: 20px 0 12px 0; }
        .card { background: #fff; border: 1px solid #e2e8f0; border-radius: 8px; padding: 20px; margin-bottom: 16px; }
        .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; margin-bottom: 16px; }
        .stat { text-align: center; padding: 12px; background: #f8fafc; border-radius: 6px; }
        .stat-value { font-size: 24px; font-weight: 700; color: #1e40af; }
        .stat-label { font-size: 12px; color: #64748b; margin-top: 4px; }
        .stat-value.created { color: #16a34a; }
        .stat-value.updated { color: #2563eb; }
        .stat-value.skipped { color: #9ca3af; }
        .stat-value.errors { color: #dc2626; }
        .btn { padding: 8px 16px; border-radius: 6px; border: 1px solid #e2e8f0; background: #fff; cursor: pointer; font-size: 14px; font-weight: 500; }
        .btn:hover { background: #f1f5f9; }
        .btn-primary { background: #2563eb; color: #fff; border-color: #2563eb; }
        .btn-primary:hover { background: #1d4ed8; }
        .btn:disabled { opacity: 0.5; cursor: not-allowed; }
        .config-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; font-size: 14px; }
        .config-grid dt { color: #64748b; }
        .config-grid dd { margin: 0; font-weight: 500; }
        table { width: 100%; border-collapse: collapse; font-size: 14px; }
        th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid #f1f5f9; }
        th { color: #64748b; font-weight: 500; font-size: 12px; text-transform: uppercase; }
        .badge { display: inline-block; padding: 2px 8px; border-radius: 4px; font-size: 12px; font-weight: 500; }
        .badge-success { background: #dcfce7; color: #166534; }
        .badge-warning { background: #fef9c3; color: #854d0e; }
        .badge-info { background: #dbeafe; color: #1e40af; }
        .error-banner { background: #fef2f2; color: #991b1b; padding: 12px; border-radius: 6px; margin-bottom: 16px; border: 1px solid #fecaca; }
        .header-row { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
        .meta { font-size: 13px; color: #64748b; }
      </style>
      <div class="container">
        <div class="header-row">
          <h2>LLM Price Sync</h2>
          <button class="btn btn-primary" id="syncBtn" ${this.syncing ? 'disabled' : ''}>
            ${this.syncing ? 'Syncing...' : 'Sync Now'}
          </button>
        </div>

        ${!s || s.status === 'no_sync_yet' ? `
          <div class="card">
            <p style="color: #64748b;">No sync has been performed yet. Click "Sync Now" to fetch the latest LLM prices.</p>
          </div>
        ` : ''}

        ${hasSync ? `
          <div class="card">
            <h3>Last Sync</h3>
            <p class="meta">Completed ${this.formatDate(ls.started_at)} in ${ls.duration || 'N/A'}</p>
            <div class="stats">
              <div class="stat">
                <div class="stat-value created">${ls.models_created || 0}</div>
                <div class="stat-label">Created</div>
              </div>
              <div class="stat">
                <div class="stat-value updated">${ls.models_updated || 0}</div>
                <div class="stat-label">Updated</div>
              </div>
              <div class="stat">
                <div class="stat-value skipped">${ls.models_skipped || 0}</div>
                <div class="stat-label">Skipped</div>
              </div>
              <div class="stat">
                <div class="stat-value errors">${(ls.errors || []).length}</div>
                <div class="stat-label">Errors</div>
              </div>
            </div>
            ${ls.errors && ls.errors.length > 0 ? `
              <div class="error-banner">
                <strong>Errors:</strong><br>
                ${ls.errors.map(e => `<div>- ${this.escapeHtml(e)}</div>`).join('')}
              </div>
            ` : ''}
          </div>
        ` : ''}

        <div class="card">
          <h3>Configuration</h3>
          <dl class="config-grid">
            <dt>Prices URL</dt>
            <dd>${this.escapeHtml(cfg.prices_url || 'Default')}</dd>
            <dt>Schedule</dt>
            <dd>${this.escapeHtml(cfg.sync_interval_cron || '0 */6 * * *')}</dd>
            <dt>Auto-Create Models</dt>
            <dd>${cfg.auto_create_models !== false ? 'Yes' : 'No'}</dd>
            <dt>Dry Run</dt>
            <dd>${cfg.dry_run ? 'Yes' : 'No'}</dd>
            <dt>Vendor Filter</dt>
            <dd>${cfg.vendor_filter && cfg.vendor_filter.length > 0 ? cfg.vendor_filter.join(', ') : 'All vendors'}</dd>
          </dl>
        </div>

        ${this.history.length > 0 ? `
          <div class="card">
            <h3>Sync History</h3>
            <table>
              <thead>
                <tr>
                  <th>Date</th>
                  <th>Created</th>
                  <th>Updated</th>
                  <th>Skipped</th>
                  <th>Errors</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                ${this.history.map(h => {
                  const entry = typeof h === 'string' ? JSON.parse(h) : h;
                  return `
                    <tr>
                      <td>${this.formatDate(entry.started_at)}</td>
                      <td><span class="badge badge-success">${entry.models_created || 0}</span></td>
                      <td><span class="badge badge-info">${entry.models_updated || 0}</span></td>
                      <td>${entry.models_skipped || 0}</td>
                      <td>${(entry.errors || []).length > 0 ? `<span class="badge badge-warning">${entry.errors.length}</span>` : '0'}</td>
                      <td>${entry.duration || '-'}</td>
                    </tr>
                  `;
                }).join('')}
              </tbody>
            </table>
          </div>
        ` : ''}
      </div>
    `;

    this.shadowRoot.getElementById('syncBtn')?.addEventListener('click', () => this.triggerSync());
  }

  escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str || '';
    return div.innerHTML;
  }
}

customElements.define('price-sync-dashboard', PriceSyncDashboard);
