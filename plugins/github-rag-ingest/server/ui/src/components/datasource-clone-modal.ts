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

@customElement('datasource-clone-modal')
export class DatasourceCloneModal extends LitElement {
  @property({ type: Object }) sourceDatasource: Datasource | null = null;
  @property({ type: Object }) api: PluginAPI | null = null;
  @property({ type: Function }) onClose: (() => void) | null = null;
  @property({ type: Function }) onSuccess: ((datasourceId: number) => void) | null = null;

  @state() private name = '';
  @state() private namespace = '';
  @state() private loading = false;
  @state() private error = '';

  static styles = css`
    .modal-overlay {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: rgba(0, 0, 0, 0.5);
      display: flex;
      align-items: center;
      justify-content: center;
      z-index: 1000;
    }

    .modal-content {
      background: white;
      border-radius: 8px;
      padding: 24px;
      width: 90%;
      max-width: 500px;
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
    }

    .modal-header {
      margin: 0 0 20px 0;
      font-size: 20px;
      font-weight: 600;
    }

    .form-group {
      margin-bottom: 16px;
    }

    label {
      display: block;
      margin-bottom: 6px;
      font-weight: 500;
      font-size: 14px;
    }

    input {
      width: 100%;
      padding: 8px 12px;
      border: 1px solid #ddd;
      border-radius: 4px;
      font-size: 14px;
      box-sizing: border-box;
    }

    input:focus {
      outline: none;
      border-color: #1976d2;
    }

    .hint {
      font-size: 12px;
      color: #666;
      margin-top: 4px;
    }

    .error {
      background: #ffebee;
      color: #c62828;
      padding: 12px;
      border-radius: 4px;
      margin-bottom: 16px;
      font-size: 14px;
    }

    .button-group {
      display: flex;
      gap: 12px;
      justify-content: flex-end;
      margin-top: 24px;
    }

    button {
      padding: 8px 16px;
      border-radius: 4px;
      font-size: 14px;
      cursor: pointer;
      border: none;
      transition: background 0.2s;
    }

    .btn-cancel {
      background: #f5f5f5;
      color: #333;
    }

    .btn-cancel:hover {
      background: #e0e0e0;
    }

    .btn-primary {
      background: #1976d2;
      color: white;
    }

    .btn-primary:hover {
      background: #1565c0;
    }

    .btn-primary:disabled {
      background: #ccc;
      cursor: not-allowed;
    }

    .source-info {
      background: #f5f5f5;
      padding: 12px;
      border-radius: 4px;
      margin-bottom: 16px;
      font-size: 13px;
    }

    .source-info strong {
      display: block;
      margin-bottom: 4px;
    }
  `;

  connectedCallback() {
    super.connectedCallback();
    if (this.sourceDatasource) {
      this.name = `Copy of ${this.sourceDatasource.name}`;
      this.namespace = this.sourceDatasource.db_name || '';
    }
  }

  private async handleSubmit(e: Event) {
    e.preventDefault();

    if (!this.api || !this.sourceDatasource) {
      this.error = 'API not available';
      return;
    }

    if (!this.name.trim()) {
      this.error = 'Name is required';
      return;
    }

    if (!this.namespace.trim()) {
      this.error = 'Namespace is required';
      return;
    }

    this.loading = true;
    this.error = '';

    try {
      // Step 1: Clone datasource (copies all config including API keys)
      const cloneResult = await this.api.call('clone_datasource', {
        source_datasource_id: this.sourceDatasource.id,
      });

      if (!cloneResult.success) {
        this.error = cloneResult.error || 'Failed to clone datasource';
        this.loading = false;
        return;
      }

      const newDatasourceId = cloneResult.data.datasource_id;

      // Step 2: Update name and namespace on cloned datasource
      const updateResult = await this.api.call('update_datasource_fields', {
        datasource_id: newDatasourceId,
        name: this.name.trim(),
        db_name: this.namespace.trim(),
      });

      if (updateResult.success) {
        if (this.onSuccess) {
          this.onSuccess(newDatasourceId);
        }
        if (this.onClose) {
          this.onClose();
        }
      } else {
        this.error = updateResult.error || 'Failed to update datasource fields';
      }
    } catch (err: any) {
      this.error = `Error: ${err.message}`;
    } finally {
      this.loading = false;
    }
  }

  private handleCancel() {
    if (this.onClose) {
      this.onClose();
    }
  }

  render() {
    if (!this.sourceDatasource) {
      return html``;
    }

    return html`
      <div class="modal-overlay" @click=${this.handleCancel}>
        <div class="modal-content" @click=${(e: Event) => e.stopPropagation()}>
          <h2 class="modal-header">Clone Datasource</h2>

          <div class="source-info">
            <strong>Cloning from:</strong>
            ${this.sourceDatasource.name}
            <div class="hint">
              All configuration including API keys will be copied
            </div>
          </div>

          ${this.error ? html`<div class="error">${this.error}</div>` : ''}

          <form @submit=${this.handleSubmit}>
            <div class="form-group">
              <label for="name">Datasource Name *</label>
              <input
                type="text"
                id="name"
                .value=${this.name}
                @input=${(e: any) => (this.name = e.target.value)}
                required
                ?disabled=${this.loading}
              />
            </div>

            <div class="form-group">
              <label for="namespace">Namespace / Collection Name *</label>
              <input
                type="text"
                id="namespace"
                .value=${this.namespace}
                @input=${(e: any) => (this.namespace = e.target.value)}
                required
                ?disabled=${this.loading}
              />
              <div class="hint">
                Vector store collection/namespace (db_name field)
              </div>
            </div>

            <div class="button-group">
              <button
                type="button"
                class="btn-cancel"
                @click=${this.handleCancel}
                ?disabled=${this.loading}
              >
                Cancel
              </button>
              <button
                type="submit"
                class="btn-primary"
                ?disabled=${this.loading}
              >
                ${this.loading ? 'Creating...' : 'Create Datasource'}
              </button>
            </div>
          </form>
        </div>
      </div>
    `;
  }
}
