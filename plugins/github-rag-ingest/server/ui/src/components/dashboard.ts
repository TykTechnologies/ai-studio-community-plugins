import { LitElement, html, css } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

interface PluginAPI {
  call(method: string, payload: any): Promise<any>;
}

declare global {
  interface HTMLElement {
    pluginAPI?: PluginAPI;
  }
}

@customElement('github-rag-dashboard')
export class GitHubRAGDashboard extends LitElement {
  @property({ type: String }) rpcBase = '';
  @state() private stats: any = null;
  @state() private loading = false;

  static styles = css`
    :host {
      display: block;
      padding: 24px;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    }

    .header {
      margin-bottom: 24px;
    }

    h2 {
      margin: 0 0 8px 0;
      font-size: 24px;
      font-weight: 600;
    }

    .subtitle {
      color: #666;
      font-size: 14px;
    }

    .stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }

    .stat-card {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      padding: 16px;
    }

    .stat-label {
      font-size: 12px;
      color: #666;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-bottom: 8px;
    }

    .stat-value {
      font-size: 32px;
      font-weight: 600;
      color: #1976d2;
    }

    .info-card {
      background: #e3f2fd;
      border: 1px solid #90caf9;
      border-radius: 8px;
      padding: 16px;
      margin-bottom: 16px;
    }

    .info-card h3 {
      margin: 0 0 12px 0;
      font-size: 16px;
      color: #0d47a1;
    }

    .info-card ul {
      margin: 0;
      padding-left: 20px;
    }

    .info-card li {
      margin-bottom: 8px;
      color: #1565c0;
    }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.loadStats();
  }

  async loadStats() {
    try {
      await this.waitForPluginAPI();
      const result = await this.pluginAPI!.call('get_statistics', {});

      if (result.success) {
        this.stats = {
          totalRepos: result.data.total_repos || 0,
          activeRepos: result.data.active_repos || 0,
          totalJobs: result.data.total_jobs || 0,
          chunksIngested: result.data.chunks_ingested || 0,
        };
      }
    } catch (err) {
      console.error('Failed to load stats:', err);
      // Show zeros on error
      this.stats = {
        totalRepos: 0,
        activeRepos: 0,
        totalJobs: 0,
        chunksIngested: 0,
      };
    }
  }

  async waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) return;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error('Plugin API timeout');
  }

  render() {
    return html`
      <div class="header">
        <h2>GitHub RAG Ingestion</h2>
        <p class="subtitle">Manage GitHub repositories and RAG datasource ingestion</p>
      </div>

      <div class="info-card">
        <h3>Getting Started</h3>
        <ul>
          <li>Navigate to <strong>Repositories</strong> to add GitHub repositories</li>
          <li>Configure chunking strategy and datasource assignment</li>
          <li>Run manual ingestion or set up scheduled syncs</li>
          <li>View ingestion jobs and logs in the <strong>Jobs</strong> tab</li>
        </ul>
      </div>

      <div class="stats-grid">
        <div class="stat-card">
          <div class="stat-label">Repositories</div>
          <div class="stat-value">${this.stats?.totalRepos || 0}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Active Syncs</div>
          <div class="stat-value">${this.stats?.activeRepos || 0}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Total Jobs</div>
          <div class="stat-value">${this.stats?.totalJobs || 0}</div>
        </div>
        <div class="stat-card">
          <div class="stat-label">Chunks Ingested</div>
          <div class="stat-value">${this.stats?.chunksIngested || 0}</div>
        </div>
      </div>
    `;
  }
}
