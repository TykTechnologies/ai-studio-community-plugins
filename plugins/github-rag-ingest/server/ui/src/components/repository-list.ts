import { LitElement, html, css } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import './repository-form';

interface Repository {
  id: string;
  name: string;
  owner: string;
  url: string;
  branch: string;
  datasource_id: number;
  sync_enabled: boolean;
  sync_schedule: string;
  last_sync_at: string;
  last_sync_commit: string;
}

@customElement('github-rag-repository-list')
export class GitHubRAGRepositoryList extends LitElement {
  @property({ type: String }) rpcBase = '';
  @state() private repositories: Repository[] = [];
  @state() private loading = false;
  @state() private error = '';
  @state() private showForm = false;
  @state() private editingRepo: Repository | null = null;

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

    .btn {
      padding: 10px 20px;
      border-radius: 4px;
      border: none;
      cursor: pointer;
      font-size: 14px;
      font-weight: 500;
    }

    .btn-primary {
      background: #1976d2;
      color: white;
    }

    .btn-primary:hover {
      background: #1565c0;
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
      color: #333;
      border-bottom: 1px solid #e0e0e0;
    }

    td {
      padding: 12px 16px;
      border-bottom: 1px solid #f5f5f5;
    }

    tr:hover {
      background: #f9f9f9;
    }

    .status-badge {
      display: inline-block;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 12px;
      font-weight: 500;
    }

    .status-active {
      background: #e8f5e9;
      color: #2e7d32;
    }

    .status-inactive {
      background: #fafafa;
      color: #666;
    }

    .actions {
      display: flex;
      gap: 8px;
    }

    .btn-sm {
      padding: 4px 12px;
      font-size: 12px;
    }

    .empty-state {
      text-align: center;
      padding: 64px 24px;
      color: #666;
    }

    .error {
      background: #ffebee;
      border: 1px solid #ef5350;
      padding: 12px;
      border-radius: 4px;
      color: #c62828;
      margin-bottom: 16px;
    }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.loadRepositories();
  }

  async loadRepositories() {
    this.loading = true;
    this.error = '';

    try {
      await this.waitForPluginAPI();
      const result = await this.pluginAPI!.call('list_repositories', {});

      if (result.success) {
        this.repositories = result.data.repositories || [];
      } else {
        this.error = result.error || 'Failed to load repositories';
      }
    } catch (err: any) {
      this.error = `Error: ${err.message}`;
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

  private handleAddRepository() {
    this.showForm = true;
    this.editingRepo = null;
  }

  private handleEditRepository(repo: Repository) {
    this.showForm = true;
    this.editingRepo = repo;
  }

  private async handleRunIngestion(repoId: string, type: string = 'incremental') {
    try {
      const result = await this.pluginAPI!.call('trigger_ingestion', {
        repo_id: repoId,
        type: type,
        dry_run: false,
      });

      if (result.success) {
        alert(`${type === 'full' ? 'Full' : 'Incremental'} ingestion started! Job ID: ${result.data.job_id}`);
      } else {
        alert(`Error: ${result.error}`);
      }
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  }

  private async handleDeleteRepository(repo: Repository) {
    if (!confirm(`Delete repository "${repo.owner}/${repo.name}"? This will also delete all ingested chunks.`)) {
      return;
    }

    try {
      const result = await this.pluginAPI!.call('delete_repository', {
        id: repo.id,
        delete_chunks: true,
      });

      if (result.success) {
        this.loadRepositories();
      } else {
        alert(`Error: ${result.error}`);
      }
    } catch (err: any) {
      alert(`Error: ${err.message}`);
    }
  }

  private handleFormSave(repo: any) {
    this.showForm = false;
    this.editingRepo = null;
    this.loadRepositories();
  }

  private handleFormCancel() {
    this.showForm = false;
    this.editingRepo = null;
  }

  private formatDate(dateStr: string) {
    if (!dateStr) return 'Never';
    return new Date(dateStr).toLocaleString();
  }

  render() {
    if (this.showForm) {
      return html`
        <github-rag-repository-form
          .repository=${this.editingRepo}
          .api=${this.pluginAPI}
          .onSave=${this.handleFormSave.bind(this)}
          .onCancel=${this.handleFormCancel.bind(this)}
        ></github-rag-repository-form>
      `;
    }

    return html`
      <div class="header">
        <h2>Repositories</h2>
        <button class="btn btn-primary" @click=${this.handleAddRepository}>
          Add Repository
        </button>
      </div>

      ${this.error ? html`<div class="error">${this.error}</div>` : ''}

      ${this.loading ? html`<div>Loading...</div>` :
        this.repositories.length === 0 ? html`
          <div class="empty-state">
            <p>No repositories configured</p>
            <p>Click "Add Repository" to get started</p>
          </div>
        ` : html`
          <div class="table-container">
            <table>
              <thead>
                <tr>
                  <th>Repository</th>
                  <th>Branch</th>
                  <th>Sync Status</th>
                  <th>Last Sync</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${this.repositories.map(repo => html`
                  <tr>
                    <td>
                      <strong>${repo.owner}/${repo.name}</strong><br>
                      <small style="color: #666;">${repo.url}</small>
                    </td>
                    <td>${repo.branch}</td>
                    <td>
                      <span class="status-badge ${repo.sync_enabled ? 'status-active' : 'status-inactive'}">
                        ${repo.sync_enabled ? 'Active' : 'Inactive'}
                      </span>
                    </td>
                    <td>${this.formatDate(repo.last_sync_at)}</td>
                    <td>
                      <div class="actions">
                        <button class="btn btn-sm btn-primary" @click=${() => this.handleRunIngestion(repo.id, 'incremental')} title="Sync only changed files">
                          Sync
                        </button>
                        <button class="btn btn-sm btn-primary" @click=${() => this.handleRunIngestion(repo.id, 'full')} title="Re-process all files">
                          Full Sync
                        </button>
                        <button class="btn btn-sm" @click=${() => this.handleEditRepository(repo)}>
                          Edit
                        </button>
                        <button class="btn btn-sm" @click=${() => this.handleDeleteRepository(repo)}>
                          Delete
                        </button>
                      </div>
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
