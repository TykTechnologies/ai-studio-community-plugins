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
    files_added: number;
    files_changed: number;
    files_deleted: number;
    files_skipped: number;
    chunks_written: number;
    chunks_deleted: number;
    errors: number;
  };
}

interface JobLog {
  timestamp: string;
  level: string;
  message: string;
  details: string;
}

interface PluginAPI {
  call(method: string, payload: any): Promise<any>;
}

@customElement('github-rag-job-detail')
export class GitHubRAGJobDetail extends LitElement {
  @property({ type: String }) jobId = '';
  @property({ type: Object }) api: PluginAPI | null = null;
  @property({ type: Function }) onBack: (() => void) | null = null;

  @state() private job: Job | null = null;
  @state() private logs: JobLog[] = [];
  @state() private loading = false;
  @state() private error = '';
  @state() private selectedLevel = '';

  static styles = css`
    :host {
      display: block;
      padding: 24px;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    }

    .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 24px;
    }

    h2 {
      margin: 0;
      font-size: 24px;
      font-weight: 600;
    }

    .back-btn {
      background: #f5f5f5;
      color: #333;
      padding: 8px 16px;
      border: none;
      border-radius: 4px;
      cursor: pointer;
    }

    .job-info {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      padding: 16px;
      margin-bottom: 16px;
    }

    .stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
      gap: 12px;
      margin-top: 12px;
    }

    .stat {
      background: #f9f9f9;
      padding: 12px;
      border-radius: 4px;
    }

    .stat-label {
      font-size: 12px;
      color: #666;
      margin-bottom: 4px;
    }

    .stat-value {
      font-size: 20px;
      font-weight: 600;
      color: #1976d2;
    }

    .logs-container {
      background: white;
      border: 1px solid #e0e0e0;
      border-radius: 8px;
      overflow: hidden;
    }

    .logs-header {
      padding: 12px 16px;
      background: #f5f5f5;
      border-bottom: 1px solid #e0e0e0;
      display: flex;
      justify-content: space-between;
      align-items: center;
    }

    .filter {
      display: flex;
      gap: 8px;
      align-items: center;
    }

    select {
      padding: 6px 12px;
      border: 1px solid #ddd;
      border-radius: 4px;
    }

    .logs {
      max-height: 600px;
      overflow-y: auto;
    }

    .log-entry {
      padding: 12px 16px;
      border-bottom: 1px solid #f5f5f5;
      font-family: 'Monaco', 'Consolas', monospace;
      font-size: 13px;
    }

    .log-entry:hover {
      background: #f9f9f9;
    }

    .log-info { border-left: 3px solid #2196f3; }
    .log-warn { border-left: 3px solid #ff9800; background: #fff3e0; }
    .log-error { border-left: 3px solid #f44336; background: #ffebee; }
    .log-debug { border-left: 3px solid #9e9e9e; }

    .log-header {
      display: flex;
      gap: 12px;
      margin-bottom: 4px;
      color: #666;
    }

    .log-timestamp {
      font-size: 11px;
    }

    .log-level {
      font-weight: 600;
      text-transform: uppercase;
      font-size: 11px;
    }

    .log-message {
      color: #333;
      margin-bottom: 4px;
    }

    .log-details {
      color: #666;
      font-size: 12px;
    }

    .empty {
      padding: 48px;
      text-align: center;
      color: #666;
    }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.loadJobDetails();
  }

  async loadJobDetails() {
    if (!this.api || !this.jobId) return;

    this.loading = true;
    try {
      // Load job
      const jobResult = await this.api.call('get_job', { id: this.jobId });
      if (jobResult.success) {
        this.job = jobResult.data;
      }

      // Load logs
      const logsResult = await this.api.call('get_job_logs', {
        job_id: this.jobId,
        level: this.selectedLevel,
        limit: 1000,
        offset: 0,
      });
      if (logsResult.success) {
        this.logs = logsResult.data.logs || [];
      }
    } catch (err: any) {
      this.error = `Error: ${err.message}`;
    } finally {
      this.loading = false;
    }
  }

  private handleLevelFilter(e: Event) {
    const select = e.target as HTMLSelectElement;
    this.selectedLevel = select.value;
    this.loadJobDetails();
  }

  private formatDate(dateStr: string) {
    return new Date(dateStr).toLocaleString();
  }

  render() {
    if (!this.job) {
      return html`<div class="empty">Loading job details...</div>`;
    }

    return html`
      <div class="header">
        <h2>Job ${this.job.id.substring(0, 8)}</h2>
        <button class="back-btn" @click=${() => this.onBack && this.onBack()}>
          ← Back to Jobs
        </button>
      </div>

      <div class="job-info">
        <strong>Repository:</strong> ${this.job.repo_id}<br>
        <strong>Type:</strong> ${this.job.type}<br>
        <strong>Status:</strong> ${this.job.status}<br>
        <strong>Started:</strong> ${this.formatDate(this.job.started_at)}<br>
        ${this.job.completed_at ? html`<strong>Completed:</strong> ${this.formatDate(this.job.completed_at)}<br>` : ''}

        <div class="stats-grid">
          <div class="stat">
            <div class="stat-label">Files Scanned</div>
            <div class="stat-value">${this.job.stats.files_scanned}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Files Added</div>
            <div class="stat-value">${this.job.stats.files_added}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Files Skipped</div>
            <div class="stat-value">${this.job.stats.files_skipped}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Chunks Written</div>
            <div class="stat-value">${this.job.stats.chunks_written}</div>
          </div>
          <div class="stat">
            <div class="stat-label">Errors</div>
            <div class="stat-value">${this.job.stats.errors}</div>
          </div>
        </div>
      </div>

      <div class="logs-container">
        <div class="logs-header">
          <strong>Execution Logs</strong>
          <div class="filter">
            <label>Level:</label>
            <select @change=${this.handleLevelFilter}>
              <option value="">All</option>
              <option value="info">Info</option>
              <option value="warn">Warn</option>
              <option value="error">Error</option>
              <option value="debug">Debug</option>
            </select>
          </div>
        </div>

        <div class="logs">
          ${this.logs.length === 0 ? html`
            <div class="empty">No logs available</div>
          ` : html`
            ${this.logs.map(log => html`
              <div class="log-entry log-${log.level}">
                <div class="log-header">
                  <span class="log-timestamp">${this.formatDate(log.timestamp)}</span>
                  <span class="log-level">${log.level}</span>
                </div>
                <div class="log-message">${log.message}</div>
                ${log.details ? html`<div class="log-details">${log.details}</div>` : ''}
              </div>
            `)}
          `}
        </div>
      </div>
    `;
  }
}
