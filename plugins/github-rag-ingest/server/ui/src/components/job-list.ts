import { LitElement, html, css } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

interface Job {
  id: string;
  repo_id: string;
  type: string;
  status: string;
  started_at: string;
  completed_at?: string;
  stats: {
    files_scanned: number;
    chunks_written: number;
    chunks_deleted: number;
  };
}

@customElement('github-rag-job-list')
export class GitHubRAGJobList extends LitElement {
  @property({ type: String }) rpcBase = '';
  @state() private jobs: Job[] = [];
  @state() private loading = false;
  @state() private error = '';

  static styles = css`
    :host {
      display: block;
      padding: 24px;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    }

    h2 {
      margin: 0 0 24px 0;
      font-size: 24px;
      font-weight: 600;
    }

    .table-container {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      overflow: hidden;
    }

    table {
      width: 100%;
      border-collapse: collapse;
    }

    th {
      background: #f5f5f5;
      padding: 12px 16px;
      text-align: left;
      font-weight: 600;
      font-size: 13px;
      border-bottom: 1px solid #e0e0e0;
    }

    td {
      padding: 12px 16px;
      border-bottom: 1px solid #f5f5f5;
    }

    .status-badge {
      display: inline-block;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
    }

    .status-success { background: #e8f5e9; color: #2e7d32; }
    .status-running { background: #e3f2fd; color: #1976d2; }
    .status-failed { background: #ffebee; color: #c62828; }
    .status-queued { background: #fff3e0; color: #f57c00; }

    .empty-state {
      text-align: center;
      padding: 64px 24px;
      color: #666;
    }

    .btn {
      padding: 4px 12px;
      border-radius: 4px;
      border: 1px solid #ddd;
      background: white;
      cursor: pointer;
      font-size: 12px;
    }

    .btn:hover {
      background: #f5f5f5;
    }

    .btn-sm {
      padding: 4px 12px;
      font-size: 12px;
    }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.loadJobs();
  }

  async loadJobs() {
    this.loading = true;
    try {
      await this.waitForPluginAPI();
      const result = await this.pluginAPI!.call('list_jobs', { limit: 50, offset: 0 });
      if (result.success) {
        this.jobs = result.data.jobs || [];
      }
    } catch (err) {
      this.error = String(err);
    } finally {
      this.loading = false;
    }
  }

  async waitForPluginAPI() {
    for (let i = 0; i < 50; i++) {
      if (this.pluginAPI) return;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error('Plugin API timeout');
  }

  private getStatusClass(status: string) {
    return `status-badge status-${status}`;
  }

  private formatDate(dateStr: string) {
    return new Date(dateStr).toLocaleString();
  }

  private handleViewJob(jobId: string) {
    console.log('View job clicked:', jobId);
    this.selectedJobId = jobId;
    this.requestUpdate(); // Force re-render
  }

  private handleBackToList() {
    console.log('Back to list clicked');
    this.selectedJobId = null;
    this.loadJobs();
  }

  render() {
    if (this.selectedJobId) {
      return html`
        <github-rag-job-detail
          .jobId=${this.selectedJobId}
          .api=${this.pluginAPI}
          .onBack=${this.handleBackToList.bind(this)}
        ></github-rag-job-detail>
      `;
    }

    return html`
      <h2>Ingestion Jobs</h2>

      ${this.loading ? html`<div>Loading...</div>` :
        this.jobs.length === 0 ? html`
          <div class="empty-state">
            <p>No ingestion jobs yet</p>
          </div>
        ` : html`
          <div class="table-container">
            <table>
              <thead>
                <tr>
                  <th>Job ID</th>
                  <th>Type</th>
                  <th>Status</th>
                  <th>Files</th>
                  <th>Chunks</th>
                  <th>Started</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${this.jobs.map(job => html`
                  <tr>
                    <td><code>${job.id.substring(0, 8)}</code></td>
                    <td>${job.type}</td>
                    <td><span class="${this.getStatusClass(job.status)}">${job.status}</span></td>
                    <td>${job.stats.files_scanned}</td>
                    <td>${job.stats.chunks_written}</td>
                    <td>${this.formatDate(job.started_at)}</td>
                    <td>
                      <button class="btn btn-sm" @click=${() => this.handleViewJob(job.id)}>
                        View Logs
                      </button>
                    </td>
                  </tr>
                `)}
              </tbody>
            </table>
          </div>
        `}
    `;
  }
}
