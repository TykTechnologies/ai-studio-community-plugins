import { LitElement, html, css } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

interface Datasource {
  id: number;
  name: string;
  short_description: string;
  db_name: string;
}

interface PluginAPI {
  call(method: string, payload: any): Promise<any>;
}

@customElement('github-rag-repository-form')
export class GitHubRAGRepositoryForm extends LitElement {
  @property({ type: Object }) repository: any = null;
  @property({ type: Object }) api: PluginAPI | null = null;
  @property({ type: Function }) onSave: ((repo: any) => void) | null = null;
  @property({ type: Function }) onCancel: (() => void) | null = null;

  @state() private datasources: Datasource[] = [];
  @state() private showCloneModal = false;
  @state() private selectedDatasourceForClone: Datasource | null = null;
  @state() private formData: any = {
    name: '',
    owner: '',
    url: '',
    branch: 'main',
    auth_type: 'public',
    pat_token: '',
    ssh_private_key: '',
    ssh_passphrase: '',
    datasource_id: 0,
    target_paths: [],
    file_masks: ['*'],
    ignore_patterns: [],
    chunking_strategy: 'hybrid',
    chunk_size: 1000,
    chunk_overlap: 200,
    sync_schedule: '',
    sync_enabled: false,
  };
  @state() private loading = false;
  @state() private error = '';

  static styles = css`
    :host {
      display: block;
    }

    .form-container {
      background: white;
      border-radius: 8px;
      padding: 24px;
      max-width: 800px;
    }

    h3 {
      margin: 0 0 24px 0;
      font-size: 20px;
      font-weight: 600;
    }

    .form-grid {
      display: grid;
      gap: 16px;
    }

    .form-group {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }

    label {
      font-size: 14px;
      font-weight: 500;
      color: #333;
    }

    input, select, textarea {
      padding: 10px;
      border: 1px solid #ddd;
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
    }

    input:focus, select:focus, textarea:focus {
      outline: none;
      border-color: #1976d2;
    }

    .form-row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 16px;
    }

    .actions {
      display: flex;
      gap: 12px;
      margin-top: 24px;
      padding-top: 24px;
      border-top: 1px solid #e0e0e0;
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

    .btn-secondary {
      background: #f5f5f5;
      color: #333;
    }

    .btn-secondary:hover {
      background: #e0e0e0;
    }

    .error {
      background: #ffebee;
      border: 1px solid #ef5350;
      padding: 12px;
      border-radius: 4px;
      color: #c62828;
      margin-bottom: 16px;
    }

    .hint {
      font-size: 12px;
      color: #666;
      margin-top: 4px;
    }

    .btn-clone {
      padding: 8px 16px;
      background: #f5f5f5;
      color: #333;
      border: 1px solid #ddd;
      border-radius: 4px;
      cursor: pointer;
      font-size: 14px;
      white-space: nowrap;
      transition: background 0.2s;
    }

    .btn-clone:hover {
      background: #e0e0e0;
    }

    .btn-clone:disabled {
      opacity: 0.5;
      cursor: not-allowed;
    }

    .checkbox-group {
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .checkbox-group input[type="checkbox"] {
      width: auto;
    }

    textarea {
      min-height: 80px;
      resize: vertical;
    }
  `;

  async connectedCallback() {
    super.connectedCallback();

    // Load datasources first, then set form data
    await this.loadDatasources();

    if (this.repository) {
      this.formData = { ...this.repository };
      // Trigger re-render to update select value
      this.requestUpdate();
    }
  }

  async loadDatasources() {
    if (!this.api) {
      console.error('No API available to load datasources');
      return;
    }

    try {
      const result = await this.api.call('list_datasources', {});
      if (result.success) {
        this.datasources = result.data.datasources || [];
        console.log('Loaded datasources:', this.datasources);
      } else {
        console.error('Failed to load datasources:', result.error);
      }
    } catch (err: any) {
      console.error('Failed to load datasources:', err);
    }
  }

  private handleCloneClick() {
    const selectedId = this.formData.datasource_id;
    if (!selectedId || selectedId === 0) {
      alert('Please select a datasource first');
      return;
    }

    const datasource = this.datasources.find(ds => ds.id === selectedId);
    if (datasource) {
      this.selectedDatasourceForClone = datasource;
      this.showCloneModal = true;
    }
  }

  private handleCloneSuccess(newDatasourceId: number) {
    this.loadDatasources().then(() => {
      this.updateField('datasource_id', newDatasourceId);
    });
  }

  private handleCloseModal() {
    this.showCloneModal = false;
    this.selectedDatasourceForClone = null;
  }

  private handleSubmit(e: Event) {
    e.preventDefault();
    this.saveRepository();
  }

  private async saveRepository() {
    if (!this.api) {
      this.error = 'No API available';
      return;
    }

    this.loading = true;
    this.error = '';

    try {
      const method = this.repository ? 'update_repository' : 'create_repository';

      // Automatically enable sync if schedule is provided
      const payload = this.repository
        ? { ...this.formData, id: this.repository.id, sync_enabled: !!this.formData.sync_schedule }
        : { ...this.formData, sync_enabled: !!this.formData.sync_schedule };

      const result = await this.api.call(method, payload);

      if (result.success) {
        if (this.onSave) {
          this.onSave(result.data);
        }
      } else {
        this.error = result.error || 'Failed to save repository';
      }
    } catch (err: any) {
      this.error = `Error: ${err.message}`;
    } finally {
      this.loading = false;
    }
  }

  private handleCancel() {
    if (this.onCancel) {
      this.onCancel();
    }
  }

  private updateField(field: string, value: any) {
    this.formData = { ...this.formData, [field]: value };
  }

  private updateArrayField(field: string, value: string) {
    const items = value.split(',').map(s => s.trim()).filter(s => s);
    this.formData = { ...this.formData, [field]: items };
  }

  render() {
    return html`
      <div class="form-container">
        <h3>${this.repository ? 'Edit Repository' : 'Add Repository'}</h3>

        ${this.error ? html`<div class="error">${this.error}</div>` : ''}

        <form @submit=${this.handleSubmit}>
          <div class="form-grid">
            <div class="form-row">
              <div class="form-group">
                <label>Repository Name *</label>
                <input
                  type="text"
                  required
                  .value=${this.formData.name}
                  @input=${(e: any) => this.updateField('name', e.target.value)}
                  placeholder="my-repo"
                />
              </div>

              <div class="form-group">
                <label>Owner *</label>
                <input
                  type="text"
                  required
                  .value=${this.formData.owner}
                  @input=${(e: any) => this.updateField('owner', e.target.value)}
                  placeholder="TykTechnologies"
                />
              </div>
            </div>

            <div class="form-group">
              <label>Repository URL *</label>
              <input
                type="url"
                required
                .value=${this.formData.url}
                @input=${(e: any) => this.updateField('url', e.target.value)}
                placeholder="https://github.com/owner/repo"
              />
            </div>

            <div class="form-row">
              <div class="form-group">
                <label>Branch *</label>
                <input
                  type="text"
                  required
                  .value=${this.formData.branch}
                  @input=${(e: any) => this.updateField('branch', e.target.value)}
                  placeholder="main"
                />
              </div>

              <div class="form-group">
                <label>Authentication</label>
                <select
                  .value=${this.formData.auth_type}
                  @change=${(e: any) => this.updateField('auth_type', e.target.value)}
                >
                  <option value="public">Public (No Auth)</option>
                  <option value="pat">Personal Access Token</option>
                  <option value="ssh">SSH Key</option>
                </select>
              </div>
            </div>

            ${this.formData.auth_type === 'pat' ? html`
              <div class="form-group">
                <label>Personal Access Token *</label>
                <input
                  type="password"
                  required
                  .value=${this.formData.pat_token}
                  @input=${(e: any) => this.updateField('pat_token', e.target.value)}
                  placeholder="ghp_..."
                />
                <div class="hint">⚠️  Token stored in ${this.formData.secrets_backend || 'KV'} storage</div>
              </div>
            ` : ''}

            ${this.formData.auth_type === 'ssh' ? html`
              <div class="form-group">
                <label>SSH Private Key *</label>
                <textarea
                  required
                  .value=${this.formData.ssh_private_key}
                  @input=${(e: any) => this.updateField('ssh_private_key', e.target.value)}
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                ></textarea>
              </div>

              <div class="form-group">
                <label>SSH Passphrase (if encrypted)</label>
                <input
                  type="password"
                  .value=${this.formData.ssh_passphrase}
                  @input=${(e: any) => this.updateField('ssh_passphrase', e.target.value)}
                />
              </div>
            ` : ''}

            <div class="form-group">
              <label>Datasource *</label>
              <div style="display: flex; gap: 8px; align-items: start;">
                <select
                  required
                  style="flex: 1;"
                  .value=${String(this.formData.datasource_id)}
                  @change=${(e: any) => this.updateField('datasource_id', parseInt(e.target.value))}
                >
                  <option value="0">Select datasource...</option>
                  ${this.datasources.map(ds => html`
                    <option value="${ds.id}">${ds.name} - ${ds.db_source_type || 'Unknown'}</option>
                  `)}
                </select>
                <button
                  type="button"
                  class="btn-clone"
                  @click=${this.handleCloneClick}
                  title="Clone selected datasource with different namespace"
                >
                  Clone
                </button>
              </div>
              <div class="hint">The datasource must have an embedder configured</div>
            </div>

            <div class="form-group">
              <label>Target Paths (comma-separated)</label>
              <input
                type="text"
                .value=${this.formData.target_paths.join(', ')}
                @input=${(e: any) => this.updateArrayField('target_paths', e.target.value)}
                placeholder="src/, docs/"
              />
              <div class="hint">Leave empty to include all paths</div>
            </div>

            <div class="form-group">
              <label>File Masks (comma-separated)</label>
              <input
                type="text"
                .value=${this.formData.file_masks.join(', ')}
                @input=${(e: any) => this.updateArrayField('file_masks', e.target.value)}
                placeholder="*.go, *.md, *.ts"
              />
            </div>

            <div class="form-row">
              <div class="form-group">
                <label>Chunking Strategy</label>
                <select
                  .value=${this.formData.chunking_strategy}
                  @change=${(e: any) => this.updateField('chunking_strategy', e.target.value)}
                >
                  <option value="simple">Simple</option>
                  <option value="code_aware">Code-Aware</option>
                  <option value="hybrid">Hybrid (Recommended)</option>
                </select>
              </div>

              <div class="form-group">
                <label>Chunk Size</label>
                <input
                  type="number"
                  min="100"
                  max="4000"
                  .value=${String(this.formData.chunk_size)}
                  @input=${(e: any) => this.updateField('chunk_size', parseInt(e.target.value))}
                />
              </div>
            </div>

            <div class="form-group">
              <label>Sync Schedule (cron expression)</label>
              <input
                type="text"
                .value=${this.formData.sync_schedule}
                @input=${(e: any) => this.updateField('sync_schedule', e.target.value)}
                placeholder="0 2 * * * (2 AM daily)"
              />
              <div class="hint">Leave empty for manual sync only. When a schedule is provided, automatic sync is enabled automatically.</div>
            </div>
          </div>

          <div class="actions">
            <button type="submit" class="btn btn-primary" ?disabled=${this.loading}>
              ${this.loading ? 'Saving...' : 'Save Repository'}
            </button>
            <button type="button" class="btn btn-secondary" @click=${this.handleCancel}>
              Cancel
            </button>
          </div>
        </form>
      </div>

      ${this.showCloneModal && this.selectedDatasourceForClone ? html`
        <datasource-clone-modal
          .sourceDatasource=${this.selectedDatasourceForClone}
          .api=${this.api}
          .onClose=${this.handleCloseModal.bind(this)}
          .onSuccess=${this.handleCloneSuccess.bind(this)}
        ></datasource-clone-modal>
      ` : ''}
    `;
  }
}
