// LLM Cache Dashboard Web Component
class LLMCacheDashboard extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this.data = {
      metrics: null,
      config: null,
      loading: true
    };
    this.refreshInterval = null;
  }

  connectedCallback() {
    console.log('LLMCacheDashboard component initialized');
    this.render();
    this.setupEventListeners();
    this.waitForPluginAPI();
  }

  disconnectedCallback() {
    if (this.refreshInterval) {
      clearInterval(this.refreshInterval);
    }
  }

  async waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) {
        console.log('Plugin API found, loading data...');
        this.loadData();
        // Auto-refresh every 10 seconds
        this.refreshInterval = setInterval(() => this.loadMetrics(), 10000);
        return;
      }
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    console.error('Plugin API injection timeout');
    this.showError('Plugin API initialization timeout - please refresh the page');
  }

  async loadData() {
    this.setLoading(true);
    try {
      await Promise.all([
        this.loadMetrics(),
        this.loadConfig()
      ]);
    } finally {
      this.setLoading(false);
    }
  }

  async loadMetrics() {
    try {
      const result = await this.pluginAPI.call('getMetrics', {});
      console.log('Metrics loaded:', result);
      this.data.metrics = result;
      this.updateMetricsDisplay();
    } catch (error) {
      console.error('Failed to load metrics:', error);
      this.showError('Failed to load metrics: ' + error.message);
    }
  }

  async loadConfig() {
    try {
      const result = await this.pluginAPI.call('getConfig', {});
      console.log('Config loaded:', result);
      this.data.config = result;
      this.updateConfigDisplay();
    } catch (error) {
      console.error('Failed to load config:', error);
    }
  }

  async clearCache() {
    if (!confirm('Are you sure you want to clear all cached responses? This action cannot be undone.')) {
      return;
    }

    try {
      const result = await this.pluginAPI.call('clearCache', {});
      console.log('Cache cleared:', result);
      this.showSuccess('Cache cleared successfully');
      await this.loadMetrics();
    } catch (error) {
      console.error('Failed to clear cache:', error);
      this.showError('Failed to clear cache: ' + error.message);
    }
  }

  setLoading(loading) {
    this.data.loading = loading;
    const spinner = this.shadowRoot.querySelector('#loading-spinner');
    const content = this.shadowRoot.querySelector('#content');

    if (spinner && content) {
      spinner.style.display = loading ? 'block' : 'none';
      content.style.display = loading ? 'none' : 'block';
    }
  }

  updateMetricsDisplay() {
    const m = this.data.metrics;
    if (!m) return;

    // Check if we're in aggregated mode (control plane) or local mode (edge)
    const isAggregated = m.mode === 'aggregated';

    // Update mode indicator
    const modeIndicator = this.shadowRoot.querySelector('#mode-indicator');
    if (modeIndicator) {
      if (isAggregated) {
        modeIndicator.innerHTML = `<span class="mode-badge aggregated">Aggregated View</span> <span class="edge-count">${m.edge_count || 0} edge instance(s)</span>`;
        modeIndicator.style.display = 'block';
      } else {
        modeIndicator.innerHTML = `<span class="mode-badge local">Local View</span>`;
        modeIndicator.style.display = 'block';
      }
    }

    // Update stat values
    this.updateStat('hit-count', m.hit_count || 0);
    this.updateStat('miss-count', m.miss_count || 0);
    this.updateStat('bypass-count', m.bypass_count || 0);
    this.updateStat('eviction-count', m.eviction_count || 0);
    this.updateStat('active-entries', m.active_entries || 0);
    this.updateStat('tokens-saved', this.formatNumber(m.total_tokens_saved || 0));

    // Update hit rate
    const hitRate = ((m.hit_rate || 0) * 100).toFixed(1);
    this.updateStat('hit-rate', hitRate + '%');

    // Update cache size
    const sizeBytes = m.cache_size_bytes || 0;
    const maxBytes = m.max_size_bytes || 1;
    const usedMB = (sizeBytes / (1024 * 1024)).toFixed(2);
    const maxMB = (maxBytes / (1024 * 1024)).toFixed(0);
    const usagePercent = maxBytes > 0 ? ((sizeBytes / maxBytes) * 100).toFixed(1) : '0';

    this.updateStat('cache-size', `${usedMB} MB / ${maxMB} MB`);
    this.updateStat('cache-usage', usagePercent + '%');

    // Update progress bar
    const progressBar = this.shadowRoot.querySelector('#cache-progress');
    if (progressBar) {
      progressBar.style.width = usagePercent + '%';
      progressBar.style.background = usagePercent > 90 ? '#f44336' : usagePercent > 70 ? '#ff9800' : '#4caf50';
    }

    // In aggregated mode, update the storage section to clarify it's a sum across edges
    const storageSubtitle = this.shadowRoot.querySelector('#storage-subtitle');
    const storageInfo = this.shadowRoot.querySelector('#storage-info');
    if (isAggregated) {
      const edgeCount = m.edge_count || 0;
      if (storageSubtitle) {
        storageSubtitle.textContent = `Total across ${edgeCount} edge instance(s)`;
      }
      if (storageInfo) {
        storageInfo.textContent = `Each edge instance has its own independent cache. See the Edge Instances table below for per-edge storage details.`;
      }
    } else {
      if (storageSubtitle) {
        storageSubtitle.textContent = 'Current Usage';
      }
      if (storageInfo) {
        storageInfo.textContent = 'When the cache reaches maximum size, least recently used entries are evicted to make room for new ones.';
      }
    }

    // Update edge instances table (aggregated mode only)
    this.updateEdgeTable(m);
  }

  updateEdgeTable(metrics) {
    const edgeSection = this.shadowRoot.querySelector('#edge-instances-section');
    if (!edgeSection) return;

    if (metrics.mode !== 'aggregated' || !metrics.edges || metrics.edges.length === 0) {
      edgeSection.style.display = 'none';
      return;
    }

    edgeSection.style.display = 'block';

    const tbody = this.shadowRoot.querySelector('#edge-table-body');
    if (!tbody) return;

    tbody.innerHTML = metrics.edges.map(edge => {
      const lastUpdate = edge.last_update ? new Date(edge.last_update * 1000).toLocaleString() : 'Never';
      const hitRate = ((edge.hit_rate || 0) * 100).toFixed(1);
      const cacheSizeBytes = edge.cache_size_bytes || 0;
      const maxSizeBytes = edge.max_size_bytes || 0;
      const cacheSize = (cacheSizeBytes / (1024 * 1024)).toFixed(2);
      const maxSize = maxSizeBytes > 0 ? (maxSizeBytes / (1024 * 1024)).toFixed(0) : '?';
      const usagePercent = maxSizeBytes > 0 ? ((cacheSizeBytes / maxSizeBytes) * 100).toFixed(0) : '0';

      return `
        <tr>
          <td><code>${edge.edge_id || 'Unknown'}</code></td>
          <td>${edge.namespace || '-'}</td>
          <td class="stat-success">${edge.hit_count || 0}</td>
          <td>${edge.miss_count || 0}</td>
          <td>${hitRate}%</td>
          <td>${edge.active_entries || 0}</td>
          <td>${cacheSize} / ${maxSize} MB (${usagePercent}%)</td>
          <td>${this.formatNumber(edge.tokens_saved || 0)}</td>
          <td class="last-update">${lastUpdate}</td>
        </tr>
      `;
    }).join('');
  }

  updateConfigDisplay() {
    const c = this.data.config;
    if (!c) return;

    const configInfo = this.shadowRoot.querySelector('#config-info');
    if (configInfo) {
      configInfo.innerHTML = `
        <div class="config-item">
          <span class="config-label">Status</span>
          <span class="config-value ${c.enabled ? 'status-enabled' : 'status-disabled'}">
            ${c.enabled ? 'Enabled' : 'Disabled'}
          </span>
        </div>
        <div class="config-item">
          <span class="config-label">TTL</span>
          <span class="config-value">${this.formatDuration(c.ttl_seconds)}</span>
        </div>
        <div class="config-item">
          <span class="config-label">Max Entry Size</span>
          <span class="config-value">${c.max_entry_size_kb} KB</span>
        </div>
        <div class="config-item">
          <span class="config-label">Max Cache Size</span>
          <span class="config-value">${c.max_cache_size_mb} MB</span>
        </div>
        <div class="config-item">
          <span class="config-label">Namespaces</span>
          <span class="config-value">${(c.namespaces || []).join(', ') || 'None'}</span>
        </div>
        <div class="config-item">
          <span class="config-label">Normalize Prompts</span>
          <span class="config-value">${c.normalize_prompts ? 'Yes' : 'No'}</span>
        </div>
        <div class="config-item">
          <span class="config-label">Expose Cache Key</span>
          <span class="config-value">${c.expose_cache_key_header ? 'Yes' : 'No'}</span>
        </div>
      `;
    }
  }

  updateStat(id, value) {
    const el = this.shadowRoot.querySelector(`#${id}`);
    if (el) el.textContent = value;
  }

  formatNumber(num) {
    if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
    if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
    return num.toString();
  }

  formatDuration(seconds) {
    if (seconds >= 3600) return (seconds / 3600).toFixed(1) + ' hours';
    if (seconds >= 60) return (seconds / 60).toFixed(0) + ' minutes';
    return seconds + ' seconds';
  }

  setupEventListeners() {
    this.shadowRoot.querySelector('#refresh-btn')?.addEventListener('click', () => {
      this.loadData();
    });

    this.shadowRoot.querySelector('#clear-cache-btn')?.addEventListener('click', () => {
      this.clearCache();
    });
  }

  showError(message) {
    this.showMessage(message, 'error');
  }

  showSuccess(message) {
    this.showMessage(message, 'success');
  }

  showMessage(message, type) {
    const messageDiv = this.shadowRoot.querySelector('#message');
    if (!messageDiv) return;

    messageDiv.style.display = 'block';
    messageDiv.textContent = message;

    if (type === 'success') {
      messageDiv.style.background = '#e8f5e9';
      messageDiv.style.color = '#2e7d32';
      messageDiv.style.borderLeft = '4px solid #4caf50';
    } else {
      messageDiv.style.background = '#ffebee';
      messageDiv.style.color = '#c62828';
      messageDiv.style.borderLeft = '4px solid #f44336';
    }

    setTimeout(() => {
      messageDiv.style.display = 'none';
    }, 5000);
  }

  render() {
    this.shadowRoot.innerHTML = `
      <style>
        :host {
          display: block;
          padding: 24px;
          font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        }

        .header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          margin-bottom: 24px;
        }

        h2 {
          margin: 0;
          color: #333;
        }

        .header-actions {
          display: flex;
          gap: 12px;
        }

        .stats-grid {
          display: grid;
          grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
          gap: 16px;
          margin-bottom: 32px;
        }

        .stat-card {
          background: white;
          border: 1px solid #e0e0e0;
          border-radius: 8px;
          padding: 20px;
          text-align: center;
        }

        .stat-card.highlight {
          background: linear-gradient(135deg, #e3f2fd 0%, #bbdefb 100%);
          border-color: #90caf9;
        }

        .stat-card.success {
          background: linear-gradient(135deg, #e8f5e9 0%, #c8e6c9 100%);
          border-color: #a5d6a7;
        }

        .stat-card.warning {
          background: linear-gradient(135deg, #fff3e0 0%, #ffe0b2 100%);
          border-color: #ffcc80;
        }

        .stat-value {
          font-size: 28px;
          font-weight: bold;
          color: #1976d2;
          margin-bottom: 8px;
        }

        .stat-card.success .stat-value {
          color: #388e3c;
        }

        .stat-card.warning .stat-value {
          color: #f57c00;
        }

        .stat-label {
          font-size: 14px;
          color: #666;
        }

        .section {
          background: white;
          border: 1px solid #e0e0e0;
          border-radius: 8px;
          margin-bottom: 24px;
        }

        .section-header {
          padding: 16px 20px;
          border-bottom: 1px solid #e0e0e0;
          font-weight: bold;
          font-size: 16px;
          color: #333;
        }

        .section-content {
          padding: 20px;
        }

        .cache-usage-bar {
          background: #e0e0e0;
          border-radius: 8px;
          height: 24px;
          overflow: hidden;
          margin: 16px 0;
        }

        .cache-usage-fill {
          height: 100%;
          transition: width 0.3s ease, background 0.3s ease;
        }

        .usage-labels {
          display: flex;
          justify-content: space-between;
          font-size: 13px;
          color: #666;
        }

        .config-item {
          display: flex;
          justify-content: space-between;
          padding: 12px 0;
          border-bottom: 1px solid #f0f0f0;
        }

        .config-item:last-child {
          border-bottom: none;
        }

        .config-label {
          color: #666;
          font-size: 14px;
        }

        .config-value {
          font-weight: 500;
          font-size: 14px;
        }

        .status-enabled {
          color: #388e3c;
        }

        .status-disabled {
          color: #d32f2f;
        }

        button {
          padding: 10px 20px;
          border: none;
          border-radius: 4px;
          cursor: pointer;
          font-size: 14px;
          font-weight: 500;
          transition: all 0.2s;
        }

        button:hover {
          transform: translateY(-1px);
          box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }

        button:disabled {
          opacity: 0.5;
          cursor: not-allowed;
          transform: none;
        }

        .btn-primary {
          background: #1976d2;
          color: white;
        }

        .btn-primary:hover:not(:disabled) {
          background: #1565c0;
        }

        .btn-danger {
          background: #f44336;
          color: white;
        }

        .btn-danger:hover:not(:disabled) {
          background: #d32f2f;
        }

        #loading-spinner {
          text-align: center;
          padding: 60px;
          color: #666;
          font-size: 16px;
        }

        #message {
          padding: 14px;
          border-radius: 4px;
          margin-bottom: 20px;
          display: none;
        }

        .two-columns {
          display: grid;
          grid-template-columns: 1fr 1fr;
          gap: 24px;
        }

        @media (max-width: 768px) {
          .two-columns {
            grid-template-columns: 1fr;
          }
        }

        .info-text {
          font-size: 13px;
          color: #666;
          margin-top: 12px;
          line-height: 1.5;
        }

        .cache-icon {
          width: 48px;
          height: 48px;
          margin: 0 auto 16px;
          opacity: 0.8;
        }

        .mode-badge {
          display: inline-block;
          padding: 4px 12px;
          border-radius: 16px;
          font-size: 12px;
          font-weight: 600;
          text-transform: uppercase;
        }

        .mode-badge.aggregated {
          background: linear-gradient(135deg, #e3f2fd 0%, #bbdefb 100%);
          color: #1565c0;
        }

        .mode-badge.local {
          background: linear-gradient(135deg, #e8f5e9 0%, #c8e6c9 100%);
          color: #2e7d32;
        }

        .edge-count {
          font-size: 14px;
          color: #666;
          margin-left: 8px;
        }

        #mode-indicator {
          margin-bottom: 20px;
          display: none;
        }

        .edge-table {
          width: 100%;
          border-collapse: collapse;
          font-size: 13px;
        }

        .edge-table th,
        .edge-table td {
          padding: 12px 8px;
          text-align: left;
          border-bottom: 1px solid #e0e0e0;
        }

        .edge-table th {
          background: #f5f5f5;
          font-weight: 600;
          color: #333;
        }

        .edge-table tr:hover {
          background: #fafafa;
        }

        .edge-table code {
          background: #e3f2fd;
          padding: 2px 6px;
          border-radius: 4px;
          font-size: 12px;
        }

        .edge-table .stat-success {
          color: #388e3c;
          font-weight: 600;
        }

        .edge-table .last-update {
          color: #666;
          font-size: 12px;
        }

        #edge-instances-section {
          display: none;
        }
      </style>

      <div id="loading-spinner">Loading Cache Dashboard...</div>
      <div id="message"></div>

      <div id="content" style="display: none;">
        <div class="header">
          <h2>LLM Response Cache</h2>
          <div class="header-actions">
            <button id="refresh-btn" class="btn-primary">Refresh</button>
            <button id="clear-cache-btn" class="btn-danger">Clear Cache</button>
          </div>
        </div>

        <div id="mode-indicator"></div>

        <div class="stats-grid">
          <div class="stat-card highlight">
            <div class="stat-value" id="hit-rate">0%</div>
            <div class="stat-label">Hit Rate</div>
          </div>
          <div class="stat-card success">
            <div class="stat-value" id="hit-count">0</div>
            <div class="stat-label">Cache Hits</div>
          </div>
          <div class="stat-card">
            <div class="stat-value" id="miss-count">0</div>
            <div class="stat-label">Cache Misses</div>
          </div>
          <div class="stat-card">
            <div class="stat-value" id="bypass-count">0</div>
            <div class="stat-label">Bypasses</div>
          </div>
          <div class="stat-card success">
            <div class="stat-value" id="tokens-saved">0</div>
            <div class="stat-label">Tokens Saved</div>
          </div>
          <div class="stat-card">
            <div class="stat-value" id="active-entries">0</div>
            <div class="stat-label">Active Entries</div>
          </div>
        </div>

        <div class="two-columns">
          <div class="section" id="storage-section">
            <div class="section-header">Cache Storage</div>
            <div class="section-content">
              <div style="text-align: center; margin-bottom: 16px;">
                <div style="font-size: 24px; font-weight: bold; color: #333;" id="cache-size">0 MB / 256 MB</div>
                <div style="font-size: 14px; color: #666;" id="storage-subtitle">Current Usage</div>
              </div>

              <div class="cache-usage-bar">
                <div class="cache-usage-fill" id="cache-progress" style="width: 0%; background: #4caf50;"></div>
              </div>

              <div class="usage-labels">
                <span>0%</span>
                <span id="cache-usage">0%</span>
                <span>100%</span>
              </div>

              <div style="display: flex; justify-content: space-between; margin-top: 20px; padding-top: 16px; border-top: 1px solid #f0f0f0;">
                <div style="text-align: center;">
                  <div style="font-size: 20px; font-weight: bold; color: #f57c00;" id="eviction-count">0</div>
                  <div style="font-size: 13px; color: #666;">Evictions</div>
                </div>
              </div>

              <div class="info-text" id="storage-info">
                When the cache reaches maximum size, least recently used entries are evicted to make room for new ones.
              </div>
            </div>
          </div>

          <div class="section">
            <div class="section-header">Configuration</div>
            <div class="section-content" id="config-info">
              <div class="config-item">
                <span class="config-label">Loading...</span>
              </div>
            </div>
          </div>
        </div>

        <div id="edge-instances-section" class="section">
          <div class="section-header">Edge Instances</div>
          <div class="section-content">
            <table class="edge-table">
              <thead>
                <tr>
                  <th>Edge ID</th>
                  <th>Namespace</th>
                  <th>Hits</th>
                  <th>Misses</th>
                  <th>Hit Rate</th>
                  <th>Entries</th>
                  <th>Cache Size</th>
                  <th>Tokens Saved</th>
                  <th>Last Update</th>
                </tr>
              </thead>
              <tbody id="edge-table-body">
              </tbody>
            </table>
            <div class="info-text">
              Statistics from connected edge instances are aggregated above. Each edge maintains its own local cache.
            </div>
          </div>
        </div>

        <div class="section">
          <div class="section-header">How It Works</div>
          <div class="section-content">
            <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 24px;">
              <div style="text-align: center; padding: 16px;">
                <div style="font-size: 32px; margin-bottom: 8px;">🔑</div>
                <div style="font-weight: bold; margin-bottom: 8px;">Cache Key Generation</div>
                <div style="font-size: 13px; color: #666;">Requests are hashed using model, messages, tools, and temperature to create unique cache keys.</div>
              </div>
              <div style="text-align: center; padding: 16px;">
                <div style="font-size: 32px; margin-bottom: 8px;">⚡</div>
                <div style="font-weight: bold; margin-bottom: 8px;">Instant Response</div>
                <div style="font-size: 13px; color: #666;">Cache hits return immediately without calling the LLM, saving time and tokens.</div>
              </div>
              <div style="text-align: center; padding: 16px;">
                <div style="font-size: 32px; margin-bottom: 8px;">🔒</div>
                <div style="font-weight: bold; margin-bottom: 8px;">Namespace Isolation</div>
                <div style="font-size: 13px; color: #666;">Cache entries are isolated by API key, app, or organization to prevent data leakage.</div>
              </div>
              <div style="text-align: center; padding: 16px;">
                <div style="font-size: 32px; margin-bottom: 8px;">🔄</div>
                <div style="font-weight: bold; margin-bottom: 8px;">LRU Eviction</div>
                <div style="font-size: 13px; color: #666;">Least recently used entries are automatically evicted when the cache is full.</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    `;
  }
}

// Register the custom element
customElements.define('llm-cache-dashboard', LLMCacheDashboard);
